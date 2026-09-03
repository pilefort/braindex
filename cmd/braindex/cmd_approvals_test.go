package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pilefort/braindex/internal/approvals"
)

const sampleApprovals = `# 承認待ち

## 1. 設定ファイルの形式

**決めたいこと:** 形式
**なぜ今決めるか:** 次の PR
**選択肢:**
- A. JSON — 依存なし
- B. TOML — コメント可
**私の案:** A — 依存を増やさない
**決めないとどうなるか:** 止まる
`

// syncBuffer は goroutine から書かれる stdout を読めるようにする。
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestApprovals_Usage(t *testing.T) {
	var so, se bytes.Buffer
	if code := dispatch([]string{"approvals"}, &so, &se); code != 1 || !strings.Contains(se.String(), "serve") {
		t.Errorf("引数なし: code=%d\n%s", code, se.String())
	}
	se.Reset()
	if code := dispatch([]string{"approvals", "-h"}, &so, &se); code != 0 || !strings.Contains(se.String(), "使い方") {
		t.Errorf("-h: code=%d\n%s", code, se.String())
	}
	se.Reset()
	if code := dispatch([]string{"approvals", "help"}, &so, &se); code != 0 || !strings.Contains(se.String(), "使い方") {
		t.Errorf("help: code=%d\n%s", code, se.String())
	}
	se.Reset()
	if code := dispatch([]string{"approvals", "nope"}, &so, &se); code != 1 || !strings.Contains(se.String(), `"nope"`) {
		t.Errorf("不明なサブ: code=%d\n%s", code, se.String())
	}
	se.Reset()
	if code := dispatch([]string{"approvals", "serve", "-file", filepath.Join(t.TempDir(), "none.md")}, &so, &se); code != 1 || !strings.Contains(se.String(), "読めない") {
		t.Errorf("ファイル無し: code=%d\n%s", code, se.String())
	}
}

func TestApprovalsServe_EmptyAndWarnings(t *testing.T) {
	dir := t.TempDir()
	ap := filepath.Join(dir, "work", "APPROVALS.md")
	writeFile(t, ap, "# 承認待ち\n\n（なし）\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"approvals", "serve", "-file", ap, "-no-open"}, &so, &se); code != 0 || !strings.Contains(so.String(), "判断待ちはない") {
		t.Errorf("空: code=%d\n%s%s", code, so.String(), se.String())
	}

	writeFile(t, ap, "# 承認待ち\n\n## 欠けた項目\n\n**決めたいこと:** x\n")
	so.Reset()
	se.Reset()
	code := dispatch([]string{"approvals", "serve", "-file", ap, "-no-open", "-timeout", "0.2", "-dir", filepath.Join(dir, "tmp")}, &so, &se)
	if code != 2 {
		t.Errorf("時間切れ: code=%d\n%s", code, se.String())
	}
	mustContain(t, "stderr", se.String(), "warning: [1] 欠けた項目: なぜ今決めるか が未記載", "選択肢 が 1 つ以下", "回答なし")
}

// -timeout に負・NaN・Duration に収まらない値を渡したら、待ち受けを始めずに誤りとして落ちる
// (素通しすると Timeout <= 0 が「無期限」と解釈され、黙って待ち続ける)。
func TestApprovalsServe_BadTimeout(t *testing.T) {
	dir := t.TempDir()
	ap := filepath.Join(dir, "work", "APPROVALS.md")
	writeFile(t, ap, sampleApprovals)
	for _, v := range []string{"-1", "NaN", "1e30"} {
		var so, se bytes.Buffer
		if code := dispatch([]string{"approvals", "serve", "-file", ap, "-no-open", "-timeout", v}, &so, &se); code != 1 {
			t.Errorf("-timeout %s: code=%d\n%s%s", v, code, so.String(), se.String())
		}
		if !strings.Contains(se.String(), "-timeout") {
			t.Errorf("-timeout %s: stderr に理由が無い: %s", v, se.String())
		}
		if so.Len() != 0 {
			t.Errorf("-timeout %s: 待ち受けを始めた: %s", v, so.String())
		}
	}
}

func TestApprovalsServe_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	ap := filepath.Join(dir, "hub", "work", "APPROVALS.md")
	writeFile(t, ap, sampleApprovals)
	tmp := filepath.Join(dir, "tmp")

	ready := make(chan string, 1)
	approvalsOnReady = func(u string) { ready <- u }
	defer func() { approvalsOnReady = nil }()

	var so syncBuffer
	var se bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- dispatch([]string{"approvals", "serve", "-file", ap, "-no-open", "-dir", tmp, "-timeout", "10"}, &so, &se)
	}()
	var url string
	select {
	case url = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("待ち受けが始まらない")
	}

	// フォームに nonce が埋まっていて、それで POST すると受理される
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	page, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`"nonce":"([0-9a-f]{32})"`).FindStringSubmatch(string(page))
	if m == nil {
		t.Fatalf("フォームに nonce が無い:\n%s", page)
	}
	body := `{"nonce":"` + m[1] + `","items":[{"n":1,"title":"設定ファイルの形式","choice":"B","comment":"コメントが要る"}]}`
	req, err := http.NewRequest("POST", url+"reply", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", strings.TrimSuffix(url, "/"))
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("POST = %d", res.StatusCode)
	}

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("code=%d\n%s", code, se.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("回答後に終わらない")
	}
	out := so.String()
	mustContain(t, "stdout", out, "form: http://127.0.0.1:", "(1 件・id=hub-", "reply: ", "[1] 設定ファイルの形式 → B（コメントが要る）", "next: braindex approvals apply")

	p, err := approvals.Resolve(ap, tmp)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p.Reply)
	if err != nil {
		t.Fatal(err)
	}
	var rep approvals.Reply
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Nonce != m[1] || len(rep.Items) != 1 || rep.Items[0].Choice != "B" || rep.ReceivedAt == "" {
		t.Errorf("reply.json = %+v", rep)
	}

	// 未反映の回答があると note を出す(2 回目は時間切れで終わる)
	se.Reset()
	if code := dispatch([]string{"approvals", "serve", "-file", ap, "-no-open", "-dir", tmp, "-timeout", "0.2"}, &so, &se); code != 2 {
		t.Errorf("2 回目: code=%d", code)
	}
	mustContain(t, "stderr", se.String(), "note: 未反映の回答がある(このまま回答すると上書きする)")
}
