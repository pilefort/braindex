package textblock

import "strings"

// MergePreservingOutside は範囲外の改行・空行をバイト単位で残す。
// 管理範囲は Merge で生成する。範囲が無い場合は必要な区切り改行だけを足す。
func MergePreservingOutside(existing, begin, end string, lines []string) string {
	start, stop := -1, len(existing)
	offset := 0
	for _, line := range strings.SplitAfter(existing, "\n") {
		marker := strings.TrimSpace(line)
		if start < 0 && marker == begin {
			start = offset
		} else if start >= 0 && marker == end {
			stop = offset + len(line)
			break
		}
		offset += len(line)
	}
	block := Merge("", begin, end, lines)
	if start < 0 {
		if block == "" {
			return existing
		}
		sep := ""
		if existing != "" && !strings.HasSuffix(existing, "\n") {
			sep = "\n"
		}
		return existing + sep + block
	}
	return existing[:start] + block + existing[stop:]
}
