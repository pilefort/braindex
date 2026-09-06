package diagnose

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/catalog"
	"github.com/pilefort/braindex/internal/indexdata"
	"github.com/pilefort/braindex/internal/scan"
	"github.com/pilefort/braindex/internal/scan/scantest"
)

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeNote(t *testing.T, p string) {
	t.Helper()
	writeFile(t, p, "# "+strings.TrimSuffix(filepath.Base(p), ".md")+"\n\n記録日: 2026-08-01\n\n本文\n")
}

// makeRoot は root 直下に hub・alpha(ノート 3 件)・beta(docs 無し)を置く。
func makeRoot(t *testing.T) (root, hub string) {
	t.Helper()
	root = t.TempDir()
	hub = filepath.Join(root, "hub")
	writeFile(t, filepath.Join(hub, "braindex.json"), `{"root": ".."}`)
	writeNote(t, filepath.Join(root, "alpha", "docs", "notes", "a.md"))
	writeNote(t, filepath.Join(root, "alpha", "docs", "notes", "common", "c.md"))
	writeFile(t, filepath.Join(root, "alpha", "docs", "decisions.md"), "# 決定\n\n## 何かを決めた\n\n記録日: 2026-08-02\n理由: x\n根拠: y\n")
	if err := os.MkdirAll(filepath.Join(root, "beta"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, hub
}

// saveCatalog は cfg で索引を作って hub/index/catalog.md に書き、そのパスを返す。
func saveCatalog(t *testing.T, cfg scan.Config, hub, date string) string {
	t.Helper()
	res, err := catalog.Build(cfg, date)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(hub, "index", "catalog.md")
	writeFile(t, p, string(res.Catalog))
	return p
}

func build(t *testing.T, in Input) Report {
	t.Helper()
	r, err := Build(in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return r
}

// 索引がいまの走査と一致していれば要確認は無く、設定・走査・索引の 3 節が揃う。
func TestBuild_一致していれば問題なし(t *testing.T) {
	root, hub := makeRoot(t)
	cfg := scan.Config{Root: root}
	cat := saveCatalog(t, cfg, hub, "2026-09-01")
	r := build(t, Input{ConfigFile: filepath.Join(hub, "braindex.json"), Cfg: cfg, CatalogPath: cat, Date: "2026-09-06"})
	if len(r.Problems) != 0 {
		t.Fatalf("問題なしのはず: %q", r.Problems)
	}
	if r.Scan.Entries != 3 || len(r.Scan.Gaps) != 0 || len(r.Scan.Warnings) != 0 {
		t.Errorf("走査: %+v", r.Scan)
	}
	if r.Saved.Status != "ok" || r.Saved.Generated != "2026-09-01" || r.Saved.Entries != 3 || r.Saved.Coverage != "complete" || r.Saved.Diff == nil || r.Saved.Diff.Count() != 0 {
		t.Errorf("保存済み: %+v", r.Saved)
	}
	if !r.Config.NotesDirsDefault || strings.Join(r.Config.NotesDirs, ",") != "docs/notes" || !strings.HasSuffix(r.Config.File, "/hub/braindex.json") {
		t.Errorf("設定: %+v", r.Config)
	}
	// リポは root 直下の全部(ノートの無い beta・hub も)。名前昇順
	var names []string
	for _, ri := range r.Scan.Repos {
		names = append(names, fmt.Sprintf("%s=%d", ri.Name, ri.Entries))
	}
	if got := strings.Join(names, " "); got != "alpha=3 beta=0 hub=0" {
		t.Errorf("リポ別: %s", got)
	}
	alpha := r.Scan.Repos[0]
	if got := fmt.Sprint(alpha.Kinds); got != "[{decisions 1} {notes 1} {notes/common 1}]" {
		t.Errorf("種別: %s", got)
	}
	if got := fmt.Sprint(alpha.Places); got != "[{docs/decisions.md ok 1} {docs/notes ok 2}]" {
		t.Errorf("置き場: %s", got)
	}
	if got := fmt.Sprint(r.Scan.Repos[1].Places); got != "[{docs/decisions.md missing 0} {docs/notes missing 0}]" {
		t.Errorf("beta の置き場: %s", got)
	}
	text := string(Render(r))
	for _, want := range []string{
		"## まとめ\n- 問題なし",
		"- 索引に載る: 3 件（root 直下 3 リポのうち 1 リポ）",
		"  - alpha: 3 件（decisions 1・notes 1・notes/common 1）\n",
		"  - beta: 0 件 — docs/decisions.md 無し・docs/notes 無し\n",
		"- 状態: 生成 2026-09-01・3 件",
		"- 走査の記録: 読めなかった範囲なし",
		"- いまの走査との差: なし",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("テキストに %q が無い:\n%s", want, text)
		}
	}
}

// 同じ材料からは同じテキスト・同じ JSON になる(規則ベース)。
func TestBuild_同じ材料なら同じ出力(t *testing.T) {
	root, hub := makeRoot(t)
	cfg := scan.Config{Root: root}
	cat := saveCatalog(t, cfg, hub, "2026-09-01")
	in := Input{Cfg: cfg, CatalogPath: cat, Date: "2026-09-06", Path: "alpha/docs/notes/a.md"}
	r1, r2 := build(t, in), build(t, in)
	if !bytes.Equal(Render(r1), Render(r2)) {
		t.Errorf("テキストが揺れる")
	}
	j1, _ := JSON(r1)
	j2, _ := JSON(r2)
	if !bytes.Equal(j1, j2) {
		t.Errorf("JSON が揺れる")
	}
}

// 索引を作った後にノートを足し、別のノートを消すと、未反映と「無い」に分かれて要確認になる。
func TestBuild_索引が古い(t *testing.T) {
	root, hub := makeRoot(t)
	cfg := scan.Config{Root: root}
	cat := saveCatalog(t, cfg, hub, "2026-09-01")
	writeNote(t, filepath.Join(root, "alpha", "docs", "notes", "new.md"))
	if err := os.Remove(filepath.Join(root, "alpha", "docs", "notes", "a.md")); err != nil {
		t.Fatal(err)
	}
	r := build(t, Input{Cfg: cfg, CatalogPath: cat, Date: "2026-09-06"})
	d := r.Saved.Diff
	if d == nil || fmt.Sprint(d.NotIndexed) != "[alpha/docs/notes/new.md]" || fmt.Sprint(d.Gone) != "[alpha/docs/notes/a.md]" || len(d.Unconfirmed)+len(d.OutOfScope) != 0 {
		t.Fatalf("差: %+v", d)
	}
	if len(r.Problems) != 1 || !strings.Contains(r.Problems[0], "未反映 1・確認不能 0・対象外 0・無い 1") {
		t.Errorf("要確認: %q", r.Problems)
	}
	text := string(Render(r))
	for _, want := range []string{
		"- いまの走査との差: 2 件\n  - 未反映 1 件（いま見つかるが索引に無い。再生成で載る）\n    - alpha/docs/notes/new.md\n",
		"  - 無い 1 件（索引にあるが、置き場は確認できてそのパスに無い）\n    - alpha/docs/notes/a.md\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("テキストに %q が無い:\n%s", want, text)
		}
	}
}

// 読めなくなったディレクトリは「読めなかった範囲」に出て、索引にあったその中の行は「無い」でなく「確認不能」になる。
// 読めない extra の起点も同じく確認不能。
func TestBuild_読めなかった範囲は確認不能(t *testing.T) {
	root, hub := makeRoot(t)
	writeNote(t, filepath.Join(root, "alpha", "research", "r.md"))
	cfg := scan.Config{Root: root, Extra: []scan.ExtraRule{{Repo: "alpha", Path: "research", Kind: "research"}}}
	cat := saveCatalog(t, cfg, hub, "2026-09-01")
	scantest.MakeUnreadable(t, filepath.Join(root, "alpha", "docs", "notes", "common"))
	scantest.MakeUnreadable(t, filepath.Join(root, "alpha", "research"))

	r := build(t, Input{Cfg: cfg, CatalogPath: cat, Date: "2026-09-06", Path: "alpha/docs/notes/common/c.md"})
	if got := fmt.Sprint(r.Scan.Gaps); !strings.Contains(got, "alpha/docs/notes/common/ true") || !strings.Contains(got, "alpha/research/ true") {
		t.Fatalf("読めなかった範囲: %s", got)
	}
	if len(r.Scan.Warnings) != 0 {
		t.Errorf("読めなかった範囲は警告に重ねて出さない: %q", r.Scan.Warnings)
	}
	if r.Scan.Repos[0].Gaps != 2 || fmt.Sprint(r.Scan.Repos[0].Places) != "[{docs/decisions.md ok 1} {docs/notes ok 1}]" {
		t.Errorf("alpha: %+v", r.Scan.Repos[0])
	}
	if ex := r.Config.Extra[0]; ex.Status != "unreadable" || ex.Entries != 0 {
		t.Errorf("extra: %+v", ex)
	}
	d := r.Saved.Diff
	if d == nil || len(d.Gone) != 0 || len(d.NotIndexed) != 0 || len(d.OutOfScope) != 0 {
		t.Fatalf("差: %+v", d)
	}
	if got := fmt.Sprint(d.Unconfirmed); got != "[{alpha/docs/notes/common/c.md alpha/docs/notes/common/} {alpha/research/r.md alpha/research/}]" {
		t.Errorf("確認不能: %s", got)
	}
	if p := r.Path; p == nil || !p.Covered || p.Scanned != "gap" || p.Gap != "alpha/docs/notes/common/" || p.Indexed != "yes" || p.Entry != "2026-08-01・c" {
		t.Errorf("パス: %+v", p)
	}
	text := string(Render(r))
	for _, want := range []string{
		"- 読めなかった範囲: 2 件（この範囲のノートは載らない。無いのか読めないのかは分からない）\n  - alpha/docs/notes/common/ — ",
		"alpha/research（直下のみ・種別 research）: 起点を読めなかった（確認不能）",
		"  - 確認不能 2 件（索引にあるが、今回読めなかった範囲の中。有無は分からない）\n    - alpha/docs/notes/common/c.md — alpha/docs/notes/common/\n",
		"## パス alpha/docs/notes/common/c.md\n- いまの設定: 対象（notes_dirs docs/notes）\n- いま走査すると: 読めなかった範囲 alpha/docs/notes/common/ の中（有無は分からない）\n- 保存済みの索引: 載っている（2026-08-01・c）\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("テキストに %q が無い:\n%s", want, text)
		}
	}
	if len(r.Problems) != 2 {
		t.Errorf("要確認は 読めなかった範囲・差 の 2 件: %q", r.Problems)
	}
}

