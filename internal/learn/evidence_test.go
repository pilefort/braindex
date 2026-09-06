package learn

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/scan"
	"github.com/pilefort/braindex/internal/textsearch"
)

// fakeSearch は本文検索の口を差し替える。呼ばれた条件を記録し、決めた結果を返す。
func fakeSearch(res textsearch.Result, err error) (Searcher, *[]textsearch.Query) {
	var calls []textsearch.Query
	return func(q textsearch.Query) (textsearch.Result, error) {
		calls = append(calls, q)
		if err != nil {
			return textsearch.Result{}, err
		}
		res.Query = q
		return res, nil
	}, &calls
}

func hit(path string, line int, terms ...string) textsearch.Hit {
	return textsearch.Hit{Repo: strings.SplitN(path, "/", 2)[0], Kind: "notes", Path: path, Line: line, Col: 1, Text: "本文の転記 SECRET-BODY", Terms: terms}
}

// 照合前の候補: 索引に無い語が 2 節に分かれて載っている
func candidates() Report {
	return Report{Today: "2026-09-05", Days: 14, Sources: map[string]int{"index": 3, "sessions": 50, "keep": 2, "corrections": 2},
		Unsettled:      []Item{{Word: "kubernetes", Sessions: 4}, {Word: "istio", Sessions: 3}},
		Stumbles:       []Item{{Word: "ingress", Corrections: 2, Sessions: 2}},
		ReadNotWritten: []Item{{Word: "webassembly", Keeps: 2}},
	}
}

// 本文にある語は「発見」と位置、無い語は走査した範囲が全部読めていれば「未発見」。訂正の文脈の節は照合しない。
func TestVerify_発見と未発見(t *testing.T) {
	r := candidates()
	search, calls := fakeSearch(textsearch.Result{Files: 7, Hits: []textsearch.Hit{
		hit("repo-a/docs/notes/k8s.md", 12, "kubernetes"),
		hit("repo-a/docs/notes/k8s.md", 30, "kubernetes"),
		hit("repo-a/docs/notes/wasm.md", 3, "webassembly"),
		hit("repo-b/docs/decisions.md", 7, "kubernetes"),
		hit("repo-b/docs/notes/mesh.md", 2, "kubernetes"),
	}}, nil)
	if err := Verify(&r, search, VerifyOptions{MaxLocations: 3}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Fatalf("本文検索は 1 回のはず: %d", len(*calls))
	}
	q := (*calls)[0]
	if strings.Join(q.Terms, ",") != "kubernetes,istio,webassembly" || !q.Any || !q.WholeWord || q.MatchCase {
		t.Errorf("検索条件が違う: %+v", q)
	}
	k := r.Unsettled[0].Evidence
	if k == nil || k.Status != Found || k.Files != 3 || k.Lines != 4 || len(k.Locations) != 3 ||
		k.Locations[0] != (Location{Path: "repo-a/docs/notes/k8s.md", Line: 12}) || k.Locations[2] != (Location{Path: "repo-b/docs/decisions.md", Line: 7}) {
		t.Errorf("kubernetes: %+v", k)
	}
	if i := r.Unsettled[1].Evidence; i == nil || i.Status != Absent || i.Files != 0 || i.Lines != 0 || len(i.Locations) != 0 {
		t.Errorf("istio: %+v", i)
	}
	if w := r.ReadNotWritten[0].Evidence; w == nil || w.Status != Found || w.Files != 1 || w.Lines != 1 {
		t.Errorf("webassembly: %+v", w)
	}
	if r.Stumbles[0].Evidence != nil {
		t.Errorf("訂正の文脈の節は照合しないはず: %+v", r.Stumbles[0].Evidence)
	}
	v := r.Verification
	if v == nil || !v.Done || v.Words != 3 || v.Files != 7 || len(v.Gaps) != 0 || !v.Complete {
		t.Errorf("照合の要約: %+v", v)
	}
}

