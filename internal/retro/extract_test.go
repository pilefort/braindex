package retro

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pilefort/braindex/internal/sessions"
)

func extractFixture(t *testing.T, w Window, label string) Result {
	t.Helper()
	return Extract(Input{
		Sessions:    fixture(t),
		Window:      w,
		WindowLabel: label,
		Corrections: []*Dictionary{Corrections()},
		Sentiment:   Sentiment(),
		Loc:         time.UTC,
		Home:        "/home/someone",
	})
}

func fileByRel(res Result, rel string) *DigestFile {
	for i := range res.Files {
		if res.Files[i].RelPath == rel {
			return &res.Files[i]
		}
	}
	return nil
}

// セッションごとの md: 見出し → 発話ごとに「直前のアシスタント本文 → ユーザー発話」。訂正・感情のヒットは見出し行に印を付ける。
func TestExtract_Digest(t *testing.T) {
	res := extractFixture(t, Window{}, "全期間")
	var rels []string
	for _, f := range res.Files {
		rels = append(rels, f.RelPath)
	}
	// 並びは開始時刻(時刻なしは先頭)→ セッション ID。ディレクトリ名は表示用プロジェクト名の記号を _ に潰したもの
	want := []string{
		"sessions/_work_other/00000000_0000_d1.md",
		"sessions/_repo-a/20260803_1000_a1.md",
		"sessions/_repo-b/20260812_1200_b1.md",
		"sessions/_work_other/20260825_0100_c1.md",
	}
	if !reflect.DeepEqual(rels, want) {
		t.Fatalf("ファイルの並び:\n want=%v\n  got=%v", want, rels)
	}
	a := fileByRel(res, "sessions/_repo-a/20260803_1000_a1.md")
	wantA := strings.Join([]string{
		"# session a1",
		"project: ~/repo-a",
		"start: 2026-08-03 10:00",
		"end: 2026-08-12 09:00",
		"window: 全期間",
		"user_turns: 3",
		"corrections: 1",
		"",
		"### [08-03 10:00] #1 USER",
		"索引を作って",
		"",
		"### [08-03 10:05] #2 USER ★ 訂正候補: 違う",
		"← ASSISTANT [08-03 10:01]: 違う話ですが作ります",
		"違う、docs だけ",
		"",
		"### [08-12 09:00] #3 USER ☆ 感情: ありがと",
		"ありがとう",
		"",
	}, "\n")
	if got := string(a.Content); got != wantA {
		t.Errorf("a1 のダイジェスト:\n want=%q\n  got=%q", wantA, got)
	}
	if a.UserTurns != 3 || a.Corrections != 1 || a.Session != "a1" || a.Project != "~/repo-a" {
		t.Errorf("a1 のメタ: %+v", *a)
	}
	// 時刻なしのセッション
	d := fileByRel(res, "sessions/_work_other/00000000_0000_d1.md")
	if !strings.HasPrefix(string(d.Content), "# session d1\nproject: /work/other\nstart: -\nend: -\nwindow: 全期間\nuser_turns: 1\ncorrections: 1\n\n### [-] #1 USER ★ 訂正候補: 違う\n時刻なし。違う\n") {
		t.Errorf("d1 のダイジェスト: %q", string(d.Content))
	}
}

func TestExtract_Index(t *testing.T) {
	res := extractFixture(t, Window{}, "全期間")
	want := strings.Join([]string{
		"start\tproject\tsession\tuser_turns\tcorrections\tfile",
		"-\t/work/other\td1\t1\t1\tsessions/_work_other/00000000_0000_d1.md",
		"2026-08-03 10:00\t~/repo-a\ta1\t3\t1\tsessions/_repo-a/20260803_1000_a1.md",
		"2026-08-12 12:00\t~/repo-b\tb1\t2\t1\tsessions/_repo-b/20260812_1200_b1.md",
		"2026-08-25 01:00\t/work/other\tc1\t12\t3\tsessions/_work_other/20260825_0100_c1.md",
		"",
	}, "\n")
	if got := string(res.Index); got != want {
		t.Errorf("index.tsv:\n want=%q\n  got=%q", want, got)
	}
	if res.Sessions != 4 || res.UserTurns != 18 || res.CorrectionTurns != 6 {
		t.Errorf("合計: %+v", res)
	}
}

