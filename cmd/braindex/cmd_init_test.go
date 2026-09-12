package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/schedule"
	"github.com/pilefort/braindex/internal/template"
)

// braindex init -add all <dir> は hub の雛形を全部展開し、作成したファイルを stdout に列挙する。
func TestInit_Hub(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hub")
	var so, se bytes.Buffer
	if code := dispatch([]string{"init", "-add", "all", dir}, &so, &se); code != 0 {
		t.Fatalf("exit=%d want 0\nstderr=%s", code, se.String())
	}
	for _, p := range []string{"README.md", "braindex.json", ".gitignore", "news/feeds.example.json", "docs/conventions.md", "work/review/.gitkeep", ".claude/skills/braindex-review/SKILL.md", ".claude/skills/retro/SKILL.md", ".claude/skills/record-lint/SKILL.md", ".claude/skills/contradiction-scan/SKILL.md", ".claude/skills/research-distill/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err != nil {
			t.Errorf("作られていない: %s", p)
		}
	}
	if !strings.Contains(so.String(), "作成: braindex.json") {
		t.Errorf("stdout に作成の記録が無い: %s", so.String())
	}
	// news の置き場: 設定に news 節があり、.gitignore が既読・キャッシュ・ダイジェストを除外し、keep は残す
	cfg, _ := os.ReadFile(filepath.Join(dir, "braindex.json"))
	if !strings.Contains(string(cfg), `"news"`) || !strings.Contains(string(cfg), `"llm": "off"`) {
		t.Errorf("braindex.json に news 節(llm: off)が無い: %s", cfg)
	}
	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	for _, want := range []string{"news/.*.json", "news/digest_*", "news/.ingested/"} {
		if !strings.Contains(string(gi), want) {
			t.Errorf(".gitignore に %s が無い: %s", want, gi)
		}
	}
	if strings.Contains(string(gi), "\nnews/keep") {
		t.Errorf(".gitignore が keep を除外している(keep は蓄積側): %s", gi)
	}

	// 再実行: 何も上書きせず、その旨を出す
	so.Reset()
	se.Reset()
	if code := dispatch([]string{"init", dir}, &so, &se); code != 0 {
		t.Fatalf("2 回目 exit=%d want 0\nstderr=%s", code, se.String())
	}
	if !strings.Contains(so.String(), "保持(既存): README.md") || strings.Contains(so.String(), "作成: ") {
		t.Errorf("2 回目の出力が不正: %s", so.String())
	}
}

// 展開した hub でそのまま braindex が動く(設定の root は ".." で親ディレクトリ)。
func TestInit_ThenGenerate(t *testing.T) {
	parent := t.TempDir()
	hub := filepath.Join(parent, "hub")
	writeFile(t, filepath.Join(parent, "repo-a", "docs", "notes", "a.md"), "# A\n\n結論: a\n記録日: 2026-01-02\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"init", hub}, &so, &se); code != 0 {
		t.Fatalf("init exit=%d\n%s", code, se.String())
	}
	so.Reset()
	se.Reset()
	cfg := filepath.Join(hub, "braindex.json")
	if code := dispatch([]string{"-config", cfg, "-date", "2026-01-03"}, &so, &se); code != 0 {
		t.Fatalf("generate exit=%d\n%s", code, se.String())
	}
	b, err := os.ReadFile(filepath.Join(hub, "index", "catalog.md"))
	if err != nil {
		t.Fatalf("catalog が無い: %v", err)
	}
	if !strings.Contains(string(b), "repo-a/docs/notes/a.md") {
		t.Errorf("hub 自身の設定で親ディレクトリのリポが索引されていない:\n%s", b)
	}
}

