package scan

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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
	files, _, err := Scan(testConfig())
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
	files, _, err := Scan(testConfig())
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
// docs/decisions.md は notes_dirs と無関係に拾う。archive は従来どおり除外。
func TestScan_NotesDir(t *testing.T) {
	files, _, err := Scan(Config{Root: "testdata/root-wiki", NotesDirs: []string{"wiki"}})
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
	files, _, err := Scan(Config{Root: "testdata/root-wiki", NotesDirs: []string{"wiki", "docs/notes"}})
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
	files, _, err := Scan(Config{Root: "testdata/root-wiki", NotesDirs: []string{"docs", "docs/notes"}})
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

// 存在しない extra の起点は警告(エラーにも無言スキップにもしない)。root が空・存在しなければエラー。
func TestScan_WarningsAndErrors(t *testing.T) {
	cfg := Config{Root: "testdata/root", Extra: []ExtraRule{{Repo: "ext", Path: "no-such-dir", Kind: "x"}}}
	files, warns, err := Scan(cfg)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(files) == 0 {
		t.Errorf("警告があっても自動規則の結果は返すべき")
	}
	if len(warns) != 1 || warns[0] != "extra ext/no-such-dir: 存在しない" {
		t.Errorf("警告 1 件「extra ext/no-such-dir: 存在しない」(パスを繰り返さない・OS の文言を出さない)を期待: %q", warns)
	}

	if _, _, err := Scan(Config{Root: ""}); err == nil || strings.Contains(err.Error(), "-root") {
		t.Errorf("root 空はエラーで、ライブラリの文に CLI のフラグ名を含めない: %v", err)
	}
	if _, _, err := Scan(Config{Root: "testdata/no-such-root"}); err == nil {
		t.Errorf("root 不在でエラーになっていない")
	}
}

// archive の判定はルート相対パスに掛ける。root 自体が archive という名前のディレクトリの下にあっても、
// その中のノートは索引に載る(原型は絶対パス全体で判定していたので全件除外されていた)。
func TestScan_ArchiveJudgedOnRootRelativePath(t *testing.T) {
	files, _, err := Scan(Config{Root: "testdata/archive/root"})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.Rel] = f.Kind
	}
	want := map[string]string{
		"repo-x/docs/notes/keep.md": "notes",
		"repo-x/docs/decisions.md":  "decisions",
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

// extra の exclude はグロブ。"/" を含まないパターンはファイル名に、含むパターンは起点からの相対パスに掛ける。
func TestScan_ExtraExcludeGlob(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "r", "x")
	for _, rel := range []string{"a.md", "b.draft.md", "sub/c.md", "sub/d.draft.md"} {
		p := filepath.Join(base, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# "+rel+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		desc    string
		exclude []string
		want    []string
	}{
		{"ファイル名グロブ", []string{"*.draft.md"}, []string{"r/x/a.md", "r/x/sub/c.md"}},
		{"相対パスグロブ", []string{"sub/*"}, []string{"r/x/a.md", "r/x/b.draft.md"}},
		{"完全一致(従来どおり)", []string{"a.md"}, []string{"r/x/b.draft.md", "r/x/sub/c.md", "r/x/sub/d.draft.md"}},
	}
	for _, c := range cases {
		files, _, err := Scan(Config{Root: root, Extra: []ExtraRule{{Repo: "r", Path: "x", Recursive: true, Kind: "x", Exclude: c.exclude}}})
		if err != nil {
			t.Fatalf("[%s] Scan: %v", c.desc, err)
		}
		var got []string
		for _, f := range files {
			got = append(got, f.Rel)
		}
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("[%s] want=%v got=%v", c.desc, c.want, got)
		}
	}

	// 不正なパターンは設定の誤りなのでエラー(無言で文字列比較に落とさない)。
	// 文言には「どの extra の」「どのパターンが」を出す(設定を直す手掛かりになる)
	_, _, err := Scan(Config{Root: root, Extra: []ExtraRule{{Repo: "r", Path: "x", Kind: "x", Exclude: []string{"["}}}})
	if err == nil {
		t.Fatalf("不正なグロブでエラーになっていない")
	}
	for _, want := range []string{"r/x", `"["`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("エラー文に %q が無い: %v", want, err)
		}
	}
}

