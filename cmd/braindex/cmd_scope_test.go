package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func scopeCatalog(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "internal", "scope", "testdata", "catalog.md"))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "catalog.md")
	writeFile(t, p, string(b))
	return p
}

// -catalog と -topic で走査対象を chunk に分けて出す。
func TestScope_Topic(t *testing.T) {
	var so, se bytes.Buffer
	if code := dispatch([]string{"scope", "-catalog", scopeCatalog(t), "-topic", "長さ"}, &so, &se); code != 0 {
		t.Fatalf("exit=%d\n%s%s", code, so.String(), se.String())
	}
	s := so.String()
	if !strings.HasPrefix(s, "# braindex scope: topic:長さ\n対象 2 件 / 1 chunk\n") || !strings.Contains(s, "repo-b/docs/notes/common/measure.md") || strings.Contains(s, "heading.md") {
		t.Errorf("出力が違う:\n%s", s)
	}
}

// 2 件未満は一覧を出したうえで stderr に理由を書き、終了コード 2。
func TestScope_TooFew(t *testing.T) {
	var so, se bytes.Buffer
	if code := dispatch([]string{"scope", "-catalog", scopeCatalog(t), "-topic", "見出し"}, &so, &se); code != 2 {
		t.Fatalf("exit=%d want 2\n%s%s", code, so.String(), se.String())
	}
	if !strings.Contains(so.String(), "対象 1 件") || !strings.Contains(se.String(), "突き合わせられない") {
		t.Errorf("stdout=%s stderr=%s", so.String(), se.String())
	}
}

// -dir は索引を使わない。-json は機械可読。
func TestScope_DirJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "# 甲\n\n記録日: 2026-08-01\n")
	writeFile(t, filepath.Join(dir, "b.md"), "# 乙\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"scope", "-dir", dir, "-json", "-size", "1"}, &so, &se); code != 0 {
		t.Fatalf("exit=%d\n%s%s", code, so.String(), se.String())
	}
	var got struct {
		Mode   string `json:"mode"`
		N      int    `json:"n_entries"`
		Chunks [][]struct {
			Title string `json:"title"`
			Path  string `json:"path"`
			Date  string `json:"date"`
		} `json:"chunks"`
	}
	if err := json.Unmarshal(so.Bytes(), &got); err != nil {
		t.Fatalf("JSON でない: %v\n%s", err, so.String())
	}
	wantPath := filepath.ToSlash(filepath.Join(dir, "a.md")) // 渡したディレクトリと結合した形＝そのまま開ける
	if !strings.HasPrefix(got.Mode, "dir:") || got.N != 2 || len(got.Chunks) != 2 || got.Chunks[0][0].Title != "甲" || got.Chunks[0][0].Path != wantPath || got.Chunks[0][0].Date != "2026-08-01" {
		t.Errorf("内容が違う: %s", so.String())
	}
	if _, err := os.Stat(filepath.FromSlash(got.Chunks[0][0].Path)); err != nil {
		t.Errorf("出力のパスが開けない: %v", err)
	}
}

// -dir にファイル(ディレクトリでないパス)を渡すと、次に何を渡せばよいかが分かる文で終了コード 1。
func TestScope_DirFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.md")
	writeFile(t, file, "# 甲\n\n記録日: 2026-08-01\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"scope", "-dir", file}, &so, &se); code != 1 {
		t.Fatalf("exit=%d want 1\n%s%s", code, so.String(), se.String())
	}
	if !strings.Contains(se.String(), "ディレクトリでない") || !strings.Contains(se.String(), "ディレクトリを渡す") {
		t.Errorf("次に何を渡せばよいかの案内が無い: %s", se.String())
	}
}

// -dir の列挙は索引と同じ走査規則: archive セグメントとドットで始まるディレクトリは対象外。
func TestScope_Dirは索引と同じ走査規則で除外する(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "# 甲\n\n記録日: 2026-08-01\n")
	writeFile(t, filepath.Join(dir, "b.md"), "# 乙\n\n記録日: 2026-08-02\n")
	writeFile(t, filepath.Join(dir, "archive", "old.md"), "# 退避\n\n記録日: 2026-07-01\n")
	writeFile(t, filepath.Join(dir, ".hidden", "h.md"), "# 隠し\n\n記録日: 2026-07-02\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"scope", "-dir", dir}, &so, &se); code != 0 {
		t.Fatalf("exit=%d\n%s%s", code, so.String(), se.String())
	}
	out := so.String()
	if !strings.Contains(out, "対象 2 件") {
		t.Errorf("件数が違う: %s", out)
	}
	if strings.Contains(out, "退避") || strings.Contains(out, "隠し") {
		t.Errorf("除外されるはずのノートが出ている: %s", out)
	}
}

