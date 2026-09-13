// Package links extracts and ranks local note connections without reading target files.
package links

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
)

type Kind string

const (
	KindLink    Kind = "link"
	KindWiki    Kind = "wiki"
	KindMention Kind = "mention"
	FileName         = "links.tsv"
)

type Edge struct {
	From, To string
	Kind     Kind
}
type Unresolved struct{ Link, Wiki, Mention int }
type Ref struct {
	Target string
	Kind   Kind
}
type Note struct{ Rel, Repo string }

var (
	markdown   = regexp.MustCompile(`!?\[(?:[^\[\]]|\[[^\]]*\])*\]\(\s*<?([^()\s<>]*(?:\([^()\s]*\)[^()\s<>]*)*)>?(?:\s+["'(][^)]*["')])?\s*\)`)
	wiki       = regexp.MustCompile(`!?\[\[([^\[\]\n]+?)\]\]`)
	scheme     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
	urlPattern = regexp.MustCompile(`https?://[^\s<>]+`)
	mention    = regexp.MustCompile(`(?:\.{1,2}/|[\w\-぀-ヿ一-鿿×＿]+/)*[\w\-぀-ヿ一-鿿×＿.]+\.md`)
)

// Extract keeps inline-code mentions, but ignores fenced code and inline-code links.
func Extract(content []byte) []Ref {
	s := strings.ReplaceAll(strings.TrimPrefix(string(content), "\ufeff"), "\r\n", "\n")
	var body strings.Builder
	var fence byte
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			if fence == 0 {
				fence = line[0]
			} else if line[0] == fence {
				fence = 0
			}
			continue
		}
		if fence == 0 {
			body.WriteString(line)
			body.WriteByte('\n')
		}
	}
	s = body.String()
	plain := maskInline(s)
	out := []Ref{}
	seen := map[Ref]bool{}
	add := func(target string, kind Kind) {
		r := Ref{target, kind}
		if target != "" && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	for _, m := range markdown.FindAllStringSubmatch(plain, -1) {
		if m[0][0] == '!' {
			continue
		}
		t := m[1]
		if scheme.MatchString(t) || strings.HasPrefix(t, "#") {
			continue
		}
		if i := strings.IndexAny(t, "#?"); i >= 0 {
			t = t[:i]
		}
		t, err := url.PathUnescape(t)
		if err == nil && strings.HasSuffix(t, ".md") {
			add(t, KindLink)
		}
	}
	for _, m := range wiki.FindAllStringSubmatch(plain, -1) {
		if m[0][0] == '!' {
			continue
		}
		t := m[1]
		if i := strings.IndexAny(t, "|#"); i >= 0 {
			t = t[:i]
		}
		add(strings.TrimSuffix(strings.TrimSpace(t), ".md"), KindWiki)
	}
	// Remove markup even inside inline code so a link cannot also become a mention.
	s = markdown.ReplaceAllString(s, " ")
	s = wiki.ReplaceAllString(s, " ")
	s = urlPattern.ReplaceAllString(s, " ")
	for _, m := range mention.FindAllStringIndex(s, -1) {
		if m[0] > 0 && (asciiAlnum(s[m[0]-1]) || strings.ContainsRune("./-", rune(s[m[0]-1]))) {
			continue
		}
		if m[1] < len(s) && asciiAlnum(s[m[1]]) {
			continue
		}
		add(s[m[0]:m[1]], KindMention)
	}
	return out
}

func asciiAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func maskInline(s string) string {
	b := []byte(s)
	for i := 0; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		start := i
		for i < len(s) && s[i] == '`' {
			i++
		}
		n := i - start
		for j := i; j < len(s); {
			if s[j] != '`' {
				j++
				continue
			}
			end := j
			for end < len(s) && s[end] == '`' {
				end++
			}
			if end-j == n {
				for k := start; k < end; k++ {
					if b[k] != '\n' {
						b[k] = ' '
					}
				}
				i = end
				break
			}
			j = end
		}
	}
	return string(b)
}

func base(s string) string { return strings.ToLower(strings.TrimSuffix(path.Base(s), ".md")) }

