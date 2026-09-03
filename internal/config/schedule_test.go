package config

import (
	"strings"
	"testing"
)

// schedule 節を読む。省略した項目はゼロ値のまま(既定のジョブは schedule.Settings.WithDefaults が埋める)。
func TestLoad_ScheduleSection(t *testing.T) {
	p := write(t, `{"root": "..", "schedule": {"jobs": [{"name": "review", "args": ["review"], "when": "weekly:fri:18:00"}]}}`)
	cfg, found, err := Load(p)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if len(cfg.Schedule.Jobs) != 1 {
		t.Fatalf("schedule 節の読み取りが不正: %+v", cfg.Schedule)
	}
	j := cfg.Schedule.Jobs[0]
	if j.Name != "review" || len(j.Args) != 1 || j.Args[0] != "review" || j.When != "weekly:fri:18:00" {
		t.Errorf("ジョブの読み取りが不正: %+v", j)
	}

	// 節ごと省略したら既定の 2 本になる
	p = write(t, `{"root": ".."}`)
	cfg, _, err = Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Schedule.WithDefaults().Jobs; len(got) != 2 {
		t.Errorf("既定のジョブは 2 本: %+v", got)
	}

	// schedule 節の中の未知キーもエラー
	p = write(t, `{"root": "..", "schedule": {"job": []}}`)
	if _, _, err := Load(p); err == nil || !strings.Contains(err.Error(), "job") {
		t.Errorf("未知キーがエラーにならない: %v", err)
	}
}
