package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile はテスト用にファイルを書く(親ディレクトリも作る)。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// makeRoot は <tmp>/root/repo-a/docs/notes/a.md を持つ最小のルートを作り、root のパスを返す。
func makeRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "root")
	writeFile(t, filepath.Join(root, "repo-a", "docs", "notes", "a.md"), "# A\n\n結論: a\n記録日: 2026-01-02\n")
	return root
}

func runOK(t *testing.T, o options) (stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	if code := run(o, &out, &errb); code != 0 {
		t.Fatalf("exit=%d want 0\nstderr=%s", code, errb.String())
	}
	return out.String(), errb.String()
}

// -root と -out だけで、設定ファイル無しでも動く。
func TestRun_FlagsOnlyWithoutConfig(t *testing.T) {
	root := makeRoot(t)
	out := filepath.Join(t.TempDir(), "sub", "catalog.md")
	runOK(t, options{root: root, out: out, date: "2026-01-03"})
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("出力が無い: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "repo-a/docs/notes/a.md") || !strings.Contains(s, "2026-01-03") {
		t.Errorf("出力内容が不正:\n%s", s)
	}
}

// 設定ファイルの root は設定ファイルのディレクトリ基準で解決し、
// 出力先の既定はそのディレクトリの index/catalog.md。
func TestRun_ConfigRelativePaths(t *testing.T) {
	root := makeRoot(t)
	hub := filepath.Join(root, "hub")
	cfg := filepath.Join(hub, "braindex.json")
	writeFile(t, cfg, `{"root": ".."}`)
	runOK(t, options{config: cfg, date: "2026-01-03"})
	if _, err := os.Stat(filepath.Join(hub, "index", "catalog.md")); err != nil {
		t.Errorf("既定の出力先 hub/index/catalog.md が無い: %v", err)
	}
}

// -root は設定ファイルの root より優先する。
func TestRun_RootFlagOverridesConfig(t *testing.T) {
	root := makeRoot(t)
	cfg := filepath.Join(t.TempDir(), "braindex.json")
	writeFile(t, cfg, `{"root": "/nonexistent/should-not-be-used"}`)
	out := filepath.Join(t.TempDir(), "catalog.md")
	runOK(t, options{config: cfg, root: root, out: out, date: "2026-01-03"})
}

// root がフラグにも設定にも無ければ失敗(終了コード 1)し、索引は書かない。
func TestRun_MissingRootFails(t *testing.T) {
	out := filepath.Join(t.TempDir(), "catalog.md")
	cfg := filepath.Join(t.TempDir(), "braindex.json")
	writeFile(t, cfg, `{}`)
	var so, se bytes.Buffer
	if code := run(options{config: cfg, out: out}, &so, &se); code != 1 {
		t.Errorf("exit=%d want 1", code)
	}
	if !strings.Contains(se.String(), "root") {
		t.Errorf("stderr に root の説明が無い: %s", se.String())
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("失敗時に索引を書いてしまった")
	}
}

// -config で明示した設定ファイルが無ければ失敗する(既定パスの不在とは区別する)。
func TestRun_ExplicitConfigMissingFails(t *testing.T) {
	var so, se bytes.Buffer
	out := filepath.Join(t.TempDir(), "c.md")
	code := run(options{config: filepath.Join(t.TempDir(), "nope.json"), root: makeRoot(t), out: out}, &so, &se)
	if code != 1 {
		t.Errorf("exit=%d want 1", code)
	}
	if !strings.Contains(se.String(), "nope.json") {
		t.Errorf("stderr に見つからなかった設定ファイル名が無い: %s", se.String())
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("失敗時に索引を書いてしまった")
	}
}

// 設定ファイルの未知のキー(notes_dir など打ち間違い)は無視せずエラーにする。
func TestRun_UnknownConfigKeyFails(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "braindex.json")
	writeFile(t, cfg, `{"root": "..", "notes_dir": "wiki"}`)
	var so, se bytes.Buffer
	if code := run(options{config: cfg}, &so, &se); code != 1 {
		t.Errorf("exit=%d want 1", code)
	}
	if !strings.Contains(se.String(), "notes_dir") {
		t.Errorf("stderr に未知のキー名が無い: %s", se.String())
	}
}

// -date は YYYY-MM-DD だけを受け付ける。
func TestRun_BadDateFails(t *testing.T) {
	var so, se bytes.Buffer
	out := filepath.Join(t.TempDir(), "c.md")
	if code := run(options{root: makeRoot(t), out: out, date: "2026/01/03"}, &so, &se); code != 1 {
		t.Errorf("exit=%d want 1", code)
	}
	if !strings.Contains(se.String(), "-date") || !strings.Contains(se.String(), "2026/01/03") {
		t.Errorf("stderr にフラグ名と渡した値が無い: %s", se.String())
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("失敗時に索引を書いてしまった")
	}
}

// 存在しない extra は警告を stderr に出し、索引は書いたうえで終了コード 2。
func TestRun_WarningsExitTwo(t *testing.T) {
	root := makeRoot(t)
	cfg := filepath.Join(t.TempDir(), "braindex.json")
	writeFile(t, cfg, `{"root": "`+filepath.ToSlash(root)+`", "extra": [{"repo": "repo-a", "path": "missing", "kind": "x"}]}`)
	out := filepath.Join(t.TempDir(), "catalog.md")
	var so, se bytes.Buffer
	if code := run(options{config: cfg, out: out, date: "2026-01-03"}, &so, &se); code != 2 {
		t.Errorf("exit=%d want 2\nstderr=%s", code, se.String())
	}
	if !strings.Contains(se.String(), "missing") {
		t.Errorf("stderr に警告が無い: %s", se.String())
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("警告ありでも索引は書くべき: %v", err)
	}
}

