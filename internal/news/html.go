package news

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strings"

	"github.com/pilefort/braindex/internal/feed"
	"github.com/pilefort/braindex/internal/weblink"
)

// SelectionType は選別 JSON の type。HTML の「選別を書き出す」が出し、news apply が読む。
const SelectionType = "braindex-news-selection"

// SelectionPrefix は選別 JSON のファイル名の接頭辞(Downloads から拾う目印)。
const SelectionPrefix = "braindex-news-selection_"

// RenderHTML は選別 UI 付きの自己完結 HTML(外部 JS・CSS 無し・LF)を組む。
// フィードは分類(category)ごとにまとめ、各フィードで 主要 → 関心外と判定(<details> で折りたたみ) の順。
// 「残す／不要」はブラウザの localStorage に覚え、「選別を書き出す」で JSON をダウンロードする(サーバ不要)。
// 同じ入力からは同じバイト列になる(生成日時を入れない。日付は o.Today)。
// luckyPick は関心外から拾い上げた 1 件（どのフィード由来かを覚えておく）。
type luckyPick struct {
	e feed.Entry
	r Result
}

func RenderHTML(results []Result, o DigestOptions) []byte {
	byCat := map[string][]Result{}
	var lucky []luckyPick
	var failures, empty []string
	totalNew, totalMain := 0, 0
	for _, r := range results {
		if r.Err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", r.Source.Name, r.Err))
			continue
		}
		if len(r.New) == 0 {
			empty = append(empty, r.Source.Name)
			continue
		}
		byCat[r.Source.Category] = append(byCat[r.Source.Category], r)
		totalNew += len(r.New)
	}
	cats := make([]string, 0, len(byCat))
	for c := range byCat {
		cats = append(cats, c)
	}
	sort.Strings(cats)

	var parts, overview strings.Builder
	for _, cat := range cats {
		category := cat
		if category == "" {
			category = "その他"
		}
		count := 0
		var highlights []string
		for _, r := range byCat[cat] {
			count += len(r.New)
			for _, e := range r.New {
				if len(highlights) >= 2 {
					break
				}
				title := e.Title
				if a, ok := o.Annotations[e.ID]; ok && a.Title != "" {
					title = a.Title
				}
				runes := []rune(title)
				if len(runes) > 40 {
					title = string(runes[:40]) + "…"
				}
				highlights = append(highlights, title)
			}
		}
		fmt.Fprintf(&overview, `<button class="topic" data-category="%s"><strong>%s →</strong><span>%s</span><small>新着 %d 件</small></button>`, esc(cat), esc(category), esc(strings.Join(highlights, " ／ ")), count)
		fmt.Fprintf(&parts, `<section class="category" data-category="%s">`, esc(cat))
		if cat != "" {
			fmt.Fprintf(&parts, "<h2>%s</h2>\n", esc(cat))
		}
		for _, r := range byCat[cat] {
			parts.WriteString(`<section class="feed-group">`)
			main, low := Split(r.New, o.Ranking, o.MinScore)
			var picked []feed.Entry
			low, picked = TakeSerendipity(low, o.Serendipity)
			for _, e := range picked {
				lucky = append(lucky, luckyPick{e: e, r: r})
			}
			shown := capped(main, o.Cap)
			totalMain += len(shown)
			fmt.Fprintf(&parts, "<h3>%s（新着 %d 件", esc(r.Source.Name), len(r.New))
			if o.Ranking != nil {
				fmt.Fprintf(&parts, "・主要 %d 件", len(main))
			}
			parts.WriteString("）</h3>\n<ul>\n")
			for _, e := range shown {
				parts.WriteString(itemHTML(e, r, o, false))
			}
			parts.WriteString("</ul>\n")
			if len(main) > len(shown) {
				fmt.Fprintf(&parts, "<small>…他 %d 件は省略（上限 %d 件/フィード）</small>\n", len(main)-len(shown), o.Cap)
			}
			if len(low) > 0 {
				fmt.Fprintf(&parts, "<details class=\"lowbox\"><summary>ほかの記事 %d 件（おすすめ以外も見る）</summary>\n<ul>\n", len(low))
				lowShown := capped(low, o.Cap)
				for _, e := range lowShown {
					parts.WriteString(itemHTML(e, r, o, true))
				}
				parts.WriteString("</ul>\n")
				if len(low) > len(lowShown) {
					fmt.Fprintf(&parts, "<small>…他 %d 件は省略</small>\n", len(low)-len(lowShown))
				}
				parts.WriteString("</details>\n")
			}
			parts.WriteString("</section>\n")
		}
		parts.WriteString("</section>\n")
	}
	if len(lucky) > 0 {
		// 関心の外から拾い上げた記事。カテゴリの絞り込みでは消えないよう、独立した枠に置く。
		fmt.Fprintf(&parts, `<section class="category serendipity"><h2>%s</h2>`, esc(SerendipityLabel))
		parts.WriteString("<p class=\"intro\">関心の外と判定した記事から、日替わりで選びました。読まないと決めた分野の外側を見るための枠です。</p>\n")
		parts.WriteString("<section class=\"feed-group\"><ul>\n")
		for _, x := range lucky {
			parts.WriteString(itemHTML(x.e, x.r, o, true))
		}
		parts.WriteString("</ul>\n</section>\n</section>\n")
	}
	items := parts.String()
	if items == "" {
		items = "<p>新着はありません。</p>\n"
	}

	var foot []string
	if len(empty) > 0 {
		foot = append(foot, "新着なし: "+esc(strings.Join(empty, ", ")))
	}
	if len(failures) > 0 {
		foot = append(foot, "<b class=\"prune\">取得失敗:</b> "+esc(strings.Join(failures, " / ")))
	}
	if len(o.Totals) > 0 {
		cands := map[string]bool{}
		for _, f := range PruneCandidates(o.Totals, PruneMinShown) {
			cands[f] = true
		}
		names := make([]string, 0, len(o.Totals))
		for f := range o.Totals {
			names = append(names, f)
		}
		sort.Strings(names)
		var lines []string
		for _, f := range names {
			d := o.Totals[f]
			line := fmt.Sprintf("%s: 残す %d / 見た %d", esc(f), d.Kept, d.Shown)
			if d.Hidden > 0 {
				line += fmt.Sprintf("・関心外 %d 件中 救済 %d", d.Hidden, d.Rescued)
			}
			if cands[f] {
				line += " <b class=\"prune\">← 間引き候補（一度も残していない）</b>"
			}
			lines = append(lines, line)
		}
		foot = append(foot, "<b>選別の反映状況（累積）:</b><br>"+strings.Join(lines, "<br>")+
			"<br><small>救済 = 関心外と判定されたのに残した件数（採点の見逃し）。増えるフィードは news/interests.md に関心語を足す。"+
			"間引きは news/feeds.json から該当行を消す。</small>")
	}
	scoring, determinism := "採点なし（全件を主要表示）", "取得・既読・採点は規則ベース"
	if o.Ranking != nil {
		scoring = fmt.Sprintf("関心度は語の一致（braindex news profile）。%d 以上を主要表示", o.MinScore)
		if n := LLMScored(results, o.Ranking); n > 0 {
			scoring = fmt.Sprintf("関心度は語の一致（braindex news profile）に LLM 補助（news.llm・%d 件・バッジの説明に LLM と出る）を重ねたもの。%d 以上を主要表示", n, o.MinScore)
			determinism = "取得・既読は規則ベース、採点に LLM 補助あり"
		}
	}
	foot = append(foot, fmt.Sprintf("生成: %s / braindex news fetch（%s。%s）", esc(o.Today), determinism, scoring))

	title := fmt.Sprintf("ニュースダイジェスト %s（%s 層・新着 %d 件・主要 %d 件）", o.Today, o.Layer, totalNew, totalMain)
	reading := Reading{Articles: map[string]*ReadingArticle{}, Receipts: map[string]string{}}
	if o.Reading != nil {
		reading = *o.Reading
	}
	rb, _ := json.Marshal(reading)
	library := "false"
	heading := "今日、気になる話を見つける。"
	intro := "まず要点をつかんで、読みたい記事を「あとで読む」へ。"
	if o.Library {
		library = "true"
		title = "あとで読む — ニュース"
		heading = "気になった記事を、続きを読める場所へ。"
		intro = "保存した記事と解説。分からないところは、同じ記事から続けて相談できます。"
	}
	out := strings.NewReplacer(
		"__TITLE__", esc(title),
		"__HEADING__", esc(heading), "__INTRO__", esc(intro), "__OVERVIEW__", overview.String(),
		"__READING_JS__", string(rb), "__LIBRARY_JS__", library, "__LIBRARY_HREF__", esc(o.LibraryHref),
		"__CSS__", uiCSS, "__JS__", uiJS,
		"__DATE_JS__", jsString(o.Today),
		"__LAYER_JS__", jsString(o.Layer),
		"__ITEMS__", items,
		"__FOOTER__", strings.Join(foot, "<br><br>"),
	).Replace(htmlTemplate)
	return []byte(out)
}

