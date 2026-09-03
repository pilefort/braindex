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
// BOM が残ると encoding/json が先頭バイトで `invalid character ... looking for
// beginning of value` を返して落ちる(2026-09-03 の実測では '\ufeff' と 'ï' の両方の
// 表示を見たが、どちらの条件でそうなるかは特定できていない。どちらにせよ落ちる)。
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

// BOM を落とすのは先頭の 1 個だけ。値の中の U+FEFF は JSON 文字列の合法な文字なので、
// そのまま値として保たれる(無条件に全部消すと文字列の中身まで変わってしまう)。
//
// 入力は生バイト、期待値は Go のエスケープ、という非対称がこのテストの肝。
// 生の BOM を Go のソースに置けない(illegal byte order mark)のは期待値の側だけで、
// 入力を "\ufeff" とエスケープで書くと JSON デコーダが解釈する 6 文字の ASCII になり、
// BOM 除去コードが触りうるバイトが 1 つも無いテストになってしまう。
func TestLoad_BOM_途中のものは消さない(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "braindex.json")
	// 値の中に生の BOM バイト(EF BB BF)を置く
	body := []byte("{\"root\": \"..\", \"notes_dirs\": [\"docs/\xEF\xBB\xBFnotes\"]}")
	if err := os.WriteFile(p, body, 0o644); err != nil {
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