// 展開が途中で失敗しても、そこまでに作ったファイルは stdout に列挙する(無言で作らない)。
// docs を通常ファイルにしておくと、docs/ より前のファイル(スキル・README・braindex.json)を作った後で失敗する。
func TestInit_PartialFailureListsCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hub")
	writeFile(t, filepath.Join(dir, "docs"), "x")
	var so, se bytes.Buffer
	if code := dispatch([]string{"init", "-add", "conventions", dir}, &so, &se); code != 1 {
		t.Fatalf("exit=%d want 1\nstderr=%s", code, se.String())
	}
	if !strings.Contains(se.String(), "braindex init:") {
		t.Errorf("stderr にエラーが無い: %s", se.String())
	}
	for _, p := range []string{".gitattributes", "CLAUDE.md", "README.md", "braindex.json"} {
		if !strings.Contains(so.String(), "作成: "+p+"\n") {
			t.Errorf("失敗前に作った %s が stdout に無い:\n%s", p, so.String())
		}
	}
	if strings.Contains(so.String(), "braindex init: 作成") || strings.Contains(so.String(), "次:") {
		t.Errorf("失敗したのに完了の要約や次の案内が出ている:\n%s", so.String())
	}
}

// 引数の誤り: ディレクトリが 2 つ以上・不正なフラグ。文言を出して 1 を返し、何も書かない。-h は使い方を出して 0。
func TestInit_BadArgs(t *testing.T) {
	base := t.TempDir()
	a, b := filepath.Join(base, "a"), filepath.Join(base, "b")
	var so, se bytes.Buffer
	if code := dispatch([]string{"init", a, b}, &so, &se); code != 1 || !strings.Contains(se.String(), "1 つまで") {
		t.Errorf("引数 2 つ: exit=%d stderr=%s", code, se.String())
	}
	for _, d := range []string{a, b} {
		if _, err := os.Stat(d); err == nil {
			t.Errorf("引数の誤りなのに %s が作られた", d)
		}
	}
	se.Reset()
	if code := dispatch([]string{"init", "-nope"}, &so, &se); code != 1 || !strings.Contains(se.String(), "使い方") {
		t.Errorf("不正なフラグ: exit=%d stderr=%s", code, se.String())
	}
	if _, err := os.Stat("braindex.json"); err == nil {
		t.Errorf("不正なフラグなのにカレントディレクトリに展開された")
	}
	se.Reset()
	if code := dispatch([]string{"init", "-h"}, &so, &se); code != 0 || !strings.Contains(se.String(), "使い方") {
		t.Errorf("init -h: exit=%d stderr=%s", code, se.String())
	}
}

// braindex init -repo ../x -add retro のように -repo の直後にパスを書くと、-repo は値を取らない真偽フラグなので
// そのパスがディレクトリの位置引数になり、それより後ろの -add retro はフラグとして認識されず位置引数に混ざる
// (go の flag は最初の非フラグ引数でフラグの解釈を止める)。「ディレクトリは 1 つまで」という的外れなメッセージでなく、
// 何が起きたかと正しい書き方を示す。
func TestInit_RepoFlagFollowedByPathGivesHelpfulError(t *testing.T) {
	var so, se bytes.Buffer
	code := dispatch([]string{"init", "-repo", "../x", "-add", "retro"}, &so, &se)
	if code != 1 {
		t.Fatalf("exit=%d want 1: stderr=%s", code, se.String())
	}
	got := se.String()
	if !strings.Contains(got, "-repo") || !strings.Contains(got, "真偽フラグ") {
		t.Errorf("-repo が値を取らないことの説明が無い: %s", got)
	}
	if !strings.Contains(got, "フラグより前") && !strings.Contains(got, "フラグはすべてディレクトリより前") {
		t.Errorf("正しい書き方(フラグを先に書く)の案内が無い: %s", got)
	}
}

// -repo の後ろに単にディレクトリを 2 つ渡しただけ(フラグを混同していない)なら、
// 「-repo は真偽フラグ」の案内は出さず、従来どおり「ディレクトリは 1 つまで」で止める。
func TestInit_RepoWithTwoPlainDirsKeepsGenericError(t *testing.T) {
	var so, se bytes.Buffer
	code := dispatch([]string{"init", "-repo", "../x", "../y"}, &so, &se)
	if code != 1 {
		t.Fatalf("exit=%d want 1: stderr=%s", code, se.String())
	}
	got := se.String()
	if !strings.Contains(got, "1 つまで") {
		t.Errorf("従来のメッセージが出ていない: %s", got)
	}
	if strings.Contains(got, "真偽フラグ") {
		t.Errorf("フラグの混同案内が誤って出た(フラグを混同していない場合): %s", got)
	}
}

