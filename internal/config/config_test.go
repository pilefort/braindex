package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	p := write(t, `{"root": "..", "notes_dirs": ["docs/notes", "wiki"],
	  "extra": [{"repo": "r", "path": "x", "recursive": true, "kind": "k", "exclude": ["*.draft.md"]}]}`)
	cfg, found, err := Load(p)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if cfg.Root != ".." || strings.Join(cfg.NotesDirs, ",") != "docs/notes,wiki" {
		t.Errorf("読み取り結果が不正: %+v", cfg)
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

// 壊れた JSON はエラー。
func TestLoad_BadJSON(t *testing.T) {
	p := write(t, `{"root": `)
	if _, _, err := Load(p); err == nil {
		t.Errorf("不正な JSON でエラーになっていない")
	}
}