// 不正なフラグは使い方エラーとして終了コード 1(「警告つきで完了」の 2 と区別する)。
func TestParseArgs_UnknownFlagExitsOne(t *testing.T) {
	var se bytes.Buffer
	_, code, done := parseArgs([]string{"-bogus"}, &se)
	if !done || code != 1 {
		t.Errorf("done=%v code=%d want done=true code=1", done, code)
	}
	if !strings.Contains(se.String(), "-bogus") {
		t.Errorf("stderr にフラグ名が無い: %s", se.String())
	}
}

// 位置引数は受け付けない(サブコマンドは未実装。黙って通常の走査に入らない)。
func TestParseArgs_PositionalRejected(t *testing.T) {
	var se bytes.Buffer
	_, code, done := parseArgs([]string{"init", "-root", "x"}, &se)
	if !done || code != 1 {
		t.Errorf("done=%v code=%d want done=true code=1", done, code)
	}
	if !strings.Contains(se.String(), "init") {
		t.Errorf("stderr に受け付けなかった引数が無い: %s", se.String())
	}
}

// -h は使い方を出して 0 で終わる。
func TestParseArgs_Help(t *testing.T) {
	var se bytes.Buffer
	_, code, done := parseArgs([]string{"-h"}, &se)
	if !done || code != 0 {
		t.Errorf("done=%v code=%d want done=true code=0", done, code)
	}
	if !strings.Contains(se.String(), "-root") {
		t.Errorf("stderr に使い方が無い: %s", se.String())
	}
}

// フラグは options に入り、done=false で本処理へ進む。
func TestParseArgs_Options(t *testing.T) {
	var se bytes.Buffer
	o, _, done := parseArgs([]string{"-config", "c.json", "-root", "r", "-out", "o.md", "-date", "2026-01-03"}, &se)
	if done {
		t.Fatalf("done=true: %s", se.String())
	}
	want := options{config: "c.json", root: "r", out: "o.md", date: "2026-01-03"}
	if o != want {
		t.Errorf("got %+v want %+v", o, want)
	}
}

// 設定 JSON の末尾に余分な内容があれば失敗(先頭の 1 値だけ読んで黙認しない)。
func TestRun_TrailingContentInConfigFails(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "braindex.json")
	writeFile(t, cfg, `{"root": "."} trailing-garbage`)
	var so, se bytes.Buffer
	if code := run(options{config: cfg, root: makeRoot(t), out: filepath.Join(t.TempDir(), "c.md")}, &so, &se); code != 1 {
		t.Errorf("exit=%d want 1", code)
	}
	if !strings.Contains(se.String(), "末尾") {
		t.Errorf("stderr に末尾の説明が無い: %s", se.String())
	}
}

// 設定ファイルが無く -root も無いときは、どの設定ファイルを探したかを言う(打ち間違いに気づけるように)。
func TestRun_NoConfigNoRootMentionsConfigName(t *testing.T) {
	t.Chdir(t.TempDir())
	var so, se bytes.Buffer
	if code := run(options{}, &so, &se); code != 1 {
		t.Errorf("exit=%d want 1", code)
	}
	if !strings.Contains(se.String(), defaultConfig) {
		t.Errorf("stderr に既定の設定ファイル名 %s が無い: %s", defaultConfig, se.String())
	}
}

// 既定の設定ファイルはカレントの braindex.json(-config 無しで拾い、出力の既定はその隣の index/catalog.md)。
func TestRun_DefaultConfigInCwd(t *testing.T) {
	root := makeRoot(t)
	hub := t.TempDir()
	writeFile(t, filepath.Join(hub, defaultConfig), `{"root": "`+filepath.ToSlash(root)+`"}`)
	t.Chdir(hub)
	runOK(t, options{date: "2026-01-03"})
	if _, err := os.Stat(filepath.Join(hub, "index", "catalog.md")); err != nil {
		t.Errorf("既定の出力先 index/catalog.md が無い: %v", err)
	}
}

// -out の相対パスはカレント基準(設定ファイルのディレクトリ基準ではない)。
func TestRun_OutRelativeToCwd(t *testing.T) {
	root := makeRoot(t)
	cwd := t.TempDir()
	cfg := filepath.Join(t.TempDir(), "sub", "braindex.json")
	writeFile(t, cfg, `{"root": "`+filepath.ToSlash(root)+`"}`)
	t.Chdir(cwd)
	runOK(t, options{config: cfg, out: "here.md", date: "2026-01-03"})
	if _, err := os.Stat(filepath.Join(cwd, "here.md")); err != nil {
		t.Errorf("カレント基準の here.md が無い: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfg), "here.md")); err == nil {
		t.Errorf("設定ファイルのディレクトリに書いてしまった")
	}
}

// -version は版を 1 行 stdout に出して 0 で終わる。索引は作らない
// (どの版が入っているか利用者が言えないと、動きの違いが版差か設定か切り分けられない。
// 設計レビュー 2026-09-06 M9)。
func TestVersionFlag(t *testing.T) {
	var so, se bytes.Buffer
	if code := dispatch([]string{"-version"}, &so, &se); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, se.String())
	}
	out := so.String()
	if !strings.HasPrefix(out, "braindex ") || strings.Count(out, "\n") != 1 {
		t.Errorf("1 行で版を出す: %q", out)
	}
	if se.String() != "" {
		t.Errorf("stderr: %q", se.String())
	}
	// go test は go build で作るので版は module のもの、(devel) にはならない
	if strings.Contains(out, "(不明)") {
		t.Errorf("版を取れていない: %q", out)
	}
}
