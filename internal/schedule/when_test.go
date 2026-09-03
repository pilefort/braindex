package schedule

import (
	"strings"
	"testing"
	"time"
)

func TestParseWhen(t *testing.T) {
	ok := []struct {
		in   string
		want When
	}{
		{"daily:09:00", When{Hour: 9}},
		{"daily:00:00", When{}},
		{"daily:23:59", When{Hour: 23, Minute: 59}},
		{"weekly:mon:09:00", When{Weekly: true, Weekday: time.Monday, Hour: 9}},
		{"weekly:sun:07:05", When{Weekly: true, Weekday: time.Sunday, Hour: 7, Minute: 5}},
		{"weekly:sat:18:30", When{Weekly: true, Weekday: time.Saturday, Hour: 18, Minute: 30}},
	}
	for _, c := range ok {
		got, err := ParseWhen(c.in)
		if err != nil {
			t.Errorf("ParseWhen(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseWhen(%q): got=%+v want=%+v", c.in, got, c.want)
		}
	}
}

// 誤りは既定に丸めず、何が悪いか分かる文でエラーにする。
func TestParseWhen_Errors(t *testing.T) {
	bad := []struct {
		in   string
		want string // エラー文に含まれる語
	}{
		{"", "daily:HH:MM"},
		{"weekly:mon:09:00:00", "daily:HH:MM"},
		{"0 9 * * 1", "cron 式は受けない"},
		{"*/15 * * * *", "cron 式は受けない"},
		{"hourly:09:00", "daily:HH:MM"},
		{"weekly:monday:09:00", "曜日は"},
		{"weekly:Mon:09:00", "曜日は"},
		{"daily:9:00", "2 桁ずつ"},
		{"daily:09:0", "2 桁ずつ"},
		{"daily:25:00", "時は 00〜23"},
		{"daily:09:60", "分は 00〜59"},
		{"daily:ab:00", "時は 00〜23"},
		{"daily:09:cd", "分は 00〜59"},
		{"daily: 9:00", "時は 00〜23"},
	}
	for _, c := range bad {
		_, err := ParseWhen(c.in)
		if err == nil {
			t.Errorf("ParseWhen(%q): エラーにする", c.in)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("ParseWhen(%q): エラー文に %q を含める: %v", c.in, c.want, err)
		}
	}
}

func TestWhen_CronFields(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"daily:09:00", "0 9 * * *"},
		{"daily:07:05", "5 7 * * *"},
		{"weekly:mon:09:00", "0 9 * * 1"},
		{"weekly:sun:09:05", "5 9 * * 0"},
		{"weekly:sat:23:59", "59 23 * * 6"},
	}
	for _, c := range cases {
		w, err := ParseWhen(c.in)
		if err != nil {
			t.Fatalf("ParseWhen(%q): %v", c.in, err)
		}
		if got := w.CronFields(); got != c.want {
			t.Errorf("CronFields(%q): got=%q want=%q", c.in, got, c.want)
		}
	}
}

func TestWhen_SchtasksArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"daily:09:00", []string{"/SC", "DAILY", "/ST", "09:00"}},
		{"weekly:mon:09:00", []string{"/SC", "WEEKLY", "/D", "MON", "/ST", "09:00"}},
		{"weekly:thu:07:05", []string{"/SC", "WEEKLY", "/D", "THU", "/ST", "07:05"}},
		{"weekly:sun:23:59", []string{"/SC", "WEEKLY", "/D", "SUN", "/ST", "23:59"}},
	}
	for _, c := range cases {
		w, err := ParseWhen(c.in)
		if err != nil {
			t.Fatalf("ParseWhen(%q): %v", c.in, err)
		}
		got := w.SchtasksArgs()
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("SchtasksArgs(%q): got=%v want=%v", c.in, got, c.want)
		}
	}
}
