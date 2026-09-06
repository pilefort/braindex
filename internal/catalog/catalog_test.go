package catalog

import (
	"bytes"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pilefort/braindex/internal/render"
	"github.com/pilefort/braindex/internal/scan"
	"github.com/pilefort/braindex/internal/scan/scantest"
)

var update = flag.Bool("update", false, "ゴールデンファイルを更新する")

// e2e は internal/scan の合成 testdata を丸ごと索引化する設定。
func e2eConfig() scan.Config {
	return scan.Config{
		Root: filepath.Join("..", "scan", "testdata", "root"),
		Extra: []scan.ExtraRule{
			{Repo: "ext", Path: ".", Recursive: false, Kind: "root",
				Exclude: []string{"README.md", "CLAUDE.md"}},
		},
	}
}

// golden.md は表の形式(render の出力)の正本で、indexdata と review の往復テスト(Parse → Render で元に戻る)も読む。
// Build が先頭に足す走査の記録は render の外なので golden には含めず、ここで「golden + 記録 = Build の出力」を確かめる。
func TestBuild_E2EGolden(t *testing.T) {
	res, err := Build(e2eConfig(), "2026-08-07")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	got := render.Render(res.Records, "2026-08-07")
	if res.Entries != 12 {
		t.Errorf("件数: want=12 got=%d", res.Entries)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("警告なしを期待: %q", res.Warnings)
	}
	if !res.Coverage.Complete() {
		t.Errorf("全件読めたので走査の記録は完全のはず: %+v", res.Coverage)
	}
	if string(res.Catalog) != string(withCoverage(got, res.Coverage)) || !strings.Contains(string(res.Catalog), "\n走査: 読めなかった範囲なし\n\n## ext\n") {
		t.Errorf("Build の出力が「表 + 走査の記録」でない:\n%s", res.Catalog)
	}

	golden := filepath.Join("testdata", "golden.md")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("golden 更新: %s", golden)
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("golden 読み込み: %v (先に `go test ./internal/catalog/ -update` で生成)", err)
	}
	if string(got) != string(want) {
		t.Errorf("catalog が golden と不一致:\n--- got ---\n%s", got)
	}
}

