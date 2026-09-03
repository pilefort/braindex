package config

import (
	"strings"
	"testing"
)

// news 節を読む。省略した項目はゼロ値のまま(既定値は news.Settings.WithDefaults が埋める)。
func TestLoad_NewsSection(t *testing.T) {
	p := write(t, `{"root": "..", "news": {"dir": "n", "cap_per_layer": {"daily": 5}, "profile_days": 7, "sessions_dir": "logs"}}`)
	cfg, found, err := Load(p)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if cfg.News.Dir != "n" || cfg.News.Feeds != "" || cfg.News.SeenDays != 0 || cfg.News.CapPerLayer["daily"] != 5 {
		t.Errorf("news 節の読み取りが不正: %+v", cfg.News)
	}
	s := cfg.News.WithDefaults()
	if s.Dir != "n" || s.Feeds != "news/feeds.json" || s.SeenDays != 90 || s.Cap("daily") != 5 || s.Cap("weekly") != 20 || s.ProfileDays != 7 || s.SessionsDir != "logs" {
		t.Errorf("既定値の補完が不正: %+v", s)
	}

	// show_min_score: 0 は「全件を主要表示」の指定として読めること(未設定と区別する)
	p = write(t, `{"root": "..", "news": {"show_min_score": 0}}`)
	cfg, _, err = Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.News.ShowMinScore == nil || *cfg.News.ShowMinScore != 0 {
		t.Errorf("show_min_score: 0 が読めていない: %v", cfg.News.ShowMinScore)
	}
	if got := cfg.News.WithDefaults().MinScore(); got != 0 {
		t.Errorf("0 が既定に置き換わっている: %d", got)
	}
	p = write(t, `{"root": ".."}`)
	cfg, _, err = Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.News.ShowMinScore != nil {
		t.Errorf("未設定は nil のまま: %v", *cfg.News.ShowMinScore)
	}

	// news 節の中の未知キーもエラー
	p = write(t, `{"root": "..", "news": {"seen_day": 7}}`)
	if _, _, err := Load(p); err == nil || !strings.Contains(err.Error(), "seen_day") {
		t.Errorf("未知キーがエラーにならない: %v", err)
	}
}
