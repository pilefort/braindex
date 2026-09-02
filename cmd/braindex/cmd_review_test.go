package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// hubWithRepo は親ディレクトリに hub(braindex init)と repo-a(ノート 1 つ)を作る。
func hubWithRepo(t *testing.T) (parent, hub string) {
	t.Helper()
	parent = t.TempDir()
	hub = filepath.Join(parent, "hub")
	writeFile(t, filepath.Join(parent, "repo-a", "docs", "notes", "a.md"), "# A\n\n結論: a\n記録日: 2026-01-02\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"init", hub}, &so, &se); code != 0 {
		t.Fatalf("init exit=%d\n%s", code, se.String())
	}
	return parent, hub
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	return string(b)
}

func mustContain(t *testing.T, what, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Errorf("%s に %q が無い:\n%s", what, sub, s)
		}
	}
}

// git を使うテストの道具。日付を固定してコミットする。git が無ければ skip。
func gitOrSkip(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無い環境")
	}
}

func gitRun(t *testing.T, dir, date string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "core.autocrlf=false"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date+"T12:00:00+00:00", "GIT_COMMITTER_DATE="+date+"T12:00:00+00:00")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitCommitAll(t *testing.T, dir, date, msg string) {
	t.Helper()
	gitRun(t, dir, date, "add", "-A")
	gitRun(t, dir, date, "commit", "-q", "-m", msg)
}

// init した空 hub でそのまま review が動く。git 管理外なので警告つき(終了コード 2)だが下書きは書かれ、
// 機械節は埋まり、判断節は見出しだけ。同じ日にもう一度実行しても上書きしない(終了コード 1)。
func TestReview_E2E_NoGit(t *testing.T) {
	_, hub := hubWithRepo(t)
	cfg := filepath.Join(hub, "braindex.json")
	var so, se bytes.Buffer
	code := dispatch([]string{"review", "-config", cfg, "-date", "2026-09-02"}, &so, &se)
	if code != 2 {
		t.Fatalf("exit=%d want 2\nstderr=%s", code, se.String())
	}
	out := filepath.Join(hub, "work", "review", "2026-09-02.md")
	mustContain(t, "stdout", so.String(), "review 下書き: "+out+"(前回 2026-08-19)")
	mustContain(t, "stderr", se.String(), "repo-a: git 管理外", "警告")
	got := readFile(t, out)
	mustContain(t, "下書き", got,
		"# 週次レビュー 2026-09-02\n",
		"前回: 2026-08-19（初回のため 14 日前）\n",
		"## 索引（件数と増減）\n",
		"前回 0 件 → 今回 ", // hub 自身の docs/decisions.md も載るので件数は固定しない
		"前回の索引: なし（初回。全件を追加として数える）",
		"- 追加: repo-a/docs/notes/a.md（2026-01-02・A）\n",
		"## 差分ファイル（リポ別）\n",
		"git 管理外（飛ばした）: ",
		"## 放置 TODO\n\n4 週間以上（2026-08-05 以前から）",
		"## アーカイブ候補（機械条件のみ）\n\n索引の日付が 6 か月より前（2026-03-02 より前）",
		"移動・削除はしない。\n\n- なし\n", // a.md は 6 か月より前だが、初回は全件が索引の「追加」なので候補にしない
		"## 今週の差分ダイジェスト（リポ別）\n\n（",
		"## アーカイブ（実施・見送りと理由）\n",
		"## 次アクション\n\n（1〜3 件）\n")
	if strings.Contains(got, "\r") {
		t.Errorf("CRLF が混入")
	}
	if _, err := os.Stat(filepath.Join(hub, "index", "catalog.md")); err == nil {
		t.Errorf("review が索引を書いてしまった")
	}

	// 2 回目: 既にあるので書かない
	so.Reset()
	se.Reset()
	if code := dispatch([]string{"review", "-config", cfg, "-date", "2026-09-02"}, &so, &se); code != 1 {
		t.Errorf("2 回目 exit=%d want 1", code)
	}
	mustContain(t, "2 回目 stderr", se.String(), "既にある", "-stdout")
	if readFile(t, out) != got {
		t.Errorf("2 回目で上書きされた")
	}

	// -stdout はファイルに書かず標準出力へ。既存ファイルがあっても動く
	so.Reset()
	se.Reset()
	if code := dispatch([]string{"review", "-config", cfg, "-date", "2026-09-02", "-stdout"}, &so, &se); code != 2 {
		t.Errorf("-stdout exit=%d want 2\n%s", code, se.String())
	}
	if so.String() != got {
		t.Errorf("-stdout の内容がファイルと違う:\n%s", so.String())
	}
}

// 前回日の解決: 記録の置き場にある今日より前で最新の YYYY-MM-DD.md。未来の日付・他の名前は無視。-since が最優先。
func TestReview_SinceResolution(t *testing.T) {
	_, hub := hubWithRepo(t)
	cfg := filepath.Join(hub, "braindex.json")
	dir := filepath.Join(hub, "work", "review")
	for _, n := range []string{"2026-08-20.md", "2026-08-13.md", "2026-09-10.md", "notes.md"} {
		writeFile(t, filepath.Join(dir, n), "x\n")
	}
	var so, se bytes.Buffer
	dispatch([]string{"review", "-config", cfg, "-date", "2026-09-03", "-stdout"}, &so, &se)
	mustContain(t, "前回日", so.String(), "前回: 2026-08-20（work/review/2026-08-20.md）\n")

	so.Reset()
	dispatch([]string{"review", "-config", cfg, "-date", "2026-09-03", "-since", "2026-08-01", "-stdout"}, &so, &se)
	mustContain(t, "-since", so.String(), "前回: 2026-08-01（-since で指定）\n")

	so.Reset()
	se.Reset()
	if code := dispatch([]string{"review", "-config", cfg, "-since", "8/1", "-stdout"}, &so, &se); code != 1 || !strings.Contains(se.String(), "YYYY-MM-DD") {
		t.Errorf("不正な -since: exit=%d stderr=%s", code, se.String())
	}
}

