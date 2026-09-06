//go:build !windows

package scantest

import (
	"os"
	"testing"
)

// MakeUnreadable は path(ファイルかディレクトリ)の権限を 000 にして読めなくする。
// Stat は親ディレクトリの権限で通るので、走査には「見つかるが読めない」として見える。
// root は権限に縛られないので skip する。テスト終了時に元の権限へ戻す。
func MakeUnreadable(t testing.TB, path string) {
	t.Helper()
	if os.Getuid() == 0 {
		t.Skip("root では権限で読めなくできない")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mode := info.Mode().Perm()
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, mode) })
}