// braindex init -repo <dir> は各プロジェクトのリポ側の骨格だけを置く(hub 用の README や braindex.json は作らない)。
func TestInit_Repo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo-a")
	var so, se bytes.Buffer
	if code := dispatch([]string{"init", "-repo", dir}, &so, &se); code != 0 {
		t.Fatalf("exit=%d want 0\nstderr=%s", code, se.String())
	}
	for _, p := range []string{"docs/decisions.md", "docs/notes/common/.gitkeep", "docs/notes/project/.gitkeep", "work/APPROVALS.md", "work/TODO.md"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err != nil {
			t.Errorf("作られていない: %s", p)
		}
	}
	for _, p := range []string{"README.md", "braindex.json", "CLAUDE.md"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err == nil {
			t.Errorf("repo には作らないはず: %s", p)
		}
	}
	if !strings.Contains(so.String(), "作成: docs/decisions.md") {
		t.Errorf("stdout に作成の記録が無い: %s", so.String())
	}
	// 展開したリポは既定の規約どおりなので、hub から設定なしで索引される
	hub := filepath.Join(filepath.Dir(dir), "hub")
	writeFile(t, filepath.Join(dir, "docs", "notes", "project", "n.md"), "# N\n\n結論: n\n記録日: 2026-01-02\n")
	so.Reset()
	se.Reset()
	if code := dispatch([]string{"init", hub}, &so, &se); code != 0 {
		t.Fatalf("init hub exit=%d\n%s", code, se.String())
	}
	so.Reset()
	se.Reset()
	if code := dispatch([]string{"-config", filepath.Join(hub, "braindex.json"), "-date", "2026-01-03"}, &so, &se); code != 0 {
		t.Fatalf("generate exit=%d\n%s", code, se.String())
	}
	b, _ := os.ReadFile(filepath.Join(hub, "index", "catalog.md"))
	if !strings.Contains(string(b), "repo-a/docs/notes/project/n.md") || !strings.Contains(string(b), "repo-a/docs/decisions.md") {
		t.Errorf("init -repo で作ったリポが索引されていない:\n%s", b)
	}
}

// 展開した hub の braindex.json(retro 節)で、そのまま braindex retro check が動く(設定の検証を通り、
// テンプレの窓 14 日で testdata の 3 発話・訂正 1 を数え、閾値超えを 3 で返す)。閾値の値は表示で固定しない(設定の既定は較正で変わる)。
func TestInit_ThenRetroCheck(t *testing.T) {
	fixUTC(t)
	hub := filepath.Join(t.TempDir(), "hub")
	var so, se bytes.Buffer
	if code := dispatch([]string{"init", "-add", "retro", hub}, &so, &se); code != 0 {
		t.Fatalf("init exit=%d\n%s", code, se.String())
	}
	// -all-projects: fixture のセッションの cwd は hub の root の外にあるので、絞りを外して数える
	code, out, errs := execRetroCheck(t, "-config", filepath.Join(hub, "braindex.json"), "-sessions", retroTestdata, "-date", "2026-09-01", "-all-projects")
	if code != 3 {
		t.Fatalf("exit=%d want 3\nstdout=%s\nstderr=%s", code, out, errs)
	}
	if !strings.Contains(out, "直近 14 日の訂正率 33.3%(発話 3・訂正 1)") || !strings.Contains(out, "→ 閾値を超えた。") {
		t.Errorf("stdout が想定と違う: %q", out)
	}
}

