package template

import (
	"os"
	"path/filepath"
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
// (足していない機能のファイルは作らない・決定 2026-09-05)。
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

// 台帳が無い hub は、既存ファイルを全部「編集済み」として扱う(決定 2026-09-04)。
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
