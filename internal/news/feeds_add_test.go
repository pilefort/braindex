package news

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestAddFeedsValidation(t *testing.T) {
	catalog := []CatalogEntry{{Name: "Example", URL: "https://example.com/rss", Genre: "開発", Tags: []string{"日本語"}}}
	for _, tc := range []struct {
		name, initial, query, url, reason string
		count                             int
	}{
		{"catalog", `[]`, "", catalog[0].URL + "/", "", 1},
		{"unknown", `[]`, "", "https://other.example/rss", "目録に無い URL", 0},
		{"unknown newline", `[]`, "", "https://other.example/rss\nother line", "目録に無い URL", 0},
		{"existing", `[{"name":"Renamed","url":"https://example.com/rss/"}]`, "", catalog[0].URL, "", 0},
		{"name conflict", `[{"name":"Example","url":"https://other.example/rss"}]`, "", catalog[0].URL, "同名の取材先が別 URL で登録済み", 0},
		{"query", `[]`, "compiler", SearchFeedURL("compiler"), "", 1},
		{"query ja", `[]`, "科学", SearchFeedURL("科学"), "", 1},
		{"query mismatch", `[]`, "compiler", SearchFeedURL("science"), "検索語と URL が合わない", 0},
		{"two words", `[]`, "compiler science", SearchFeedURL("compiler science"), "語の規則を通らない", 0},
		{"uppercase", `[]`, "Compiler", SearchFeedURL("Compiler"), "語の規則を通らない", 0},
		{"short", `[]`, "zz", SearchFeedURL("zz"), "語の規則を通らない", 0},
		{"query name conflict", `[{"name":"検索: compiler","url":"https://other.example/rss"}]`, "compiler", SearchFeedURL("compiler"), "同名の取材先が別 URL で登録済み", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "feeds.json")
			if err := os.WriteFile(path, []byte(tc.initial), 0600); err != nil {
				t.Fatal(err)
			}
			reqs := []FeedRequest{{URL: tc.url, Query: tc.query}}
			added, msgs, err := AddFeeds(path, catalog, reqs, "daily", "2026-09-13")
			if err != nil || len(added) != tc.count {
				t.Fatalf("added=%v msgs=%v err=%v", added, msgs, err)
			}
			for _, msg := range msgs {
				if strings.ContainsAny(msg, "\r\n") {
					t.Fatalf("複数行の理由: %q", msg)
				}
			}
			if tc.reason != "" {
				if len(msgs) != 1 || !strings.Contains(msgs[0], tc.reason) {
					t.Fatalf("msgs=%v", msgs)
				}
			} else if len(msgs) != 0 {
				t.Fatalf("msgs=%v", msgs)
			}
			got, err := LoadFeeds(path)
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			if tc.count == 0 && string(before) != tc.initial {
				t.Fatal("拒否でファイルが変化した")
			}
			if tc.count == 1 {
				s := got[len(got)-1]
				wantName, wantURL, wantLang, wantCat := "Example", catalog[0].URL, "ja", "開発"
				if tc.query != "" {
					wantName, wantURL, wantLang, wantCat = "検索: "+tc.query, SearchFeedURL(tc.query), SearchFeedLang(tc.query), "検索"
				}
				if s != (Source{Name: wantName, URL: wantURL, Layer: "daily", Lang: wantLang, Category: wantCat, Note: "braindex news の候補から追加（2026-09-13）"}) {
					t.Fatalf("source=%+v", s)
				}
				added, msgs, err = AddFeeds(path, catalog, reqs, "daily", "2026-09-13")
				after, _ := os.ReadFile(path)
				if err != nil || len(added) != 0 || len(msgs) != 0 || !bytes.Equal(before, after) {
					t.Fatalf("再実行: %v %v %v", added, msgs, err)
				}
			}
		})
	}
}

