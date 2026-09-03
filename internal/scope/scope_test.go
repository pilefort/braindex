package scope

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "catalog.md"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func titles(es []Entry) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Title)
	}
	return out
}

// catalog の表を Entry にする。見出し行・区切り行は飛ばし、リポは H2 から取る。
func TestParseCatalog(t *testing.T) {
	es, err := ParseCatalog(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"記事の長さと読了率", "見出しの指針", "長さの測り方"}
	if got := titles(es); !reflect.DeepEqual(got, want) {
		t.Errorf("titles=%v want %v", got, want)
	}
	if es[0].Repo != "repo-a" || es[2].Repo != "repo-b" || es[2].Path != "repo-b/docs/notes/common/measure.md" || es[2].Date != "2026-08-25" {
		t.Errorf("列の対応が違う: %+v", es)
	}
}

// 形式が変わったら(列数が 5 でない)エラーにして気づけるようにする。
func TestParseCatalog_BrokenRow(t *testing.T) {
	if _, err := ParseCatalog([]byte("## r\n| a | b |\n")); err == nil {
		t.Error("5 列でない行がエラーにならない")
	}
}

// topic はタイトル・要旨・パスの部分一致でリポをまたぐ。repo は完全一致。
func TestFilter(t *testing.T) {
	es, _ := ParseCatalog(fixture(t))
	if got := titles(Filter(es, "長さ", "")); !reflect.DeepEqual(got, []string{"記事の長さと読了率", "長さの測り方"}) {
		t.Errorf("topic: %v", got)
	}
	if got := titles(Filter(es, "HEADING", "")); !reflect.DeepEqual(got, []string{"見出しの指針"}) { // パス・大小無視
		t.Errorf("topic(path): %v", got)
	}
	if got := titles(Filter(es, "", "repo-a")); !reflect.DeepEqual(got, []string{"記事の長さと読了率", "見出しの指針"}) {
		t.Errorf("repo: %v", got)
	}
	if got := titles(Filter(es, "長さ", "repo-b")); !reflect.DeepEqual(got, []string{"長さの測り方"}) {
		t.Errorf("topic+repo: %v", got)
	}
	if got := Filter(es, "無い語", ""); len(got) != 0 {
		t.Errorf("一致なし: %v", got)
	}
}

func TestChunk(t *testing.T) {
	es := []Entry{{Path: "1"}, {Path: "2"}, {Path: "3"}, {Path: "4"}, {Path: "5"}}
	got := Chunk(es, 2)
	if len(got) != 3 || len(got[0]) != 2 || len(got[2]) != 1 || got[2][0].Path != "5" {
		t.Errorf("chunk=%v", got)
	}
	if got := Chunk(nil, 2); len(got) != 0 {
		t.Errorf("空: %v", got)
	}
	if got := Chunk(es, 0); len(got) != 1 { // 0 以下は既定 12
		t.Errorf("size 0: %v", got)
	}
}

// ディレクトリ列挙: 再帰・*.md だけ・パス昇順・タイトルと日付は本文から。
func TestEnumerateDir(t *testing.T) {
	es, err := EnumerateDir(filepath.Join("testdata", "notes"))
	if err != nil {
		t.Fatal(err)
	}
	wantPaths := []string{"length_no.md", "length_yes.md", "sub/heading.md"}
	var paths []string
	for _, e := range es {
		paths = append(paths, e.Path)
	}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Errorf("paths=%v want %v", paths, wantPaths)
	}
	if es[0].Title != "長さは読了率に無関係" || es[0].Date != "2026-08-25" || es[0].Repo != "notes" || es[0].Kind != "dir" {
		t.Errorf("先頭の内容が違う: %+v", es[0])
	}
	if _, err := EnumerateDir(filepath.Join("testdata", "nope")); err == nil {
		t.Error("無いディレクトリがエラーにならない")
	}
}

// 再現シナリオ: 矛盾する 2 ノートが同じ走査対象に入る。
func TestBuild_DirTopic(t *testing.T) {
	r, err := Build(nil, Options{Dir: filepath.Join("testdata", "notes"), Topic: "長さ"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Entries != 2 || len(r.Chunks) != 1 || r.Mode != "topic:長さ" {
		t.Errorf("result=%+v", r)
	}
	if got := titles(r.Chunks[0]); !reflect.DeepEqual(got, []string{"長さは読了率に無関係", "長さは読了率に効く"}) {
		t.Errorf("titles=%v", got)
	}
}

// mode の表示: full / repo:<名> / topic:<語>(topic が優先)。
func TestBuild_Mode(t *testing.T) {
	for _, c := range []struct {
		o    Options
		mode string
		n    int
	}{
		{Options{}, "full", 3},
		{Options{Repo: "repo-a"}, "repo:repo-a", 2},
		{Options{Topic: "長さ", Repo: "repo-a"}, "topic:長さ", 1},
	} {
		r, err := Build(fixture(t), c.o)
		if err != nil {
			t.Fatal(err)
		}
		if r.Mode != c.mode || r.Entries != c.n {
			t.Errorf("%+v: mode=%q n=%d want %q %d", c.o, r.Mode, r.Entries, c.mode, c.n)
		}
	}
}

// 決定性: 同一 catalog から同一出力(2 回生成してバイト一致)。
func TestRender_Deterministic(t *testing.T) {
	var outs [][]byte
	for i := 0; i < 2; i++ {
		r, err := Build(fixture(t), Options{Size: 2})
		if err != nil {
			t.Fatal(err)
		}
		outs = append(outs, Render(r))
	}
	if !bytes.Equal(outs[0], outs[1]) {
		t.Errorf("出力が一致しない:\n%s\n---\n%s", outs[0], outs[1])
	}
	want := "# braindex scope: full\n対象 3 件 / 2 chunk\n\n## chunk 1 (2 件)\n" +
		"- [repo-a notes/project 2026-08-20] 記事の長さと読了率  —  repo-a/docs/notes/project/length.md\n" +
		"- [repo-a notes/project 2026-08-10] 見出しの指針  —  repo-a/docs/notes/project/heading.md\n\n" +
		"## chunk 2 (1 件)\n" +
		"- [repo-b notes/common 2026-08-25] 長さの測り方  —  repo-b/docs/notes/common/measure.md\n\n"
	if string(outs[0]) != want {
		t.Errorf("出力が違う:\n%s\nwant:\n%s", outs[0], want)
	}
}
