// Package indexdata は表示機能に依存しない索引の共通データを持つ。
package indexdata

// Entry はノートを指す索引の一行。本文そのものは保持しない。
type Entry struct {
	Repo    string
	Date    string
	Kind    string
	Title   string
	Summary string
	Path    string // root 相対・スラッシュ区切り
}
