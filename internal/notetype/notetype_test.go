package notetype

import (
	"bytes"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	for _, tt := range []struct {
		name, text, value string
		line              int
		err               string
	}{
		{"absent", "# 題\n本文\n", "", 0, ""},
		{"present", "\ufeff# 題\r\n 種別 ： 失敗 \r\n", Failure, 2, ""},
		{"invalid", "種別: 決定\n", "決定", 1, "1 行目"},
		{"alias", "種別: failure\n", "failure", 1, "failure"},
		{"duplicate", "種別: 手順\n種別: 観測\n", Howto, 1, "2 行ある"},
		{"fence", "```md\n種別: 失敗\n~~~\n種別: 手順\n```\n種別: 観測\n", Observation, 6, ""},
		{"tilde", "~~~~md\n種別: 失敗\n~~~\n種別: 手順\n~~~~\n", "", 0, ""},
		{"late", strings.Repeat("\n", 10) + "種別: 失敗", "", 0, ""},
		{"tenth", strings.Repeat("\n", 9) + "種別: 手順", Howto, 10, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v, l, err := Parse([]byte(tt.text))
			if v != tt.value || l != tt.line || (tt.err == "" && err != nil) || (tt.err != "" && (err == nil || !strings.Contains(err.Error(), tt.err))) {
				t.Fatalf("Parse = %q, %d, %v", v, l, err)
			}
		})
	}
}

func TestInsert(t *testing.T) {
	for _, tt := range []struct{ name, before, after string }{
		{"date", "# 題\n記録日: 2026-09-13\n本文\n", "# 題\n記録日: 2026-09-13\n種別: 失敗\n本文\n"},
		{"h1", "# 題\n本文", "# 題\n種別: 失敗\n本文"},
		{"blank", "# 題\n\n本文\n", "# 題\n\n種別: 失敗\n本文\n"},
		{"noH1", "本文\n", "種別: 失敗\n本文\n"},
		{"replace", "# 題\n種別：手順\n本文\n", "# 題\n種別: 失敗\n本文\n"},
		{"crlfBom", "\ufeff# 題\r\n記録日：2026-09-13\r\n本文\r\n", "\ufeff# 題\r\n記録日：2026-09-13\r\n種別: 失敗\r\n本文\r\n"},
		{"fencedDate", "# 題\n```\n記録日: 2026-09-13\n```\n本文", "# 題\n種別: 失敗\n```\n記録日: 2026-09-13\n```\n本文"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Insert([]byte(tt.before), "failure")
			if err != nil || string(got) != tt.after {
				t.Fatalf("Insert = %q, %v; want %q", got, err, tt.after)
			}
		})
	}
}

func TestInsertRejectsWithoutChangingContent(t *testing.T) {
	for _, before := range []string{strings.Repeat("\n", 9) + "記録日: 2026-09-13\n", strings.Repeat("\n", 9) + "# 題\n本文", strings.Repeat("\n", 10) + "種別: 観測", "種別: 観測\n種別: 手順"} {
		b := []byte(before)
		got, err := Insert(b, Failure)
		if err == nil || !bytes.Equal(got, b) || string(b) != before {
			t.Fatalf("Insert = %q, %v", got, err)
		}
	}
}

func TestSuggestAxes(t *testing.T) {
	for _, tt := range []struct {
		title, name, want string
		axes              int
	}{
		{"起動の落とし穴", "note.md", Failure, 1},
		{"調査結果と失敗の記録", "note.md", "", 2},
		{"雑記", "note.md", "", 0},
		{"使い方", "HOW TO.md", Howto, 1},
		{"記録", "RESEARCH.md", Observation, 1},
	} {
		s := Suggest("r/docs/notes/n.md", tt.title, tt.name)
		if s.Candidate != tt.want || len(s.Axes) != tt.axes {
			t.Fatalf("Suggest = %+v", s)
		}
	}
}

func TestFieldsIgnoresProse(t *testing.T) {
	// 実ノートで「種別: **字幕**＝主人公の発話／**紙**＝指示書」のように語を別の意味で使う行があった(2026-09-13)
	got := Fields([]byte("# t\n種別: **字幕**＝主人公の発話／**紙**＝指示書\n種別: 失敍\n"))
	if len(got) != 1 || got[0].Value != "失敍" || got[0].Line != 3 {
		t.Fatalf("文の行は無視し短い誤りは残す: %+v", got)
	}
}
