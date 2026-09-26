package catalog

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/scan"
)

func TestBuild_WorktreesDeterministic(t *testing.T) {
	root := t.TempDir()
	for _, repo := range []string{"main", "wt", "group-wt"} {
		p := filepath.Join(root, repo, "docs", "notes")
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "a.md"), []byte("# Note\n\n記録日: 2026-01-01\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if repo != "main" {
			if err := os.WriteFile(filepath.Join(root, repo, ".git"), []byte("gitdir: ../main/.git/worktrees/wt\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, include := range []bool{false, true} {
		cfg := scan.Config{Root: root, IncludeWorktrees: include}
		a, err := Build(cfg, "2026-09-26")
		if err != nil {
			t.Fatal(err)
		}
		b, err := Build(cfg, "2026-09-26")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a.Catalog, b.Catalog) {
			t.Error("2 回生成でバイト不一致")
		}
		cov, err := ParseCoverage(a.Catalog)
		if err != nil || !reflect.DeepEqual(cov, a.Coverage) {
			t.Fatalf("coverage=%+v want=%+v err=%v", cov, a.Coverage, err)
		}
		if include {
			if a.Entries != 3 || len(cov.ExcludedWorktrees) != 0 {
				t.Fatalf("include: %+v", a)
			}
		} else {
			if a.Entries != 1 || !reflect.DeepEqual(cov.ExcludedWorktrees, []string{"group-wt", "wt"}) {
				t.Fatalf("default: %+v", a)
			}
			for _, repo := range cov.ExcludedWorktrees {
				if !strings.Contains(string(a.Catalog), "- 対象外: "+repo+" — git worktree\n") {
					t.Errorf("リポ名の記録なし: %s", a.Catalog)
				}
			}
		}
	}
}

func TestCoverage_WorktreesAndGaps(t *testing.T) {
	cov := Coverage{Known: true, Gaps: []scan.Gap{{Rel: "main/docs/notes", Dir: true, Reason: "unreadable"}}, ExcludedWorktrees: []string{"group/wt"}}
	md := withCoverage([]byte("# Catalog\n"), cov)
	got, err := ParseCoverage(md)
	if err != nil || !reflect.DeepEqual(got, cov) {
		t.Fatalf("got=%+v want=%+v err=%v", got, cov, err)
	}
}