// notes_dirs を変えると、索引にあった行は「無い」でなく「対象外」になり、理由が付く。
func TestBuild_設定を変えた行は対象外(t *testing.T) {
	root, hub := makeRoot(t)
	cat := saveCatalog(t, scan.Config{Root: root}, hub, "2026-09-01")
	cfg := scan.Config{Root: root, NotesDirs: []string{"wiki"}}
	r := build(t, Input{Cfg: cfg, CatalogPath: cat, Date: "2026-09-06", Path: "alpha/docs/notes/a.md"})
	d := r.Saved.Diff
	if d == nil || len(d.OutOfScope) != 2 || len(d.Gone) != 0 || d.OutOfScope[0].Path != "alpha/docs/notes/a.md" || d.OutOfScope[0].Reason != "ノート置き場（wiki）にも docs/decisions.md にも extra にも無い場所" {
		t.Fatalf("差: %+v", d)
	}
	if r.Config.NotesDirsDefault || fmt.Sprint(r.Scan.Repos[0].Places) != "[{docs/decisions.md ok 1} {wiki missing 0}]" {
		t.Errorf("設定・置き場: %+v %+v", r.Config, r.Scan.Repos[0].Places)
	}
	if p := r.Path; p == nil || p.Covered || p.Scanned != "absent" || p.Indexed != "yes" {
		t.Errorf("パス: %+v", p)
	}
	text := string(Render(r))
	if !strings.Contains(text, "- notes_dirs: wiki\n") || !strings.Contains(text, "  - 対象外 2 件（索引にあるが、いまの設定では走査しない場所）\n    - alpha/docs/notes/a.md — ノート置き場（wiki）") || !strings.Contains(text, "- いまの設定: 対象外（ノート置き場（wiki）") {
		t.Errorf("テキスト:\n%s", text)
	}
}

