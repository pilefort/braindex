// Package mdhtml は最小・自作の Markdown → HTML 変換。braindex answer が回答を自己完結 HTML にするための同梱依存。
//
// 原型は作者の answer_html.py に同梱されていた md_to_html.py(自前パーサ・決定的・純粋関数)。依存を足さず Go に写した。
// 見出し/表/リスト/チェックボックス/引用/コード/水平線/段落と、行内の強調・コード・リンク・画像・[[wiki]] に対応する。
// 同じ入力からは同じ出力(決定性テストで担保)。外部 I/O は無い。
package mdhtml

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pilefort/braindex/internal/weblink"
)

// Options は変換の設定。ゼロ値(何も指定しない)が従来の動き。
type Options struct {
	// BaseDir は md に書かれた相対パスを解決する基準ディレクトリ(ふつうは md の置き場所)。
	// 空なら相対パスをそのまま出す。
	BaseDir string

	// SoftWrap は段落・引用の中の改行を「エディタの折り返し」として扱う。
	// 文の終わりで終わる行と、行末に空白 2 つを置いた行の後ろだけ <br> にし、
	// 途中で切れた行は次の行と続ける(→ softwrap.go)。
	// 既定(false)は 1 行 1 <br> で、answer の md の書き方を変えない。
	SoftWrap bool
}

var (
	h1RE       = regexp.MustCompile(`(?m)^#\s+(.+?)\s*$`)
	codeSpanRE = regexp.MustCompile("`([^`]+)`")
	boldRE     = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	linkRE     = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	imgRE      = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	wikiRE     = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	stashRE    = regexp.MustCompile("\x00([0-9]+)\x00")
	tagRE      = regexp.MustCompile(`<[^>]*>`)
	driveRE    = regexp.MustCompile(`^[A-Za-z]:[\\/]`) // Windows の絶対パス(C:/ や C:\)

	tableSepRE = regexp.MustCompile(`^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)+\|?\s*$`)
	listItemRE = regexp.MustCompile(`^(\s*)([-*+]|[0-9]+\.)\s+(.*)$`)
	taskRE     = regexp.MustCompile(`^\[( |x|X)\]\s+(.*)$`)
	headingRE  = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	hrRE       = regexp.MustCompile(`^(-{3,}|\*{3,}|_{3,})\s*$`)
	quoteRE    = regexp.MustCompile(`^\s*>\s?`)
)

// ExtractTitle は先頭の `# 見出し` をタイトルにする。無ければファイル名(拡張子 .md を除く)。
func ExtractTitle(md, filename string) string {
	if m := h1RE.FindStringSubmatch(md); m != nil {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSuffix(filename, ".md")
}

// escapeText は & < > だけをエスケープする(原型の html.escape(quote=False) と同じ)。
func escapeText(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	return strings.ReplaceAll(s, ">", "&gt;")
}

// escapeAttr は属性値・title 用。引用符もエスケープする(原型の html.escape(quote=True) と同じ)。
func escapeAttr(s string) string {
	s = escapeText(s)
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return strings.ReplaceAll(s, "'", "&#x27;")
}

func lstrip(s string) string { return strings.TrimLeftFunc(s, unicode.IsSpace) }

// splitRow は表の 1 行を | で分割し、前後の空セルを落として各セルを trim する。
func splitRow(line string) []string {
	cells := strings.Split(strings.TrimSpace(line), "|")
	if len(cells) > 0 && strings.TrimSpace(cells[0]) == "" {
		cells = cells[1:]
	}
	if len(cells) > 0 && strings.TrimSpace(cells[len(cells)-1]) == "" {
		cells = cells[:len(cells)-1]
	}
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

// italic は *text* を <em> にする。原型の正規表現 (?<![\*\w])\*([^*\n]+)\*(?!\*) を手で写した
// (Go の regexp は先読み・後読みを持たない)。
func italic(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] != '*' {
			b.WriteByte(s[i])
			i++
			continue
		}
		// 直前が * か単語文字なら開き記号ではない
		if i > 0 {
			r, _ := utf8.DecodeLastRuneInString(s[:i])
			if r == '*' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
				b.WriteByte('*')
				i++
				continue
			}
		}
		end := strings.IndexAny(s[i+1:], "*\n")
		if end <= 0 || s[i+1+end] != '*' || (i+2+end < len(s) && s[i+2+end] == '*') {
			b.WriteByte('*')
			i++
			continue
		}
		b.WriteString("<em>")
		b.WriteString(s[i+1 : i+1+end])
		b.WriteString("</em>")
		i = i + 2 + end
	}
	return b.String()
}

