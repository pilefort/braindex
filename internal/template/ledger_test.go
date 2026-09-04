package template

import (
	"os"
	"path/filepath"
	"testing"
)

// Install は展開したファイルのハッシュを台帳に記録する。update が「利用者が編集したか」を
// 見分ける材料になる(決定 2026-09-04)。
func TestInstall_WritesLedger(t *testing.T) {
	dst := t.TempDir()
	res, err := Install(dst, KindHub)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	led, found, err := LoadLedger(dst)
	if err != nil {
		t.Fatalf("LoadLedger: %v", err)
	}
	if !found {
		t.Fatal("台帳が書かれていない")
	}
	if led.Version != LedgerVersion {
		t.Errorf("version=%d want %d", led.Version, LedgerVersion)
	}
	if led.Kind != string(KindHub) {
		t.Errorf("kind=%q want %q", led.Kind, KindHub)
	}
	if len(led.Files) != len(res.Created) {
		t.Errorf("台帳の件数=%d want %d(作成した数)", len(led.Files), len(res.Created))
	}
	// 記録されたハッシュは、実際に置いたファイルの内容と一致する
	for _, p := range res.Created {
		b, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(p)))
		if err != nil {
			t.Fatalf("読めない %s: %v", p, err)
		}
		if got := led.Files[p]; got != Hash(b) {
			t.Errorf("%s: 台帳=%q want %q", p, got, Hash(b))
		}
	}
}

// 既存ファイル(展開をスキップしたもの)は記録しない。素性が分からないので、update では
// 「編集済み」として扱わせる。
func TestInstall_LedgerSkipsExisting(t *testing.T) {
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(dst, "README.md"), []byte("私の README\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(dst, KindHub); err != nil {
		t.Fatalf("Install: %v", err)
	}
	led, _, err := LoadLedger(dst)
	if err != nil {
		t.Fatalf("LoadLedger: %v", err)
	}
	if _, ok := led.Files["README.md"]; ok {
		t.Error("既存ファイルを台帳に記録している")
	}
}

// 台帳の書き出しは決定的(同じ内容なら常に同じバイト列)。
func TestSaveLedger_Deterministic(t *testing.T) {
	l := Ledger{Kind: string(KindHub), Files: map[string]string{
		"docs/overview.md": "b", "README.md": "a", "work/TODO.md": "c",
	}}
	var got [2][]byte
	for i := range got {
		dst := t.TempDir()
		if err := SaveLedger(dst, l); err != nil {
			t.Fatalf("SaveLedger: %v", err)
		}
		b, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(LedgerPath)))
		if err != nil {
			t.Fatal(err)
		}
		got[i] = b
	}
	if string(got[0]) != string(got[1]) {
		t.Error("2 回の書き出しが一致しない")
	}
	if want := byte('\n'); got[0][len(got[0])-1] != want {
		t.Error("末尾が改行で終わっていない")
	}
}

// 台帳が無い hub でも LoadLedger はエラーにしない(初めての update で普通に起きる)。
func TestLoadLedger_Missing(t *testing.T) {
	led, found, err := LoadLedger(t.TempDir())
	if err != nil {
		t.Fatalf("台帳が無いだけでエラー: %v", err)
	}
	if found {
		t.Error("found=true になっている")
	}
	if led.Files == nil {
		t.Error("Files が nil。呼び出し側が書き込めない")
	}
}
