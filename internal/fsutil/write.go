// Package fsutil はファイル書き込みの共通処理。
package fsutil

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// osRename はテストで差し替える(置き換えの失敗を再現するため)。
var osRename = os.Rename

// WriteAtomic は data を path に書く。同じディレクトリに一時ファイルを作って書き切ってから
// path へ置き換えるので、途中で電源が落ちても・書き込みが失敗しても、path が半端な内容になることはない。
// path が既にあれば置き換える(os.WriteFile と同じ)。
//
// 索引・レビューの下書き・台帳は「前回の内容」に意味がある。os.WriteFile は先に切り詰めてから書くので、
// 書いている途中で落ちると前回の内容ごと失う。次に読むのが人でなくコマンド(前回の索引との差分を取る review、
// 何を書いたかを覚えている update の台帳)なので、半端なファイルは黙って間違った結果を出す。
//
// 一時ファイルの名前は os.CreateTemp に任せる(PID だと同じプロセスの中で 2 か所が同時に書くとぶつかる)。
// 先頭に "." を付けるのは、走査の対象("." で始まるものは除く)に入れないため。
func WriteAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("一時ファイルを作れない(%s): %w", dir, err)
	}
	tmp := f.Name()
	cleanup := func(err error) error {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if _, err := f.Write(data); err != nil {
		return cleanup(fmt.Errorf("%s に書けない: %w", path, err))
	}
	if err := f.Sync(); err != nil {
		return cleanup(fmt.Errorf("%s を書き切れない: %w", path, err))
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("%s を閉じられない: %w", path, err)
	}
	if err := os.Chmod(tmp, perm); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("%s の権限を設定できない: %w", path, err)
	}
	// Windows でも Go の os.Rename は既存のファイルを置き換える(MoveFileEx の REPLACE_EXISTING)。
	if err := osRename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("%s を置き換えられない: %w", path, err)
	}
	return nil
}
