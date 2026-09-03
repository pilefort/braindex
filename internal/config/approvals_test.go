package config

import (
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/approvals"
)

// approvals 節を読む。省略した項目はゼロ値のまま(既定は approvals.Settings.WithDefaults が埋める)。
func TestLoad_ApprovalsSection(t *testing.T) {
	p := write(t, `{"root": "..", "approvals": {"file": "work/判断待ち.md", "decisions": "docs/決定.md", "timeout_sec": 300}}`)
	cfg, found, err := Load(p)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	want := approvals.Settings{File: "work/判断待ち.md", Decisions: "docs/決定.md", TimeoutSec: 300}
	if cfg.Approvals != want {
		t.Errorf("approvals 節の読み取りが不正: %+v want %+v", cfg.Approvals, want)
	}

	// 節ごと省略したら WithDefaults が既定を埋める
	p = write(t, `{"root": ".."}`)
	cfg, _, err = Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := cfg.Approvals.WithDefaults()
	if got.File != approvals.DefaultFile || got.Decisions != approvals.DefaultDecisions || got.TimeoutSec != 0 {
		t.Errorf("既定が埋まらない: %+v", got)
	}

	// timeout_sec の 0 は「無期限」。明示しても既定と同じ意味なので読めるだけでよい
	p = write(t, `{"root": "..", "approvals": {"timeout_sec": 0}}`)
	cfg, _, err = Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.Approvals.Validate(); err != nil {
		t.Errorf("0(無期限)が弾かれた: %v", err)
	}

	// 負の timeout_sec は Validate で弾く(JSON としては読める)
	p = write(t, `{"root": "..", "approvals": {"timeout_sec": -1}}`)
	cfg, _, err = Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.Approvals.Validate(); err == nil {
		t.Error("負の timeout_sec を通した")
	}

	// approvals 節の中の未知キーもエラー
	p = write(t, `{"root": "..", "approvals": {"files": "a.md"}}`)
	if _, _, err := Load(p); err == nil || !strings.Contains(err.Error(), "files") {
		t.Errorf("未知キーがエラーにならない: %v", err)
	}
}
