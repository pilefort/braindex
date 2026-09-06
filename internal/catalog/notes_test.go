package catalog

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/changehistory"
	"github.com/pilefort/braindex/internal/scan"
	"github.com/pilefort/braindex/internal/scan/scantest"
)

// Build は本文を読めたノートごとに内容ハッシュを返す(Records と同じ並び・同じパス)。索引の中には入れない。
func TestBuild_Notes(t *testing.T) {
	res, err := Build(e2eConfig(), "2026-08-07")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Notes) != len(res.Records) || len(res.Notes) != 12 {
		t.Fatalf("notes=%d records=%d", len(res.Notes), len(res.Records))
	}
	root := e2eConfig().Root
	for i, n := range res.Notes {
		if n.Path != res.Records[i].Path {
			t.Errorf("%d: パスの並びが Records と違う: %s != %s", i, n.Path, res.Records[i].Path)
		}
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(n.Path)))
		if err != nil {
			t.Fatal(err)
		}
		if n.Hash != changehistory.Hash(content) {
			t.Errorf("%s: ハッシュが本文と合わない", n.Path)
		}
		if strings.Contains(string(res.Catalog), n.Hash) {
			t.Errorf("%s: ハッシュが索引の中に書かれている", n.Path)
		}
	}
}

// 要旨より後ろの本文だけを変えると、索引はバイト一致のまま(行は日付・種別・タイトル・要旨・パスだけ)で、
// ハッシュだけが変わる。これが索引と別に記録を持つ理由。
func TestBuild_本文だけの変更は索引を変えずハッシュを変える(t *testing.T) {
	mk := func(body string) scan.Config {
		root := t.TempDir()
		p := filepath.Join(root, "r", "docs", "notes", "n.md")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# 題\n\n結論: 同じ要旨\n記録日: 2026-01-02\n\n"+body), 0o644); err != nil {
			t.Fatal(err)
		}
		return scan.Config{Root: root}
	}
	a, err := Build(mk("本文の後半\n"), "2026-08-07")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(mk("本文の後半を書き足した\n"), "2026-08-07")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Catalog, b.Catalog) {
		t.Errorf("索引が変わった:\n%s\n---\n%s", a.Catalog, b.Catalog)
	}
	if len(a.Notes) != 1 || len(b.Notes) != 1 || a.Notes[0].Hash == b.Notes[0].Hash {
		t.Errorf("ハッシュが変わっていない: %+v %+v", a.Notes, b.Notes)
	}
}

// 読めなかったファイルは Notes に入らない(走査の記録の側に残る)。読めなかったことを「本文が消えた」にしない。
func TestBuild_読めないファイルはNotesに無い(t *testing.T) {
	root := t.TempDir()
	notes := filepath.Join(root, "r", "docs", "notes")
	if err := os.MkdirAll(notes, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ok.md", "bad.md"} {
		if err := os.WriteFile(filepath.Join(notes, name), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	scantest.MakeUnreadable(t, filepath.Join(notes, "bad.md"))
	res, err := Build(scan.Config{Root: root}, "2026-08-07")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Notes) != 1 || res.Notes[0].Path != "r/docs/notes/ok.md" {
		t.Errorf("読めた方だけのはず: %+v", res.Notes)
	}
	if _, ok := res.Coverage.Gap("r/docs/notes/bad.md"); !ok {
		t.Errorf("読めなかった方は走査の記録にあるはず: %+v", res.Coverage)
	}
}
