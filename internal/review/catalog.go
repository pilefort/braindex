package review

import (
	"fmt"
	"strings"

	"github.com/pilefort/braindex/internal/render"
)

// ParseCatalog は render.Render が書いた catalog.md を読み戻し、エントリ列を返す(Render の逆)。
//
// H2 見出し(## <リポ名>)をリポ名、その後に続く表の各行を 1 エントリとする。先頭の説明行・表の見出し行・
// 区切り行は読み飛ばす。Render はセル内の半角 | を全角 ｜ に置換しているので、行を | で割れば必ず 5 列になる。
// 5 列でない表行は壊れている(手で編集された)ので、無言で読み飛ばさずエラーにする。
// 入力の BOM と CRLF は正規化する(索引は LF で書くが、checkout で変換された可能性がある)。
func ParseCatalog(b []byte) ([]render.Entry, error) {
	var out []render.Entry
	repo := ""
	for i, line := range splitLines(b) {
		if strings.HasPrefix(line, "## ") {
			repo = strings.TrimSpace(line[3:])
			continue
		}
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "|") {
			continue
		}
		if repo == "" {
			return nil, fmt.Errorf("catalog %d 行目: リポの見出し(## )より前に表の行がある", i+1)
		}
		cells := splitRow(t)
		if len(cells) != 5 {
			return nil, fmt.Errorf("catalog %d 行目: 表の列が 5 でない(%d 列)。braindex が書いた形でない", i+1, len(cells))
		}
		if isHeaderRow(cells) || isSeparatorRow(cells) {
			continue
		}
		out = append(out, render.Entry{
			Repo:    repo,
			Date:    cells[0],
			Kind:    cells[1],
			Title:   cells[2],
			Summary: cells[3],
			Path:    cells[4],
		})
	}
	return out, nil
}

// splitRow は "| a | b | c |" を ["a","b","c"] にする(各セルは前後の空白を落とす)。
func splitRow(row string) []string {
	inner := strings.TrimSuffix(strings.TrimPrefix(row, "|"), "|")
	parts := strings.Split(inner, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func isHeaderRow(cells []string) bool {
	return cells[0] == "日付" && cells[4] == "パス"
}

func isSeparatorRow(cells []string) bool {
	for _, c := range cells {
		if strings.Trim(c, "-:") != "" {
			return false
		}
	}
	return true
}

// splitLines は BOM を除去し CRLF/CR を LF に正規化して行に分割する(extract と同じ規則)。
func splitLines(content []byte) []string {
	if len(content) >= 3 && content[0] == 0xEF && content[1] == 0xBB && content[2] == 0xBF {
		content = content[3:]
	}
	s := string(content)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}
