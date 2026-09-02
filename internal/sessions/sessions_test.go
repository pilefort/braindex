package sessions

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatalf("時刻の書き間違い %q: %v", s, err)
	}
	return tm
}

// 架空のログ testdata/projects を読む。中身は sessions_test の期待値と対で保つ。
func loadTestdata(t *testing.T) ([]Session, []string) {
	t.Helper()
	got, warns, err := Dir{Path: "testdata/projects"}.Sessions(Options{})
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	return got, warns
}

func TestExcludeReason(t *testing.T) {
	cases := []struct {
		desc, text, want string
	}{
		{"ふつうの発話", "索引を作って", ""},
		{"reminder 付きの発話は数える", "<system-reminder>\n注意書き\n</system-reminder>\n違う、docs だけ", ""},
		{"reminder だけ", "<system-reminder>注意書き</system-reminder>", "empty"},
		{"複数の reminder だけ", "<system-reminder>a</system-reminder>\n<system-reminder>b</system-reminder>\n", "empty"},
		{"空白だけ", "  \n\t", "empty"},
		{"スラッシュコマンド", "<command-name>/clear</command-name>\n<command-message>clear</command-message>", "command"},
		{"ローカルコマンドの出力", "<local-command-stdout>ok</local-command-stdout>", "command"},
		{"reminder の後ろのコマンドも command", "<system-reminder>x</system-reminder>\n<command-name>/model</command-name>", "command"},
		{"継続要約", "This session is being continued from a previous conversation that ran out of context.", "continuation"},
		{"中断(先頭)", "[Request interrupted by user for tool use]", "interrupt"},
		{"中断(途中に含む)", "やっぱりやめて\n[Request interrupted by user]", "interrupt"},
		{"サブエージェントの完了通知", "<task-notification>\n<task-id>abc</task-id>\n<status>completed</status>\n</task-notification>", "task-notification"},
		{"本文中の task-notification という語は数える", "task-notification の形式を変えたい", ""},
	}
	for _, c := range cases {
		if got := ExcludeReason(c.text); got != c.want {
			t.Errorf("ExcludeReason[%s]: want=%q got=%q", c.desc, c.want, got)
		}
	}
}

func TestDir_Sessions_OrderAndSkip(t *testing.T) {
	got, _ := loadTestdata(t)
	// 人間の発話が無い aaaa3333 と、<slug>/ の下に無い stray.jsonl は含めない。並びは開始時刻の昇順(時刻なしは先頭)。
	var ids []string
	for _, s := range got {
		ids = append(ids, s.ID)
	}
	want := []string{"bbbb4444", "bbbb2222", "aaaa1111"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("セッションの並び: want=%v got=%v", want, ids)
	}
}

func TestDir_Sessions_MainSession(t *testing.T) {
	got, warns := loadTestdata(t)
	var s Session
	for _, x := range got {
		if x.ID == "aaaa1111" {
			s = x
		}
	}
	if s.ID == "" {
		t.Fatal("aaaa1111 が読めていない")
	}
	if s.Project != "/work/repo-a" || s.Version != "2.1.258" {
		t.Errorf("Project/Version: got=%q/%q", s.Project, s.Version)
	}
	if filepath.Base(s.Path) != "aaaa1111.jsonl" {
		t.Errorf("Path: got=%q", s.Path)
	}
	if !s.Start.Equal(mustTime(t, "2026-08-20T01:00:00.000Z")) || !s.End.Equal(mustTime(t, "2026-08-20T01:11:00.000Z")) {
		t.Errorf("Start/End: got=%v/%v", s.Start, s.End)
	}
	if s.UserTurns != 3 {
		t.Errorf("UserTurns: want=3 got=%d", s.UserTurns)
	}
	want := []Turn{
		{Role: User, Index: 1, Time: mustTime(t, "2026-08-20T01:00:00.000Z"), Text: "最初の依頼です。索引を作って"},
		// 同じ message.id の assistant 行(thinking → text → tool_use×3)は 1 発話に束ねる。
		// 時刻は本文か tool_use を含む最初の行のもの(thinking だけの行 01:00:05 は数えない)
		{Role: Assistant, Time: mustTime(t, "2026-08-20T01:00:06.000Z"), Text: "はい、作ります。", Tools: []ToolUse{{Name: "Read", Count: 2}, {Name: "Bash", Count: 1}}},
		{Role: Assistant, Time: mustTime(t, "2026-08-20T01:01:00.000Z"), Text: "できました。"},
		// system-reminder のブロックは本文から落とす
		{Role: User, Index: 2, Time: mustTime(t, "2026-08-20T01:02:00.000Z"), Text: "違う、そうじゃなくて docs だけ"},
		{Role: Assistant, Time: mustTime(t, "2026-08-20T01:09:00.000Z"), Text: "直します。"},
		{Role: User, Index: 3, Time: mustTime(t, "2026-08-20T01:10:00.000Z"), Text: "ありがとう。次は README"},
		{Role: Assistant, Time: mustTime(t, "2026-08-20T01:11:00.000Z"), Text: "README を直しました。"},
	}
	if len(s.Turns) != len(want) {
		t.Fatalf("Turns の数: want=%d got=%d\n%+v", len(want), len(s.Turns), s.Turns)
	}
	for i := range want {
		if !turnEqual(s.Turns[i], want[i]) {
			t.Errorf("Turns[%d]:\n want=%+v\n  got=%+v", i, want[i], s.Turns[i])
		}
	}
	if h := s.HumanTurns(); len(h) != 3 || h[0].Index != 1 || h[2].Index != 3 {
		t.Errorf("HumanTurns: got=%+v", h)
	}
	// JSON でない 1 行は警告して飛ばす(本文は出さない)
	if len(warns) != 1 || !strings.Contains(warns[0], "aaaa1111.jsonl") || !strings.Contains(warns[0], "1 行") {
		t.Errorf("warnings: got=%q", warns)
	}
}

