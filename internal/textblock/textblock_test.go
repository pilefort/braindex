package textblock

import (
	"reflect"
	"testing"
)

func TestMerge(t *testing.T) {
	const block = "BEGIN\nnew\nEND\n"
	cases := []struct{ name, existing, want string }{
		{"empty", "", block},
		{"append", "keep\n", "keep\n" + block},
		{"replace", "before\nBEGIN\nold\nEND\nafter\n", "before\n" + block + "after\n"},
		{"idempotent", block, block},
		{"other", "OTHER\nkeep\nOTHER END\n", "OTHER\nkeep\nOTHER END\n" + block},
		{"unterminated", "keep\nBEGIN\nold\nlast", "keep\n" + block},
		{"crlf", "keep\r\n\r\n", "keep\n" + block},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Merge(c.existing, "BEGIN", "END", []string{"new"})
			if got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
			if again := Merge(got, "BEGIN", "END", []string{"new"}); again != got {
				t.Fatal("not idempotent")
			}
		})
	}
}

func TestLinesAndRemove(t *testing.T) {
	if Lines("keep", "BEGIN", "END") != nil {
		t.Fatal("missing must be nil")
	}
	for _, s := range []string{"BEGIN\na\nb\nEND\n", "BEGIN\na\nb"} {
		if got := Lines(s, "BEGIN", "END"); !reflect.DeepEqual(got, []string{"a", "b"}) {
			t.Fatal(got)
		}
		if got := Merge(s, "BEGIN", "END", nil); got != "" {
			t.Fatal(got)
		}
	}
	if got := Merge("keep\r\n\r\n", "BEGIN", "END", nil); got != "keep\n" {
		t.Fatal(got)
	}
}

func TestMergePreservingOutside(t *testing.T) {
	before, after := "一\r\n二\r\n三\r\n\r\n", "四\r\n五\r\n六\n\n"
	got := MergePreservingOutside(before+"BEGIN\r\nold\r\nEND\r\n"+after, "BEGIN", "END", []string{"new"})
	want := before + "BEGIN\nnew\nEND\n" + after
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if MergePreservingOutside(got, "BEGIN", "END", []string{"new"}) != got {
		t.Fatal("not idempotent")
	}
	if got := MergePreservingOutside("keep", "BEGIN", "END", []string{"new"}); got != "keep\nBEGIN\nnew\nEND\n" {
		t.Fatal(got)
	}
}
