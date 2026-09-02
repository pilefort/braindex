// Package catalog は scan → extract → render を束ねて catalog.md を生成する。
// main と e2e テストの両方から使う。
package catalog

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pilefort/braindex/internal/extract"
	"github.com/pilefort/braindex/internal/render"
	"github.com/pilefort/braindex/internal/scan"
)

// Result は Build の結果。
type Result struct {
	Catalog  []byte   // catalog.md の内容
	Entries  int      // 索引に載せた件数
	Warnings []string // 飛ばしたファイル・ディレクトリの説明(無ければ空)。無言スキップにしない
}

// Build は cfg に従って対象を走査・抽出し、catalog.md のバイト列を返す。
// genDate は先頭「生成:」行に載せる実行日("YYYY-MM-DD")。
// 読めないファイルは Warnings に積んで飛ばし、残りで索引を作る。
func Build(cfg scan.Config, genDate string) (Result, error) {
	files, warnings, err := scan.Scan(cfg)
	if err != nil {
		return Result{}, err
	}
	res := Result{Warnings: warnings}
	entries := make([]render.Entry, 0, len(files))
	for _, f := range files {
		info, err := os.Stat(f.Abs)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %v", f.Rel, err))
			continue
		}
		content, err := os.ReadFile(f.Abs)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %v", f.Rel, err))
			continue
		}
		m := extract.Extract(filepath.Base(f.Abs), content, info.ModTime(), f.Kind)
		entries = append(entries, render.Entry{
			Repo:    f.Repo,
			Date:    m.Date,
			Kind:    f.Kind,
			Title:   m.Title,
			Summary: m.Summary,
			Path:    f.Rel,
		})
	}
	res.Catalog = render.Render(entries, genDate)
	res.Entries = len(entries)
	return res, nil
}