// 窓の外の発話は書かない。窓に発話が無いセッションはファイルも作らない。時刻なしは窓が無制限のときだけ。
func TestExtract_Window(t *testing.T) {
	res := extractFixture(t, Window{Since: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)}, "2026-08-10 以降")
	var rels []string
	for _, f := range res.Files {
		rels = append(rels, f.RelPath)
	}
	want := []string{"sessions/_repo-a/20260803_1000_a1.md", "sessions/_repo-b/20260812_1200_b1.md", "sessions/_work_other/20260825_0100_c1.md"}
	if !reflect.DeepEqual(rels, want) {
		t.Fatalf("窓で絞ったファイル: want=%v got=%v", want, rels)
	}
	a := string(fileByRel(res, "sessions/_repo-a/20260803_1000_a1.md").Content)
	if !strings.Contains(a, "window: 2026-08-10 以降\nuser_turns: 1\ncorrections: 0\n") || strings.Contains(a, "#1 USER") || !strings.Contains(a, "#3 USER") {
		t.Errorf("a1 は窓の中の #3 だけ: %q", a)
	}
}

// 長い本文は切る: ユーザー 2000 字・アシスタント 300 字。直前のアシスタント本文は前の発話以降の全部を改行でつなぎ、ツール名と回数を添える。
func TestExtract_Truncate(t *testing.T) {
	long := strings.Repeat("あ", 2100)
	asst := strings.Repeat("い", 350)
	s := session("s1", "/x",
		sessions.Turn{Role: sessions.User, Index: 1, Text: "はじめ"},
		sessions.Turn{Role: sessions.Assistant, Text: asst, Tools: []sessions.ToolUse{{Name: "Read", Count: 2}, {Name: "Bash", Count: 1}}},
		sessions.Turn{Role: sessions.Assistant, Text: "つづき"},
		sessions.Turn{Role: sessions.User, Index: 2, Text: long},
	)
	res := Extract(Input{Sessions: []sessions.Session{s}, Loc: time.UTC})
	if len(res.Files) != 1 {
		t.Fatalf("ファイル数: want=1 got=%d", len(res.Files))
	}
	got := string(res.Files[0].Content)
	if !strings.Contains(got, "\n"+strings.Repeat("あ", 2000)+"…(+100 字)\n") {
		t.Errorf("ユーザー本文の切り詰め: %q", got[len(got)-80:])
	}
	// 2 つのアシスタント発話は " / " でつないでから 300 字に切る(350 + 3 + 3 = 356 字 → +56)
	if !strings.Contains(got, "← ASSISTANT [-]: "+strings.Repeat("い", 300)+"…(+56 字) [tools: Read×2, Bash]\n") {
		t.Errorf("アシスタント本文の切り詰めとツール: %q", got)
	}
	// 辞書を渡さなければ印は付かない
	if strings.Contains(got, "★") || strings.Contains(got, "☆") {
		t.Errorf("辞書なしで印が付く: %q", got)
	}
}

// 同じ入力からは同じバイト列(決定性)。
func TestExtract_Deterministic(t *testing.T) {
	a := extractFixture(t, Window{}, "全期間")
	b := extractFixture(t, Window{}, "全期間")
	if !reflect.DeepEqual(a, b) {
		t.Error("同じ入力から 2 回生成して一致しない")
	}
}

func TestSafeName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"~/repo-a", "_repo-a"},
		{"/work/other", "_work_other"},
		{`C:\Users\someone\repo`, "C_Users_someone_repo"},
		{"-work-repo-b", "-work-repo-b"},
		{"", "_"},
	}
	for _, c := range cases {
		if got := safeName(c.in); got != c.want {
			t.Errorf("safeName(%q): want=%q got=%q", c.in, c.want, got)
		}
	}
}
