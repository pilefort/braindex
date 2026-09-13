package links

import (
	"path"
	"sort"
	"strings"

	"github.com/pilefort/braindex/internal/indexdata"
)

type Term struct {
	Word    string   `json:"word"`
	Weight  int      `json:"weight"`
	Sources []string `json:"sources"`
}
type RankOptions struct {
	Limit     int
	NoMention bool
}
type WordMatch struct {
	Word   string   `json:"word"`
	Weight int      `json:"weight"`
	Where  []string `json:"where"`
}

// LinkCounts records contributing edges before the total connection score is capped at 3.
type LinkCounts struct {
	Link    int `json:"link"`
	Wiki    int `json:"wiki"`
	Mention int `json:"mention"`
}

func (c LinkCounts) Score() float64 { return min(3, float64(c.Link+c.Wiki)+float64(c.Mention)*0.5) }

type Ranked struct {
	Path  string      `json:"path"`
	Repo  string      `json:"repo"`
	Title string      `json:"title"`
	Date  string      `json:"date"`
	Score float64     `json:"score"`
	Words []WordMatch `json:"words"`
	Links LinkCounts  `json:"links"`
}
type Ranking struct {
	ThisRepo   []Ranked `json:"this_repo"`
	OtherRepos []Ranked `json:"other_repos"`
}

func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

// Rank freezes the lexical seed set before adding any connection points.
func Rank(entries []indexdata.Entry, edges []Edge, terms []Term, thisRepo string, opt RankOptions) Ranking {
	rows := make([]Ranked, len(entries))
	positions := map[string]int{}
	seeds := map[string]bool{}
	for i, e := range entries {
		r := Ranked{Path: e.Path, Repo: e.Repo, Title: e.Title, Date: e.Date, Words: []WordMatch{}}
		for _, t := range terms {
			if t.Word == "" || t.Weight <= 0 {
				continue
			}
			m := WordMatch{Word: t.Word, Weight: t.Weight, Where: []string{}}
			for _, field := range []struct {
				name, text string
				multiplier int
			}{{"title", e.Title, 2}, {"summary", e.Summary, 1}, {"path", path.Base(e.Path), 1}} {
				if strings.Contains(lowerASCII(field.text), lowerASCII(t.Word)) {
					r.Score += float64(t.Weight * field.multiplier)
					m.Where = append(m.Where, field.name)
				}
			}
			if len(m.Where) > 0 {
				r.Words = append(r.Words, m)
			}
		}
		rows[i] = r
		positions[e.Path] = i
		seeds[e.Path] = r.Score > 0
	}
	seen := map[Edge]bool{}
	for _, e := range edges {
		if seen[e] || e.From == e.To || opt.NoMention && e.Kind == KindMention {
			continue
		}
		seen[e] = true
		add := func(target, source string) {
			i, ok := positions[target]
			if !ok || !seeds[source] {
				return
			}
			switch e.Kind {
			case KindLink:
				rows[i].Links.Link++
			case KindWiki:
				rows[i].Links.Wiki++
			case KindMention:
				rows[i].Links.Mention++
			}
		}
		add(e.From, e.To)
		add(e.To, e.From)
	}
	out := Ranking{ThisRepo: []Ranked{}, OtherRepos: []Ranked{}}
	for _, r := range rows {
		r.Score += r.Links.Score()
		if r.Score <= 0 {
			continue
		}
		if r.Repo == thisRepo {
			out.ThisRepo = append(out.ThisRepo, r)
		} else {
			out.OtherRepos = append(out.OtherRepos, r)
		}
	}
	order := func(rr []Ranked) []Ranked {
		sort.Slice(rr, func(i, j int) bool {
			a, b := rr[i], rr[j]
			if a.Score != b.Score {
				return a.Score > b.Score
			}
			if a.Date != b.Date {
				return a.Date > b.Date
			}
			return a.Path < b.Path
		})
		if opt.Limit > 0 && len(rr) > opt.Limit {
			rr = rr[:opt.Limit]
		}
		return rr
	}
	out.ThisRepo = order(out.ThisRepo)
	out.OtherRepos = order(out.OtherRepos)
	return out
}