// 索引の既定は設定ファイルと同じディレクトリの index/catalog.md。無ければ作り方を添えて 1。
func TestScope_DefaultCatalog(t *testing.T) {
	hub := t.TempDir()
	writeFile(t, filepath.Join(hub, "braindex.json"), `{"root": ".."}`)
	b, _ := os.ReadFile(filepath.Join("..", "..", "internal", "scope", "testdata", "catalog.md"))
	writeFile(t, filepath.Join(hub, "index", "catalog.md"), string(b))
	var so, se bytes.Buffer
	if code := dispatch([]string{"scope", "-config", filepath.Join(hub, "braindex.json"), "-full"}, &so, &se); code != 0 {
		t.Fatalf("exit=%d\n%s%s", code, so.String(), se.String())
	}
	if !strings.Contains(so.String(), "対象 3 件") {
		t.Errorf("出力が違う: %s", so.String())
	}
	os.Remove(filepath.Join(hub, "index", "catalog.md"))
	se.Reset()
	if code := dispatch([]string{"scope", "-config", filepath.Join(hub, "braindex.json"), "-full"}, &so, &se); code != 1 || !strings.Contains(se.String(), "索引を読めない") {
		t.Errorf("索引なし: exit=%d stderr=%s", code, se.String())
	}
}

// scope は root を使わないので、設定に root が無くても・設定ファイルが無くても既定の索引を読みに行く。
// root を要求すると、案内される -root を scope が受け付けないため利用者が行き止まりになる。
func TestScope_DefaultCatalog_RootUnset(t *testing.T) {
	hub := t.TempDir()
	writeFile(t, filepath.Join(hub, "braindex.json"), `{"notes_dirs": ["docs/notes"]}`)
	b, _ := os.ReadFile(filepath.Join("..", "..", "internal", "scope", "testdata", "catalog.md"))
	writeFile(t, filepath.Join(hub, "index", "catalog.md"), string(b))
	var so, se bytes.Buffer
	if code := dispatch([]string{"scope", "-config", filepath.Join(hub, "braindex.json"), "-full"}, &so, &se); code != 0 {
		t.Fatalf("exit=%d\n%s%s", code, so.String(), se.String())
	}
	if !strings.Contains(so.String(), "対象 3 件") {
		t.Errorf("出力が違う: %s", so.String())
	}

	// 設定ファイルが無い場所では、索引の作り方を案内する(-root ではない)
	t.Chdir(t.TempDir())
	so.Reset()
	se.Reset()
	if code := dispatch([]string{"scope", "-full"}, &so, &se); code != 1 {
		t.Fatalf("設定なし: exit=%d\n%s%s", code, so.String(), se.String())
	}
	if !strings.Contains(se.String(), "索引を読めない") || strings.Contains(se.String(), "-root") {
		t.Errorf("設定なしの案内が違う(scope は -root を受け付けない): %s", se.String())
	}
}

// フラグの誤り: モード未指定・2 つ指定・-size 0・位置引数は 1。-h は使い方を出して 0。
func TestScope_BadArgs(t *testing.T) {
	c := scopeCatalog(t)
	for _, args := range [][]string{
		{"scope", "-catalog", c},
		{"scope", "-catalog", c, "-topic", "x", "-full"},
		{"scope", "-catalog", c, "-full", "-size", "0"},
		{"scope", "-catalog", c, "-full", "extra"},
	} {
		var so, se bytes.Buffer
		if code := dispatch(args, &so, &se); code != 1 || se.Len() == 0 {
			t.Errorf("%v: exit=%d stderr=%s", args, code, se.String())
		}
	}
	var so, se bytes.Buffer
	if code := dispatch([]string{"scope", "-h"}, &so, &se); code != 0 || !strings.Contains(se.String(), "使い方") {
		t.Errorf("-h: exit=%d stderr=%s", code, se.String())
	}
}
