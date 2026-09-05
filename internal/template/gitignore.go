package template

import (
	"bytes"
	"strings"
)

// GitignorePath は hub の .gitignore(展開先からの相対)。雛形の中身は news の作業ファイルの除外だけ。
const GitignorePath = ".gitignore"

// MergeGitignore は existing(hub の今の .gitignore。無ければ nil)に tmpl(雛形)のうち無い行を足して返す。
//
// .gitignore は利用者が自分の行を持っていることが多いので、ファイル単位で「上書きしない」と news の行が
// 永久に入らない。行単位で見て、パターン行が 1 つでも欠けていれば雛形の塊(まだ無いコメント行と欠けたパターン行)を
// 末尾に足す。既に全部あれば existing をそのまま返す(changed=false)。既存の改行は触らず、足す行は
// 既存が CRLF なら CRLF、それ以外は LF にする(改行を混在させないため)。
func MergeGitignore(existing, tmpl []byte) (out []byte, changed bool) {
	have := map[string]bool{}
	for _, ln := range strings.Split(string(existing), "\n") {
		have[strings.TrimRight(ln, "\r")] = true
	}
	var add []string
	var comments []string
	for _, ln := range strings.Split(strings.TrimRight(string(tmpl), "\n"), "\n") {
		switch {
		case ln == "", have[ln]:
		case strings.HasPrefix(ln, "#"):
			comments = append(comments, ln)
		default:
			add = append(add, ln)
		}
	}
	if len(add) == 0 {
		return existing, false
	}
	eol := "\n"
	if bytes.Contains(existing, []byte("\r\n")) {
		eol = "\r\n"
	}
	var buf bytes.Buffer
	buf.Write(existing)
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		buf.WriteString(eol)
	}
	if len(existing) > 0 {
		buf.WriteString(eol) // 自分の行と雛形の塊の間を 1 行あける
	}
	for _, c := range comments {
		buf.WriteString(c + eol)
	}
	for _, ln := range add {
		buf.WriteString(ln + eol)
	}
	return buf.Bytes(), true
}
