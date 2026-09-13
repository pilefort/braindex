// Package catalog は scan → extract → render を束ねて catalog.md を生成する。
// main と e2e テストの両方から使う。
package catalog

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pilefort/braindex/internal/changehistory"
	"github.com/pilefort/braindex/internal/extract"
	"github.com/pilefort/braindex/internal/indexdata"
	"github.com/pilefort/braindex/internal/links"
	"github.com/pilefort/braindex/internal/render"
	"github.com/pilefort/braindex/internal/scan"
)

// Result は Build の結果。
type Result struct {
	Links      []links.Edge
	LinksTSV   []byte
	Unresolved links.Unresolved
	Records    []indexdata.Entry    // 表の読み戻しと同じ値。走査順。直接の受け渡し用。
	Catalog    []byte               // catalog.md の内容(先頭に走査の記録を含む)
	Entries    int                  // 索引に載せた件数
	Warnings   []string             // 飛ばしたファイル・ディレクトリの説明(無ければ空)。無言スキップにしない
	Coverage   Coverage             // 走査の記録(Known は常に true)。Catalog の先頭にも同じ内容を書く
	Notes      []changehistory.Note // 本文を読めたノートの内容ハッシュ(走査順・Records と同じ並び)。索引には入れず、本文の変更の記録(changes.json)の材料にする
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
	linkNotes := []links.Note{}
	refs := map[string][]links.Ref{}
	for _, f := range sc.Files {
		content, err := os.ReadFile(f.Abs)
		if err != nil {
			reason := scan.DescribeErr(err)
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s", f.Rel, reason))
			gaps = append(gaps, scan.Gap{Rel: f.Rel, Reason: reason})
			continue
		}
		m := extract.Extract(filepath.Base(f.Abs), content, f.Kind)
		linkNotes = append(linkNotes, links.Note{Rel: f.Rel, Repo: f.Repo})
		refs[f.Rel] = links.Extract(content)
		res.Notes = append(res.Notes, changehistory.Note{Path: f.Rel, Hash: changehistory.Hash(content)})
		entries = append(entries, render.Entry{
			Repo:    f.Repo,
			Date:    m.Date,
			Kind:    f.Kind,
			Title:   m.Title,
			Summary: m.Summary,
			Path:    f.Rel,
		})
	}
	resolved, unresolved := links.Resolve(linkNotes, refs)
	res.Unresolved = unresolved
	res.Links = []links.Edge{}
	for _, e := range resolved {
		if !links.ValidPath(e.From) || !links.ValidPath(e.To) {
			res.Warnings = append(res.Warnings, fmt.Sprintf("つながりのパスにタブ・改行があるため除外: %q → %q", e.From, e.To))
			continue
		}
		res.Links = append(res.Links, e)
	}
	res.LinksTSV = links.Marshal(res.Links)
	res.Coverage = Coverage{Known: true, Gaps: scan.SortGaps(gaps)}
	res.Catalog = withCoverage(render.Render(entries, genDate), res.Coverage)
	res.Records = make([]indexdata.Entry, len(entries))
	for i, e := range entries {
		res.Records[i] = render.CatalogEntry(e)
	}
	res.Entries = len(entries)
	return res, nil
}
