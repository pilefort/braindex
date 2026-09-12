package news

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/feed"
)

func TestAppendKeeps_DuplicateSelectionLinks(t *testing.T) {
	p := filepath.Join(t.TempDir(), "keep.md")
	k := Keep{Title: "first", Link: "https://example.com/a"}
	n, err := appendKeeps(p, "2026-09", []Keep{k, {Title: "second", Link: k.Link}}, "2026-09-01", "daily")
	if err != nil || n != 1 {
		t.Fatalf("added=%d err=%v", n, err)
	}
	b, err := os.ReadFile(p)
	if err != nil || strings.Count(string(b), k.Link) != 1 || strings.Contains(string(b), "second") {
		t.Fatalf("body=%s err=%v", b, err)
	}
}

func TestMarkdown_LinkDestinations(t *testing.T) {
	for _, link := range []string{"https://example.com/a", "https://example.com/a(b)", "https://example.com/a b"} {
		t.Run(link, func(t *testing.T) {
			dest := link
			if strings.ContainsAny(link, "() ") {
				dest = "<" + link + ">"
			}
			want := "[Title](" + dest + ")"
			k := Keep{Title: "Title", Link: link}
			if got := KeepMarkdown([]Keep{k}, "2026-09-01", "daily"); !strings.Contains(got, want) {
				t.Errorf("keep=%s want %s", got, want)
			}
			rs := []Result{{Source: Source{Name: "Feed"}, New: []feed.Entry{{Title: "Title", Link: link}}}}
			if got := string(Digest(rs, DigestOptions{Cap: 10})); !strings.Contains(got, want) {
				t.Errorf("digest=%s want %s", got, want)
			}
			for _, oldDest := range []string{link, dest} {
				p := filepath.Join(t.TempDir(), "keep.md")
				if err := os.WriteFile(p, []byte("- [Title]("+oldDest+")\n"), 0600); err != nil {
					t.Fatal(err)
				}
				n, err := appendKeeps(p, "2026-09", []Keep{k}, "2026-09-01", "daily")
				if err != nil || n != 0 {
					t.Errorf("existing %s: added=%d err=%v", oldDest, n, err)
				}
			}
		})
	}
}
