package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/news"
)

func newsSuggest(t *testing.T, hub string, args ...string) (code int, so, se string) {
	t.Helper()
	var sob, seb bytes.Buffer
	code = dispatch(append([]string{"news", "suggest", "-config", filepath.Join(hub, "braindex.json"), "-date", "2026-09-01"}, args...), &sob, &seb)
	return code, sob.String(), seb.String()
}

// profileHub の材料(keep「ゴルーチンの本」・補助 Rust)から、目録の Go Blog(照合語 ゴルーチン)と Rust Blog(rust)が候補に出る。
// feeds.json に Rust Blog を登録しておくと除かれる。
func TestNewsSuggest(t *testing.T) {
	hub := profileHub(t)
	code, so, se := newsSuggest(t, hub)
	if code != 2 { // feeds.json が無いので警告つき
		t.Fatalf("exit=%d\n%s", code, se)
	}
	if !strings.Contains(se, "feeds.json") || !strings.Contains(se, "警告 1 件") {
		t.Errorf("feeds.json 不在の警告が無い:\n%s", se)
	}
	for _, want := range []string{"# 取材先の候補(2026-09-01・直近 14 日)", "目録 98 本(登録済み 0 本を除外)", "**Go Blog** — 言語 — 当たった語: ゴルーチン", "**Rust Blog** — 言語 — 当たった語: rust"} {
		if !strings.Contains(so, want) {
			t.Errorf("出力に %q が無い:\n%s", want, so)
		}
	}
	if strings.Contains(so, "ゴルーチンの本") {
		t.Errorf("材料の本文(見出し)が出力に載っている:\n%s", so)
	}

	writeFile(t, filepath.Join(hub, "news", "feeds.json"), `[{"name": "Rust", "url": "https://blog.rust-lang.org/feed.xml/"}]`)
	code, so, se = newsSuggest(t, hub, "-json")
	if code != 0 {
		t.Fatalf("exit=%d\n%s", code, se)
	}
	var r news.SuggestReport
	if err := json.Unmarshal([]byte(so), &r); err != nil {
		t.Fatal(err)
	}
	if r.Registered != 1 || r.CatalogSize != 98 {
		t.Errorf("registered=%d catalog=%d", r.Registered, r.CatalogSize)
	}
	for _, s := range r.Suggestions {
		if s.Name == "Rust Blog" {
			t.Errorf("登録済み(末尾スラッシュ違い)の Rust Blog が候補に出ている")
		}
	}

	if code, _, se := newsSuggest(t, hub, "-top", "-1"); code != 1 || !strings.Contains(se, "-top") {
		t.Errorf("-top -1: exit=%d\n%s", code, se)
	}
	if code, _, se := newsSuggest(t, hub, "extra"); code != 1 || !strings.Contains(se, "受け付けない") {
		t.Errorf("余分な引数: exit=%d\n%s", code, se)
	}
}

// 同じ材料からは同じ出力(決定性)。
func TestNewsSuggest_Deterministic(t *testing.T) {
	hub := profileHub(t)
	_, a, _ := newsSuggest(t, hub)
	_, b, _ := newsSuggest(t, hub)
	if a != b {
		t.Errorf("出力が違う:\n%s\n---\n%s", a, b)
	}
}
