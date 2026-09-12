package news

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/pilefort/braindex/internal/feed"
	"github.com/pilefort/braindex/internal/interest"
	"github.com/pilefort/braindex/internal/weblink"
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
//
// demoted に名前がある取材先は、点の上限を DemotedMaxScore に下げる(不要ばかり付く取材先を
// 主要表示から下ろす。決定 2026-09-06 → manual/news.md「決めたこと」)。nil なら下げない。
func Rank(results []Result, p interest.Profile, demoted map[string]bool) Ranking {
	if len(p.Terms) == 0 {
		return nil
	}
	rk := Ranking{}
	rater := interest.NewRater(p) // 重み表は 1 回だけ作る(記事ごとに作り直さない)
	for _, r := range results {
		for _, e := range r.New {
			rk[e.ID] = rater.Rate(e.Title + " " + e.Summary)
		}
	}
	return CapDemoted(rk, results, demoted)
}

// CapDemoted は demoted に名前がある取材先の記事の点を DemotedMaxScore まで下げる(当たった語は残す)。
// 点を書き換えた後(LLM の採点を重ねた後など)にもう一度呼んでよい。rk か demoted が空ならそのまま返す。
//
// 採点の後で点を上書きする経路(ApplyAnnotations)があるので、下げるのは Rank の中だけにしない。
// 中だけにすると「上限を 1 に下げた」と言いながら LLM の 3 で主要表示に出る(外部レビュー 2026-09-12)。
func CapDemoted(rk Ranking, results []Result, demoted map[string]bool) Ranking {
	if rk == nil || len(demoted) == 0 {
		return rk
	}
	for _, r := range results {
		if !demoted[r.Source.Name] {
			continue
		}
		for _, e := range r.New {
			if s, ok := rk[e.ID]; ok && s.Value > DemotedMaxScore {
				s.Value = DemotedMaxScore
				rk[e.ID] = s
			}
		}
	}
	return rk
}

// Split は新着を 主要(関心度 minScore 以上・降順・同点は記載順)と 関心外(未満・記載順)に分ける。
// rk が nil なら全件が主要(記載順)。rk に無い記事は未採点として主要に入れ、並べ替えでは minScore と同じ扱い(原型と同じ)。
func Split(entries []feed.Entry, rk Ranking, minScore int) (main, low []feed.Entry) {
	if rk == nil {
		return entries, nil
	}
	value := func(e feed.Entry) int {
		if s, ok := rk[e.ID]; ok {
			return s.Value
		}
		return minScore
	}
	for _, e := range entries {
		if value(e) >= minScore {
			main = append(main, e)
		} else {
			low = append(low, e)
		}
	}
	sort.SliceStable(main, func(i, j int) bool { return value(main[i]) > value(main[j]) })
	return main, low
}

// SerendipityLabel は「関心外だが今日だけ拾い上げた」記事に付ける名前。
const SerendipityLabel = "もしかして興味あるかも"

// PickSerendipity は関心外と判定した記事から n 件を選ぶ（意図しない発見のため）。
// 関心度が高い方(=関心の周辺)から先に選ぶ。0 は無関係・宣伝・人事・相場と判定されたもので、毎日出す価値が薄い。
// 同点は「日付＋記事 ID」のハッシュ順。1 つのフィードから 2 件は選ばない。
// 同じ日・同じ入力なら毎回同じ記事になる(日付が変われば変わる)。
func PickSerendipity(results []Result, rk Ranking, minScore, n int, day string) map[string]bool {
	if n <= 0 || rk == nil {
		return nil
	}
	type cand struct {
		id, feed, key string
		score         int
	}
	var cands []cand
	for _, r := range results {
		if r.Err != nil {
			continue
		}
		_, low := Split(r.New, rk, minScore)
		for _, e := range low {
			sum := sha256.Sum256([]byte(day + "\x00" + e.ID))
			cands = append(cands, cand{id: e.ID, feed: r.Source.Name, key: hex.EncodeToString(sum[:8]), score: rk[e.ID].Value})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].key < cands[j].key
	})
	picks, seen := map[string]bool{}, map[string]bool{}
	for _, c := range cands {
		if len(picks) >= n {
			break
		}
		if seen[c.feed] {
			continue
		}
		seen[c.feed] = true
		picks[c.id] = true
	}
	return picks
}

