package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// root 直下に 3 ディレクトリ: alpha(git・TODO を 2 回コミット)、beta(git 管理外・TODO の mtime を古くする)、
// gamma(TODO 無し)。cutoff 2026-08-05 で、alpha の 07-01 の行と beta の全行(mtime 06-01)だけが残る。
func TestStaleTodos(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	if err := os.MkdirAll(alpha, 0o755); err != nil {
		t.Fatal(err)
	}
	r := newTestRepoAt(t, alpha)
	r.write("work/TODO.md", "# TODO\n\n- [ ] 古い項目 x\n- [ ] 直す予定 y\n- [x] 済み\n")
	r.commit("2026-07-01", "todo")
	r.write("work/TODO.md", "# TODO\n\n- [ ] 古い項目 x\n- [ ] 直した y\n- [x] 済み\n* [ ] 新しい z\n")
	r.commit("2026-08-25", "todo2")

	beta := filepath.Join(root, "beta", "work")
	if err := os.MkdirAll(beta, 0o755); err != nil {
		t.Fatal(err)
	}
	betaTodo := filepath.Join(beta, "TODO.md")
	if err := os.WriteFile(betaTodo, []byte("- [ ] p\n- [ ] q\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(betaTodo, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "gamma", "docs"), 0o755); err != nil {
		t.Fatal(err)
	}

	repos, warnings, err := StaleTodos(root, &r.git, "2026-08-05")
	if err != nil {
		t.Fatalf("StaleTodos: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("警告なしを期待: %q", warnings)
	}
	if len(repos) != 2 || repos[0].Repo != "alpha" || repos[1].Repo != "beta" {
		t.Fatalf("repos: %+v", repos)
	}
	a := repos[0].Items
	if len(a) != 1 || a[0].Text != "古い項目 x" || a[0].Line != 3 || a[0].Date != "2026-07-01" || a[0].Approx {
		t.Errorf("alpha: %+v", a)
	}
	b := repos[1].Items
	if len(b) != 2 || b[0].Text != "p" || b[1].Text != "q" || !b[0].Approx || b[0].Date != old.Local().Format("2006-01-02") {
		t.Errorf("beta: %+v", b)
	}

	// git を使わなければ alpha も mtime(今日)になり、放置には数えない
	repos, _, err = StaleTodos(root, nil, "2026-08-05")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].Repo != "beta" {
		t.Errorf("git なし: %+v", repos)
	}

	// 閾値ちょうどの日は含む(節の文言「cutoff 以前から」)。1 日前を閾値にすれば外れる
	repos, _, err = StaleTodos(root, &r.git, "2026-07-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 || repos[0].Repo != "alpha" || len(repos[0].Items) != 1 {
		t.Errorf("閾値ちょうど: %+v", repos)
	}
	repos, _, err = StaleTodos(root, &r.git, "2026-06-30")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].Repo != "beta" {
		t.Errorf("閾値の前日: %+v", repos)
	}
}

// CRLF の TODO.md でも blame の行と本文の行が対応し、Text に \r が残らない。
// リポ側の core.autocrlf を false にして blob も作業ツリーも CRLF のままにする(実行環境の autocrlf に左右されないため)。
func TestStaleTodos_CRLF(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "crlf")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := newTestRepoAt(t, dir)
	r.run("config", "core.autocrlf", "false")
	r.write("work/TODO.md", "- [ ] a\r\n  - [ ] sub\r\n")
	r.commit("2026-07-01", "crlf")
	r.write("work/TODO.md", "- [ ] a\r\n  - [ ] sub\r\n* [ ] b\r\n")
	r.commit("2026-08-25", "crlf2")
	repos, warnings, err := StaleTodos(root, &r.git, "2026-08-05")
	if err != nil || len(warnings) != 0 {
		t.Fatalf("err=%v warnings=%q", err, warnings)
	}
	if len(repos) != 1 || len(repos[0].Items) != 2 {
		t.Fatalf("repos: %+v", repos)
	}
	for i, want := range []TodoItem{{Text: "a", Line: 1, Date: "2026-07-01"}, {Text: "sub", Line: 2, Date: "2026-07-01"}} {
		if repos[0].Items[i] != want {
			t.Errorf("[%d]: want %+v got %+v", i, want, repos[0].Items[i])
		}
	}
}

// 未チェック項目の判定: 記号は - * +、字下げ・タブ・末尾の空白・CRLF の \r を許す。
// 済み([x] [X])・空の項目・記号と [ ] の間や後ろに空白が無いもの・番号付きは拾わない。
func TestTodoLine(t *testing.T) {
	match := map[string]string{
		"- [ ] x":           "x",
		"* [ ] x":           "x",
		"+ [ ] x":           "x",
		"  - [ ] 字下げ":       "字下げ",
		"-\t[ ]\tタブ":        "タブ",
		"- [ ] 末尾の空白   ":    "末尾の空白",
		"- [ ] x\r":         "x",
		"- [ ] 途中の [ ] も本文": "途中の [ ] も本文",
	}
	for in, text := range match {
		m := todoLine.FindStringSubmatch(in)
		if m == nil || m[1] != text {
			t.Errorf("%q: want %q got %v", in, text, m)
		}
	}
	for _, in := range []string{"- [x] 済み", "- [X] 済み", "- [ ]", "- [ ]   ", "-[ ] x", "- [ ]x", "- [] x", "[ ] x", "1. [ ] x", "- [ ] "} {
		if todoLine.MatchString(in) {
			t.Errorf("%q を拾った", in)
		}
	}
}

