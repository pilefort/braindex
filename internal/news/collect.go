package news

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/pilefort/braindex/internal/feed"
)

// Fetcher はフィードを取得する側。実体は feed.Fetcher。テストでは差し替える。
type Fetcher interface {
	Fetch(ctx context.Context, url string) (feed.Document, error)
}

// Result はフィード 1 本の取得結果。
type Result struct {
	Source  Source
	Entries []feed.Entry // 取得した全記事(記載順)
	New     []feed.Entry // 既読に無い記事(Replay なら全記事)
	Err     error        // 取得・パースの失敗。nil でなければ Entries / New は空
}

// concurrency は同時に取得するフィードの本数。
const concurrency = 4

// Collect は各フィードを取得し、既読との差分を新着にして、既読に加える。
// 結果の順は srcs の順(取得は並行だが、既読への追加と出力は順に行うので決定的)。
// replay なら既読を見ず全記事を新着にし、既読も更新しない(再生成用)。
func Collect(ctx context.Context, f Fetcher, srcs []Source, seen Seen, today string, replay bool) []Result {
	results := make([]Result, len(srcs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	for i, s := range srcs {
		wg.Add(1)
		go func(i int, s Source) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			doc, err := f.Fetch(ctx, s.URL)
			r := Result{Source: s, Err: err}
			if err == nil {
				r.Entries = doc.Entries
			}
			results[i] = r
		}(i, s)
	}
	wg.Wait()
	for i := range results {
		r := &results[i]
		if r.Err != nil {
			continue
		}
		if replay {
			r.New = r.Entries
			continue
		}
		r.New = seen.FilterNew(r.Entries)
		seen.Mark(r.Entries, today)
	}
	return results
}

// Failed は取得に失敗した結果だけを返す。
func Failed(results []Result) []Result {
	var out []Result
	for _, r := range results {
		if r.Err != nil {
			out = append(out, r)
		}
	}
	return out
}

// AllFailed は 1 本も取得できなかったとき true(フィードが 0 本でも true)。
func AllFailed(results []Result) bool {
	return len(Failed(results)) == len(results)
}

// Digest は新着のダイジェスト(Markdown・LF)を組む。新着が無いフィードは書かず、失敗は末尾に列挙する。
// 各フィードは cap 件まで書き、超えた分は件数だけ書く。同じ入力からは同じバイト列になる。
func Digest(results []Result, layer, today string, cap int) []byte {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# ニュースダイジェスト %s（%s 層）\n\n", today, layer)
	total, feeds := 0, 0
	for _, r := range results {
		if r.Err == nil {
			feeds++
			total += len(r.New)
		}
	}
	fmt.Fprintf(&sb, "新着 %d 件（フィード %d 本）\n\n", total, feeds)
	for _, r := range results {
		if r.Err != nil || len(r.New) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "## %s（", r.Source.Name)
		if r.Source.Category != "" {
			fmt.Fprintf(&sb, "%s・", r.Source.Category)
		}
		fmt.Fprintf(&sb, "新着 %d 件）\n", len(r.New))
		for i, e := range r.New {
			if i >= cap {
				fmt.Fprintf(&sb, "- （上限 %d 件を超えた %d 件は省略）\n", cap, len(r.New)-cap)
				break
			}
			sb.WriteString("- ")
			if e.Published != "" {
				fmt.Fprintf(&sb, "%s ", e.Published)
			}
			fmt.Fprintf(&sb, "[%s](%s)\n", escapeTitle(e.Title), e.Link)
		}
		sb.WriteString("\n")
	}
	if failed := Failed(results); len(failed) > 0 {
		sb.WriteString("## 取得失敗\n")
		for _, r := range failed {
			fmt.Fprintf(&sb, "- %s: %v\n", r.Source.Name, r.Err)
		}
	}
	return []byte(sb.String())
}

// escapeTitle はリンクの文字列にできない文字を避ける(角括弧だけ。Markdown の他の記号はそのまま)。
func escapeTitle(s string) string {
	return strings.NewReplacer("[", "［", "]", "］").Replace(s)
}