func TestAddFeedsPreservesBytes(t *testing.T) {
	e := CatalogEntry{Name: "New", URL: "https://example.com/new"}
	for _, bom := range []string{"", "\xef\xbb\xbf"} {
		for _, body := range []string{"[]", "[ \r\n\t ]", " \r\n[\n\t{ \"url\":\"https://example.com/old\", \"name\": \"Old\" } \r\n\t]\r\n  "} {
			path := filepath.Join(t.TempDir(), "feeds.json")
			original := bom + body
			if err := os.WriteFile(path, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			added, _, err := AddFeeds(path, []CatalogEntry{e}, []FeedRequest{{URL: e.URL}, {URL: e.URL}}, "all", "2026-09-13")
			if err != nil || !reflect.DeepEqual(added, []string{"New"}) {
				t.Fatalf("%v %v", added, err)
			}
			got, _ := os.ReadFile(path)
			end := strings.LastIndex(original, "]")
			prefix := original[:strings.Index(original, "[")+1]
			if strings.Contains(body, "Old") {
				prefix = strings.TrimRight(original[:end], " \r\n\t") + "," + original[len(strings.TrimRight(original[:end], " \r\n\t")):end]
			}
			if !bytes.HasPrefix(got, []byte(prefix+"\n  {\"name\":\"New\",\"url\":\"https://example.com/new\",\"layer\":\"\",\"lang\":\"en\",\"category\":\"\",\"note\":")) || !bytes.HasSuffix(got, []byte("\n"+original[end:])) {
				t.Fatalf("整形が変わった: %q", got)
			}
			if _, err := ParseFeeds(got, path); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestAddFeedsLayers(t *testing.T) {
	for _, layer := range []string{"all", "", "daily"} {
		for _, genre := range []string{"開発", "一般ニュース（日本）", "一般ニュース（海外）"} {
			path := filepath.Join(t.TempDir(), "feeds.json")
			if err := os.WriteFile(path, []byte("[]"), 0600); err != nil {
				t.Fatal(err)
			}
			e := CatalogEntry{Name: "Example", URL: "https://example.com/rss", Genre: genre}
			if _, _, err := AddFeeds(path, []CatalogEntry{e}, []FeedRequest{{URL: e.URL}}, layer, "2026-09-13"); err != nil {
				t.Fatal(err)
			}
			ss, err := LoadFeeds(path)
			if err != nil {
				t.Fatal(err)
			}
			want := layer
			if want == "all" {
				want = ""
			}
			if e.IsGeneralNews() {
				want = LayerGeneralNews
			}
			if ss[0].Layer != want {
				t.Fatalf("layer=%q genre=%q got=%q", layer, genre, ss[0].Layer)
			}
		}
	}
}

func TestAddFeedsFailures(t *testing.T) {
	e := CatalogEntry{Name: "Example", URL: "https://example.com/rss"}
	path := filepath.Join(t.TempDir(), "feeds.json")
	reqs := []FeedRequest{{URL: e.URL}}
	if _, _, err := AddFeeds(path, []CatalogEntry{e}, reqs, "", "2026-09-13"); err == nil {
		t.Fatal("missing accepted")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing file created")
	}
	for _, bad := range []string{"null", "[", `[{"name":"Old","url":"https://example.com/old","unknown":true}]`} {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := AddFeeds(path, []CatalogEntry{e}, reqs, "", "2026-09-13"); err == nil {
			t.Fatalf("accepted: %s", bad)
		}
		if got, _ := os.ReadFile(path); string(got) != bad {
			t.Fatal("invalid file changed")
		}
	}
	if err := os.WriteFile(path, []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	orig := writeAtomic
	t.Cleanup(func() { writeAtomic = orig })
	writeAtomic = func(string, []byte, fs.FileMode) error { return errors.New("disk full") }
	if added, _, err := AddFeeds(path, []CatalogEntry{e}, reqs, "", "2026-09-13"); err == nil || len(added) != 0 {
		t.Fatalf("added=%v err=%v", added, err)
	}
	if got, _ := os.ReadFile(path); string(got) != "[]" {
		t.Fatal("failed write changed file")
	}
}

func TestAddFeedsPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix の権限のみ")
	}
	path := filepath.Join(t.TempDir(), "feeds.json")
	if err := os.WriteFile(path, []byte("[]"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	e := CatalogEntry{Name: "Example", URL: "https://example.com/rss"}
	if _, _, err := AddFeeds(path, []CatalogEntry{e}, []FeedRequest{{URL: e.URL}}, "", "2026-09-13"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0640 {
		t.Fatalf("mode=%v err=%v", fi, err)
	}
}

func TestAddFeedsRevalidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feeds.json")
	initial := []byte("[\n]\n")
	if err := os.WriteFile(path, initial, 0600); err != nil {
		t.Fatal(err)
	}
	// 呼び出し側から渡された目録の行にも、保存前の ParseFeeds を適用する。
	e := CatalogEntry{URL: "https://example.com/rss"}
	orig := writeAtomic
	t.Cleanup(func() { writeAtomic = orig })
	writeAtomic = func(string, []byte, fs.FileMode) error { t.Fatal("検証失敗なのに書き込んだ"); return nil }
	added, _, err := AddFeeds(path, []CatalogEntry{e}, []FeedRequest{{URL: e.URL}}, "daily", "2026-09-13")
	if err == nil || len(added) != 0 {
		t.Fatalf("added=%v err=%v", added, err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, initial) {
		t.Fatal("original changed")
	}
}

func TestAddFeedsKeepsAmpersand(t *testing.T) {
	// 検索フィードの URL の & を \u0026 にしない(利用者が手で書くファイルに合わせる。2026-09-13 の実物確認で見つけた)
	dir := t.TempDir()
	path := filepath.Join(dir, "feeds.json")
	if err := os.WriteFile(path, []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := AddFeeds(path, Catalog(), []FeedRequest{{URL: SearchFeedURL("claude"), Query: "claude"}}, "daily", "2026-09-13"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if bytes.Contains(b, []byte(`\u0026`)) || !bytes.Contains(b, []byte("&hl=")) {
		t.Fatalf("& がエスケープされている:\n%s", b)
	}
}