// attrURL は href / src に出す値。属性を閉じる " と、URL に載らない空白だけを % 表記にする。
func attrURL(u string) string {
	return strings.ReplaceAll(strings.ReplaceAll(u, `"`, "%22"), " ", "%20")
}

// hasScheme は URL のスキーム("https:" など)が付いているかを返す。
// weblink.Safe と同じ読み方をする: "://" より前に / ? # があればスキーム区切りではなく、
// 1 文字のスキームは Windows のドライブ文字として扱う。
func hasScheme(s string) bool {
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return false
	}
	if j := strings.IndexAny(s, "/?#"); j >= 0 && j < i {
		return false
	}
	if i == 1 {
		if r := rune(s[0]); ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') {
			return false // Windows のドライブ文字
		}
	}
	return true
}

// isRelativeLocal は BaseDir からの解決の対象かを返す。スキーム付き・絶対パス・同一文書内リンク(#)は対象外。
func isRelativeLocal(p string) bool {
	return p != "" && !strings.HasPrefix(p, "#") && !strings.HasPrefix(p, "/") &&
		!driveRE.MatchString(p) && !hasScheme(p)
}

// localURL は href / src に出す値。ローカルの絶対パス(Windows のドライブ文字・/ 始まり)は file:// の URL にする。
// HTML は一時置き場に書かれ Markdown と同じ場所に無いので、絶対パスで参照させる。"C:/..." を素のまま出しても
// file と解釈するかはブラウザと OS 次第なので明示する。
// opt.BaseDir があれば相対パスもそこからの絶対パスにする。HTML が md と別のディレクトリに書かれる以上、
// 相対のままでは解決できない(2026-09-12 実測: `![図](./fig.svg)` が開けなかった)。BaseDir が空なら従来どおりそのまま。
// Markdown 側で file:// と書いたものは、リンクと同じく落とす(決定 2026-09-03。パスで書けばよい → manual/design.md「決めたこと」)。
func localURL(p string, opt Options) string {
	p = strings.TrimSpace(p)
	if opt.BaseDir != "" && isRelativeLocal(p) {
		path, frag := p, ""
		if i := strings.IndexByte(p, '#'); i >= 0 {
			path, frag = p[:i], p[i:]
		}
		if path != "" {
			// 区切りは / に戻す。Windows の Join は \ を返し、下の判定(先頭の / とドライブ文字)に掛からない
			p = filepath.ToSlash(filepath.Join(opt.BaseDir, filepath.FromSlash(path))) + frag
		}
	}
	switch {
	case driveRE.MatchString(p):
		p = "file:///" + strings.ReplaceAll(p, `\`, "/")
	case strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//"):
		p = "file://" + p
	}
	return attrURL(p)
}

// inline は行内記法を HTML にする。コード退避 → エスケープ → wiki → 画像 → リンク → 強調 → 復帰、の順。
// 退避表には復帰時にそのまま出す HTML を入れる。行内コードのほか、<img> も入れて後段の強調・リンクに触らせない
// (alt や src の中の * や [ を記法として解釈させないため)。
func inline(text string, opt Options) string {
	var stash []string
	keep := func(html string) string {
		stash = append(stash, html)
		return "\x00" + strconv.Itoa(len(stash)-1) + "\x00"
	}
	restore := func(s string) string {
		return stashRE.ReplaceAllStringFunc(s, func(m string) string {
			idx, err := strconv.Atoi(m[1 : len(m)-1])
			if err != nil || idx < 0 || idx >= len(stash) {
				return m // 自分が書いた目印ではない。触らずに残す
			}
			return stash[idx]
		})
	}
	text = codeSpanRE.ReplaceAllStringFunc(text, func(m string) string {
		return keep("<code>" + escapeText(m[1:len(m)-1]) + "</code>")
	})
	text = escapeText(text)
	text = wikiRE.ReplaceAllString(text, `<span class="wl">$1</span>`)
	text = imgRE.ReplaceAllStringFunc(text, func(m string) string {
		sm := imgRE.FindStringSubmatch(m)
		if !weblink.Safe(sm[2]) {
			// リンクと同じ判定。javascript: や data: は src に出さず、alt の文字だけ残す
			return sm[1]
		}
		// alt は属性値なので、退避したコードは文字に戻し、タグは剥がし、" をエスケープする
		alt := strings.ReplaceAll(tagRE.ReplaceAllString(restore(sm[1]), ""), `"`, "&quot;")
		return keep(`<img src="` + localURL(sm[2], opt) + `" alt="` + alt + `">`)
	})
	text = linkRE.ReplaceAllStringFunc(text, func(m string) string {
		sm := linkRE.FindStringSubmatch(m)
		if !weblink.Safe(sm[2]) {
			// javascript: のようなスキームは href に出さず、文字だけ残す(リンクの文言は消さない)
			return sm[1]
		}
		// 画像と同じく、ローカルの絶対パスは file:// にする。素のまま出すと file:// のページからは
		// 相対パスとして解決されて開けない(実測 2026-09-06)
		return `<a href="` + localURL(sm[2], opt) + `" target="_blank" rel="noopener">` + sm[1] + `</a>`
	})
	text = boldRE.ReplaceAllString(text, "<strong>$1</strong>")
	text = italic(text)
	return restore(text)
}

