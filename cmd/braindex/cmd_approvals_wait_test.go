package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/pilefort/braindex/internal/approvals"
)

// waitHarness は approvals wait を回す土台。hook が書く「開いた印」と、
// serve -apply が残す回答 JSON を手で置き、待ち方だけを確かめる。
type waitHarness struct {
	t    *testing.T
	file string
	dir  string
	p    approvals.Paths
}

func newWaitHarness(t *testing.T) *waitHarness {
	t.Helper()
	root := t.TempDir()
	h := &waitHarness{
		t:    t,
		file: filepath.Join(root, "hub", "work", "APPROVALS.md"),
		dir:  filepath.Join(root, "tmp"),
	}
	writeFile(t, h.file, sampleApprovals)
	p, err := approvals.Resolve(h.file, h.dir)
	if err != nil {
		t.Fatal(err)
	}
	h.p = p
	return h
}

// opened は hook がフォームを開いた印を置く。
func (h *waitHarness) opened(at time.Time) {
	h.t.Helper()
	b, err := json.Marshal(hookState{SHA: "0123456789ab", PID: 4242, Opened: at.Format(time.RFC3339)})
	if err != nil {
		h.t.Fatal(err)
	}
	writeFile(h.t, approvalsHookStatePath(h.p), string(b))
}

// answer は回答 JSON を path(p.Reply か p.Applied)に置く。
func (h *waitHarness) answer(path string, at time.Time, choice string) {
	h.t.Helper()
	h.answerWithResult(path, at, choice, nil)
}

// answerWithResult は apply が書き足す反映の結果つきで回答 JSON を置く。
func (h *waitHarness) answerWithResult(path string, at time.Time, choice string, res *approvals.AppliedResult) {
	h.t.Helper()
	rep := approvals.Reply{Nonce: "n", ReceivedAt: at.Format(time.RFC3339),
		Items: []approvals.ReplyItem{{N: 1, Title: "設定ファイルの形式", Choice: choice}}, Result: res}
	b, err := json.Marshal(rep)
	if err != nil {
		h.t.Fatal(err)
	}
	writeFile(h.t, path, string(b))
}

type waitResult struct {
	code           int
	stdout, stderr string
}

// start は wait を別の goroutine で起動する。テストが長引かないよう見に行く間隔を短くする。
func (h *waitHarness) start(args ...string) <-chan waitResult {
	ch := make(chan waitResult, 1)
	argv := append([]string{"approvals", "wait", "-file", h.file, "-dir", h.dir, "-interval", "0.02"}, args...)
	go func() {
		var so, se bytes.Buffer
		code := dispatch(argv, &so, &se)
		ch <- waitResult{code, so.String(), se.String()}
	}()
	return ch
}

func (h *waitHarness) finish(ch <-chan waitResult) waitResult {
	h.t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(10 * time.Second):
		h.t.Fatal("wait が終わらない")
		return waitResult{}
	}
}

func TestApprovalsWait_ReturnsWhenAnswerApplied(t *testing.T) {
	h := newWaitHarness(t)
	h.opened(time.Now())
	ch := h.start("-timeout", "5")
	time.Sleep(100 * time.Millisecond)
	h.answer(h.p.Applied, time.Now(), "A")

	r := h.finish(ch)
	if r.code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", r.code, r.stdout, r.stderr)
	}
	mustContain(t, "stdout", r.stdout, "回答が届いた", "[1] 設定ファイルの形式 → A", h.p.Decisions)
}

// フックが開いてから wait が起動するまでに答えられても拾う(開いた時刻を基準にする理由)。
func TestApprovalsWait_AnswerBeforeWaitStarts(t *testing.T) {
	h := newWaitHarness(t)
	opened := time.Now().Add(-30 * time.Second)
	h.opened(opened)
	h.answer(h.p.Applied, opened.Add(10*time.Second), "B")

	r := h.finish(h.start("-timeout", "5"))
	if r.code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", r.code, r.stdout, r.stderr)
	}
	mustContain(t, "stdout", r.stdout, "→ B")
}

func TestApprovalsWait_IgnoresAnswerBeforeFormOpened(t *testing.T) {
	h := newWaitHarness(t)
	now := time.Now()
	h.answer(h.p.Applied, now.Add(-time.Minute), "A") // 前のフォームの回答
	h.opened(now)

	r := h.finish(h.start("-timeout", "0.3"))
	if r.code != 3 {
		t.Fatalf("前のフォームの回答で終わった: code=%d stdout=%s", r.code, r.stdout)
	}
	mustContain(t, "stderr", r.stderr, "回答なし")
}

func TestApprovalsWait_DoesNotReportSameAnswerTwice(t *testing.T) {
	h := newWaitHarness(t)
	now := time.Now()
	h.opened(now)
	h.answer(h.p.Applied, now, "A")

	if r := h.finish(h.start("-timeout", "5")); r.code != 0 {
		t.Fatalf("1 回目: code=%d stderr=%s", r.code, r.stderr)
	}
	if r := h.finish(h.start("-timeout", "0.3")); r.code != 3 {
		t.Fatalf("同じ回答をもう一度知らせた: code=%d stdout=%s", r.code, r.stdout)
	}
}

func TestApprovalsWait_ReplyLeftUnapplied(t *testing.T) {
	h := newWaitHarness(t)
	now := time.Now()
	h.opened(now)
	h.answer(h.p.Reply, now, "A") // 反映に失敗して reply.json が残った

	r := h.finish(h.start("-timeout", "5"))
	if r.code != 2 {
		t.Fatalf("code=%d stdout=%s stderr=%s", r.code, r.stdout, r.stderr)
	}
	mustContain(t, "stdout", r.stdout, "未反映", "braindex approvals apply")
}

// apply は項目が見つからなくても回答を .applied.json に移す。反映の結果に警告があれば、
// 「反映された」と知らせない(題の食い違い・文字化けで、元の判断待ちが手つかずのまま残る)。
func TestApprovalsWait_ApplyWarningsMeanNotApplied(t *testing.T) {
	h := newWaitHarness(t)
	now := time.Now()
	h.opened(now)
	h.answerWithResult(h.p.Applied, now, "A", &approvals.AppliedResult{
		Warnings: []string{"警告: 回答の項目 [1] 違う題 が APPROVALS.md に見つからない → 未反映"},
	})

	r := h.finish(h.start("-timeout", "5"))
	if r.code != 2 {
		t.Fatalf("code=%d stdout=%s stderr=%s", r.code, r.stdout, r.stderr)
	}
	mustContain(t, "stdout", r.stdout, "未反映", "違う題 が APPROVALS.md に見つからない")
}

func TestApprovalsWait_WithoutHookStateWaitsFromStart(t *testing.T) {
	h := newWaitHarness(t)
	h.answer(h.p.Applied, time.Now().Add(-time.Minute), "A") // 起動前からある回答は拾わない

	ch := h.start("-timeout", "5")
	time.Sleep(100 * time.Millisecond)
	h.answer(h.p.Applied, time.Now(), "B")

	r := h.finish(ch)
	if r.code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", r.code, r.stdout, r.stderr)
	}
	mustContain(t, "stdout", r.stdout, "→ B")
}

func TestApprovalsWait_RejectsBadArgs(t *testing.T) {
	for _, args := range [][]string{{"-timeout", "-1"}, {"-interval", "0"}, {"extra"}} {
		var so, se bytes.Buffer
		if code := dispatch(append([]string{"approvals", "wait"}, args...), &so, &se); code != 1 {
			t.Errorf("%v: code=%d stderr=%s", args, code, se.String())
		}
	}
}
