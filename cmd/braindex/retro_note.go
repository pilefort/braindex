package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// retroNotePattern は振り返りの所見ノートのファイル名(hub の docs/notes/retro-YYYY-MM-DD.md)。
// 置き場は docs/notes/ の下ならどこでもよい(common / project に分けている人がいる)。
var retroNotePattern = regexp.MustCompile(`^retro-(\d{4}-\d{2}-\d{2})\.md$`)

// hasRetroNoteInWindow は hub の docs/notes/ の下に、日付が窓の中の所見ノートがあるかを返す。
// 状態ファイルは持たない——「振り返ったか」の答えはノートの有無そのもので、別に記録すると食い違う
// (設計レビュー 2026-09-06 M7)。
//
// hubDir が空(設定ファイルが無い)なら確かめようがないので true を返す(余計なことを言わない)。
func hasRetroNoteInWindow(hubDir string, since, until time.Time) bool {
	if hubDir == "" {
		return true
	}
	root := filepath.Join(hubDir, "docs", "notes")
	found := false
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || found {
			return nil
		}
		m := retroNotePattern.FindStringSubmatch(d.Name())
		if m == nil {
			return nil
		}
		t, perr := time.ParseInLocation("2006-01-02", m[1], localLoc)
		if perr != nil {
			return nil
		}
		if !t.Before(since) && t.Before(until) {
			found = true
		}
		return nil
	})
	if _, err := os.Stat(root); err != nil {
		return true // 置き場そのものが無いなら、規約に沿っていない hub なので言わない
	}
	return found
}
