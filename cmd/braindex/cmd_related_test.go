package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pilefort/braindex/internal/links"
)

func relatedFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	cfg := filepath.Join(root, "hub/braindex.json")
	writeFile(t, cfg, `{"root":"..","retro":{"sessions_dir":"sessions"}}`)
	for _, repo := range []string{"alpha", "beta"} {
		writeFile(t, filepath.Join(root, repo, "work/ISSUE-a.md"), "orchard\n")
	}
	writeFile(t, filepath.Join(root, "alpha/docs/notes/a.md"), "# Orchard\n\n結論: fruit\n\n[related](../../../beta/docs/notes/b.md)\n")
	writeFile(t, filepath.Join(root, "beta/docs/notes/b.md"), "# Harvest\n\n結論: fruit\n")
	row := map[string]any{"type": "user", "version": "2.1.258", "cwd": filepath.Join(root, "alpha"), "timestamp": "2026-01-01T00:00:00Z", "message": map[string]string{"content": "harvest"}}
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "hub/sessions/project/session-one.jsonl"), string(b)+"\n")
	runOK(t, options{config: cfg, date: "2026-01-01"})
	return root, cfg
}

func TestRelated_SectionsAndJSON(t *testing.T) {
	root, cfg := relatedFixture(t)
	for _, asJSON := range []bool{false, true} {
		args := []string{"-config", cfg, "-repo", "alpha", "-issue", filepath.Join(root, "alpha/work/ISSUE-a.md")}
		if asJSON {
			args = append(args, "-json")
		}
		var so, se bytes.Buffer
		code := runRelated(args, &so, &se)
		if code != 0 {
			t.Fatalf("code=%d %s", code, &se)
		}
		if !asJSON {
			if !strings.Contains(so.String(), "## このリポ（alpha）") || !strings.Contains(so.String(), "## 他のリポ") {
				t.Fatal(&so)
			}
			continue
		}
		var out relatedOutput
		if err := json.Unmarshal(so.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out.Repo != "alpha" || len(out.ThisRepo) != 1 || len(out.OtherRepos) != 1 || len(out.Warnings) != 0 {
			t.Fatalf("%+v", out)
		}
		weights := map[string]int{}
		for _, term := range out.Terms {
			weights[term.Word] = term.Weight
		}
		if weights["orchard"] != 2 || weights["harvest"] != 1 {
			t.Fatal(weights)
		}
		var shape map[string]json.RawMessage
		json.Unmarshal(so.Bytes(), &shape)
		for _, k := range []string{"repo", "terms", "this_repo", "other_repos", "warnings"} {
			if _, ok := shape[k]; !ok {
				t.Fatal(k)
			}
		}
	}
}

func TestRelated_WarningsAndFailures(t *testing.T) {
	for _, name := range []string{"issue", "sessions", "links", "outside", "config", "flags", "no-terms", "bad-links"} {
		t.Run(name, func(t *testing.T) {
			root, cfg := relatedFixture(t)
			args := []string{"-config", cfg, "-repo", "alpha"}
			want := 2
			needle := ""
			remove := func(p string) {
				t.Helper()
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
			}
			switch name {
			case "issue":
				remove(filepath.Join(root, "alpha/work/ISSUE-a.md"))
				needle = "ISSUE が無い"
			case "sessions":
				writeFile(t, cfg, `{"root":"..","retro":{"sessions_dir":"absent"}}`)
				needle = "セッションの置き場が無い"
			case "links":
				remove(filepath.Join(root, "hub/index/links.tsv"))
				needle = "links.tsv が無い"
			case "outside":
				args = []string{"-config", cfg}
				want = 1
				needle = "-repo"
			case "config":
				args = []string{"-config", filepath.Join(root, "absent.json")}
				want = 1
				needle = "設定ファイルが無い"
			case "flags":
				args = append(args, "-limit", "0")
				want = 1
				needle = "1 以上"
			case "bad-links":
				writeFile(t, filepath.Join(root, "hub/index/links.tsv"), "broken")
				want = 1
				needle = "ヘッダ"
			case "no-terms":
				writeFile(t, filepath.Join(root, "alpha/work/ISSUE-a.md"), "alpha")
				writeFile(t, filepath.Join(root, "hub/sessions/project/session-one.jsonl"), "")
				needle = "材料が無い"
			}
			var so, se bytes.Buffer
			code := runRelated(args, &so, &se)
			if code != want || !strings.Contains(se.String(), needle) {
				t.Fatalf("code=%d want=%d stderr=%s", code, want, &se)
			}
		})
	}
}

func TestRelated_CwdAndExplicitSession(t *testing.T) {
	root, cfg := relatedFixture(t)
	t.Chdir(filepath.Join(root, "alpha"))
	p := filepath.Join(root, "hub/sessions/project/session-one.jsonl")
	old := time.Now().AddDate(0, 0, -30)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	for _, explicit := range []bool{false, true} {
		args := []string{"-config", cfg, "-json"}
		if explicit {
			args = append(args, "-session", "session-one")
		}
		var so, se bytes.Buffer
		if code := runRelated(args, &so, &se); code != 0 {
			t.Fatalf("code=%d %s", code, &se)
		}
		var out relatedOutput
		if err := json.Unmarshal(so.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, term := range out.Terms {
			if term.Word == "harvest" {
				found = true
			}
		}
		if out.Repo != "alpha" || found != explicit {
			t.Fatalf("explicit=%v out=%+v", explicit, out)
		}
	}
}

func TestRelatedTerms_MaxWeightSourcesAndRepoExclusion(t *testing.T) {
	terms := relatedTerms(map[string][]string{"ISSUE": {"orchard orchard alpha https://host.invalid/private"}, "セッション": {"orchard harvest beta"}, "引数": {"harvest"}}, "alpha/beta")
	if len(terms) != 2 {
		t.Fatal(terms)
	}
	byWord := map[string]links.Term{}
	for _, term := range terms {
		byWord[term.Word] = term
	}
	for _, word := range []string{"orchard", "harvest"} {
		if byWord[word].Weight != 2 || len(byWord[word].Sources) != 2 {
			t.Fatal(byWord)
		}
	}
}
