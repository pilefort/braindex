package extract

import "testing"

func TestSummarySkipsType(t *testing.T) {
	for _, prefix := range []string{"# 題\n\n", "# 題\n記録日: 2026-09-13\n"} {
		if got := Extract("note.md", []byte(prefix+"種別: 失敗\n本文の結論。\n"), "notes").Summary; got != "本文の結論。" {
			t.Fatalf("要旨: %q", got)
		}
	}
}