// TakeSerendipity は関心外の並びから、選ばれた記事を抜き出す。残りと抜いた分を返す
// (同じ記事が「ほかの記事」と両方に出ると、選別の状態が壊れるため)。
func TakeSerendipity(low []feed.Entry, picks map[string]bool) (rest, picked []feed.Entry) {
	if len(picks) == 0 {
		return low, nil
	}
	for _, e := range low {
		if picks[e.ID] {
			picked = append(picked, e)
		} else {
			rest = append(rest, e)
		}
	}
	return rest, picked
}

// DigestOptions はダイジェストの体裁。
type DigestOptions struct {
	Layer       string               // 層の名前(見出し)
	Today       string               // 日付(見出し)
	Cap         int                  // 1 フィードあたりの表示上限(主要・関心外それぞれ)
	Ranking     Ranking              // 採点。nil なら一段(全件を主要)
	MinScore    int                  // 主要に入れる最低の関心度(Ranking が nil なら使わない)
	Totals      map[string]FeedStats // 選別の累積(フィード別)。HTML の脚注に出す。nil なら出さない
	Annotations Annotations          // LLM 補助の注釈(訳)。nil なら訳を出さない
	Reading     *Reading             // 記事の相談・回答・取り込み確認。
	Library     bool                 // 日付をまたぐ保存記事の一覧。
	LibraryHref string               // 生成先から読書一覧への相対URL（CLIが組む）。
	Serendipity map[string]bool      // 関心外から拾い上げる記事の ID(PickSerendipity)。nil なら枠を出さない
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
	var lucky []feed.Entry
	for _, r := range results {
		if r.Err != nil || len(r.New) == 0 {
			continue
		}
		main, low := Split(r.New, o.Ranking, o.MinScore)
		var picked []feed.Entry
		low, picked = TakeSerendipity(low, o.Serendipity)
		lucky = append(lucky, picked...)
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
	if len(lucky) > 0 {
		fmt.Fprintf(&sb, "## %s（%d 件）\n", SerendipityLabel, len(lucky))
		sb.WriteString("関心の外と判定した記事から、日替わりで選びました。\n")
		writeTier(&sb, lucky, o, "")
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
		// HTML(itemHTML)・keep(appendKeeps)と同じく安全でないリンクは載せない
		// (決定 2026-09-03「生成物のリンクは http(s) 以外を落とす」→ manual/design.md「決めたこと」)。
		// リンクを付けず題名だけを出す(HTML 側と同じ扱い)。
		if weblink.Safe(e.Link) {
			fmt.Fprintf(sb, "[%s](%s)", escapeTitle(e.Title), e.Link)
		} else {
			sb.WriteString(escapeTitle(e.Title))
		}
		if s, ok := o.Ranking[e.ID]; ok { // 無い＝未採点(★ を付けない)
			fmt.Fprintf(sb, " ★%d", s.Value)
			if len(s.Matched) > 0 {
				fmt.Fprintf(sb, "（%s）", strings.Join(s.Matched, "・"))
			}
		}
		if tr := o.Annotations.translation(e.ID); tr != "" {
			fmt.Fprintf(sb, "／訳: %s", escapeTitle(tr))
		}
		sb.WriteString("\n")
	}
}

// escapeTitle はリンクの文字列にできない文字を避ける(角括弧だけ。Markdown の他の記号はそのまま)。
func escapeTitle(s string) string {
	return strings.NewReplacer("[", "［", "]", "］").Replace(s)
}