type listItem struct {
	indent  int
	ordered bool
	check   int // -1 なし / 0 未 / 1 済
	content string
}

// buildList は項目列を入れ子の ul/ol にする。
func buildList(items []listItem) string {
	type frame struct {
		indent int
		tag    string
	}
	var out strings.Builder
	var stack []frame
	pop := func() {
		out.WriteString("</" + stack[len(stack)-1].tag + ">")
		stack = stack[:len(stack)-1]
	}
	for _, it := range items {
		tag := "ul"
		if it.ordered {
			tag = "ol"
		}
		for len(stack) > 0 && it.indent < stack[len(stack)-1].indent {
			pop()
		}
		if len(stack) == 0 || it.indent > stack[len(stack)-1].indent {
			out.WriteString("<" + tag + ">")
			stack = append(stack, frame{it.indent, tag})
		} else if stack[len(stack)-1].tag != tag {
			pop()
			out.WriteString("<" + tag + ">")
			stack = append(stack, frame{it.indent, tag})
		}
		switch it.check {
		case -1:
			out.WriteString("<li>" + it.content + "</li>")
		case 1:
			out.WriteString(`<li class="task"><input type="checkbox" disabled checked> ` + it.content + "</li>")
		default:
			out.WriteString(`<li class="task"><input type="checkbox" disabled> ` + it.content + "</li>")
		}
	}
	for len(stack) > 0 {
		pop()
	}
	return out.String()
}

func isFence(line string) bool { return strings.HasPrefix(lstrip(line), "```") }

func isTableStart(lines []string, i int) bool {
	return strings.Contains(lines[i], "|") && i+1 < len(lines) && tableSepRE.MatchString(lines[i+1])
}

// Body は Markdown をブロック要素の HTML(本文だけ・<html> 無し)にする。
func Body(md string) string { return BodyWith(md, Options{}) }

