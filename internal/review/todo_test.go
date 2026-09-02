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
