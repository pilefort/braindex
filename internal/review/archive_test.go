package review

import (
	"strings"
	"testing"
)

// 合成 testdata の索引(12 件)から、cutoff 2026-07-13 より前・decisions でない・touched に無いものを選ぶ。
func TestArchiveCandidates(t *testing.T) {
	entries, err := ParseCatalog(afterCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	touched := map[string]bool{"repo-both/docs/notes/common/a.md": true} // 07-10 だが今回触られた
	got := ArchiveCandidates(entries, "2026-07-13", touched)
	want := []struct{ repo, path string }{
		{"repo-both", "repo-both/docs/notes/misc/20260705_table.md"}, // decisions.md(07-01)は種別で除外
		{"repo-common", "repo-common/docs/notes/common/e.md"},
		{"repo-flat", "repo-flat/docs/notes/c.md"},
		{"repo-proj", "repo-proj/docs/notes/project/d.md"},
	}
	var flat []struct{ repo, path string }
	for _, r := range got {
		for _, e := range r.Entries {
			flat = append(flat, struct{ repo, path string }{r.Repo, e.Path})
		}
	}
	if len(flat) != len(want) {
		t.Fatalf("候補: want %v got %v", want, flat)
	}
	for i := range want {
		if flat[i] != want[i] {
			t.Errorf("[%d]: want %v got %v", i, want[i], flat[i])
		}
	}
	// 閾値を古くすれば 0 件
	if got := ArchiveCandidates(entries, "2026-01-01", nil); len(got) != 0 {
		t.Errorf("候補が残る: %+v", got)
	}
	// 閾値ちょうどの日は含まない(「cutoff より前」)。07-06 を閾値にすると 07-06 の e.md は外れ、07-05 だけ残る
	got = ArchiveCandidates(entries, "2026-07-06", nil)
	if len(got) != 1 || len(got[0].Entries) != 1 || got[0].Entries[0].Path != "repo-both/docs/notes/misc/20260705_table.md" {
		t.Errorf("閾値ちょうど: %+v", got)
	}
	// 初回(前回の索引が無い)は全件が「追加」= touched なので、閾値を未来にしても候補は出ない(決定 2026-09-02)
	d, err := DiffIndex(nil, afterCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := ArchiveCandidates(entries, "2026-12-31", d.Touched()); len(got) != 0 {
		t.Errorf("初回なのに候補: %+v", got)
	}
}

// 索引差分の Touched は追加・変更のパスだけ(削除は今回の索引に無いので候補にもならない)。
func TestIndexDiff_Touched(t *testing.T) {
	d, err := DiffIndex(readTestdata(t, "before.md"), afterCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	touched := d.Touched()
	for _, p := range []string{"repo-flat/docs/notes/c.md", "ext/20260721_root-a.md", "repo-proj/docs/notes/project/d.md"} {
		if !touched[p] {
			t.Errorf("%s が touched に無い", p)
		}
	}
	if touched["repo-both/docs/notes/project/gone.md"] || len(touched) != 5 {
		t.Errorf("touched: %v", touched)
	}
}

func TestWriteArchiveSection(t *testing.T) {
	entries, err := ParseCatalog(afterCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	repos := ArchiveCandidates(entries, "2026-07-07", nil)
	var b strings.Builder
	WriteArchiveSection(&b, repos, 6, "2026-07-07")
	want := "## アーカイブ候補（機械条件のみ）\n\n索引の日付が 6 か月より前（2026-07-07 より前）で、今回の差分に無いノート。結論が新しいノートで上書きされたかは実物を読んで判断する。移動・削除はしない。\n\n### repo-both\n- repo-both/docs/notes/misc/20260705_table.md（2026-07-05・表だけノートF）\n\n### repo-common\n- repo-common/docs/notes/common/e.md（2026-07-06・共通のみE）\n"
	if b.String() != want {
		t.Errorf("節が不一致:\n--- got ---\n%s\n--- want ---\n%s", b.String(), want)
	}
	b.Reset()
	WriteArchiveSection(&b, nil, 6, "2026-03-02")
	if !strings.HasSuffix(b.String(), "\n- なし\n") {
		t.Errorf("なし:\n%s", b.String())
	}
}

func TestDateHelpers(t *testing.T) {
	if d, err := DaysBefore("2026-09-02", 28); err != nil || d != "2026-08-05" {
		t.Errorf("DaysBefore: %s %v", d, err)
	}
	if d, err := MonthsBefore("2026-09-02", 6); err != nil || d != "2026-03-02" {
		t.Errorf("MonthsBefore: %s %v", d, err)
	}
	if d, err := MonthsBefore("2026-08-31", 6); err != nil || d != "2026-03-03" {
		t.Errorf("MonthsBefore 月末: %s %v(time.AddDate の繰り上がり)", d, err)
	}
	if _, err := DaysBefore("2026/09/02", 1); err == nil || !strings.Contains(err.Error(), "YYYY-MM-DD") {
		t.Errorf("不正な日付: %v", err)
	}
}
