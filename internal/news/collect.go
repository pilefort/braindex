package news

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/pilefort/braindex/internal/feed"
	"github.com/pilefort/braindex/internal/interest"
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

// Ranking は新着の関心度。記事 ID → 採点。nil なら採点無し(全件を主要表示)。
type Ranking map[string]interest.Score

// Rank は各フィードの新着を関心プロファイルで採点する。プロファイルが空なら nil(採点無し)。
// 照合する文字列は 見出し＋概要。
func Rank(results []Result, p interest.Profile) Ranking {
	if len(p.Terms) == 0 {
		return nil
	}
	rk := Ranking{}
	for _, r := range results {
		for _, e := range r.New {
			rk[e.ID] = interest.Rate(p, e.Title+" "+e.Summary)
		}
	}
	return rk
}

// Split は新着を 主要(関心度 minScore 以上・降順・同点は記載順)と 関心外(未満・記載順)に分ける。
// rk が nil なら全件が主要(記載順)。
func Split(entries []feed.Entry, rk Ranking, minScore int) (main, low []feed.Entry) {
	if rk == nil {
		return entries, nil
	}
	for _, e := range entries {
		if rk[e.ID].Value >= minScore {
			main = append(main, e)
		} else {
			low = append(low, e)
		}
	}
	sort.SliceStable(main, func(i, j int) bool { return rk[main[i].ID].Value > rk[main[j].ID].Value })
	return main, low
}

// DigestOptions はダイジェストの体裁。
type DigestOptions struct {
	Layer    string  // 層の名前(見出し)
	Today    string  // 日付(見出し)
	Cap      int     // 1 フィードあたりの表示上限(主要・関心外それぞれ)
	Ranking  Ranking // 採点。nil なら一段(全件を主要)
	MinScore int     // 主要に入れる最低の関心度(Ranking が nil なら使わない)
}

// Digest は新着のダイジェスト(Markdown・LF)を組む。
// フィードごとに 主要(関心度 降順) → 関心外と判定(折りたたみ相当) の二段。新着が無いフィードは書かず、失敗は末尾に列挙する。
// 各段は Cap 件まで書き、超えた分は件数だけ書く。同じ入力からは同じバイト列になる。
func Digest(results []Result, o DigestOptions) []byte {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# ニュースダイジェスト %s（%s 層）\n\n", o.Today, o.Layer)
	total, feeds := 0, 0
	for _, r := range results {
		if r.Err == nil {
			feeds++
			total += len(r.New)
		}
	}
	fmt.Fprintf(&sb, "新着 %d 件（フィード %d 本）", total, feeds)
	if o.Ranking == nil {
		sb.WriteString("・採点なし")
	} else {
		fmt.Fprintf(&sb, "・関心度 %d 以上を主要表示", o.MinScore)
	}
	sb.WriteString("\n\n")
	for _, r := range results {
		if r.Err != nil || len(r.New) == 0 {
			continue
		}
		main, low := Split(r.New, o.Ranking, o.MinScore)
		fmt.Fprintf(&sb, "## %s（", r.Source.Name)
		if r.Source.Category != "" {
			fmt.Fprintf(&sb, "%s・", r.Source.Category)
		}
		fmt.Fprintf(&sb, "新着 %d 件", len(r.New))
		if o.Ranking != nil {
			fmt.Fprintf(&sb, "・主要 %d 件", len(main))
		}
		sb.WriteString("）\n")
		writeTier(&sb, main, o, "")
		if len(low) > 0 {
			fmt.Fprintf(&sb, "- 関心外と判定 %d 件:\n", len(low))
			writeTier(&sb, low, o, "  ")
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

// writeTier は 1 段を書く。indent は行頭の字下げ(関心外は 2 段目のリスト)。
func writeTier(sb *strings.Builder, entries []feed.Entry, o DigestOptions, indent string) {
	for i, e := range entries {
		if o.Cap > 0 && i >= o.Cap {
			fmt.Fprintf(sb, "%s- （上限 %d 件を超えた %d 件は省略）\n", indent, o.Cap, len(entries)-o.Cap)
			break
		}
		sb.WriteString(indent + "- ")
		if e.Published != "" {
			fmt.Fprintf(sb, "%s ", e.Published)
		}
		fmt.Fprintf(sb, "[%s](%s)", escapeTitle(e.Title), e.Link)
		if o.Ranking != nil {
			s := o.Ranking[e.ID]
			fmt.Fprintf(sb, " ★%d", s.Value)
			if len(s.Matched) > 0 {
				fmt.Fprintf(sb, "（%s）", strings.Join(s.Matched, "・"))
			}
		}
		sb.WriteString("\n")
	}
}

// escapeTitle はリンクの文字列にできない文字を避ける(角括弧だけ。Markdown の他の記号はそのまま)。
func escapeTitle(s string) string {
	return strings.NewReplacer("[", "［", "]", "］").Replace(s)
}