// 読めなかった範囲があるときは、未発見の語を「無い」と断定せず「確認不能」にする。発見した語は発見のまま。
func TestVerify_読めなかった範囲があれば未発見を断定しない(t *testing.T) {
	r := candidates()
	search, _ := fakeSearch(textsearch.Result{Files: 6,
		Hits:     []textsearch.Hit{hit("repo-a/docs/notes/k8s.md", 12, "kubernetes")},
		Gaps:     []scan.Gap{{Rel: "repo-b/docs/notes/locked", Dir: true, Reason: "permission denied"}},
		Warnings: []string{"repo-b/docs/notes/locked: permission denied"},
	}, nil)
	if err := Verify(&r, search, VerifyOptions{}); err != nil {
		t.Fatal(err)
	}
	if r.Unsettled[0].Evidence.Status != Found {
		t.Errorf("kubernetes は発見のまま: %+v", r.Unsettled[0].Evidence)
	}
	if r.Unsettled[1].Evidence.Status != Unknown {
		t.Errorf("istio は確認不能のはず: %+v", r.Unsettled[1].Evidence)
	}
	if r.ReadNotWritten[0].Evidence.Status != Unknown {
		t.Errorf("webassembly は確認不能のはず: %+v", r.ReadNotWritten[0].Evidence)
	}
	v := r.Verification
	if v == nil || v.Complete || len(v.Gaps) != 1 || v.Gaps[0] != (Gap{Rel: "repo-b/docs/notes/locked", Dir: true, Reason: "permission denied"}) || len(v.Warnings) != 1 {
		t.Errorf("照合の要約: %+v", v)
	}
	s := string(r.Marshal())
	for _, want := range []string{"確認できなかった範囲 1 件", "- repo-b/docs/notes/locked/ — permission denied", "- istio — セッション 3 本／確認不能（読めなかった範囲がある）"} {
		if !strings.Contains(s, want) {
			t.Errorf("出力に %q が無い:\n%s", want, s)
		}
	}
}

// 照合する語が無ければ本文を読まない(検索の口を呼ばない)。要約は「実行した・語 0」。
func TestVerify_語が無ければ検索しない(t *testing.T) {
	r := Report{Today: "2026-09-05", Sources: map[string]int{}, Stumbles: []Item{{Word: "ingress", Corrections: 2}}}
	search, calls := fakeSearch(textsearch.Result{}, nil)
	if err := Verify(&r, search, VerifyOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Errorf("語が無いのに検索した: %+v", *calls)
	}
	if v := r.Verification; v == nil || !v.Done || v.Words != 0 {
		t.Errorf("照合の要約: %+v", v)
	}
}

// 検索そのものが失敗したら(root が無い等)エラーを返し、候補は照合前のまま(Evidence は付けない)。
func TestVerify_検索の失敗はそのまま返す(t *testing.T) {
	r := candidates()
	search, _ := fakeSearch(textsearch.Result{}, errors.New("root が空"))
	err := Verify(&r, search, VerifyOptions{})
	if err == nil || !strings.Contains(err.Error(), "root が空") {
		t.Fatalf("err=%v", err)
	}
	if r.Unsettled[0].Evidence != nil || r.Verification != nil {
		t.Errorf("失敗時に照合結果を付けた: %+v %+v", r.Unsettled[0].Evidence, r.Verification)
	}
	// 照合前の出力は「本文照合: なし」と断り、項目には本文の欄を付けない
	s := string(r.Marshal())
	if !strings.Contains(s, "本文照合: なし") || strings.Contains(s, "／本文") {
		t.Errorf("照合前の出力が違う:\n%s", s)
	}
}

