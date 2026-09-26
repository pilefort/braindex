package diagnose

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/catalog"
	"github.com/pilefort/braindex/internal/scan"
)

func TestBuild_WorktreeOutOfScope(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "wt", "docs", "notes", "a.md"), "# Note\n")
	writeFile(t, filepath.Join(root, "wt", ".git"), "gitdir: ../main/.git/worktrees/wt\n")
	for _, savedIncludes := range []bool{true, false} {
		cfg := scan.Config{Root: root, IncludeWorktrees: savedIncludes, Extra: []scan.ExtraRule{{Repo: "wt", Path: ".", Recursive: true, Kind: "root"}}}
		previous, err := catalog.Build(cfg, "2026-09-25")
		if err != nil {
			t.Fatal(err)
		}
		saved := filepath.Join(t.TempDir(), "catalog.md")
		writeFile(t, saved, string(previous.Catalog))
		cfg.IncludeWorktrees = false
		r, err := Build(Input{Cfg: cfg, CatalogPath: saved, Date: "2026-09-26", Path: "wt/docs/notes/a.md"})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Scan.Repos) != 1 || r.Scan.Repos[0].Excluded != "git worktree" || len(r.Scan.Repos[0].Places) != 0 {
			t.Fatalf("repos=%+v", r.Scan.Repos)
		}
		if r.Path.Covered || r.Path.Rule != "git worktree" {
			t.Errorf("path=%+v", r.Path)
		}
		if r.Config.Extra[0].Status != "worktree" {
			t.Errorf("extra=%+v", r.Config.Extra)
		}
		if savedIncludes && (len(r.Saved.Diff.OutOfScope) != 1 || len(r.Saved.Diff.Gone) != 0 || r.Saved.Diff.OutOfScope[0].Reason != "git worktree") {
			t.Errorf("diff=%+v", r.Saved.Diff)
		}
		if !strings.Contains(string(Render(r)), "wt: 対象外（git worktree）") {
			t.Errorf("対象外の表示なし: %s", Render(r))
		}
		j, err := JSON(r)
		if err != nil || !strings.Contains(string(j), `"excluded": "git worktree"`) {
			t.Errorf("JSON=%s err=%v", j, err)
		}
	}
}
