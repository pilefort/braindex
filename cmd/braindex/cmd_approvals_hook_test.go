package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/approvals"
)

// hookHarness は stdin と起動を差し替えて approvals hook を回す土台。
type hookHarness struct {
	t       *testing.T
	hub     string
	file    string
	dir     string
	spawned []approvals.Paths
}

func newHookHarness(t *testing.T) *hookHarness {
	t.Helper()
	root := t.TempDir()
	h := &hookHarness{
		t:    t,
		hub:  filepath.Join(root, "hub"),
		file: filepath.Join(root, "hub", "work", "APPROVALS.md"),
		dir:  filepath.Join(root, "tmp"),
	}
	stdin, spawn := approvalsHookStdin, approvalsHookSpawn
	t.Cleanup(func() { approvalsHookStdin, approvalsHookSpawn = stdin, spawn })
	approvalsHookSpawn = func(p approvals.Paths, timeout float64, noOpen bool) (int, error) {
		h.spawned = append(h.spawned, p)
		return 4242, nil
	}
	return h
}

func (h *hookHarness) write(body string) {
	h.t.Helper()
	writeFile(h.t, h.file, body)
}

// run は hook を 1 回動かし、stdout に返った JSON を返す(無出力なら nil)。
func (h *hookHarness) run(args ...string) *hookOutput {
	h.t.Helper()
	in, err := json.Marshal(hookInput{CWD: h.hub})
	if err != nil {
		h.t.Fatal(err)
	}
	return h.runWithInput(string(in), args...)
}

func (h *hookHarness) runWithInput(stdin string, args ...string) *hookOutput {
	h.t.Helper()
	approvalsHookStdin = strings.NewReader(stdin)
	var so, se bytes.Buffer
	argv := append([]string{"approvals", "hook", "-dir", h.dir}, args...)
	if code := dispatch(argv, &so, &se); code != 0 {
		h.t.Fatalf("hook は常に 0 を返すはず: code=%d\n%s", code, se.String())
	}
	if so.Len() == 0 {
		return nil
	}
	var out hookOutput
	if err := json.Unmarshal(so.Bytes(), &out); err != nil {
		h.t.Fatalf("stdout が JSON でない: %q", so.String())
	}
	return &out
}

func TestApprovalsHook_NoPendingDoesNothing(t *testing.T) {
	h := newHookHarness(t)

	if out := h.run(); out != nil { // 判断待ちのファイルがまだ無い
		t.Errorf("ファイルが無いのに出力した: %+v", out)
	}
	h.write("# 承認待ち\n\n(なし)\n")
	if out := h.run(); out != nil {
		t.Errorf("項目 0 件なのに出力した: %+v", out)
	}
	if len(h.spawned) != 0 {
		t.Errorf("起動してはいけない: %d 回", len(h.spawned))
	}
}

func TestApprovalsHook_OpensFormAndAsksToTell(t *testing.T) {
	h := newHookHarness(t)
	h.write(sampleApprovals)

	out := h.run()
	if out == nil {
		t.Fatal("出力が無い")
	}
	if len(h.spawned) != 1 {
		t.Fatalf("起動回数=%d", len(h.spawned))
	}
	if h.spawned[0].Approvals != h.file {
		t.Errorf("別のファイルを渡した: %s", h.spawned[0].Approvals)
	}
	mustContain(t, "systemMessage", out.SystemMessage, "1 件")
	if out.Decision != "block" {
		t.Errorf("decision=%q", out.Decision)
	}
	mustContain(t, "reason", out.Reason, "1 件", "リンクを置くだけで終えない")
	// 回答が届いたらアシスタントが続きに戻れるよう、待つコマンドを置き場ごと渡す
	mustContain(t, "reason", out.Reason, "approvals wait -file \""+filepath.ToSlash(h.file)+"\"", "-dir \""+filepath.ToSlash(h.dir)+"\"", "バックグラウンド")
	// Git Bash では引用符の外の \ が消え、コマンド名が見つからなくなる。パスは / 区切りで渡す
	// (Go は Windows でも / 区切りを受け付ける)。
	start, end := strings.Index(out.Reason, "`"), strings.LastIndex(out.Reason, "`")
	if start < 0 || end <= start {
		t.Fatalf("コマンド行が ` で囲まれていない: %s", out.Reason)
	}
	if cmd := out.Reason[start+1 : end]; strings.Contains(cmd, `\`) {
		t.Errorf("コマンド行に \\ が残っている: %s", cmd)
	}
}

func TestApprovalsHook_SameContentOpensOnce(t *testing.T) {
	h := newHookHarness(t)
	h.write(sampleApprovals)

	h.run()
	if out := h.run(); out != nil {
		t.Errorf("同じ内容で 2 回目を出力した: %+v", out)
	}
	if len(h.spawned) != 1 {
		t.Errorf("同じ内容で %d 回開いた", len(h.spawned))
	}
}

func TestApprovalsHook_ChangedContentOpensAgain(t *testing.T) {
	h := newHookHarness(t)
	h.write(sampleApprovals)
	h.run()

	h.write(sampleApprovals + "\n## 2. 二件目の判断\n\n**決めたいこと:** 何か\n")
	out := h.run()
	if out == nil {
		t.Fatal("内容が変わったのに開かなかった")
	}
	if len(h.spawned) != 2 {
		t.Errorf("起動回数=%d", len(h.spawned))
	}
	mustContain(t, "systemMessage", out.SystemMessage, "2 件")
}

func TestApprovalsHook_StopHookActiveDoesNotBlock(t *testing.T) {
	h := newHookHarness(t)
	h.write(sampleApprovals)

	in, err := json.Marshal(hookInput{CWD: h.hub, StopHookActive: true})
	if err != nil {
		t.Fatal(err)
	}
	out := h.runWithInput(string(in))
	if out == nil {
		t.Fatal("出力が無い")
	}
	if out.Decision != "" || out.Reason != "" {
		t.Errorf("続きを促してはいけない: %+v", out) // 促すと終わりのない往復になる
	}
	mustContain(t, "systemMessage", out.SystemMessage, "1 件")
}

func TestApprovalsHook_ReasonCanBeOverridden(t *testing.T) {
	h := newHookHarness(t)
	h.write(sampleApprovals)

	out := h.run("-reason", "利用側の規約に合わせた文")
	if out == nil {
		t.Fatal("出力が無い")
	}
	if out.Reason != "利用側の規約に合わせた文" {
		t.Errorf("reason=%q", out.Reason)
	}
}

func TestApprovalsHook_FileFlagWinsOverCwd(t *testing.T) {
	h := newHookHarness(t)
	other := filepath.Join(t.TempDir(), "spoke", "work", "APPROVALS.md")
	writeFile(t, other, sampleApprovals)

	if out := h.run("-file", other); out == nil {
		t.Fatal("出力が無い")
	}
	if len(h.spawned) != 1 || h.spawned[0].Approvals != other {
		t.Errorf("-file を優先していない: %+v", h.spawned)
	}
	if _, err := os.Stat(h.file); !os.IsNotExist(err) {
		t.Errorf("cwd 側のファイルを作ってはいけない")
	}
}
