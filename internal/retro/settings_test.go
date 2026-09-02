package retro

import (
	"path/filepath"
	"testing"
)

func TestSettings_WithDefaults(t *testing.T) {
	s := Settings{}.WithDefaults()
	if s.SessionsDir != "" || s.WindowDays != 14 || s.Threshold != 0.08 || s.PositionBins != "1-10,11-30,31-" || s.Dictionary != "" || s.DictionaryExtra != "" {
		t.Errorf("既定値: got=%+v", s)
	}
	full := Settings{SessionsDir: "~/logs", WindowDays: 7, Threshold: 0.2, PositionBins: "1-5,6-", Dictionary: "d.txt", DictionaryExtra: "e.txt"}
	if got := full.WithDefaults(); got != full {
		t.Errorf("指定した値は変えない: got=%+v", got)
	}
}

func TestExpandHome(t *testing.T) {
	home := filepath.Join("h", "someone")
	cases := []struct{ in, want string }{
		{"~", home},
		{"~/logs", filepath.Join(home, "logs")},
		{`~\logs`, filepath.Join(home, "logs")},
		{"logs", "logs"},
		{"~x", "~x"}, // ホームではない
		{"", ""},
	}
	for _, c := range cases {
		if got := ExpandHome(c.in, home); got != c.want {
			t.Errorf("ExpandHome(%q): want=%q got=%q", c.in, c.want, got)
		}
	}
}

func TestResolvePath(t *testing.T) {
	home := filepath.Join("h", "someone")
	base := filepath.Join("hub", "dir")
	abs, _ := filepath.Abs("x")
	cases := []struct{ in, want string }{
		{"~/logs", filepath.Join(home, "logs")},
		{"logs/extra", filepath.Join(base, "logs", "extra")}, // 設定ファイルのディレクトリ基準
		{abs, abs},
		{"", ""},
	}
	for _, c := range cases {
		if got := ResolvePath(c.in, base, home); got != c.want {
			t.Errorf("ResolvePath(%q): want=%q got=%q", c.in, c.want, got)
		}
	}
}
