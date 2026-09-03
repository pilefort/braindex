package template_test

import (
	"path/filepath"
	"testing"

	"github.com/pilefort/braindex/internal/approvals"
	"github.com/pilefort/braindex/internal/config"
	"github.com/pilefort/braindex/internal/template"
)

// hub テンプレの braindex.json に approvals 節があり、config が読めて、既定と一致する。
//
// テンプレに既定と同じ値を書くのは、利用者が「何を設定できるか」をファイルを開いて知れるようにするため
// (schedule 節も同じ扱い)。値がずれると「テンプレのとおりに動かない」になるので、ここで縛る。
func TestHub_ApprovalsSection(t *testing.T) {
	dir := t.TempDir()
	if _, err := template.Install(dir, template.KindHub); err != nil {
		t.Fatalf("Install: %v", err)
	}
	cfg, found, err := config.Load(filepath.Join(dir, "braindex.json"))
	if err != nil || !found {
		t.Fatalf("テンプレの braindex.json を読めない: found=%v err=%v", found, err)
	}
	got := cfg.Approvals
	want := approvals.Settings{
		File:       approvals.DefaultFile,
		Decisions:  approvals.DefaultDecisions,
		TimeoutSec: 0,
	}
	if got != want {
		t.Errorf("テンプレの approvals 節が既定と違う: %+v want %+v", got, want)
	}
	// 節を書いたとおりに読めることの裏取り。WithDefaults を通しても変わらない
	if got.WithDefaults() != want {
		t.Errorf("WithDefaults で値が変わった: %+v", got.WithDefaults())
	}
	if err := got.Validate(); err != nil {
		t.Errorf("テンプレの値が Validate を通らない: %v", err)
	}
}