// 読めないファイルは警告にして飛ばし、残りは索引に載せる(無言スキップにしない)。
// 飛ばした範囲は走査の記録(Coverage)に「ファイル」として残り、索引の先頭にも書かれる。
func TestBuild_UnreadableFileWarns(t *testing.T) {
	root := t.TempDir()
	notes := filepath.Join(root, "r", "docs", "notes")
	if err := os.MkdirAll(notes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notes, "ok.md"), []byte("# ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(notes, "bad.md")
	if err := os.WriteFile(bad, []byte("# bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scantest.MakeUnreadable(t, bad)
	res, err := Build(scan.Config{Root: root}, "2026-08-07")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if res.Entries != 1 || !strings.Contains(string(res.Catalog), "ok.md") {
		t.Errorf("読める方だけ載るべき: entries=%d\n%s", res.Entries, res.Catalog)
	}
	if len(res.Warnings) != 1 || !strings.HasPrefix(res.Warnings[0], "r/docs/notes/bad.md: ") || strings.Count(res.Warnings[0], "bad.md") != 1 {
		t.Errorf("警告 1 件「r/docs/notes/bad.md: <理由>」(パスを繰り返さない)を期待: %q", res.Warnings)
	}
	if len(res.Coverage.Gaps) != 1 || res.Coverage.Gaps[0].Rel != "r/docs/notes/bad.md" || res.Coverage.Gaps[0].Dir || res.Coverage.Gaps[0].Reason == "" {
		t.Fatalf("走査の記録に {r/docs/notes/bad.md, ファイル, 理由} の 1 件を期待: %+v", res.Coverage)
	}
	if !strings.Contains(string(res.Catalog), "\n走査: 読めなかった範囲 1 件") ||
		!strings.Contains(string(res.Catalog), "\n- 読めなかった: r/docs/notes/bad.md — "+res.Coverage.Gaps[0].Reason+"\n") {
		t.Errorf("索引の先頭に読めなかった範囲が無い:\n%s", res.Catalog)
	}
	cov, err := ParseCoverage(res.Catalog)
	if err != nil || !reflect.DeepEqual(cov, res.Coverage) {
		t.Errorf("索引から読み戻した記録が違う: %+v %v", cov, err)
	}
}

// 列挙できないディレクトリは走査の記録に「ディレクトリ」として残る(配下のノートの有無は分からない)。
// 同じ状態から 2 回作ればバイト一致する(決定性)。
func TestBuild_UnreadableDirIsGap(t *testing.T) {
	root := t.TempDir()
	notes := filepath.Join(root, "r", "docs", "notes")
	for _, rel := range []string{"ok.md", "locked/x.md"} {
		p := filepath.Join(notes, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# "+rel+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	scantest.MakeUnreadable(t, filepath.Join(notes, "locked"))
	a, err := Build(scan.Config{Root: root}, "2026-08-07")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if a.Entries != 1 || len(a.Coverage.Gaps) != 1 || a.Coverage.Gaps[0].Rel != "r/docs/notes/locked" || !a.Coverage.Gaps[0].Dir {
		t.Fatalf("entries=%d coverage=%+v", a.Entries, a.Coverage)
	}
	if !strings.Contains(string(a.Catalog), "\n- 読めなかった: r/docs/notes/locked/ — ") {
		t.Errorf("索引の先頭にディレクトリの範囲(末尾 /)が無い:\n%s", a.Catalog)
	}
	b, err := Build(scan.Config{Root: root}, "2026-08-07")
	if err != nil {
		t.Fatalf("Build(2 回目): %v", err)
	}
	if !bytes.Equal(a.Catalog, b.Catalog) {
		t.Errorf("読めない範囲があるときに 2 回生成でバイト不一致(決定性違反)")
	}
}

func TestBuild_Deterministic(t *testing.T) {
	a, err := Build(e2eConfig(), "2026-08-07")
	if err != nil {
		t.Fatalf("Build(1 回目): %v", err)
	}
	b, err := Build(e2eConfig(), "2026-08-07")
	if err != nil {
		t.Fatalf("Build(2 回目): %v", err)
	}
	if string(a.Catalog) != string(b.Catalog) {
		t.Errorf("2 回生成でバイト不一致(決定性違反)")
	}
}

// clone 直後を模す: 同じ内容のツリーを 2 つ作り、mtime だけ変えて生成しても出力はバイト一致する
// (git は mtime を保存しないので、mtime に依存すると clone ごとに索引が変わる)。
// 日付を持たないノート(ext/docs/guides/style.md)を含める。
func TestBuild_DeterministicAcrossMtime(t *testing.T) {
	src := filepath.Join("..", "scan", "testdata", "root")
	a := copyTree(t, src)
	b := copyTree(t, src)
	touchAll(t, a, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	touchAll(t, b, time.Date(2030, 6, 15, 12, 0, 0, 0, time.UTC))
	guides := scan.ExtraRule{Repo: "ext", Path: "docs/guides", Kind: "guides"}
	cfgA, cfgB := e2eConfig(), e2eConfig()
	cfgA.Root, cfgB.Root = a, b
	cfgA.Extra = append(cfgA.Extra, guides)
	cfgB.Extra = append(cfgB.Extra, guides)
	ra, err := Build(cfgA, "2026-08-07")
	if err != nil {
		t.Fatalf("Build(a): %v", err)
	}
	rb, err := Build(cfgB, "2026-08-07")
	if err != nil {
		t.Fatalf("Build(b): %v", err)
	}
	undated := "|  | guides | 書き方の指針 | 本文 | ext/docs/guides/style.md |"
	if !strings.Contains(string(ra.Catalog), undated) {
		t.Fatalf("日付なしノートが空欄の行として載っていない:%s", ra.Catalog)
	}
	if !bytes.Equal(ra.Catalog, rb.Catalog) {
		t.Errorf("mtime が違うだけで出力が変わった(決定性違反):%s---%s", ra.Catalog, rb.Catalog)
	}
}

// copyTree は src を一時ディレクトリに複製して、そのパスを返す。
func copyTree(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		content, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// touchAll は root 以下の全ファイルの mtime を tm にそろえる。
func touchAll(t *testing.T, root string, tm time.Time) {
	t.Helper()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		return os.Chtimes(p, tm, tm)
	})
	if err != nil {
		t.Fatal(err)
	}
}
