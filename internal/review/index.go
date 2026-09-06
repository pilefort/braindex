package review

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pilefort/braindex/internal/catalog"
	"github.com/pilefort/braindex/internal/render"
	"github.com/pilefort/braindex/internal/scan"
)

// IndexDiff は前回の索引と今回の走査結果の差。パス(root 相対)を鍵に突き合わせる。
//
// 前回にあって今回無い行は、そのまま「削除」にしない。今回の走査で読めなかった範囲にあれば「確認不能」
// (あるかどうか分からない)、今の設定が見に行かない場所なら「対象外」(設定を変えた・archive へ移した)で、
// 残りだけが「削除」。読めなかった範囲を削除と数えると、週次レビューの増減が嘘をつく(設計レビュー補足 2026-09-06)。
type IndexDiff struct {
	Before       int              // 前回の件数
	After        int              // 今回の件数
	Repos        []RepoDiff       // 差分のあるリポだけ。リポ名昇順
	Coverage     catalog.Coverage // 今回の索引の走査の記録
	PrevCoverage catalog.Coverage // 前回の索引の走査の記録(記録を持たない版なら Known=false)
}

// RepoDiff は 1 リポ分の差。各スライスはパス昇順。
type RepoDiff struct {
	Repo        string
	Added       []render.Entry
	Changed     []EntryChange
	Removed     []render.Entry // 削除: 置き場は確認できたが、そのパスが無かった
	Unconfirmed []Unconfirmed  // 確認不能: 今回の走査で読めなかった範囲にあり、有無が分からない
	OutOfScope  []render.Entry // 対象外: 今の設定では走査しない場所にある(削除とは言えない)
}

// EntryChange は同じパスで中身が変わった行。Columns は変わった列の名前(日付・種別・タイトル・要旨 の順)。
type EntryChange struct {
	Before  render.Entry
	After   render.Entry
	Columns []string
}

// Unconfirmed は前回にあって今回無いが、読めなかった範囲に入っていて有無を確認できない行。
type Unconfirmed struct {
	Entry render.Entry
	Gap   scan.Gap // 含んでいた読めなかった範囲
}

// Counts は追加・変更・削除の合計を返す。確認不能・対象外は含めない(Held)。
func (d IndexDiff) Counts() (added, changed, removed int) {
	for _, r := range d.Repos {
		added += len(r.Added)
		changed += len(r.Changed)
		removed += len(r.Removed)
	}
	return added, changed, removed
}

// Held は削除と断定しなかった行の数(確認不能・対象外)を返す。
func (d IndexDiff) Held() (unconfirmed, outOfScope int) {
	for _, r := range d.Repos {
		unconfirmed += len(r.Unconfirmed)
		outOfScope += len(r.OutOfScope)
	}
	return unconfirmed, outOfScope
}

// DiffIndex は前回の catalog.md と今回の catalog.md(再走査して render した内容)を比べる。
// 純関数で git にも時計にも依らない。同じ入力からは同じ結果を返す。
// before が空(前回の索引が無い)なら全件が追加になる。走査の記録は両方の索引の先頭から読む。
// cfg は今の走査設定(対象外の判定に使う)。nil なら対象外の判定をせず、前回にあって今回無い行は
// 読めなかった範囲に無い限り削除になる。
func DiffIndex(before, after []byte, cfg *scan.Config) (IndexDiff, error) {
	oldEntries, err := ParseCatalog(before)
	if err != nil {
		return IndexDiff{}, fmt.Errorf("前回の索引: %w", err)
	}
	newEntries, err := ParseCatalog(after)
	if err != nil {
		return IndexDiff{}, fmt.Errorf("今回の索引: %w", err)
	}
	prev, err := catalog.ParseCoverage(before)
	if err != nil {
		return IndexDiff{}, fmt.Errorf("前回の索引: %w", err)
	}
	cov, err := catalog.ParseCoverage(after)
	if err != nil {
		return IndexDiff{}, fmt.Errorf("今回の索引: %w", err)
	}
	j := judge{cov: cov, prev: prev}
	if cfg != nil {
		c := *cfg
		j.covers = func(rel string) bool { return scan.Covers(c, rel) }
	}
	return diffEntries(oldEntries, newEntries, j), nil
}

