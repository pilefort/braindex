package interest

import "testing"

// isKatakana の `|| r == 'ー'` は冗長(#42 と #73 で二度指摘された)。
// 長音 U+30FC は前段の範囲判定(r >= 0x30A0 && r <= 0x30FF)に含まれるので、
// 消しても isKatakana('ー') の結果は変わらない。消す前にそれを確かめる。
func TestIsKatakana_長音は範囲判定だけでtrueになる(t *testing.T) {
	r := rune('ー') // U+30FC
	if !(r >= 0x30A0 && r <= 0x30FF) {
		t.Fatalf("長音 U+%04X が前段の範囲(0x30A0〜0x30FF)に入っていない前提が崩れている", r)
	}
	if !isKatakana(r) {
		t.Errorf("isKatakana('ー') = false, want true")
	}
}

// 長音を含むカタカナ語の抽出そのものが変わらないことも、Words 経由で確かめる。
func TestWords_長音入りのカタカナ語(t *testing.T) {
	got := Words("データベース")
	found := false
	for _, w := range got {
		if w == "データベース" {
			found = true
		}
	}
	if !found {
		t.Errorf("〔データベース〕が無い: %v", got)
	}
}
