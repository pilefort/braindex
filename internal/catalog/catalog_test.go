package catalog

import (
	"flag"
	"os"
	"path/filepath"
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
	got, n, err := Build(e2eConfig(), "2026-08-07")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if n != 12 {
		t.Errorf("件数: want=12 got=%d", n)
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

func TestBuild_Deterministic(t *testing.T) {
	a, _, _ := Build(e2eConfig(), "2026-08-07")
	b, _, _ := Build(e2eConfig(), "2026-08-07")
	if string(a) != string(b) {
		t.Errorf("2 回生成でバイト不一致(決定性違反)")
	}
}
