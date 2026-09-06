package extract

import (
	"strings"
	"testing"
)

func TestExtract_Title(t *testing.T) {
	cases := []struct {
		name, content, want string
	}{
		{"f.md", "# タイトルX\n\n本文", "タイトルX"},
		{"no-title.md", "本文のみ\nもう一行", "no-title"},       // H1 無し → ファイル名
		{"h2first.md", "## これは H2\n# 本当のH1\n", "本当のH1"}, // H2 はタイトルにしない
	}
	for _, c := range cases {
		got := Extract(c.name, []byte(c.content), "").Title
		if got != c.want {
			t.Errorf("Title(%q): want=%q got=%q", c.name, c.want, got)
		}
	}
}

func TestExtract_TitleBOM(t *testing.T) {
	// 先頭 BOM(EF BB BF)付きファイル。ソースに BOM リテラルを置かずバイトで組み立てる。
	content := append([]byte{0xEF, 0xBB, 0xBF}, []byte("# BOM付きタイトル\n\n結論: x")...)
	got := Extract("bom.md", content, "").Title
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
		{"日付なし(mtime には頼らない)", "plain.md", "# X\n\n本文に日付なし\n", ""},
		// 月日が範囲外の候補は日付とみなさない(8 桁形式と同じ扱い)。ISO・和暦とも飛ばして次を探す。
		{"ファイル名ISOの月が不正で本文へ", "2026-13-45_x.md", "# X\n\n記録日: 2026-05-05\n", "2026-05-05"},
		{"本文ISOの月日が不正は飛ばす", "plain.md", "# X\n\n2026-13-45 は誤記\n記録日: 2026-05-06\n", "2026-05-06"},
		{"同じ行の不正の後ろにある正しい日付", "plain.md", "# X\n\n2026-00-10 → 2026-05-07\n", "2026-05-07"},
		{"本文和暦の月日が不正は飛ばす", "plain.md", "# X\n\n2026年13月45日\n", ""},
	}
	for _, c := range cases {
		got := Extract(c.name, []byte(c.content), "").Date
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
		{"日付行は要旨にしない", "# T\n\n記録日: 2026-09-06\n結論: ここが要旨。\n", "", "結論: ここが要旨。"},
		{"日付行しか無ければ空", "# T\n\n更新日: 2026-09-06\n", "", ""},
		{"decisionsは末尾の1件", "# 設計判断\n\n## D1\n## D2\n## D3\n## D4\n", "decisions", "D4"},
		{"decisions1件", "# 決定\n\n## A\n", "decisions", "A"},
		{"decisionsは決定が無ければ空", "# 決定\n\n本文だけ\n", "decisions", ""},
	}
	for _, c := range cases {
		got := Extract("f.md", []byte(c.content), c.kind).Summary
		if got != c.want {
			t.Errorf("Summary[%s]: want=%q got=%q", c.desc, c.want, got)
		}
	}
}

// decisions.md は追記式なので、他のノートと索引の作り方を変える(設計レビュー 2026-09-06 M4)。
// 日付は「一番新しい記録日」(最後に何か決めた日)・タイトルに件数・要旨は末尾の 1 件。
func TestExtract_Decisions(t *testing.T) {
	content := "# 設計判断\n\n## 古い決定\n記録日: 2026-07-01\n理由: …\n\n" +
		"## 新しい決定\n記録日: 2026-09-06\n理由: …\n\n" +
		"## 後から書き足した古い決定\n記録日: 2026-08-01\n理由: …\n"
	m := Extract("decisions.md", []byte(content), "decisions")
	if m.Title != "設計判断（3 件）" {
		t.Errorf("Title: %q", m.Title)
	}
	// 末尾の記録日(2026-08-01)ではなく最大(2026-09-06)。先頭 10 行にも無い
	if m.Date != "2026-09-06" {
		t.Errorf("Date: %q", m.Date)
	}
	if m.Summary != "後から書き足した古い決定" {
		t.Errorf("Summary: %q", m.Summary)
	}

	// 記録日が無ければ従来の日付規則(ファイル名 → 先頭 10 行)に落ちる
	m = Extract("20260704_decisions.md", []byte("# 決定\n\n## A\n## B\n"), "decisions")
	if m.Date != "2026-07-04" || m.Title != "決定（2 件）" || m.Summary != "B" {
		t.Errorf("記録日なし: %+v", m)
	}

	// コードフェンスの中の ## は決定として数えない
	m = Extract("decisions.md", []byte("# 決定\n\n## A\n\n```\n## これは見出しではない\n```\n"), "decisions")
	if m.Title != "決定（1 件）" || m.Summary != "A" {
		t.Errorf("フェンス内: %+v", m)
	}
}
