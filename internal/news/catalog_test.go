package news

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/interest"
)

// 同梱の目録が規則を通ること(name・url の一意・https・照合語が語の規則を通る)。
func TestCatalog_Embedded(t *testing.T) {
	es := Catalog()
	if len(es) != 98 {
		t.Fatalf("目録の本数: got %d want 98(docs/notes/project/news-feed-catalog-2026-09-05.md)", len(es))
	}
	for _, e := range es {
		if !strings.HasPrefix(e.URL, "https://") {
			t.Errorf("%s: https でない: %s", e.Name, e.URL)
		}
		if e.Genre == "" || len(e.Tags) == 0 {
			t.Errorf("%s: genre か tags が空", e.Name)
		}
	}
}

func TestParseCatalog_Rejects(t *testing.T) {
	cases := map[string]string{
		"照合語なし":       `[{"genre":"g","name":"a","url":"https://x/","tags":["t"],"keywords":[]}]`,
		"ストップワード":     `[{"genre":"g","name":"a","url":"https://x/","tags":["t"],"keywords":["github"]}]`,
		"2 文字":        `[{"genre":"g","name":"a","url":"https://x/","tags":["t"],"keywords":["fn"]}]`, // 許可リスト(go・ai・ci・ui・db)の外
		"大文字":         `[{"genre":"g","name":"a","url":"https://x/","tags":["t"],"keywords":["Docker"]}]`,
		"name 重複":     `[{"genre":"g","name":"a","url":"https://x/","tags":["t"],"keywords":["docker"]},{"genre":"g","name":"a","url":"https://y/","tags":["t"],"keywords":["docker"]}]`,
		"url 重複(末尾/)": `[{"genre":"g","name":"a","url":"https://x/","tags":["t"],"keywords":["docker"]},{"genre":"g","name":"b","url":"https://x","tags":["t"],"keywords":["docker"]}]`,
		"http(s) でない": `[{"genre":"g","name":"a","url":"ftp://x/","tags":["t"],"keywords":["docker"]}]`,
	}
	for name, src := range cases {
		if _, err := parseCatalog([]byte(src)); err == nil {
			t.Errorf("%s: エラーになるべき", name)
		}
	}
}

func testCatalog() []CatalogEntry {
	return []CatalogEntry{
		{Genre: "ツール", Name: "Docker Blog", URL: "https://d/feed/", Tags: []string{"コンテナ"}, Keywords: []string{"docker", "コンテナ"}},
		{Genre: "言語", Name: "Go Blog", URL: "https://g/feed", Tags: []string{"Go"}, Keywords: []string{"golang"}},
		{Genre: "言語", Name: "Rust Blog", URL: "https://r/feed", Tags: []string{"Rust"}, Keywords: []string{"rust"}},
		{Genre: "経済", Name: "Econ", URL: "https://e/feed", Tags: []string{"経済"}, Keywords: []string{"金利"}},
	}
}

func testProfile() interest.Profile {
	return interest.Profile{Today: "2026-09-05", Days: 14, Terms: []interest.Term{
		{Word: "docker", Weight: 1.5}, {Word: "golang", Weight: 2}, {Word: "コンテナ", Weight: 0.5}, {Word: "rust", Weight: 2},
	}}
}

// 判定表: 当たった語の重みの和で並び、同点は名前順。登録済みは除く。当たらない取材先は出ない。
func TestSuggest(t *testing.T) {
	got := Suggest(testCatalog(), testProfile(), []Source{{Name: "rust", URL: "https://r/feed/"}}, 0)
	if len(got) != 2 {
		t.Fatalf("候補数: got %d want 2: %+v", len(got), got)
	}
	// Docker Blog 1.5+0.5=2.0、Go Blog 2.0 → 同点は名前順(Docker Blog < Go Blog)
	if got[0].Name != "Docker Blog" || got[1].Name != "Go Blog" {
		t.Errorf("並び: %s, %s", got[0].Name, got[1].Name)
	}
	if got[0].Score != 2 || strings.Join(got[0].Matched, ",") != "docker,コンテナ" {
		t.Errorf("Docker Blog: score %v matched %v", got[0].Score, got[0].Matched)
	}
	if top := Suggest(testCatalog(), testProfile(), nil, 1); len(top) != 1 {
		t.Errorf("-top 1: got %d", len(top))
	}
}

func TestSuggest_Deterministic(t *testing.T) {
	r1 := BuildSuggestReport(testCatalog(), testProfile(), nil, 10)
	r2 := BuildSuggestReport(testCatalog(), testProfile(), nil, 10)
	if !bytes.Equal(r1.Marshal(), r2.Marshal()) {
		t.Error("同じ材料で出力が違う")
	}
	j1, _ := r1.JSON()
	j2, _ := r2.JSON()
	if !bytes.Equal(j1, j2) {
		t.Error("同じ材料で JSON が違う")
	}
}

func TestSuggestReport_Marshal(t *testing.T) {
	r := BuildSuggestReport(testCatalog(), testProfile(), []Source{{Name: "rust", URL: "https://r/feed"}}, 10)
	md := string(r.Marshal())
	for _, want := range []string{"# 取材先の候補(2026-09-05・直近 14 日)", "関心語 4・目録 4 本(登録済み 1 本を除外)",
		"1. **Docker Blog** — ツール — 当たった語: docker, コンテナ — https://d/feed/", "2. **Go Blog**"} {
		if !strings.Contains(md, want) {
			t.Errorf("出力に %q が無い:\n%s", want, md)
		}
	}
	if strings.Contains(md, "Rust Blog") || strings.Contains(md, "Econ") {
		t.Errorf("登録済み・不一致の取材先が出ている:\n%s", md)
	}
	empty := BuildSuggestReport(testCatalog(), interest.Profile{Today: "2026-09-05", Days: 14}, nil, 10)
	if !strings.Contains(string(empty.Marshal()), "当たる取材先なし") {
		t.Errorf("0 件の文言が無い:\n%s", empty.Marshal())
	}
}
func TestCatalogMetadata(t *testing.T) {
	cat := Catalog()
	counts := map[string]int{}
	general := 0
	for _, e := range cat {
		counts[e.Lang()]++
		if e.IsGeneralNews() {
			general++
		}
		got, ok := LookupCatalog(cat, e.URL+"/")
		if !ok || got.Name != e.Name {
			t.Fatalf("lookup %s: %+v %v", e.URL, got, ok)
		}
	}
	if counts["ja"] != 20 || counts["en"] != 78 || general != 13 {
		t.Fatalf("lang=%v general=%d", counts, general)
	}
	if len(CatalogKeywords(cat)) != 328 {
		t.Fatalf("keywords=%d", len(CatalogKeywords(cat)))
	}
	if _, ok := LookupCatalog(cat, "https://example.com/missing"); ok {
		t.Fatal("unknown URL matched")
	}
}
