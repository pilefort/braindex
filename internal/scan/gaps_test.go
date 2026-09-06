package scan

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/scan/scantest"
)

// scanFiles は見つけたファイルだけが要るテストの近道。
func scanFiles(cfg Config) ([]File, error) {
	res, err := Scan(cfg)
	return res.Files, err
}

func writeNote(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("# "+filepath.Base(p)+"\n\n本文\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 途中のディレクトリを列挙できないと、その範囲を Gaps に「ディレクトリ」として返す(配下は確認不能)。
// 走査は続き、読めた方のファイルは Files に載る。警告にも従来どおり同じ内容を 1 行出す。
func TestScan_UnreadableDirIsGap(t *testing.T) {
	root := t.TempDir()
	notes := filepath.Join(root, "r", "docs", "notes")
	writeNote(t, filepath.Join(notes, "ok.md"))
	writeNote(t, filepath.Join(notes, "locked", "x.md"))
	scantest.MakeUnreadable(t, filepath.Join(notes, "locked"))

	res, err := Scan(Config{Root: root})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(res.Files) != 1 || res.Files[0].Rel != "r/docs/notes/ok.md" {
		t.Errorf("読めた方だけ載るべき: %+v", res.Files)
	}
	if len(res.Gaps) != 1 || res.Gaps[0].Rel != "r/docs/notes/locked" || !res.Gaps[0].Dir || res.Gaps[0].Reason == "" {
		t.Fatalf("Gaps に {r/docs/notes/locked, Dir, 理由} の 1 件を期待: %+v", res.Gaps)
	}
	if len(res.Warnings) != 1 || !strings.HasPrefix(res.Warnings[0], "r/docs/notes/locked: ") {
		t.Errorf("警告 1 件「r/docs/notes/locked: <理由>」を期待: %q", res.Warnings)
	}
	g := res.Gaps[0]
	for rel, want := range map[string]bool{
		"r/docs/notes/locked/x.md":      true,
		"r/docs/notes/locked/deep/y.md": true,
		"r/docs/notes/locked":           true,
		"r/docs/notes/lockedx.md":       false, // 前方一致でなくセグメント単位
		"r/docs/notes/ok.md":            false,
	} {
		if got := g.Covers(rel); got != want {
			t.Errorf("Covers(%s)=%v want %v", rel, got, want)
		}
	}
}

// 非再帰 extra の起点を列挙できない場合も Gaps に入る(存在しない起点は設定の誤りなので警告だけ)。
func TestScan_UnreadableExtraBaseIsGap(t *testing.T) {
	root := t.TempDir()
	writeNote(t, filepath.Join(root, "r", "x", "a.md"))
	scantest.MakeUnreadable(t, filepath.Join(root, "r", "x"))

	res, err := Scan(Config{Root: root, Extra: []ExtraRule{{Repo: "r", Path: "x", Kind: "x"}}})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(res.Files) != 0 {
		t.Errorf("列挙できないのに拾っている: %+v", res.Files)
	}
	if len(res.Gaps) != 1 || res.Gaps[0].Rel != "r/x" || !res.Gaps[0].Dir {
		t.Errorf("Gaps に {r/x, Dir} の 1 件を期待: %+v", res.Gaps)
	}

	// 存在しない起点は確認できた事実なので Gaps には入れない(従来どおり警告のみ)
	res, err = Scan(Config{Root: root, Extra: []ExtraRule{{Repo: "r", Path: "missing", Kind: "x"}}})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(res.Gaps) != 0 || len(res.Warnings) != 1 {
		t.Errorf("存在しない起点は警告だけのはず: gaps=%+v warnings=%q", res.Gaps, res.Warnings)
	}
}

// ファイルが見つからないのと読めないのは別。ファイルの読み取り自体は catalog が行うので、
// ここでは「ファイルは Stat できて Files に載る(読めるかは見ない)」ことだけ確かめる。
func TestScan_UnreadableFileStillListed(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "r", "docs", "notes", "bad.md")
	writeNote(t, bad)
	scantest.MakeUnreadable(t, bad)
	res, err := Scan(Config{Root: root})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(res.Files) != 1 || res.Files[0].Rel != "r/docs/notes/bad.md" || len(res.Gaps) != 0 {
		t.Errorf("読めないファイルも発見はされる: files=%+v gaps=%+v", res.Files, res.Gaps)
	}
}

// SortGaps は Rel 昇順・重複なし。入力は変えない。
func TestSortGaps(t *testing.T) {
	in := []Gap{{Rel: "b/x.md"}, {Rel: "a/docs/notes", Dir: true, Reason: "r1"}, {Rel: "b/x.md", Reason: "dup"}}
	got := SortGaps(in)
	want := []Gap{{Rel: "a/docs/notes", Dir: true, Reason: "r1"}, {Rel: "b/x.md"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got=%+v want=%+v", got, want)
	}
	if in[0].Rel != "b/x.md" {
		t.Errorf("入力を書き換えた: %+v", in)
	}
	if SortGaps(nil) != nil {
		t.Errorf("空は nil のまま")
	}
}

// Covers は「今の設定がそのパスを見に行くか」をファイルシステムを見ずに答える。Scan と同じ規則。
func TestCovers(t *testing.T) {
	cfg := Config{
		Extra: []ExtraRule{
			{Repo: "ext", Path: ".", Kind: "root", Exclude: []string{"README.md"}},
			{Repo: "ext", Path: "research", Recursive: true, Kind: "research", Exclude: []string{"drafts", "*.draft.md", "topic-a/private"}},
			{Repo: "ext", Path: "docs/guides", Kind: "guides"},
		},
	}
	cases := []struct {
		rel  string
		want bool
		why  string
	}{
		{"a/docs/notes/x.md", true, "既定のノート置き場"},
		{"a/docs/notes/sub/deep/x.md", true, "サブディレクトリも"},
		{"a/docs/notes/X.MD", true, "拡張子は大文字小文字を問わない"},
		{"a/docs/decisions.md", true, "決定記録"},
		{"a/docs/notes/archive/x.md", false, "archive セグメント"},
		{"archive/docs/notes/x.md", false, "リポ名が archive"},
		{"a/docs/notes/x.txt", false, "md でない"},
		{"a/docs/x.md", false, "ノート置き場の外"},
		{"a/wiki/x.md", false, "notes_dirs に無い置き場"},
		{".hidden/docs/notes/x.md", false, "ドットで始まるリポ"},
		{"a", false, "リポだけ"},
		{"a/docs/notes/../x.md", false, "外へ出るパス"},
		{"a/docs/notes//x.md", false, "空のセグメント"},
		{"ext/top.md", true, "非再帰 extra の起点直下"},
		{"ext/README.md", false, "extra の exclude(名前)"},
		{"ext/sub/x.md", false, "非再帰 extra の起点直下でない"},
		{"ext/research/top.md", true, "再帰 extra の起点直下"},
		{"ext/research/topic-a/survey.md", true, "再帰 extra の配下"},
		{"ext/research/topic-a/notes.draft.md", false, "exclude のグロブ(ファイル名)"},
		{"ext/research/drafts/x.md", false, "exclude のディレクトリ名(枝ごと)"},
		{"ext/research/drafts/deep/x.md", false, "exclude のディレクトリ名(深い枝も)"},
		{"ext/research/topic-a/private/x.md", false, "exclude の相対パス(枝ごと)"},
		{"ext/researchx/x.md", false, "起点の前方一致でなくセグメント単位"},
		{"ext/docs/guides/style.md", true, "サブパス起点の非再帰 extra"},
		{"ext/docs/guides/deep/x.md", false, "非再帰なので配下は対象外"},
		{"other/research/x.md", false, "extra は指定リポだけ"},
	}
	for _, c := range cases {
		if got := Covers(cfg, c.rel); got != c.want {
			t.Errorf("[%s] Covers(%s)=%v want %v", c.why, c.rel, got, c.want)
		}
	}

	wiki := Config{NotesDirs: []string{"wiki"}}
	if !Covers(wiki, "a/wiki/x.md") || Covers(wiki, "a/docs/notes/x.md") {
		t.Errorf("notes_dirs=[wiki] なら wiki/ だけが対象")
	}
	if !Covers(wiki, "a/docs/decisions.md") {
		t.Errorf("decisions.md は notes_dirs と無関係に対象")
	}
	dot := Config{NotesDirs: []string{"."}}
	if !Covers(dot, "a/anything/x.md") || Covers(dot, "a/archive/x.md") {
		t.Errorf("notes_dirs=[.] はリポ内の全部(archive を除く)が対象")
	}
	if Covers(Config{}, "") {
		t.Errorf("空パスは対象外")
	}
}
