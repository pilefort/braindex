// Package explain は解説の md を、目次と図を備えた自己完結の HTML にする。braindex explain の中身。
//
// LLM を呼ばず、同じ入力からは同じ出力になる(決定 2026-09-12)。解説の文章と図は書き手が md と .svg に書き、
// ここが担うのは「読みやすい HTML にする」ところだけ。Markdown → HTML の変換そのものは mdhtml に任せ、
// 図(`![図1: 説明](fig1.svg)`)と目次をこちらで組み立てる。
package explain

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/pilefort/braindex/internal/mdhtml"
)

// Options は変換の設定。
type Options struct {
	// BaseDir は md の置き場所。図の .svg と、本文に書かれた相対パスの基準にする。
	BaseDir string
}

var (
	// imgOnlyRE は 1 行まるごとが画像の行(段落が画像 1 つだけ)。
	imgOnlyRE = regexp.MustCompile(`^!\[([^\]]*)\]\(([^)]+)\)$`)
	// figAltRE は alt の「図N: 説明」。N を図番号に、残りをキャプションにする。
	figAltRE = regexp.MustCompile(`^図\s*([0-9]+)\s*[:：]?\s*(.*)$`)
)

// Render は解説の md を 1 枚の HTML にする。
// 返す problems は本文にも印を出した問題(図が無い等)。呼び出し側がこれを警告と終了コードにする。
func Render(md, title string, opt Options) (string, []string) {
	r := &renderer{opt: opt, md: mdhtml.Options{BaseDir: opt.BaseDir}, figNums: map[string]bool{}}
	// 図番号のリンクは本文を全部読んでから張る。「図1」が図より前に出てくることがあるため。
	body, items := addHeadingIDs(linkFigureRefs(r.body(md), r.figNums))
	return mdhtml.Shell(title, tocHTML(items)+body, mdhtml.Parts{CSS: css, JS: js, MainClass: "explain"}), r.problems
}

// renderer は 1 回の変換の途中経過。図の通し番号と、出た問題を持つ。
type renderer struct {
	opt      Options
	md       mdhtml.Options
	figs     int
	graphs   int
	figNums  map[string]bool // 本文に出てきた図番号(「図1」からのリンクを張れるか見る)
	problems []string
}

// body は md をブロックに切り分けて本文の HTML にする。
// 図の行だけを自分で組み立て、それ以外の地の文はひと続きのまま mdhtml に渡す
// (段落・表・リストの解釈を二重に持たないため)。
func (r *renderer) body(md string) string {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	md = strings.ReplaceAll(md, "\r", "\n")
	lines := strings.Split(md, "\n")
	var out, prose []string
	flush := func() {
		if len(prose) > 0 {
			out = append(out, wrapTables(mdhtml.RenderBody(strings.Join(prose, "\n"), r.md)))
			prose = nil
		}
	}
	inFence := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "```") {
			inFence = !inFence
		} else if !inFence {
			if alt, src, ok := figureLine(line); ok {
				flush()
				out = append(out, r.figure(alt, src))
				continue
			}
			if spec, probs, ok := parseGraphDirective(line); len(probs) > 0 || ok {
				r.problems = append(r.problems, probs...)
				flush()
				if html, next := r.graph(spec, ok, lines, i+1); html != "" {
					out = append(out, html)
					i = next - 1
					continue
				}
				continue // 指定の行は本文に出さない(読み取ったら捨てる)
			}
		}
		prose = append(prose, line)
	}
	flush()
	return strings.Join(out, "\n")
}

// figureLine は行が図の参照(行まるごとが .svg の画像)かを見る。
// 字下げのある行は対象外にする(リストの中の画像まで段落から切り離さないため)。
func figureLine(line string) (alt, src string, ok bool) {
	if line != strings.TrimSpace(line) || !strings.HasPrefix(line, "![") {
		return "", "", false
	}
	m := imgOnlyRE.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	src = strings.TrimSpace(m[2])
	if !strings.HasSuffix(strings.ToLower(src), ".svg") || hasScheme(src) {
		return "", "", false
	}
	return m[1], src, true
}