// 展開した hub の braindex.json(schedule 節)で、そのまま braindex schedule print が動く
// (設定の検証を通り、review・retro を足してあればテンプレの 2 本が登録コマンドになる)。OS へは登録しない。
func TestInit_ThenSchedulePrint(t *testing.T) {
	hub := filepath.Join(t.TempDir(), "hub")
	var so, se bytes.Buffer
	if code := dispatch([]string{"init", "-add", "review,retro,schedule", hub}, &so, &se); code != 0 {
		t.Fatalf("init exit=%d\n%s", code, se.String())
	}
	if !strings.Contains(so.String(), "braindex schedule install") {
		t.Errorf("init の案内に定期実行の 1 行が無い:\n%s", so.String())
	}
	code, out, errs := execSchedule(t, "windows", &fakeRunner{}, "print", "-config", filepath.Join(hub, "braindex.json"))
	if code != 0 {
		t.Fatalf("exit=%d\n%s", code, errs)
	}
	for _, want := range []string{schedule.TaskName(hub, "review"), schedule.TaskName(hub, "retro"), "09:00", "09:05", "retro check"} {
		if !strings.Contains(out, want) {
			t.Errorf("print の出力に %q が無い:\n%s", want, out)
		}
	}
}

// 既定の braindex init は「利用者の置き場を変えない」機能を配る: 索引の設定(README・CLAUDE.md・.gitattributes・
// braindex.json)と retro・news・schedule。規約(docs/・work/)と review は配らず、案内に -add conventions の入口を出す。
// (入口の設計 2026-09-05。同日の「段 0 だけ」を上書き)
func TestInit_Default(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hub")
	var so, se bytes.Buffer
	if code := dispatch([]string{"init", dir}, &so, &se); code != 0 {
		t.Fatalf("exit=%d want 0\nstderr=%s", code, se.String())
	}
	for _, p := range []string{"README.md", "CLAUDE.md", ".gitattributes", "braindex.json", ".claude/skills/retro/SKILL.md", "news/feeds.example.json", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err != nil {
			t.Errorf("作られていない: %s", p)
		}
	}
	for _, p := range []string{"docs", "work", ".claude/skills/record-lint", ".claude/skills/braindex-review"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err == nil {
			t.Errorf("既定なのに %s がある(規約と review は -add で足す)", p)
		}
	}
	cfg := readFile(t, filepath.Join(dir, "braindex.json"))
	for _, want := range []string{`"retro"`, `"news"`, `"schedule"`, `"name": "retro"`, `"name": "news"`} {
		if !strings.Contains(cfg, want) {
			t.Errorf("既定の braindex.json に %s が無い:\n%s", want, cfg)
		}
	}
	for _, bad := range []string{`"review"`, `"approvals"`} {
		if strings.Contains(cfg, bad) {
			t.Errorf("既定の braindex.json に %s がある:\n%s", bad, cfg)
		}
	}
	out := so.String()
	for _, want := range []string{"作成: braindex.json", "作成: .claude/skills/retro/SKILL.md", "braindex init: 作成 7・追記 0・保持 0", "次: braindex.json の root", "braindex init -list", "braindex init -add conventions", "retro check", "feeds.json", "schedule print"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout に %q が無い:\n%s", want, out)
		}
	}
	for _, bad := range []string{"`braindex review`", "braindex lint"} {
		if strings.Contains(out, bad) {
			t.Errorf("既定の案内に %q がある(足していない機能の案内):\n%s", bad, out)
		}
	}
	// 既定のままで索引が作れる
	writeFile(t, filepath.Join(filepath.Dir(dir), "repo-a", "docs", "notes", "a.md"), "# A\n\n結論: a\n記録日: 2026-01-02\n")
	so.Reset()
	se.Reset()
	if code := dispatch([]string{"-config", filepath.Join(dir, "braindex.json"), "-date", "2026-01-03"}, &so, &se); code != 0 {
		t.Fatalf("generate exit=%d\n%s", code, se.String())
	}
	if b := readFile(t, filepath.Join(dir, "index", "catalog.md")); !strings.Contains(b, "repo-a/docs/notes/a.md") {
		t.Errorf("既定の設定で索引が作られていない:\n%s", b)
	}
}