// blame の行日付: コミット済みの行は著者日、未コミットの行は今日(放置にならない)。未追跡は ok=false。
func TestLineDates(t *testing.T) {
	r := newTestRepo(t)
	r.write("work/TODO.md", "- [ ] a\n")
	r.commit("2026-07-01", "a")
	r.write("work/TODO.md", "- [ ] a\n- [ ] b\n")
	dates, ok := r.git.LineDates(r.dir, "work/TODO.md")
	if !ok || len(dates) != 2 || dates[0] != "2026-07-01" {
		t.Fatalf("dates=%v ok=%v", dates, ok)
	}
	if dates[1] < time.Now().AddDate(0, 0, -1).Format("2006-01-02") {
		t.Errorf("未コミットの行が古い日付: %s", dates[1])
	}
	r.write("docs/untracked.md", "x\n")
	if _, ok := r.git.LineDates(r.dir, "docs/untracked.md"); ok {
		t.Errorf("未追跡なのに ok")
	}
}

// blameSample は 3 行の TODO.md に対する git blame --line-porcelain の実出力(git 2.x・2026-09-03 に採取。ハッシュは架空)。
// 1 行目は root コミット(boundary 付き・2026-07-02T01:00+09:00 = UTC では 07-01)、2 行目は別コミット(08-25 UTC)、
// 3 行目は未コミット(author-time は採取時の「今」)。
const blameSample = "1111111111111111111111111111111111111111 1 1 1\n" +
	"author t\nauthor-mail <t@example.com>\nauthor-time 1782921600\nauthor-tz +0900\n" +
	"committer t\ncommitter-mail <t@example.com>\ncommitter-time 1782921600\ncommitter-tz +0900\n" +
	"summary first\nboundary\nfilename work/TODO.md\n\t- [ ] a\n" +
	"2222222222222222222222222222222222222222 2 2 1\n" +
	"author t\nauthor-mail <t@example.com>\nauthor-time 1787659200\nauthor-tz +0000\n" +
	"committer t\ncommitter-mail <t@example.com>\ncommitter-time 1787659200\ncommitter-tz +0000\n" +
	"summary second\nprevious 1111111111111111111111111111111111111111 work/TODO.md\nfilename work/TODO.md\n\t- [ ] b2\n" +
	"0000000000000000000000000000000000000000 3 3 1\n" +
	"author Not Committed Yet\nauthor-mail <not.committed.yet>\nauthor-time 1788365997\nauthor-tz +0900\n" +
	"committer Not Committed Yet\ncommitter-mail <not.committed.yet>\ncommitter-time 1788365997\ncommitter-tz +0900\n" +
	"summary Version of work/TODO.md from work/TODO.md\nprevious 2222222222222222222222222222222222222222 work/TODO.md\nfilename work/TODO.md\n\t\n"

// blame の解析: 行本体(タブ始まり)ごとに直前の author-time を author-tz で日付にする。boundary・未コミット・空行も 1 行。
// author-time が無い・読めない行があれば ok=false(1970-01-01 のような日付を作らず、mtime に倒す)。
func TestParseBlame(t *testing.T) {
	dates, ok := parseBlame(blameSample)
	want := []string{"2026-07-02", "2026-08-25", "2026-09-03"}
	if !ok || strings.Join(dates, ",") != strings.Join(want, ",") {
		t.Errorf("want %v ok=true, got %v ok=%v", want, dates, ok)
	}
	if dates, ok := parseBlame(""); !ok || len(dates) != 0 {
		t.Errorf("空ファイル: dates=%v ok=%v", dates, ok)
	}
	if dates, ok := parseBlame(strings.Replace(blameSample, "author-time 1787659200", "author-time x", 1)); ok {
		t.Errorf("author-time が読めないのに ok: %v", dates)
	}
	if dates, ok := parseBlame("\t- [ ] a\n"); ok {
		t.Errorf("author-time が無いのに ok: %v", dates)
	}
}

func TestFormatEpoch(t *testing.T) {
	// 2026-07-01T23:30:00+09:00 = 2026-07-01T14:30:00Z。UTC で見れば同日、-10:00 なら前日
	epoch := time.Date(2026, 7, 1, 14, 30, 0, 0, time.UTC).Unix()
	if d := formatEpoch(epoch, "+0900"); d != "2026-07-01" {
		t.Errorf("+0900: %s", d)
	}
	if d := formatEpoch(epoch, "-1000"); d != "2026-07-01" {
		t.Errorf("-1000: %s", d)
	}
	if d := formatEpoch(epoch, "-1500"); d != "2026-06-30" {
		t.Errorf("-1500: %s", d)
	}
	if d := formatEpoch(epoch, "bad"); d != "2026-07-01" {
		t.Errorf("不正な tz は UTC: %s", d)
	}
}

func TestWriteTodoSection(t *testing.T) {
	repos := []RepoTodos{
		{Repo: "alpha", Items: []TodoItem{{Text: "x", Line: 3, Date: "2026-07-01"}}},
		{Repo: "beta", Items: []TodoItem{{Text: "p", Line: 1, Date: "2026-06-01", Approx: true}}},
	}
	var b strings.Builder
	WriteTodoSection(&b, repos, 4, "2026-08-05")
	want := "## 放置 TODO\n\n4 週間以上（2026-08-05 以前から）動いていない未完了の項目。日付は行が最後に変わった日（`git blame`）。`~` は git で追えず mtime。\n\n### alpha\n- [ ] x（2026-07-01）\n\n### beta\n- [ ] p（~2026-06-01）\n"
	if b.String() != want {
		t.Errorf("節が不一致:\n--- got ---\n%s\n--- want ---\n%s", b.String(), want)
	}
	b.Reset()
	WriteTodoSection(&b, nil, 4, "2026-08-05")
	if !strings.HasSuffix(b.String(), "\n- なし\n") {
		t.Errorf("なし:\n%s", b.String())
	}
}
