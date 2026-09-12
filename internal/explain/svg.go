package explain

import "strings"

// SanitizeSVG は .svg の中身を HTML に埋め込める形にして返す。
//
// 取り出すのは最初の <svg から最後の </svg> までで、前後の <?xml ?> や DOCTYPE は落とす
// (HTML の中では宣言が邪魔になる)。そのうえで <script> 要素と on... で始まる属性を落とす。
// <style> と <animate> は残す——図のアニメーションはこの 2 つで書くため(SPEC「除去」)。
// <svg> が見つからなければ空文字を返す。
func SanitizeSVG(s string) string {
	lo := strings.ToLower(s)
	start := indexOpenTag(lo, "svg")
	if start < 0 {
		return ""
	}
	end := strings.LastIndex(lo, "</svg>")
	if end < start {
		return ""
	}
	return sanitizeNodes(s[start : end+len("</svg>")])
}

// indexOpenTag は "<name" の開始位置を返す。name の直後が名前の続き(<svgx など)なら数えない。
func indexOpenTag(lo, name string) int {
	from := 0
	for {
		i := strings.Index(lo[from:], "<"+name)
		if i < 0 {
			return -1
		}
		i += from
		j := i + 1 + len(name)
		if j >= len(lo) || !isNameByte(lo[j]) {
			return i
		}
		from = i + 1
	}
}

// sanitizeNodes は SVG の断片を舐めて、<script> と on... 属性を落としたものを組み直す。
// コメント・CDATA・宣言はそのまま通す(中の > に引っかからないよう終端まで読む)。
func sanitizeNodes(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		j := strings.IndexByte(s[i:], '<')
		if j < 0 {
			b.WriteString(s[i:])
			break
		}
		b.WriteString(s[i : i+j])
		i += j
		rest := s[i:]

		if n, ok := passThrough(rest, "<!--", "-->"); ok {
			b.WriteString(rest[:n])
			i += n
			continue
		}
		if n, ok := passThrough(rest, "<![CDATA[", "]]>"); ok {
			b.WriteString(rest[:n])
			i += n
			continue
		}
		if strings.HasPrefix(rest, "<!") || strings.HasPrefix(rest, "<?") {
			n := strings.IndexByte(rest, '>') + 1
			if n <= 0 {
				n = len(rest)
			}
			b.WriteString(rest[:n])
			i += n
			continue
		}
		if strings.HasPrefix(rest, "</") {
			n := strings.IndexByte(rest, '>') + 1
			if n <= 0 {
				b.WriteString(rest)
				break
			}
			name := strings.ToLower(strings.TrimSpace(rest[2 : n-1]))
			b.WriteString("</" + name + ">")
			i += n
			continue
		}
		name, attrs, selfClose, n, ok := parseTag(rest)
		if !ok {
			// タグとして読めない裸の < 。文字として出す
			b.WriteString("&lt;")
			i++
			continue
		}
		if name == "script" {
			i += n
			if !selfClose {
				if k := indexCloseTag(strings.ToLower(s[i:]), "script"); k >= 0 {
					i += k
				} else {
					i = len(s)
				}
			}
			continue
		}
		b.WriteString("<" + name)
		for _, a := range attrs {
			b.WriteString(" " + a)
		}
		if selfClose {
			b.WriteString("/>")
		} else {
			b.WriteString(">")
		}
		i += n
	}
	return b.String()
}

// passThrough は rest が open で始まるとき、end までの長さを返す。end が無ければ末尾まで。
func passThrough(rest, open, end string) (int, bool) {
	if !strings.HasPrefix(rest, open) {
		return 0, false
	}
	k := strings.Index(rest[len(open):], end)
	if k < 0 {
		return len(rest), true
	}
	return len(open) + k + len(end), true
}

// indexCloseTag は "</name>" の直後までの長さを返す(lo は小文字化済み)。
func indexCloseTag(lo, name string) int {
	k := strings.Index(lo, "</"+name)
	if k < 0 {
		return -1
	}
	g := strings.IndexByte(lo[k:], '>')
	if g < 0 {
		return -1
	}
	return k + g + 1
}

// parseTag は s[0]=='<' から開始タグを読み、要素名(小文字)・出し直す属性・自己終了・タグ全体の長さを返す。
// 属性は on... で始まるものを落とし、値は " で囲み直す(同じ入力からは同じ形に揃える)。
func parseTag(s string) (name string, attrs []string, selfClose bool, n int, ok bool) {
	if len(s) < 2 || s[0] != '<' || !isNameByte(s[1]) {
		return "", nil, false, 0, false
	}
	i := 1
	for i < len(s) && isNameByte(s[i]) {
		i++
	}
	name = strings.ToLower(s[1:i])
	for i < len(s) {
		for i < len(s) && isSpaceByte(s[i]) {
			i++
		}
		if i >= len(s) {
			return "", nil, false, 0, false
		}
		if s[i] == '>' {
			return name, attrs, false, i + 1, true
		}
		if s[i] == '/' {
			if i+1 < len(s) && s[i+1] == '>' {
				return name, attrs, true, i + 2, true
			}
			i++
			continue
		}
		as := i
		for i < len(s) && isNameByte(s[i]) {
			i++
		}
		if i == as {
			i++ // 名前として読めない 1 文字は捨てて先へ進む
			continue
		}
		an := s[as:i]
		for i < len(s) && isSpaceByte(s[i]) {
			i++
		}
		val, hasVal := "", false
		if i < len(s) && s[i] == '=' {
			i++
			for i < len(s) && isSpaceByte(s[i]) {
				i++
			}
			if i < len(s) && (s[i] == '"' || s[i] == '\'') {
				q := s[i]
				i++
				vs := i
				for i < len(s) && s[i] != q {
					i++
				}
				val, hasVal = s[vs:i], true
				if i < len(s) {
					i++
				}
			} else {
				vs := i
				for i < len(s) && !isSpaceByte(s[i]) && s[i] != '>' && s[i] != '/' {
					i++
				}
				val, hasVal = s[vs:i], true
			}
		}
		if strings.HasPrefix(strings.ToLower(an), "on") {
			continue // イベントハンドラは落とす
		}
		if hasVal {
			attrs = append(attrs, an+`="`+strings.ReplaceAll(val, `"`, "&quot;")+`"`)
		} else {
			attrs = append(attrs, an)
		}
	}
	return "", nil, false, 0, false
}

func isNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '-' || c == '_' || c == ':' || c == '.'
}

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}
