package feed

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestNormalizeLink(t *testing.T) {
	cases := map[string]string{
		"https://a.com/x?utm_source=rss&utm_medium=feed": "https://a.com/x",
		"https://a.com/x?ref=hn":                         "https://a.com/x",
		"https://a.com/x/":                               "https://a.com/x",
		"https://a.com/x?id=123":                         "https://a.com/x?id=123",
		"https://a.com/x?utm_source=x&id=1":              "https://a.com/x?id=1", // 先頭の追跡パラメータを消しても "?" が残る
		"https://a.com/x?id=1&utm_source=x&b=2":          "https://a.com/x?id=1&b=2",
		"https://a.com/x?b=2&a=1":                        "https://a.com/x?b=2&a=1", // 順序は保つ
		"https://a.com/x#section":                        "https://a.com/x",
		"https://a.com/x?refresh=1":                      "https://a.com/x?refresh=1", // ref の前方一致では消さない
		"  https://a.com/x  ":                            "https://a.com/x",
		"":                                               "",
	}
	for in, want := range cases {
		if got := NormalizeLink(in); got != want {
			t.Errorf("NormalizeLink(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEntryID(t *testing.T) {
	if EntryID("https://a.com/x?utm_source=rss", "T") != EntryID("https://a.com/x", "別題") {
		t.Error("追跡パラメータとタイトルの違いで ID が変わる")
	}
	if EntryID("", "A") == EntryID("", "B") {
		t.Error("リンクが無いときタイトルで区別できない")
	}
	if id := EntryID("https://a.com/x", ""); len(id) != 16 {
		t.Errorf("長さ %d(16 桁を期待): %s", len(id), id)
	}
	// sha256 の先頭 16 桁(16 進)。値は python の hashlib.sha256(b"https://example.com/a").hexdigest()[:16] と一致
	if got := EntryID("https://example.com/a", ""); got != "2dce0a4c50441bfc" {
		t.Errorf("EntryID = %s", got)
	}
}

func TestCleanSummary(t *testing.T) {
	raw := "<p>Hello <b>world</b> &amp; more.</p>\n\n  extra   spaces"
	if got := CleanSummary(raw, SummaryLimit); got != "Hello world & more. extra spaces" {
		t.Errorf("got %q", got)
	}
	long := strings.Repeat("あ", 300)
	out := CleanSummary(long, 100)
	if utf8.RuneCountInString(out) != 101 || !strings.HasSuffix(out, "…") {
		t.Errorf("切り詰め: len=%d out=%q", utf8.RuneCountInString(out), out[len(out)-9:])
	}
	if CleanSummary("", SummaryLimit) != "" || CleanSummary("<div></div>", SummaryLimit) != "" {
		t.Error("空が空にならない")
	}
	if got := CleanSummary("abc", 0); got != "abc" {
		t.Errorf("limit 0 で切られた: %q", got)
	}
}

func TestParseDate(t *testing.T) {
	cases := map[string]string{
		"Fri, 14 Aug 2026 10:00:00 GMT":   "2026-08-14",
		"Fri, 14 Aug 2026 23:30:00 -0500": "2026-08-15", // UTC に揃える
		"Fri, 14 Aug 2026 23:30:00 +0000": "2026-08-14",
		"Sat, 1 Aug 2026 10:00:00 +0900":  "2026-08-01",
		"14 Aug 2026 10:00:00 GMT":        "2026-08-14",
		"2026-08-14T10:00:00Z":            "2026-08-14",
		"2026-08-14T01:00:00+09:00":       "2026-08-13",
		"2026-08-14T10:00:00.123+09:00":   "2026-08-14",
		"2026-08-14T10:00:00":             "2026-08-14",
		"2026-08-14":                      "2026-08-14",
		"  2026-08-14T10:00:00Z  ":        "2026-08-14",
		"":                                "",
		"yesterday":                       "",
		"Fri, 14 Aug 2026":                "",
	}
	for in, want := range cases {
		if got := ParseDate(in); got != want {
			t.Errorf("ParseDate(%q) = %q, want %q", in, got, want)
		}
	}
}

// ゾーンの略称(JST・EST など)を含む日付は、実行環境のタイムゾーンに関わらず同じ暦日になる。
// 略称を実行環境のゾーンで解決すると、同じ記事の日付がマシンによって 1 日ずれる。
func TestParseDate_IndependentOfLocalZone(t *testing.T) {
	orig := time.Local
	t.Cleanup(func() { time.Local = orig })
	const in = "Fri, 14 Aug 2026 08:00:00 JST"
	for _, loc := range []*time.Location{
		time.UTC,
		time.FixedZone("JST", 9*60*60),
		time.FixedZone("EST", -5*60*60),
	} {
		time.Local = loc
		if got := ParseDate(in); got != "2026-08-14" {
			t.Errorf("local=%s: ParseDate(%q) = %q, want %q", loc, in, got, "2026-08-14")
		}
	}
}

func TestCleanSummaryMetadataOnly(t *testing.T) {
	for _, raw := range []string{"Article URL: https://example.com/a\nComments URL: https://example.com/c\nPoints: 12\n# Comments: 3", "<p>Article URL: <a href='https://example.com'>https://example.com</a></p><p>Points: 12</p>", "https://example.com/a\n\nhttps://example.com/b"} {
		if got := CleanSummary(raw, 20); got != "" {
			t.Errorf("got %q", got)
		}
	}
	if got := CleanSummary("Points: 12\nActual explanation.", 0); got != "Points: 12 Actual explanation." {
		t.Fatalf("got %q", got)
	}
}