// extra の起点が archive セグメントの下にあると、archive の除外規則で全件が落ちる。
// 設定の誤りなので無言で 0 件にせず警告する(起点が無い・読めない場合と同じ扱い)。
func TestScan_ExtraUnderArchiveWarns(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"r/archive/old/a.md", "archive/notes/b.md"} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# "+rel+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		desc string
		rule ExtraRule
	}{
		{"起点のパスに archive", ExtraRule{Repo: "r", Path: "archive/old", Recursive: true, Kind: "old"}},
		{"リポ名が archive", ExtraRule{Repo: "archive", Path: "notes", Kind: "n"}},
	}
	for _, c := range cases {
		files, warnings, err := Scan(Config{Root: root, Extra: []ExtraRule{c.rule}})
		if err != nil {
			t.Fatalf("[%s] Scan: %v", c.desc, err)
		}
		if len(files) != 0 {
			t.Errorf("[%s] archive 配下なのに拾っている: %v", c.desc, files)
		}
		want := "extra " + c.rule.Repo + "/" + c.rule.Path
		if len(warnings) != 1 || !strings.Contains(warnings[0], want) || !strings.Contains(warnings[0], "archive") {
			t.Errorf("[%s] 警告に %q と archive を含む 1 件を期待: %v", c.desc, want, warnings)
		}
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

// docs/decisions.md の Stat が権限エラー等で失敗したら警告にする(存在しないのは正常で警告しない)。
func TestScan_DecisionsStatErrorWarns(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("chmod 000 で読めなくする方法が使えない環境")
	}
	root := t.TempDir()
	docs := filepath.Join(root, "r", "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "decisions.md"), []byte("# d\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(docs, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(docs, 0o755) })
	_, warns, err := Scan(Config{Root: root})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !containsSub(warns, "r/docs/decisions.md") {
		t.Errorf("decisions.md の警告が無い: %q", warns)
	}
}

// notes_dir がディレクトリでなくファイルなら警告(規約外の状態)。パスは root 相対・スラッシュ区切り。
func TestScan_NotesDirIsFileWarns(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "r", "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "notes"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, warns, err := Scan(Config{Root: root})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(warns) != 1 || warns[0] != "r/docs/notes: ディレクトリではない" {
		t.Errorf("警告 1 件「r/docs/notes: ディレクトリではない」を期待: %q", warns)
	}
}

// DescribeErr はパスを繰り返さず、存在しないは日本語の定型にする。
func TestDescribeErr(t *testing.T) {
	_, err := os.Stat(filepath.Join(t.TempDir(), "nope"))
	if got := DescribeErr(err); got != "存在しない" {
		t.Errorf("ErrNotExist: got %q", got)
	}
	pe := &fs.PathError{Op: "open", Path: "/some/path", Err: errors.New("boom")}
	if got := DescribeErr(pe); got != "boom" {
		t.Errorf("PathError: got %q", got)
	}
	if got := DescribeErr(errors.New("plain")); got != "plain" {
		t.Errorf("plain: got %q", got)
	}
}

