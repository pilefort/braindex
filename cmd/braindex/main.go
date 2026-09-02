// braindex — 複数リポを横断する知識の索引 (index/catalog.md) を決定的に再生成する CLI。
//
// scan(スキャン対象の発見) → extract(タイトル・日付・要旨の抽出) → render(catalog.md 生成)
// の順に処理する。hub リポ(索引を置くリポ)のルートで実行する。
//
// 終了コード:
//   - 0: 成功
//   - 1: 失敗(設定・root が読めない等。索引は書かない)
//   - 2: 警告つきで完了(読めないファイルや存在しない extra を飛ばした。索引は書く)
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
	os.Exit(run("braindex.json", "index/catalog.md", time.Now().Format("2006-01-02")))
}

func run(cfgPath, outPath, genDate string) int {
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "braindex:", err)
		return 1
	}
	res, err := catalog.Build(cfg, genDate)
	if err != nil {
		fmt.Fprintln(os.Stderr, "braindex:", err)
		return 1
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "braindex: 警告:", w)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "braindex:", err)
		return 1
	}
	if err := os.WriteFile(outPath, res.Catalog, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "braindex:", err)
		return 1
	}
	fmt.Printf("catalog 生成: %d 件 → %s\n", res.Entries, outPath)
	if len(res.Warnings) > 0 {
		fmt.Fprintf(os.Stderr, "braindex: 警告 %d 件(終了コード 2)\n", len(res.Warnings))
		return 2
	}
	return 0
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
