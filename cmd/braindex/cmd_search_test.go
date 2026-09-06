package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/scan/scantest"
)

// searchRoot は repo-a(notes 2 本)と repo-b(decisions)を持つ root を作る。
func searchRoot(t *testing.T) string {
	t.Helper()
	root := makeRoot(t) // repo-a/docs/notes/a.md(結論: a)
	writeFile(t, filepath.Join(root, "repo-a", "docs", "notes", "b.md"), "# B\n\n前置き。\n\n本文の後半に決定性の話がある。\n")
	writeFile(t, filepath.Join(root, "repo-b", "docs", "decisions.md"), "# 決定\n\n## 決定性を守る\n\n理由: 決定性が要る。\n")
	return root
}

// -root と語だけで動き、「パス:行: 内容」をパス→行の順で出す。一致ありで確認できなかった範囲が無ければ終了コード 0。
func TestSearch_Basic(t *testing.T) {
	var so, se bytes.Buffer
	if code := dispatch([]string{"search", "-root", searchRoot(t), "決定性"}, &so, &se); code != 0 {
		t.Fatalf("exit=%d\n%s%s", code, so.String(), se.String())
	}
	want := "# braindex search: 決定性（語を含む行・大小無視）\n" +
		"対象 3 ファイル・一致 3 行（2 ファイル）\n" +
		"repo-a/docs/notes/b.md:5: 本文の後半に決定性の話がある。\n" +
		"repo-b/docs/decisions.md:3: ## 決定性を守る\n" +
		"repo-b/docs/decisions.md:5: 理由: 決定性が要る。\n"
	if so.String() != want || se.Len() != 0 {
		t.Errorf("出力が違う:\n--- got ---\n%s--- stderr ---\n%s", so.String(), se.String())
	}
}

// 一致なしでも走査した範囲を全部確認できていれば 0 で、その旨を出す。
func TestSearch_NoMatchComplete(t *testing.T) {
	var so, se bytes.Buffer
	if code := dispatch([]string{"search", "-root", searchRoot(t), "無い語"}, &so, &se); code != 0 {
		t.Fatalf("exit=%d\n%s%s", code, so.String(), se.String())
	}
	if !strings.Contains(so.String(), "対象 3 ファイル・一致なし（走査した範囲は全部確認できた）") {
		t.Errorf("出力が違う:\n%s", so.String())
	}
}

// 読めないファイルがあれば「確認できなかった範囲」を結果に出し、警告つきの終了コード 2。一致なしは「無いとは言えない」と書く。
func TestSearch_Unreadable(t *testing.T) {
	root := searchRoot(t)
	scantest.MakeUnreadable(t, filepath.Join(root, "repo-a", "docs", "notes", "b.md"))
	var so, se bytes.Buffer
	if code := dispatch([]string{"search", "-root", root, "後半"}, &so, &se); code != 2 {
		t.Fatalf("exit=%d want 2\n%s%s", code, so.String(), se.String())
	}
	s := so.String()
	if !strings.Contains(s, "一致なし（ただし確認できなかった範囲がある。無いとは言えない）") ||
		!strings.Contains(s, "確認できなかった範囲 1 件（この中に一致があるかは分からない）\n- repo-a/docs/notes/b.md — ") {
		t.Errorf("stdout が違う:\n%s", s)
	}
	if !strings.Contains(se.String(), "braindex search: 警告: repo-a/docs/notes/b.md: ") || !strings.Contains(se.String(), "警告 1 件(終了コード 2)") {
		t.Errorf("stderr が違う:\n%s", se.String())
	}
}

