package textsearch

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/scan"
	"github.com/pilefort/braindex/internal/scan/scantest"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// makeRoot は 2 リポを持つ root を作る。
//
//	repo-a/docs/notes/long.md   … タイトル・要旨に無い語「決定性」が 40 行目にだけある(本文後半だけの一致)
//	repo-a/docs/notes/same.md   … 「同名ノート」の一方
//	repo-b/docs/notes/same.md   … 「同名ノート」の他方(パスで区別される)
//	repo-b/docs/decisions.md    … 同じ語が 2 行にある(複数一致)。ラテン文字の大小の揺れも入れる
//	repo-b/docs/notes/archive/old.md … archive の下(除外対象)
//	repo-b/docs/notes/memo.txt  … .md でない(除外対象)
func makeRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "root")
	var b strings.Builder
	b.WriteString("# 索引の設計\n\n結論: 索引はパスを引くためのもの。\n")
	for i := 0; i < 36; i++ {
		b.WriteString("本文の行。\n")
	}
	b.WriteString("同じ入力からは常にバイト一致する。これを決定性と呼ぶ。\n")
	write(t, filepath.Join(root, "repo-a", "docs", "notes", "long.md"), b.String())
	write(t, filepath.Join(root, "repo-a", "docs", "notes", "same.md"), "# 甲\n\nベクトル検索は入れない。\n")
	write(t, filepath.Join(root, "repo-b", "docs", "notes", "same.md"), "# 乙\n\n\n\tベクトル DB も入れない。\n")
	write(t, filepath.Join(root, "repo-b", "docs", "decisions.md"), "# 決定\n\n## Go の版\n\n理由: go 1.26 を使う。\nGoogle の例は見ない。\n")
	write(t, filepath.Join(root, "repo-b", "docs", "notes", "archive", "old.md"), "# 古い\n\n決定性もベクトルも書いてある。\n")
	write(t, filepath.Join(root, "repo-b", "docs", "notes", "memo.txt"), "決定性 ベクトル\n")
	return root
}

func paths(hits []Hit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Path)
	}
	return out
}

