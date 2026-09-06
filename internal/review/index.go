package review

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pilefort/braindex/internal/render"
)

// IndexDiff は前回の索引と今回の走査結果の差。パス(root 相対)を鍵に突き合わせる。
type IndexDiff struct {
	Before int        // 前回の件数
	After  int        // 今回の件数
	Repos  []RepoDiff // 差分のあるリポだけ。リポ名昇順
}

// RepoDiff は 1 リポ分の差。各スライスはパス昇順。
type RepoDiff struct {
	Repo    string
	Added   []render.Entry
	Changed []EntryChange
	Removed []render.Entry
}

// EntryChange は同じパスで中身が変わった行。Columns は変わった列の名前(日付・種別・タイトル・要旨 の順)。
type EntryChange struct {
	Before  render.Entry
	After   render.Entry
	Columns []string
}

// Counts は追加・変更・削除の合計を返す。
func (d IndexDiff) Counts() (added, changed, removed int) {
	for _, r := range d.Repos {
		added += len(r.Added)
		changed += len(r.Changed)
		removed += len(r.Removed)
	}
	return added, changed, removed
}

// DiffIndex は前回の catalog.md と今回の catalog.md(再走査して render した内容)を比べる。
// 純関数で git にも時計にも依らない。同じ入力からは同じ結果を返す。
// before が空(前回の索引が無い)なら全件が追加になる。
func DiffIndex(before, after []byte) (IndexDiff, error) {
	oldEntries, err := ParseCatalog(before)
	if err != nil {
		return IndexDiff{}, fmt.Errorf("前回の索引: %w", err)
	}
	newEntries, err := ParseCatalog(after)
	if err != nil {
		return IndexDiff{}, fmt.Errorf("今回の索引: %w", err)
	}
	return diffEntries(oldEntries, newEntries), nil
}

func diffEntries(oldEntries, newEntries []render.Entry) IndexDiff {
	d := IndexDiff{Before: len(oldEntries), After: len(newEntries)}
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
		if !seen[o.Path] {
			r := repoOf(o.Repo)
			r.Removed = append(r.Removed, o)
			seen[o.Path] = true // 前回側の重複も 1 回だけ
		}
	}
	for _, r := range repos {
		sort.Slice(r.Added, func(i, j int) bool { return r.Added[i].Path < r.Added[j].Path })
		sort.Slice(r.Changed, func(i, j int) bool { return r.Changed[i].After.Path < r.Changed[j].After.Path })
		sort.Slice(r.Removed, func(i, j int) bool { return r.Removed[i].Path < r.Removed[j].Path })
		d.Repos = append(d.Repos, *r)
	}
	sort.Slice(d.Repos, func(i, j int) bool { return d.Repos[i].Repo < d.Repos[j].Repo })
	return d
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
	fmt.Fprintf(b, "前回 %d 件 → 今回 %d 件（追加 %d・変更 %d・削除 %d）。前回の索引: %s\n", d.Before, d.After, added, changed, removed, source)
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
	}
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
func WriteIndexUnavailable(b *strings.Builder, reason, source string) {
	b.WriteString("## 索引（件数と増減）\n\n")
	fmt.Fprintf(b, "前回の索引を読めなかった（%s）。増減は出さない。前回の索引: %s\n", reason, source)
}