// 走査の記録を持たない索引(この記録を書く前の版)は「記録なし」で、欠けがあったかは分からないと言う。
func TestBuild_記録の無い索引(t *testing.T) {
	root, hub := makeRoot(t)
	cat := filepath.Join(hub, "index", "catalog.md")
	writeFile(t, cat, "# 知識カタログ(braindex 自動生成 — 手で編集しない)\n\n生成: 2026-08-01 / 1 リポジトリ / 1 件\n再生成: hub リポで `braindex` を実行\n\n## alpha\n\n| 日付 | 種別 | タイトル | 要旨 | パス |\n|---|---|---|---|---|\n| 2026-08-01 | notes | a | 本文 | alpha/docs/notes/a.md |\n")
	r := build(t, Input{Cfg: scan.Config{Root: root}, CatalogPath: cat, Date: "2026-09-06"})
	if r.Saved.Status != "ok" || r.Saved.Coverage != "unknown" || r.Saved.Generated != "2026-08-01" || r.Saved.Entries != 1 {
		t.Fatalf("保存済み: %+v", r.Saved)
	}
	if len(r.Problems) != 2 || !strings.Contains(r.Problems[0], "走査の記録が無い") || !strings.Contains(r.Problems[1], "未反映 2") {
		t.Errorf("要確認: %q", r.Problems)
	}
	if !strings.Contains(string(Render(r)), "- 走査の記録: 記録なし（この記録を持たない版で生成。欠けがあったかは分からない）") {
		t.Errorf("テキスト:\n%s", Render(r))
	}
}

