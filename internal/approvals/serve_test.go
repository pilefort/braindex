package approvals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// startServe は Serve を裏で起動し、URL と結果チャネルを返す。
func startServe(t *testing.T, o ServeOptions) (url string, done <-chan serveResult) {
	t.Helper()
	ready := make(chan string, 1)
	o.OnReady = func(u string) { ready <- u }
	ch := make(chan serveResult, 1)
	go func() {
		r, err := Serve(context.Background(), o)
		ch <- serveResult{r, err}
	}()
	select {
	case url = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("サーバが起動しない")
	}
	return url, ch
}

type serveResult struct {
	reply Reply
	err   error
}

func post(t *testing.T, url, origin, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", url+"reply", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestServe_RoundTrip(t *testing.T) {
	html := []byte("<!doctype html><title>t</title>")
	url, done := startServe(t, ServeOptions{HTML: html, Nonce: "n1", Timeout: 5 * time.Second})
	if !strings.HasPrefix(url, "http://127.0.0.1:") || !strings.HasSuffix(url, "/") {
		t.Fatalf("url = %q", url)
	}

	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !bytes.Equal(got, html) || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
		t.Errorf("GET / = %d %q %q", res.StatusCode, res.Header.Get("Content-Type"), got)
	}
	if res, err := http.Get(url + "other"); err != nil || res.StatusCode != 404 {
		t.Errorf("GET /other = %v %v", res, err)
	}

	// nonce 違い・Origin 違い・GET は拒否し、サーバは待ち続ける
	if code, body := post(t, url, url[:len(url)-1], `{"nonce":"bad","items":[{"n":1,"choice":"A"}]}`); code != 403 || !strings.Contains(body, "error") {
		t.Errorf("nonce 違い = %d %s", code, body)
	}
	if code, _ := post(t, url, "http://evil.example", `{"nonce":"n1","items":[{"n":1,"choice":"A"}]}`); code != 403 {
		t.Errorf("Origin 違い = %d", code)
	}
	if code, _ := post(t, url, url[:len(url)-1], `{"nonce":"n1","items":[]}`); code != 400 {
		t.Errorf("項目なし = %d", code)
	}
	if res, _ := http.Get(url + "reply"); res.StatusCode != 405 {
		t.Errorf("GET /reply = %d", res.StatusCode)
	}
	select {
	case r := <-done:
		t.Fatalf("拒否した POST で終わった: %+v", r)
	case <-time.After(100 * time.Millisecond):
	}

	code, body := post(t, url, url[:len(url)-1], `{"nonce":"n1","items":[{"n":1,"title":"題","choice":"A","comment":"c"},{"n":2,"choice":"hold","comment":""}]}`)
	if code != 200 || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("正しい POST = %d %s", code, body)
	}
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatal(r.err)
		}
		if len(r.reply.Items) != 2 || r.reply.Items[0].Title != "題" || r.reply.Items[0].Choice != "A" || r.reply.Items[0].Comment != "c" || r.reply.Items[1].Choice != "hold" {
			t.Errorf("reply = %+v", r.reply)
		}
		if r.reply.Nonce != "n1" || r.reply.ReceivedAt == "" {
			t.Errorf("reply meta = %+v", r.reply)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("回答後に Serve が終わらない")
	}
	// 終了後は接続できない
	if _, err := http.Get(url); err == nil {
		t.Error("終了後も応答する")
	}
}

func TestServe_Timeout(t *testing.T) {
	_, done := startServe(t, ServeOptions{HTML: []byte("x"), Nonce: "n", Timeout: 100 * time.Millisecond})
	select {
	case r := <-done:
		if !errors.Is(r.err, ErrTimeout) {
			t.Errorf("err = %v, want ErrTimeout", r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout で終わらない")
	}
}

func TestServe_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	ch := make(chan error, 1)
	go func() {
		_, err := Serve(ctx, ServeOptions{HTML: []byte("x"), Nonce: "n", OnReady: func(u string) { ready <- u }})
		ch <- err
	}()
	<-ready
	cancel()
	select {
	case err := <-ch:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel で終わらない")
	}
}

func TestReplyJSON(t *testing.T) {
	r := Reply{Nonce: "n", ReceivedAt: "2026-01-02T03:04:05+09:00", Items: []ReplyItem{{N: 1, Title: "t", Choice: "A", Comment: "c"}}}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"nonce":"n","received_at":"2026-01-02T03:04:05+09:00","items":[{"n":1,"title":"t","choice":"A","comment":"c"}]}`
	if string(b) != want {
		t.Errorf("json = %s", b)
	}
}

func TestNewNonce(t *testing.T) {
	a, b := NewNonce(), NewNonce()
	if len(a) != 32 || a == b {
		t.Errorf("nonce = %q %q", a, b)
	}
}
