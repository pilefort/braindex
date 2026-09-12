package scan_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/pilefort/braindex/internal/catalog"
	"github.com/pilefort/braindex/internal/scan"
)

func TestScan_GlobalExclusionsAndDeterminism(t *testing.T) {
	for _, recursive := range []bool{false, true} {
		for _, base := range []string{"docs/notes", "./docs//notes", "."} {
			cfg := scan.Config{Root: t.TempDir(), Extra: []scan.ExtraRule{
				{Repo: "alpha", Path: base, Recursive: recursive, Exclude: []string{"drafts", "skip.md"}},
				{Repo: "alpha", Path: "docs/notes/drafts", Recursive: true},
				{Repo: "alpha", Path: "extra", Recursive: recursive},
			}}
			paths := map[string]bool{
				"alpha/docs/notes/drafts/p.md":       !recursive,
				"alpha/docs/notes/skip.md":           !recursive && base == ".",
				"alpha/docs/notes/keep.md":           true,
				"alpha/docs/notes/.drafts/x.md":      false,
				"alpha/docs/notes/.hidden.md":        false,
				"alpha/extra/.hidden.md":             false,
				"alpha/extra/.drafts/x.md":           false,
				"alpha/extra/keep.md":                true,
				"beta/docs/notes/drafts/p.md":        true,
				"alpha/docs/notes-other/drafts/p.md": false,
			}
			for p := range paths {
				abs := filepath.Join(cfg.Root, filepath.FromSlash(p))
				if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(abs, []byte("# Note\n"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			res, err := scan.Scan(cfg)
			if err != nil {
				t.Fatal(err)
			}
			found := map[string]bool{}
			for _, f := range res.Files {
				found[f.Rel] = true
			}
			for p, want := range paths {
				if found[p] != want {
					t.Errorf("recursive=%v base=%s Scan(%s)=%v want %v", recursive, base, p, found[p], want)
				}
				if got := scan.Covers(cfg, p); got != want {
					t.Errorf("recursive=%v base=%s Covers(%s)=%v want %v", recursive, base, p, got, want)
				}
			}
			a, err := catalog.Build(cfg, "2026-09-13")
			if err != nil {
				t.Fatal(err)
			}
			b, err := catalog.Build(cfg, "2026-09-13")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(a.Catalog, b.Catalog) {
				t.Fatal("2 回生成でバイト不一致")
			}
		}
	}
}

func TestScan_ExtraScopeAndExplicitDotOrigins(t *testing.T) {
	for _, recursive := range []bool{false, true} {
		cfg := scan.Config{Root: t.TempDir(), NotesDirs: []string{"docs/notes", ".config/.notes"}, Extra: []scan.ExtraRule{
			{Repo: "alpha", Path: ".", Recursive: false, Exclude: []string{"README.md", "CLAUDE.md"}},
			{Repo: "alpha", Path: "research", Recursive: true},
			{Repo: "alpha", Path: ".github/docs", Recursive: recursive},
			{Repo: "alpha", Path: ".extra", Recursive: recursive},
			{Repo: "alpha", Path: "docs/notes", Recursive: true, Exclude: []string{"drafts"}},
		}}
		paths := map[string]bool{
			"alpha/README.md":                   false,
			"alpha/CLAUDE.md":                   false,
			"alpha/research/x/README.md":        true,
			"alpha/docs/notes/deep/drafts/p.md": false,
			"alpha/docs/notes/deep/keep.md":     true,
			"alpha/.github/docs/a.md":           true,
			"alpha/.github/docs/sub/c.md":       recursive,
			"alpha/.github/docs/.drafts/b.md":   false,
			"alpha/.github/docs/.hidden.md":     false,
			"alpha/.extra/a.md":                 true,
			"alpha/.extra/.drafts/b.md":         false,
			"alpha/.config/.notes/a.md":         true,
			"alpha/.config/.notes/sub/c.md":     true,
			"alpha/.config/.notes/.drafts/b.md": false,
			"alpha/.config/.notes/.hidden.md":   false,
		}
		for p := range paths {
			abs := filepath.Join(cfg.Root, filepath.FromSlash(p))
			if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(abs, []byte("# Note\n"), 0644); err != nil {
				t.Fatal(err)
			}
		}
		res, err := scan.Scan(cfg)
		if err != nil {
			t.Fatal(err)
		}
		found := map[string]bool{}
		for _, f := range res.Files {
			found[f.Rel] = true
		}
		for p, want := range paths {
			if found[p] != want {
				t.Errorf("recursive=%v Scan(%s)=%v want %v", recursive, p, found[p], want)
			}
			if got := scan.Covers(cfg, p); got != want {
				t.Errorf("recursive=%v Covers(%s)=%v want %v", recursive, p, got, want)
			}
		}
		a, err := catalog.Build(cfg, "2026-09-13")
		if err != nil {
			t.Fatal(err)
		}
		b, err := catalog.Build(cfg, "2026-09-13")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a.Catalog, b.Catalog) {
			t.Fatal("2 回生成でバイト不一致")
		}
	}
}