// 索引のタイトル・要旨には無く本文の後半にだけある語が、ファイルと行番号つきで当たる。
func TestRun_本文後半だけの一致(t *testing.T) {
	root := makeRoot(t)
	res, err := Run(scan.Config{Root: root}, Query{Terms: []string{"決定性"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 {
		t.Fatalf("1 件のはず: %+v", res.Hits)
	}
	h := res.Hits[0]
	if h.Path != "repo-a/docs/notes/long.md" || h.Line != 40 || h.Col != 21 || h.Repo != "repo-a" || h.Kind != "notes" {
		t.Errorf("出典位置が違う: %+v", h)
	}
	if h.Text != "同じ入力からは常にバイト一致する。これを決定性と呼ぶ。" || !reflect.DeepEqual(h.Terms, []string{"決定性"}) {
		t.Errorf("行の内容か語が違う: %+v", h)
	}
	if !res.Complete() || res.Files != 4 || res.Total != 1 || len(res.Warnings) != 0 {
		t.Errorf("読めたファイル 4・確認できなかった範囲なしのはず: %+v", res)
	}
}

// 同名のノートは別々のパスで両方当たる。パス昇順に並ぶ。
func TestRun_同名ノート(t *testing.T) {
	root := makeRoot(t)
	res, err := Run(scan.Config{Root: root}, Query{Terms: []string{"ベクトル"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"repo-a/docs/notes/same.md", "repo-b/docs/notes/same.md"}
	if !reflect.DeepEqual(paths(res.Hits), want) {
		t.Errorf("パス: got=%v want=%v", paths(res.Hits), want)
	}
	// タブで始まる行は前後の空白を落として返し、Col は元の行の位置(タブの次=2 文字目)
	if h := res.Hits[1]; h.Line != 4 || h.Col != 2 || h.Text != "ベクトル DB も入れない。" {
		t.Errorf("空白の扱いが違う: %+v", h)
	}
}

// 同じファイルの複数行に当たれば行ごとに 1 件、行番号の昇順。大小は既定で無視し、Terms には元の語を返す。
func TestRun_複数一致と大小無視(t *testing.T) {
	root := makeRoot(t)
	res, err := Run(scan.Config{Root: root}, Query{Terms: []string{"GO"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 3 {
		t.Fatalf("Go の版・go 1.26・Google の 3 行のはず: %+v", res.Hits)
	}
	if res.Hits[0].Line != 3 || res.Hits[1].Line != 5 || res.Hits[2].Line != 6 {
		t.Errorf("行番号が昇順でない: %+v", res.Hits)
	}
	if res.Hits[0].Col != 4 || res.Hits[0].Terms[0] != "GO" || res.Hits[0].Kind != "decisions" {
		t.Errorf("位置・語・種別が違う: %+v", res.Hits[0])
	}
	// 大小を区別すると当たらない
	res, _ = Run(scan.Config{Root: root}, Query{Terms: []string{"GO"}, MatchCase: true})
	if len(res.Hits) != 0 {
		t.Errorf("MatchCase で当たるべきでない: %+v", res.Hits)
	}
}

// WholeWord はラテン文字の語にだけ境界を引く(go は google に当たらない)。かな・漢字は連続の中でも当たる。
func TestRun_語の境界(t *testing.T) {
	root := makeRoot(t)
	res, err := Run(scan.Config{Root: root}, Query{Terms: []string{"go"}, WholeWord: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 2 || res.Hits[0].Line != 3 || res.Hits[1].Line != 5 {
		t.Errorf("Go の版・go 1.26 の 2 行のはず(Google は除く): %+v", res.Hits)
	}
	write(t, filepath.Join(root, "repo-a", "docs", "notes", "compound.md"), "# 複合\n\n全索引化とベクトルデータベース。\n")
	res, _ = Run(scan.Config{Root: root}, Query{Terms: []string{"索引", "ベクトル"}, WholeWord: true})
	if len(res.Hits) != 1 || res.Hits[0].Path != "repo-a/docs/notes/compound.md" {
		t.Errorf("漢字・カタカナは複合語の中でも当たるはず: %+v", res.Hits)
	}
	// 境界に阻まれた最初の出現を飛ばして、後ろの出現に当たる
	write(t, filepath.Join(root, "repo-a", "docs", "notes", "later.md"), "# 後ろ\n\nago では無く go だ。\n")
	res, _ = Run(scan.Config{Root: root}, Query{Terms: []string{"go"}, WholeWord: true, Repo: "repo-a"})
	if len(res.Hits) != 1 || res.Hits[0].Col != 10 {
		t.Errorf("2 つ目の go(10 文字目)に当たるはず: %+v", res.Hits)
	}
}

// 既定は全部の語を含む行だけ(AND)。Any なら 1 語でも当たり、Terms に含まれていた語だけが載る。
func TestRun_複数の語(t *testing.T) {
	root := makeRoot(t)
	res, err := Run(scan.Config{Root: root}, Query{Terms: []string{"入力", "決定性"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Line != 40 || res.Hits[0].Col != 3 {
		t.Errorf("両方を含む 40 行目だけ・Col は先に出る「入力」の位置: %+v", res.Hits)
	}
	res, _ = Run(scan.Config{Root: root}, Query{Terms: []string{"ベクトル", "決定性"}, Any: true})
	if len(res.Hits) != 3 {
		t.Fatalf("long.md の 40 行目と same.md ×2 の 3 件のはず: %+v", res.Hits)
	}
	if !reflect.DeepEqual(res.Hits[0].Terms, []string{"決定性"}) || !reflect.DeepEqual(res.Hits[1].Terms, []string{"ベクトル"}) {
		t.Errorf("行ごとに含まれていた語だけ: %+v", res.Hits)
	}
	by := res.ByTerm()
	if len(by["ベクトル"]) != 2 || len(by["決定性"]) != 1 || len(by) != 2 {
		t.Errorf("ByTerm: %+v", by)
	}
}

// archive の下・.md でないファイル・設定が見に行かない場所は読まない(索引と同じ走査規則)。
func TestRun_除外対象(t *testing.T) {
	root := makeRoot(t)
	// wiki/ に語があっても、notes_dirs に無ければ読まない
	write(t, filepath.Join(root, "repo-a", "wiki", "w.md"), "決定性\n")
	res, err := Run(scan.Config{Root: root}, Query{Terms: []string{"決定性"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(res.Hits); !reflect.DeepEqual(got, []string{"repo-a/docs/notes/long.md"}) {
		t.Errorf("archive・.txt・wiki は読まないはず: %v", got)
	}
	// notes_dirs に足せば読む(対象外は設定の問題であって、本文が無いのではない)
	res, _ = Run(scan.Config{Root: root, NotesDirs: []string{"docs/notes", "wiki"}}, Query{Terms: []string{"決定性"}})
	if got := paths(res.Hits); !reflect.DeepEqual(got, []string{"repo-a/docs/notes/long.md", "repo-a/wiki/w.md"}) {
		t.Errorf("wiki を足せば読むはず: %v", got)
	}
}

// 開けないファイルは Gaps に載り(不一致と区別)、読めた分は返す。ディレクトリが列挙できなければ走査の Gaps を引き継ぐ。
func TestRun_読取失敗(t *testing.T) {
	root := makeRoot(t)
	bad := filepath.Join(root, "repo-a", "docs", "notes", "long.md")
	scantest.MakeUnreadable(t, bad)
	res, err := Run(scan.Config{Root: root}, Query{Terms: []string{"決定性"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 || res.Complete() || res.Files != 3 {
		t.Errorf("一致なし・確認できなかった範囲あり・読めたのは 3 件のはず: hits=%+v gaps=%+v files=%d", res.Hits, res.Gaps, res.Files)
	}
	if len(res.Gaps) != 1 || res.Gaps[0].Rel != "repo-a/docs/notes/long.md" || res.Gaps[0].Dir || res.Gaps[0].Reason == "" {
		t.Errorf("Gaps に {repo-a/docs/notes/long.md, ファイル, 理由} の 1 件を期待: %+v", res.Gaps)
	}
	if len(res.Warnings) != 1 || !strings.HasPrefix(res.Warnings[0], "repo-a/docs/notes/long.md: ") {
		t.Errorf("警告 1 件「<rel>: <理由>」を期待: %q", res.Warnings)
	}
}

func TestRun_列挙できないディレクトリ(t *testing.T) {
	root := makeRoot(t)
	locked := filepath.Join(root, "repo-b", "docs", "notes", "locked")
	write(t, filepath.Join(locked, "x.md"), "決定性\n")
	scantest.MakeUnreadable(t, locked)
	res, err := Run(scan.Config{Root: root}, Query{Terms: []string{"決定性"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Gaps) != 1 || res.Gaps[0].Rel != "repo-b/docs/notes/locked" || !res.Gaps[0].Dir {
		t.Errorf("走査の Gaps(ディレクトリ)を引き継ぐはず: %+v", res.Gaps)
	}
	// リポで絞ると、他のリポの確認できなかった範囲は載らない
	res, _ = Run(scan.Config{Root: root}, Query{Terms: []string{"決定性"}, Repo: "repo-a"})
	if !res.Complete() || len(res.Hits) != 1 {
		t.Errorf("repo-a に絞れば repo-b の範囲は関係ない: gaps=%+v hits=%d", res.Gaps, len(res.Hits))
	}
}

// 結果の順は走査順でなくパス→行の順。同じ材料からは同じ結果になり、元のノートは変わらない。
func TestRun_決定的な順(t *testing.T) {
	root := makeRoot(t)
	// extra は走査で最後に拾われるが、パスの順では docs/notes より前に来る
	write(t, filepath.Join(root, "repo-a", "aaa", "first.md"), "決定性\n")
	cfg := scan.Config{Root: root, Extra: []scan.ExtraRule{{Repo: "repo-a", Path: "aaa", Kind: "aaa"}}}
	before, _ := os.ReadFile(filepath.Join(root, "repo-a", "docs", "notes", "long.md"))
	a, err := Run(cfg, Query{Terms: []string{"決定性"}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Run(cfg, Query{Terms: []string{"決定性"}})
	if !reflect.DeepEqual(a, b) {
		t.Errorf("2 回の結果が違う:\n%+v\n%+v", a, b)
	}
	want := []string{"repo-a/aaa/first.md", "repo-a/docs/notes/long.md"}
	if !reflect.DeepEqual(paths(a.Hits), want) {
		t.Errorf("パス昇順でない: %v", paths(a.Hits))
	}
	after, _ := os.ReadFile(filepath.Join(root, "repo-a", "docs", "notes", "long.md"))
	if string(before) != string(after) {
		t.Errorf("検索でノートが変わった")
	}
}

// 1 行が数 MB でも落ちず、Text は一致箇所の周辺だけ(切った側に …)。BOM は 1 文字目に数えず、CRLF の \r は Text に残らない。
func TestRun_長い行とBOMとCRLF(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	long := strings.Repeat("あ", 3<<20) + "決定性" + strings.Repeat("い", 300)
	write(t, filepath.Join(root, "r", "docs", "notes", "big.md"), "\xEF\xBB\xBF決定性の話\r\n"+long+"\r\n末尾の決定性")
	res, err := Run(scan.Config{Root: root}, Query{Terms: []string{"決定性"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 3 {
		t.Fatalf("3 行とも当たるはず: %d", len(res.Hits))
	}
	if h := res.Hits[0]; h.Line != 1 || h.Col != 1 || h.Text != "決定性の話" {
		t.Errorf("BOM と CR を落とした 1 行目: %+v", h)
	}
	h := res.Hits[1]
	if h.Line != 2 || h.Col != 3<<20+1 || !strings.HasPrefix(h.Text, "…") || !strings.HasSuffix(h.Text, "…") || !strings.Contains(h.Text, "決定性") {
		t.Errorf("長い行は周辺だけ: line=%d col=%d len=%d head=%q", h.Line, h.Col, len([]rune(h.Text)), string([]rune(h.Text)[:5]))
	}
	if n := len([]rune(h.Text)); n != maxText+2 {
		t.Errorf("抜粋は %d 文字+… ×2 のはず: %d", maxText, n)
	}
	if h := res.Hits[2]; h.Line != 3 || h.Col != 4 || h.Text != "末尾の決定性" {
		t.Errorf("改行で終わらない最終行も数える: %+v", h)
	}
}

// 大小無視で多バイト文字が混ざっても Col は元の行の文字位置。
func TestRun_多バイトと位置(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	write(t, filepath.Join(root, "r", "docs", "notes", "n.md"), "あいう ABC\n")
	res, err := Run(scan.Config{Root: root}, Query{Terms: []string{"abc"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Col != 5 {
		t.Errorf("5 文字目のはず: %+v", res.Hits)
	}
}

// Kind は完全一致か「種別/」で始まるもの。Limit は並べたあと先頭 N 件(Total は全数)。
func TestRun_絞り込みと上限(t *testing.T) {
	root := makeRoot(t)
	write(t, filepath.Join(root, "repo-a", "docs", "notes", "common", "c.md"), "ベクトル\n")
	res, err := Run(scan.Config{Root: root}, Query{Terms: []string{"ベクトル"}, Kind: "notes"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"repo-a/docs/notes/common/c.md", "repo-a/docs/notes/same.md", "repo-b/docs/notes/same.md"}
	if !reflect.DeepEqual(paths(res.Hits), want) {
		t.Errorf("notes と notes/common: %v", paths(res.Hits))
	}
	res, _ = Run(scan.Config{Root: root}, Query{Terms: []string{"ベクトル"}, Kind: "notes/common"})
	if !reflect.DeepEqual(paths(res.Hits), want[:1]) {
		t.Errorf("notes/common だけ: %v", paths(res.Hits))
	}
	res, _ = Run(scan.Config{Root: root}, Query{Terms: []string{"ベクトル"}, Limit: 2})
	if res.Total != 3 || !reflect.DeepEqual(paths(res.Hits), want[:2]) {
		t.Errorf("Total=3・先頭 2 件: total=%d %v", res.Total, paths(res.Hits))
	}
}

// 条件の誤りは error。一致が無ければ Hits は nil でなく空(JSON で null にしない)。
func TestRun_条件の誤りと空(t *testing.T) {
	root := makeRoot(t)
	for _, q := range []Query{{}, {Terms: []string{""}}, {Terms: []string{"a", ""}}, {Terms: []string{"a"}, Limit: -1}} {
		if _, err := Run(scan.Config{Root: root}, q); err == nil {
			t.Errorf("%+v で error のはず", q)
		}
	}
	if _, err := Run(scan.Config{}, Query{Terms: []string{"a"}}); err == nil {
		t.Errorf("root が空なら error のはず")
	}
	res, err := Run(scan.Config{Root: root}, Query{Terms: []string{"無い語"}})
	if err != nil || res.Hits == nil || len(res.Hits) != 0 || res.Total != 0 || !res.Complete() {
		t.Errorf("一致なし・確認できなかった範囲なし: %+v err=%v", res, err)
	}
}
