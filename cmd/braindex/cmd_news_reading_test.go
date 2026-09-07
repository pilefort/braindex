package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/news"
)

func TestNewsReadingConversation(t *testing.T) {
	hub, _ := newsHub(t)
	inbox := filepath.Join(hub, "inbox")
	os.MkdirAll(inbox, 0755)
	sel := news.Selection{Type: news.SelectionType, Date: "2026-01-02", Layer: "daily", ExportedAt: "2026-01-02T10:00:00Z", Keeps: []news.Keep{{ID: "article-1", Title: "並行処理", Link: "https://example.com/1", Feed: "A"}}, Reading: []news.ReadingUpdate{{ID: "article-1", Status: "hold", Questions: []news.Question{{ID: "q1", Mode: "stuck", Text: "具体例で教えて", Created: "2026-01-02T10:00:00Z"}}}}}
	b, _ := json.Marshal(sel)
	writeFile(t, filepath.Join(inbox, news.SelectionPrefix+"1.json"), string(b))
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var so, se bytes.Buffer
		args = append(args, "-config", filepath.Join(hub, "braindex.json"))
		code := dispatch(append([]string{"news"}, args...), &so, &se)
		return code, so.String(), se.String()
	}
	if code, _, err := run("apply", "-inbox", inbox); code != 0 {
		t.Fatal(err)
	}
	if code, out, err := run("reading", "-pending"); code != 0 || !strings.Contains(out, "q1") {
		t.Fatalf("pending: %d %s %s", code, out, err)
	}
	if code, out, err := run("reading", "-id", "article-1", "-question", "q1"); code != 0 || !strings.Contains(out, "具体例で教えて") || !strings.Contains(out, "https://example.com/1") {
		t.Fatalf("prompt: %d %s %s", code, out, err)
	}
	answer := filepath.Join(hub, "answer.md")
	writeFile(t, answer, "## 具体例\n\n複数の仕事を並行して進めます。\n\n出典: https://example.com/1\n")
	if code, _, err := run("reading", "-id", "article-1", "-question", "q1", "-answer", answer, "-no-open"); code != 0 {
		t.Fatal(err)
	}
	lib, err := news.LoadReading(filepath.Join(hub, "news"))
	if err != nil {
		t.Fatal(err)
	}
	if lib.Articles["article-1"].Questions[0].Answer == "" {
		t.Fatal("answer missing")
	}
	h := readFile(t, filepath.Join(hub, "news", news.ReadingHTML))
	if !strings.Contains(h, "具体例") {
		t.Fatal("HTML missing answer")
	}
	if code, out, _ := run("reading", "-pending"); code != 0 || !strings.Contains(out, "回答未登録: 0 件") {
		t.Fatal(out)
	}
	writeFile(t, answer, "書き換え")
	if code, _, err := run("reading", "-id", "article-1", "-question", "q1", "-answer", answer, "-no-open"); code != 1 || !strings.Contains(err, "上書きしない") {
		t.Fatal("answer overwrite allowed")
	}
	for _, args := range [][]string{{"reading", "-id", "article-1"}, {"reading", "-answer", answer}, {"reading", "-pending", "-id", "article-1", "-question", "q1"}} {
		if code, _, _ := run(args...); code != 1 {
			t.Fatalf("invalid args passed: %v", args)
		}
	}
	// 本文を自動取得したりLLMを起動せず、空の一覧も生成できる。
	if code, _, err := run("reading", "-no-open"); code != 0 {
		t.Fatal(err)
	}
}

// -ask: 概要の画面から詳しい解説を頼むとき、相談を作ってから回答を登録する。
func TestNewsReadingAsk(t *testing.T) {
	hub, _ := newsHub(t)
	inbox := filepath.Join(hub, "inbox")
	os.MkdirAll(inbox, 0755)
	sel := news.Selection{Type: news.SelectionType, Date: "2026-01-02", Layer: "daily", ExportedAt: "2026-01-02T10:00:00Z",
		Keeps: []news.Keep{{ID: "article-1", Title: "並行処理", Link: "https://example.com/1", Feed: "A"}}}
	b, _ := json.Marshal(sel)
	writeFile(t, filepath.Join(inbox, news.SelectionPrefix+"1.json"), string(b))
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var so, se bytes.Buffer
		args = append(args, "-config", filepath.Join(hub, "braindex.json"))
		code := dispatch(append([]string{"news"}, args...), &so, &se)
		return code, so.String(), se.String()
	}
	if code, _, err := run("apply", "-inbox", inbox); code != 0 {
		t.Fatal(err)
	}
	answer := filepath.Join(hub, "detail.md")
	writeFile(t, answer, "## 仕組み\n\n詳しい説明です。\n")
	if code, _, err := run("reading", "-id", "article-1", "-ask", "detail", "-answer", answer, "-no-open"); code != 0 {
		t.Fatalf("ask: %s", err)
	}
	lib, err := news.LoadReading(filepath.Join(hub, "news"))
	if err != nil {
		t.Fatal(err)
	}
	qs := lib.Articles["article-1"].Questions
	if len(qs) != 1 || qs[0].Mode != "detail" || qs[0].Answer == "" {
		t.Fatalf("相談が作られていない: %+v", qs)
	}
	// もう一度頼めば別の相談になる(回答済みを上書きしない)
	if code, _, err := run("reading", "-id", "article-1", "-ask", "detail", "-answer", answer, "-no-open"); code != 0 {
		t.Fatalf("2 回目: %s", err)
	}
	if lib, _ := news.LoadReading(filepath.Join(hub, "news")); len(lib.Articles["article-1"].Questions) != 2 {
		t.Error("2 回目で相談が増えていない")
	}
	// 知らない種類は断る。-question との併用も断る
	if code, _, err := run("reading", "-id", "article-1", "-ask", "nope", "-answer", answer, "-no-open"); code != 1 || !strings.Contains(err, "相談の種類が違う") {
		t.Errorf("未知の種類: %d %s", code, err)
	}
	if code, _, err := run("reading", "-id", "article-1", "-ask", "detail", "-question", "q1", "-answer", answer, "-no-open"); code != 1 || !strings.Contains(err, "-question は要らない") {
		t.Errorf("併用: %d %s", code, err)
	}
}
