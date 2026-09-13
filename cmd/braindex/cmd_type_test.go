package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/catalog"
	"github.com/pilefort/braindex/internal/notetype"
	"github.com/pilefort/braindex/internal/scan"
)

func typeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for path, body := range map[string]string{
		"r/docs/notes/a.md":   "# 起動の失敗\n\n本文\n",
		"r/docs/notes/b.md":   "# 調査と失敗\n\n本文\n",
		"r/docs/notes/c.md":   "# 雑記\n\n本文\n",
		"r/docs/notes/d.md":   "# 導入の手順\n種別: 手順\n本文\n",
		"r/docs/decisions.md": "# 失敗の記録\n\n本文\n",
	} {
		writeFile(t, filepath.Join(root, filepath.FromSlash(path)), body)
	}
	return root
}

func TestTypeSuggest(t *testing.T) {
	root := typeFixture(t)
	var so, se bytes.Buffer
	code := dispatch([]string{"type", "suggest", "-root", root, "-json"}, &so, &se)
	var got []notetype.Suggestion
	if err := json.Unmarshal(so.Bytes(), &got); err != nil {
		t.Fatal(err, so.String(), se.String())
	}
	if code != 0 || len(got) != 2 || got[0].Path != "r/docs/notes/a.md" || got[0].Candidate != "失敗" || got[1].Candidate != "" || len(got[1].Axes) != 2 {
		t.Fatalf("%d %+v %s", code, got, se.String())
	}
	so.Reset()
	se.Reset()
	out := filepath.Join(t.TempDir(), "candidates.md")
	if code := dispatch([]string{"type", "suggest", "-root", root, "-out", out}, &so, &se); code != 0 {
		t.Fatal(code, se.String())
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "候補なし 1 件") || !strings.Contains(string(b), "- [ ] 失敗 | r/docs/notes/a.md") {
		t.Fatal(string(b))
	}
	if code := dispatch([]string{"type", "suggest", "-root", root, "-out", out}, &so, &se); code != 1 {
		t.Fatal("候補を上書き", code)
	}
	after, _ := os.ReadFile(out)
	if !bytes.Equal(b, after) {
		t.Fatal("候補が変わった")
	}
}

func TestTypeApply(t *testing.T) {
	root := typeFixture(t)
	p := filepath.Join(root, "r/docs/notes/a.md")
	before := "\ufeff# 起動の失敗\r\n記録日: 2026-09-13\r\n本文\r\n"
	writeFile(t, p, before)
	list := filepath.Join(t.TempDir(), "choices.md")
	writeFile(t, list, "- [X] failure | r/docs/notes/a.md | 題\n- [ ] 観測 | r/docs/notes/b.md | 題\n- [x] 未定（失敗・観測） | r/docs/notes/c.md | 題\n- [x] 観測 | r/docs/notes/d.md | 題\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"type", "apply", list, "-root", root, "-dry-run"}, &so, &se); code != 2 {
		t.Fatal(code, se.String())
	}
	dry := so.String()
	b, _ := os.ReadFile(p)
	if string(b) != before {
		t.Fatal("dry-run で本文が変わった")
	}
	so.Reset()
	se.Reset()
	if code := dispatch([]string{"type", "apply", list, "-root", root}, &so, &se); code != 2 {
		t.Fatal(code, se.String())
	}
	if so.String() != dry {
		t.Fatalf("dry-run と結果が違う: %s / %s", dry, so.String())
	}
	b, _ = os.ReadFile(p)
	if string(b) != "\ufeff# 起動の失敗\r\n記録日: 2026-09-13\r\n種別: 失敗\r\n本文\r\n" {
		t.Fatalf("本文: %q", b)
	}
	for _, name := range []string{"b.md", "c.md"} {
		b, _ := os.ReadFile(filepath.Join(root, "r/docs/notes", name))
		if strings.Contains(string(b), "種別:") {
			t.Fatal("未承認を変更", name)
		}
	}
	b, _ = os.ReadFile(filepath.Join(root, "r/docs/notes/d.md"))
	if !strings.Contains(string(b), "種別: 手順") {
		t.Fatal("既存値を変更")
	}
	so.Reset()
	se.Reset()
	dispatch([]string{"type", "apply", list, "-root", root}, &so, &se)
	if !strings.Contains(so.String(), "そのまま: r/docs/notes/a.md") {
		t.Fatal(so.String())
	}
}

