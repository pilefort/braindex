package interest

import (
	"fmt"
	"reflect"
	"testing"
)

func TestCommandTerms(t *testing.T) {
	p := Profile{Terms: []Term{
		{Word: "private-index", Weight: 100, Counts: map[string]float64{SourceIndex: 100}},
		{Word: "private-session", Weight: 100, Counts: map[string]float64{SourceSessions: 100}},
		{Word: "extra", Counts: map[string]float64{SourceExtra: 1}},
		{Word: "keep", Counts: map[string]float64{SourceKeep: 2}},
		{Word: "both", Counts: map[string]float64{SourceKeep: 2, SourceExtra: 1, SourceSessions: 1000}},
		{Word: "also", Counts: map[string]float64{SourceKeep: 2, SourceExtra: 1}},
	}}
	want := []string{"also", "both", "keep", "extra"}
	if got := p.CommandTerms(); !reflect.DeepEqual(got, want) {
		t.Fatalf("%v want %v", got, want)
	}
	if p.Terms[0].Word != "private-index" {
		t.Fatal("profile mutated")
	}
	for i := 0; i < 40; i++ {
		p.Terms = append(p.Terms, Term{Word: fmt.Sprintf("z%02d", i), Counts: map[string]float64{SourceExtra: 1}})
	}
	if got := p.CommandTerms(); len(got) != 30 || got[29] != "z25" {
		t.Fatal(got)
	}
}