// 索引が無い・手で編集されて読めない、はそれぞれ理由つきで要確認になる。診断自体は出す。
func TestBuild_索引が無いか壊れている(t *testing.T) {
	root, hub := makeRoot(t)
	cat := filepath.Join(hub, "index", "catalog.md")
	r := build(t, Input{Cfg: scan.Config{Root: root}, CatalogPath: cat, Date: "2026-09-06"})
	if r.Saved.Status != "missing" || r.Saved.Diff != nil || len(r.Problems) != 1 || !strings.Contains(r.Problems[0], "保存済みの索引が無い") {
		t.Errorf("無し: %+v %q", r.Saved, r.Problems)
	}
	if !strings.Contains(string(Render(r)), "- 状態: 無し（hub で braindex を実行して作る）") {
		t.Errorf("テキスト:\n%s", Render(r))
	}
	writeFile(t, cat, "## alpha\n\n| a | b |\n")
	r = build(t, Input{Cfg: scan.Config{Root: root}, CatalogPath: cat, Date: "2026-09-06"})
	if r.Saved.Status != "invalid" || r.Saved.Error == "" || len(r.Problems) != 1 || !strings.Contains(r.Problems[0], "手で編集された") {
		t.Errorf("壊れている: %+v %q", r.Saved, r.Problems)
	}
}

