package mdhtml

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Opt-in: BRAINDEX_BROWSER_TEST=1 go test ./internal/mdhtml -run TestThreadBrowser -v
// 埋め込み JS(開閉の記憶・「新着」の印・全部畳む)は Go のテストからは形しか見られないので、
// 実ブラウザで操作して確かめる。外部サイトへは接続しない。
func TestThreadBrowser(t *testing.T) {
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
	md := RenderThread("索引の設計", []Entry{
		{At: "2026-09-06T12:00:00+09:00", Q: "3 つ目の質問", Body: "3 つ目の回答。\n"},
		{At: "2026-09-06T11:00:00+09:00", Q: "2 つ目の質問", Body: "2 つ目の回答。\n\n- [ ] 消し込みの項目\n"},
		{At: "2026-09-06T10:00:00+09:00", Q: "1 つ目の質問", Body: "1 つ目の回答。\n"},
	})
	if err := os.WriteFile(filepath.Join(dir, "thread.html"), []byte(ThreadPage(md, "索引の設計")), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("python", "thread_browser_test.py", dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}
