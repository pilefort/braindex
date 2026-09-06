//go:build windows

package scantest

import (
	"syscall"
	"testing"
)

// MakeUnreadable は path(ファイルかディレクトリ)を共有なしで開いたままにし、他の open と列挙を
// 共有違反で失敗させる。Stat は通るので、走査には「見つかるが読めない」として見える。
// テスト終了時にハンドルを閉じて元に戻す(t.TempDir の後始末より先に走る)。
func MakeUnreadable(t testing.TB, path string) {
	t.Helper()
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	// FILE_FLAG_BACKUP_SEMANTICS が無いとディレクトリは開けない。ファイルにも付けて害は無い
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatalf("排他で開けない: %s: %v", path, err)
	}
	t.Cleanup(func() { syscall.CloseHandle(h) })
}