// 存在しない extra の起点は「設定の誤り」、archive の下は「全件除外」として出す。警告は読めなかった範囲と分けて数える。
func TestBuild_存在しないextra(t *testing.T) {
	root, hub := makeRoot(t)
	writeNote(t, filepath.Join(root, "alpha", "archive", "old.md"))
	cfg := scan.Config{Root: root, Extra: []scan.ExtraRule{
		{Repo: "alpha", Path: "missing", Kind: "x"},
		{Repo: "alpha", Path: "archive", Recursive: true, Kind: "old"},
		{Repo: "alpha", Path: ".", Kind: "top", Exclude: []string{"README.md"}},
	}}
	cat := saveCatalog(t, cfg, hub, "2026-09-01")
	r := build(t, Input{Cfg: cfg, CatalogPath: cat, Date: "2026-09-06"})
	if got := fmt.Sprintf("%s %s %s", r.Config.Extra[0].Status, r.Config.Extra[1].Status, r.Config.Extra[2].Status); got != "missing archive ok" {
		t.Errorf("extra の状態: %s", got)
	}
	if len(r.Scan.Warnings) != 2 || len(r.Scan.Gaps) != 0 {
		t.Errorf("警告 2 件(存在しない・archive)のはず: %q gaps=%v", r.Scan.Warnings, r.Scan.Gaps)
	}
	if len(r.Problems) != 1 || !strings.Contains(r.Problems[0], "走査の警告が 2 件") {
		t.Errorf("要確認: %q", r.Problems)
	}
	text := string(Render(r))
	for _, want := range []string{
		"- extra: 3 件\n  - alpha/missing（直下のみ・種別 x）: 起点が存在しない（設定の誤り。索引には何も載らない）\n",
		"  - alpha/archive（再帰・種別 old）: 起点のパスに archive を含むので全件除外（載せるなら archive の外に置く）\n",
		"  - alpha/.（直下のみ・種別 top・除外 README.md）: 起点あり・この起点の下で索引に載る 0 件\n",
		"- 警告: 2 件\n  - extra alpha/missing: 存在しない\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("テキストに %q が無い:\n%s", want, text)
		}
	}
}

// 設定の値の誤り(exclude のパターン・extra.repo の段数など)や root が読めないときは走査できないが、
// 診断は出す: 読んだ設定と保存済みの索引を示し、走査の節に理由を置く。差は比べない(走査していない)。
// 設定を診断する道具が設定の誤りで何も言わずに止まると、どこが誤りかを別の手段で探すことになる。
func TestBuild_走査できない設定でも設定と索引は出す(t *testing.T) {
	root, hub := makeRoot(t)
	good := scan.Config{Root: root}
	cat := saveCatalog(t, good, hub, "2026-09-01")
	bad := scan.Config{Root: root, Extra: []scan.ExtraRule{{Repo: "alpha", Path: "docs", Kind: "x", Exclude: []string{"[a"}}}}
	r := build(t, Input{ConfigFile: filepath.Join(hub, "braindex.json"), Cfg: bad, CatalogPath: cat, Date: "2026-09-06", Path: "alpha/docs/notes/a.md"})
	if !strings.Contains(r.Scan.Failed, "exclude のパターンが不正") || r.Scan.Entries != 0 || len(r.Scan.Repos) != 0 {
		t.Errorf("走査: %+v", r.Scan)
	}
	if len(r.Config.Extra) != 1 || r.Config.Extra[0].Status != "ok" || r.Config.Root != filepath.ToSlash(root) {
		t.Errorf("設定: %+v", r.Config)
	}
	if r.Saved.Status != "ok" || r.Saved.Entries != 3 || r.Saved.Diff != nil {
		t.Errorf("保存済み(差は比べない): %+v", r.Saved)
	}
	if len(r.Problems) != 1 || !strings.Contains(r.Problems[0], "走査できない: extra alpha/docs: exclude のパターンが不正") {
		t.Errorf("要確認: %q", r.Problems)
	}
	if p := r.Path; p == nil || p.Covered || p.Scanned != "unknown" || p.Indexed != "yes" {
		t.Errorf("パス: %+v", r.Path)
	}
	text := string(Render(r))
	for _, want := range []string{
		"- 要確認 1 件:\n  - 走査できない: extra alpha/docs: exclude のパターンが不正: \"[a\"（索引の生成も同じ理由で止まる。設定か root を直す）\n",
		"- extra: 1 件\n  - alpha/docs（直下のみ・種別 x・除外 [a）: 起点あり・この起点の下で索引に載る 0 件\n",
		"## いま走査すると\n- 走査していない: extra alpha/docs: exclude のパターンが不正: \"[a\"\n",
		"- 状態: 生成 2026-09-01・3 件\n",
		"- いまの走査との差: 比べていない（走査していない）\n",
		"## パス alpha/docs/notes/a.md\n- いまの設定: 判定していない（走査できない設定）\n- いま走査すると: 走査していない\n- 保存済みの索引: 載っている（2026-08-01・a）\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("テキストに %q が無い:\n%s", want, text)
		}
	}
	// 同じ材料なら同じ出力
	if again := build(t, Input{ConfigFile: filepath.Join(hub, "braindex.json"), Cfg: bad, CatalogPath: cat, Date: "2026-09-06", Path: "alpha/docs/notes/a.md"}); !bytes.Equal(Render(again), Render(r)) {
		t.Error("2 回の出力が違う")
	}

	// root が読めない(存在しない)ときも同じ形
	r = build(t, Input{Cfg: scan.Config{Root: filepath.Join(root, "nowhere")}, CatalogPath: cat, Date: "2026-09-06"})
	if !strings.Contains(r.Scan.Failed, "root を読めない") || len(r.Problems) != 1 || !strings.Contains(r.Problems[0], "走査できない: root を読めない") {
		t.Errorf("root が無い: failed=%q problems=%q", r.Scan.Failed, r.Problems)
	}
	if r.Saved.Status != "ok" || r.Saved.Diff != nil {
		t.Errorf("root が無くても保存済みの索引は読む(差は比べない): %+v", r.Saved)
	}
}

