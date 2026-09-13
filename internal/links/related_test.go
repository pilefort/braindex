package links

import (
	"github.com/pilefort/braindex/internal/indexdata"
	"testing"
)

func TestRank_WeightsFieldsAndOrdering(t *testing.T) {
	entries := []indexdata.Entry{
		{Repo: "r", Path: "r/a.md", Title: "Alpha"}, {Repo: "r", Path: "r/b.md", Summary: "alpha"},
		{Repo: "r", Path: "r/c.md", Title: "beta"}, {Repo: "r", Path: "r/d.md", Summary: "alpha", Date: "2026-01-01"},
		{Repo: "r", Path: "r/e.md", Summary: "alpha", Date: "2026-02-01"}, {Repo: "r", Path: "r/f.md", Summary: "alpha", Date: "2026-02-01"},
		{Repo: "other", Path: "other/alpha.md"},
	}
	r := Rank(entries, nil, []Term{{Word: "alpha", Weight: 2}, {Word: "beta", Weight: 1}}, "r", RankOptions{})
	want := []string{"r/a.md", "r/e.md", "r/f.md", "r/d.md", "r/b.md", "r/c.md"}
	for i, p := range want {
		if r.ThisRepo[i].Path != p {
			t.Fatalf("rank=%+v", r.ThisRepo)
		}
	}
	if r.ThisRepo[0].Score != 4 || r.OtherRepos[0].Score != 2 {
		t.Fatal(r)
	}
	r = Rank(entries, nil, []Term{{Word: "alpha", Weight: 2}}, "r", RankOptions{Limit: 1})
	if len(r.ThisRepo) != 1 || len(r.OtherRepos) != 1 {
		t.Fatal(r)
	}
}

func TestRank_OneHopMentionCapAndNoMention(t *testing.T) {
	entries := []indexdata.Entry{{Repo: "r", Path: "a", Title: "alpha"}, {Repo: "r", Path: "b"}, {Repo: "r", Path: "c"}, {Repo: "r", Path: "d"}, {Repo: "r", Path: "e", Title: "alpha"}}
	edges := []Edge{{"a", "b", KindMention}, {"b", "c", KindLink}, {"d", "a", KindLink}, {"d", "a", KindWiki}, {"d", "e", KindLink}, {"d", "e", KindWiki}, {"a", "b", KindMention}}
	for _, noMention := range []bool{false, true} {
		r := Rank(entries, edges, []Term{{Word: "alpha", Weight: 2}}, "r", RankOptions{NoMention: noMention})
		byPath := map[string]Ranked{}
		for _, v := range r.ThisRepo {
			byPath[v.Path] = v
		}
		if _, ok := byPath["c"]; ok {
			t.Fatal("two-hop row appeared")
		}
		if byPath["d"].Score != 3 {
			t.Fatal("cap", byPath["d"])
		}
		if noMention {
			if _, ok := byPath["b"]; ok {
				t.Fatal("mention counted")
			}
		} else if byPath["b"].Score != 0.5 {
			t.Fatal(byPath["b"])
		}
	}
}