// 出力には出典の位置(パスと行)だけを載せ、本文の行は Markdown にも JSON にも載せない。
func TestVerify_出力に本文を載せない(t *testing.T) {
	r := candidates()
	search, _ := fakeSearch(textsearch.Result{Files: 2, Hits: []textsearch.Hit{
		hit("repo-a/docs/notes/k8s.md", 12, "kubernetes"),
		hit("repo-a/docs/notes/k8s.md", 30, "kubernetes"),
		hit("repo-a/docs/notes/wasm.md", 3, "webassembly"),
	}}, nil)
	if err := Verify(&r, search, VerifyOptions{MaxLocations: 1}); err != nil {
		t.Fatal(err)
	}
	md := string(r.Marshal())
	js, err := r.JSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{md, string(js)} {
		if strings.Contains(out, "SECRET-BODY") {
			t.Errorf("本文の転記が出力に載っている:\n%s", out)
		}
	}
	for _, want := range []string{
		"本文照合: 2 ファイルを読んだ・確認できなかった範囲なし",
		"## 触れているが索引に無い（2）",
		"- kubernetes — セッション 4 本／本文で発見（2 行・1 ファイル）: repo-a/docs/notes/k8s.md:12 ほか",
		"- istio — セッション 3 本／本文でも未発見（走査した範囲は全部読めた）",
		"## 残した記事にあるが索引に無い（1）",
		"- webassembly — keep 2 件／本文で発見（1 行・1 ファイル）: repo-a/docs/notes/wasm.md:3\n",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("出力に %q が無い:\n%s", want, md)
		}
	}
	for _, want := range []string{`"evidence": {`, `"status": "found"`, `"status": "absent"`, `"path": "repo-a/docs/notes/k8s.md"`, `"line": 12`, `"verification": {`, `"complete": true`} {
		if !strings.Contains(string(js), want) {
			t.Errorf("JSON に %q が無い:\n%s", want, js)
		}
	}
}

// 実ファイルで照合する。位置が元ファイルの行と一致し、同じ材料からは同じバイト列が出る。
// 索引の要旨(先頭行)に無く本文の後半にだけある語を「記録なし」と断定しない(受け入れ条件)。
func TestVerify_実ファイルの位置と決定性(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("repo-a/docs/notes/cluster.md", "# クラスタの運用\n\n結論: 手順を残す\n\n前置き。\n\nKubernetes の Ingress は後半に書く。\n")
	write("repo-b/docs/decisions.md", "# 決定\n\n## kubernetes を使う\n\n理由: 略\n")
	run := func() (Report, []byte) {
		r := candidates()
		if err := Verify(&r, BodySearcher(scan.Config{Root: root}), VerifyOptions{}); err != nil {
			t.Fatal(err)
		}
		return r, r.Marshal()
	}
	r, a := run()
	_, b := run()
	if !bytes.Equal(a, b) {
		t.Fatalf("同じ材料でバイト列が違う:\n%s\n---\n%s", a, b)
	}
	k := r.Unsettled[0].Evidence
	if k == nil || k.Status != Found || k.Lines != 2 || k.Files != 2 ||
		k.Locations[0] != (Location{Path: "repo-a/docs/notes/cluster.md", Line: 7}) || k.Locations[1] != (Location{Path: "repo-b/docs/decisions.md", Line: 3}) {
		t.Errorf("kubernetes の位置が元ファイルと違う: %+v", k)
	}
	if i := r.Unsettled[1].Evidence; i == nil || i.Status != Absent {
		t.Errorf("istio は未発見のはず: %+v", i)
	}
	if v := r.Verification; v == nil || v.Files != 2 || !v.Complete {
		t.Errorf("照合の要約: %+v", v)
	}
}

// Candidates は節をまたいで候補を 1 件ずつ返す(後続の「候補への回答」が識別に使う)。
func TestCandidates_節と語で識別する(t *testing.T) {
	r := candidates()
	cs := r.Candidates()
	var got []string
	for _, c := range cs {
		got = append(got, string(c.Section)+":"+c.Item.Word)
	}
	if want := "unsettled:kubernetes unsettled:istio stumbles:ingress read_not_written:webassembly"; strings.Join(got, " ") != want {
		t.Errorf("候補の並び: %q", strings.Join(got, " "))
	}
	// Item は Report の中の項目を指す(書き込みが Report に反映される)
	cs[0].Item.Evidence = &Evidence{Status: Absent}
	if r.Unsettled[0].Evidence == nil {
		t.Error("Candidates の Item が Report の項目を指していない")
	}
}