// -json は terms・files・total・hits・gaps・complete を持つ。gaps のキーは小文字。
func TestSearch_JSON(t *testing.T) {
	root := searchRoot(t)
	locked := filepath.Join(root, "repo-b", "docs", "notes", "locked")
	writeFile(t, filepath.Join(locked, "x.md"), "決定性\n")
	scantest.MakeUnreadable(t, locked)
	var so, se bytes.Buffer
	if code := dispatch([]string{"search", "-root", root, "-json", "-repo", "repo-b", "-any", "決定性", "理由"}, &so, &se); code != 2 {
		t.Fatalf("exit=%d want 2\n%s%s", code, so.String(), se.String())
	}
	var got struct {
		Terms    []string `json:"terms"`
		Any      bool     `json:"any"`
		Repo     string   `json:"repo"`
		Files    int      `json:"files"`
		Total    int      `json:"total"`
		Complete bool     `json:"complete"`
		Hits     []struct {
			Path  string   `json:"path"`
			Line  int      `json:"line"`
			Col   int      `json:"col"`
			Text  string   `json:"text"`
			Terms []string `json:"terms"`
		} `json:"hits"`
		Gaps []struct {
			Rel    string `json:"rel"`
			Dir    bool   `json:"dir"`
			Reason string `json:"reason"`
		} `json:"gaps"`
	}
	if err := json.Unmarshal(so.Bytes(), &got); err != nil {
		t.Fatalf("JSON でない: %v\n%s", err, so.String())
	}
	if len(got.Terms) != 2 || !got.Any || got.Repo != "repo-b" || got.Files != 1 || got.Total != 2 || got.Complete {
		t.Errorf("条件・件数が違う: %s", so.String())
	}
	if len(got.Hits) != 2 || got.Hits[1].Path != "repo-b/docs/decisions.md" || got.Hits[1].Line != 5 || got.Hits[1].Col != 1 ||
		got.Hits[1].Text != "理由: 決定性が要る。" || strings.Join(got.Hits[1].Terms, ",") != "決定性,理由" {
		t.Errorf("hits が違う: %s", so.String())
	}
	if len(got.Gaps) != 1 || got.Gaps[0].Rel != "repo-b/docs/notes/locked" || !got.Gaps[0].Dir || got.Gaps[0].Reason == "" {
		t.Errorf("gaps が違う: %s", so.String())
	}
}

// 設定ファイルの root は設定ファイルのディレクトリ基準(索引生成と同じ解決)。
func TestSearch_ConfigRoot(t *testing.T) {
	root := searchRoot(t)
	cfg := filepath.Join(root, "hub", "braindex.json")
	writeFile(t, cfg, `{"root": ".."}`)
	var so, se bytes.Buffer
	if code := dispatch([]string{"search", "-config", cfg, "-kind", "decisions", "決定性"}, &so, &se); code != 0 {
		t.Fatalf("exit=%d\n%s%s", code, so.String(), se.String())
	}
	if !strings.Contains(so.String(), "対象 1 ファイル（種別 decisions）・一致 2 行（1 ファイル）\n") || strings.Contains(so.String(), "b.md") {
		t.Errorf("出力が違う:\n%s", so.String())
	}
}

// 語が無い・root が無い・不明なフラグは 1。-h は 0。
func TestSearch_Errors(t *testing.T) {
	root := searchRoot(t)
	for _, args := range [][]string{
		{"search", "-root", root},
		{"search", "-root", root, ""},
		{"search", "-root", root, "-limit", "-1", "x"},
		{"search", "-root", root, "-nope", "x"},
		{"search", "-root", filepath.Join(root, "none"), "x"},
	} {
		var so, se bytes.Buffer
		if code := dispatch(args, &so, &se); code != 1 {
			t.Errorf("%v: exit=%d want 1\n%s%s", args, code, so.String(), se.String())
		}
	}
	// 設定ファイルも -root も無い
	wd, _ := os.Getwd()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	var so, se bytes.Buffer
	if code := dispatch([]string{"search", "x"}, &so, &se); code != 1 || !strings.Contains(se.String(), "root が未指定") {
		t.Errorf("exit=%d stderr=%s", code, se.String())
	}
	so.Reset()
	se.Reset()
	if code := dispatch([]string{"search", "-h"}, &so, &se); code != 0 || !strings.Contains(se.String(), "使い方: braindex search") {
		t.Errorf("-h: exit=%d stderr=%s", code, se.String())
	}
}

// -limit は先頭 N 行だけ出し、見出しに全数を書く。
func TestSearch_Limit(t *testing.T) {
	var so, se bytes.Buffer
	if code := dispatch([]string{"search", "-root", searchRoot(t), "-limit", "1", "決定性"}, &so, &se); code != 0 {
		t.Fatalf("exit=%d\n%s%s", code, so.String(), se.String())
	}
	if !strings.Contains(so.String(), "一致 3 行（先頭 1 行だけ出す）\nrepo-a/docs/notes/b.md:5: ") || strings.Contains(so.String(), "decisions.md") {
		t.Errorf("出力が違う:\n%s", so.String())
	}
}
