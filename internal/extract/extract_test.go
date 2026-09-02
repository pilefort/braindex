package extract

import (
	"strings"
	"testing"
	"time"
)

var fixedMtime = time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)

func TestExtract_Title(t *testing.T) {
	cases := []struct {
		name, content, want string
	}{
		{"f.md", "# タイトルX\n\n本文", "タイトルX"},
		{"no-title.md", "本文のみ\nもう一行", "no-title"},       // H1 無し → ファイル名
		{"h2first.md", "## これは H2\n# 本当のH1\n", "本当のH1"}, // H2 はタイトルにしない
	}
	for _, c := range cases {
		got := Extract(c.name, []byte(c.content), fixedMtime, "").Title
		if got != c.want {
			t.Errorf("Title(%q): want=%q got=%q", c.name, c.want, got)
		}
	}
}

func TestExtract_TitleBOM(t *testing.T) {
	// 先頭 BOM(EF BB BF)付きファイル。ソースに BOM リテラルを置かずバイトで組み立てる。
	content := append([]byte{0xEF, 0xBB, 0xBF}, []byte("# BOM付きタイトル\n\n結論: x")...)
	got := Extract("bom.md", content, fixedMtime, "").Title
	if got != "BOM付きタイトル" {
		t.Errorf("BOM 付きタイトル: want=%q got=%q", "BOM付きタイトル", got)
	}
}

func TestExtract_Date(t *testing.T) {
	cases := []struct {
		desc, name, content, want string
	}{
		{"ファイル名8桁", "20260721_x.md", "# X", "2026-07-21"},
		{"ファイル名ISO", "note_2026-07-18.md", "# X", "2026-07-18"},
		{"本文ISO(タイトル括弧)", "plain.md", "# X (2026-07-28)\n", "2026-07-28"},
		{"本文記録日", "plain.md", "# X\n\n記録日: 2026-07-28\n", "2026-07-28"},
		{"本文和暦パディング", "plain.md", "# X\n\n2026年7月8日 に調査\n", "2026-07-08"},
		{"8桁が不正で本文へ", "20261399_x.md", "# X\n\n記録日: 2026-05-05\n", "2026-05-05"},
		{"mtimeフォールバック", "plain.md", "# X\n\n本文に日付なし\n", "~2026-08-07"},
	}
	for _, c := range cases {
		got := Extract(c.name, []byte(c.content), fixedMtime, "").Date
		if got != c.want {
			t.Errorf("Date[%s]: want=%q got=%q", c.desc, c.want, got)
		}
	}
}

func TestExtract_Summary(t *testing.T) {
	long := strings.Repeat("あ", 100)
	wantLong := strings.Repeat("あ", 80) + "…"

	cases := []struct {
		desc, content, kind, want string
	}{
		{"結論行", "# T\n\n結論: これは要旨。\n本文続き", "", "結論: これは要旨。"},
		{"表をスキップ", "# T\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n本当の要旨行\n", "", "本当の要旨行"},
		{"コードフェンスをスキップ", "# T\n\n```\ncode line\n```\n要旨はここ\n", "", "要旨はここ"},
		{"80rune切り", "# T\n\n" + long + "\n", "", wantLong},
		{"見出しと表だけ→H2連結", "# T\n\n## 見出しA\n| x |\n|---|\n\n## 見出しB\n", "", "見出しA / 見出しB"},
		{"decisions末尾3件", "# 設計判断\n\n## D1\n## D2\n## D3\n## D4\n", "decisions", "D2 / D3 / D4"},
		{"decisions3件未満", "# 決定\n\n## A\n## B\n", "decisions", "A / B"},
	}
	for _, c := range cases {
		got := Extract("f.md", []byte(c.content), fixedMtime, c.kind).Summary
		if got != c.want {
			t.Errorf("Summary[%s]: want=%q got=%q", c.desc, c.want, got)
		}
	}
}
