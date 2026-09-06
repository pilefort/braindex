package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/scan"
	"github.com/pilefort/braindex/internal/scan/scantest"
)

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustContain(t *testing.T, what, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Errorf("%s に %q が無い:\n%s", what, sub, s)
		}
	}
}

// Build は今回の走査の記録と今の設定を索引の差分に渡す。前回にあって今回無い行のうち、列挙できなかった
// ディレクトリの中のものは「確認不能」、今の設定が見に行かない場所のものは「対象外」、残りが「削除」。
// アーカイブ候補の節にも、読めなかった範囲のノートを候補にしない断りが出る(設計レビュー補足 2026-09-06)。
func TestBuild_読めなかった範囲は削除にしない(t *testing.T) {
	root := t.TempDir()
	hub := t.TempDir()
	notes := filepath.Join(root, "r", "docs", "notes")
	writeFile(t, filepath.Join(notes, "keep.md"), "# 残る\n\n結論: 残る\n記録日: 2026-01-04\n")
	writeFile(t, filepath.Join(notes, "locked", "x.md"), "# 読めない範囲の中\n\n結論: x\n記録日: 2026-01-02\n")
	scantest.MakeUnreadable(t, filepath.Join(notes, "locked"))

	// 前回の索引(ディスク。hub は git 管理外なのでこれが前回になる)。前回は全部読めていた
	header := "| 日付 | 種別 | タイトル | 要旨 | パス |\n|---|---|---|---|---|\n"
	writeFile(t, filepath.Join(hub, "index", "catalog.md"),
		"# 知識カタログ(braindex 自動生成 — 手で編集しない)\n\n生成: 2026-08-19 / 1 リポジトリ / 4 件\n"+
			"走査: 読めなかった範囲なし\n\n## r\n"+header+
			"| 2026-01-04 | notes | 残る | 結論: 残る | r/docs/notes/keep.md |\n"+
			"| 2026-01-02 | notes/locked | 読めない範囲の中 | 結論: x | r/docs/notes/locked/x.md |\n"+
			"| 2026-01-01 | notes | 消えた | 結論: 無い | r/docs/notes/gone.md |\n"+
			"| 2026-01-03 | wiki | 置き場が設定から外れた | 結論: w | r/wiki/w.md |\n")

	res, err := Build(Input{
		Today:      "2026-09-02",
		Since:      "2026-08-19",
		SinceNote:  "テスト",
		Cfg:        scan.Config{Root: root},
		HubDir:     hub,
		CatalogRel: "index/catalog.md",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	got := string(res.Report)
	mustContain(t, "下書き", got,
		"前回 4 件 → 今回 1 件（追加 0・変更 0・削除 1・確認不能 1・対象外 1）。前回の索引: index/catalog.md（ディスク。",
		"走査の記録: 今回は読めなかった範囲 1 件（その範囲の行は削除でなく確認不能にした）／前回は読めなかった範囲なし\n",
		"- 今回読めなかった: r/docs/notes/locked/ — ",
		"\n### r\n",
		"- 削除: r/docs/notes/gone.md（2026-01-01・消えた）\n",
		"- 確認不能: r/docs/notes/locked/x.md（2026-01-02・読めない範囲の中）— 読めなかった範囲: r/docs/notes/locked/\n",
		"- 対象外: r/wiki/w.md（2026-01-03・置き場が設定から外れた）— 今の設定では走査しない場所\n",
		"## アーカイブ候補（機械条件のみ）\n",
		"\n読めなかった範囲（1 件・索引の節を見る）のノートは候補に入っていない。有無を確認できていないので、アーカイブの判断もしない。\n",
	)
	if strings.Contains(got, "- 削除: r/docs/notes/locked/x.md") || strings.Contains(got, "- 削除: r/wiki/w.md") {
		t.Errorf("確認不能・対象外を削除に数えている:\n%s", got)
	}
	found := false
	for _, w := range res.Warnings {
		if strings.HasPrefix(w, "r/docs/notes/locked: ") {
			found = true
		}
	}
	if !found {
		t.Errorf("読めなかった範囲の警告が無い: %q", res.Warnings)
	}
}

// 全部読めたときは断りを出さず、走査の記録は「読めなかった範囲なし」。前回が記録を持たない版なら「記録なし」と書く。
func TestBuild_全部読めたときの走査の記録(t *testing.T) {
	root := t.TempDir()
	hub := t.TempDir()
	writeFile(t, filepath.Join(root, "r", "docs", "notes", "keep.md"), "# 残る\n\n結論: 残る\n記録日: 2026-01-04\n")
	// 前回の索引は「走査:」の行を持たない旧い版
	writeFile(t, filepath.Join(hub, "index", "catalog.md"),
		"# 知識カタログ\n\n生成: 2026-08-19 / 1 リポジトリ / 1 件\n\n## r\n"+
			"| 日付 | 種別 | タイトル | 要旨 | パス |\n|---|---|---|---|---|\n"+
			"| 2026-01-04 | notes | 残る | 結論: 残る | r/docs/notes/keep.md |\n")
	res, err := Build(Input{Today: "2026-09-02", Since: "2026-08-19", SinceNote: "テスト", Cfg: scan.Config{Root: root}, HubDir: hub, CatalogRel: "index/catalog.md"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	got := string(res.Report)
	mustContain(t, "下書き", got,
		"前回 1 件 → 今回 1 件（追加 0・変更 0・削除 0）。",
		"走査の記録: 今回は読めなかった範囲なし／前回は記録なし（この記録を持たない索引。欠けがあったかは分からない）\n",
	)
	if strings.Contains(got, "候補に入っていない") {
		t.Errorf("読めなかった範囲が無いのに断りが出ている:\n%s", got)
	}
}
