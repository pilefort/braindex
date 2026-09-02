// Package mdhtml は最小・自作の Markdown → HTML 変換。braindex answer が回答を自己完結 HTML にするための同梱依存。
//
// 原型は作者の answer_html.py に同梱されていた md_to_html.py(自前パーサ・決定的・純粋関数)。依存を足さず Go に写した。
// 見出し/表/リスト/チェックボックス/引用/コード/水平線/段落と、行内の強調・コード・リンク・[[wiki]] に対応する。
// 同じ入力からは同じ出力(決定性テストで担保)。外部 I/O は無い。
package mdhtml

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	h1RE       = regexp.MustCompile(`(?m)^#\s+(.+?)\s*$`)
	codeSpanRE = regexp.MustCompile("`([^`]+)`")
	boldRE     = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	linkRE     = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	wikiRE     = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
	stashRE    = regexp.MustCompile("\x00([0-9]+)\x00")

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

// inline は行内記法を HTML にする。コード退避 → エスケープ → wiki → リンク → 強調 → コード復帰、の順。
func inline(text string) string {
	var stash []string
	text = codeSpanRE.ReplaceAllStringFunc(text, func(m string) string {
		stash = append(stash, m[1:len(m)-1])
		return "\x00" + strconv.Itoa(len(stash)-1) + "\x00"
	})
	text = escapeText(text)
	text = wikiRE.ReplaceAllString(text, `<span class="wl">$1</span>`)
	text = linkRE.ReplaceAllStringFunc(text, func(m string) string {
		sm := linkRE.FindStringSubmatch(m)
		u := strings.ReplaceAll(strings.ReplaceAll(sm[2], `"`, "%22"), " ", "%20")
		return `<a href="` + u + `" target="_blank" rel="noopener">` + sm[1] + `</a>`
	})
	text = boldRE.ReplaceAllString(text, "<strong>$1</strong>")
	text = italic(text)
	return stashRE.ReplaceAllStringFunc(text, func(m string) string {
		idx, _ := strconv.Atoi(m[1 : len(m)-1])
		return "<code>" + escapeText(stash[idx]) + "</code>"
	})
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
func Body(md string) string {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	md = strings.ReplaceAll(md, "\r", "\n")
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
			out = append(out, "<h"+lv+">"+inline(strings.TrimSpace(m[2]))+"</h"+lv+">")
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
					body.WriteString("<td>" + inline(cell) + "</td>")
				}
				body.WriteString("</tr>")
			}
			var th strings.Builder
			for _, c := range header {
				th.WriteString("<th>" + inline(c) + "</th>")
			}
			out = append(out, "<table><thead><tr>"+th.String()+"</tr></thead><tbody>"+body.String()+"</tbody></table>")
			continue
		}

		if strings.HasPrefix(lstrip(line), ">") {
			var parts []string
			for i < n && strings.HasPrefix(lstrip(lines[i]), ">") {
				b := quoteRE.ReplaceAllString(lines[i], "")
				if strings.TrimSpace(b) != "" {
					parts = append(parts, inline(b))
				}
				i++
			}
			out = append(out, "<blockquote>"+strings.Join(parts, "<br>")+"</blockquote>")
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
					it.content = inline(content)
					items = append(items, it)
					i++
				} else if strings.TrimSpace(lines[i]) != "" && (strings.HasPrefix(lines[i], " ") || strings.HasPrefix(lines[i], "\t")) && len(items) > 0 {
					items[len(items)-1].content += " " + inline(strings.TrimSpace(lines[i]))
					i++
				} else {
					break
				}
			}
			out = append(out, buildList(items))
			continue
		}

		var buf []string
		for i < n && strings.TrimSpace(lines[i]) != "" && !isFence(lines[i]) {
			l2 := lines[i]
			if headingRE.MatchString(l2) || hrRE.MatchString(l2) || listItemRE.MatchString(l2) ||
				strings.HasPrefix(lstrip(l2), ">") || isTableStart(lines, i) {
				break
			}
			buf = append(buf, inline(strings.TrimSpace(l2)))
			i++
		}
		if len(buf) > 0 {
			out = append(out, "<p>"+strings.Join(buf, "<br>")+"</p>")
		}
	}
	return strings.Join(out, "\n")
}
