package news

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommandHelper(t *testing.T) {
	mode := os.Getenv("BRAINDEX_COMMAND_TEST")
	if mode == "" {
		return
	}
	switch mode {
	case "exit":
		fmt.Fprintln(os.Stderr, "first error\nsecond error")
		os.Exit(1)
	case "timeout":
		fmt.Fprintln(os.Stderr, "waiting")
		time.Sleep(30 * time.Second)
		os.Exit(1)
	case "broken":
		fmt.Fprintln(os.Stderr, "broken response")
		fmt.Print("not json")
		os.Exit(0)
	}
	var req struct {
		Version int              `json:"version"`
		Terms   []string         `json:"terms"`
		Items   []annotationItem `json:"items"`
	}
	if json.NewDecoder(os.Stdin).Decode(&req) != nil || req.Version != 1 {
		os.Exit(3)
	}
	rows := []map[string]any{}
	for _, it := range req.Items {
		if it.Feed == "" || len([]rune(it.Summary)) > 300 {
			os.Exit(4)
		}
		rows = append(rows, map[string]any{"id": it.ID, "r": 2, "tag": `<b>&"x"</b>[tag]` + strings.Repeat("x", 20), "t": "must ignore", "s": "must ignore", "nt": true})
	}
	if mode == "invalid" {
		rows = append(rows, map[string]any{"id": "unasked", "r": 2}, map[string]any{"id": req.Items[0].ID, "r": 4}, map[string]any{"id": req.Items[0].ID, "r": "3"})
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"items": rows})
	os.Exit(0)
}
func testCommand(t *testing.T, mode string) Command {
	t.Helper()
	t.Setenv("BRAINDEX_COMMAND_TEST", mode)
	return Command{Argv: []string{os.Args[0], "-test.run=^TestCommandHelper$"}, Timeout: 60 * time.Second}
}
func TestCommandAnnotationsAndCache(t *testing.T) {
	for _, mode := range []string{"ok", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			c := testCommand(t, mode)
			results := llmResults()
			results[0].New[0].Summary = strings.Repeat("あ", 310)
			cache := Annotations{}
			rep := Annotate(context.Background(), c, results, cache, AnnotateOptions{Command: true, Pool: 2, Batch: 2, Terms: []string{"ai"}})
			if rep.Annotated != 3 || rep.Retried != 0 || (mode == "ok" && rep.Failed != 0) || (mode == "invalid" && rep.Failed == 0) {
				t.Fatalf("report=%+v", rep)
			}
			if _, ok := cache["unasked"]; ok {
				t.Fatal("unknown id cached")
			}
			for _, a := range cache {
				if a.Title != "" || a.Summary != "" || a.NoTitle || !a.External || len([]rune(a.Tag)) != 24 || *a.Score != 2 {
					t.Fatalf("annotation=%+v", a)
				}
			}
			path := filepath.Join(t.TempDir(), "cache.json")
			if err := cache.Save(path); err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadAnnotations(path)
			if err != nil {
				t.Fatal(err)
			}
			c.Argv = []string{"missing-command-must-not-run"}
			rep = Annotate(context.Background(), c, results, loaded, AnnotateOptions{Command: true, Pool: 2})
			if rep.Requested != 0 || rep.Failed != 0 {
				t.Fatalf("cache asked again: %+v", rep)
			}
			rk := ApplyAnnotations(nil, results, loaded)
			options := DigestOptions{Ranking: rk, MinScore: 2}
			html := string(RenderHTML(results, options))
			if strings.Contains(html, "<b>") || !strings.Contains(html, "&lt;b&gt;&amp;&#34;x&#34;&lt;/b&gt;[tag]") || !strings.Contains(html, ">外部</span>") {
				t.Fatal("unsafe or missing tag/badge")
			}
			if md := string(Digest(results, options)); !strings.Contains(md, "（外部）") || !strings.Contains(md, `<b>&"x"</b>［tag］`) || strings.Contains(md, "[tag]") {
				t.Fatal(md)
			}
			CapDemoted(rk, results, map[string]bool{results[0].Source.Name: true})
			if rk[results[0].New[0].ID].Value != DemotedMaxScore {
				t.Fatal("demotion bypassed")
			}
		})
	}
}
func TestCommandFailures(t *testing.T) {
	for _, mode := range []string{"exit", "timeout", "broken", "missing"} {
		t.Run(mode, func(t *testing.T) {
			c := testCommand(t, mode)
			if mode == "timeout" {
				c.Timeout = 150 * time.Millisecond
			}
			if mode == "missing" {
				c.Argv = []string{filepath.Join(t.TempDir(), "not-found")}
			}
			cache := Annotations{}
			rep := Annotate(context.Background(), c, llmResults(), cache, AnnotateOptions{Command: true})
			if rep.Failed == 0 || len(cache) != 0 || rep.Retried != 0 {
				t.Fatalf("%+v cache=%v", rep, cache)
			}
			msg := strings.Join(rep.Errors, " ")
			if mode == "timeout" && !strings.Contains(msg, context.DeadlineExceeded.Error()) {
				t.Fatal(msg)
			}
			if mode == "exit" && (!strings.Contains(msg, "first error") || strings.Contains(msg, "second error")) {
				t.Fatal(msg)
			}
			if mode == "broken" && !strings.Contains(msg, "broken response") {
				t.Fatal(msg)
			}
		})
	}
}
func TestCommandSettings(t *testing.T) {
	for _, s := range []Settings{{LLM: LLMCommand}, {LLM: LLMCommand, LLMCommand: []string{""}}} {
		if s.Validate() == nil {
			t.Fatal("empty argv accepted")
		}
	}
	for _, mode := range []string{"", LLMOff, LLMClaudeCLI, LLMCommand} {
		if err := (Settings{LLM: mode, LLMCommand: []string{"program", "argument with spaces"}}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
}
func TestCommandResponsePartialAndOldCache(t *testing.T) {
	got, ws := parseCommandResponse(`{"items":[{"id":"a","r":0},{"id":"b","r":3.5},{"id":"c","r":-1}]}`, []annotationItem{{ID: "a"}, {ID: "b"}, {ID: "c"}})
	if len(got) != 1 || *got["a"].Score != 0 || len(ws) == 0 {
		t.Fatalf("%v %v", got, ws)
	}
	path := filepath.Join(t.TempDir(), "old.json")
	os.WriteFile(path, []byte(`{"a":{"t":"translation","s":"summary","r":2}}`), 0600)
	a, err := LoadAnnotations(path)
	if err != nil || a["a"].Title != "translation" || a["a"].Tag != "" {
		t.Fatalf("%v %v", a, err)
	}
}

func TestCommandDuplicateArticlesAndBudget(t *testing.T) {
	c := testCommand(t, "ok")
	results := llmResults()
	results = append(results, results[0])
	cache := Annotations{}
	rep := Annotate(context.Background(), c, results, cache, AnnotateOptions{Command: true, Pool: 2, Batch: 1})
	if rep.Requested != 3 || rep.Annotated != 3 || rep.Failed != 0 {
		t.Fatalf("duplicate request: %+v", rep)
	}
	c = testCommand(t, "timeout")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	rep = Annotate(ctx, c, results, Annotations{}, AnnotateOptions{Command: true, Batch: 1})
	if rep.Failed == 0 || !strings.Contains(strings.Join(rep.Errors, " "), "全体の時間上限") {
		t.Fatalf("budget: %+v", rep)
	}
}
