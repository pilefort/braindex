package mdhtml

// スレッド化した回答(braindex answer -append)の読み書きと描画。
// 1 つの話題に回答を重ねるための形で、新しいエントリが上に積まれ、既読のものは次に開いたとき畳まれる。
// エントリの境界は Markdown の記法でなく HTML コメントのマーカーで持つ——回答本文に `##` の見出しが
// あってもエントリが割れないようにするため(2026-09-06)。

import (
	"strconv"
	"strings"
	"time"
)

const (
	entryOpen  = "<!--braindex:entry " // マーカー行の始まり
	entryClose = "-->"                 // マーカー行の終わり
)

// Entry はスレッドの 1 エントリ(1 回の質問と、それへの回答)。
type Entry struct {
	At   string // 書いた日時(RFC3339)。表示と要素 id に使う。空なら日時不明
	Q    string // ユーザーの質問(逐語・改行は空白に潰す)。空なら見出しは日時だけ
	Body string // 回答の Markdown 本文
}

// ParseThread はスレッド .md をタイトルとエントリ列に分ける。エントリは書かれている順(新しいものが先)。
// マーカーが 1 つも無い .md は「タイトル＋日時不明の 1 エントリ」として読む(1 枚ものに追記できるようにするため)。
func ParseThread(md string) (string, []Entry) {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	md = strings.ReplaceAll(md, "\r", "\n")
	var head []string
	var entries []Entry
	var bodies [][]string
	cur := -1
	fence := false // コードブロックの中はマーカーに見えても境界にしない(回答にマーカーの例を書けるように)
	for _, ln := range strings.Split(md, "\n") {
		if isFence(ln) {
			fence = !fence
		}
		if at, q, ok := parseEntryLine(ln); ok && !fence {
			entries = append(entries, Entry{At: at, Q: q})
			bodies = append(bodies, nil)
			cur++
			continue
		}
		if cur < 0 {
			head = append(head, ln)
			continue
		}
		bodies[cur] = append(bodies[cur], ln)
	}
	for i := range entries {
		entries[i].Body = strings.Trim(strings.Join(bodies[i], "\n"), "\n")
	}
	title, rest := splitTitle(head)
	if cur < 0 && strings.TrimSpace(rest) != "" {
		entries = append(entries, Entry{Body: strings.Trim(rest, "\n")})
	}
	return title, entries
}

// RenderThread はタイトルとエントリ列をスレッド .md にする(ParseThread の逆)。
func RenderThread(title string, entries []Entry) string {
	var b strings.Builder
	if title != "" {
		b.WriteString("# " + title + "\n")
	}
	for _, e := range entries {
		b.WriteString("\n" + entryOpen + `at="` + escapeAttr(e.At) +
			`" q="` + escapeAttr(oneLine(e.Q)) + `"` + entryClose + "\n\n")
		b.WriteString(strings.Trim(e.Body, "\n") + "\n")
	}
	return b.String()
}

// Prepend は新しいエントリをスレッド .md の先頭(タイトルの直後)に足した .md を返す。
// title が空でスレッドにもタイトルが無いときは fallback を使う。
func Prepend(md, fallbackTitle string, e Entry) string {
	title, entries := ParseThread(md)
	if title == "" {
		title = fallbackTitle
	}
	return RenderThread(title, append([]Entry{e}, entries...))
}

// ThreadPage はスレッド .md を自己完結 HTML にする(CSS・JS 埋め込み・外部読み込み無し)。
func ThreadPage(md, title string) string {
	t, entries := ParseThread(md)
	if title == "" {
		title = t
	}
	var b strings.Builder
	b.WriteString("<h1>" + escapeText(title) + "</h1>\n")
	b.WriteString(`<div class="thr-bar">` +
		`<button class="thr-b" id="thr-open">全部開く</button>` +
		`<button class="thr-b" id="thr-close">全部畳む</button>` +
		`<button class="thr-b" id="thr-auto">自動更新</button>` + "</div>\n")
	// 同じ日時が並んだときの連番は古い方から数える。新しいエントリは上に足されるので、
	// 新しい順に数えると追記のたびに既存の id が 1 つずつずれ、開閉の記憶が別のエントリに移る。
	ids := make([]string, len(entries))
	used := map[string]int{}
	for i := len(entries) - 1; i >= 0; i-- {
		id := entryID(entries[i].At)
		used[id]++
		if n := used[id]; n > 1 {
			id += "-" + strconv.Itoa(n)
		}
		ids[i] = id
	}
	for i, e := range entries {
		id := ids[i]
		b.WriteString(`<details class="ent" id="` + id + `" open>` + "\n")
		b.WriteString(`<summary><span class="ent-w">` + escapeText(formatAt(e.At)) + `</span>`)
		if q := strings.TrimSpace(e.Q); q != "" {
			b.WriteString(`<span class="ent-q">` + escapeText(q) + `</span>`)
		}
		b.WriteString(`<span class="ent-n">新着</span></summary>` + "\n")
		b.WriteString(`<div class="ent-b">` + renderBody(e.Body) + "</div>\n</details>\n")
	}
	return shell(title, b.String(), threadJS)
}

