package scan

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ExcludesWorktree は repo(root 相対のリポ名)を git worktree として除外するか返す。
// .git が通常ファイルで先頭が gitdir: の場合だけ該当する。リンク先や gitdir: の値は辿らない。
// Root が空の設定では判定しない。判定できない場合は error を返し、走査側でリポ全体を Gap にする。
func ExcludesWorktree(cfg Config, repo string) (bool, error) {
	if cfg.IncludeWorktrees || cfg.Root == "" {
		return false, nil
	}
	if err := checkRepoName(repo, cfg.Depth()); err != nil {
		return false, err
	}
	p := filepath.Join(cfg.Root, filepath.FromSlash(repo), ".git")
	st, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !st.Mode().IsRegular() {
		return false, nil
	}
	f, err := os.Open(p)
	if err != nil {
		return false, err
	}
	defer f.Close()
	var prefix [7]byte
	_, err = io.ReadFull(f, prefix[:])
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return string(prefix[:]) == "gitdir:", nil
}
