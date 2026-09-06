package review

import (
	"github.com/pilefort/braindex/internal/indexdata"
	"github.com/pilefort/braindex/internal/render"
	"strings"
)

// ParseCatalog は共通の読み取り処理を呼ぶ互換入口。
func ParseCatalog(b []byte) ([]render.Entry, error) {
	return indexdata.ParseCatalog(b)
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
