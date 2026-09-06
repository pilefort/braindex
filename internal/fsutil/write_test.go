package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "catalog.md")

	if err := WriteAtomic(p, []byte("1 回目\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(p); err != nil || string(b) != "1 回目\n" {
		t.Fatalf("1 回目: err=%v %q", err, b)
	}
	// 既にあるファイルは置き換える
	if err := WriteAtomic(p, []byte("2 回目\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "2 回目\n" {
		t.Errorf("2 回目: %q", b)
	}
	// 一時ファイルを残さない
	if n := countTemp(t, dir); n != 0 {
		t.Errorf("一時ファイルが %d 件残った", n)
	}
}

// 置き換えに失敗したら、元のファイルは前回の内容のまま残り、一時ファイルも残らない。
// 索引・レビューの下書き・台帳は「前回の内容」に意味があるので、失敗して古いまま残るほうが
// 半端な内容で上書きされるより良い。
func TestWriteAtomic_置き換えに失敗しても元のファイルは壊れない(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ledger.json")
	if err := os.WriteFile(p, []byte("前回の内容\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := osRename
	osRename = func(string, string) error { return errors.New("置き換えできない") }
	t.Cleanup(func() { osRename = orig })

	err := WriteAtomic(p, []byte("新しい内容\n"), 0o644)
	if err == nil || !strings.Contains(err.Error(), "置き換えられない") {
		t.Fatalf("err=%v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "前回の内容\n" {
		t.Errorf("元のファイルが変わった: %q", b)
	}
	if n := countTemp(t, dir); n != 0 {
		t.Errorf("一時ファイルが %d 件残った", n)
	}
}

// 置き場が無ければ一時ファイルを作る時点で失敗する(os.WriteFile と同じく MkdirAll はしない)。
func TestWriteAtomic_置き場が無い(t *testing.T) {
	p := filepath.Join(t.TempDir(), "no-such-dir", "x.md")
	if err := WriteAtomic(p, []byte("x"), 0o644); err == nil {
		t.Error("エラーにならない")
	}
}

func countTemp(t *testing.T, dir string) int {
	t.Helper()
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, de := range des {
		if strings.HasPrefix(de.Name(), ".") {
			n++
		}
	}
	return n
}
