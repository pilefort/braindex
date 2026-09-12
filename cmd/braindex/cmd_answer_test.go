package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pilefort/braindex/internal/mdhtml"
)

// 置き場所と「開く」をテスト用に差し替える。開いた先のパスを返す。
func stubAnswer(t *testing.T) (dir string, opened *[]string) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), "answers")
	var o []string
	origDir, origOpen := answersDir, openInBrowser
	answersDir = func() string { return dir }
	openInBrowser = func(p string) error { o = append(o, p); return nil }
	t.Cleanup(func() { answersDir, openInBrowser = origDir, origOpen })
	return dir, &o
}

// md を渡すと一時置き場に <同名>.html を書き、既定ブラウザで開く。中身は mdhtml.Page と同じ。BOM は落とす。
func TestAnswer_WriteAndOpen(t *testing.T) {
	dir, opened := stubAnswer(t)
	src := filepath.Join(t.TempDir(), "memo.md")
	writeFile(t, src, "\uFEFF# 題名\n\n本文\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"answer", src}, &so, &se); code != 0 {
		t.Fatalf("exit=%d want 0\nstdout=%s\nstderr=%s", code, so.String(), se.String())
	}
	out := filepath.Join(dir, "memo.html")
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != mdhtml.Page("# 題名\n\n本文\n", "題名") {
		t.Error("HTML の中身が mdhtml.Page と違う(BOM が残っている?)")
	}
	if !strings.Contains(so.String(), "書いた "+out) {
		t.Errorf("書いたパスの報告が無い: %s", so.String())
	}
	if len(*opened) != 1 || (*opened)[0] != out {
		t.Errorf("開いた先が違う: %v", *opened)
	}
}

// -out で出力先を指定できる。-no-open なら開かない。
func TestAnswer_OutNoOpen(t *testing.T) {
	_, opened := stubAnswer(t)
	src := filepath.Join(t.TempDir(), "a.md")
	writeFile(t, src, "x\n")
	out := filepath.Join(t.TempDir(), "sub", "b.html")
	var so, se bytes.Buffer
	if code := dispatch([]string{"answer", "-no-open", "-out", out, src}, &so, &se); code != 0 {
		t.Fatalf("exit=%d want 0\n%s", code, se.String())
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("-out に書かれていない: %v", err)
	}
	if len(*opened) != 0 {
		t.Errorf("-no-open なのに開いた: %v", *opened)
	}
}

// 実行のたびに置き場所の -ttl-days より古いファイルを消す。0 なら消さない。サブディレクトリは触らない。
func TestAnswer_TTL(t *testing.T) {
	dir, _ := stubAnswer(t)
	old := filepath.Join(dir, "old.html")
	fresh := filepath.Join(dir, "fresh.html")
	writeFile(t, old, "o")
	writeFile(t, fresh, "f")
	writeFile(t, filepath.Join(dir, "sub", "old.html"), "s")
	past := time.Now().Add(-20 * 24 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "a.md")
	writeFile(t, src, "x\n")

	var so, se bytes.Buffer
	if code := dispatch([]string{"answer", "-no-open", "-ttl-days", "0", src}, &so, &se); code != 0 {
		t.Fatalf("exit=%d\n%s", code, se.String())
	}
	if _, err := os.Stat(old); err != nil {
		t.Error("-ttl-days 0 なのに消えた")
	}
	so.Reset()
	if code := dispatch([]string{"answer", "-no-open", src}, &so, &se); code != 0 {
		t.Fatalf("exit=%d\n%s", code, se.String())
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("14 日より古いファイルが残っている")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("新しいファイルが消えた")
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", "old.html")); err != nil {
		t.Error("サブディレクトリの中を消した")
	}
	if !strings.Contains(so.String(), "14 日より古い 1 ファイルを消した") {
		t.Errorf("削除の報告が無い: %s", so.String())
	}
	if code := dispatch([]string{"answer", "-ttl-days", "-1", src}, &so, &se); code != 1 {
		t.Errorf("負の -ttl-days は exit 1 のはず: %d", code)
	}
}

