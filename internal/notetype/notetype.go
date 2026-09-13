// Package notetype は本文の内容の種別を読み書きする。
package notetype

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/pilefort/braindex/internal/textutil"
)

const (
	Failure     = "失敗"
	Howto       = "手順"
	Observation = "観測"
	None        = "未記入"
)

var typeRe = regexp.MustCompile(`^種別\s*[:：](.*)$`)
var recordDateRe = regexp.MustCompile(`^記録日\s*[:：]`)

// Normalize は CLI の別名を日本語にする。空文字は絞り込みなし。
func Normalize(value string) (string, error) {
	switch value {
	case "", Failure, Howto, Observation, None:
		return value, nil
	case "failure":
		return Failure, nil
	case "howto":
		return Howto, nil
	case "observation":
		return Observation, nil
	case "none":
		return None, nil
	}
	return "", fmt.Errorf("内容の種別が不正: %q（失敗・手順・観測・未記入）", value)
}

func Valid(value string) bool { return value == Failure || value == Howto || value == Observation }

// Field はコードフェンスの外にある種別行。
type Field struct {
	Value string
	Line  int
}

// looksLikeProse は値が 3 語の書き間違いでなく文(空白を含む・9 文字以上)かを見る。
func looksLikeProse(v string) bool {
	return strings.ContainsAny(v, " 	　") || len([]rune(v)) > 8
}

// Fields は本文全体の種別行を返す(値が文のものは除く)。lint は 11 行目以降も調べる。
func Fields(content []byte) []Field {
	lines := textutil.SplitLines(content)
	outside := outsideFence(lines)
	var fields []Field
	for i, line := range lines {
		if !outside[i] {
			continue
		}
		if m := typeRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			v := strings.TrimSpace(m[1])
			if looksLikeProse(v) {
				continue // 「種別: **字幕**＝…」のように語を別の意味で使った行(実ノートで観測)。種別行と見なさない
			}
			fields = append(fields, Field{v, i + 1})
		}
	}
	return fields
}

func outsideFence(lines []string) []bool {
	out := make([]bool, len(lines))
	var marker byte
	width := 0
	for i, line := range lines {
		t := strings.TrimSpace(line)
		n := 0
		if len(t) > 0 && (t[0] == '`' || t[0] == '~') {
			for n < len(t) && t[n] == t[0] {
				n++
			}
		}
		if marker == 0 {
			if n >= 3 {
				marker, width = t[0], n
				continue
			}
			out[i] = true
		} else if n >= width && t[0] == marker && strings.TrimSpace(t[n:]) == "" {
			marker = 0
		}
	}
	return out
}

// Parse は先頭 10 行から最初の種別を採る。値の誤りと重複は行番号つきで返す。
func Parse(content []byte) (value string, line int, err error) {
	lines := textutil.SplitLines(content)
	if len(lines) > 10 {
		lines = lines[:10]
	}
	var problems []string
	for _, f := range Fields([]byte(strings.Join(lines, "\n"))) {
		if line == 0 {
			value, line = f.Value, f.Line
		} else {
			problems = append(problems, fmt.Sprintf("種別行が 2 行ある（%d 行目と %d 行目）", line, f.Line))
		}
		if !Valid(f.Value) {
			problems = append(problems, fmt.Sprintf("%d 行目: 種別の値が不正: %q", f.Line, f.Value))
		}
	}
	if len(problems) > 0 {
		err = fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return
}

// Insert は改行コードと BOM を保って種別を挿入する。失敗時は元の本文を返す。
func Insert(content []byte, value string) ([]byte, error) {
	v, err := Normalize(value)
	if err != nil || !Valid(v) {
		return content, fmt.Errorf("書き込めない種別: %q", value)
	}
	lines := textutil.SplitLines(content)
	fields := Fields(content)
	pos := -1
	if len(fields) > 1 {
		return content, fmt.Errorf("種別行が 2 行ある（%d 行目と %d 行目）", fields[0].Line, fields[1].Line)
	}
	if len(fields) == 1 {
		pos = fields[0].Line - 1
	} else {
		outside := outsideFence(lines)
		for i, l := range lines {
			if i >= 10 {
				break
			}
			if outside[i] && recordDateRe.MatchString(strings.TrimSpace(l)) {
				pos = i + 1
				break
			}
		}
		if pos < 0 {
			pos = 0
			for i, l := range lines {
				if outside[i] && strings.HasPrefix(l, "# ") {
					pos = i + 1
					if pos < len(lines) && strings.TrimSpace(lines[pos]) == "" {
						pos++
					}
					break
				}
			}
		}
	}
	if pos >= 10 {
		return content, fmt.Errorf("種別行が %d 行目になる（先頭 10 行以内に必要）", pos+1)
	}
	field := "種別: " + v
	if len(fields) == 1 {
		lines[pos] = field
	} else {
		if pos > len(lines) {
			pos = len(lines)
		}
		lines = append(lines, "")
		copy(lines[pos+1:], lines[pos:])
		lines[pos] = field
	}
	newline := "\n"
	if bytes.Contains(content, []byte("\r\n")) {
		newline = "\r\n"
	} else if bytes.Contains(content, []byte("\r")) {
		newline = "\r"
	}
	bom := ""
	if bytes.HasPrefix(content, []byte{0xef, 0xbb, 0xbf}) {
		bom = "\ufeff"
	}
	return []byte(bom + strings.Join(lines, newline)), nil
}

func Matches(filter, value string) bool {
	return filter == "" || filter == value || (filter == None && value == "")
}
