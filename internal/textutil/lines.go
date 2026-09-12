// Package textutil はテキストの共通処理を提供する。
package textutil

import "strings"

// SplitLines は先頭の UTF-8 BOM を除去し、CRLF/CR を LF に正規化して分割する。
// 空入力と末尾の空行も strings.Split と同じく保持する。
func SplitLines(content []byte) []string {
	if len(content) >= 3 && content[0] == 0xEF && content[1] == 0xBB && content[2] == 0xBF {
		content = content[3:]
	}
	s := strings.ReplaceAll(string(content), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}
