package config

import (
	"os"
	"path/filepath"
	"testing"
)

// 先頭に UTF-8 BOM が付いた braindex.json も読める。
//
// Windows PowerShell 5.1 の `Set-Content -Encoding utf8` は BOM を付けるので、
// 設定ファイルを PowerShell で書き出した利用者が踏む(2026-09-03 に schedule の実機確認で遭遇)。
// BOM が残ると encoding/json が先頭バイトで
// `invalid character 'ï' looking for beginning of value` を返して落ちる。
func TestLoad_BOM(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "braindex.json")
	body := `{"root": "..", "review": {"since_days": 21}}`
	if err := os.WriteFile(p, append([]byte{0xEF, 0xBB, 0xBF}, body...), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, found, err := Load(p)
	if err != nil {
		t.Fatalf("BOM 付きを読めない: %v", err)
	}
	if !found {
		t.Fatal("found=false")
	}
	if cfg.Root != ".." {
		t.Errorf("root の読み取りが不正: %q", cfg.Root)
	}
	if cfg.Review.SinceDays != 21 {
		t.Errorf("BOM の後ろの節が読めていない: %+v", cfg.Review)
	}
}

// BOM を落とすのは先頭の 1 個だけ。途中に現れた場合は JSON の誤りとして落とす
// (BOM を無条件に全部消すと、文字列の中の U+FEFF まで変わってしまう)。
func TestLoad_BOM_途中のものは消さない(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "braindex.json")
	// 値の側に U+FEFF を含める。読めたうえで値が保たれることを見る
	// (Go のソースに生の BOM は置けない=illegal byte order mark。エスケープで書く)
	body := "{\"root\": \"..\", \"notes_dirs\": [\"docs/\\ufeffnotes\"]}"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.NotesDirs) != 1 || cfg.NotesDirs[0] != "docs/\ufeffnotes" {
		t.Errorf("値の中の U+FEFF が変わった: %q", cfg.NotesDirs)
	}
}