func containsSub(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// 自動規則で拾ったファイルを extra が重ねて指しても、索引には 1 回だけ載る(先に拾った自動規則のラベルが勝つ)。
func TestScan_ExtraDoesNotDuplicateAutoFiles(t *testing.T) {
	cfg := Config{Root: "testdata/root", Extra: []ExtraRule{{Repo: "ext", Path: "docs", Recursive: true, Kind: "x"}}}
	files, _, err := Scan(cfg)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	count := map[string]int{}
	kind := map[string]string{}
	for _, f := range files {
		count[f.Rel]++
		kind[f.Rel] = f.Kind
	}
	if count["ext/docs/notes/project/extnote.md"] != 1 {
		t.Errorf("自動規則と extra で二重に載っている: %d 回", count["ext/docs/notes/project/extnote.md"])
	}
	if kind["ext/docs/notes/project/extnote.md"] != "notes/project" {
		t.Errorf("先に拾った自動規則のラベルが勝つべき: %q", kind["ext/docs/notes/project/extnote.md"])
	}
	if count["ext/docs/guides/style.md"] != 1 || kind["ext/docs/guides/style.md"] != "x/guides" {
		t.Errorf("extra だけが指すファイルは extra のラベルで 1 回: count=%d kind=%q", count["ext/docs/guides/style.md"], kind["ext/docs/guides/style.md"])
	}
}

// extra が docs 全体を指しても、docs/decisions.md は自動規則の種別 decisions で 1 回だけ載る
// (notes_dirs の入れ子と同じく decisions が勝つ。TestScan_NotesDirs_Dedupe の extra 版)。
func TestScan_ExtraDoesNotDuplicateDecisions(t *testing.T) {
	cfg := Config{Root: "testdata/root", Extra: []ExtraRule{{Repo: "repo-both", Path: "docs", Recursive: true, Kind: "x"}}}
	files, _, err := Scan(cfg)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	count := map[string]int{}
	kind := map[string]string{}
	for _, f := range files {
		count[f.Rel]++
		kind[f.Rel] = f.Kind
	}
	for _, c := range []struct{ rel, want string }{
		{"repo-both/docs/decisions.md", "decisions"},
		{"repo-both/docs/notes/common/a.md", "notes/common"},
	} {
		if count[c.rel] != 1 || kind[c.rel] != c.want {
			t.Errorf("%s: 1 回・種別 %q を期待: count=%d kind=%q", c.rel, c.want, count[c.rel], kind[c.rel])
		}
	}
}

// notes_dirs と extra.path はリポ内の相対パスに限る。".." を含む・絶対パスは設定の誤りなのでエラー。
// エラー文は「どの設定の・何が」だめかを名指しする(root 不在など別の理由で落ちたのと区別できるように)。
func TestScan_RejectsEscapingPaths(t *testing.T) {
	abs := t.TempDir() // OS ごとの絶対パス(Windows は C:\... 、他は /...)
	cases := []struct {
		desc string
		cfg  Config
		want string // エラー文に含まれるべき語
	}{
		{"notes_dirs に ..", Config{Root: "testdata/root", NotesDirs: []string{"../outside"}}, `notes_dirs: ".." でリポの外を指せない: "../outside"`},
		{"notes_dirs に途中の ..", Config{Root: "testdata/root", NotesDirs: []string{"docs/../../x"}}, `notes_dirs: ".." でリポの外を指せない`},
		{"notes_dirs に絶対パス", Config{Root: "testdata/root", NotesDirs: []string{abs}}, "notes_dirs: 絶対パスは書けない"},
		{"extra.path に ..", Config{Root: "testdata/root", Extra: []ExtraRule{{Repo: "ext", Path: "../repo-flat", Kind: "x"}}}, `extra ext/path: ".." でリポの外を指せない: "../repo-flat"`},
		{"extra.path に絶対パス", Config{Root: "testdata/root", Extra: []ExtraRule{{Repo: "ext", Path: abs, Kind: "x"}}}, "extra ext/path: 絶対パスは書けない"},
		{"extra.repo に区切り", Config{Root: "testdata/root", Extra: []ExtraRule{{Repo: "ext/docs", Path: ".", Kind: "x"}}}, `extra: repo は root 直下のディレクトリ名だけを書く: "ext/docs"`},
		{"extra.repo が空", Config{Root: "testdata/root", Extra: []ExtraRule{{Repo: "", Path: "docs", Kind: "x"}}}, `extra: repo は root 直下のディレクトリ名だけを書く: ""`},
		{"extra.repo が ..", Config{Root: "testdata/root", Extra: []ExtraRule{{Repo: "..", Path: ".", Kind: "x"}}}, `extra: repo は root 直下のディレクトリ名だけを書く: ".."`},
	}
	for _, c := range cases {
		_, _, err := Scan(c.cfg)
		if err == nil {
			t.Errorf("[%s] エラーになっていない", c.desc)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("[%s] エラー文が原因を名指ししていない: want=%q got=%q", c.desc, c.want, err)
		}
	}
	// "." と "" はリポ直下の意味で許す
	if _, _, err := Scan(Config{Root: "testdata/root", Extra: []ExtraRule{{Repo: "ext", Path: ".", Kind: "x"}}}); err != nil {
		t.Errorf("extra.path \".\" が拒否された: %v", err)
	}
	if _, _, err := Scan(Config{Root: "testdata/root", Extra: []ExtraRule{{Repo: "ext", Path: "", Kind: "x"}}}); err != nil {
		t.Errorf("extra.path \"\" が拒否された: %v", err)
	}
}