func TestTypeApplyRejectsUnsafePaths(t *testing.T) {
	root := typeFixture(t)
	for _, p := range []string{"../outside.md", "/outside.md", "C:/outside.md", "r/docs/notes/a.md:stream.md"} {
		if _, err := typeNotePath(root, p); err == nil {
			t.Fatalf("不正なパスを受理: %s", p)
		}
	}
	list := filepath.Join(t.TempDir(), "choices.md")
	writeFile(t, list, "- [x] 失敗 | ../outside.md | 題\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"type", "apply", list, "-root", root}, &so, &se); code != 2 || !strings.Contains(se.String(), "root 相対") {
		t.Fatal(code, se.String())
	}
}

func TestSearchTypeCLI(t *testing.T) {
	root := typeFixture(t)
	writeFile(t, filepath.Join(root, "r/docs/notes/a.md"), "# 題\n種別: 失敗\n本文\n")
	for _, tt := range []struct {
		typ         string
		files, code int
		normalized  string
	}{{"failure", 1, 0, "失敗"}, {"none", 3, 0, "未記入"}, {"決定", 0, 1, ""}} {
		var so, se bytes.Buffer
		code := dispatch([]string{"search", "-root", root, "-type", tt.typ, "-json", "本文"}, &so, &se)
		if code != tt.code {
			t.Fatal(tt.typ, code, se.String())
		}
		if code == 1 {
			continue
		}
		var got searchOut
		if err := json.Unmarshal(so.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Type != tt.normalized || got.Files != tt.files || len(got.Hits) != tt.files {
			t.Fatalf("%+v", got)
		}
		if tt.typ == "failure" && got.Hits[0].Type != "失敗" {
			t.Fatal(got.Hits)
		}
	}
	writeFile(t, filepath.Join(root, "r/docs/notes/b.md"), "種別: invalid\n本文\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"search", "-root", root, "-type", "none", "本文"}, &so, &se); code != 2 || !strings.Contains(se.String(), "種別の値が不正") || !strings.Contains(so.String(), "b.md:2") {
		t.Fatal(code, so.String(), se.String())
	}
	so.Reset()
	se.Reset()
	dispatch([]string{"search", "-root", root, "-type", "failure", "本文"}, &so, &se)
	if !strings.Contains(so.String(), "内容の種別 失敗") {
		t.Fatal(so.String())
	}
}

func TestScopeTypeCLI(t *testing.T) {
	root := typeFixture(t)
	writeFile(t, filepath.Join(root, "r/docs/notes/a.md"), "# 題\n種別: 手順\n本文\n")
	r, err := catalog.Build(scan.Config{Root: root}, "2026-09-13")
	if err != nil {
		t.Fatal(err)
	}
	hub := t.TempDir()
	cat := filepath.Join(hub, "index/catalog.md")
	writeFile(t, cat, string(r.Catalog))
	cfg := filepath.Join(hub, "braindex.json")
	data, _ := json.Marshal(map[string]string{"root": root})
	writeFile(t, cfg, string(data))
	for _, args := range [][]string{{"scope", "-full", "-type", "手順", "-config", cfg}, {"scope", "-full", "-type", "howto", "-catalog", cat, "-root", root}, {"scope", "-dir", filepath.Join(root, "r/docs/notes"), "-type", "手順"}} {
		var so, se bytes.Buffer
		if code := dispatch(args, &so, &se); code != 0 || !strings.Contains(so.String(), "対象 2 件") || !strings.Contains(so.String(), "［手順］") || strings.Contains(so.String(), "b.md") {
			t.Fatal(code, so.String(), se.String())
		}
	}
	writeFile(t, cfg, "{}")
	var so, se bytes.Buffer
	if code := dispatch([]string{"scope", "-full", "-type", "手順", "-config", cfg}, &so, &se); code != 1 || !strings.Contains(se.String(), "-type には root が要る") {
		t.Fatal(code, se.String())
	}
	so.Reset()
	se.Reset()
	if code := dispatch([]string{"scope", "-full", "-config", cfg}, &so, &se); code != 0 || strings.Contains(so.String(), "［") {
		t.Fatal(code, so.String(), se.String())
	}
	so.Reset()
	se.Reset()
	if code := dispatch([]string{"scope", "-type", "手順", "-config", cfg}, &so, &se); code != 1 {
		t.Fatal("単独指定を受理", code)
	}
}