// judge は「前回にあって今回無い行」を 削除／確認不能／対象外 に振り分ける材料。
type judge struct {
	cov    catalog.Coverage      // 今回の走査の記録
	prev   catalog.Coverage      // 前回の索引の記録(節に書くだけ。振り分けには使わない)
	covers func(rel string) bool // 今の設定がそのパスを見に行くか。nil なら全部見に行く扱い
}

func diffEntries(oldEntries, newEntries []render.Entry, j judge) IndexDiff {
	d := IndexDiff{Before: len(oldEntries), After: len(newEntries), Coverage: j.cov, PrevCoverage: j.prev}
	oldByPath := make(map[string]render.Entry, len(oldEntries))
	for _, e := range oldEntries {
		if _, dup := oldByPath[e.Path]; !dup {
			oldByPath[e.Path] = e
		}
	}
	repos := map[string]*RepoDiff{}
	repoOf := func(name string) *RepoDiff {
		r, ok := repos[name]
		if !ok {
			r = &RepoDiff{Repo: name}
			repos[name] = r
		}
		return r
	}
	seen := make(map[string]bool, len(newEntries))
	for _, e := range newEntries {
		if seen[e.Path] {
			continue
		}
		seen[e.Path] = true
		o, ok := oldByPath[e.Path]
		if !ok {
			r := repoOf(e.Repo)
			r.Added = append(r.Added, e)
			continue
		}
		if cols := changedColumns(o, e); len(cols) > 0 {
			r := repoOf(e.Repo)
			r.Changed = append(r.Changed, EntryChange{Before: o, After: e, Columns: cols})
		}
	}
	for _, o := range oldEntries {
		if seen[o.Path] {
			continue
		}
		seen[o.Path] = true // 前回側の重複も 1 回だけ
		r := repoOf(o.Repo)
		switch {
		case gapOf(j.cov, o.Path) != nil:
			r.Unconfirmed = append(r.Unconfirmed, Unconfirmed{Entry: o, Gap: *gapOf(j.cov, o.Path)})
		case j.covers != nil && !j.covers(o.Path):
			r.OutOfScope = append(r.OutOfScope, o)
		default:
			r.Removed = append(r.Removed, o)
		}
	}
	for _, r := range repos {
		sort.Slice(r.Added, func(i, k int) bool { return r.Added[i].Path < r.Added[k].Path })
		sort.Slice(r.Changed, func(i, k int) bool { return r.Changed[i].After.Path < r.Changed[k].After.Path })
		sort.Slice(r.Removed, func(i, k int) bool { return r.Removed[i].Path < r.Removed[k].Path })
		sort.Slice(r.Unconfirmed, func(i, k int) bool { return r.Unconfirmed[i].Entry.Path < r.Unconfirmed[k].Entry.Path })
		sort.Slice(r.OutOfScope, func(i, k int) bool { return r.OutOfScope[i].Path < r.OutOfScope[k].Path })
		d.Repos = append(d.Repos, *r)
	}
	sort.Slice(d.Repos, func(i, k int) bool { return d.Repos[i].Repo < d.Repos[k].Repo })
	return d
}

// gapOf は rel を含む読めなかった範囲を返す。無ければ nil。
func gapOf(cov catalog.Coverage, rel string) *scan.Gap {
	if g, ok := cov.Gap(rel); ok {
		return &g
	}
	return nil
}

// changedColumns は同じパスの 2 行で変わった列の名前を返す。パス以外の 4 列を見る。
func changedColumns(o, n render.Entry) []string {
	var cols []string
	if o.Date != n.Date {
		cols = append(cols, "日付")
	}
	if o.Kind != n.Kind {
		cols = append(cols, "種別")
	}
	if o.Title != n.Title {
		cols = append(cols, "タイトル")
	}
	if o.Summary != n.Summary {
		cols = append(cols, "要旨")
	}
	return cols
}

