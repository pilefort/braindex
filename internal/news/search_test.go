package news

import (
	"github.com/pilefort/braindex/internal/interest"
	"reflect"
	"testing"
)

func TestSearchFeedURL(t *testing.T) {
	for _, tc := range []struct{ q, encoded, lang, region string }{
		{"compiler", "compiler", "en", "&hl=en-US&gl=US&ceid=US:en"},
		{"two words", "two+words", "en", "&hl=en-US&gl=US&ceid=US:en"},
		{"a&b", "a%26b", "en", "&hl=en-US&gl=US&ceid=US:en"},
		{"日本語", "%E6%97%A5%E6%9C%AC%E8%AA%9E", "ja", "&hl=ja&gl=JP&ceid=JP:ja"},
	} {
		if got := SearchFeedURL(tc.q); got != "https://news.google.com/rss/search?q="+tc.encoded+tc.region {
			t.Errorf("q=%q url=%s", tc.q, got)
		}
		if got := SearchFeedLang(tc.q); got != tc.lang {
			t.Errorf("q=%q lang=%s", tc.q, got)
		}
	}
}

func TestQueryCandidates(t *testing.T) {
	catalog := []CatalogEntry{{Keywords: []string{"catalog"}}}
	p := interest.Profile{Terms: []interest.Term{{Word: "catalog", Weight: 5}, {Word: "registered", Weight: 4}, {Word: "alpha", Weight: 3}, {Word: "bravo", Weight: 3}, {Word: "charlie", Weight: 2}, {Word: "zero", Weight: 0}, {Word: "negative", Weight: -1}}}
	feeds := []Source{{URL: SearchFeedURL("registered") + "/"}}
	for _, tc := range []struct {
		top  int
		want []string
	}{{2, []string{"alpha", "bravo"}}, {0, []string{"alpha", "bravo", "charlie"}}, {10, []string{"alpha", "bravo", "charlie"}}} {
		got := QueryCandidates(catalog, p, feeds, tc.top)
		if !reflect.DeepEqual(got, tc.want) || !reflect.DeepEqual(got, QueryCandidates(catalog, p, feeds, tc.top)) {
			t.Fatalf("top=%d got=%v", tc.top, got)
		}
	}
}
