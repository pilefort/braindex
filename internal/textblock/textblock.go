// Package textblock は区切り行で囲んだ範囲を差し替える。
package textblock

import "strings"

// Merge は CRLF を LF に揃えて範囲を差し替える。空の lines は範囲を削除する。
func Merge(existing, begin, end string, lines []string) string {
	before, _, after, found := splitBlock(existing, begin, end)
	if len(lines) == 0 {
		if !found {
			return normalize(existing)
		}
		return joinLines(append(before, after...))
	}
	block := make([]string, 0, len(lines)+2)
	block = append(block, begin)
	block = append(block, lines...)
	block = append(block, end)
	if !found {
		return joinLines(append(before, block...))
	}
	out := make([]string, 0, len(before)+len(block)+len(after))
	out = append(out, before...)
	out = append(out, block...)
	out = append(out, after...)
	return joinLines(out)
}

// Lines はマーカーを除く中身を返す。ブロックが無ければ nil。
func Lines(existing, begin, end string) []string {
	_, inside, _, found := splitBlock(existing, begin, end)
	if !found {
		return nil
	}
	return inside
}

// Split は範囲の前・中・後と、開始マーカーの有無を返す。
func Split(existing, begin, end string) (before, inside, after []string, found bool) {
	return splitBlock(existing, begin, end)
}

// splitBlock は crontab を「ブロックの前・中・後」に分ける。
// found=false のとき before は全行(末尾に足す前提)、inside と after は空。
func splitBlock(existing, beginMarker, endMarker string) (before, inside, after []string, found bool) {
	lines := splitLines(existing)
	begin, end := -1, -1
	for i, l := range lines {
		switch strings.TrimSpace(l) {
		case beginMarker:
			if begin < 0 {
				begin = i
			}
		case endMarker:
			if begin >= 0 && end < 0 {
				end = i
			}
		}
	}
	// 開始だけあって終了が無いファイルは、壊れた記録として末尾までをブロックとみなす
	// (次の install で正しい形に戻る。ブロック外の行を巻き込まないよう、開始が無ければ何もしない)。
	if begin < 0 {
		return lines, nil, nil, false
	}
	if end < 0 {
		// 最終行までが中身。end = len(lines)-1 にすると最終行を終了マーカーの位置とみなしてしまい、
		// 利用者が書き足した行が install のたびに 1 行ずつ消える。
		return lines[:begin], lines[begin+1:], nil, true
	}
	return lines[:begin], lines[begin+1 : end], lines[end+1:], true
}

// splitLines は改行(CRLF も)で分け、末尾の空行は落とす。
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// joinLines は行を crontab の全文にする。空でなければ必ず末尾を改行で終える
// (最終行に改行が無い crontab を受け付けない実装があるため)。
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// normalize は改行を LF に揃え、末尾の改行を 1 つにする。
func normalize(s string) string {
	return joinLines(splitLines(s))
}