// figure は 1 枚の図を <figure> にする。.svg の中身を読んで埋め込み、alt の「図N: 説明」を figcaption に出す。
// 読めなければ落とさずに、その場所に「図 <パス> が無い」と出して問題に積む(SPEC「図が見つからないとき」)。
func (r *renderer) figure(alt, src string) string {
	r.figs++
	id := "figx-" + strconv.Itoa(r.figs)
	caption := alt
	if m := figAltRE.FindStringSubmatch(alt); m != nil {
		id = "fig-" + m[1]
		r.figNums[m[1]] = true
		caption = "図" + m[1]
		if rest := strings.TrimSpace(m[2]); rest != "" {
			caption += ": " + rest
		}
	}
	inner := ""
	if raw, err := os.ReadFile(r.resolve(src)); err != nil {
		inner = r.miss("図 " + src + " が無い")
	} else if svg := SanitizeSVG(string(raw)); strings.TrimSpace(svg) == "" {
		inner = r.miss("図 " + src + " に <svg> が無い")
	} else {
		inner = svg
	}
	out := `<figure id="` + id + `" class="bx-fig">` + "\n" + inner
	if strings.TrimSpace(caption) != "" {
		out += "\n<figcaption>" + escapeText(caption) + "</figcaption>"
	}
	return out + "\n</figure>"
}

// graph は graph の指定に続く表を、グラフと元の表の組にする。next は表の次の行の位置。
// 表はグラフの下に残す——数字そのものを読めるようにするため(決定 2026-09-12)。
// 指定の次の行に表が無ければ空文字を返し、指定の行だけを捨てる。
func (r *renderer) graph(spec graphSpec, drawable bool, lines []string, i int) (string, int) {
	if !isTableStart(lines, i) {
		r.problems = append(r.problems, "graph の指定の次の行に表が無い")
		return "", i
	}
	header, rows, raw, next := readTable(lines, i)
	svg := ""
	if drawable {
		s, probs := chartSVG(spec, header, rows)
		r.problems, svg = append(r.problems, probs...), s
	}
	r.graphs++
	table := wrapTables(mdhtml.RenderBody(strings.Join(raw, "\n"), r.md))
	return `<figure id="graph-` + strconv.Itoa(r.graphs) + `" class="bx-graph">` + "\n" +
		svg + "\n" + table + "\n</figure>", next
}

// miss は図の代わりに出す印。同じ文言を stderr にも出すため problems にも積む。
func (r *renderer) miss(msg string) string {
	r.problems = append(r.problems, msg)
	return `<div class="bx-miss">` + escapeText(msg) + `</div>`
}

// resolve は md に書かれた .svg のパスを実ファイルのパスにする。
func (r *renderer) resolve(src string) string {
	p := filepath.FromSlash(src)
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(r.opt.BaseDir, p)
}

// wrapTables は表を横スクロールできる箱に入れる。広い表が本文の幅を押し広げると、
// 表以外の行まで横に流れて読めなくなる(SPEC「表は横に溢れたらその表だけ横スクロールする」)。
// mdhtml が出す表は属性の無い <table> で、本文の文字は escapeText を通っているので取り違えない。
func wrapTables(html string) string {
	html = strings.ReplaceAll(html, "<table>", `<div class="bx-tw"><table>`)
	return strings.ReplaceAll(html, "</table>", "</table></div>")
}

// figRefRE は本文中の図番号の参照(「図1」「図 1」)。
var figRefRE = regexp.MustCompile(`図\s?([0-9]+)`)

// linkFigureRefs は本文の「図1」から、その図へ飛べるリンクを張る。
// 張るのは実際にある図番号だけ。タグの中と、<figure>・<a>・<code>・<pre>・見出しの中は触らない
// (figcaption の「図1:」が自分自身へのリンクになるのを避ける)。
func linkFigureRefs(html string, nums map[string]bool) string {
	if len(nums) == 0 {
		return html
	}
	text := func(s string) string {
		return figRefRE.ReplaceAllStringFunc(s, func(m string) string {
			n := figRefRE.FindStringSubmatch(m)[1]
			if !nums[n] {
				return m
			}
			return `<a class="bx-ref" href="#fig-` + n + `">` + m + "</a>"
		})
	}
	var b strings.Builder
	i := 0
	for i < len(html) {
		j := strings.IndexByte(html[i:], '<')
		if j < 0 {
			b.WriteString(text(html[i:]))
			break
		}
		b.WriteString(text(html[i : i+j]))
		i += j
		rest := html[i:]
		if skip := closerFor(rest); skip != "" {
			if k := strings.Index(rest, skip); k >= 0 {
				b.WriteString(rest[:k+len(skip)])
				i += k + len(skip)
				continue
			}
		}
		k := strings.IndexByte(rest, '>')
		if k < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:k+1])
		i += k + 1
	}
	return b.String()
}

