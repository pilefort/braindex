package template

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeAt はテスト用に dst の相対パスへ書く。
func writeAt(t *testing.T, dst, rel string, b []byte) {
	t.Helper()
	p := filepath.Join(dst, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readAt(t *testing.T, dst, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("読めない %s: %v", rel, err)
	}
	return b
}

// sampleFile は雛形から 1 ファイル選ぶ。特定の中身に依存しないため内容も返す。
func sampleFile(t *testing.T) File {
	t.Helper()
	files, err := Files(KindHub)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Path == "README.md" {
			return f
		}
	}
	t.Fatal("雛形に README.md が無い")
	return File{}
}

func has(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// 空の hub(台帳なし・ファイルなし)では、推定できる機能が無いので段 0(core)のファイルだけ作る
// (足していない機能のファイルは作らない・決定 2026-09-05 → manual/init-update.md「決めたこと」)。
func TestUpdate_CreatesMissing(t *testing.T) {
	dst := t.TempDir()
	files, err := FeatureFiles(nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Update(dst, KindHub, UpdateOptions{})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(res.Created) != len(files) || len(res.Conflicts) != 0 || len(res.Updated) != 0 {
		t.Errorf("created=%d updated=%d conflicts=%d want %d/0/0",
			len(res.Created), len(res.Updated), len(res.Conflicts), len(files))
	}
	if !res.Inferred || len(res.Features) != 1 || res.Features[0] != FeatureCore {
		t.Errorf("inferred=%v features=%v want true/[core]", res.Inferred, res.Features)
	}
	led, found, _ := LoadLedger(dst)
	if !found {
		t.Error("台帳が書かれていない")
	}
	if led.Features == nil || len(led.Features) != 0 {
		t.Errorf("台帳の features=%v want []", led.Features)
	}
}

// 台帳のハッシュと現物が一致する(＝配った版のまま触っていない)なら、黙って今の版にする。
func TestUpdate_OverwritesUnmodified(t *testing.T) {
	dst := t.TempDir()
	f := sampleFile(t)
	old := []byte("配った当時の版\n")
	writeAt(t, dst, f.Path, old)
	if err := SaveLedger(dst, Ledger{Kind: string(KindHub), Files: map[string]string{f.Path: Hash(old)}}); err != nil {
		t.Fatal(err)
	}
	res, err := Update(dst, KindHub, UpdateOptions{})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !has(res.Updated, f.Path) {
		t.Errorf("%s が Updated に無い: %+v", f.Path, res.Updated)
	}
	if got := readAt(t, dst, f.Path); string(got) != string(f.Content) {
		t.Error("今の版に更新されていない")
	}
	if _, err := os.Lstat(filepath.Join(dst, filepath.FromSlash(f.Path+NewSuffix))); err == nil {
		t.Error("未編集なのに .new を置いている")
	}
	// 台帳は今の版のハッシュに進む
	led, _, err := LoadLedger(dst)
	if err != nil {
		t.Fatalf("LoadLedger: %v", err)
	}
	if led.Files[f.Path] != Hash(f.Content) {
		t.Error("台帳が今の版に更新されていない")
	}
}

// 利用者が編集していたら、そのファイルは残して .new を隣に置く。
func TestUpdate_KeepsEditedAndWritesNew(t *testing.T) {
	dst := t.TempDir()
	f := sampleFile(t)
	shipped := []byte("配った当時の版\n")
	edited := []byte("私が直した版\n")
	writeAt(t, dst, f.Path, edited)
	if err := SaveLedger(dst, Ledger{Kind: string(KindHub), Files: map[string]string{f.Path: Hash(shipped)}}); err != nil {
		t.Fatal(err)
	}
	res, err := Update(dst, KindHub, UpdateOptions{})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	var found bool
	for _, c := range res.Conflicts {
		if c.Path == f.Path && c.New == f.Path+NewSuffix {
			found = true
		}
	}
	if !found {
		t.Errorf("%s が Conflicts に無い: %+v", f.Path, res.Conflicts)
	}
	if got := readAt(t, dst, f.Path); string(got) != string(edited) {
		t.Error("利用者の編集を壊している")
	}
	if got := readAt(t, dst, f.Path+NewSuffix); string(got) != string(f.Content) {
		t.Error(".new が今の版になっていない")
	}
	// 取り込み漏れを隠さないため、台帳は進めない
	led, _, err := LoadLedger(dst)
	if err != nil {
		t.Fatalf("LoadLedger: %v", err)
	}
	if led.Files[f.Path] != Hash(shipped) {
		t.Error("編集済みなのに台帳を進めている")
	}
}

// 台帳が無い hub は、既存ファイルを全部「編集済み」として扱う(決定 2026-09-04 → manual/init-update.md「決めたこと」)。
func TestUpdate_NoLedgerTreatsExistingAsEdited(t *testing.T) {
	dst := t.TempDir()
	f := sampleFile(t)
	writeAt(t, dst, f.Path, []byte("素性の分からない版\n"))
	res, err := Update(dst, KindHub, UpdateOptions{})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0].Path != f.Path {
		t.Errorf("conflicts=%+v want %s の 1 件", res.Conflicts, f.Path)
	}
}

// -force は編集済みでも上書きする。
func TestUpdate_Force(t *testing.T) {
	dst := t.TempDir()
	f := sampleFile(t)
	writeAt(t, dst, f.Path, []byte("私が直した版\n"))
	res, err := Update(dst, KindHub, UpdateOptions{Force: true})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(res.Conflicts) != 0 || !has(res.Updated, f.Path) {
		t.Errorf("force なのに conflicts=%+v updated=%v", res.Conflicts, res.Updated)
	}
	if got := readAt(t, dst, f.Path); string(got) != string(f.Content) {
		t.Error("上書きされていない")
	}
}

// 内容が既に今の版と同じなら Unchanged。台帳は追いつかせる。
func TestUpdate_Unchanged(t *testing.T) {
	dst := t.TempDir()
	f := sampleFile(t)
	writeAt(t, dst, f.Path, f.Content)
	res, err := Update(dst, KindHub, UpdateOptions{})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !has(res.Unchanged, f.Path) {
		t.Errorf("%s が Unchanged に無い: %v", f.Path, res.Unchanged)
	}
	led, _, err := LoadLedger(dst)
	if err != nil {
		t.Fatalf("LoadLedger: %v", err)
	}
	if led.Files[f.Path] != Hash(f.Content) {
		t.Error("台帳に記録されていない")
	}
}

// -dry-run は何も書かない(結果だけ返す)。
func TestUpdate_DryRunWritesNothing(t *testing.T) {
	dst := t.TempDir()
	f := sampleFile(t)
	writeAt(t, dst, f.Path, []byte("私が直した版\n"))
	res, err := Update(dst, KindHub, UpdateOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(res.Conflicts) != 1 {
		t.Errorf("conflicts=%d want 1", len(res.Conflicts))
	}
	if _, err := os.Lstat(filepath.Join(dst, filepath.FromSlash(f.Path+NewSuffix))); err == nil {
		t.Error("dry-run なのに .new を書いている")
	}
	if _, found, _ := LoadLedger(dst); found {
		t.Error("dry-run なのに台帳を書いている")
	}
	if len(res.Created) == 0 {
		t.Error("dry-run でも作られる予定のファイルは報告する")
	}
	if _, err := os.Lstat(filepath.Join(dst, "docs")); err == nil {
		t.Error("dry-run なのにファイルを作っている")
	}
}

// 書いた分は都度台帳に残す。最後にまとめて保存すると、途中で失敗したときに
// 「書いたのに台帳に無い」ファイルができ、次の update がそれを「利用者が編集した」と見て
// .new を置いてしまう(設計レビュー 2026-09-06 M8)。
func TestUpdate_途中で失敗しても書いた分は台帳に残る(t *testing.T) {
	dst := t.TempDir()
	if _, err := Install(dst, KindRepo); err != nil {
		t.Fatal(err)
	}
	// 配ったファイルを全部消して、update が作り直す状況にする
	files, err := Files(KindRepo)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 2 {
		t.Fatalf("テストの前提: 2 ファイル以上 (%d)", len(files))
	}
	for _, f := range files {
		if err := os.Remove(filepath.Join(dst, filepath.FromSlash(f.Path))); err != nil {
			t.Fatal(err)
		}
	}
	// 台帳も空にして、全ファイルが「作成」になるようにする
	if err := SaveLedger(dst, Ledger{Kind: string(KindRepo), Files: map[string]string{}}); err != nil {
		t.Fatal(err)
	}

	// 2 ファイル目の書き込みで失敗させる
	orig := writeFile
	n := 0
	writeFile = func(path string, b []byte) error {
		n++
		if n == 2 {
			return errors.New("書き込みに失敗した")
		}
		return orig(path, b)
	}
	t.Cleanup(func() { writeFile = orig })

	if _, err := Update(dst, KindRepo, UpdateOptions{}); err == nil {
		t.Fatal("エラーにならない")
	}

	led, _, err := LoadLedger(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(led.Files) != 1 {
		t.Errorf("1 ファイル目だけが台帳に残るはず: %v", led.Files)
	}
	if led.Files[files[0].Path] != Hash(files[0].Content) {
		t.Errorf("1 ファイル目の記録が違う: %v", led.Files)
	}
}

// 配布するファイルの書き込みは、置き換えに失敗しても元のファイルを壊さない(半端な内容で上書きしない)。
// 台帳は配ったファイルのハッシュを覚えているので、半端なファイルは次の update で「利用者が編集した」と
// 誤認されて .new が置かれる(設計レビュー 2026-09-06 M14)。
func TestWriteFileToDisk_置き換えに失敗しても元のファイルは壊れない(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "README.md")
	if err := writeFileToDisk(path, []byte("前回の内容\n")); err != nil {
		t.Fatal(err)
	}
	blockReplace(t, path)

	if err := writeFileToDisk(path, []byte("新しい内容\n")); err == nil {
		t.Fatal("エラーにならない")
	}
	if b, _ := os.ReadFile(path); string(b) != "前回の内容\n" {
		t.Errorf("元のファイルが変わった: %q", b)
	}
	des, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, de := range des {
		if strings.HasPrefix(de.Name(), ".") {
			t.Errorf("一時ファイルが残った: %s", de.Name())
		}
	}
}

// blockReplace は path を「原子的には書き換えられない」状態にする。path そのものは書けるので、
// 切り詰めてから書く os.WriteFile は成功して前回の内容を失い、一時ファイル経由の置き換えは失敗して前回の内容が残る。
// Windows: path を開いたままにする(Go の os.Open は FILE_SHARE_DELETE を付けないので、置き換えと削除が失敗する)。
// それ以外: 親ディレクトリの書き込み権限を外す(一時ファイルを作れない。root は権限を無視するので skip)。
func blockReplace(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return
	}
	if os.Getuid() == 0 {
		t.Skip("root は権限を無視するので、書けない置き場を作れない")
	}
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
}
