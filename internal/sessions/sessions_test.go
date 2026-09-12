package sessions

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDir_Sessions_MetadataAndCalls(t *testing.T) {
	all, _ := loadTestdata(t)
	var s Session
	for _, candidate := range all {
		if candidate.ID == "aaaa1111" {
			s = candidate
		}
	}
	if len(s.Turns) != 7 {
		t.Fatalf("Turns: %+v", s.Turns)
	}
	if s.Turns[0].UUID != "u1" || s.Turns[0].ParentUUID != "" || s.Turns[0].Usage != nil {
		t.Errorf("user: %+v", s.Turns[0])
	}
	a := s.Turns[1]
	if a.UUID != "a1" || a.ParentUUID != "u1" {
		t.Errorf("最初の thinking 行の ID: %+v", a)
	}
	wantUsage := Usage{InputTokens: 11, OutputTokens: 971, CacheCreationInputTokens: 13, CacheReadInputTokens: 17}
	if a.Usage == nil || *a.Usage != wantUsage || s.TotalUsage() != wantUsage {
		t.Errorf("usage: %+v total=%+v", a.Usage, s.TotalUsage())
	}
	wantCalls := []ToolCall{
		{ID: "t1", Name: "Read", Time: mustTime(t, "2026-08-20T01:00:07Z"), HasResult: true},
		{ID: "t2", Name: "Read", Time: mustTime(t, "2026-08-20T01:00:08Z"), HasResult: true},
		{ID: "t3", Name: "Bash", Command: "go build ./...", Time: mustTime(t, "2026-08-20T01:00:09Z"), HasResult: true, IsError: true},
	}
	if !reflect.DeepEqual(a.Calls, wantCalls) {
		t.Errorf("Calls: %+v", a.Calls)
	}
}

func TestReader_UsageLastRow(t *testing.T) {
	for _, tc := range []struct {
		name, tail string
		want       *Usage
	}{
		{"増加", `"usage":{"output_tokens":971},"content":"続き"`, &Usage{OutputTokens: 971}},
		{"同値", `"usage":{"output_tokens":7},"content":"続き"`, &Usage{OutputTokens: 7}},
		{"thinking の最終値", `"usage":{"output_tokens":971},"content":[{"type":"thinking"}]`, &Usage{OutputTokens: 971}},
		{"null", `"usage":null,"content":"続き"`, nil},
		{"欠落", `"content":"続き"`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r reader
			r.line([]byte(`{"type":"assistant","uuid":"first","parentUuid":null,"message":{"id":"m","usage":{"output_tokens":7},"content":"開始"}}`))
			r.line([]byte(`{"type":"assistant","uuid":"last","parentUuid":"first","message":{"id":"m",` + tc.tail + `}}`))
			if len(r.s.Turns) != 1 {
				t.Fatalf("Turns: %+v", r.s.Turns)
			}
			got := r.s.Turns[0]
			if got.UUID != "first" || got.ParentUUID != "" || !reflect.DeepEqual(got.Usage, tc.want) {
				t.Errorf("Turn: %+v usage=%+v", got, got.Usage)
			}
		})
	}
}

func TestSession_TotalUsage(t *testing.T) {
	s := Session{Turns: []Turn{{Role: User}, {Role: Assistant, Usage: &Usage{1, 2, 3, 4}}, {Role: Assistant, Usage: &Usage{5, 6, 7, 8}}}, Subagents: []Subagent{{Turns: []Turn{{Usage: &Usage{100, 100, 100, 100}}}}}}
	if got := s.TotalUsage(); got != (Usage{6, 8, 10, 12}) {
		t.Errorf("TotalUsage: %+v", got)
	}
	if got := (Session{}).TotalUsage(); got != (Usage{}) {
		t.Errorf("empty: %+v", got)
	}
}

func TestReader_CallsWithoutResult(t *testing.T) {
	var r reader
	r.line([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"pending","name":"Bash","input":{"command":"go test ./..."}},{"type":"tool_use","id":"other","name":"Read","input":{"command":"ignored"}}]}}`))
	r.line([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"unknown","is_error":true},{"type":"text","text":"続けて"}]}}`))
	got := r.s.Turns[0].Calls
	if len(got) != 2 || got[0].Command != "go test ./..." || got[0].HasResult || got[0].IsError || got[1].Command != "" || got[1].HasResult {
		t.Fatalf("Calls: %+v", got)
	}
}

