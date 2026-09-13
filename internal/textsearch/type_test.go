package textsearch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pilefort/braindex/internal/scan"
)

func TestSearchType(t *testing.T) {
	root := t.TempDir()
	var sc scan.Result
	for name, content := range map[string]string{"failure.md": "\ufeff# 題\r\n種別: 失敗\r\n検索語\r\n", "none.md": "検索語\n", "bad.md": "種別: failure\n検索語\n", "howto.md": "種別: 手順\n検索語\n"} {
		p := filepath.Join(root, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		sc.Files = append(sc.Files, scan.File{Abs: p, Rel: name, Repo: "r", Kind: "notes"})
	}
	for _, tt := range []struct {
		typ        string
		files      int
		normalized string
	}{{"失敗", 1, "失敗"}, {"failure", 1, "失敗"}, {"none", 2, "未記入"}, {"未記入", 2, "未記入"}, {"", 4, ""}} {
		r, err := Search(sc, Query{Terms: []string{"検索語"}, Type: tt.typ})
		if err != nil || r.Files != tt.files || len(r.Hits) != tt.files || len(r.Gaps) != 0 || len(r.Warnings) != 1 || r.Query.Type != tt.normalized {
			t.Fatalf("%s: %+v, %v", tt.typ, r, err)
		}
		if tt.files == 1 && (r.Hits[0].Type != "失敗" || r.Hits[0].Line != 3) {
			t.Fatalf("hit = %+v", r.Hits[0])
		}
	}
	if _, err := Search(sc, Query{Terms: []string{"検索語"}, Type: "決定"}); err == nil {
		t.Fatal("不正な条件を受理")
	}
}
