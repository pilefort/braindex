package textutil

import (
	"reflect"
	"testing"
)

func TestSplitLines(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"", []string{""}},
		{"\uFEFF", []string{""}},
		{"\uFEFF一\r\n二\r三\n", []string{"一", "二", "三", ""}},
		{"\n\n", []string{"", "", ""}},
		{"一\uFEFF二", []string{"一\uFEFF二"}},
	} {
		if got := SplitLines([]byte(tc.in)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("SplitLines(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
