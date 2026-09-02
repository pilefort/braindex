package review

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pilefort/braindex/internal/render"
)

// RepoArchive は 1 リポ分のアーカイブ候補。Entries はパス昇順。
type RepoArchive struct {
	Repo    string
	Entries []render.Entry
}

// ArchiveCandidates は今回の索引エントリから、機械条件だけでアーカイブ候補を選ぶ:
// 種別が decisions でなく(決定記録は追記式で古くならない)、日付があり、cutoff(YYYY-MM-DD)より前で、
// touched(root 相対パスの集合。今回の差分ファイルと索引の追加・変更)に無いもの。
// 「結論が新しいノートで上書きされたか」は読まないと分からないので、判断は人に残す。
func ArchiveCandidates(entries []render.Entry, cutoff string, touched map[string]bool) []RepoArchive {
	byRepo := map[string]*RepoArchive{}
	for _, e := range entries {
		if e.Kind == "decisions" || e.Date == "" || e.Date >= cutoff || touched[e.Path] {
			continue
		}
		r, ok := byRepo[e.Repo]
		if !ok {
			r = &RepoArchive{Repo: e.Repo}
			byRepo[e.Repo] = r
		}
		r.Entries = append(r.Entries, e)
	}
	var out []RepoArchive
	for _, r := range byRepo {
		sort.Slice(r.Entries, func(i, j int) bool { return r.Entries[i].Path < r.Entries[j].Path })
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Repo < out[j].Repo })
	return out
}

// Touched は索引差分のうち追加・変更された行のパス(root 相対)の集合を返す。アーカイブ候補の除外に使う。
func (d IndexDiff) Touched() map[string]bool {
	m := map[string]bool{}
	for _, r := range d.Repos {
		for _, e := range r.Added {
			m[e.Path] = true
		}
		for _, c := range r.Changed {
			m[c.After.Path] = true
		}
	}
	return m
}

// WriteArchiveSection は「## アーカイブ候補（機械条件のみ）」の節を書く。months は閾値の月数、cutoff はその日付。
func WriteArchiveSection(b *strings.Builder, repos []RepoArchive, months int, cutoff string) {
	b.WriteString("## アーカイブ候補（機械条件のみ）\n\n")
	fmt.Fprintf(b, "索引の日付が %d か月より前（%s より前）で、今回の差分に無いノート。結論が新しいノートで上書きされたかは実物を読んで判断する。移動・削除はしない。\n", months, cutoff)
	if len(repos) == 0 {
		b.WriteString("\n- なし\n")
		return
	}
	for _, r := range repos {
		b.WriteString("\n### " + r.Repo + "\n")
		for _, e := range r.Entries {
			fmt.Fprintf(b, "- %s（%s）\n", e.Path, describe(e))
		}
	}
}

// DaysBefore は today(YYYY-MM-DD)の n 日前を返す。
func DaysBefore(today string, n int) (string, error) {
	t, err := parseDate(today)
	if err != nil {
		return "", err
	}
	return t.AddDate(0, 0, -n).Format("2006-01-02"), nil
}

// MonthsBefore は today(YYYY-MM-DD)の n か月前を返す(月末の繰り上がりは time.AddDate の規則に従う)。
func MonthsBefore(today string, n int) (string, error) {
	t, err := parseDate(today)
	if err != nil {
		return "", err
	}
	return t.AddDate(0, -n, 0).Format("2006-01-02"), nil
}

func parseDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("日付は YYYY-MM-DD で指定する: %q", s)
	}
	return t, nil
}
