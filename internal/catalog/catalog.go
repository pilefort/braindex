// Package catalog は scan → extract → render を束ねて catalog.md を生成する。
// main と e2e テストの両方から使う。
package catalog

import (
	"os"
	"path/filepath"

	"github.com/pilefort/braindex/internal/extract"
	"github.com/pilefort/braindex/internal/render"
	"github.com/pilefort/braindex/internal/scan"
)

// Build は cfg に従って対象を走査・抽出し、catalog.md のバイト列を返す。
// genDate は先頭「生成:」行に載せる実行日("YYYY-MM-DD")。
func Build(cfg scan.Config, genDate string) ([]byte, int, error) {
	files, err := scan.Scan(cfg)
	if err != nil {
		return nil, 0, err
	}
	entries := make([]render.Entry, 0, len(files))
	for _, f := range files {
		info, err := os.Stat(f.Abs)
		if err != nil {
			continue // 読めないものは飛ばす
		}
		content, err := os.ReadFile(f.Abs)
		if err != nil {
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
	return render.Render(entries, genDate), len(entries), nil
}