// -add core だけなら段 0(索引の設定だけ)になる。
func TestInit_AddCoreOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hub")
	var so, se bytes.Buffer
	if code := dispatch([]string{"init", "-add", "core", dir}, &so, &se); code != 0 {
		t.Fatalf("exit=%d want 0\nstderr=%s", code, se.String())
	}
	if !strings.Contains(so.String(), "braindex init: 作成 4・追記 0・保持 0") {
		t.Errorf("段 0 は 4 ファイル:\n%s", so.String())
	}
	cfg := readFile(t, filepath.Join(dir, "braindex.json"))
	for _, bad := range []string{`"review"`, `"retro"`, `"news"`, `"schedule"`, `"approvals"`} {
		if strings.Contains(cfg, bad) {
			t.Errorf("段 0 の braindex.json に %s がある:\n%s", bad, cfg)
		}
	}
}

// 既定の hub に review を足す: conventions を連れてきてその旨を出し、braindex.json は「追記」(review 節と job)になる。
// 全部足し終えると -add all で一括展開した hub とファイルが一致する。同じ機能を 2 回足しても何も変わらない。
func TestInit_AddStepwise(t *testing.T) {
	step := filepath.Join(t.TempDir(), "step")
	once := filepath.Join(t.TempDir(), "once")
	var so, se bytes.Buffer
	if code := dispatch([]string{"init", step}, &so, &se); code != 0 {
		t.Fatalf("init exit=%d\n%s", code, se.String())
	}
	so.Reset()
	if code := dispatch([]string{"init", "-add", "review", step}, &so, &se); code != 0 {
		t.Fatalf("-add review exit=%d\n%s", code, se.String())
	}
	out := so.String()
	for _, want := range []string{"依存として含めた機能: conventions", "追記(無い節・行を足した): braindex.json", "作成: docs/conventions.md", "作成: .claude/skills/braindex-review/SKILL.md", "保持(既存): README.md", "`braindex review`", "`braindex lint`"} {
		if !strings.Contains(out, want) {
			t.Errorf("-add review の stdout に %q が無い:\n%s", want, out)
		}
	}
	if strings.Contains(out, "retro check") || strings.Contains(out, "feeds.json") {
		t.Errorf("足していない機能の案内が出ている:\n%s", out)
	}
	// 既定で retro・news・schedule は入っているので、もう一度足しても変わらない
	for _, add := range []string{"retro", "news", "schedule"} {
		so.Reset()
		if code := dispatch([]string{"init", "-add", add, step}, &so, &se); code != 0 {
			t.Fatalf("-add %s exit=%d\n%s", add, code, se.String())
		}
		if strings.Contains(so.String(), "作成: ") || strings.Contains(so.String(), "追記(") {
			t.Errorf("既定で入っている %s を足し直して変更が出た:\n%s", add, so.String())
		}
	}
	if code := dispatch([]string{"init", "-add", "all", once}, &so, &se); code != 0 {
		t.Fatalf("-add all exit=%d\n%s", code, se.String())
	}
	files, err := template.Files(template.KindHub)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		a, err := os.ReadFile(filepath.Join(step, filepath.FromSlash(f.Path)))
		if err != nil {
			t.Errorf("段階的な hub に %s が無い", f.Path)
			continue
		}
		b := []byte(readFile(t, filepath.Join(once, filepath.FromSlash(f.Path))))
		if f.Path == "braindex.json" {
			// 後から足した機能の job は末尾に付くので、並びでなく内容(job は名前順)で比べる
			if ca, cb := canonicalConfig(t, a), canonicalConfig(t, b); ca != cb {
				t.Errorf("braindex.json が段階的と一括で違う:\n--- 段階的\n%s\n--- 一括\n%s", ca, cb)
			}
		} else if !bytes.Equal(a, b) {
			t.Errorf("%s が段階的と一括で違う:\n--- 段階的\n%s\n--- 一括\n%s", f.Path, a, b)
		}
		if !bytes.Equal(b, f.Content) {
			t.Errorf("一括の %s が雛形と違う", f.Path)
		}
	}
	// 2 回目は何も変わらない。依存(conventions)は既にあるので「足した」とは言わず「含めた」と出す
	so.Reset()
	if code := dispatch([]string{"init", "-add", "review", step}, &so, &se); code != 0 {
		t.Fatalf("2 回目 exit=%d\n%s", code, se.String())
	}
	if strings.Contains(so.String(), "作成: ") || strings.Contains(so.String(), "追記") && !strings.Contains(so.String(), "追記 0") {
		t.Errorf("2 回目に変更がある:\n%s", so.String())
	}
	if !strings.Contains(so.String(), "依存として含めた機能: conventions") || strings.Contains(so.String(), "足した機能") {
		t.Errorf("2 回目の依存の文言が不正(既にある機能を「足した」と言っている):\n%s", so.String())
	}
	if strings.Contains(so.String(), "次:") {
		t.Errorf("何も変えていないのに案内が出ている:\n%s", so.String())
	}
}

