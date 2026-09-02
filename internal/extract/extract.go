// Package extract はノートからタイトル・日付・要旨を機械的に抽出する。
// LLM は使わない(決定性と再生成コストを優先。「結論を先頭に・日付を入れる」規約が効いている)。
package extract

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Meta は 1 ファイルから抽出した索引メタ情報。
type Meta struct {
	Title   string
	Date    string // "YYYY-MM-DD"。ファイル名にも本文にも無ければ ""(日付なし。mtime には頼らない)
	Summary string
}

const summaryRunes = 80

var (
	isoRe = regexp.MustCompile(`(\d{4})-(\d{2})-(\d{2})`)
	jpRe  = regexp.MustCompile(`(\d{4})年(\d{1,2})月(\d{1,2})日`)
	fn8Re = regexp.MustCompile(`20\d{6}`)
)

// Extract は name(ファイル名)・content(本文)・kind から Meta を作る。
func Extract(name string, content []byte, kind string) Meta {
	lines := splitLines(content)
	m := Meta{
		Title: extractTitle(lines, name),
		Date:  extractDate(name, lines),
	}
	if kind == "decisions" {
		m.Summary = summarizeDecisions(lines)
	} else {
		m.Summary = summarize(lines, firstH1Index(lines))
	}
	return m
}

// splitLines は BOM を除去し CRLF/CR を LF に正規化して行に分割する。
func splitLines(content []byte) []string {
	// UTF-8 BOM (EF BB BF) を除去。ソースに BOM リテラルを置かず、バイトで判定する。
	if len(content) >= 3 && content[0] == 0xEF && content[1] == 0xBB && content[2] == 0xBF {
		content = content[3:]
	}
	s := string(content)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}

func firstH1Index(lines []string) int {
	for i, l := range lines {
		if strings.HasPrefix(l, "# ") {
			return i
		}
	}
	return -1
}

// extractTitle は最初の "# " 行。無ければファイル名から .md を除いたもの。
func extractTitle(lines []string, name string) string {
	if i := firstH1Index(lines); i >= 0 {
		return strings.TrimSpace(lines[i][2:])
	}
	return strings.TrimSuffix(name, ".md")
}

// extractDate は ファイル名 → 本文先頭 10 行 の優先順で日付を決める。どちらにも無ければ ""。
// 月日が範囲外の候補(13 月・45 日など)は日付とみなさず、次の候補を探す。
// mtime にはフォールバックしない(git は mtime を保存しないので、clone ごとに索引が変わってしまう)。
func extractDate(name string, lines []string) string {
	if d := dateFromFilename(name); d != "" {
		return d
	}
	if d := dateFromContent(lines); d != "" {
		return d
	}
	return ""
}

func dateFromFilename(name string) string {
	if m := fn8Re.FindString(name); m != "" {
		y, mo, d := m[0:4], m[4:6], m[6:8]
		if validMD(mo, d) {
			return y + "-" + mo + "-" + d
		}
	}
	return isoDate(name)
}

func dateFromContent(lines []string) string {
	n := len(lines)
	if n > 10 {
		n = 10
	}
	for i := 0; i < n; i++ {
		if d := isoDate(lines[i]); d != "" {
			return d
		}
		if d := jpDate(lines[i]); d != "" {
			return d
		}
	}
	return ""
}

// isoDate は s 中の最初の「月日が範囲内の」YYYY-MM-DD を返す。無ければ ""。
func isoDate(s string) string {
	for _, m := range isoRe.FindAllStringSubmatch(s, -1) {
		if validMD(m[2], m[3]) {
			return m[1] + "-" + m[2] + "-" + m[3]
		}
	}
	return ""
}

// jpDate は s 中の最初の「月日が範囲内の」YYYY年M月D日 を YYYY-MM-DD にして返す。無ければ ""。
func jpDate(s string) string {
	for _, m := range jpRe.FindAllStringSubmatch(s, -1) {
		if validMD(m[2], m[3]) {
			mo, _ := strconv.Atoi(m[2])
			d, _ := strconv.Atoi(m[3])
			return fmt.Sprintf("%s-%02d-%02d", m[1], mo, d)
		}
	}
	return ""
}

// validMD は月が 1〜12・日が 1〜31 に収まるかだけを見る(月ごとの日数・閏年は見ない)。
func validMD(mo, d string) bool {
	m, _ := strconv.Atoi(mo)
	dd, _ := strconv.Atoi(d)
	return m >= 1 && m <= 12 && dd >= 1 && dd <= 31
}

// summarize はタイトル行より後の最初の「見出し・表行・コードフェンス・空行のいずれでもない」行を
// 要旨とする。そのような行が無ければ H2 見出しを連結して代用する。
func summarize(lines []string, titleIdx int) string {
	start := titleIdx + 1
	if titleIdx < 0 {
		start = 0
	}
	inFence := false
	var h2 []string
	for i := start; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || t == "" {
			continue
		}
		if strings.HasPrefix(t, "#") {
			if strings.HasPrefix(t, "## ") {
				h2 = append(h2, strings.TrimSpace(t[3:]))
			}
			continue
		}
		if strings.HasPrefix(t, "|") {
			continue
		}
		return truncateRunes(t, summaryRunes)
	}
	if len(h2) > 0 {
		return truncateRunes(strings.Join(h2, " / "), summaryRunes)
	}
	return ""
}

// summarizeDecisions は decisions.md 用。末尾側の H2 見出し 3 件を連結する
// (追記式なので末尾が最新の決定)。
func summarizeDecisions(lines []string) string {
	var h2 []string
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "## ") {
			h2 = append(h2, strings.TrimSpace(t[3:]))
		}
	}
	if len(h2) > 3 {
		h2 = h2[len(h2)-3:]
	}
	return truncateRunes(strings.Join(h2, " / "), summaryRunes)
}

// truncateRunes は rune 単位で n 文字に切り、切ったら … を付ける。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
