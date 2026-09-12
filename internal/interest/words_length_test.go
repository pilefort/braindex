package interest

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Words のラテン語判定は下限を文字数(utf8.RuneCountInString)、上限をバイト数(len)で
// 別々の基準を使っていた(words.go:53)。latinRe([A-Za-z][A-Za-z0-9_\-]*) は ASCII だけを
// 拾うため、マッチした文字列は常にバイト数 == 文字数になる。基準を文字数に揃えても
// 実際に拾う語が変わらないことを、まずこの不変条件で確かめる。
func TestWords_ラテン語のバイト数と文字数は一致する(t *testing.T) {
	samples := []string{
		"go", "ai", "hello-world_123", strings.Repeat("a", maxLatin), strings.Repeat("b", maxLatin+5),
		"a_b-c", "ABC123",
	}
	for _, m := range samples {
		if len(m) != utf8.RuneCountInString(m) {
			t.Errorf("%q: len=%d runes=%d(latinRe が ASCII 以外を拾った)", m, len(m), utf8.RuneCountInString(m))
		}
	}
}

// 上限を文字数に揃えても、maxLatin ちょうど・1 文字超えの境界の拾い方は変わらない
// (ASCII だけなので、バイト数の判定と文字数の判定は同じ語で同じ結果になる)。
func TestWords_上限を文字数に揃えても境界は変わらない(t *testing.T) {
	at := strings.Repeat("x", maxLatin)
	over := strings.Repeat("y", maxLatin+1)
	got := Words(at + " " + over)
	has := func(w string) bool {
		for _, g := range got {
			if g == w {
				return true
			}
		}
		return false
	}
	if !has(at) {
		t.Errorf("上限ちょうどの語が無い: %v", got)
	}
	if has(over) {
		t.Errorf("上限を超えた語が残っている: %v", got)
	}
}
