package verify

import (
	"github.com/pilefort/braindex/internal/sessions"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func sessionTime(n int) time.Time { return time.Date(2026, 1, 1, 0, 0, n, 0, time.UTC) }
func sessionCall(n int, cmd string, failed bool) sessions.Turn {
	return sessions.Turn{Role: sessions.Assistant, Calls: []sessions.ToolCall{{Name: "Bash", Command: cmd, Time: sessionTime(n), HasResult: true, IsError: failed}}}
}
func TestCheckSessionStatuses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		calls  []sessions.Turn
		status string
	}{
		{"found", []sessions.Turn{sessionCall(1, "go test", false)}, Found},
		{"failed", []sessions.Turn{sessionCall(1, "go test", true)}, Failed},
		{"missing", nil, NotFound},
		{"later", []sessions.Turn{sessionCall(11, "go test", false)}, NotFound},
		{"same_time", []sessions.Turn{sessionCall(10, "go test", false)}, NotFound},
		{"latest_failed", []sessions.Turn{sessionCall(2, "pytest", true), sessionCall(1, "npm test", false)}, Failed},
		{"latest_success", []sessions.Turn{sessionCall(1, "pytest", true), sessionCall(2, "npm test", false)}, Found},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := sessions.Session{Turns: append(tc.calls, sessions.Turn{Role: sessions.Assistant, Time: sessionTime(10), Text: "テストが通りました。"})}
			got := CheckSession(s)
			if len(got) != 1 || got[0].Kind != "session:test" || got[0].Status != tc.status {
				t.Fatal(got)
			}
		})
	}
}
func TestCheckSessionExclusions(t *testing.T) {
	for _, text := range []string{"テストが通らない。", "テストが通りません。", "テストが通らず終了。", "未確認だがテストが通った。", "これからテストが通る予定。", "コミットしましたとは言っていない。", "tests pass is planned.", "```text\nテストが通った。\n```", "~~~~\nコミットした。\n~~~\npushした。\n~~~~", "> テストが通った。\n  > コミットした。", "    テストが通った。", "\tコミットした。"} {
		got := CheckSession(sessions.Session{Turns: []sessions.Turn{{Role: sessions.Assistant, Time: sessionTime(10), Text: text}}})
		if len(got) != 1 || got[0].Kind != "session" || got[0].Status != Found {
			t.Errorf("%q: %+v", text, got)
		}
	}
	got := CheckSession(sessions.Session{Turns: []sessions.Turn{{Role: sessions.User, Text: "テストが通った。"}}})
	if got[0].Kind != "session" {
		t.Fatal(got)
	}
}
func TestCheckSessionDictionary(t *testing.T) {
	claims := []string{"テストをパスしました。tests passed.", "ビルドは成功しました。vetが通った。", "コミット済み。", "push済み。プッシュしました。"}
	for i, rule := range sessionRules {
		for _, cmd := range rule.commands {
			got := CheckSession(sessions.Session{Turns: []sessions.Turn{sessionCall(1, cmd, false), {Role: sessions.Assistant, Time: sessionTime(10), Text: claims[i]}}})
			for _, r := range got {
				if r.Kind != "session:"+rule.kind || r.Status != Found {
					t.Errorf("%s: %+v", cmd, r)
				}
			}
		}
	}
}
func TestCheckSessionSubagent(t *testing.T) {
	s := sessions.Session{Turns: []sessions.Turn{sessionCall(1, "go test", true), {Role: sessions.Assistant, Time: sessionTime(10), Text: "テストが通った。"}}, Subagents: []sessions.Subagent{{Turns: []sessions.Turn{sessionCall(2, "go test", false)}}}}
	got := CheckSession(s)
	if len(got) != 1 || got[0].Status != Found || !strings.Contains(got[0].Detail, "subagent=true") {
		t.Fatal(got)
	}
	if !reflect.DeepEqual(got, CheckSession(s)) {
		t.Fatal("非決定的な結果")
	}
}
func TestCheckSessionMissingEvidence(t *testing.T) {
	call := sessionCall(1, "go test", false)
	claim := sessions.Turn{Role: sessions.Assistant, Time: sessionTime(10), Text: "テストが通った。"}
	call.Calls[0].HasResult = false
	if got := CheckSession(sessions.Session{Turns: []sessions.Turn{call, claim}}); got[0].Status != Found || !strings.Contains(got[0].Detail, "成否未確認") {
		t.Fatal(got)
	}
	call.Calls[0].Time = time.Time{}
	if got := CheckSession(sessions.Session{Turns: []sessions.Turn{call, claim}}); got[0].Status != NotFound {
		t.Fatal(got)
	}
	claim.Time = time.Time{}
	if got := CheckSession(sessions.Session{Turns: []sessions.Turn{sessionCall(1, "go test", false), claim}}); got[0].Status != NotFound || !strings.Contains(got[0].Detail, "時刻がない") {
		t.Fatal(got)
	}
	call = sessionCall(1, "go test", false)
	call.Calls[0].Name = "Read"
	claim.Time = sessionTime(10)
	if got := CheckSession(sessions.Session{Turns: []sessions.Turn{call, claim}}); got[0].Status != NotFound {
		t.Fatal(got)
	}
}
func TestCheckSessionDetail(t *testing.T) {
	cmd := "go test\t" + strings.Repeat("字", 80) + "\n"
	got := CheckSession(sessions.Session{Turns: []sessions.Turn{sessionCall(1, cmd, false), {Role: sessions.Assistant, Time: sessionTime(10), Text: "テストが\t通った。"}}})[0]
	want := "2026-01-01T00:00:01Z subagent=false " + string([]rune(collapseSpace(cmd))[:60])
	if got.Detail != want || strings.ContainsAny(got.Target, "\t\n\r") {
		t.Fatal(got)
	}
}
func TestSessionPathAndID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".claude", "projects", "project")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sample.jsonl")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{path, "sample"} {
		got := Session(target)
		if len(got) != 3 || got[0].Status != Found || got[1].Status != Failed || got[2].Status != NotFound {
			t.Fatalf("%s: %+v", target, got)
		}
	}
	subdir := filepath.Join(dir, "sample", "subagents")
	if err := os.MkdirAll(subdir, 0700); err != nil {
		t.Fatal(err)
	}
	sublog := `{"type":"assistant","isSidechain":true,"timestamp":"2026-01-01T00:00:01Z","message":{"content":[{"type":"tool_use","id":"commit","name":"Bash","input":{"command":"git commit -m example"}}]}}
{"type":"user","isSidechain":true,"timestamp":"2026-01-01T00:00:02Z","message":{"content":[{"type":"tool_result","tool_use_id":"commit","is_error":false}]}}
`
	if err := os.WriteFile(filepath.Join(subdir, "agent-example.jsonl"), []byte(sublog), 0600); err != nil {
		t.Fatal(err)
	}
	if got := Session(path); len(got) != 3 || got[2].Status != Found || !strings.Contains(got[2].Detail, "subagent=true") {
		t.Fatal(got)
	}
	if err := os.WriteFile(path, append(append([]byte(nil), data...), []byte("broken\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if got := Session(path); got[0].Status != Error {
		t.Fatal(got)
	}
	if got := Session(filepath.Join(dir, "missing.jsonl")); got[0].Status != Error {
		t.Fatal(got)
	}
	if err := os.WriteFile(path, []byte("broken\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := Session(path); got[0].Status != Error {
		t.Fatal(got)
	}
}
