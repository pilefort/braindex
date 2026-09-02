package scan

import (
	"sort"
	"strings"
	"testing"
)

func testConfig() Config {
	return Config{
		Root: "testdata/root",
		Extra: []ExtraRule{
			{Repo: "ext", Path: ".", Recursive: false, Kind: "root",
				Exclude: []string{"README.md", "CLAUDE.md"}},
			{Repo: "ext", Path: "research", Recursive: true, Kind: "research"},
			{Repo: "ext", Path: "projects", Recursive: true, Kind: "projects"},
			{Repo: "ext", Path: "docs/guides", Recursive: false, Kind: "guides"},
		},
	}
}

func TestScan_FoundSet(t *testing.T) {
	files, err := Scan(testConfig())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	got := map[string]string{} // rel -> kind
	for _, f := range files {
		if _, dup := got[f.Rel]; dup {
			t.Errorf("重複エントリ: %s", f.Rel)
		}
		got[f.Rel] = f.Kind
		// Repo は Rel の先頭セグメントと一致するはず
		if seg := strings.SplitN(f.Rel, "/", 2)[0]; seg != f.Repo {
			t.Errorf("Repo 不一致: rel=%s repo=%s", f.Rel, f.Repo)
		}
	}

	want := map[string]string{
		"ext/docs/notes/project/extnote.md":           "notes/project", // auto は例外リポの notes も拾う
		"ext/20260721_root-a.md":                      "root",          // extra(非再帰)
		"ext/20260722_root-b.md":                      "root",
		"ext/research/topic-a/20260815_survey.md":     "research/topic-a", // extra(再帰): kind/<先頭サブディレクトリ>
		"ext/research/top.md":                         "research",         // extra(再帰): 起点直下は kind そのまま
		"ext/projects/alpha/plan.md":                  "projects/alpha",
		"ext/docs/guides/style.md":                    "guides", // extra(非再帰・サブパス起点)
		"repo-arch/docs/notes/project/keep.md":        "notes/project",
		"repo-both/docs/decisions.md":                 "decisions",
		"repo-both/docs/notes/common/a.md":            "notes/common",
		"repo-both/docs/notes/misc/20260705_table.md": "notes/misc", // 任意サブディレクトリ
		"repo-both/docs/notes/project/b.md":           "notes/project",
		"repo-common/docs/notes/common/e.md":          "notes/common",  // common のみ
		"repo-dec/docs/decisions.md":                  "decisions",     // decisions のみ
		"repo-flat/docs/notes/c.md":                   "notes",         // フラット(直下)
		"repo-proj/docs/notes/project/d.md":           "notes/project", // project のみ
	}

	for rel, kind := range want {
		if got[rel] != kind {
			t.Errorf("欠落 or 種別違い: %s want=%q got=%q", rel, kind, got[rel])
		}
	}
	if len(got) != len(want) {
		t.Errorf("件数不一致: want=%d got=%d\n got=%s", len(want), len(got), sortedKeys(got))
	}
}

func TestScan_Exclusions(t *testing.T) {
	files, err := Scan(testConfig())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, f := range files {
		switch {
		case strings.Contains(f.Rel, "/archive/"):
			t.Errorf("archive が除外されていない: %s", f.Rel)
		case strings.HasSuffix(f.Rel, "ext/README.md") || strings.HasSuffix(f.Rel, "ext/CLAUDE.md"):
			t.Errorf("exclude 指定が除外されていない: %s", f.Rel)
		case strings.HasPrefix(f.Rel, "repo-nodocs/"):
			t.Errorf("docs を持たないリポが拾われた: %s", f.Rel)
		}
	}
}

// notes_dirs を [wiki] にすると、docs/notes でなく wiki/ を走査し、種別ラベルは末尾セグメント(wiki)になる。
// docs/decisions.md は notes_dir と無関係に拾う。archive は従来どおり除外。
func TestScan_NotesDir(t *testing.T) {
	files, err := Scan(Config{Root: "testdata/root-wiki", NotesDirs: []string{"wiki"}})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.Rel] = f.Kind
	}
	want := map[string]string{
		"repo-w/wiki/top.md":       "wiki",
		"repo-w/wiki/sub/x.md":     "wiki/sub",
		"repo-w/docs/decisions.md": "decisions",
	}
	for rel, kind := range want {
		if got[rel] != kind {
			t.Errorf("欠落 or 種別違い: %s want=%q got=%q", rel, kind, got[rel])
		}
	}
	if _, bad := got["repo-w/docs/notes/ignored.md"]; bad {
		t.Errorf("notes_dirs=[wiki] なのに docs/notes が拾われた")
	}
	if _, bad := got["repo-w/wiki/archive/old.md"]; bad {
		t.Errorf("archive が除外されていない")
	}
	if len(got) != len(want) {
		t.Errorf("件数不一致: want=%d got=%d\n got=%s", len(want), len(got), sortedKeys(got))
	}
}

// notes_dirs に複数を並べると、それぞれを走査し、ラベルは各ディレクトリの末尾セグメントになる。
func TestScan_NotesDirs_Multiple(t *testing.T) {
	files, err := Scan(Config{Root: "testdata/root-wiki", NotesDirs: []string{"wiki", "docs/notes"}})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got := map[string]string{}
	for _, f := range files {
		if _, dup := got[f.Rel]; dup {
			t.Errorf("重複エントリ: %s", f.Rel)
		}
		got[f.Rel] = f.Kind
	}
	want := map[string]string{
		"repo-w/wiki/top.md":           "wiki",
		"repo-w/wiki/sub/x.md":         "wiki/sub",
		"repo-w/docs/notes/ignored.md": "notes",
		"repo-w/docs/decisions.md":     "decisions",
	}
	for rel, kind := range want {
		if got[rel] != kind {
			t.Errorf("欠落 or 種別違い: %s want=%q got=%q", rel, kind, got[rel])
		}
	}
	if len(got) != len(want) {
		t.Errorf("件数不一致: want=%d got=%d got=%v", len(want), len(got), sortedKeys(got))
	}
}

// 入れ子の指定(docs と docs/notes)でも同じファイルは 1 回だけ。先に書いた指定のラベルが勝つ
// (docs 起点なら docs/notes/ignored.md は "docs/notes"。後の docs/notes 起点なら "notes" になるはずのもの)。
// docs/decisions.md は notes_dirs に含まれていても種別 decisions のまま。
func TestScan_NotesDirs_Dedupe(t *testing.T) {
	files, err := Scan(Config{Root: "testdata/root-wiki", NotesDirs: []string{"docs", "docs/notes"}})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got := map[string]string{}
	count := 0
	for _, f := range files {
		count++
		got[f.Rel] = f.Kind
	}
	if count != 2 {
		t.Errorf("件数不一致: want=2 got=%d got=%v", count, sortedKeys(got))
	}
	if got["repo-w/docs/notes/ignored.md"] != "docs/notes" {
		t.Errorf("先勝ちのラベルでない: got=%q", got["repo-w/docs/notes/ignored.md"])
	}
	if got["repo-w/docs/decisions.md"] != "decisions" {
		t.Errorf("decisions.md の種別が decisions でない: got=%q", got["repo-w/docs/decisions.md"])
	}
}

func sortedKeys(m map[string]string) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, "\n ")
}
