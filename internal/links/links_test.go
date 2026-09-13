package links

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExtract_CodeAndExcludedForms(t *testing.T) {
	input := "\ufeff# note\r\n```md\r\n[x](fence.md) [[Fence]] fence.md\r\n```\r\n~~~\n[x](tilde.md)\n~~~\n" +
		"`[x](inline.md) [[Inline]] code.md` ``double.md``\n" +
		"![img](image.md) ![[embed.md]] [x](https://host/a.md) [x](#anchor) [x](other.txt)\n" +
		"[x](../x.md#heading) [x](%E3%81%82.md?q=1) [[Name|label]] [[Name#heading]] plain.md plain.md https://host/url.md /absolute.md suffix.mdA\n"
	got := Extract([]byte(input))
	want := []Ref{{"../x.md", KindLink}, {"あ.md", KindLink}, {"Name", KindWiki}, {"code.md", KindMention}, {"double.md", KindMention}, {"plain.md", KindMention}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v; want %#v", got, want)
	}
}

func TestExtract_ResearchMarkdownPattern(t *testing.T) {
	got := Extract([]byte(`[label [nested]](note(v2).md "title") [label](<other.md> 'title')`))
	want := []Ref{{"note(v2).md", KindLink}, {"other.md", KindLink}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%#v want=%#v", got, want)
	}
}

func fixture(t *testing.T) ([]Note, map[string][]Ref) {
	t.Helper()
	notes := []Note{}
	refs := map[string][]Ref{}
	err := filepath.WalkDir("testdata/root", func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel("testdata/root", p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		notes = append(notes, Note{rel, strings.Split(rel, "/")[0]})
		refs[rel] = Extract(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return notes, refs
}

func TestResolve_FixtureAndDeterminism(t *testing.T) {
	notes, refs := fixture(t)
	edges, u := Resolve(notes, refs)
	want := []Edge{{"alpha/docs/a.md", "alpha/docs/あ.md", KindLink}, {"alpha/docs/a.md", "alpha/x.md", KindLink}, {"alpha/docs/a.md", "alpha/x.md", KindWiki}, {"alpha/docs/a.md", "beta/docs/remote.md", KindWiki}}
	if !reflect.DeepEqual(edges, want) || u != (Unresolved{Wiki: 2}) {
		t.Fatalf("edges=%#v unresolved=%+v", edges, u)
	}
	for i, j := 0, len(notes)-1; i < j; i, j = i+1, j-1 {
		notes[i], notes[j] = notes[j], notes[i]
	}
	again, v := Resolve(notes, refs)
	if !reflect.DeepEqual(edges, again) || u != v {
		t.Fatal("scan order changed output")
	}
}

func TestResolve_MentionPriorityAndSelf(t *testing.T) {
	notes := []Note{{"r/docs/a.md", "r"}, {"r/docs/x.md", "r"}, {"r/x.md", "r"}, {"r/deep/y.md", "r"}, {"other/z.md", "other"}}
	for _, tt := range []struct{ target, want string }{{"x.md", "r/docs/x.md"}, {"docs/x.md", "r/docs/x.md"}, {"other/z.md", "other/z.md"}, {"y.md", "r/deep/y.md"}, {"a.md", ""}, {"missing.md", ""}} {
		t.Run(tt.target, func(t *testing.T) {
			edges, u := Resolve(notes, map[string][]Ref{"r/docs/a.md": {{tt.target, KindMention}, {tt.target, KindMention}}})
			if tt.want == "" {
				if len(edges) != 0 {
					t.Fatal(edges)
				}
			} else if len(edges) != 1 || edges[0].To != tt.want {
				t.Fatal(edges)
			}
			if (u.Mention == 1) != (tt.target == "missing.md") {
				t.Fatal(u)
			}
		})
	}
	edges, _ := Resolve(notes, map[string][]Ref{"r/docs/a.md": {{"/x.md", KindLink}, {"../x.md", KindLink}, {"a.md", KindLink}}})
	if len(edges) != 1 || edges[0].To != "r/x.md" {
		t.Fatal(edges)
	}
}

func TestTSV_RoundTripAndValidation(t *testing.T) {
	for _, edges := range [][]Edge{{}, {{"a.md", "b.md", KindLink}, {"a.md", "c.md", KindMention}}} {
		b := Marshal(edges)
		got, err := Parse(b)
		if err != nil || !reflect.DeepEqual(got, edges) {
			t.Fatalf("%q: %#v %v", b, got, err)
		}
	}
	for _, b := range []string{"", "wrong\n", "from\tto\tkind\na\tb\tunknown\n", "from\tto\tkind\na\tb\tlink", "from\tto\tkind\na\rb\tc\tlink\n"} {
		if _, err := Parse([]byte(b)); err == nil {
			t.Fatalf("accepted %q", b)
		}
	}
	if got := string(Marshal([]Edge{{"bad\tpath", "b.md", KindLink}})); got != "from\tto\tkind\n" {
		t.Fatal(got)
	}
}