// root 直下にリポが無ければ、索引に載るものが無いと言う。
func TestBuild_空のroot(t *testing.T) {
	root := t.TempDir()
	r := build(t, Input{Cfg: scan.Config{Root: root}, CatalogPath: filepath.Join(root, "catalog.md"), Date: "2026-09-06"})
	if r.Scan.Entries != 0 || len(r.Scan.Repos) != 0 || len(r.Problems) != 2 || !strings.Contains(r.Problems[0], "1 件も無い") {
		t.Errorf("%+v %q", r.Scan, r.Problems)
	}
	if !strings.Contains(string(Render(r)), "- リポ別: root 直下にディレクトリが無い") {
		t.Errorf("テキスト:\n%s", Render(r))
	}
}

// Compare は前回にあって今回無い行を 確認不能 → 対象外 → 無い の順で振り分け、今回だけの行を未反映にする。純関数。
func TestCompare_振り分け(t *testing.T) {
	saved := []indexdata.Entry{
		{Path: "a/docs/notes/keep.md"},
		{Path: "a/docs/notes/locked/x.md"},
		{Path: "a/docs/notes/archive/old.md"},
		{Path: "a/docs/notes/gone.md"},
		{Path: "a/docs/notes/gone.md"}, // 重複は 1 回だけ
	}
	current := []indexdata.Entry{{Path: "a/docs/notes/keep.md"}, {Path: "a/docs/notes/new.md"}, {Path: "a/docs/notes/new.md"}}
	cov := catalog.Coverage{Known: true, Gaps: []scan.Gap{{Rel: "a/docs/notes/locked", Dir: true, Reason: "r"}}}
	d := Compare(saved, current, cov, scan.Config{})
	if got := fmt.Sprint(d.NotIndexed, d.Unconfirmed, d.OutOfScope, d.Gone); got != "[a/docs/notes/new.md] [{a/docs/notes/locked/x.md a/docs/notes/locked/}] [{a/docs/notes/archive/old.md パスに archive セグメントを含む（アーカイブは索引から外れる）}] [a/docs/notes/gone.md]" {
		t.Errorf("振り分け: %s", got)
	}
	if d.Count() != 4 {
		t.Errorf("Count=%d", d.Count())
	}
}

