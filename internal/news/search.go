package news

import (
	"net/url"

	"github.com/pilefort/braindex/internal/interest"
)

// SearchFeedLang は語の文字種で検索の言語を決める。通信はしない。
func SearchFeedLang(q string) string {
	for _, r := range q {
		if r > 127 {
			return "ja"
		}
	}
	return "en"
}

// SearchFeedURL は承認後に登録する検索 RSS の URL を組む。通信はしない。
func SearchFeedURL(q string) string {
	region := "&hl=en-US&gl=US&ceid=US:en"
	if SearchFeedLang(q) == "ja" {
		region = "&hl=ja&gl=JP&ceid=JP:ja"
	}
	return "https://news.google.com/rss/search?q=" + url.QueryEscape(q) + region
}

// QueryCandidates は重み順の関心語から目録に無く未登録の検索語を選ぶ。
// top <= 0 は Suggest と同じく全件。
func QueryCandidates(catalog []CatalogEntry, p interest.Profile, feeds []Source, top int) []string {
	keywords := CatalogKeywords(catalog)
	registered := map[string]bool{}
	for _, f := range feeds {
		registered[normalizeURL(f.URL)] = true
	}
	var out []string
	for _, t := range p.Terms {
		if t.Weight <= 0 || keywords[t.Word] || registered[normalizeURL(SearchFeedURL(t.Word))] {
			continue
		}
		out = append(out, t.Word)
		if top > 0 && len(out) >= top {
			break
		}
	}
	return out
}
