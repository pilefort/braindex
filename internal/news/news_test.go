package news

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/feed"
)

func TestSettings_Defaults(t *testing.T) {
	s := Settings{}.WithDefaults()
	if s.Dir != "news" || s.Feeds != "news/feeds.json" || s.SeenDays != 90 {
		t.Errorf("既定値: %+v", s)
	}
	if s.Cap("daily") != 15 || s.Cap("weekly") != 25 || s.Cap("all") != 20 || s.Cap("") != 20 {
		t.Errorf("既定の上限: daily=%d weekly=%d all=%d", s.Cap("daily"), s.Cap("weekly"), s.Cap("all"))
	}
	// 指定した層だけ上書き。他の層は既定の表ではなく DefaultCap(指定した表が正)
	s = Settings{CapPerLayer: map[string]int{"daily": 5}}.WithDefaults()
	if s.Cap("daily") != 5 || s.Cap("weekly") != 20 {
		t.Errorf("上書き: daily=%d weekly=%d", s.Cap("daily"), s.Cap("weekly"))
	}
}

// WithDefaults が埋める層別上限は複製。呼び出し側が書き換えても既定の表は汚れない。
func TestSettings_DefaultsAreCopied(t *testing.T) {
	s := Settings{}.WithDefaults()
	s.CapPerLayer["daily"] = 1
	if DefaultCapPerLayer["daily"] != 15 {
		t.Errorf("既定の表が書き換わった: %v", DefaultCapPerLayer)
	}
	if got := (Settings{}).WithDefaults().Cap("daily"); got != 15 {
		t.Errorf("次の WithDefaults に漏れた: %d", got)
	}
}

func TestParseFeeds(t *testing.T) {
	good := `[{"name": "A", "url": "https://example.com/a.xml", "layer": "daily", "lang": "en", "category": "tech", "note": "x"},
	         {"name": "B", "url": "http://example.com/b.xml", "layer": "weekly"},
	         {"name": "C", "url": "https://example.com/c.xml"}]`
	srcs, err := ParseFeeds([]byte(good), "feeds.json")
	if err != nil || len(srcs) != 3 || srcs[0].Category != "tech" || srcs[1].Layer != "weekly" {
		t.Fatalf("err=%v srcs=%+v", err, srcs)
	}
	if got := FilterLayer(srcs, "daily"); len(got) != 1 || got[0].Name != "A" {
		t.Errorf("daily: %+v", got)
	}
	if got := FilterLayer(srcs, LayerAll); len(got) != 3 {
		t.Errorf("all: %+v", got)
	}
	if got := FilterLayer(srcs, "none"); got != nil {
		t.Errorf("none: %+v", got)
	}
	if got := Layers(srcs); !reflect.DeepEqual(got, []string{"daily", "weekly"}) {
		t.Errorf("Layers: %v", got)
	}

	bad := map[string]string{ // 入力 → エラーに含まれる語
		`[{"url": "https://example.com/a.xml"}]`:                                     "name が無い",
		`[{"name": "A", "url": "ftp://example.com/a"}]`:                              "http(s) でない",
		`[{"name": "A", "url": "https://x/a"}, {"name": "A", "url": "https://x/b"}]`: "重複",
		`[{"name": "A", "url": "https://x/a", "layers": "daily"}]`:                   "layers",
		`{"feeds": []}`: "feeds.json",
		`[{"name": "A", "url": "https://x/a"}] []`: "余分な内容",
	}
	for in, want := range bad {
		_, err := ParseFeeds([]byte(in), "feeds.json")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err=%v(%q を期待)", in, err, want)
		}
	}
}

func TestLoadFeeds_Missing(t *testing.T) {
	_, err := LoadFeeds(filepath.Join(t.TempDir(), "feeds.json"))
	if err == nil || !strings.Contains(err.Error(), "フィード一覧が無い") {
		t.Errorf("err=%v", err)
	}
}

func entry(i string) feed.Entry { return feed.Entry{ID: i, Title: "t" + i, Link: "https://x/" + i} }

