package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// When は起動の時刻。cron 式は受けない(schtasks に一般変換できないため。SPEC-schedule)。
type When struct {
	Weekly  bool         // false なら毎日
	Weekday time.Weekday // Weekly のときだけ意味を持つ
	Hour    int          // 0〜23
	Minute  int          // 0〜59
}

// weekdays は設定に書ける曜日の綴りと、cron 番号・schtasks の綴り。
// 表の順序が「使える綴り」のエラー文言の順序になるので、月曜始まりで並べる。
var weekdays = []struct {
	key      string
	day      time.Weekday
	schtasks string
}{
	{"mon", time.Monday, "MON"},
	{"tue", time.Tuesday, "TUE"},
	{"wed", time.Wednesday, "WED"},
	{"thu", time.Thursday, "THU"},
	{"fri", time.Friday, "FRI"},
	{"sat", time.Saturday, "SAT"},
	{"sun", time.Sunday, "SUN"},
}

// ParseWhen は "daily:HH:MM" か "weekly:<曜日>:HH:MM" を読む。
func ParseWhen(s string) (When, error) {
	parts := strings.Split(s, ":")
	switch {
	case len(parts) == 3 && parts[0] == "daily":
		h, m, err := parseHM(parts[1], parts[2])
		if err != nil {
			return When{}, err
		}
		return When{Hour: h, Minute: m}, nil
	case len(parts) == 4 && parts[0] == "weekly":
		d, ok := lookupWeekday(parts[1])
		if !ok {
			return When{}, fmt.Errorf("曜日は %s のどれか: %q", weekdayKeys(), parts[1])
		}
		h, m, err := parseHM(parts[2], parts[3])
		if err != nil {
			return When{}, err
		}
		return When{Weekly: true, Weekday: d, Hour: h, Minute: m}, nil
	}
	return When{}, fmt.Errorf(`"daily:HH:MM" か "weekly:<曜日>:HH:MM" と書く(cron 式は受けない): %q`, s)
}

// parseHM は時と分を読む。1 桁や 3 桁、前後の空白は通さない(生成する cron 行と schtasks の /ST を固定幅にするため)。
func parseHM(hs, ms string) (int, int, error) {
	if len(hs) != 2 || len(ms) != 2 {
		return 0, 0, fmt.Errorf("時刻は 2 桁ずつの HH:MM: %q", hs+":"+ms)
	}
	// Atoi は "+9" のような符号付きも受けるので、数字 2 桁だけに絞る
	h, err := strconv.Atoi(hs)
	if err != nil || !allDigits(hs) || h > 23 {
		return 0, 0, fmt.Errorf("時は 00〜23: %q", hs)
	}
	m, err := strconv.Atoi(ms)
	if err != nil || !allDigits(ms) || m > 59 {
		return 0, 0, fmt.Errorf("分は 00〜59: %q", ms)
	}
	return h, m, nil
}

// allDigits は文字列が ASCII の数字だけでできているかを返す。
func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func lookupWeekday(key string) (time.Weekday, bool) {
	for _, w := range weekdays {
		if w.key == key {
			return w.day, true
		}
	}
	return 0, false
}

func weekdayKeys() string {
	keys := make([]string, 0, len(weekdays))
	for _, w := range weekdays {
		keys = append(keys, w.key)
	}
	return strings.Join(keys, "・")
}

// CronFields は cron の前 5 フィールド("分 時 日 月 曜")を返す。
func (w When) CronFields() string {
	dow := "*"
	if w.Weekly {
		dow = strconv.Itoa(int(w.Weekday))
	}
	return fmt.Sprintf("%d %d * * %s", w.Minute, w.Hour, dow)
}

// SchtasksArgs は schtasks の起動条件のフラグ(/SC ...)を返す。
func (w When) SchtasksArgs() []string {
	if w.Weekly {
		return []string{"/SC", "WEEKLY", "/D", w.schtasksDay(), "/ST", w.Clock()}
	}
	return []string{"/SC", "DAILY", "/ST", w.Clock()}
}

func (w When) schtasksDay() string {
	for _, x := range weekdays {
		if x.day == w.Weekday {
			return x.schtasks
		}
	}
	return "MON"
}

// Clock は "HH:MM" を返す。
func (w When) Clock() string {
	return fmt.Sprintf("%02d:%02d", w.Hour, w.Minute)
}
