package config

import (
	"strings"
	"testing"
)

// review 節を読む。省略した項目はゼロ値のまま(既定値は review.Settings.WithDefaults が埋める)。
func TestLoad_ReviewSection(t *testing.T) {
	p := write(t, `{"root": "..", "review": {"dir": "reviews", "since_days": 7}}`)
	cfg, found, err := Load(p)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if cfg.Review.Dir != "reviews" || cfg.Review.SinceDays != 7 || cfg.Review.StaleTodoWeeks != 0 || cfg.Review.ArchiveMonths != 0 {
		t.Errorf("review 節の読み取りが不正: %+v", cfg.Review)
	}
	s := cfg.Review.WithDefaults()
	if s.Dir != "reviews" || s.SinceDays != 7 || s.StaleTodoWeeks != 4 || s.ArchiveMonths != 6 {
		t.Errorf("既定値の補完が不正: %+v", s)
	}

	// review 節の中の未知キーもエラー
	p = write(t, `{"root": "..", "review": {"since_day": 7}}`)
	if _, _, err := Load(p); err == nil || !strings.Contains(err.Error(), "since_day") {
		t.Errorf("未知キーがエラーにならない: %v", err)
	}
}
