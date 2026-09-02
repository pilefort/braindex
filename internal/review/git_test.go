package review

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// testRepo は一時ディレクトリに git リポを作り、日付を固定してコミットする道具。
type testRepo struct {
	t      *testing.T
	dir    string
	git    Git
	config string // 空の設定ファイル。GIT_CONFIG_GLOBAL に与えて利用者のグローバル設定(commit.gpgsign 等)を読ませない
	date   string // 次のコミットに使う日時(GIT_AUTHOR_DATE / GIT_COMMITTER_DATE)
}

func newTestRepo(t *testing.T) *testRepo {
	t.Helper()
	g, ok := LookGit()
	if !ok {
		t.Skip("git が無い環境")
	}
	dir := t.TempDir()
	config := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(config, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r := &testRepo{t: t, dir: dir, git: g, config: config}
	r.run("init", "-q", "-b", "main")
	return r
}

// run は日付や利用者名を固定して git を実行する(環境の設定に依らないようにする)。
// グローバル設定とシステム設定は読まない(commit.gpgsign=true の環境では commit が署名を求めて失敗する)。
func (r *testRepo) run(args ...string) string {
	r.t.Helper()
	cmd := exec.Command(r.git.path, append([]string{"-C", r.dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "core.autocrlf=false"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+r.date, "GIT_COMMITTER_DATE="+r.date, "GIT_CONFIG_GLOBAL="+r.config, "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func (r *testRepo) write(rel, content string) {
	r.t.Helper()
	p := filepath.Join(r.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *testRepo) remove(rel string) {
	r.t.Helper()
	if err := os.Remove(filepath.Join(r.dir, filepath.FromSlash(rel))); err != nil {
		r.t.Fatal(err)
	}
}

// commit は作業ツリーの全変更を date(YYYY-MM-DD)のローカル時刻の正午でコミットする。
// --since / --until は git がローカル時刻で解釈するので、正午なら TZ に依らずその日の中に入る
// (UTC 正午に固定すると UTC+12 以上の TZ では前日扱いになり、--until=<日> 23:59:59 から漏れる)。
func (r *testRepo) commit(date, msg string) {
	r.t.Helper()
	r.commitAt(date+"T12:00:00", msg)
}

// commitAt は作業ツリーの全変更を datetime(YYYY-MM-DDThh:mm:ss。時差の接尾辞なし＝ローカル時刻)でコミットする。
func (r *testRepo) commitAt(datetime, msg string) {
	r.t.Helper()
	r.date = datetime
	r.run("add", "-A")
	r.run("commit", "-q", "-m", msg)
}

// 3 コミットの歴史: 08-01 に a.md・decisions・TODO を作り、08-20 に a.md 変更＋b.md と tmp.md 追加、
// 08-25 に tmp.md 削除・b.md → c.md 改名・decisions 変更・TODO 変更(範囲外)。
func historyRepo(t *testing.T) *testRepo {
	r := newTestRepo(t)
	r.write("docs/notes/a.md", "# A\n\n結論: a\n")
	r.write("docs/decisions.md", "# 決定\n\n## 1\n")
	r.write("work/TODO.md", "- [ ] x\n")
	r.commit("2026-08-01", "first")
	r.write("docs/notes/a.md", "# A\n\n結論: a2\n")
	r.write("docs/notes/b.md", "# B\n\n結論: b\n")
	r.write("docs/notes/tmp.md", "# tmp\n")
	r.commit("2026-08-20", "second")
	r.remove("docs/notes/tmp.md")
	r.remove("docs/notes/b.md")
	r.write("docs/notes/c.md", "# B\n\n結論: b\n")
	r.write("docs/decisions.md", "# 決定\n\n## 1\n\n## 2\n")
	r.write("work/TODO.md", "- [ ] x\n- [ ] y\n")
	r.commit("2026-08-25", "third")
	return r
}

func TestChangedSince(t *testing.T) {
	r := historyRepo(t)
	specs := []string{"docs/notes", "docs/decisions.md"}
	rc, err := r.git.ChangedSince(r.dir, "2026-08-10", specs)
	if err != nil {
		t.Fatalf("ChangedSince: %v", err)
	}
	if rc.Commits != 2 {
		t.Errorf("コミット数: want 2 got %d", rc.Commits)
	}
	// b.md と tmp.md は窓の中で作られて消えた(改名の旧側・削除)ので載らない。c.md は改名の新側で追加。TODO は範囲外
	want := []ChangedFile{
		{Path: "docs/decisions.md", Status: "変更"},
		{Path: "docs/notes/a.md", Status: "変更"},
		{Path: "docs/notes/c.md", Status: "追加"},
	}
	if len(rc.Files) != len(want) {
		t.Fatalf("files: want %v got %v", want, rc.Files)
	}
	for i := range want {
		if rc.Files[i] != want[i] {
			t.Errorf("files[%d]: want %v got %v", i, want[i], rc.Files[i])
		}
	}

	// 窓を 08-22 以降にすると、b.md は前回時点に存在したので削除、c.md は追加、tmp.md は削除
	rc, err = r.git.ChangedSince(r.dir, "2026-08-22", specs)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range rc.Files {
		got[f.Path] = f.Status
	}
	if rc.Commits != 1 || got["docs/notes/b.md"] != "削除" || got["docs/notes/c.md"] != "追加" || got["docs/notes/tmp.md"] != "削除" || got["docs/decisions.md"] != "変更" {
		t.Errorf("08-22 以降: commits=%d files=%v", rc.Commits, rc.Files)
	}

	// 前回日より後のコミットが無ければ空
	rc, err = r.git.ChangedSince(r.dir, "2026-09-01", specs)
	if err != nil || rc.Commits != 0 || len(rc.Files) != 0 {
		t.Errorf("差分なし: err=%v rc=%+v", err, rc)
	}
}

// --since は前回日の 0 時から。git は日付だけの --since を「その日の今の時刻」と解釈するので、時刻を明示しないと
// 前回日の 0 時〜実行時刻のコミットが、実行する時刻しだいで落ちる。
func TestChangedSince_FromStartOfDay(t *testing.T) {
	r := newTestRepo(t)
	r.write("docs/notes/before.md", "# before\n")
	r.commitAt("2026-08-21T23:59:59", "before")
	r.write("docs/notes/midnight.md", "# midnight\n")
	r.commitAt("2026-08-22T00:00:00", "midnight")
	rc, err := r.git.ChangedSince(r.dir, "2026-08-22", []string{"docs/notes"})
	if err != nil {
		t.Fatal(err)
	}
	want := []ChangedFile{{Path: "docs/notes/midnight.md", Status: "追加"}}
	if rc.Commits != 1 || !reflect.DeepEqual(rc.Files, want) {
		t.Errorf("前回日の 0 時のコミットだけが入るべき: commits=%d files=%v", rc.Commits, rc.Files)
	}
}

// --name-status の解析だけを、git を呼ばずに確かめる(改名・複製・型変更・空行)。
func TestParseNameStatus(t *testing.T) {
	out := strings.Join([]string{
		"1111111111111111111111111111111111111111",
		"",
		"R100\told.md\tnew.md",
		"C075\tsrc.md\tcopy.md",
		"T\tlink.md",
		"2222222222222222222222222222222222222222",
		"",
		"A\tborn-then-gone.md",
		"D\tborn-then-gone.md", // 同じコミットには出ないが、順序の扱いを確かめる
		"M\told.md",
		"",
	}, "\n")
	rc := parseNameStatus(out)
	if rc.Commits != 2 {
		t.Errorf("commits: %d", rc.Commits)
	}
	got := map[string]string{}
	for _, f := range rc.Files {
		got[f.Path] = f.Status
	}
	want := map[string]string{"old.md": "削除", "new.md": "追加", "copy.md": "追加", "link.md": "変更"}
	if len(got) != len(want) {
		t.Errorf("files: %v", rc.Files)
	}
	for p, s := range want {
		if got[p] != s {
			t.Errorf("%s: want %s got %q", p, s, got[p])
		}
	}
}

func TestFileAt(t *testing.T) {
	r := newTestRepo(t)
	r.write("index/catalog.md", "v1\n")
	r.commit("2026-08-01", "v1")
	r.write("index/catalog.md", "v2\n")
	r.commit("2026-08-20", "v2")
	r.write("index/catalog.md", "v3\n")
	r.commit("2026-08-25", "v3")
	r.write("index/catalog.md", "dirty\n") // 作業ツリーの未コミット版は見ない

	s, ok, err := r.git.FileAt(r.dir, "index/catalog.md", "2026-08-20")
	if err != nil || !ok {
		t.Fatalf("FileAt: ok=%v err=%v", ok, err)
	}
	if string(s.Content) != "v2\n" || s.Date != "2026-08-20" || len(s.Commit) < 7 {
		t.Errorf("snapshot: %+v", s)
	}
	if _, ok, err := r.git.FileAt(r.dir, "index/catalog.md", "2026-07-01"); ok || err != nil {
		t.Errorf("前回日以前のコミットが無いのに ok=%v err=%v", ok, err)
	}
	if _, ok, err := r.git.FileAt(r.dir, "index/nope.md", "2026-09-01"); ok || err != nil {
		t.Errorf("存在しないファイル: ok=%v err=%v", ok, err)
	}
}

func TestInRepo(t *testing.T) {
	r := newTestRepo(t)
	if !r.git.InRepo(r.dir) {
		t.Errorf("git リポなのに false")
	}
	sub := filepath.Join(r.dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if !r.git.InRepo(sub) {
		t.Errorf("サブディレクトリなのに false")
	}
	plain := t.TempDir()
	if r.git.InRepo(plain) {
		t.Errorf("git 管理外なのに true(親ディレクトリがリポの環境なら期待どおりでない)")
	}
	if _, err := r.git.ChangedSince(plain, "2026-01-01", []string{"docs"}); err == nil {
		t.Errorf("git 管理外で ChangedSince がエラーにならない")
	}
}

func TestWriteChangesSection(t *testing.T) {
	c := Changes{
		Since:     "2026-08-19",
		Pathspecs: []string{"docs/notes", "docs/decisions.md"},
		Repos: []RepoChanges{
			{Repo: "alpha", Commits: 2, Files: []ChangedFile{{Path: "docs/decisions.md", Status: "変更"}, {Path: "docs/notes/x.md", Status: "追加"}}},
			{Repo: "beta"},
		},
		Skipped: []string{"gamma"},
	}
	var b strings.Builder
	WriteChangesSection(&b, c)
	want := "## 差分ファイル（リポ別）\n\n2026-08-19 以降のコミットで docs/notes・docs/decisions.md に触れたファイル（`git log --since --name-status`）。\n\n### alpha（コミット 2）\n- 変更: docs/decisions.md\n- 追加: docs/notes/x.md\n\ngit 管理外（飛ばした）: gamma\n"
	if b.String() != want {
		t.Errorf("節が不一致:\n--- got ---\n%s\n--- want ---\n%s", b.String(), want)
	}
	if touched := c.Touched(); !touched["alpha/docs/notes/x.md"] || len(touched) != 2 {
		t.Errorf("Touched: %v", touched)
	}

	b.Reset()
	WriteChangesSection(&b, Changes{Since: "2026-08-19", Pathspecs: []string{"docs/notes"}, Repos: []RepoChanges{{Repo: "beta"}}})
	if !strings.Contains(b.String(), "\n- なし\n") || strings.Contains(b.String(), "###") {
		t.Errorf("差分なし:\n%s", b.String())
	}

	b.Reset()
	WriteChangesSection(&b, Changes{GitMissing: true})
	if !strings.Contains(b.String(), "git が見つからない") {
		t.Errorf("git 不在:\n%s", b.String())
	}
}