// 引数の誤り: 未知の機能は候補を出して 1・-repo と -add の併用は 1。どちらも何も書かない。
func TestInit_AddBadArgs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hub")
	var so, se bytes.Buffer
	if code := dispatch([]string{"init", "-add", "review,nope", dir}, &so, &se); code != 1 {
		t.Errorf("未知の機能: exit=%d want 1", code)
	}
	if !strings.Contains(se.String(), `"nope"`) || !strings.Contains(se.String(), "conventions") || !strings.Contains(se.String(), "all") {
		t.Errorf("未知の機能のエラーに候補が無い: %s", se.String())
	}
	se.Reset()
	if code := dispatch([]string{"init", "-repo", "-add", "retro", dir}, &so, &se); code != 1 || !strings.Contains(se.String(), "併用") {
		t.Errorf("-repo と -add: exit=%d stderr=%s", code, se.String())
	}
	if _, err := os.Stat(dir); err == nil {
		t.Errorf("引数の誤りなのに %s が作られた", dir)
	}
}

// -list は機能と配布物の一覧を出して終わる(何も書かない)。
func TestInit_List(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hub")
	var so, se bytes.Buffer
	if code := dispatch([]string{"init", "-list", dir}, &so, &se); code != 0 {
		t.Fatalf("exit=%d\n%s", code, se.String())
	}
	out := so.String()
	for _, want := range []string{"core(既定):", "conventions:", "review:", "retro(既定):", "news(既定):", "schedule(既定):", "all:", "依存: conventions", "設定の節: review", "ファイル: .gitattributes, CLAUDE.md, README.md, braindex.json", "news/feeds.example.json"} {
		if !strings.Contains(out, want) {
			t.Errorf("-list に %q が無い:\n%s", want, out)
		}
	}
	if _, err := os.Stat(dir); err == nil {
		t.Errorf("-list なのに %s が作られた", dir)
	}
	// 引数の誤り(ディレクトリ 2 つ・-repo との併用)は -list でも 1。一覧は出さない
	for _, args := range [][]string{{"init", "-list", dir, "other"}, {"init", "-list", "-repo", "-add", "retro", dir}} {
		so.Reset()
		se.Reset()
		if code := dispatch(args, &so, &se); code != 1 || so.Len() != 0 {
			t.Errorf("%v: exit=%d want 1・stdout=%q want 空\nstderr=%s", args[1:], code, so.String(), se.String())
		}
	}
}

// canonicalConfig は braindex.json を「schedule.jobs を名前順に並べた JSON」に正規化する(比較用)。
func canonicalConfig(t *testing.T, b []byte) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("braindex.json を読めない: %v\n%s", err, b)
	}
	if sc, ok := m["schedule"].(map[string]any); ok {
		if jobs, ok := sc["jobs"].([]any); ok {
			sort.Slice(jobs, func(i, j int) bool {
				return jobs[i].(map[string]any)["name"].(string) < jobs[j].(map[string]any)["name"].(string)
			})
		}
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
