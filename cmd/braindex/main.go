// braindex — 複数リポを横断する知識の索引 (index/catalog.md) を決定的に再生成する CLI。
//
// scan(スキャン対象の発見) → extract(タイトル・日付・要旨の抽出) → render(catalog.md 生成)
// の順に処理する。hub リポ(索引を置くリポ)のルートで実行する。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pilefort/braindex/internal/catalog"
	"github.com/pilefort/braindex/internal/scan"
)

func main() {
	if err := run("braindex.json", "index/catalog.md", time.Now().Format("2006-01-02")); err != nil {
		fmt.Fprintln(os.Stderr, "braindex:", err)
		os.Exit(1)
	}
}

func run(cfgPath, outPath, genDate string) error {
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}
	out, n, err := catalog.Build(cfg, genDate)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(outPath, out, 0o644); err != nil {
		return err
	}
	fmt.Printf("catalog 生成: %d 件 → %s\n", n, outPath)
	return nil
}

// loadConfig は braindex.json を読む。root 未指定なら ".."(hub リポの親ディレクトリ)。
func loadConfig(path string) (scan.Config, error) {
	var cfg scan.Config
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Root == "" {
		cfg.Root = ".."
	}
	return cfg, nil
}
