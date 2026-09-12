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

func TestGitRunJapaneseCRLF(t *testing.T) {
	r := newTestRepo(t)
	r.write("日本語.md", "見出し\r\n本文\r\n")
	r.run("add", "--", "日本語.md")
	out, err := r.git.run(r.dir, "ls-files")
	if err != nil || out != "日本語.md\n" {
		t.Fatalf("ls-files = %q, %v", out, err)
	}
	out, err = r.git.run(r.dir, "show", ":日本語.md")
	if err != nil || out != "見出し\n本文\n" {
		t.Fatalf("show = %q, %v", out, err)
	}
}

func newTestRepo(t *testing.T) *testRepo {
	t.Helper()
	return newTestRepoAt(t, t.TempDir())
}

// newTestRepoAt は既存のディレクトリ dir を git リポにする(root 直下に複数リポを並べるテスト用)。
func newTestRepoAt(t *testing.T, dir string) *testRepo {
	t.Helper()
	g, ok := LookGit()
	if !ok {
		t.Skip("git が無い環境")
	}
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

// ASCII 以外のファイル名は core.quotePath の既定(true)で "\346\227\245..." と八進エスケープされる。
// そのままだと差分ファイルの行が読めず、Touched() のパスが索引のパスと一致しない。
func TestChangedSince_NonASCIIPath(t *testing.T) {
	r := newTestRepo(t)
	r.write("docs/notes/日本語のメモ.md", "# メモ\n")
	r.commit("2026-08-20", "jp")
	rc, err := r.git.ChangedSince(r.dir, "2026-08-10", []string{"docs/notes"})
	if err != nil {
		t.Fatal(err)
	}
	want := []ChangedFile{{Path: "docs/notes/日本語のメモ.md", Status: "追加"}}
	if !reflect.DeepEqual(rc.Files, want) {
		t.Errorf("パスがエスケープされずに出るべき: want %v got %v", want, rc.Files)
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

// SHA-256 のリポ(git init --object-format=sha256)では %H が 64 桁(git 2.39.2 で実測)。40 桁だけを見ると
// ハッシュ行を見落としてコミット数が 0 になり、全コミットの出来事が 1 束(新しい順)に混ざって前後の判定が逆転する。
func TestParseNameStatus_SHA256(t *testing.T) {
	out := strings.Join([]string{
		strings.Repeat("a", 64), "", "D\tgone.md", // 新しいコミット: 削除
		strings.Repeat("b", 64), "", "A\tgone.md", "M\tkept.md", // 古いコミット: 追加と変更
		"",
	}, "\n")
	rc := parseNameStatus(out)
	if rc.Commits != 2 {
		t.Errorf("commits: want 2 got %d", rc.Commits)
	}
	want := []ChangedFile{{Path: "kept.md", Status: "変更"}} // gone.md は窓の中で生まれて消えたので載らない
	if !reflect.DeepEqual(rc.Files, want) {
		t.Errorf("files: want %v got %v", want, rc.Files)
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

// git init 直後でまだコミットが無いリポは「前回の索引なし・差分なし」であって失敗ではない
// (git log は HEAD の指す先が無いと fatal になる。braindex init → git init の直後に review を実行する場面)。
func TestNoCommits(t *testing.T) {
	r := newTestRepo(t)
	if _, ok, err := r.git.FileAt(r.dir, "index/catalog.md", "2026-09-01"); ok || err != nil {
		t.Errorf("FileAt: 未コミットのリポは ok=false・err=nil のはず: ok=%v err=%v", ok, err)
	}
	rc, err := r.git.ChangedSince(r.dir, "2026-01-01", []string{"docs/notes"})
	if err != nil || rc.Commits != 0 || len(rc.Files) != 0 {
		t.Errorf("ChangedSince: 未コミットのリポは差分なし・err=nil のはず: err=%v rc=%+v", err, rc)
	}
}

// 親リポの中のサブディレクトリ(root 自体が 1 つの git リポで、その直下の各ディレクトリをリポ扱いする形)でも、
// --relative でパスは dir 相対になり、dir の外のファイルは入らず、show の ./<rel> も dir 基準で解決される。
func TestSubdirOfRepo(t *testing.T) {
	r := newTestRepo(t)
	r.write("sub/docs/notes/x.md", "# x\n")
	r.write("other/docs/notes/y.md", "# y\n")
	r.write("sub/index/catalog.md", "c1\n")
	r.commit("2026-08-20", "m")
	sub := filepath.Join(r.dir, "sub")
	rc, err := r.git.ChangedSince(sub, "2026-08-10", []string{"docs/notes", "docs/decisions.md"})
	if err != nil {
		t.Fatal(err)
	}
	want := []ChangedFile{{Path: "docs/notes/x.md", Status: "追加"}}
	if rc.Commits != 1 || !reflect.DeepEqual(rc.Files, want) {
		t.Errorf("サブディレクトリ相対で、外の other/ は入らないはず: commits=%d files=%v", rc.Commits, rc.Files)
	}
	s, ok, err := r.git.FileAt(sub, "index/catalog.md", "2026-08-25")
	if err != nil || !ok || string(s.Content) != "c1\n" {
		t.Errorf("FileAt(sub): ok=%v err=%v content=%q", ok, err, s.Content)
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

// PATH に git が無ければ ok=false(差分ファイルの節を飛ばす材料)。
func TestLookGit_Missing(t *testing.T) {
	t.Setenv("PATH", "")
	if g, ok := LookGit(); ok {
		t.Errorf("PATH が空なのに見つかった: %q", g.path)
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

// 差分ファイルの起点は「前回の索引を取ったコミットの時刻」。前回日の 0 時にすると、
// その日のうち索引を取る前に入った変更を前回と今回で二重に数える(設計レビュー 2026-09-06 M3b)。
// git log --since は、コミット日時が履歴の順序と食い違うとそこで走査を打ち切り、その先にある新しいコミットを
// 取りこぼす(2026-09-06 実測 → docs/notes/common/git-since-boundary.md)。--since-as-filter(git 2.37 以降)は
// 打ち切らない。実行環境の git(2.39.2 以上)は対応しているはずなので、打ち切りが起きないことを実物で確かめる
// (2.37 未満での分岐は TestSinceAsFilterFromVersion がバージョン文字列のパースだけを別途確かめる)。
func TestChangedSince_順序が食い違っても打ち切らない(t *testing.T) {
	r := newTestRepo(t)
	if !r.git.sinceAsFilter {
		t.Skip("この環境の git は --since-as-filter に対応していない(2.37 未満)")
	}
	r.write("docs/notes/first.md", "# first\n")
	r.commitAt("2026-09-06T12:00:00", "first")
	r.write("docs/notes/second.md", "# second\n") // コミット日時が親コミットより古い(履歴の順序と食い違う)
	r.commitAt("2026-09-06T11:00:00", "second")
	r.write("docs/notes/third.md", "# third\n")
	r.commitAt("2026-09-06T13:00:00", "third")

	rc, err := r.git.ChangedSince(r.dir, "2026-09-06 12:00:00", []string{"docs/notes"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range rc.Files {
		got[f.Path] = true
	}
	// 打ち切られていると first.md(ちょうど起点の時刻)が落ちる(--since だけだと 2026-09-06 実測のとおり再現する)
	if !got["docs/notes/first.md"] || !got["docs/notes/third.md"] || got["docs/notes/second.md"] {
		t.Errorf("打ち切りが起きている(または second.md が誤って入っている): files=%v", rc.Files)
	}
}

// バージョン文字列のパースだけを確かめる(2.37 未満の git が手元に無いため、実際の分岐はこの純関数のテストで代える)。
func TestSinceAsFilterFromVersion(t *testing.T) {
	cases := []struct {
		out  string
		want bool
	}{
		{"git version 2.37.0\n", true},
		{"git version 2.37.0.windows.1\n", true},
		{"git version 2.39.2.windows.1\n", true},
		{"git version 2.40.0\n", true},
		{"git version 3.0.0\n", true},
		{"git version 2.36.9\n", false},
		{"git version 2.9.5\n", false},
		{"git version 1.9.5\n", false},
		{"git version 2\n", false},
		{"not a version string\n", false},
		{"", false},
	}
	for _, c := range cases {
		if got := sinceAsFilterFromVersion(c.out); got != c.want {
			t.Errorf("%q: want %v got %v", c.out, c.want, got)
		}
	}
}

// git の --since はその時刻ちょうどのコミットを含む(docs/notes/common/git-since-boundary.md)。
func TestChangedSince_起点は時刻で渡せる(t *testing.T) {
	r := newTestRepo(t)
	r.write("docs/notes/before.md", "# before\n")
	r.commitAt("2026-08-22T09:00:00", "before")
	r.write("docs/notes/at.md", "# at\n")
	r.commitAt("2026-08-22T10:00:00", "at")
	r.write("docs/notes/after.md", "# after\n")
	r.commitAt("2026-08-22T11:00:00", "after")

	// 10:00 ちょうどのコミットは含む(--since は inclusive)
	// 時刻はコミット時と同じくタイムゾーンなし = ローカル解釈。実行環境の TZ に依らず同じ結果になる
	rc, err := r.git.ChangedSince(r.dir, "2026-08-22 10:00:00", []string{"docs/notes"})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range rc.Files {
		got[f.Path] = true
	}
	if rc.Commits != 2 || !got["docs/notes/at.md"] || !got["docs/notes/after.md"] || got["docs/notes/before.md"] {
		t.Errorf("時刻の起点: commits=%d files=%v", rc.Commits, rc.Files)
	}

	// 日付だけを渡したときは従来どおりその日の 0 時から(実行時刻で結果が変わらない)
	rc, err = r.git.ChangedSince(r.dir, "2026-08-22", []string{"docs/notes"})
	if err != nil {
		t.Fatal(err)
	}
	if rc.Commits != 3 {
		t.Errorf("日付だけの起点: commits=%d", rc.Commits)
	}
}