// capped は es を limit 件までに切る(limit <= 0 なら切らない)。
// 引数名は組み込みの cap を隠さないよう limit にする。
func capped(es []feed.Entry, limit int) []feed.Entry {
	if limit > 0 && len(es) > limit {
		return es[:limit]
	}
	return es
}

func esc(s string) string { return html.EscapeString(s) }

// jsString は <script> の中に置ける JS の文字列リテラル(引用符を含む)にする。
// HTML の実体参照は <script> の中では復号されないので、esc では値が壊れる(層に & や " が入ると
// &amp; のまま JS の値になり、選別 JSON の layer が実際の層と食い違う)。
// encoding/json は既定で < > & をユニコードエスケープに逃がすので、閉じタグで script の外に出ることもない。
func jsString(s string) string {
	b, err := json.Marshal(s)
	if err != nil { // string の Marshal は失敗しないが、握りつぶさずに安全側へ倒す
		return `""`
	}
	return string(b)
}

// itemHTML は 1 項目の <li>。data-* に選別 JSON へ書く値を持たせる。low は折りたたみ側。
// o.Serendipity に入っている記事にはラベルを付ける(関心外から日替わりで拾い上げたもの)。
func itemHTML(e feed.Entry, r Result, o DigestOptions, low bool) string {
	cls, lowFlag := "item", "0"
	if low {
		cls, lowFlag = "item low", "1"
	}
	badge := ""
	if o.Serendipity[e.ID] {
		cls += " lucky"
		badge = `<span class="tag lucky">` + esc(SerendipityLabel) + `</span>`
	}
	score, reason := "", ""
	if s, ok := o.Ranking[e.ID]; ok {
		score = fmt.Sprint(s.Value)
		if len(s.Matched) > 0 {
			reason = "関心に合った語: " + strings.Join(s.Matched, "・")
		} else {
			reason = "関心度: " + score
		}
		if s.Value >= o.MinScore {
			reason = "おすすめ · " + reason
		}
	}
	link := e.Link
	if !weblink.Safe(link) {
		link = ""
	}
	title, summary := e.Title, e.Summary
	translation := ""
	if a, ok := o.Annotations[e.ID]; ok {
		if a.Title != "" {
			title = a.Title
			translation = "日本語訳（自動）"
		}
		if a.Summary != "" {
			summary = a.Summary
			translation = "日本語訳（自動）"
		}
	}
	if summary == "" {
		summary = "概要がありません。原文を開くか、解説を相談できます。"
	}
	category := r.Source.Category
	if category == "" {
		category = "その他"
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<li class="%s" data-id="%s" data-title="%s" data-link="%s" data-feed="%s" data-cat="%s" data-low="%s" data-r="%s" data-summary="%s">`, cls, esc(e.ID), esc(e.Title), esc(link), esc(r.Source.Name), esc(r.Source.Category), lowFlag, score, esc(summary))
	fmt.Fprintf(&b, `<div class="meta"><span class="tag">%s</span>%s<span>%s · %s</span></div><h3 class="article-title">%s</h3><p class="sum">%s</p>`, esc(category), badge, esc(r.Source.Name), esc(e.Published), esc(title), esc(summary))
	if translation != "" {
		fmt.Fprintf(&b, `<small>%s</small>`, translation)
	}
	disabled := ""
	if link == "" {
		disabled = ` disabled title="安全な原文リンクが無いため保存・相談できません"`
	}
	fmt.Fprintf(&b, `<div class="btns"><button class="bk"%s>＋ あとで読む</button><button class="explain"%s>解説してもらう</button><button class="bd">今回は見送る</button>`, disabled, disabled)
	if link != "" {
		fmt.Fprintf(&b, `<a href="%s" target="_blank" rel="noopener">原文を開く ↗</a>`, esc(link))
	}
	b.WriteString(`</div><div class="reading-controls" hidden><label>読む状態 <select class="reading-status"><option value="later">あとで読む</option><option value="deep">詳しく知りたい</option><option value="done">概要で足りた・読了</option><option value="hold">保留</option><option value="try">試したい</option><option value="none">興味なし</option></select></label></div><p class="item-status" aria-live="polite"></p>`)
	fmt.Fprintf(&b, `<details class="original"><summary>元の見出し・概要%s</summary><p>%s</p><p>%s</p><p>%s</p></details></li>`, func() string {
		if reason != "" {
			return "・おすすめの理由"
		}
		return ""
	}(), esc(e.Title), esc(e.Summary), esc(reason))
	return b.String() + "\n"
}

//go:embed ui.html
var htmlTemplate string

//go:embed ui.css
var uiCSS string

//go:embed ui.js
var uiJS string