// BodyWith は Body に変換の設定を渡す形。相対パスの基準(Options.BaseDir)を指定できる。
func BodyWith(md string, opt Options) string {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	md = strings.ReplaceAll(md, "\r", "\n")
	md = strings.ReplaceAll(md, "\x00", "") // 行内コードの退避に使う番兵と衝突するので落とす
	lines := strings.Split(md, "\n")
	n := len(lines)
	var out []string
	i := 0
	for i < n {
		line := lines[i]

		if isFence(line) {
			lang := strings.TrimSpace(lstrip(line)[3:])
			i++
			var buf []string
			for i < n && !isFence(lines[i]) {
				buf = append(buf, lines[i])
				i++
			}
			i++
			cls := ""
			if lang != "" {
				cls = ` class="lang-` + escapeAttr(lang) + `"`
			}
			out = append(out, "<pre><code"+cls+">"+escapeText(strings.Join(buf, "\n"))+"</code></pre>")
			continue
		}

		if strings.TrimSpace(line) == "" {
			i++
			continue
		}

		if m := headingRE.FindStringSubmatch(line); m != nil {
			lv := strconv.Itoa(len(m[1]))
			out = append(out, "<h"+lv+">"+inline(strings.TrimSpace(m[2]), opt)+"</h"+lv+">")
			i++
			continue
		}

		if hrRE.MatchString(line) {
			out = append(out, "<hr>")
			i++
			continue
		}

		if isTableStart(lines, i) {
			header := splitRow(line)
			i += 2
			var body strings.Builder
			for i < n && strings.Contains(lines[i], "|") && strings.TrimSpace(lines[i]) != "" {
				r := splitRow(lines[i])
				i++
				body.WriteString("<tr>")
				for c := range header {
					cell := ""
					if c < len(r) {
						cell = r[c]
					}
					body.WriteString("<td>" + inline(cell, opt) + "</td>")
				}
				body.WriteString("</tr>")
			}
			var th strings.Builder
			for _, c := range header {
				th.WriteString("<th>" + inline(c, opt) + "</th>")
			}
			out = append(out, "<table><thead><tr>"+th.String()+"</tr></thead><tbody>"+body.String()+"</tbody></table>")
			continue
		}

		if strings.HasPrefix(lstrip(line), ">") {
			var parts, raw []string
			for i < n && strings.HasPrefix(lstrip(lines[i]), ">") {
				b := quoteRE.ReplaceAllString(lines[i], "")
				if strings.TrimSpace(b) != "" {
					parts = append(parts, inline(b, opt))
					raw = append(raw, b)
				}
				i++
			}
			out = append(out, "<blockquote>"+joinLines(parts, raw, opt)+"</blockquote>")
			continue
		}

		if listItemRE.MatchString(line) {
			var items []listItem
			for i < n {
				if lm := listItemRE.FindStringSubmatch(lines[i]); lm != nil {
					it := listItem{
						indent:  utf8.RuneCountInString(strings.ReplaceAll(lm[1], "\t", "    ")),
						ordered: strings.HasSuffix(lm[2], "."),
						check:   -1,
					}
					content := lm[3]
					if tm := taskRE.FindStringSubmatch(content); tm != nil {
						it.check = 0
						if strings.EqualFold(tm[1], "x") {
							it.check = 1
						}
						content = tm[2]
					}
					it.content = inline(content, opt)
					items = append(items, it)
					i++
				} else if strings.TrimSpace(lines[i]) != "" && (strings.HasPrefix(lines[i], " ") || strings.HasPrefix(lines[i], "\t")) && len(items) > 0 {
					items[len(items)-1].content += " " + inline(strings.TrimSpace(lines[i]), opt)
					i++
				} else {
					break
				}
			}
			out = append(out, buildList(items))
			continue
		}

		var buf, raw []string
		for i < n && strings.TrimSpace(lines[i]) != "" && !isFence(lines[i]) {
			l2 := lines[i]
			if headingRE.MatchString(l2) || hrRE.MatchString(l2) || listItemRE.MatchString(l2) ||
				strings.HasPrefix(lstrip(l2), ">") || isTableStart(lines, i) {
				break
			}
			buf = append(buf, inline(strings.TrimSpace(l2), opt))
			raw = append(raw, l2)
			i++
		}
		if len(buf) > 0 {
			out = append(out, "<p>"+joinLines(buf, raw, opt)+"</p>")
		}
	}
	return strings.Join(out, "\n")
}
