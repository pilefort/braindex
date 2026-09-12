package interest

import (
	"strings"
	"testing"
	"time"
)

func TestProfileMarshal_ExtraPipe(t *testing.T) {
	since := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	p, err := Build(Input{Today: "2026-09-01", Since: since, Until: since.AddDate(0, 0, 1), Extra: []string{"あ|い"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(p.Marshal(0)); !strings.Contains(got, `| あ\|い |`) {
		t.Fatalf("table=%s", got)
	}
}

func TestParseKeep_AngledDestination(t *testing.T) {
	got := ParseKeep("2026-09", "- [Title](<https://example.com/a b(c)>)\n")
	if len(got) != 1 || got[0].Title != "Title" {
		t.Fatalf("keeps=%v", got)
	}
}

func TestProfileMarshal_NoCLIFlag(t *testing.T) {
	p := Profile{Terms: []Term{{Word: "alpha"}, {Word: "beta"}}}
	got := string(p.Marshal(1))
	if strings.Contains(got, "-top") || !strings.Contains(got, "残り 1 語") {
		t.Fatalf("table=%s", got)
	}
}
