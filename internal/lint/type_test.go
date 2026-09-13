package lint

import (
	"strings"
	"testing"
)

func TestNoteType(t *testing.T) {
	for _, tt := range []struct {
		text        string
		value, late int
	}{{"種別: failure", 1, 0}, {strings.Repeat("\n", 10) + "種別: 観測", 0, 1}, {"種別: 手順", 0, 0}, {"本文", 0, 0}, {"~~~\n種別: failure\n~~~", 0, 0}} {
		v, l := 0, 0
		for _, w := range CheckNote("note-2026-09-13.md", []byte(tt.text), NoteOptions{}) {
			if w.Kind == KindTypeValue {
				v++
				if w.Severity != SeverityWarn {
					t.Fatal(w)
				}
			}
			if w.Kind == KindTypeLate {
				l++
				if w.Line != 11 || w.Severity != SeverityWarn {
					t.Fatal(w)
				}
			}
		}
		if v != tt.value || l != tt.late {
			t.Fatalf("%q: value=%d late=%d", tt.text, v, l)
		}
	}
}