func turnEqual(a, b Turn) bool {
	return a.Role == b.Role && a.Index == b.Index && a.Time.Equal(b.Time) && a.Text == b.Text && reflect.DeepEqual(a.Tools, b.Tools)
}

func TestDir_Sessions_Fallbacks(t *testing.T) {
	got, _ := loadTestdata(t)
	var s Session
	for _, x := range got {
		if x.ID == "bbbb4444" {
			s = x
		}
	}
	// cwd が無ければディレクトリ名(slug)。timestamp が無ければゼロ値
	if s.Project != "-work-repo-b" {
		t.Errorf("Project(slug fallback): got=%q", s.Project)
	}
	if !s.Start.IsZero() || !s.End.IsZero() {
		t.Errorf("Start/End(時刻なし): got=%v/%v", s.Start, s.End)
	}
	if s.UserTurns != 1 || len(s.Turns) != 2 || s.Turns[0].Text != "時刻なしの発話" {
		t.Errorf("Turns: got=%+v", s.Turns)
	}
}

func TestDir_Sessions_Since(t *testing.T) {
	// mtime が Since より古いファイルは開かない。git は mtime を保たないので、一時ディレクトリに写して自分で設定する
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("testdata/projects")); err != nil {
		t.Fatal(err)
	}
	old := mustTime(t, "2026-08-01T00:00:00Z")
	recent := mustTime(t, "2026-09-01T00:00:00Z")
	for _, f := range []string{"-work-repo-a/aaaa1111.jsonl", "-work-repo-a/aaaa3333.jsonl"} {
		if err := os.Chtimes(filepath.Join(dir, f), old, old); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"-work-repo-b/bbbb2222.jsonl", "-work-repo-b/bbbb4444.jsonl"} {
		if err := os.Chtimes(filepath.Join(dir, f), recent, recent); err != nil {
			t.Fatal(err)
		}
	}
	got, _, err := Dir{Path: dir}.Sessions(Options{Since: mustTime(t, "2026-08-15T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range got {
		ids = append(ids, s.ID)
	}
	if want := []string{"bbbb4444", "bbbb2222"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("Since で絞った結果: want=%v got=%v", want, ids)
	}
}

func TestDir_Sessions_Deterministic(t *testing.T) {
	a, wa := loadTestdata(t)
	b, wb := loadTestdata(t)
	if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(wa, wb) {
		t.Error("同じ入力から 2 回読んで結果が違う")
	}
}

func TestDir_Sessions_MissingRoot(t *testing.T) {
	if _, _, err := (Dir{Path: "testdata/no-such-dir"}).Sessions(Options{}); err == nil {
		t.Error("無い置き場はエラーにする")
	}
}

func TestDisplayPath(t *testing.T) {
	cases := []struct{ path, home, want string }{
		{"/home/someone/projects/repo", "/home/someone", "~/projects/repo"},
		{`C:\Users\someone\projects\repo`, `C:\Users\someone`, `~\projects\repo`},
		{"/home/someone", "/home/someone", "~"},
		{"/home/someone-else/repo", "/home/someone", "/home/someone-else/repo"}, // 境界が違うものは置換しない
		{"/work/repo-a", "/home/someone", "/work/repo-a"},
		{"", "/home/someone", ""},
		{"/home/someone/repo", "", "/home/someone/repo"},
	}
	for _, c := range cases {
		if got := DisplayPath(c.path, c.home); got != c.want {
			t.Errorf("DisplayPath(%q, %q): want=%q got=%q", c.path, c.home, c.want, got)
		}
	}
}
