package retro

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSettings_WithDefaults(t *testing.T) {
	s := Settings{}.WithDefaults()
	if s.SessionsDir != "" || s.WindowDays != 14 || s.Threshold != 0.08 || s.PositionBins != "1-3,4-10,11-30,31-" || s.Dictionary != "" || s.DictionaryExtra != "" || s.Baseline() != 8 {
		t.Errorf("既定値: got=%+v", s)
	}
	weeks := 3
	full := Settings{SessionsDir: "~/logs", WindowDays: 7, Threshold: 0.2, BaselineWeeks: &weeks, PositionBins: "1-5,6-", Dictionary: "d.txt", DictionaryExtra: "e.txt"}
	if got := full.WithDefaults(); got != full {
		t.Errorf("指定した値は変えない: got=%+v", got)
	}
}

// 範囲外の値は既定値に丸めず、設定の誤りとしてエラーにする(0 は「未指定」で既定値)。
func TestSettings_Validate(t *testing.T) {
	ok := []Settings{{}, {WindowDays: 7, Threshold: 0.5}, {Threshold: 1}, {Threshold: 0.001}}
	for _, s := range ok {
		if err := s.Validate(); err != nil {
			t.Errorf("Validate(%+v): 通るはず: %v", s, err)
		}
	}
	bad := []struct {
		s    Settings
		want string // エラー文に含まれるキー名
	}{
		{Settings{Threshold: 5}, "threshold"},
		{Settings{Threshold: 1.01}, "threshold"},
		{Settings{Threshold: -1}, "threshold"},
		{Settings{WindowDays: -3}, "window_days"},
	}
	for _, c := range bad {
		err := c.s.Validate()
		if err == nil {
			t.Errorf("Validate(%+v): エラーにする", c.s)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("Validate(%+v): エラー文にキー名 %q が無い: %v", c.s, c.want, err)
		}
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
	// ホームが分からない(空)ときは展開しない(カレント相対の "logs" に化けさせない)
	for _, in := range []string{"~", "~/logs", `~\logs`} {
		if got := ExpandHome(in, ""); got != in {
			t.Errorf("ExpandHome(%q, \"\"): 展開しないはず: got=%q", in, got)
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

// 既定値そのものが自分の検査を通ることを守る(既定を変えたときに、解析できない区間や範囲外の閾値を入れてしまう事故を防ぐ)。
func TestDefaults_AreValid(t *testing.T) {
	s := Settings{}.WithDefaults()
	if err := s.Validate(); err != nil {
		t.Errorf("既定値が Validate を通らない: %v", err)
	}
	bins, err := ParseBins(DefaultPositionBins)
	if err != nil {
		t.Fatalf("DefaultPositionBins %q が解析できない: %v", DefaultPositionBins, err)
	}
	if len(bins) != 4 || bins[0].Lo != 1 || bins[len(bins)-1].Hi != 0 {
		t.Errorf("DefaultPositionBins の解析結果: %+v", bins)
	}
}

// baseline_weeks は 0 が「基準を使わない」なので、書かれていない(nil)ときだけ既定で埋める。
func TestSettings_BaselineWeeks(t *testing.T) {
	zero := 0
	three := 3
	neg := -1
	cases := []struct {
		desc string
		in   *int
		want int
	}{
		{"書かれていなければ既定", nil, DefaultBaselineWeeks},
		{"0 はそのまま(基準を使わない)", &zero, 0},
		{"指定した値", &three, 3},
	}
	for _, c := range cases {
		if got := (Settings{BaselineWeeks: c.in}).WithDefaults().Baseline(); got != c.want {
			t.Errorf("Baseline[%s]: want=%d got=%d", c.desc, c.want, got)
		}
	}
	if err := (Settings{BaselineWeeks: &neg}).Validate(); err == nil {
		t.Error("負の baseline_weeks がエラーにならない")
	}
	if err := (Settings{BaselineWeeks: &zero}).Validate(); err != nil {
		t.Errorf("0 はエラーにしない: %v", err)
	}
}
