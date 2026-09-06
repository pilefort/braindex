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
	d, err := DiffIndex(readTestdata(t, "before.md"), afterCatalog(t), nil)
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
		d, err := DiffIndex(before, after, nil)
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
	d, err := DiffIndex(nil, afterCatalog(t), nil)
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
	d, err := DiffIndex(after, after, nil)
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

// 同じリポに 追加・変更・削除 が混じるときの並び(追加 → 変更 → 削除。各群の中はパス昇順)と、
// 複数列が変わった行の列名の順(日付・種別・タイトル・要旨)、日付なしの行の表示(タイトルだけ)を見る。
// golden(before.md)は 1 リポの中でこれらを同時に踏まないので、合成した索引で補う。
// 今回側の行順はわざとパスの降順にし、出力がパス昇順に並び直されることも確かめる。
func TestWriteIndexSection_OrderAndColumns(t *testing.T) {
	header := "## r\n| 日付 | 種別 | タイトル | 要旨 | パス |\n|---|---|---|---|---|\n"
	before := header +
		"| 2026-01-01 | notes | 消える | S | r/docs/notes/a-gone.md |\n" +
		"| 2026-01-02 | notes | 全部変わる | S | r/docs/notes/m.md |\n" +
		"|  | notes | 日付なし | S | r/docs/notes/nodate.md |\n"
	after := header +
		"|  | notes | 日付なしで増えた | S | r/docs/notes/z-new.md |\n" +
		"| 2026-01-05 | notes | 増えた | S | r/docs/notes/y-new.md |\n" +
		"|  | notes | 日付なし | S | r/docs/notes/nodate.md |\n" +
		"| 2026-01-03 | notes/x | 全部変わった | S2 | r/docs/notes/m.md |\n"
	d, err := DiffIndex([]byte(before), []byte(after), nil)
	if err != nil {
		t.Fatalf("DiffIndex: %v", err)
	}
	var b strings.Builder
	WriteIndexSection(&b, d, "x")
	want := "## 索引（件数と増減）\n\n" +
		"前回 3 件 → 今回 4 件（追加 2・変更 1・削除 1）。前回の索引: x\n" +
		"走査の記録: 今回は記録なし（この記録を持たない索引。欠けがあったかは分からない）／前回は記録なし（この記録を持たない索引。欠けがあったかは分からない）\n\n" +
		"### r\n" +
		"- 追加: r/docs/notes/y-new.md（2026-01-05・増えた）\n" +
		"- 追加: r/docs/notes/z-new.md（日付なしで増えた）\n" +
		"- 変更（日付・種別・タイトル・要旨）: r/docs/notes/m.md（2026-01-03・全部変わった）\n" +
		"- 削除: r/docs/notes/a-gone.md（2026-01-01・消える）\n"
	if got := b.String(); got != want {
		t.Errorf("節が想定と不一致:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}

// 壊れた索引はエラー。どちら側かを添える。
func TestDiffIndex_BrokenInput(t *testing.T) {
	broken := []byte("| a | b |\n")
	if _, err := DiffIndex(broken, nil, nil); err == nil || !strings.Contains(err.Error(), "前回の索引") {
		t.Errorf("前回側の壊れ: err=%v", err)
	}
	if _, err := DiffIndex(nil, broken, nil); err == nil || !strings.Contains(err.Error(), "今回の索引") {
		t.Errorf("今回側の壊れ: err=%v", err)
	}
	// 走査の記録が壊れていても同じく、どちら側かを添えてエラー
	badRecord := []byte("# c\n\n走査: なんとか\n\n## r\n")
	if _, err := DiffIndex(badRecord, nil, nil); err == nil || !strings.Contains(err.Error(), "前回の索引") {
		t.Errorf("前回側の記録の壊れ: err=%v", err)
	}
}

// 前回にあって今回無い行は、今回の走査で読めなかった範囲にあれば「確認不能」、今の設定が見に行かない場所なら
// 「対象外」、どちらでもなければ「削除」。確認不能・対象外は削除の数に入れず、節では別の見出し語で出す。
// 前回の索引の記録(前回読めなかった範囲)も節に書く——その範囲の行は今回「追加」に出うるので。
// (設計レビュー補足 2026-09-06「部分的な索引を完全な調査結果として扱わない」)
func TestDiffIndex_確認不能と対象外(t *testing.T) {
	header := "| 日付 | 種別 | タイトル | 要旨 | パス |\n|---|---|---|---|---|\n"
	before := "# 知識カタログ\n\n生成: 2026-08-01 / 1 リポジトリ / 7 件\n" +
		"走査: 読めなかった範囲 1 件（…）\n- 読めなかった: r/docs/notes/old-locked.md — e\n\n" +
		"## r\n" + header +
		"| 2026-01-01 | notes | 消えた | S | r/docs/notes/gone.md |\n" +
		"| 2026-01-02 | notes/locked | 読めない範囲の中 | S | r/docs/notes/locked/x.md |\n" +
		"| 2026-01-03 | notes | 読めないファイル | S | r/docs/notes/bad.md |\n" +
		"| 2026-01-04 | notes | 残る | S | r/docs/notes/keep.md |\n" +
		"| 2026-01-05 | research | 起点は残るが無い | S | r/research/a.md |\n" +
		"| 2026-01-06 | research | exclude が足された | S | r/research/b.draft.md |\n" +
		"| 2026-01-07 | wiki | 置き場が設定から外れた | S | r/wiki/w.md |\n"
	after := "# 知識カタログ\n\n生成: 2026-08-07 / 1 リポジトリ / 2 件\n" +
		"走査: 読めなかった範囲 2 件（…）\n" +
		"- 読めなかった: r/docs/notes/bad.md — permission denied\n" +
		"- 読めなかった: r/docs/notes/locked/ — permission denied\n\n" +
		"## r\n" + header +
		"| 2026-01-04 | notes | 残る | S | r/docs/notes/keep.md |\n" +
		"| 2026-01-08 | notes | 前回読めなかった | S | r/docs/notes/old-locked.md |\n"
	cfg := &scan.Config{Extra: []scan.ExtraRule{{Repo: "r", Path: "research", Recursive: true, Kind: "research", Exclude: []string{"*.draft.md"}}}}
	d, err := DiffIndex([]byte(before), []byte(after), cfg)
	if err != nil {
		t.Fatalf("DiffIndex: %v", err)
	}
	added, changed, removed := d.Counts()
	unconfirmed, outOfScope := d.Held()
	if added != 1 || changed != 0 || removed != 2 || unconfirmed != 2 || outOfScope != 2 {
		t.Errorf("追加 %d・変更 %d・削除 %d・確認不能 %d・対象外 %d(want 1・0・2・2・2)", added, changed, removed, unconfirmed, outOfScope)
	}
	if touched := d.Touched(); len(touched) != 1 || !touched["r/docs/notes/old-locked.md"] {
		t.Errorf("touched は追加・変更だけ: %v", touched)
	}
	var b strings.Builder
	WriteIndexSection(&b, d, "x")
	want := "## 索引（件数と増減）\n\n" +
		"前回 7 件 → 今回 2 件（追加 1・変更 0・削除 2・確認不能 2・対象外 2）。前回の索引: x\n" +
		"走査の記録: 今回は読めなかった範囲 2 件（その範囲の行は削除でなく確認不能にした）／前回は読めなかった範囲 1 件（その範囲の行は今回「追加」に出うる）\n" +
		"- 今回読めなかった: r/docs/notes/bad.md — permission denied\n" +
		"- 今回読めなかった: r/docs/notes/locked/ — permission denied\n" +
		"- 前回読めなかった: r/docs/notes/old-locked.md — e\n" +
		"\n### r\n" +
		"- 追加: r/docs/notes/old-locked.md（2026-01-08・前回読めなかった）\n" +
		"- 削除: r/docs/notes/gone.md（2026-01-01・消えた）\n" +
		"- 削除: r/research/a.md（2026-01-05・起点は残るが無い）\n" +
		"- 確認不能: r/docs/notes/bad.md（2026-01-03・読めないファイル）— 読めなかった範囲: r/docs/notes/bad.md\n" +
		"- 確認不能: r/docs/notes/locked/x.md（2026-01-02・読めない範囲の中）— 読めなかった範囲: r/docs/notes/locked/\n" +
		"- 対象外: r/research/b.draft.md（2026-01-06・exclude が足された）— 今の設定では走査しない場所\n" +
		"- 対象外: r/wiki/w.md（2026-01-07・置き場が設定から外れた）— 今の設定では走査しない場所\n"
	if got := b.String(); got != want {
		t.Errorf("節が想定と不一致:\n--- got ---\n%s--- want ---\n%s", got, want)
	}

	// cfg が無ければ対象外の判定はせず、読めなかった範囲に無い行は削除になる
	d2, err := DiffIndex([]byte(before), []byte(after), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, removed2 := d2.Counts()
	unconfirmed2, outOfScope2 := d2.Held()
	if removed2 != 4 || unconfirmed2 != 2 || outOfScope2 != 0 {
		t.Errorf("cfg なし: 削除 %d・確認不能 %d・対象外 %d(want 4・2・0)", removed2, unconfirmed2, outOfScope2)
	}
}
