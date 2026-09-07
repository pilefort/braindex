package news

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/pilefort/braindex/internal/feed"
)

// Opt-in: BRAINDEX_BROWSER_TEST=1 go test ./internal/news -run TestNewsBrowser -v
// Pythonのplaywrightを使う。CIと手元の同じHTMLを実ブラウザで操作する。
func TestNewsBrowser(t *testing.T) {
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
	results := []Result{{Source: Source{Name: "開発だより", Category: "開発"}, New: []feed.Entry{{ID: "article-1", Title: "言語によって、メモリの扱いはどう違う？", Summary: "プログラムが使うメモリの管理方法を、具体的なコードとともに解説する記事です。", Link: "https://example.com/1", Published: "2026-01-02"}, {ID: "article-2", Title: "ブラウザの中でコマンドを練習する", Summary: "コマンド操作を試せる学習環境の紹介です。", Link: "https://example.com/2", Published: "2026-01-02"}}}, {Source: Source{Name: "科学だより", Category: "科学"}, New: []feed.Entry{{ID: "article-3", Title: "材料の性質を変える仕組みを知る", Summary: "材料のつながり方と強度の関係について、実験結果を紹介する記事です。", Link: "https://example.com/3", Published: "2026-01-02"}}}}
	introOV, artsOV := ParseOverview(overviewMD)
	for name, b := range map[string][]byte{"overview.html": RenderOverview("k1", "概要の試験", introOV, artsOV), "digest.html": RenderHTML(results, DigestOptions{Today: "2026-01-02", Layer: "daily", LibraryHref: "reading.html"}), "reading.html": RenderReading(Reading{Articles: map[string]*ReadingArticle{"article-1": {Keep: Keep{ID: "article-1", Title: "言語とメモリ", Link: "https://example.com/1", Summary: "メモリ管理の入門"}, Date: "2026-01-02", Status: "later", Questions: []Question{{ID: "q1", Mode: "stuck", Text: "前提から教えて", Created: "2026-01-02T10:00:00Z", Answer: "メモリは、プログラムが作業中の情報を置く場所です。\n\n出典: https://example.com/1"}}}}, Receipts: map[string]string{}})} {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("python", "ui_browser_test.py", dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		t.Fatal(err)
	}
}