// WriteIndexSection は「## 索引(件数と増減)」の節を書く。
// source は前回の索引をどこから取ったかの説明(呼び出し側が決める。例: "index/catalog.md(ディスク)")。
func WriteIndexSection(b *strings.Builder, d IndexDiff, source string) {
	b.WriteString("## 索引（件数と増減）\n\n")
	added, changed, removed := d.Counts()
	unconfirmed, outOfScope := d.Held()
	fmt.Fprintf(b, "前回 %d 件 → 今回 %d 件（追加 %d・変更 %d・削除 %d", d.Before, d.After, added, changed, removed)
	if unconfirmed > 0 {
		fmt.Fprintf(b, "・確認不能 %d", unconfirmed)
	}
	if outOfScope > 0 {
		fmt.Fprintf(b, "・対象外 %d", outOfScope)
	}
	fmt.Fprintf(b, "）。前回の索引: %s\n", source)
	writeCoverageRecord(b, d.Coverage, d.PrevCoverage)
	if len(d.Repos) == 0 {
		b.WriteString("\n- なし\n")
		return
	}
	for _, r := range d.Repos {
		b.WriteString("\n### " + r.Repo + "\n")
		for _, e := range r.Added {
			fmt.Fprintf(b, "- 追加: %s（%s）\n", e.Path, describe(e))
		}
		for _, c := range r.Changed {
			fmt.Fprintf(b, "- 変更（%s）: %s（%s）\n", strings.Join(c.Columns, "・"), c.After.Path, describe(c.After))
		}
		for _, e := range r.Removed {
			fmt.Fprintf(b, "- 削除: %s（%s）\n", e.Path, describe(e))
		}
		for _, u := range r.Unconfirmed {
			fmt.Fprintf(b, "- 確認不能: %s（%s）— 読めなかった範囲: %s\n", u.Entry.Path, describe(u.Entry), gapPath(u.Gap))
		}
		for _, e := range r.OutOfScope {
			fmt.Fprintf(b, "- 対象外: %s（%s）— 今の設定では走査しない場所\n", e.Path, describe(e))
		}
	}
}

// writeCoverageRecord は今回・前回の走査の記録を 1 行と、読めなかった範囲の一覧で書く。
// 索引の先頭にも同じ記録があるが、レビューの読み手が索引を開かずに済むようここにも出す。
func writeCoverageRecord(b *strings.Builder, cov, prev catalog.Coverage) {
	fmt.Fprintf(b, "走査の記録: 今回は%s／前回は%s\n", coverageWords(cov, "その範囲の行は削除でなく確認不能にした"), coverageWords(prev, "その範囲の行は今回「追加」に出うる"))
	for _, g := range cov.Gaps {
		fmt.Fprintf(b, "- 今回読めなかった: %s — %s\n", gapPath(g), g.Reason)
	}
	for _, g := range prev.Gaps {
		fmt.Fprintf(b, "- 前回読めなかった: %s — %s\n", gapPath(g), g.Reason)
	}
}

// coverageWords は走査の記録を短く言う。note は読めなかった範囲があるときに添える説明。
func coverageWords(c catalog.Coverage, note string) string {
	switch {
	case !c.Known:
		return "記録なし（この記録を持たない索引。欠けがあったかは分からない）"
	case len(c.Gaps) == 0:
		return "読めなかった範囲なし"
	default:
		return fmt.Sprintf("読めなかった範囲 %d 件（%s）", len(c.Gaps), note)
	}
}

// gapPath は読めなかった範囲のパス。ディレクトリは末尾に "/" を付けて索引の記録と同じ形にする。
func gapPath(g scan.Gap) string {
	if g.Dir {
		return g.Rel + "/"
	}
	return g.Rel
}

// describe は行の日付とタイトルを「2026-01-02・タイトル」の形にする。日付が無ければタイトルだけ。
func describe(e render.Entry) string {
	if e.Date == "" {
		return e.Title
	}
	return e.Date + "・" + e.Title
}

// WriteIndexUnavailable は前回の索引を読めなかったときの索引の節。増減の代わりに理由を 1 行書く。
// 数を 0 件として出すと「前回 0 件 → 今回 N 件（追加 N）」になり、全件が新しくなったように読める。
// 今回の走査の記録は増減と無関係に出す(読めなかった範囲があれば、今回の索引にも欠けがある)。
func WriteIndexUnavailable(b *strings.Builder, reason, source string, cov catalog.Coverage) {
	b.WriteString("## 索引（件数と増減）\n\n")
	fmt.Fprintf(b, "前回の索引を読めなかった（%s）。増減は出さない。前回の索引: %s\n", reason, source)
	writeCoverageRecord(b, cov, catalog.Coverage{})
}
