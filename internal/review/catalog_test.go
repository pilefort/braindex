package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/render"
)

// catalog の golden(render の出力)を読み戻して render し直すと、元とバイト一致する(Parse は Render の逆)。
func TestParseCatalog_RoundTrip(t *testing.T) {
	golden := filepath.Join("..", "catalog", "testdata", "golden.md")
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("golden 読み込み: %v", err)
	}
	entries, err := ParseCatalog(want)
	if err != nil {
		t.Fatalf("ParseCatalog: %v", err)
	}
	if len(entries) != 12 {
		t.Errorf("件数: want=12 got=%d", len(entries))
	}
	// 半角 | は全角 ｜ に置換されて表に入っているので、読み戻した値も全角のまま(Render し直しても変わらない)
	var pipe render.Entry
	for _, e := range entries {
		if e.Path == "ext/20260722_root-b.md" {
			pipe = e
		}
	}
	if pipe.Title != "例外ルート直下B ｜ パイプ" || pipe.Repo != "ext" || pipe.Kind != "root" || pipe.Date != "2026-07-22" {
		t.Errorf("行の読み戻しが不正: %+v", pipe)
	}
	got := render.Render(entries, "2026-08-07")
	if string(got) != string(want) {
		t.Errorf("Render(Parse(golden)) が golden と不一致:\n--- got ---\n%s", got)
	}
}

// BOM と CRLF が付いていても同じ結果になる(checkout で変換された索引を読めるように)。
func TestParseCatalog_NormalizesBOMAndCRLF(t *testing.T) {
	src := "## r\n| 日付 | 種別 | タイトル | 要旨 | パス |\n|---|---|---|---|---|\n| 2026-01-02 | notes | T | S | r/docs/notes/t.md |\n"
	crlf := "\xEF\xBB\xBF" + strings.ReplaceAll(src, "\n", "\r\n")
	a, err := ParseCatalog([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseCatalog([]byte(crlf))
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 1 || len(b) != 1 || a[0] != b[0] {
		t.Errorf("正規化後に一致しない: %+v vs %+v", a, b)
	}
}

// 空の入力(前回の索引が無い)はエラーでなく 0 件。
func TestParseCatalog_Empty(t *testing.T) {
	entries, err := ParseCatalog(nil)
	if err != nil || len(entries) != 0 {
		t.Errorf("空入力: entries=%v err=%v", entries, err)
	}
}

// 壊れた索引(手で編集された)は無言で読み飛ばさずエラーにする。行番号を添える。
func TestParseCatalog_Broken(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"見出しより前の表", "| 日付 | 種別 | タイトル | 要旨 | パス |\n", "1 行目: リポの見出し"},
		{"列が足りない", "## r\n| 日付 | 種別 | タイトル | 要旨 | パス |\n|---|---|---|---|---|\n| 2026-01-02 | notes | T | r/t.md |\n", "4 行目: 表の列が 5 でない(4 列)"},
	}
	for _, c := range cases {
		_, err := ParseCatalog([]byte(c.src))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err=%v want %q", c.name, err, c.want)
		}
	}
}
