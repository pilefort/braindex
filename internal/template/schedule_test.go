package template

import (
	"encoding/json"
	"strings"
	"testing"
)

// hub テンプレの braindex.json は schedule 節を持ち、README がその登録手順を案内する。
// 節を省いても既定の 2 本になるが、明示していないと利用者は時刻を変える場所に気づけない。
func TestHub_ScheduleSection(t *testing.T) {
	files, err := Files(KindHub)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]string{}
	for _, f := range files {
		byPath[f.Path] = string(f.Content)
	}
	var cfg struct {
		Schedule struct {
			Jobs []struct {
				Name string   `json:"name"`
				Args []string `json:"args"`
				When string   `json:"when"`
			} `json:"jobs"`
		} `json:"schedule"`
	}
	if err := json.Unmarshal([]byte(byPath["braindex.json"]), &cfg); err != nil {
		t.Fatalf("braindex.json を読めない: %v", err)
	}
	if len(cfg.Schedule.Jobs) != 2 {
		t.Fatalf("schedule.jobs は 2 本(review・retro): %+v", cfg.Schedule.Jobs)
	}
	want := []struct {
		name, when, args string
	}{
		{"review", "weekly:mon:09:00", "review"},
		{"retro", "weekly:mon:09:05", "retro check"},
	}
	for i, w := range want {
		got := cfg.Schedule.Jobs[i]
		if got.Name != w.name || got.When != w.when || strings.Join(got.Args, " ") != w.args {
			t.Errorf("%d 本目: got=%+v want=%v", i, got, w)
		}
	}

	readme := byPath["README.md"]
	for _, w := range []string{"braindex schedule install", "braindex schedule print", "`schedule` 節"} {
		if !strings.Contains(readme, w) {
			t.Errorf("hub テンプレの README に %q が無い", w)
		}
	}
	// 手で cron / schtasks を書く旧案内は残さない(CLI で登録できるようになったため)
	for _, bad := range []string{"schtasks /Create", "0 9 * * 1"} {
		if strings.Contains(readme, bad) {
			t.Errorf("hub テンプレの README に手書きの登録例 %q が残っている", bad)
		}
	}
}