func TestReader_MetadataAcrossInterleavedRows(t *testing.T) {
	var r reader
	for _, line := range []string{
		`{"type":"assistant","uuid":"first","parentUuid":"parent","message":{"id":"m","usage":{"output_tokens":7},"content":[{"type":"tool_use","id":"call","name":"Bash","input":{"command":"go test ./..."}}]}}`,
		`{"type":"user","message":{"content":"続けて"}}`,
		`{"type":"assistant","message":{"id":"another","usage":{"output_tokens":2},"content":"検査中です。"}}`,
		`{"type":"assistant","uuid":"last","message":{"id":"m","usage":{"output_tokens":971},"content":"検査しました。"}}`,
		`{"type":"user","isMeta":true,"message":{"content":[{"type":"tool_result","tool_use_id":"call","is_error":true},{"type":"text","text":"補足"}]}}`,
	} {
		r.line([]byte(line))
	}
	if len(r.s.Turns) != 4 || r.s.TotalUsage().OutputTokens != 973 {
		t.Fatalf("Turns=%+v total=%+v", r.s.Turns, r.s.TotalUsage())
	}
	first := r.s.Turns[0]
	if first.UUID != "first" || first.ParentUUID != "parent" || first.Usage.OutputTokens != 971 || !first.Calls[0].HasResult || !first.Calls[0].IsError {
		t.Errorf("Turn: %+v", first)
	}
}

