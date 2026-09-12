package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const figSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"/></svg>`

// md を渡すと一時置き場に <同名>.html を書き、既定ブラウザで開く。図は埋め込まれ、目次が付く。
func TestExplain_WriteAndOpen(t *testing.T) {
	dir, opened := stubAnswer(t)
	src := filepath.Join(t.TempDir(), "kaisetsu.md")
	writeFile(t, src, "\uFEFF"+"# 題名\n\n## 前提\n\n![図1: 仕組み](fig1.svg)\n")
	writeFile(t, filepath.Join(filepath.Dir(src), "fig1.svg"), figSVG)

	var so, se bytes.Buffer
	if code := dispatch([]string{"explain", src}, &so, &se); code != 0 {
		t.Fatalf("exit=%d want 0\nstdout=%s\nstderr=%s", code, so.String(), se.String())
	}
	out := filepath.Join(dir, "kaisetsu.html")
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{"<title>題名</title>", `<h2 id="s1">前提</h2>`, `<a href="#s1">前提</a>`,
		`<figure id="fig-1"`, "<rect width=\"10\" height=\"10\"/>", "<figcaption>図1: 仕組み</figcaption>"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q が無い", want)
		}
	}
	if strings.HasPrefix(got, "\uFEFF") {
		t.Error("BOM が残っている")
	}
	if !strings.Contains(so.String(), "書いた "+out) {
		t.Errorf("書いたパスの報告が無い: %s", so.String())
	}
	if len(*opened) != 1 || (*opened)[0] != out {
		t.Errorf("開いた先が違う: %v", *opened)
	}
}

// 図が見つからないときは HTML を書いて開いたうえで、警告を出して終了コード 1 を返す。
func TestExplain_図が無ければ1(t *testing.T) {
	dir, opened := stubAnswer(t)
	src := filepath.Join(t.TempDir(), "a.md")
	writeFile(t, src, "# 題\n\n![図1: 仕組み](fig1.svg)\n")

	var so, se bytes.Buffer
	if code := dispatch([]string{"explain", src}, &so, &se); code != 1 {
		t.Fatalf("exit=%d want 1\nstdout=%s\nstderr=%s", code, so.String(), se.String())
	}
	if !strings.Contains(se.String(), "図 fig1.svg が無い") {
		t.Errorf("警告が出ていない: %s", se.String())
	}
	raw, err := os.ReadFile(filepath.Join(dir, "a.html"))
	if err != nil {
		t.Fatal("HTML が書かれていない:", err)
	}
	if !strings.Contains(string(raw), "図 fig1.svg が無い") {
		t.Error("本文に印が出ていない")
	}
	if len(*opened) != 1 {
		t.Errorf("開いていない: %v", *opened)
	}
}

// -out で出力先を指定できる。-no-open なら開かない。
func TestExplain_OutNoOpen(t *testing.T) {
	_, opened := stubAnswer(t)
	src := filepath.Join(t.TempDir(), "a.md")
	writeFile(t, src, "# 題\n")
	out := filepath.Join(t.TempDir(), "sub", "b.html")

	var so, se bytes.Buffer
	if code := dispatch([]string{"explain", "-no-open", "-out", out, src}, &so, &se); code != 0 {
		t.Fatalf("exit=%d want 0\n%s", code, se.String())
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("-out に書かれていない: %v", err)
	}
	if len(*opened) != 0 {
		t.Errorf("-no-open なのに開いた: %v", *opened)
	}
}

// 引数が無い・md が読めないときは使い方を出して 1。
func TestExplain_引数の誤り(t *testing.T) {
	stubAnswer(t)
	var so, se bytes.Buffer
	if code := dispatch([]string{"explain"}, &so, &se); code != 1 {
		t.Errorf("引数無し: exit=%d want 1", code)
	}
	se.Reset()
	if code := dispatch([]string{"explain", filepath.Join(t.TempDir(), "無い.md")}, &so, &se); code != 1 {
		t.Errorf("無いファイル: exit=%d want 1", code)
	}
}
