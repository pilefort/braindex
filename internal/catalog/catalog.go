// Package catalog は scan → extract → render を束ねて catalog.md を生成する。
// main と e2e テストの両方から使う。
package catalog

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pilefort/braindex/internal/extract"
	"github.com/pilefort/braindex/internal/indexdata"
	"github.com/pilefort/braindex/internal/render"
	"github.com/pilefort/braindex/internal/scan"
)

// Result は Build の結果。
type Result struct {
	Records  []indexdata.Entry // 表の読み戻しと同じ値。走査順。直接の受け渡し用。
	Catalog  []byte            // catalog.md の内容(先頭に走査の記録を含む)
	Entries  int               // 索引に載せた件数
	Warnings []string          // 飛ばしたファイル・ディレクトリの説明(無ければ空)。無言スキップにしない
	Coverage Coverage          // 走査の記録(Known は常に true)。Catalog の先頭にも同じ内容を書く
}

// Build は cfg に従って対象を走査・抽出し、catalog.md のバイト列を返す。
// genDate は先頭「生成:」行に載せる実行日("YYYY-MM-DD")。
// 読めないファイルは Warnings に積んで飛ばし、残りで索引を作る。飛ばした範囲(列挙できなかった
// ディレクトリと読めなかったファイル)は Coverage にまとめ、索引の先頭にも書く——索引に無いことを
// 削除の根拠にさせないため。
func Build(cfg scan.Config, genDate string) (Result, error) {
	sc, err := scan.Scan(cfg)
	if err != nil {
		return Result{}, err
	}
	res := Result{Warnings: sc.Warnings}
	gaps := sc.Gaps
	entries := make([]render.Entry, 0, len(sc.Files))
	for _, f := range sc.Files {
		content, err := os.ReadFile(f.Abs)
		if err != nil {
			reason := scan.DescribeErr(err)
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s", f.Rel, reason))
			gaps = append(gaps, scan.Gap{Rel: f.Rel, Reason: reason})
			continue
		}
		m := extract.Extract(filepath.Base(f.Abs), content, f.Kind)
		entries = append(entries, render.Entry{
			Repo:    f.Repo,
			Date:    m.Date,
			Kind:    f.Kind,
			Title:   m.Title,
			Summary: m.Summary,
			Path:    f.Rel,
		})
	}
	res.Coverage = Coverage{Known: true, Gaps: scan.SortGaps(gaps)}
	res.Catalog = withCoverage(render.Render(entries, genDate), res.Coverage)
	res.Records = make([]indexdata.Entry, len(entries))
	for i, e := range entries {
		res.Records[i] = render.CatalogEntry(e)
	}
	res.Entries = len(entries)
	return res, nil
}
