package review

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/catalog"
	"github.com/pilefort/braindex/internal/scan"
)

var update = flag.Bool("update", false, "ゴールデンファイルを更新する")

// afterCatalog は internal/scan の合成 testdata を再走査した「今回の索引」(catalog の e2e と同じ設定)。
func afterCatalog(t *testing.T) []byte {
	t.Helper()
	cfg := scan.Config{
		Root: filepath.Join("..", "scan", "testdata", "root"),
		Extra: []scan.ExtraRule{
			{Repo: "ext", Path: ".", Recursive: false, Kind: "root", Exclude: []string{"README.md", "CLAUDE.md"}},
		},
	}
	res, err := catalog.Build(cfg, "2026-08-07")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("警告なしを期待: %q", res.Warnings)
	}
	return res.Catalog
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("testdata %s: %v", name, err)
	}
	return b
}

// 前回の索引(testdata/before.md: golden を手で崩したもの)と再走査の結果を比べ、節の出力を golden と照合する。
// before.md の崩し方: root-a のタイトル・a.md の要旨・table.md の種別・d.md の日付を変え、
// gone.md(今回は無い)を足し、repo-flat の c.md(今回はある)を消した。
func TestDiffIndex_Golden(t *testing.T) {
	d, err := DiffIndex(readTestdata(t, "before.md"), afterCatalog(t))
	if err != nil {
		t.Fatalf("DiffIndex: %v", err)
	}
	if d.Before != 12 || d.After != 12 {
		t.Errorf("件数: before=%d after=%d want 12/12", d.Before, d.After)
	}
	added, changed, removed := d.Counts()
	if added != 1 || changed != 4 || removed != 1 {
		t.Errorf("追加 %d・変更 %d・削除 %d(want 1・4・1)", added, changed, removed)
	}
	var b strings.Builder
	WriteIndexSection(&b, d, "testdata/before.md")
	got := []byte(b.String())

	golden := filepath.Join("testdata", "index-section.md")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("golden 更新: %s", golden)
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("golden 読み込み: %v (先に `go test ./internal/review/ -update` で生成)", err)
	}
	if string(got) != string(want) {
		t.Errorf("節が golden と不一致:\n--- got ---\n%s", got)
	}
}

// 決定性: 同じ入力から 2 回作って、構造も出力もバイト一致する(map を経由するので並びを明示的に固定している)。
func TestDiffIndex_Deterministic(t *testing.T) {
	before, after := readTestdata(t, "before.md"), afterCatalog(t)
	var outs []string
	for i := 0; i < 2; i++ {
		d, err := DiffIndex(before, after)
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		WriteIndexSection(&b, d, "x")
		outs = append(outs, b.String())
	}
	if outs[0] != outs[1] {
		t.Errorf("2 回の出力が違う:\n--- 1 ---\n%s\n--- 2 ---\n%s", outs[0], outs[1])
	}
	if strings.Contains(outs[0], "\r") {
		t.Errorf("CRLF が混入")
	}
}

// 前回の索引が無い(空)なら全件が追加。
func TestDiffIndex_NoPrevious(t *testing.T) {
	d, err := DiffIndex(nil, afterCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	added, changed, removed := d.Counts()
	if d.Before != 0 || d.After != 12 || added != 12 || changed != 0 || removed != 0 {
		t.Errorf("before=%d after=%d 追加 %d 変更 %d 削除 %d", d.Before, d.After, added, changed, removed)
	}
	if len(d.Repos) != 7 || d.Repos[0].Repo != "ext" || d.Repos[6].Repo != "repo-proj" {
		t.Errorf("リポの並びが不正: %+v", d.Repos)
	}
}

// 同じ索引同士は差分なし。節には「- なし」が出る。
func TestDiffIndex_Same(t *testing.T) {
	after := afterCatalog(t)
	d, err := DiffIndex(after, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Repos) != 0 {
		t.Errorf("差分があってはならない: %+v", d.Repos)
	}
	var b strings.Builder
	WriteIndexSection(&b, d, "x")
	if !strings.Contains(b.String(), "前回 12 件 → 今回 12 件（追加 0・変更 0・削除 0）") || !strings.Contains(b.String(), "\n- なし\n") {
		t.Errorf("差分なしの表示が不正:\n%s", b.String())
	}
}

// 壊れた索引はエラー。どちら側かを添える。
func TestDiffIndex_BrokenInput(t *testing.T) {
	broken := []byte("| a | b |\n")
	if _, err := DiffIndex(broken, nil); err == nil || !strings.Contains(err.Error(), "前回の索引") {
		t.Errorf("前回側の壊れ: err=%v", err)
	}
	if _, err := DiffIndex(nil, broken); err == nil || !strings.Contains(err.Error(), "今回の索引") {
		t.Errorf("今回側の壊れ: err=%v", err)
	}
}
