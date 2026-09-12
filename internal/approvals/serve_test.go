package approvals

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// startServe は Serve を裏で起動し、URL と結果チャネルを返す。
func startServe(t *testing.T, o ServeOptions) (url string, done <-chan serveResult) {
	t.Helper()
	return startServeCtx(t, context.Background(), o)
}

// startServeCtx は ctx つきで同じことをする。回答を受けずに終わらせたいテストで使う。
func startServeCtx(t *testing.T, ctx context.Context, o ServeOptions) (url string, done <-chan serveResult) {
	t.Helper()
	ready := make(chan string, 1)
	o.OnReady = func(u string) { ready <- u }
	ch := make(chan serveResult, 1)
	go func() {
		r, err := Serve(ctx, o)
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

// get は GET して本文を読み、Body を閉じる。取得に失敗したらテストを落とす
// (エラーを捨てて res.StatusCode を見ると、失敗時に nil 参照で落ちて理由が分からなくなる)。
func get(t *testing.T, url string) (*http.Response, []byte) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("GET %s の本文: %v", url, err)
	}
	return res, b
}

func post(t *testing.T, url, origin, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest("POST", url+"reply", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
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

	res, got := get(t, url)
	if !bytes.Equal(got, html) || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
		t.Errorf("GET / = %d %q %q", res.StatusCode, res.Header.Get("Content-Type"), got)
	}
	if res, _ := get(t, url+"other"); res.StatusCode != 404 {
		t.Errorf("GET /other = %d", res.StatusCode)
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
	if res, _ := get(t, url+"reply"); res.StatusCode != 405 {
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
	if res, err := http.Get(url); err == nil {
		res.Body.Close()
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
	// フォームの JS とテストが [0-9a-f]{32} で拾うので、16 進以外を返してはいけない
	if _, err := hex.DecodeString(a); err != nil {
		t.Errorf("16 進でない: %q", a)
	}
}

// 回答は「応答を返す前に」ディスクへ書く。後から呼び出し側が書く形だと、書けなかったときに
// ブラウザは成功表示のまま回答だけ消える(ISSUE-approvals-sent-state・2026-09-05)。
func TestServe_SavesReplyBeforeResponding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "approvals-x.reply.json")
	url, done := startServe(t, ServeOptions{HTML: []byte("x"), Nonce: "n1", Timeout: 5 * time.Second, ReplyPath: path})

	code, body := post(t, url, strings.TrimSuffix(url, "/"), `{"nonce":"n1","items":[{"n":1,"title":"題","choice":"A","comment":"c"}]}`)
	if code != 200 {
		t.Fatalf("正しい POST = %d %s", code, body)
	}
	// 応答が返った時点で(Serve の終了を待たずに)置き場にある
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("応答後に回答が書かれていない: %v", err)
	}
	var saved Reply
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatalf("回答 JSON を読めない: %v (%s)", err, b)
	}
	if len(saved.Items) != 1 || saved.Items[0].Choice != "A" || saved.Nonce != "n1" || saved.ReceivedAt == "" {
		t.Errorf("保存された回答 = %+v", saved)
	}
	var res struct {
		OK    bool   `json:"ok"`
		Saved string `json:"saved"`
	}
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatalf("応答 JSON を読めない: %v (%s)", err, body)
	}
	if !res.OK || res.Saved != path {
		t.Errorf(`応答 = %s, want {"ok":true,"saved":%q}`, body, path)
	}
	select {
	case r := <-done:
		if r.err != nil || len(r.reply.Items) != 1 {
			t.Errorf("Serve の戻り値 = %+v %v", r.reply, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("回答後に Serve が終わらない")
	}
}

// 回答 JSON の置き場は OS の共有一時ディレクトリの下(DefaultDir)。同じマシンの他ユーザーから
// 回答を読めないように、ファイルは 0600・新しく作るディレクトリは 0700 に絞る(設計判断 2026-09-12)。
// Windows は POSIX の権限ビットをほぼ持たない(os.Chmod は読み取り専用属性しか触れない)ので検査しない。
func TestWriteReply_権限(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows では POSIX の権限ビットがほぼ効かない")
	}
	dir := filepath.Join(t.TempDir(), "sub")
	path := filepath.Join(dir, "approvals-x.reply.json")
	if err := WriteReply(path, Reply{Nonce: "n0", Items: []ReplyItem{{N: 1, Choice: "A"}}}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("回答 JSON の権限 = %o, want 0600", perm)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("置き場ディレクトリの権限 = %o, want 0700", perm)
	}
}

// 置き換えに失敗しても、前回の回答は壊れない(半端な JSON で上書きしない)。
// 回答 JSON は apply が読むので、途中まで書けたファイルは黙って読めない・取り込めないになる(設計レビュー 2026-09-06 M14)。
func TestWriteReply_置き換えに失敗しても前回の回答は壊れない(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approvals-x.reply.json")
	if err := WriteReply(path, Reply{Nonce: "n0", Items: []ReplyItem{{N: 1, Choice: "A"}}}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	blockReplace(t, path)

	if err := WriteReply(path, Reply{Nonce: "n1", Items: []ReplyItem{{N: 1, Choice: "B"}}}); err == nil {
		t.Fatal("エラーにならない")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("前回の回答が変わった:\n before=%s\n after=%s", before, after)
	}
	des, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, de := range des {
		if strings.HasPrefix(de.Name(), ".") {
			t.Errorf("一時ファイルが残った: %s", de.Name())
		}
	}
}

// blockReplace は path を「原子的には書き換えられない」状態にする。path そのものは書けるので、
// 切り詰めてから書く os.WriteFile は成功して前回の内容を失い、一時ファイル経由の置き換えは失敗して前回の内容が残る。
// Windows: path を開いたままにする(Go の os.Open は FILE_SHARE_DELETE を付けないので、置き換えと削除が失敗する)。
// それ以外: 親ディレクトリの書き込み権限を外す(一時ファイルを作れない。root は権限を無視するので skip)。
func blockReplace(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return
	}
	if os.Getuid() == 0 {
		t.Skip("root は権限を無視するので、書けない置き場を作れない")
	}
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
}

// 書けなかったら 200 を返さない。ブラウザ側は既存の失敗表示(JSON を貼り付ける)に落ちる。
func TestServe_SaveFailure(t *testing.T) {
	notDir := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(notDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	url, done := startServeCtx(t, ctx, ServeOptions{HTML: []byte("x"), Nonce: "n1", ReplyPath: filepath.Join(notDir, "r.json")})

	code, body := post(t, url, strings.TrimSuffix(url, "/"), `{"nonce":"n1","items":[{"n":1,"choice":"A"}]}`)
	if code != 500 || !strings.Contains(body, "error") {
		t.Fatalf("書き込み失敗 = %d %s, want 500", code, body)
	}
	// 受け取ったことにせず待ち続ける(押し直せる)
	select {
	case r := <-done:
		t.Fatalf("書き込み失敗で Serve が終わった: %+v %v", r.reply, r.err)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancel で終わらない")
	}
}
