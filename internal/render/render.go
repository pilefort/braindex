// Package render は索引エントリから catalog.md を決定的に生成する。
// 同一入力からは常にバイト一致の出力を返す(改行は LF)。実行ごとに変わるのは先頭「生成:」行の
// 日付のみ。これにより git diff がそのまま週次差分になる。
package render

import (
	"fmt"
	"sort"
	"strings"
)

// Entry は catalog の 1 行。
type Entry struct {
	Repo    string
	Date    string
	Kind    string
	Title   string
	Summary string
	Path    string // root 相対・スラッシュ区切り
}

// Render はエントリ列を catalog.md のバイト列にする。genDate は先頭に載せる生成日(実行日)。
func Render(entries []Entry, genDate string) []byte {
	es := make([]Entry, len(entries))
	copy(es, entries)
	sort.SliceStable(es, func(i, j int) bool {
		a, b := es[i], es[j]
		if a.Repo != b.Repo {
			return a.Repo < b.Repo // リポ名昇順
		}
		if a.Date != b.Date {
			return a.Date > b.Date // 日付降順。日付なし("")は末尾
		}
		return a.Path < b.Path // 同日はパス昇順
	})

	var b strings.Builder
	b.WriteString("# 知識カタログ(braindex 自動生成 — 手で編集しない)\n\n")
	fmt.Fprintf(&b, "生成: %s / %d リポジトリ / %d 件\n", genDate, distinctRepos(es), len(es))
	b.WriteString("再生成: brain ルートで `go run ./cmd/braindex`\n")
	b.WriteString("使い方: このファイルを grep → ヒット行のパス(projects ルート相対)の実ファイルを読む\n")

	lastRepo := ""
	for _, e := range es {
		if e.Repo != lastRepo {
			b.WriteString("\n## " + e.Repo + "\n")
			b.WriteString("| 日付 | 種別 | タイトル | 要旨 | パス |\n")
			b.WriteString("|---|---|---|---|---|\n")
			lastRepo = e.Repo
		}
		b.WriteString("| " + e.Date + " | " + e.Kind + " | " + esc(e.Title) + " | " + esc(e.Summary) + " | " + e.Path + " |\n")
	}
	return []byte(b.String())
}

// distinctRepos は es がリポ名でソート済みである前提で、異なるリポの数を数える。
func distinctRepos(es []Entry) int {
	count, last := 0, ""
	for i, e := range es {
		if i == 0 || e.Repo != last {
			count++
			last = e.Repo
		}
	}
	return count
}

// esc は表を壊さないよう、セル内の半角 | を全角 ｜ に置換する。
func esc(s string) string { return strings.ReplaceAll(s, "|", "｜") }