// closerFor は中身を触らずに飛ばす要素なら、その閉じタグを返す。
func closerFor(rest string) string {
	switch {
	case strings.HasPrefix(rest, "<figure"):
		return "</figure>"
	case strings.HasPrefix(rest, "<a "), strings.HasPrefix(rest, "<a>"):
		return "</a>"
	case strings.HasPrefix(rest, "<code"):
		return "</code>"
	case strings.HasPrefix(rest, "<pre"):
		return "</pre>"
	}
	for _, lv := range []string{"1", "2", "3", "4", "5", "6"} {
		if strings.HasPrefix(rest, "<h"+lv+">") {
			return "</h" + lv + ">"
		}
	}
	return ""
}

// tocItem は目次の 1 行。level は 2(節) か 3(小節)。
type tocItem struct {
	level int
	id    string
	text  string // 見出しの HTML からタグを剥がしたもの(実体参照はそのまま)
}

// addHeadingIDs は本文の <h2>・<h3> に id を振り、目次の材料を返す。
// id は出現順の通し番号("s2"・"s2-1")にする。見出しの文字から作ると同じ見出しが 2 つあったとき衝突するため
// (SPEC「目次」)。mdhtml が出す見出しは属性の無い <h2> なので、そのまま id を足せる。
func addHeadingIDs(html string) (string, []tocItem) {
	var b strings.Builder
	var items []tocItem
	sec, sub, i := 0, 0, 0
	for i < len(html) {
		lv, p := 2, strings.Index(html[i:], "<h2>")
		if p3 := strings.Index(html[i:], "<h3>"); p3 >= 0 && (p < 0 || p3 < p) {
			lv, p = 3, p3
		}
		if p < 0 {
			b.WriteString(html[i:])
			break
		}
		open := "<h" + strconv.Itoa(lv) + ">"
		end := "</h" + strconv.Itoa(lv) + ">"
		start := i + p
		b.WriteString(html[i:start])
		k := strings.Index(html[start:], end)
		if k < 0 {
			b.WriteString(html[start:])
			break
		}
		inner := html[start+len(open) : start+k]
		id := ""
		if lv == 2 {
			sec, sub = sec+1, 0
			id = "s" + strconv.Itoa(sec)
		} else {
			sub++
			id = "s" + strconv.Itoa(sec) + "-" + strconv.Itoa(sub)
		}
		items = append(items, tocItem{level: lv, id: id, text: stripTags(inner)})
		b.WriteString("<h" + strconv.Itoa(lv) + ` id="` + id + `">` + inner + end)
		i = start + k + len(end)
	}
	return b.String(), items
}

// tocHTML は目次を組み立てる。見出しが無ければ空(目次の枠だけが残らないように)。
// 広い画面では CSS が横に固定し、狭い画面では本文の先頭で畳まれる(details なので JS 無しでも開ける)。
func tocHTML(items []tocItem) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<details id="bx-toc" class="bx-toc" open>` + "\n<summary>目次</summary>\n<nav><ul>")
	for _, it := range items {
		b.WriteString(`<li class="l` + strconv.Itoa(it.level) + `"><a href="#` + it.id + `">` + it.text + "</a></li>")
	}
	b.WriteString("</ul></nav>\n</details>\n")
	return b.String()
}

var tagRE = regexp.MustCompile(`<[^>]*>`)

// stripTags は見出しの HTML から行内のタグを剥がす。中身は mdhtml がエスケープ済みなので、そのまま出せる。
func stripTags(s string) string { return strings.TrimSpace(tagRE.ReplaceAllString(s, "")) }

// escapeText は & < > だけをエスケープする(mdhtml の同名の関数と同じ扱い)。
func escapeText(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	return strings.ReplaceAll(s, ">", "&gt;")
}

// hasScheme は URL のスキーム("https:" など)が付いているかを返す。1 文字は Windows のドライブ文字とみなす。
func hasScheme(s string) bool {
	i := strings.IndexByte(s, ':')
	if i < 1 {
		return false
	}
	if j := strings.IndexAny(s, "/?#"); j >= 0 && j < i {
		return false
	}
	return i != 1
}
