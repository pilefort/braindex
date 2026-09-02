package approvals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParse_TwoItems(t *testing.T) {
	d := Parse(load(t, "two-items.md"))
	if !strings.HasPrefix(d.Preamble, "# 承認待ち") || strings.Contains(d.Preamble, "## 1.") {
		t.Errorf("前文が見出しまで: %q", d.Preamble)
	}
	if len(d.Items) != 2 {
		t.Fatalf("項目数 = %d, want 2", len(d.Items))
	}
	a := d.Items[0]
	if a.N != 1 || a.Title != "設定ファイルの形式を JSON にするか TOML にするか" {
		t.Errorf("項目 1: n=%d title=%q", a.N, a.Title)
	}
	if got := a.Fields[FieldWhat]; got != "設定ファイル `app.json` の形式。" {
		t.Errorf("決めたいこと = %q", got)
	}
	if got := a.Fields[FieldIfUndecided]; got != "設定の読み込みが書けず、次の PR が止まる。" {
		t.Errorf("決めないとどうなるか = %q", got)
	}
	if len(a.Options) != 2 || a.Options[0].Key != "A" || a.Options[0].Label != "JSON" ||
		a.Options[0].Desc != "標準ライブラリだけで読める／コメントが書けない" || a.Options[1].Key != "B" {
		t.Errorf("選択肢 = %+v", a.Options)
	}
	if a.Recommended != "A" || a.Reason != "依存を増やさない方針（決定 2026-01-10）と合う" {
		t.Errorf("私の案 = %q 理由 = %q", a.Recommended, a.Reason)
	}
	if len(a.Warnings) != 0 {
		t.Errorf("項目 1 に warning: %v", a.Warnings)
	}
	if !strings.HasPrefix(a.Raw, "## 1. 設定ファイル") || !strings.HasSuffix(a.Raw, "止まる。\n") || strings.Contains(a.Raw, "## 2.") {
		t.Errorf("raw = %q", a.Raw)
	}

	b := d.Items[1]
	if b.N != 2 || len(b.Options) != 3 || b.Options[2].Key != "C" || b.Options[2].Label != "両方" {
		t.Errorf("項目 2: n=%d options=%+v", b.N, b.Options)
	}
	if b.Recommended != "" || b.Fields[FieldRecommend] != "案なし" {
		t.Errorf("案なし: rec=%q field=%q", b.Recommended, b.Fields[FieldRecommend])
	}
	if len(b.Holds) != 1 || !strings.Contains(b.Holds[0], "CI の仕様") {
		t.Errorf("保留行 = %v", b.Holds)
	}
	if len(b.Warnings) != 0 {
		t.Errorf("項目 2 に warning: %v", b.Warnings)
	}
}

func TestParse_Missing(t *testing.T) {
	d := Parse(load(t, "missing.md"))
	if len(d.Items) != 1 {
		t.Fatalf("項目数 = %d", len(d.Items))
	}
	it := d.Items[0]
	if it.N != 1 {
		t.Errorf("番号なし見出しは 1 から振る: %d", it.N)
	}
	want := []string{"なぜ今決めるか が未記載", "決めないとどうなるか が未記載",
		"選択肢 が 1 つ以下（A/B… で 2 つ以上、各案の得失つきで列挙する）", "私の案 C が選択肢に無い"}
	if strings.Join(it.Warnings, "|") != strings.Join(want, "|") {
		t.Errorf("warnings =\n%v\nwant\n%v", it.Warnings, want)
	}
	if it.Recommended != "" {
		t.Errorf("選択肢に無い案は推奨にしない: %q", it.Recommended)
	}
}

func TestParse_Empty(t *testing.T) {
	d := Parse(load(t, "empty.md"))
	if len(d.Items) != 0 || !strings.Contains(d.Preamble, "（なし") {
		t.Errorf("空: items=%d preamble=%q", len(d.Items), d.Preamble)
	}
}

func TestParse_AliasesAndCRLF(t *testing.T) {
	md := "# 承認待ち\r\n\r\n## 3) 題\r\n\r\n**何を決めるか**: X\r\n**背景:** Y\r\n**選択肢:**\r\n* a) 案 1 -- 得\r\n* b) 案 2\r\n  続き\r\n**推奨:** b. 理由\r\n**決めなかった場合:** Z\r\n"
	d := Parse([]byte(md))
	if len(d.Items) != 1 {
		t.Fatalf("項目数 = %d", len(d.Items))
	}
	it := d.Items[0]
	if it.N != 3 || it.Fields[FieldWhat] != "X" || it.Fields[FieldWhyNow] != "Y" || it.Fields[FieldIfUndecided] != "Z" {
		t.Errorf("別名: %+v", it.Fields)
	}
	if len(it.Options) != 2 || it.Options[0].Key != "A" || it.Options[0].Desc != "得" || it.Options[1].Key != "B" || it.Options[1].Desc != "続き" {
		t.Errorf("選択肢 = %+v", it.Options)
	}
	if it.Recommended != "B" || it.Reason != "理由" || len(it.Warnings) != 0 {
		t.Errorf("推奨 = %q 理由 = %q warnings = %v", it.Recommended, it.Reason, it.Warnings)
	}
}

// 先頭 BOM(Windows のエディタが付ける)は除去してから解析する。
// 見出しから始まるファイルでは、BOM が残ると 1 件目の見出しが読めず項目が 0 件になる。
func TestParse_BOM(t *testing.T) {
	// ソースに BOM リテラルを置かず、バイトで組み立てる(internal/extract のテストと同じ手)。
	withBOM := func(s string) []byte { return append([]byte{0xEF, 0xBB, 0xBF}, []byte(s)...) }

	d := Parse(withBOM("## 1. 題\n\n**決めたいこと:** X\n"))
	if len(d.Items) != 1 {
		t.Fatalf("項目数 = %d, want 1", len(d.Items))
	}
	if d.Items[0].Title != "題" {
		t.Errorf("題 = %q", d.Items[0].Title)
	}

	d2 := Parse(withBOM("# 承認待ち\n\n## 1. 題\n"))
	if d2.Preamble != "# 承認待ち" {
		t.Errorf("前文に BOM が残る: %q", d2.Preamble)
	}
}
