package catalog

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/indexdata"
	"github.com/pilefort/braindex/internal/render"
	"github.com/pilefort/braindex/internal/scan"
)

func sampleEntries() []render.Entry {
	return []render.Entry{
		{Repo: "alpha", Date: "2026-01-02", Kind: "notes", Title: "A", Summary: "a", Path: "alpha/docs/notes/a.md"},
		{Repo: "beta", Date: "", Kind: "notes", Title: "B", Summary: "b", Path: "beta/docs/notes/b.md"},
	}
}

// 走査の記録は説明行の直後・最初のリポ見出しの前に入り、読み戻すと同じ値になる。
// 表の読み手(indexdata.ParseCatalog)はその行を読み飛ばすので、行の読み取りは変わらない(A の互換)。
func TestCoverage_RoundTrip(t *testing.T) {
	gaps := []scan.Gap{
		{Rel: "alpha/docs/notes/locked", Dir: true, Reason: "The process cannot access the file because it is being used by another process."},
		{Rel: "beta/docs/notes/x.md", Dir: false, Reason: "permission denied"},
	}
	cov := Coverage{Known: true, Gaps: gaps}
	md := withCoverage(render.Render(sampleEntries(), "2026-08-07"), cov)

	want := "使い方: このファイルを grep → ヒット行のパス(root 相対)の実ファイルを読む。要旨だけで答えない\n" +
		"走査: 読めなかった範囲 2 件（この範囲のノートは載っていない。無いのか読めないのかは分からない）\n" +
		"- 読めなかった: alpha/docs/notes/locked/ — The process cannot access the file because it is being used by another process.\n" +
		"- 読めなかった: beta/docs/notes/x.md — permission denied\n" +
		"\n## alpha\n"
	if !strings.Contains(string(md), want) {
		t.Errorf("記録の位置か形が違う:\n%s", md)
	}

	got, err := ParseCoverage(md)
	if err != nil {
		t.Fatalf("ParseCoverage: %v", err)
	}
	if !reflect.DeepEqual(got, cov) {
		t.Errorf("読み戻し: got=%+v want=%+v", got, cov)
	}
	if !got.Known || got.Complete() {
		t.Errorf("記録あり・不完全のはず: %+v", got)
	}
	if g, ok := got.Gap("alpha/docs/notes/locked/deep/y.md"); !ok || g.Rel != "alpha/docs/notes/locked" {
		t.Errorf("ディレクトリの範囲は配下も含む: %+v %v", g, ok)
	}
	if g, ok := got.Gap("beta/docs/notes/x.md"); !ok || g.Dir {
		t.Errorf("ファイルの範囲: %+v %v", g, ok)
	}
	if _, ok := got.Gap("beta/docs/notes/y.md"); ok {
		t.Errorf("範囲外のパスに当たっている")
	}

	entries, err := indexdata.ParseCatalog(md)
	if err != nil {
		t.Fatalf("記録を足した索引を表の読み手が読めない: %v", err)
	}
	if len(entries) != 2 || entries[0].Path != "alpha/docs/notes/a.md" || entries[1].Path != "beta/docs/notes/b.md" {
		t.Errorf("表の読み取りが変わった: %+v", entries)
	}
}

// 読めなかった範囲が無ければ 1 行「読めなかった範囲なし」。読み戻すと Complete。
func TestCoverage_None(t *testing.T) {
	md := withCoverage(render.Render(sampleEntries(), "2026-08-07"), Coverage{Known: true})
	if !strings.Contains(string(md), "要旨だけで答えない\n走査: 読めなかった範囲なし\n\n## alpha\n") {
		t.Errorf("記録の位置か形が違う:\n%s", md)
	}
	got, err := ParseCoverage(md)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Complete() || len(got.Gaps) != 0 {
		t.Errorf("完全のはず: %+v", got)
	}
}

// この記録を書く前の版の索引(走査: の行が無い)は Known=false。エラーにはしない(前回の索引として読めるべき)。
func TestParseCoverage_OldIndexIsUnknown(t *testing.T) {
	old := render.Render(sampleEntries(), "2026-08-07")
	got, err := ParseCoverage(old)
	if err != nil {
		t.Fatal(err)
	}
	if got.Known || got.Complete() || len(got.Gaps) != 0 {
		t.Errorf("記録なし=完全性不明のはず: %+v", got)
	}
	// 空の索引・壊れた索引でも記録の有無だけを見る
	if got, err := ParseCoverage(nil); err != nil || got.Known {
		t.Errorf("空: %+v %v", got, err)
	}
}

// リポが 1 つも無い索引には末尾に足す。
func TestWithCoverage_EmptyIndex(t *testing.T) {
	md := withCoverage(render.Render(nil, "2026-08-07"), Coverage{Known: true})
	if !bytes.HasSuffix(md, []byte("要旨だけで答えない\n走査: 読めなかった範囲なし\n")) {
		t.Errorf("末尾に記録が無い:\n%s", md)
	}
	got, err := ParseCoverage(md)
	if err != nil || !got.Complete() {
		t.Errorf("読み戻し: %+v %v", got, err)
	}
}

// 記録の行が braindex の書く形でなければエラー(手で編集された。無言で「完全」にしない)。
func TestParseCoverage_Broken(t *testing.T) {
	head := "# 知識カタログ\n\n生成: 2026-08-07 / 1 リポジトリ / 1 件\n"
	cases := map[string]string{
		"件数が読めない":    head + "走査: なんとか\n\n## a\n",
		"件数と一覧が合わない": head + "走査: 読めなかった範囲 2 件（…）\n- 読めなかった: a/x.md — e\n\n## a\n",
		"パスが空":       head + "走査: 読めなかった範囲 1 件（…）\n- 読めなかった:  — e\n\n## a\n",
		"記録が 2 回":    head + "走査: 読めなかった範囲なし\n走査: 読めなかった範囲なし\n\n## a\n",
	}
	for desc, in := range cases {
		if _, err := ParseCoverage([]byte(in)); err == nil {
			t.Errorf("[%s] エラーになっていない", desc)
		}
	}
	// 一覧の後に別の行が続いても、一覧の終わりとして読む(理由は " — " で切り、ディレクトリは末尾の "/")
	ok := head + "走査: 読めなかった範囲 1 件（…）\n- 読めなかった: a/docs/notes/d/ — x — y\n\n## a\n"
	got, err := ParseCoverage([]byte(ok))
	if err != nil {
		t.Fatal(err)
	}
	want := []scan.Gap{{Rel: "a/docs/notes/d", Dir: true, Reason: "x — y"}}
	if !reflect.DeepEqual(got.Gaps, want) {
		t.Errorf("got=%+v want=%+v", got.Gaps, want)
	}
	// CRLF・BOM でも読める(checkout で変換された索引)
	crlf := "\xEF\xBB\xBF" + strings.ReplaceAll(ok, "\n", "\r\n")
	if got, err := ParseCoverage([]byte(crlf)); err != nil || !reflect.DeepEqual(got.Gaps, want) {
		t.Errorf("CRLF: %+v %v", got, err)
	}
}