func TestSeen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".seen.json")
	s, err := LoadSeen(path)
	if err != nil || len(s) != 0 {
		t.Fatalf("無いとき: err=%v s=%v", err, s)
	}
	s["id1"] = "2026-08-01"
	entries := []feed.Entry{entry("id1"), entry("id2")}
	if got := s.FilterNew(entries); len(got) != 1 || got[0].ID != "id2" {
		t.Errorf("FilterNew: %+v", got)
	}
	s.Mark(entries, "2026-08-15")
	if s["id1"] != "2026-08-01" || s["id2"] != "2026-08-15" {
		t.Errorf("Mark: %v", s)
	}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	want := "{\n\"id1\": \"2026-08-01\",\n\"id2\": \"2026-08-15\"\n}\n"
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != want {
		t.Errorf("Save:\n%s", b)
	}
	s2, err := LoadSeen(path)
	if err != nil || !reflect.DeepEqual(s, s2) {
		t.Errorf("往復: err=%v %v", err, s2)
	}
	if string(Seen{}.Marshal()) != "{}\n" {
		t.Errorf("空: %q", Seen{}.Marshal())
	}

	pruned, err := (Seen{"old": "2026-01-01", "new": "2026-08-10", "edge": "2026-05-17"}).Prune("2026-08-15", 90)
	if err != nil || len(pruned) != 2 || pruned["old"] != "" || pruned["new"] == "" || pruned["edge"] == "" {
		t.Errorf("Prune: err=%v %v", err, pruned)
	}
	if _, err := (Seen{}).Prune("2026/08/15", 90); err == nil {
		t.Error("日付の誤りがエラーにならない")
	}

	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSeen(path); err == nil {
		t.Error("壊れたファイルがエラーにならない")
	}
}

// stubFetcher は URL ごとに決めた結果を返す。
type stubFetcher map[string]any // feed.Document か error

func (f stubFetcher) Fetch(_ context.Context, url string) (feed.Document, error) {
	switch v := f[url].(type) {
	case feed.Document:
		return v, nil
	case error:
		return feed.Document{}, v
	}
	return feed.Document{}, errors.New("stub に無い URL " + url)
}

func TestCollect(t *testing.T) {
	srcs := []Source{{Name: "A", URL: "a"}, {Name: "B", URL: "b"}, {Name: "C", URL: "c"}}
	f := stubFetcher{
		"a": feed.Document{Entries: []feed.Entry{entry("a1"), entry("a2")}},
		"b": errors.New("HTTP 500"),
		"c": feed.Document{Entries: []feed.Entry{entry("c1")}},
	}
	seen := Seen{"a1": "2026-08-01"}
	res := Collect(context.Background(), f, srcs, seen, "2026-08-15", false)
	if len(res) != 3 || res[0].Source.Name != "A" || res[2].Source.Name != "C" {
		t.Fatalf("順序: %+v", res)
	}
	if ids(res[0].New) != "a2" || ids(res[2].New) != "c1" || res[1].Err == nil || len(res[1].Entries) != 0 {
		t.Errorf("新着: %+v", res)
	}
	if seen["a2"] != "2026-08-15" || seen["c1"] != "2026-08-15" || seen["a1"] != "2026-08-01" {
		t.Errorf("既読: %v", seen)
	}
	if AllFailed(res) || len(Failed(res)) != 1 {
		t.Errorf("失敗の数: %d", len(Failed(res)))
	}

	// replay: 既読を見ず全件、既読も増えない
	before := len(seen)
	res = Collect(context.Background(), f, srcs[:1], seen, "2026-08-16", true)
	if ids(res[0].New) != "a1,a2" || len(seen) != before {
		t.Errorf("replay: new=%s seen=%v", ids(res[0].New), seen)
	}

	if !AllFailed(Collect(context.Background(), f, srcs[1:2], seen, "2026-08-16", false)) || !AllFailed(nil) {
		t.Error("AllFailed")
	}
}

func ids(es []feed.Entry) string {
	var s []string
	for _, e := range es {
		s = append(s, e.ID)
	}
	return strings.Join(s, ",")
}

func TestDigest(t *testing.T) {
	res := []Result{
		{Source: Source{Name: "A", Category: "tech"}, New: []feed.Entry{
			{Title: "記事 [1]", Link: "https://x/1", Published: "2026-08-14"},
			{Title: "記事 2", Link: "https://x/2"},
			{Title: "記事 3", Link: "https://x/3"},
		}},
		{Source: Source{Name: "B"}, Err: errors.New("HTTP 404")},
		{Source: Source{Name: "C"}, New: nil}, // 新着なし: 書かない
		{Source: Source{Name: "D"}, New: []feed.Entry{{Title: "d", Link: "https://x/d"}}},
	}
	want := `# ニュースダイジェスト 2026-08-15（daily 層）

新着 4 件（フィード 3 本）

## A（tech・新着 3 件）
- 2026-08-14 [記事 ［1］](https://x/1)
- [記事 2](https://x/2)
- （上限 2 件を超えた 1 件は省略）

## D（新着 1 件）
- [d](https://x/d)

## 取得失敗
- B: HTTP 404
`
	got := string(Digest(res, "daily", "2026-08-15", 2))
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if string(Digest(res, "daily", "2026-08-15", 2)) != got {
		t.Error("2 回の生成が一致しない")
	}
	empty := string(Digest(nil, "all", "2026-08-15", 20))
	if !strings.Contains(empty, "新着 0 件（フィード 0 本）") || strings.Contains(empty, "取得失敗") {
		t.Errorf("空:\n%s", empty)
	}
}