// git 管理下の hub とリポ: 前回の索引は前回日時点のコミットから取り、差分ファイルはコミットから集める。警告なし(終了コード 0)。
func TestReview_E2E_WithGit(t *testing.T) {
	gitOrSkip(t)
	parent, hub := hubWithRepo(t)
	repoA := filepath.Join(parent, "repo-a")
	gitRun(t, repoA, "2026-08-01", "init", "-q", "-b", "main")
	gitCommitAll(t, repoA, "2026-08-01", "a")
	gitRun(t, hub, "2026-08-09", "init", "-q", "-b", "main")
	cfg := filepath.Join(hub, "braindex.json")
	var so, se bytes.Buffer
	if code := dispatch([]string{"-config", cfg, "-date", "2026-08-09"}, &so, &se); code != 0 {
		t.Fatalf("catalog exit=%d\n%s", code, se.String())
	}
	writeFile(t, filepath.Join(hub, "work", "review", "2026-08-09.md"), "# 週次レビュー 2026-08-09\n")
	gitCommitAll(t, hub, "2026-08-09", "review 08-09")

	// その後の変更: repo-a に b.md を足し、a.md の要旨を変える。hub の索引は再生成してコミット(週の途中の運用)
	writeFile(t, filepath.Join(repoA, "docs", "notes", "b.md"), "# B\n\n結論: b\n記録日: 2026-08-20\n")
	writeFile(t, filepath.Join(repoA, "docs", "notes", "a.md"), "# A\n\n結論: a を直した\n記録日: 2026-01-02\n")
	gitCommitAll(t, repoA, "2026-08-20", "b")
	so.Reset()
	se.Reset()
	if code := dispatch([]string{"-config", cfg, "-date", "2026-08-21"}, &so, &se); code != 0 {
		t.Fatalf("catalog 2 exit=%d\n%s", code, se.String())
	}
	gitCommitAll(t, hub, "2026-08-21", "catalog 08-21")

	so.Reset()
	se.Reset()
	if code := dispatch([]string{"review", "-config", cfg, "-date", "2026-09-02", "-stdout"}, &so, &se); code != 0 {
		t.Fatalf("review exit=%d\nstderr=%s", code, se.String())
	}
	got := so.String()
	mustContain(t, "下書き", got,
		"前回: 2026-08-09（work/review/2026-08-09.md）\n",
		"前回の索引: index/catalog.md（コミット ", // 08-21 の再生成でなく 08-09 時点と比べる
		"・2026-08-09）\n",
		"### repo-a\n- 追加: repo-a/docs/notes/b.md（2026-08-20・B）\n- 変更（要旨）: repo-a/docs/notes/a.md（2026-01-02・A）\n",
		"### repo-a（コミット 1）\n- 変更: docs/notes/a.md\n- 追加: docs/notes/b.md\n",
		"## アーカイブ候補（機械条件のみ）\n\n索引の日付が 6 か月より前（2026-03-02 より前）で、今回の差分に無いノート。")
	// a.md は 6 か月より前だが今回触られたので候補にならない
	if strings.Contains(got[strings.Index(got, "## アーカイブ候補"):], "repo-a/docs/notes/a.md") {
		t.Errorf("今回触った a.md がアーカイブ候補に出ている:\n%s", got)
	}
	if strings.Contains(got, "git 管理外") {
		t.Errorf("git 管理外の表示が出ている:\n%s", got)
	}
}

// 引数の誤り: 設定ファイルが無い・位置引数・不正なフラグは 1 で何も書かない。-h は 0。
func TestReview_BadArgs(t *testing.T) {
	var so, se bytes.Buffer
	missing := filepath.Join(t.TempDir(), "braindex.json")
	if code := dispatch([]string{"review", "-config", missing}, &so, &se); code != 1 || !strings.Contains(se.String(), "設定ファイルが無い") {
		t.Errorf("設定なし: exit=%d stderr=%s", code, se.String())
	}
	se.Reset()
	if code := dispatch([]string{"review", "extra"}, &so, &se); code != 1 || !strings.Contains(se.String(), "受け付けない") {
		t.Errorf("位置引数: exit=%d stderr=%s", code, se.String())
	}
	se.Reset()
	if code := dispatch([]string{"review", "-nope"}, &so, &se); code != 1 || !strings.Contains(se.String(), "使い方") {
		t.Errorf("不正なフラグ: exit=%d stderr=%s", code, se.String())
	}
	se.Reset()
	if code := dispatch([]string{"review", "-h"}, &so, &se); code != 0 || !strings.Contains(se.String(), "使い方: braindex review") {
		t.Errorf("-h: exit=%d stderr=%s", code, se.String())
	}
	if _, err := os.Stat("work"); err == nil {
		t.Errorf("カレントディレクトリに work/ が作られた")
	}
}
