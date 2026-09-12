package explain

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Opt-in: BRAINDEX_BROWSER_TEST=1 go test ./internal/explain -run TestExplainBrowser -v
// 埋め込み JS と CSS(目次の固定・読んでいる節の印・狭い画面での畳み・棒の伸び・動きの停止・
// 表だけの横スクロール)は Go のテストからは形しか見られないので、実ブラウザで確かめる。
// 外部サイトへは接続しない。
func TestExplainBrowser(t *testing.T) {
	if os.Getenv("BRAINDEX_BROWSER_TEST") != "1" {
		t.Skip("ブラウザ検査は BRAINDEX_BROWSER_TEST=1 と Python playwright が必要")
	}
	dir := t.TempDir()
	if d := os.Getenv("BRAINDEX_BROWSER_ARTIFACT_DIR"); d != "" {
		dir = d
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, dir, "fig1.svg", sampleSVG)
	md := strings.Join([]string{
		"# 目次ベースの検索",
		"",
		"## 基礎となる概念",
		"",
		"本文。図1 のとおり。",
		"",
		"![図1: 目次を検索単位にする](fig1.svg)",
		"",
		"### 用語",
		"",
		"本文。",
		"",
		"## 実験",
		"",
		"<!-- graph: bar x=手法 y=Recall@1,Recall@3 unit=% -->",
		"| 手法 | Recall@1 | Recall@3 |",
		"|---|---|---|",
		"| BM25 | 41.2 | 58.9 |",
		"| STAIR | 55.0 | 71.3 |",
		"",
		"## 幅の広い表",
		"",
		"| 手法の名前が長い列1 | 手法の名前が長い列2 | 手法の名前が長い列3 | 手法の名前が長い列4 |" +
			" 手法の名前が長い列5 | 手法の名前が長い列6 | 手法の名前が長い列7 | 手法の名前が長い列8 |",
		"|---|---|---|---|---|---|---|---|",
		"| 41.2 | 58.9 | 62.0 | 70.1 | 41.2 | 58.9 | 62.0 | 70.1 |",
		"",
		strings.Repeat("読むための本文。", 60),
		"",
	}, "\n")
	html, problems := Render(md, "目次ベースの検索", Options{BaseDir: dir})
	if len(problems) != 0 {
		t.Fatalf("問題が出た: %v", problems)
	}
	if err := os.WriteFile(filepath.Join(dir, "explain.html"), []byte(html), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("python", "explain_browser_test.py", dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}
