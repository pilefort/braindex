package interest

import (
	"strings"
	"testing"
)

// 中黒（・）は語の区切りにする（決定 2026-09-03）。
//
// 中黒は U+30FB でカタカナのコードブロック（U+30A0〜U+30FF）に入るため、
// 直すまでは「ファイル・フォルダ」が 1 語として残り、「ファイル」「フォルダ」が
// どちらもストップワードなのに語彙に入っていた。
func TestWords_中黒は区切り(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		want   []string // 出てほしい語
		unwant []string // 出てはいけない語
	}{
		{"両方ストップワードなら何も残らない", "ファイル・フォルダ", nil, []string{"ファイル・フォルダ"}},
		{"中黒で分けた語がそれぞれ立つ", "ドキュメント・アーカイブ", []string{"ドキュメント", "アーカイブ"}, []string{"ドキュメント・アーカイブ"}},
		{"3 つ並んでも分かれる", "アルファ・ベータ・ガンマ", []string{"アルファ", "ベータ", "ガンマ"}, []string{"アルファ・ベータ", "ベータ・ガンマ"}},
		// 語の中の長音は保たれる。末尾の長音と中黒は従来どおり Trim で落ちる
		// （"サーバー" は元から "サーバ" になる。中黒の変更とは無関係の既存仕様）
		{"語の中の長音は保たれる", "データベース", []string{"データベース"}, nil},
		{"端の中黒は従来どおり落ちる", "・アーカイブ・", []string{"アーカイブ"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Words(c.text)
			has := func(w string) bool {
				for _, g := range got {
					if g == w {
						return true
					}
				}
				return false
			}
			for _, w := range c.want {
				if !has(w) {
					t.Errorf("〔%s〕が無い: %v", w, got)
				}
			}
			for _, w := range c.unwant {
				if has(w) {
					t.Errorf("〔%s〕が残っている: %v", w, got)
				}
			}
			for _, g := range got {
				if strings.Contains(g, "・") {
					t.Errorf("中黒を含む語が出た: %q（全体 %v）", g, got)
				}
			}
		})
	}
}