func TestDir_Sessions_SubagentsDisabledDoesNotRead(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(filepath.Join(project, "session"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "session.jsonl"), []byte(`{"type":"user","message":{"content":"調べて"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	// ディレクトリでない subagents を置く。既定ではここを開かず警告もしない。
	if err := os.WriteFile(filepath.Join(project, "session", "subagents"), []byte("not a directory"), 0644); err != nil {
		t.Fatal(err)
	}
	got, warnings, err := (Dir{Path: root}).Sessions(Options{})
	if err != nil || len(warnings) != 0 || len(got) != 1 {
		t.Fatalf("got=%+v warnings=%q err=%v", got, warnings, err)
	}
	if got[0].Subagents != nil {
		t.Error("既定で Subagents が nil でない")
	}
}

func TestDir_Sessions_Subagents(t *testing.T) {
	base, baseWarnings := loadTestdata(t)
	for _, s := range base {
		if s.Subagents != nil {
			t.Fatal("既定でサブエージェントを読んだ")
		}
	}
	got, warnings, err := (Dir{Path: "testdata/projects"}).Sessions(Options{IncludeSubagents: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(base) || len(warnings) != len(baseWarnings)+1 {
		t.Fatalf("sessions=%d warnings=%q", len(got), warnings)
	}
	var agents []Subagent
	for i, s := range got {
		agents = append(agents, s.Subagents...)
		got[i].Subagents = nil
	}
	if !reflect.DeepEqual(got, base) {
		t.Error("本体が変わった")
	}
	if len(agents) != 1 {
		t.Fatalf("agents: %+v", agents)
	}
	a := agents[0]
	if a.AgentID != "x" || a.AgentType != "checker" || a.Description != "短い検査" || a.ToolUseID != "dispatch-x" || filepath.Base(a.Path) != "agent-x.jsonl" {
		t.Errorf("meta: %+v", a)
	}
	if len(a.Turns) != 3 {
		t.Fatalf("Turns: %+v", a.Turns)
	}
	if a.Turns[0].Text != "<command-name>check</command-name>" || a.Turns[0].Boilerplate || a.Turns[0].UUID != "sx1" {
		t.Errorf("除外規則を掛けた: %+v", a.Turns[0])
	}
	if len(a.Turns[1].Calls) != 1 || !a.Turns[1].Calls[0].IsError || !a.Turns[1].Calls[0].HasResult {
		t.Errorf("Calls: %+v", a.Turns[1].Calls)
	}
	if !a.Start.Equal(mustTime(t, "2026-08-20T01:00:00Z")) || !a.End.Equal(mustTime(t, "2026-08-20T01:00:02Z")) {
		t.Errorf("Start/End: %v/%v", a.Start, a.End)
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "agent-x.jsonl: JSON でない 1 行") {
		t.Errorf("warnings: %q", warnings)
	}
	second, secondWarnings, err := (Dir{Path: "testdata/projects"}).Sessions(Options{IncludeSubagents: true})
	if err != nil {
		t.Fatal(err)
	}
	for i := range second {
		second[i].Subagents = nil
	}
	if !reflect.DeepEqual(got, second) || !reflect.DeepEqual(warnings, secondWarnings) {
		t.Error("読み取りが決定的でない")
	}
}

func TestDir_Sessions_SubagentsNestedAndMissingMeta(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("testdata/projects")); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "-work-repo-a", "aaaa1111", "subagents", "workflows", "wf_example")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"agent-y.jsonl": `{"type":"assistant","isSidechain":true,"message":{"content":"検査します。"}}`, "journal.jsonl": "broken journal", "other.jsonl": "broken other"} {
		if err := os.WriteFile(filepath.Join(nested, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	got, warnings, err := (Dir{Path: root}).Sessions(Options{IncludeSubagents: true})
	if err != nil {
		t.Fatal(err)
	}
	var agents []Subagent
	for _, s := range got {
		agents = append(agents, s.Subagents...)
	}
	if len(agents) != 2 {
		t.Fatalf("agents: %+v", agents)
	}
	a := agents[1]
	if a.AgentID != "y" || a.AgentType != "" || a.Description != "" || a.ToolUseID != "" || len(a.Turns) != 1 {
		t.Errorf("meta なし: %+v", a)
	}
	if len(warnings) != 3 {
		t.Errorf("journal を読んだ、または meta なしを警告した: %q", warnings)
	}
	first := agents
	again, _, err := (Dir{Path: root}).Sessions(Options{IncludeSubagents: true})
	if err != nil {
		t.Fatal(err)
	}
	agents = nil
	for _, s := range again {
		agents = append(agents, s.Subagents...)
	}
	if !reflect.DeepEqual(first, agents) {
		t.Error("サブエージェントの読み取りが決定的でない")
	}
}

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
	// 人間の発話が無い aaaa3333・cccc0001 と、<slug>/ の下に無い stray.jsonl は含めない。並びは開始時刻の昇順(時刻なしは先頭)。
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
	// JSON でない 1 行は警告して飛ばす(本文は出さない)。2 本目は cccc0001 の未確認の版
	if len(warns) != 2 || !strings.Contains(warns[0], "aaaa1111.jsonl") || !strings.Contains(warns[0], "1 行") {
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
	_, _, err := (Dir{Path: "testdata/no-such-dir"}).Sessions(Options{})
	if err == nil {
		t.Fatal("無い置き場はエラーにする")
	}
	// 初めての利用者が最初に見る文言なので、OS の生エラーでなく「何が無いか・いつ作られるか」を言う
	if want := "セッションログの置き場が無い: testdata/no-such-dir(Claude Code を使うと ~/.claude/projects に作られる)"; err.Error() != want {
		t.Errorf("err:\n want=%q\n  got=%q", want, err.Error())
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

// line の前段の振り分け: "user"/"assistant" を含まない行は JSON を解かずに捨て(bad に数えない)、
// 含むが JSON として壊れている行と、括弧で閉じていない行は bad に数える。
func TestReaderLine_BadRows(t *testing.T) {
	cases := []struct {
		desc string
		line string
		bad  int
	}{
		{"user を含まない行は解かずに捨てる(bad にしない)", `{"type":"mode","mode":"default"}`, 0},
		{"括弧が閉じていない", `{"type":"user","content":"x"`, 1},
		{"括弧は閉じているが JSON として壊れている", `{"type":"user",,,}`, 1},
		{"type が user/assistant でない行は捨てる(bad にしない)", `{"type":"system","content":"role user"}`, 0},
	}
	for _, c := range cases {
		var r reader
		r.line([]byte(c.line))
		if r.bad != c.bad {
			t.Errorf("line[%s]: bad want=%d got=%d", c.desc, c.bad, r.bad)
		}
		if len(r.s.Turns) != 0 {
			t.Errorf("line[%s]: 発話が増えた: %+v", c.desc, r.s.Turns)
		}
	}
}

// ログの形式は Claude Code の版ごとに変わりうるので、確認済みの範囲の外の版は伝える。
// 除外規則は変えない(合わない証拠が無いうちに挙動を変えると、確かめた版での結果まで動く)。
func TestVersionInRange(t *testing.T) {
	cases := []struct {
		desc, v string
		want    bool
	}{
		{"下限ちょうど", MinKnownVersion, true},
		{"上限ちょうど", MaxKnownVersion, true},
		{"下限より古い", "2.1.257", false},
		{"上限より新しい", "2.1.264", false},
		{"メジャーが古い", "1.9.999", false},
		{"メジャーが新しい", "3.0.0", false},
		{"桁の違いを数値で比べる(2.1.9 は 2.1.10 より小さい)", "2.1.9", false},
		{"段が足りない", "2.1", false},
		{"段が多い(下限と同じ 2.1.258 の後ろに 0)", "2.1.258.0", true},
		{"空は不明として範囲内", "", true},
		{"数値でないものは不明として範囲内", "2.1.258-beta", true},
		{"数値でないものは不明として範囲内(語)", "unknown", true},
		{"負の数は不明として範囲内", "2.-1.0", true},
	}
	for _, c := range cases {
		if got := VersionInRange(c.v); got != c.want {
			t.Errorf("VersionInRange[%s] %q: want=%v got=%v", c.desc, c.v, c.want, got)
		}
	}
}

func TestDir_Sessions_未確認の版を伝える(t *testing.T) {
	_, warns := loadTestdata(t)
	var hits []string
	for _, w := range warns {
		if strings.Contains(w, "確認済み(") {
			hits = append(hits, w)
		}
	}
	// testdata の版は 2.1.258(確認済み)・空(不明)・9.0.0(新しい側)。cccc0001 は人間の発話が
	// 無くて一覧には出ないが、読んだファイルなので版は数える
	if len(hits) != 1 {
		t.Fatalf("版の警告: %q", hits)
	}
	for _, want := range []string{"より新しい版が 1 種・1 ファイル", "最も新しい 9.0.0", MinKnownVersion + "〜" + MaxKnownVersion} {
		if !strings.Contains(hits[0], want) {
			t.Errorf("警告に %q が無い: %q", want, hits[0])
		}
	}
}

func TestUnknownVersionWarnings(t *testing.T) {
	// 古い側と新しい側で 1 行ずつ。確認済みの版・空・読めない版は数えない
	got := unknownVersionWarnings(map[string]int{
		"2.1.9": 3, "2.1.100": 1, "9.0.0": 2, "10.0.0": 1,
		MinKnownVersion: 5, "": 4, "2.1.258-beta": 1,
	})
	if len(got) != 2 {
		t.Fatalf("警告の数: %d %q", len(got), got)
	}
	for _, want := range []string{"より古い版が 2 種・4 ファイル", "最も古い 2.1.9"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("古い側に %q が無い: %q", want, got[0])
		}
	}
	for _, want := range []string{"より新しい版が 2 種・3 ファイル", "最も新しい 10.0.0", "MaxKnownVersion を上げる"} {
		if !strings.Contains(got[1], want) {
			t.Errorf("新しい側に %q が無い: %q", want, got[1])
		}
	}
	if unknownVersionWarnings(map[string]int{MinKnownVersion: 1, "": 2}) != nil {
		t.Error("範囲外が無ければ何も返さない")
	}
}

// UnderRoot: cwd が root の配下にあるセッションだけ返す。配下でないもの・cwd が無いもの(置き場の
// ディレクトリ名しか分からないもの)は除いて件数を warning にまとめる(設計レビュー 2026-09-06 M2)。
func TestUnderRoot(t *testing.T) {
	sep := string(filepath.Separator)
	root := sep + filepath.Join("work")
	cases := []struct {
		desc, project string
		under, known  bool
	}{
		{"直下", filepath.Join(root, "repo-a"), true, true},
		{"孫", filepath.Join(root, "repo-a", "sub"), true, true},
		{"root そのもの", root, true, true},
		{"外", sep + filepath.Join("other", "repo-b"), false, true},
		{"root の 1 つ上", sep, false, true},
		{"名前が前方一致するだけの別ディレクトリ", sep + filepath.Join("workspace", "x"), false, true},
		{"cwd が無い(置き場の slug)", "-work-repo-a", false, false},
		{"空", "", false, false},
	}
	for _, c := range cases {
		under, known := underRoot(c.project, root)
		if under != c.under || known != c.known {
			t.Errorf("underRoot[%s] %q: want=(%v,%v) got=(%v,%v)", c.desc, c.project, c.under, c.known, under, known)
		}
	}
}

func TestDir_Sessions_UnderRootで絞る(t *testing.T) {
	// testdata のセッションの cwd は /work/repo-a と /work/repo-b。bbbb4444 は cwd が無い
	all, _, err := Dir{Path: "testdata/projects"}.Sessions(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("絞らないとき: %d 件", len(all))
	}

	root := string(filepath.Separator) + filepath.Join("work", "repo-a")
	got, warns, err := Dir{Path: "testdata/projects"}.Sessions(Options{UnderRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range got {
		ids = append(ids, s.ID)
	}
	if want := []string{"aaaa1111"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("root 配下だけ: want=%v got=%v", want, ids)
	}
	var outside, unknown bool
	for _, w := range warns {
		if strings.Contains(w, "の外のセッション 1 件を除いた") {
			outside = true
		}
		if strings.Contains(w, "作業ディレクトリが分からないセッション 1 件を除いた") {
			unknown = true
		}
	}
	if !outside || !unknown {
		t.Errorf("除いた件数の警告が無い: %q", warns)
	}
}