// IsThread は .md がスレッド(エントリのマーカーを持つ)かどうかを返す。
// スレッドの .md をそのまま渡されたときに、1 枚ものでなくスレッドとして描き直すための判定。
func IsThread(md string) bool {
	fence := false
	for _, ln := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		if isFence(ln) {
			fence = !fence
		}
		if _, _, ok := parseEntryLine(ln); ok && !fence {
			return true
		}
	}
	return false
}

// SplitH1 は先頭(最初の空行でない行)が `# 見出し` ならタイトルとして切り出し、残りの本文と共に返す。
// スレッドに足すエントリの本文からタイトル行を落とすのに使う(エントリごとに h1 が並ばないように)。
func SplitH1(md string) (string, string) {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	md = strings.ReplaceAll(md, "\r", "\n")
	lines := strings.Split(md, "\n")
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if m := headingRE.FindStringSubmatch(ln); m != nil && len(m[1]) == 1 {
			return strings.TrimSpace(m[2]), strings.Trim(strings.Join(lines[i+1:], "\n"), "\n")
		}
		break
	}
	return "", strings.Trim(md, "\n")
}

// parseEntryLine は 1 行がエントリのマーカーなら日時と質問を返す。
func parseEntryLine(ln string) (at, q string, ok bool) {
	s := strings.TrimSpace(ln)
	if !strings.HasPrefix(s, entryOpen) || !strings.HasSuffix(s, entryClose) {
		return "", "", false
	}
	s = strings.TrimSuffix(s[len(entryOpen):], entryClose)
	at, rest, found := takeAttr(s, "at")
	if !found {
		return "", "", false
	}
	q, _, _ = takeAttr(rest, "q")
	return unescapeAttr(at), unescapeAttr(q), true
}

// takeAttr は `名前="値"` を先頭から探し、値とその後ろを返す。値は escapeAttr 済みなので裸の " を含まない。
func takeAttr(s, name string) (value, rest string, ok bool) {
	k := name + `="`
	i := strings.Index(s, k)
	if i < 0 {
		return "", s, false
	}
	r := s[i+len(k):]
	j := strings.IndexByte(r, '"')
	if j < 0 {
		return "", s, false
	}
	return r[:j], r[j+1:], true
}

// unescapeAttr は escapeAttr の逆。& は最後に戻す(二重復元を避けるため)。
func unescapeAttr(s string) string {
	s = strings.ReplaceAll(s, "&#x27;", "'")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	return strings.ReplaceAll(s, "&amp;", "&")
}

// splitTitle は行の並びから最初の `# 見出し` をタイトルとして取り出し、その行を除いた残りを返す。
func splitTitle(lines []string) (string, string) {
	for i, ln := range lines {
		if m := headingRE.FindStringSubmatch(ln); m != nil && len(m[1]) == 1 {
			rest := make([]string, 0, len(lines)-1)
			rest = append(rest, lines[:i]...)
			rest = append(rest, lines[i+1:]...)
			return strings.TrimSpace(m[2]), strings.Join(rest, "\n")
		}
	}
	return "", strings.Join(lines, "\n")
}

// oneLine は改行だけを空白にする(マーカーは 1 行のため)。逐語のまま残したいので他の空白は触らない。
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

// entryID は既読の記録に使う要素 id。同じ .md からは同じ id が出る(決定的)。
func entryID(at string) string {
	if t, err := time.Parse(time.RFC3339, at); err == nil {
		return "e-" + t.Format("20060102T150405")
	}
	s := strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return r
		}
		return -1
	}, at)
	if s == "" {
		s = "x"
	}
	return "e-" + s
}

// formatAt は見出しに出す日時。読めない値は素のまま出す(落とさない)。
func formatAt(at string) string {
	if at == "" {
		return "日時不明"
	}
	if t, err := time.Parse(time.RFC3339, at); err == nil {
		return t.Format("2006-01-02 15:04")
	}
	return at
}
