package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/scan/scantest"
)

func TestScan_Worktrees(t *testing.T) {
	for _, prefix := range []string{"", "group/"} {
		t.Run(prefix, func(t *testing.T) {
			root := t.TempDir()
			for _, repo := range []string{"main", "wt", "plain", "other"} {
				for _, rel := range []string{"docs/notes/a.md", "docs/decisions.md", "research/b.md"} {
					p := filepath.Join(root, filepath.FromSlash(prefix+repo+"/"+rel))
					if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(p, []byte("# Note\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := os.Mkdir(filepath.Join(root, filepath.FromSlash(prefix+"main/.git")), 0o755); err != nil {
				t.Fatal(err)
			}
			for repo, content := range map[string]string{"wt": "gitdir: ../main/.git/worktrees/wt\n", "other": "not a gitdir\n"} {
				if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(prefix+repo+"/.git")), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cfg := Config{Root: root}
			if prefix != "" {
				cfg.RepoDepth = 2
			}
			for _, repo := range []string{"main", "wt", "plain", "other"} {
				cfg.Extra = append(cfg.Extra, ExtraRule{Repo: prefix + repo, Path: "research", Kind: "research"})
			}
			res, err := Scan(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Files) != 9 {
				t.Errorf("files=%d want 9", len(res.Files))
			}
			for _, f := range res.Files {
				if f.Repo == prefix+"wt" {
					t.Errorf("worktree が走査された: %s", f.Rel)
				}
			}
			for _, rel := range []string{"docs/notes/a.md", "docs/decisions.md", "research/b.md"} {
				if Covers(cfg, prefix+"wt/"+rel) {
					t.Errorf("worktree を Covers が対象にした: %s", rel)
				}
				if !Covers(cfg, prefix+"main/"+rel) {
					t.Errorf("通常リポが対象外: %s", rel)
				}
			}
			if len(res.Gaps) != 0 {
				t.Errorf("対象外を確認不能にしている: %+v", res.Gaps)
			}
			if len(res.ExcludedWorktrees) != 1 || res.ExcludedWorktrees[0] != prefix+"wt" {
				t.Errorf("対象外の記録: %+v", res.ExcludedWorktrees)
			}
			cfg.IncludeWorktrees = true
			res, err = Scan(cfg)
			if err != nil || len(res.Files) != 12 || len(res.ExcludedWorktrees) != 0 {
				t.Fatalf("include_worktrees: result=%+v err=%v", res, err)
			}
			for _, f := range res.Files {
				if !Covers(cfg, f.Rel) {
					t.Errorf("走査したパスが対象外: %s", f.Rel)
				}
			}
		})
	}
}

func TestWorktreeMarker(t *testing.T) {
	for _, content := range []string{"", "git", " gitdir: x", "Gitdir: x", "prefix gitdir: x"} {
		t.Run(strings.ReplaceAll(content, ":", "_"), func(t *testing.T) {
			root := t.TempDir()
			p := filepath.Join(root, "r", "docs", "notes")
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(p, "a.md"), []byte("# Note\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "r", ".git"), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			res, err := Scan(Config{Root: root})
			if err != nil || len(res.Files) != 1 {
				t.Fatalf("Scan=%+v err=%v", res, err)
			}
		})
	}
}

func TestScan_UnreadableWorktreeMarker(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "wt", "docs", "notes")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "a.md"), []byte("# Note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "wt", ".git")
	if err := os.WriteFile(marker, []byte("gitdir: ../main/.git/worktrees/wt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scantest.MakeUnreadable(t, marker)
	cfg := Config{Root: root, Extra: []ExtraRule{{Repo: "wt", Path: "docs/notes", Kind: "notes"}}}
	res, err := Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 0 || len(res.ExcludedWorktrees) != 0 || len(res.Gaps) != 1 || !res.Gaps[0].Covers("wt/docs/notes/a.md") {
		t.Fatalf("読めない .git はリポ全体を確認不能にする: %+v", res)
	}
	if !Covers(cfg, "wt/docs/notes/a.md") {
		t.Error("読めない .git で対象外と断定した")
	}
	cfg.IncludeWorktrees = true
	res, err = Scan(cfg)
	if err != nil || len(res.Files) != 1 || len(res.Gaps) != 0 {
		t.Fatalf("include: %+v err=%v", res, err)
	}
}
