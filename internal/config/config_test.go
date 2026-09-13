package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/learn"
)

func TestLoad_LearnSection(t *testing.T) {
	cfg, found, err := Load(write(t, `{"learn":{"min_sessions":5,"max_session_ratio":0.25,"min_corrections":4,"boilerplate_sessions":6}}`))
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	want := learn.Settings{MinSessions: 5, MaxSessionRatio: 0.25, MinCorrections: 4, BoilerplateSessions: 6}
	if cfg.Learn != want {
		t.Fatalf("learn 節: got=%+v want=%+v", cfg.Learn, want)
	}
	for _, raw := range []string{`{}`, `{"learn":{}}`, `{"learn":{"min_sessions":0,"max_session_ratio":0,"min_corrections":0,"boilerplate_sessions":0}}`} {
		cfg, _, err := Load(write(t, raw))
		if err != nil || cfg.Learn.WithDefaults() != (learn.Settings{}).WithDefaults() {
			t.Errorf("省略・0: %s cfg=%+v err=%v", raw, cfg.Learn, err)
		}
	}
	if _, _, err := Load(write(t, `{"learn":{"min_session":5}}`)); err == nil || !strings.Contains(err.Error(), "min_session") {
		t.Errorf("learn 節の未知キー: %v", err)
	}
}

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "braindex.json")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// 無いファイルは found=false でエラーにしない(既定パスの不在は正常)。
func TestLoad_Missing(t *testing.T) {
	cfg, found, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || found {
		t.Fatalf("found=%v err=%v; want false, nil", found, err)
	}
	if cfg.Root != "" || len(cfg.NotesDirs) != 0 {
		t.Errorf("ゼロ値を期待: %+v", cfg)
	}
}

// scan の項目を読む。
func TestLoad_Fields(t *testing.T) {
	p := write(t, `{"root": "..", "repo_depth": 2, "notes_dirs": ["docs/notes", "wiki"],
	  "extra": [{"repo": "r", "path": "x", "recursive": true, "kind": "k", "exclude": ["*.draft.md"]}]}`)
	cfg, found, err := Load(p)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if cfg.Root != ".." || cfg.RepoDepth != 2 || strings.Join(cfg.NotesDirs, ",") != "docs/notes,wiki" {
		t.Errorf("読み取り結果が不正: %+v", cfg)
	}
	// repo_depth を省略すると 0 のまま読め、有効な段数は 1(既定)
	omitted, _, err := Load(write(t, `{"root": ".."}`))
	if err != nil || omitted.RepoDepth != 0 || omitted.Depth() != 1 {
		t.Errorf("repo_depth 省略: err=%v RepoDepth=%d Depth=%d", err, omitted.RepoDepth, omitted.Depth())
	}
	if len(cfg.Extra) != 1 || cfg.Extra[0].Kind != "k" || !cfg.Extra[0].Recursive || cfg.Extra[0].Exclude[0] != "*.draft.md" {
		t.Errorf("extra が不正: %+v", cfg.Extra)
	}
}

// 未知のキーはエラー(打ち間違いを無言で無視しない)。
func TestLoad_UnknownKey(t *testing.T) {
	p := write(t, `{"root": "..", "notes_dir": "wiki"}`)
	_, found, err := Load(p)
	if err == nil || !found {
		t.Fatalf("未知キーでエラーになっていない: found=%v err=%v", found, err)
	}
	if !strings.Contains(err.Error(), "notes_dir") {
		t.Errorf("エラーにキー名が無い: %v", err)
	}
}

// 壊れた JSON と、オブジェクトの後ろに続く余分な内容はエラー。
func TestLoad_BadJSONAndTrailing(t *testing.T) {
	if _, _, err := Load(write(t, `{"root": `)); err == nil {
		t.Errorf("不正な JSON でエラーになっていない")
	}
	_, _, err := Load(write(t, `{"root": "."} trailing-garbage`))
	if err == nil || !strings.Contains(err.Error(), "末尾") {
		t.Errorf("末尾の余分な内容でエラーになっていない: %v", err)
	}
}

// retro 節(braindex retro の設定)を読める。節の中の未知のキーもエラーにする。
func TestLoad_RetroSection(t *testing.T) {
	p := filepath.Join(t.TempDir(), "braindex.json")
	if err := os.WriteFile(p, []byte(`{"root": ".", "retro": {"sessions_dir": "~/logs", "window_days": 7, "threshold": 0.2, "position_bins": "1-5,6-", "dictionary": "d.txt", "dictionary_extra": "e.txt"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, found, err := Load(p)
	if err != nil || !found {
		t.Fatalf("Load: found=%v err=%v", found, err)
	}
	r := cfg.Retro
	if r.SessionsDir != "~/logs" || r.WindowDays != 7 || r.Threshold != 0.2 || r.PositionBins != "1-5,6-" || r.Dictionary != "d.txt" || r.DictionaryExtra != "e.txt" {
		t.Errorf("retro 節: got=%+v", r)
	}
	if err := os.WriteFile(p, []byte(`{"root": ".", "retro": {"window_day": 7}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(p); err == nil {
		t.Error("retro 節の未知のキーはエラーにする")
	}
}
