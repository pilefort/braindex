package review

import (
	"fmt"
	"strings"
)

// Changes は「差分ファイル（リポ別）」の節の材料。
type Changes struct {
	Since      string        // 前回日 YYYY-MM-DD
	Pathspecs  []string      // 見た範囲(notes_dirs と docs/decisions.md)
	Repos      []RepoChanges // 索引に載ったリポのうち git 管理下のもの。リポ名昇順。差分の無いリポも含む
	Skipped    []string      // git 管理外で飛ばしたリポ。リポ名昇順
	GitMissing bool          // git が見つからず、節ごと飛ばした
}

// Touched は差分ファイルの集合を root 相対パス("<リポ>/<パス>")で返す。アーカイブ候補の除外に使う。
func (c Changes) Touched() map[string]bool {
	m := map[string]bool{}
	for _, r := range c.Repos {
		for _, f := range r.Files {
			m[r.Repo+"/"+f.Path] = true
		}
	}
	return m
}

// WriteChangesSection は「## 差分ファイル（リポ別）」の節を書く。
func WriteChangesSection(b *strings.Builder, c Changes) {
	b.WriteString("## 差分ファイル（リポ別）\n\n")
	if c.GitMissing {
		b.WriteString("git が見つからないので飛ばした（PATH に git を入れて実行し直すと出る）。\n")
		return
	}
	fmt.Fprintf(b, "%s 以降のコミットで %s に触れたファイル（`git log --since --name-status`）。\n", c.Since, strings.Join(c.Pathspecs, "・"))
	any := false
	for _, r := range c.Repos {
		if len(r.Files) == 0 {
			continue
		}
		any = true
		fmt.Fprintf(b, "\n### %s（コミット %d）\n", r.Repo, r.Commits)
		for _, f := range r.Files {
			fmt.Fprintf(b, "- %s: %s\n", f.Status, f.Path)
		}
	}
	if !any {
		b.WriteString("\n- なし\n")
	}
	if len(c.Skipped) > 0 {
		fmt.Fprintf(b, "\ngit 管理外（飛ばした）: %s\n", strings.Join(c.Skipped, "・"))
	}
}
