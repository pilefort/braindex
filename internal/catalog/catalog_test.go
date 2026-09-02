package catalog

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/scan"
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

func TestBuild_E2EGolden(t *testing.T) {
	res, err := Build(e2eConfig(), "2026-08-07")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	got := res.Catalog
	if res.Entries != 12 {
		t.Errorf("件数: want=12 got=%d", res.Entries)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("警告なしを期待: %q", res.Warnings)
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
func TestBuild_UnreadableFileWarns(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("chmod 000 で読めなくする方法が使えない環境")
	}
	root := t.TempDir()
	notes := filepath.Join(root, "r", "docs", "notes")
	if err := os.MkdirAll(notes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notes, "ok.md"), []byte("# ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(notes, "bad.md")
	if err := os.WriteFile(bad, []byte("# bad\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	res, err := Build(scan.Config{Root: root}, "2026-08-07")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if res.Entries != 1 || !strings.Contains(string(res.Catalog), "ok.md") {
		t.Errorf("読める方だけ載るべき: entries=%d\n%s", res.Entries, res.Catalog)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "bad.md") {
		t.Errorf("警告 1 件(bad.md)を期待: %q", res.Warnings)
	}
}

func TestBuild_Deterministic(t *testing.T) {
	a, _ := Build(e2eConfig(), "2026-08-07")
	b, _ := Build(e2eConfig(), "2026-08-07")
	if string(a.Catalog) != string(b.Catalog) {
		t.Errorf("2 回生成でバイト不一致(決定性違反)")
	}
}
