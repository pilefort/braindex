package news

// 概要の画面。会話で書いた「まとめて概要」の Markdown を、記事ごとに仕分けできる HTML にする。
// 選別が終わったあとの流れ: まとめて概要を頼む → この画面で仕分ける → 詳しく知りたい分だけをまとめて頼む。
// 記事の区切りは Markdown の中の <!--braindex-article id=... question=... link=...--> の行。

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"

	"github.com/pilefort/braindex/internal/mdhtml"
	"github.com/pilefort/braindex/internal/weblink"
)

//go:embed overview.css
var overviewCSS string

//go:embed overview.js
var overviewJS string

// OverviewMark は記事の区切り。Markdown のコメントなので、素の Markdown ビューアでは見えない。
const OverviewMark = "braindex-article"

// OverviewArticle は概要 1 件。
type OverviewArticle struct {
	ID       string // 記事 ID(news reading -id に渡す)
	Question string // 概要を頼んだときの相談 ID(記録用。空でもよい)
	Link     string // 原文の URL
	Title    string // 本文の最初の見出し。無ければ ID
	Body     string // 本文(Markdown)
}

var overviewMarkRe = regexp.MustCompile(`(?m)^<!--\s*` + OverviewMark + `\s+([^>]*?)-->\s*$`)
var overviewAttrRe = regexp.MustCompile(`([a-z]+)=("[^"]*"|\S+)`)
var overviewHeadRe = regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*$`)

// ParseOverview は概要の Markdown を前書きと記事に分ける。区切りが 1 つも無ければ記事は空で返る
// (その場合は呼び出し側が「区切りが無い」と伝える。黙って 1 件にまとめない)。
func ParseOverview(md string) (intro string, arts []OverviewArticle) {
	locs := overviewMarkRe.FindAllStringSubmatchIndex(md, -1)
	if len(locs) == 0 {
		return strings.TrimSpace(md), nil
	}
	intro = strings.TrimSpace(md[:locs[0][0]])
	for i, loc := range locs {
		end := len(md)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		a := OverviewArticle{Body: strings.TrimSpace(md[loc[1]:end])}
		for _, kv := range overviewAttrRe.FindAllStringSubmatch(md[loc[2]:loc[3]], -1) {
			v := strings.Trim(kv[2], `"`)
			switch kv[1] {
			case "id":
				a.ID = v
			case "question":
				a.Question = v
			case "link":
				a.Link = v
			case "title":
				a.Title = v
			}
		}
		if a.Title == "" {
			if m := overviewHeadRe.FindStringSubmatch(a.Body); m != nil {
				a.Title = m[1]
			} else {
				a.Title = a.ID
			}
		}
		if !weblink.Safe(a.Link) {
			a.Link = ""
		}
		arts = append(arts, a)
	}
	return intro, arts
}

// RenderOverview は仕分けできる HTML を組む(自己完結・外部読み込み無し・外へ送らない)。
// id は保存の鍵(同じ概要を開き直すと前の仕分けが戻る)。
func RenderOverview(id, title, intro string, arts []OverviewArticle) []byte {
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"ja\">\n<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	fmt.Fprintf(&b, "<title>%s</title>\n<style>%s</style>\n</head>\n<body>\n", esc(title), overviewCSS)
	b.WriteString(`<main class="doc">`)
	fmt.Fprintf(&b, "<h1>%s</h1>\n", esc(title))
	if intro != "" {
		fmt.Fprintf(&b, `<div class="intro">%s</div>`+"\n", mdhtml.Linkify(mdhtml.Body(intro)))
	}
	b.WriteString(`<ol class="arts">` + "\n")
	for _, a := range arts {
		fmt.Fprintf(&b, `<li class="art" data-id="%s" data-q="%s" data-link="%s" data-title="%s">`,
			esc(a.ID), esc(a.Question), esc(a.Link), esc(a.Title))
		fmt.Fprintf(&b, `<div class="body">%s</div>`, mdhtml.Linkify(mdhtml.Body(a.Body)))
		b.WriteString(`<fieldset><legend>この記事は？</legend><div class="choices">`)
		for _, c := range []struct{ v, label string }{
			{"deep", "詳しく知りたい"}, {"done", "概要で足りた"}, {"none", "興味なし"},
		} {
			fmt.Fprintf(&b, `<label><input type="radio" name="s-%s" value="%s"><span>%s</span></label>`,
				esc(a.ID), c.v, esc(c.label))
		}
		b.WriteString("</div></fieldset></li>\n")
	}
	b.WriteString("</ol>\n</main>\n")
	b.WriteString(`<div class="bar"><span id="progress"></span><button id="ask" class="primary" disabled>詳しく知りたい分を頼む <span id="n">0</span></button></div>` + "\n")
	b.WriteString(`<dialog id="d"><div class="dhead"><strong id="dhead"></strong><button id="close">閉じる</button></div>` +
		`<textarea id="req" readonly aria-label="この会話へ貼り付ける相談文"></textarea>` +
		`<p class="note"><button id="copy">相談文をコピー</button> コピーは送信ではありません。会話に貼って送ってください。</p>` +
		`<p class="note" id="copyState" role="status" aria-live="polite"></p></dialog>` + "\n")
	fmt.Fprintf(&b, "<script>const META={id:%s};\n%s</script>\n</body>\n</html>\n", jsString(id), overviewJS)
	return []byte(b.String())
}
