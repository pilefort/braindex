package interest

import (
	"reflect"
	"testing"
)

func TestRate(t *testing.T) {
	p := Profile{Terms: []Term{{Word: "ゴルーチン", Weight: 2.0}, {Word: "パース", Weight: 1.0}, {Word: "rust", Weight: 0.4}}}
	cases := []struct {
		text    string
		value   int
		matched []string
	}{
		{"関係ない見出し", 0, nil},
		{"Rust の話", 1, []string{"rust"}},                            // 0.4/2 = 0.2 → 1
		{"パース の話", 2, []string{"パース"}},                              // 1/2 = 0.5 → 2
		{"ゴルーチン 入門", 2, []string{"ゴルーチン"}},                          // 2/2 = 1 → 2(最強の語 1 つで主要)
		{"Rust で パース と ゴルーチン", 3, []string{"ゴルーチン", "パース", "rust"}}, // 3.4/2 = 1.7 → 3。当たった語は重み降順
		{"ゴルーチン ゴルーチン ゴルーチン", 2, []string{"ゴルーチン"}},                 // 同じ語は 1 回
		{"rust-lang は別の語", 0, nil},                                  // ハイフンつなぎは 1 語(rust-lang)なので rust には当たらない
	}
	for _, c := range cases {
		got := Rate(p, c.text)
		if got.Value != c.value || !reflect.DeepEqual(got.Matched, c.matched) {
			t.Errorf("Rate(%q) = %+v, want %d %v", c.text, got, c.value, c.matched)
		}
	}
	if got := Rate(Profile{}, "ゴルーチン"); got.Value != 0 || got.Matched != nil {
		t.Errorf("空のプロファイル: %+v", got)
	}
}
