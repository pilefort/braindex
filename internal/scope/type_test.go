package scope

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/render"
)

func TestBuildType(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{"a.md": "# A\n種別: 手順\n", "b.md": "# B\n種別: 失敗\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	catalog := render.Render([]render.Entry{{Repo: "r", Path: "a.md", Title: "A"}, {Repo: "r", Path: "b.md", Title: "B"}, {Repo: "r", Path: "missing.md", Title: "C"}}, "2026-09-13")
	r, err := Build(catalog, Options{Type: "howto", Root: root})
	if err != nil || r.Entries != 1 || r.Chunks[0][0].Type != "手順" || len(r.Warnings) != 1 || !strings.Contains(string(Render(r)), "［手順］") {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = Build(catalog, Options{Type: "none", Root: root})
	if err != nil || r.Entries != 1 || r.Chunks[0][0].Path != "missing.md" {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = Build(nil, Options{Type: "手順", Dir: root})
	if err != nil || r.Entries != 1 || r.Chunks[0][0].Title != "A" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := Build(catalog, Options{Type: "手順"}); err == nil {
		t.Fatal("root が無い条件を受理")
	}
	r, err = Build(catalog, Options{})
	if err != nil || r.Entries != 3 || len(r.Warnings) != 0 || strings.Contains(string(Render(r)), "［") {
		t.Fatalf("従来の出力: %+v %v", r, err)
	}
}