// WhyNotCovered は scan.Covers が false のパスに理由を付ける。Covers が true のパスには使わない。
func TestWhyNotCovered(t *testing.T) {
	cfg := scan.Config{Extra: []scan.ExtraRule{{Repo: "ext", Path: "research", Kind: "research", Exclude: []string{"*.draft.md"}}}}
	for rel, want := range map[string]string{
		"":                                "パスが空",
		"a/docs/notes/x.txt":              "拡張子が .md でない",
		"a/docs/notes/archive/x.md":       "パスに archive セグメントを含む（アーカイブは索引から外れる）",
		"a.md":                            "リポ名だけで、リポ内のパスが無い",
		".hidden/docs/notes/x.md":         "ドットで始まるリポは見ない",
		"a/docs/notes/../x.md":            "パスの形が不正（空のセグメント・. ・..）",
		"ext/research/deep/x.md":          "extra ext/research の範囲だが、exclude に当たるか、直下のみの指定でサブディレクトリにある",
		"ext/research/x.draft.md":         "extra ext/research の範囲だが、exclude に当たるか、直下のみの指定でサブディレクトリにある",
		"a/wiki/x.md":                     "ノート置き場（docs/notes）にも docs/decisions.md にも extra にも無い場所",
		"other/research/x.md":             "ノート置き場（docs/notes）にも docs/decisions.md にも extra にも無い場所",
		"/a/docs/notes/x.txt/":            "拡張子が .md でない",
		"ext/docs/notes/x.md/../../y.txt": "拡張子が .md でない",
	} {
		if scan.Covers(cfg, rel) {
			t.Errorf("%q は Covers が true なので理由の対象外", rel)
			continue
		}
		if got := WhyNotCovered(cfg, rel); got != want {
			t.Errorf("WhyNotCovered(%q)=%q want %q", rel, got, want)
		}
	}
}

// WhichRule は対象のパスに当たった規則を、Covers と同じ順(decisions → notes_dirs → extra)で言う。
func TestWhichRule(t *testing.T) {
	cfg := scan.Config{NotesDirs: []string{"docs/notes", "wiki"}, Extra: []scan.ExtraRule{
		{Repo: "ext", Path: "research", Recursive: true, Kind: "research"},
		{Repo: "ext", Path: ".", Kind: "top"},
	}}
	nd := effectiveNotesDirs(cfg)
	for rel, want := range map[string]string{
		"a/docs/decisions.md":    "docs/decisions.md（決定記録）",
		"a/docs/notes/x.md":      "notes_dirs docs/notes",
		"a/wiki/deep/x.md":       "notes_dirs wiki",
		"ext/research/deep/x.md": "extra ext/research",
		"ext/top.md":             "extra ext/.",
		"ext/docs/notes/x.md":    "notes_dirs docs/notes",
	} {
		if !scan.Covers(cfg, rel) {
			t.Errorf("%q は Covers が true のはず", rel)
			continue
		}
		if got := WhichRule(cfg, nd, rel); got != want {
			t.Errorf("WhichRule(%q)=%q want %q", rel, got, want)
		}
	}
	if got := WhichRule(scan.Config{NotesDirs: []string{"."}}, []string{"."}, "a/anything/x.md"); got != "notes_dirs ." {
		t.Errorf("notes_dirs=[.]: %q", got)
	}
}

// 索引の先頭「生成:」行の日付。BOM と CRLF は正規化する。無ければ空。
func TestGeneratedDate(t *testing.T) {
	if got := generatedDate([]byte("\xEF\xBB\xBF# 知識カタログ\r\n\r\n生成: 2026-09-01 / 2 リポジトリ / 5 件\r\n## a\r\n")); got != "2026-09-01" {
		t.Errorf("got %q", got)
	}
	if got := generatedDate([]byte("# x\n\n## a\n生成: 2026-09-01\n")); got != "" {
		t.Errorf("見出しより後の行は見ない: %q", got)
	}
}