// -dir は置き場所を表示(作成)して終わる。-purge は置き場所の中を全部消す。
func TestAnswer_DirAndPurge(t *testing.T) {
	dir, _ := stubAnswer(t)
	var so, se bytes.Buffer
	if code := dispatch([]string{"answer", "-dir"}, &so, &se); code != 0 || strings.TrimSpace(so.String()) != dir {
		t.Fatalf("-dir: exit=%d out=%q want %q", code, so.String(), dir)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Error("-dir が置き場所を作っていない")
	}
	writeFile(t, filepath.Join(dir, "a.html"), "a")
	writeFile(t, filepath.Join(dir, "b.md"), "b")
	so.Reset()
	if code := dispatch([]string{"answer", "-purge"}, &so, &se); code != 0 {
		t.Fatalf("-purge: exit=%d\n%s", code, se.String())
	}
	if !strings.Contains(so.String(), "2 ファイルを消した") {
		t.Errorf("-purge の報告が違う: %s", so.String())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("-purge 後に残っている: %d", len(entries))
	}
}

// 引数なし・無いファイルは 1。
func TestAnswer_Errors(t *testing.T) {
	stubAnswer(t)
	var so, se bytes.Buffer
	if code := dispatch([]string{"answer"}, &so, &se); code != 1 {
		t.Errorf("引数なしは exit 1 のはず: %d", code)
	}
	se.Reset()
	if code := dispatch([]string{"answer", filepath.Join(t.TempDir(), "none.md")}, &so, &se); code != 1 {
		t.Errorf("無いファイルは exit 1 のはず: %d", code)
	}
	if !strings.Contains(se.String(), "braindex answer:") {
		t.Errorf("読めない理由が stderr に出ていない: %q", se.String())
	}
}

// ブラウザを開けなかったときは、HTML は書いたうえで開き方を案内して 1 で終わる。
func TestAnswer_OpenFails(t *testing.T) {
	dir, _ := stubAnswer(t)
	openInBrowser = func(string) error { return errors.New("開けない") }
	src := filepath.Join(t.TempDir(), "a.md")
	writeFile(t, src, "x\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"answer", src}, &so, &se); code != 1 {
		t.Errorf("開けなければ exit 1 のはず: %d", code)
	}
	out := filepath.Join(dir, "a.html")
	if _, err := os.Stat(out); err != nil {
		t.Errorf("開けなくても HTML は書かれているべき: %v", err)
	}
	if !strings.Contains(se.String(), out) {
		t.Errorf("案内に出力先のパスが無い: %q", se.String())
	}
}

// -h は使い方を出して 0(フラグの誤りの 1 と区別する)。
func TestAnswer_Help(t *testing.T) {
	stubAnswer(t)
	var so, se bytes.Buffer
	if code := dispatch([]string{"answer", "-h"}, &so, &se); code != 0 {
		t.Errorf("-h は exit 0 のはず: %d", code)
	}
	if !strings.Contains(se.String(), "使い方: braindex answer") {
		t.Errorf("使い方が出ていない: %q", se.String())
	}
}

// md に書いた相対パスの画像は、md の置き場所からの絶対パスにして出す。
// HTML は一時置き場に書かれるので、相対のままでは開けない(2026-09-12 の不具合)。
func TestAnswer_RelativePathResolvedFromMarkdownDir(t *testing.T) {
	dir, _ := stubAnswer(t)
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "memo.md")
	writeFile(t, src, "# 題名\n\n![図](./fig.svg)\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"answer", "-no-open", src}, &so, &se); code != 0 {
		t.Fatalf("exit=%d want 0\nstderr=%s", code, se.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, "memo.html"))
	if err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(filepath.Join(srcDir, "fig.svg"))
	if err != nil {
		t.Fatal(err)
	}
	want := `<img src="file:///` + strings.TrimPrefix(filepath.ToSlash(abs), "/") + `"`
	if !strings.Contains(string(got), want) {
		t.Errorf("出力に %s が無い", want)
	}
}
