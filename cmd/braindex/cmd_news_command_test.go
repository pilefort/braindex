package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/news"
)

func TestNewsCommandProcess(t *testing.T) {
	if os.Getenv("BRAINDEX_FETCH_COMMAND_TEST") != "1" {
		return
	}
	if marker := os.Getenv("BRAINDEX_FETCH_COMMAND_MARKER"); marker != "" {
		os.WriteFile(marker, []byte("called"), 0600)
	}
	var req struct {
		Version int      `json:"version"`
		Terms   []string `json:"terms"`
		Items   []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if json.NewDecoder(os.Stdin).Decode(&req) != nil || req.Version != 1 {
		os.Exit(1)
	}
	rows := []map[string]any{}
	for _, it := range req.Items {
		rows = append(rows, map[string]any{"id": it.ID, "r": 3, "tag": "physics"})
	}
	json.NewEncoder(os.Stdout).Encode(map[string]any{"items": rows})
	os.Exit(0)
}
func writeCommandConfig(t *testing.T, hub, mode string, argv []string) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"root": "..", "news": map[string]any{"llm": mode, "llm_command": argv, "serendipity": 0}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(hub, "braindex.json"), string(b))
}
func TestNewsFetchCommand(t *testing.T) {
	hub, _ := newsHub(t)
	t.Setenv("BRAINDEX_FETCH_COMMAND_TEST", "1")
	marker := filepath.Join(t.TempDir(), "called")
	t.Setenv("BRAINDEX_FETCH_COMMAND_MARKER", marker)
	// 一部の旧テストが factory を nil に戻すので、この試験は本物の Command を明示する。
	orig := newNewsAnnotator
	newNewsAnnotator = func(s news.Settings) (news.Annotator, error) { return news.Command{Argv: s.LLMCommand}, nil }
	t.Cleanup(func() { newNewsAnnotator = orig })
	writeCommandConfig(t, hub, "command", []string{os.Args[0], "-test.run=^TestNewsCommandProcess$"})
	writeFile(t, filepath.Join(hub, "news", "interests.md"), "ゴルーチン\n")
	for _, flag := range []string{"-no-llm", "-no-score"} {
		code, _, se := llmFetch(t, hub, "-replay", flag)
		if code != 2 {
			t.Fatalf("%s %d %s", flag, code, se)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("called with %s", flag)
		}
	}
	code, so, se := llmFetch(t, hub, "-replay")
	if code != 2 || !strings.Contains(so, "★3（外部・ゴルーチン） physics") {
		t.Fatalf("%d %s %s", code, so, se)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal(err)
	}
	os.Remove(marker)
	code, _, se = llmFetch(t, hub, "-replay")
	if code != 2 {
		t.Fatal(se)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("cache asked again")
	}
}
func TestNewsFetchCommandFallbackAndOffOutput(t *testing.T) {
	hub, _ := newsHub(t)
	writeFile(t, filepath.Join(hub, "news", "interests.md"), "ゴルーチン\n")
	orig := newNewsAnnotator
	newNewsAnnotator = func(s news.Settings) (news.Annotator, error) { return news.Command{Argv: s.LLMCommand}, nil }
	t.Cleanup(func() { newNewsAnnotator = orig })
	writeCommandConfig(t, hub, "off", nil)
	code, before, se := llmFetch(t, hub, "-replay")
	if code != 2 {
		t.Fatal(se)
	}
	writeCommandConfig(t, hub, "off", []string{"missing-unused-program"})
	code, after, se := llmFetch(t, hub, "-replay")
	if code != 2 || before != after {
		t.Fatalf("off changed: %d %s", code, se)
	}
	writeCommandConfig(t, hub, "command", []string{filepath.Join(t.TempDir(), "missing-program")})
	code, so, se := llmFetch(t, hub, "-replay")
	if code != 2 || !strings.Contains(so, "★2（ゴルーチン）") || !strings.Contains(se, "外部採点") {
		t.Fatalf("%d %s %s", code, so, se)
	}
	// 成功した場合以外に外部の印を付けない。
	if strings.Contains(so, "（外部") {
		t.Fatal(fmt.Sprintf("unexpected external score: %s", so))
	}
}
