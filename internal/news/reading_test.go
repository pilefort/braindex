package news

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadingRoundTrip(t *testing.T) {
	dir := t.TempDir()
	inbox := filepath.Join(dir, "inbox")
	os.MkdirAll(inbox, 0755)
	sel := Selection{Type: SelectionType, Date: "2026-01-02", Layer: "daily", ExportedAt: "2026-01-02T12:00:00Z", Keeps: []Keep{{ID: "a", Title: "記事", Link: "https://example.com/a", Feed: "A", Summary: "要点"}}, Reading: []ReadingUpdate{{ID: "a", Status: "later", Questions: []Question{{ID: "q1", Mode: "stuck", Text: "前提を知りたい", Created: "2026-01-02T12:00:00Z"}}}}}
	put := func(name string, s Selection) {
		t.Helper()
		b, _ := json.Marshal(s)
		if err := os.WriteFile(filepath.Join(inbox, SelectionPrefix+name+".json"), b, 0644); err != nil {
			t.Fatal(err)
		}
	}
	put("1", sel)
	if _, err := Ingest(dir, []string{inbox}, nil); err != nil {
		t.Fatal(err)
	}
	lib, err := LoadReading(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Articles) != 1 || len(lib.Articles["a"].Questions) != 1 {
		t.Fatalf("lost article/question: %#v", lib)
	}
	if err := lib.Answer("a", "q1", "背景の説明\n\n[出典](https://example.com/a)"); err != nil {
		t.Fatal(err)
	}
	if err := lib.Save(dir); err != nil {
		t.Fatal(err)
	}
	// ブラウザが持つ古い回答なしの質問でも、保存済みの回答は消さない。
	put("2", sel)
	if _, err := Ingest(dir, []string{inbox}, nil); err != nil {
		t.Fatal(err)
	}
	lib, err = LoadReading(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := lib.Articles["a"]
	if !strings.Contains(a.Questions[0].Answer, "背景") || a.Summary != "要点" {
		t.Fatalf("lost answer/summary: %#v", a)
	}
	if err := lib.Answer("a", "q1", "別の答え"); err == nil {
		t.Fatal("answered question overwritten")
	}
	if err := lib.Answer("a", "q2", "答え"); err == nil {
		t.Fatal("unknown question accepted")
	}
	// 古い書き出しで読書状態を巻き戻さない。追記した質問は失わない。
	sel.Library = true
	sel.ExportedAt = "2026-01-03T12:00:00Z"
	sel.Reading[0].Status = "done"
	put("3", sel)
	if _, err := Ingest(dir, []string{inbox}, nil); err != nil {
		t.Fatal(err)
	}
	sel.ExportedAt = "2026-01-02T12:00:00Z"
	sel.Reading[0].Status = "later"
	put("4", sel)
	if _, err := Ingest(dir, []string{inbox}, nil); err != nil {
		t.Fatal(err)
	}
	lib, _ = LoadReading(dir)
	if lib.Articles["a"].Status != "done" {
		t.Fatal("stale selection rewound status")
	}
	// 古いタブを後から書き出しても、状態を変えていない記事は巻き戻さない。
	unchanged := false
	sel.Reading[0].StatusChanged = &unchanged
	sel.ExportedAt = "2026-01-04T12:00:00Z"
	put("5", sel)
	if _, err := Ingest(dir, []string{inbox}, nil); err != nil {
		t.Fatal(err)
	}
	lib, _ = LoadReading(dir)
	if lib.Articles["a"].Status != "done" {
		t.Fatal("old tab rewound unchanged status")
	}
	changed := true
	sel.Reading[0].StatusChanged = &changed
	sel.Reading[0].StatusUpdated = "2026-01-02T12:00:00Z"
	put("6", sel)
	if _, err := Ingest(dir, []string{inbox}, nil); err != nil {
		t.Fatal(err)
	}
	lib, _ = LoadReading(dir)
	if lib.Articles["a"].Status != "done" {
		t.Fatal("old draft rewound status")
	}
	h := string(RenderReading(lib))
	if !strings.Contains(h, "背景") || !strings.Contains(h, "解説を読む") || !strings.Contains(h, "https://example.com/a") {
		t.Fatal("reading page lacks saved explanation")
	}
	if h != string(RenderReading(lib)) {
		t.Fatal("reading HTML is not deterministic")
	}
}

func TestReadingUntrustedUpdates(t *testing.T) {
	lib := Reading{Articles: map[string]*ReadingArticle{}}
	sel := Selection{Date: "2026-01-02", ExportedAt: "2026-01-02T12:00:00Z", Keeps: []Keep{{ID: "a", Title: "<script>悪意</script>", Link: "https://example.com/a"}, {ID: "bad", Link: "javascript:alert(1)"}}, Reading: []ReadingUpdate{{ID: "a", Status: "unknown", Questions: []Question{{ID: "q1", Mode: "stuck", Text: "<img onerror=alert(1)>", Created: "2026-01-02T12:00:00Z", Answer: "偽の回答"}}}, {ID: "unknown", Status: "done"}}}
	lib.Merge(sel)
	if len(lib.Articles) != 1 {
		t.Fatal("unsafe or unknown article added")
	}
	a := lib.Articles["a"]
	if a.Status != "later" || a.Questions[0].Answer != "" {
		t.Fatal("untrusted state/answer imported")
	}
	if strings.Contains(string(RenderReading(lib)), "<script>悪意") || strings.Contains(string(RenderReading(lib)), "<img onerror") {
		t.Fatal("HTML injection")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ReadingFile), []byte("broken"), 0644)
	if _, err := LoadReading(dir); err == nil {
		t.Fatal("corruption ignored")
	}
}

func TestReadingBootstrap(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, IngestedDir)
	os.MkdirAll(old, 0755)
	sel := Selection{Type: SelectionType, Date: "2026-01-02", Layer: "daily", Keeps: []Keep{{ID: "a", Title: "以前の記事", Link: "https://example.com/a"}}}
	b, _ := json.Marshal(sel)
	path := filepath.Join(old, SelectionPrefix+"old.json")
	os.WriteFile(path, b, 0644)
	r, err := LoadReading(dir)
	if err != nil || len(r.Articles) != 1 {
		t.Fatalf("以前の記事を復元できない: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(b) {
		t.Fatal("以前の記録を書き換えた")
	}
}