// Resolve uses only the supplied notes, never the filesystem.
func Resolve(notes []Note, refs map[string][]Ref) ([]Edge, Unresolved) {
	byPath := map[string]Note{}
	byBase := map[string][]Note{}
	for _, n := range notes {
		if _, ok := byPath[n.Rel]; !ok {
			byPath[n.Rel] = n
			byBase[base(n.Rel)] = append(byBase[base(n.Rel)], n)
		}
	}
	unique := func(name, repo string, same bool) string {
		match := ""
		count := 0
		for _, n := range byBase[base(name)] {
			if (n.Repo == repo) == same {
				match = n.Rel
				count++
			}
		}
		if count == 1 {
			return match
		}
		return ""
	}
	u := Unresolved{}
	seen := map[Edge]bool{}
	for from, rr := range refs {
		n, ok := byPath[from]
		if !ok {
			continue
		}
		seenRefs := map[Ref]bool{}
		for _, r := range rr {
			if seenRefs[r] {
				continue
			}
			seenRefs[r] = true
			to := ""
			exists := func(p string) string {
				if _, ok := byPath[p]; ok {
					return p
				}
				return ""
			}
			switch r.Kind {
			case KindLink:
				if strings.HasPrefix(r.Target, "/") {
					to = exists(path.Join(n.Repo, r.Target))
				} else {
					to = exists(path.Join(path.Dir(from), r.Target))
				}
			case KindWiki:
				to = unique(r.Target, n.Repo, true)
				if to == "" {
					to = unique(r.Target, n.Repo, false)
				}
			case KindMention:
				for _, p := range []string{path.Join(path.Dir(from), r.Target), path.Join(n.Repo, r.Target), path.Clean(r.Target)} {
					if to = exists(p); to != "" {
						break
					}
				}
				if to == "" {
					to = unique(r.Target, n.Repo, true)
				}
			}
			if to == "" {
				switch r.Kind {
				case KindLink:
					u.Link++
				case KindWiki:
					u.Wiki++
				case KindMention:
					u.Mention++
				}
				continue
			}
			if to != from {
				seen[Edge{from, to, r.Kind}] = true
			}
		}
	}
	edges := make([]Edge, 0, len(seen))
	for e := range seen {
		edges = append(edges, e)
	}
	sortEdges(edges)
	return edges, u
}

func sortEdges(edges []Edge) {
	sort.Slice(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Kind < b.Kind
	})
}

// ValidPath reports whether a root-relative path can be represented in the TSV.
func ValidPath(p string) bool { return p != "" && !strings.ContainsAny(p, "\t\r\n") }

func Marshal(edges []Edge) []byte {
	ee := append([]Edge(nil), edges...)
	sortEdges(ee)
	var b strings.Builder
	b.WriteString("from\tto\tkind\n")
	seen := map[Edge]bool{}
	for _, e := range ee {
		if ValidPath(e.From) && ValidPath(e.To) && !seen[e] {
			fmt.Fprintf(&b, "%s\t%s\t%s\n", e.From, e.To, e.Kind)
			seen[e] = true
		}
	}
	return []byte(b.String())
}

func Parse(b []byte) ([]Edge, error) {
	lines := strings.Split(string(b), "\n")
	if len(lines) < 2 || lines[0] != "from\tto\tkind" || lines[len(lines)-1] != "" {
		return nil, fmt.Errorf("links.tsv: ヘッダまたは末尾の改行が不正")
	}
	out := []Edge{}
	seen := map[Edge]bool{}
	for i, line := range lines[1 : len(lines)-1] {
		p := strings.Split(line, "\t")
		if len(p) != 3 || !ValidPath(p[0]) || !ValidPath(p[1]) || (Kind(p[2]) != KindLink && Kind(p[2]) != KindWiki && Kind(p[2]) != KindMention) {
			return nil, fmt.Errorf("links.tsv %d 行目: 不正な辺", i+2)
		}
		e := Edge{p[0], p[1], Kind(p[2])}
		if !seen[e] {
			out = append(out, e)
			seen[e] = true
		}
	}
	sortEdges(out)
	return out, nil
}
