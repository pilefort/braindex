package main

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pilefort/braindex/internal/approvals"
)

func TestApprovalsStatus(t *testing.T) {
	dir := t.TempDir()
	hub := filepath.Join(dir, "hub")
	ap := filepath.Join(hub, "work", "APPROVALS.md")
	tmp := filepath.Join(dir, "tmp")
	writeFile(t, ap, sampleApprovals)
	p, _ := approvals.Resolve(ap, tmp)

	var so, se bytes.Buffer
	if code := dispatch([]string{"approvals", "status", "-file", ap, "-dir", tmp}, &so, &se); code != 0 {
		t.Fatalf("code=%d\n%s", code, se.String())
	}
	mustContain(t, "stdout", so.String(), "id="+p.ID+" project="+hub, "items=1", "decisions="+p.Decisions+" (なし)", "reply="+p.Reply+" (なし)",
		"[1] 設定ファイルの形式: 選択肢 2・私の案 A")
	if se.Len() != 0 {
		t.Errorf("stderr が空でない: %s", se.String())
	}

	// 未反映の回答があれば 2、記載漏れがあれば 2
	writeFile(t, p.Reply, sampleReply)
	so.Reset()
	if code := dispatch([]string{"approvals", "status", "-file", ap, "-dir", tmp}, &so, &se); code != 2 || !strings.Contains(so.String(), "未反映の回答あり") {
		t.Errorf("回答あり: code=%d\n%s", code, so.String())
	}
	os.Remove(p.Reply)
	writeFile(t, ap, "# 承認待ち\n\n## 欠け\n\n**決めたいこと:** x\n**保留（2026-01-01）:** y\n")
	so.Reset()
	se.Reset()
	if code := dispatch([]string{"approvals", "status", "-file", ap, "-dir", tmp}, &so, &se); code != 2 {
		t.Errorf("記載漏れ: code=%d", code)
	}
	mustContain(t, "stdout", so.String(), "[1] 欠け: 選択肢 0・保留 1")
	mustContain(t, "stderr", se.String(), "warning: [1] 欠け: なぜ今決めるか が未記載")

	se.Reset()
	if code := dispatch([]string{"approvals", "status", "-file", filepath.Join(dir, "none.md")}, &so, &se); code != 1 {
		t.Errorf("ファイル無し: code=%d", code)
	}
}

func TestApprovalsServe_Apply(t *testing.T) {
	dir := t.TempDir()
	hub := filepath.Join(dir, "hub")
	ap := filepath.Join(hub, "work", "APPROVALS.md")
	dec := filepath.Join(hub, "docs", "decisions.md")
	tmp := filepath.Join(dir, "tmp")
	writeFile(t, ap, sampleApprovals)

	ready := make(chan string, 1)
	approvalsOnReady = func(u string) { ready <- u }
	defer func() { approvalsOnReady = nil }()

	var so syncBuffer
	var se bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- dispatch([]string{"approvals", "serve", "-apply", "-file", ap, "-no-open", "-dir", tmp, "-timeout", "10"}, &so, &se)
	}()
	var url string
	select {
	case url = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("待ち受けが始まらない")
	}
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	var page bytes.Buffer
	page.ReadFrom(res.Body)
	res.Body.Close()
	m := regexp.MustCompile(`"nonce":"([0-9a-f]{32})"`).FindStringSubmatch(page.String())
	if m == nil {
		t.Fatal("nonce が無い")
	}
	req, _ := http.NewRequest("POST", url+"reply", strings.NewReader(`{"nonce":"`+m[1]+`","items":[{"n":1,"title":"設定ファイルの形式","choice":"A","comment":""}]}`))
	req.Header.Set("Origin", strings.TrimSuffix(url, "/"))
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != 200 {
		t.Fatalf("POST = %v %v", res, err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("code=%d\n%s", code, se.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("終わらない")
	}
	mustContain(t, "stdout", so.String(), "reply: ", "反映: 決定 1 件 → "+dec, "[1] 設定ファイルの形式 → A. JSON")
	if strings.Contains(so.String(), "next: braindex approvals apply") {
		t.Error("-apply なのに apply を促している")
	}
	mustContain(t, "decisions", readFile(t, dec), "## 設定ファイルの形式 → A. JSON", "理由: 依存なし。私の案の理由: 依存を増やさない。却下: B. TOML（コメント可）")
	if got := readFile(t, ap); !strings.Contains(got, "（なし。") {
		t.Errorf("APPROVALS.md が消し込まれていない:\n%s", got)
	}
	p, _ := approvals.Resolve(ap, tmp)
	if _, err := os.Stat(p.Applied); err != nil {
		t.Error("applied.json が無い")
	}
}
