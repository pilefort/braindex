package notetype_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/pilefort/braindex/internal/catalog"
	"github.com/pilefort/braindex/internal/render"
	"github.com/pilefort/braindex/internal/scan"
)

func TestCatalogTypeGoldenDeterministic(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "repo-a/docs/notes/note.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("testdata/note.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, fixture, 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := catalog.Build(scan.Config{Root: root}, "2026-09-13")
	if err != nil {
		t.Fatal(err)
	}
	b, err := catalog.Build(scan.Config{Root: root}, "2026-09-13")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Catalog, b.Catalog) {
		t.Fatal("同じ入力の索引が一致しない")
	}
	golden, err := os.ReadFile("../catalog/testdata/type.golden.md")
	if err != nil {
		t.Fatal(err)
	}
	if got := render.Render(a.Records, "2026-09-13"); !bytes.Equal(got, golden) {
		t.Fatalf("5 列の golden と不一致:\n%s", got)
	}
	if err := os.WriteFile(p, bytes.ReplaceAll(fixture, []byte("種別: 失敗\n"), nil), 0o644); err != nil {
		t.Fatal(err)
	}
	old, err := catalog.Build(scan.Config{Root: root}, "2026-09-13")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Catalog, old.Catalog) {
		t.Fatal("種別行の追加で索引が変わった")
	}
}
