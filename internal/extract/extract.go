// Package extract はノートからタイトル・日付・要旨を機械的に抽出する。
// LLM は使わない(決定性と再生成コストを優先。「結論を先頭に・日付を入れる」規約が効いている)。
package extract

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/pilefort/braindex/internal/textutil"
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
	// 要旨の候補にしない行(日付だけの行)。「結論を先頭に」の規約では日付行が結論の前に来ることがあり、
	// そのまま拾うと索引の要旨が全部「記録日: …」になる
	dateLineRe = regexp.MustCompile(`^(記録日|日付|更新日|作成日|Date)\s*[:：]`)
	// decisions.md の各決定に付く記録日の行
	recordDateRe = regexp.MustCompile(`^記録日\s*[:：]`)
)

// Extract は name(ファイル名)・content(本文)・kind から Meta を作る。
func Extract(name string, content []byte, kind string) Meta {
	lines := splitLines(content)
	m := Meta{
		Title: extractTitle(lines, name),
		Date:  extractDate(name, lines),
	}
	if kind == "decisions" {
		// decisions.md は 1 ファイルに決定が積み上がる追記式なので、他のノートと索引の作り方を変える。
		// 日付はファイル名や先頭 10 行でなく「一番新しい記録日」(最後に何か決めた日)、
		// タイトルには件数、要旨は末尾の 1 件(最後に決まったこと)。
		h2 := headings2(lines)
		m.Title = fmt.Sprintf("%s（%d 件）", m.Title, len(h2))
		if d := latestRecordDate(lines); d != "" {
			m.Date = d
		}
		if len(h2) > 0 {
			m.Summary = truncateRunes(h2[len(h2)-1], summaryRunes)
		}
	} else {
		m.Summary = summarize(lines, firstH1Index(lines))
	}
	return m
}

// headings2 は H2 見出しを出現順に返す(コードフェンスの中は数えない)。
func headings2(lines []string) []string {
	var out []string
	inFence := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.HasPrefix(t, "## ") {
			out = append(out, strings.TrimSpace(t[3:]))
		}
	}
	return out
}

// latestRecordDate は「記録日:」で始まる行に書かれた日付のうち最も新しいものを返す。無ければ ""。
// decisions.md は追記式で、ファイルの日付は「最後に何か決めた日」が知りたい情報なので、
// 先頭 10 行だけでなく全文を見る。
func latestRecordDate(lines []string) string {
	best := ""
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if !recordDateRe.MatchString(t) {
			continue
		}
		d := isoDate(t)
		if d == "" {
			d = jpDate(t)
		}
		if d > best { // "YYYY-MM-DD" は文字列の大小がそのまま日付の大小
			best = d
		}
	}
	return best
}

// splitLines は BOM を除去し CRLF/CR を LF に正規化して行に分割する。
func splitLines(content []byte) []string {
	return textutil.SplitLines(content)
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
		if strings.HasPrefix(t, "|") || dateLineRe.MatchString(t) {
			continue
		}
		return truncateRunes(t, summaryRunes)
	}
	if len(h2) > 0 {
		return truncateRunes(strings.Join(h2, " / "), summaryRunes)
	}
	return ""
}

// truncateRunes は rune 単位で n 文字に切り、切ったら … を付ける。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
