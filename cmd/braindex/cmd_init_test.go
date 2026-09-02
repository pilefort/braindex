package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// braindex init <dir> は hub の雛形を展開し、作成したファイルを stdout に列挙する。
func TestInit_Hub(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hub")
	var so, se bytes.Buffer
	if code := dispatch([]string{"init", dir}, &so, &se); code != 0 {
		t.Fatalf("exit=%d want 0\nstderr=%s", code, se.String())
	}
	for _, p := range []string{"README.md", "braindex.json", "docs/conventions.md", "work/review/.gitkeep", ".claude/skills/braindex-review/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err != nil {
			t.Errorf("作られていない: %s", p)
		}
	}
	if !strings.Contains(so.String(), "作成: braindex.json") {
		t.Errorf("stdout に作成の記録が無い: %s", so.String())
	}

	// 再実行: 何も上書きせず、その旨を出す
	so.Reset()
	se.Reset()
	if code := dispatch([]string{"init", dir}, &so, &se); code != 0 {
		t.Fatalf("2 回目 exit=%d want 0\nstderr=%s", code, se.String())
	}
	if !strings.Contains(so.String(), "保持(既存): README.md") || strings.Contains(so.String(), "作成: ") {
		t.Errorf("2 回目の出力が不正: %s", so.String())
	}
}

// 展開した hub でそのまま braindex が動く(設定の root は ".." で親ディレクトリ)。
func TestInit_ThenGenerate(t *testing.T) {
	parent := t.TempDir()
	hub := filepath.Join(parent, "hub")
	writeFile(t, filepath.Join(parent, "repo-a", "docs", "notes", "a.md"), "# A\n\n結論: a\n記録日: 2026-01-02\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"init", hub}, &so, &se); code != 0 {
		t.Fatalf("init exit=%d\n%s", code, se.String())
	}
	so.Reset()
	se.Reset()
	cfg := filepath.Join(hub, "braindex.json")
	if code := dispatch([]string{"-config", cfg, "-date", "2026-01-03"}, &so, &se); code != 0 {
		t.Fatalf("generate exit=%d\n%s", code, se.String())
	}
	b, err := os.ReadFile(filepath.Join(hub, "index", "catalog.md"))
	if err != nil {
		t.Fatalf("catalog が無い: %v", err)
	}
	if !strings.Contains(string(b), "repo-a/docs/notes/a.md") {
		t.Errorf("hub 自身の設定で親ディレクトリのリポが索引されていない:\n%s", b)
	}
}

// 展開が途中で失敗しても、そこまでに作ったファイルは stdout に列挙する(無言で作らない)。
// docs を通常ファイルにしておくと、docs/ より前の 5 ファイルを作った後で失敗する。
func TestInit_PartialFailureListsCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hub")
	writeFile(t, filepath.Join(dir, "docs"), "x")
	var so, se bytes.Buffer
	if code := dispatch([]string{"init", dir}, &so, &se); code != 1 {
		t.Fatalf("exit=%d want 1\nstderr=%s", code, se.String())
	}
	if !strings.Contains(se.String(), "braindex init:") {
		t.Errorf("stderr にエラーが無い: %s", se.String())
	}
	for _, p := range []string{".gitattributes", "CLAUDE.md", "README.md", "braindex.json"} {
		if !strings.Contains(so.String(), "作成: "+p+"\n") {
			t.Errorf("失敗前に作った %s が stdout に無い:\n%s", p, so.String())
		}
	}
	if strings.Contains(so.String(), "braindex init: 作成") || strings.Contains(so.String(), "次:") {
		t.Errorf("失敗したのに完了の要約や次の案内が出ている:\n%s", so.String())
	}
}

// 引数の誤り: ディレクトリが 2 つ以上・不正なフラグ。-h は使い方を出して 0。
func TestInit_BadArgs(t *testing.T) {
	var so, se bytes.Buffer
	if code := dispatch([]string{"init", "a", "b"}, &so, &se); code != 1 {
		t.Errorf("引数 2 つ exit=%d want 1", code)
	}
	se.Reset()
	if code := dispatch([]string{"init", "-nope"}, &so, &se); code != 1 {
		t.Errorf("不正なフラグ exit=%d want 1", code)
	}
	se.Reset()
	if code := dispatch([]string{"init", "-h"}, &so, &se); code != 0 || !strings.Contains(se.String(), "使い方") {
		t.Errorf("init -h: exit=%d stderr=%s", code, se.String())
	}
}
