package scan

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/scan/scantest"
)

// repo_depth 2 の設定(testdata/root-depth2)。extra の repo も group/name で書く。
func depth2Config() Config {
	return Config{
		Root:      "testdata/root-depth2",
		RepoDepth: 2,
		Extra:     []ExtraRule{{Repo: "group-b/repo-z", Path: "research", Recursive: true, Kind: "research"}},
	}
}

// repo_depth 2 では root/<group>/<name> がリポで、リポ名(H2 見出し)は "group/name"。
// 1 段目の配置(stray/docs/notes)は見ない(stray/docs がリポと見なされ、その下に docs/notes が無い)。
// group 直下のファイル(group-a/README.md)・docs を持たないリポ・archive は従来どおり載らない。
func TestScan_RepoDepth2(t *testing.T) {
	res, err := Scan(depth2Config())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(res.Warnings) != 0 || len(res.Gaps) != 0 {
		t.Errorf("警告も読めない範囲も無いはず: warnings=%q gaps=%+v", res.Warnings, res.Gaps)
	}
	got := map[string]string{}
	for _, f := range res.Files {
		if _, dup := got[f.Rel]; dup {
			t.Errorf("重複エントリ: %s", f.Rel)
		}
		got[f.Rel] = f.Kind
		// Repo は Rel の先頭 2 セグメント
		parts := strings.SplitN(f.Rel, "/", 3)
		if len(parts) < 3 || f.Repo != parts[0]+"/"+parts[1] {
			t.Errorf("Repo 不一致: rel=%s repo=%s", f.Rel, f.Repo)
		}
	}
	want := map[string]string{
		"group-a/repo-x/docs/decisions.md":                "decisions",
		"group-a/repo-x/docs/notes/20260801_x-note.md":    "notes",
		"group-a/repo-x/docs/notes/project/x-proj.md":     "notes/project",
		"group-a/repo-y/docs/notes/y-note.md":             "notes",
		"group-b/repo-z/docs/notes/common/z-common.md":    "notes/common",
		"group-b/repo-z/research/top.md":                  "research",
		"group-b/repo-z/research/topic/20260806_paper.md": "research/topic",
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

// 既定(repo_depth 未指定)は 1 段。明示の 1 と結果が完全に一致し、Depth() は 1 を返す。
// 2 段の木を既定のまま走査すると、直下の配置(stray)だけが載る——これが今までの挙動。
func TestScan_DefaultDepthIsOne(t *testing.T) {
	if (Config{}).Depth() != 1 || (Config{RepoDepth: 1}).Depth() != 1 || (Config{RepoDepth: 2}).Depth() != 2 {
		t.Errorf("Depth: 0 → 1、1 → 1、2 → 2 のはず")
	}
	implicit, err := Scan(testConfig())
	if err != nil {
		t.Fatalf("Scan(既定): %v", err)
	}
	explicit := testConfig()
	explicit.RepoDepth = 1
	one, err := Scan(explicit)
	if err != nil {
		t.Fatalf("Scan(repo_depth 1): %v", err)
	}
	if !reflect.DeepEqual(implicit, one) {
		t.Errorf("repo_depth 未指定と 1 で結果が違う:\n未指定=%+v\n1=%+v", implicit, one)
	}

	files, err := scanFiles(Config{Root: "testdata/root-depth2"})
	if err != nil {
		t.Fatalf("Scan(2 段の木を 1 段で): %v", err)
	}
	if len(files) != 1 || files[0].Rel != "stray/docs/notes/stray.md" || files[0].Repo != "stray" || files[0].Kind != "notes" {
		t.Errorf("1 段の走査は直下の配置だけを拾うはず: %+v", files)
	}
}

// 負の repo_depth は設定の誤り(0 は省略と同じで 1)。
func TestScan_RepoDepthNegative(t *testing.T) {
	_, err := Scan(Config{Root: "testdata/root", RepoDepth: -1})
	if err == nil || !strings.Contains(err.Error(), "repo_depth") {
		t.Errorf("負の repo_depth がエラーにならない: %v", err)
	}
	if _, err := Scan(Config{Root: "testdata/root", RepoDepth: 0}); err != nil {
		t.Errorf("0 は省略と同じで通るはず: %v", err)
	}
}

// ListRepos は depth 段下のディレクトリをリポとして名前順に返す。depth 1 未満は 1。
// ディレクトリでないもの(group-a/README.md)は数えない。Dir は root と同じ基準のパス。
func TestListRepos(t *testing.T) {
	root := "testdata/root-depth2"
	names := func(rs []Repo) string {
		var ns []string
		for _, r := range rs {
			ns = append(ns, r.Name)
		}
		return strings.Join(ns, ",")
	}
	for _, c := range []struct {
		depth int
		want  string
	}{
		{1, "group-a,group-b,stray"},
		{0, "group-a,group-b,stray"}, // 1 未満は 1
		{2, "group-a/repo-x,group-a/repo-y,group-b/repo-nodocs,group-b/repo-z,stray/docs"},
		{3, "group-a/repo-x/docs,group-a/repo-y/docs,group-b/repo-z/docs,group-b/repo-z/research,stray/docs/notes"},
	} {
		repos, gaps, err := ListRepos(root, c.depth)
		if err != nil {
			t.Fatalf("ListRepos(depth %d): %v", c.depth, err)
		}
		if got := names(repos); got != c.want {
			t.Errorf("depth %d: want=%s got=%s", c.depth, c.want, got)
		}
		if len(gaps) != 0 {
			t.Errorf("depth %d: 読めない範囲は無いはず: %+v", c.depth, gaps)
		}
	}
	repos, _, _ := ListRepos(root, 2)
	if want := filepath.Join(root, "group-a", "repo-x"); repos[0].Dir != want {
		t.Errorf("Dir: want=%s got=%s", want, repos[0].Dir)
	}
	if _, _, err := ListRepos(filepath.Join(root, "no-such"), 1); err == nil {
		t.Errorf("root 不在でエラーになっていない")
	}
}

// "." で始まるディレクトリはどの段でも見ない。列挙できない group は Gap(ディレクトリ)にして続ける。
// Scan からも同じ Gap が Result.Gaps と Warnings に出る。
func TestListRepos_HiddenAndUnreadable(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{".hidden/r0", "g/.hidden", "g/r1", "locked/r2"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeNote(t, filepath.Join(root, "g", "r1", "docs", "notes", "n.md"))
	if err := os.WriteFile(filepath.Join(root, "g", "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	scantest.MakeUnreadable(t, filepath.Join(root, "locked"))

	repos, gaps, err := ListRepos(root, 2)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 1 || repos[0].Name != "g/r1" {
		t.Errorf("g/r1 だけのはず: %+v", repos)
	}
	if len(gaps) != 1 || gaps[0].Rel != "locked" || !gaps[0].Dir || gaps[0].Reason == "" {
		t.Errorf("Gaps に {locked, Dir, 理由} の 1 件を期待: %+v", gaps)
	}

	res, err := Scan(Config{Root: root, RepoDepth: 2})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(res.Files) != 1 || res.Files[0].Rel != "g/r1/docs/notes/n.md" || res.Files[0].Repo != "g/r1" {
		t.Errorf("読めた方だけ載るべき: %+v", res.Files)
	}
	if len(res.Gaps) != 1 || res.Gaps[0].Rel != "locked" || !res.Gaps[0].Dir {
		t.Errorf("Scan の Gaps に locked を期待: %+v", res.Gaps)
	}
	if len(res.Warnings) != 1 || !strings.HasPrefix(res.Warnings[0], "locked: ") {
		t.Errorf("警告 1 件「locked: <理由>」を期待: %q", res.Warnings)
	}
}

// SplitRepo は段数に従ってリポ名とリポ内のパスに分ける。
func TestSplitRepo(t *testing.T) {
	cases := []struct {
		depth        int
		rel          string
		repo, inRepo string
		ok           bool
	}{
		{1, "a/docs/x.md", "a", "docs/x.md", true},
		{1, "/a/x.md/", "a", "x.md", true}, // 前後の "/" は無視
		{1, "a", "", "", false},
		{1, ".h/x.md", "", "", false},
		{2, "g/a/docs/notes/x.md", "g/a", "docs/notes/x.md", true},
		{2, "a/b/c", "a/b", "c", true},
		{2, "g/a", "", "", false},
		{2, "g/.a/x.md", "", "", false},
		{2, "g//x.md", "", "", false},
		{2, "g/../x.md", "", "", false},
	}
	for _, c := range cases {
		repo, inRepo, ok := SplitRepo(Config{RepoDepth: c.depth}, c.rel)
		if repo != c.repo || inRepo != c.inRepo || ok != c.ok {
			t.Errorf("SplitRepo(depth %d, %q) = (%q, %q, %v) want (%q, %q, %v)", c.depth, c.rel, repo, inRepo, ok, c.repo, c.inRepo, c.ok)
		}
	}
}

// Covers も repo_depth に従う。1 段目の配置は depth 2 では対象外で、extra は group/name のリポにだけ掛かる。
func TestCovers_RepoDepth2(t *testing.T) {
	cfg := Config{RepoDepth: 2, Extra: []ExtraRule{{Repo: "g/ext", Path: "research", Recursive: true, Kind: "research"}}}
	cases := []struct {
		rel  string
		want bool
		why  string
	}{
		{"g/a/docs/notes/x.md", true, "2 段目のリポのノート置き場"},
		{"g/a/docs/notes/sub/x.md", true, "サブディレクトリも"},
		{"g/a/docs/decisions.md", true, "決定記録"},
		{"a/docs/notes/x.md", false, "1 段目の配置(a/docs がリポ扱いで、notes/x.md は置き場の外)"},
		{"g/a/x.md", false, "リポ直下"},
		{"g/ext/research/topic/x.md", true, "group/name の extra"},
		{"h/ext/research/x.md", false, "別の group の同名リポには掛からない"},
		{".g/a/docs/notes/x.md", false, "ドットで始まる group"},
		{"g/.a/docs/notes/x.md", false, "ドットで始まるリポ"},
		{"g/a/docs/notes/archive/x.md", false, "archive セグメント"},
		{"g/a", false, "リポだけ"},
	}
	for _, c := range cases {
		if got := Covers(cfg, c.rel); got != c.want {
			t.Errorf("[%s] Covers(%s)=%v want %v", c.why, c.rel, got, c.want)
		}
	}
}

// extra.repo は repo_depth に合わせた段数のリポ名(スラッシュ区切り)に限る。
func TestScan_ExtraRepoMatchesDepth(t *testing.T) {
	root := "testdata/root-depth2"
	want := `extra: repo は root から 2 段のディレクトリ名をスラッシュで区切って書く(repo_depth が 2): `
	for _, bad := range []string{"repo-z", "group-b/repo-z/research", "group-b//repo-z", "group-b/..", "./repo-z", "", `group-b\repo-z`} {
		_, err := Scan(Config{Root: root, RepoDepth: 2, Extra: []ExtraRule{{Repo: bad, Path: "research", Kind: "x"}}})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("extra.repo=%q: エラー文が原因を名指ししていない: %v", bad, err)
		}
	}
	if _, err := Scan(Config{Root: root, RepoDepth: 2, Extra: []ExtraRule{{Repo: "group-b/repo-z", Path: "research", Kind: "x"}}}); err != nil {
		t.Errorf("group/name の extra.repo が拒否された: %v", err)
	}
	// depth 1 の文言は従来どおり(バックスラッシュも区切りとして弾く)
	_, err := Scan(Config{Root: "testdata/root", Extra: []ExtraRule{{Repo: `ext\docs`, Path: ".", Kind: "x"}}})
	if err == nil || !strings.Contains(err.Error(), "extra: repo は root 直下のディレクトリ名だけを書く") {
		t.Errorf("depth 1 のバックスラッシュ: %v", err)
	}
}
