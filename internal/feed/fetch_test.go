package feed

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// GET で取得してパースする。送るヘッダは User-Agent と Accept だけ。リダイレクトは追う。
func TestFetch_OK(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "atom.xml"))
	if err != nil {
		t.Fatal(err)
	}
	var gotUA, gotMethod, gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/old":
			http.Redirect(w, r, "/feed.atom", http.StatusMovedPermanently)
		case "/feed.atom":
			gotUA, gotMethod, gotCookie = r.Header.Get("User-Agent"), r.Method, r.Header.Get("Cookie")
			w.Header().Set("Content-Type", "application/atom+xml")
			w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d, err := Fetcher{}.Fetch(context.Background(), srv.URL+"/old")
	if err != nil {
		t.Fatal(err)
	}
	if d.Format != "atom" || len(d.Entries) != 2 {
		t.Errorf("format=%s entries=%d", d.Format, len(d.Entries))
	}
	if gotMethod != http.MethodGet || gotUA != DefaultUserAgent || gotCookie != "" {
		t.Errorf("method=%s ua=%q cookie=%q", gotMethod, gotUA, gotCookie)
	}
}

// 4xx/5xx は "HTTP <code>"、フィードでない応答は ErrNotFeed、大きすぎる応答は ErrTooLarge。
func TestFetch_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
		case "/broken":
			w.WriteHeader(http.StatusInternalServerError)
		case "/html":
			w.Write([]byte("<html><body>not a feed</body></html>"))
		case "/huge":
			w.Write([]byte(`<rss version="2.0"><channel><title>` + strings.Repeat("x", 2000) + `</title></channel></rss>`))
		}
	}))
	defer srv.Close()
	f := Fetcher{MaxBytes: 1024}

	if _, err := f.Fetch(context.Background(), srv.URL+"/missing"); err == nil || err.Error() != "HTTP 404" {
		t.Errorf("404: %v", err)
	}
	if _, err := f.Fetch(context.Background(), srv.URL+"/broken"); err == nil || err.Error() != "HTTP 500" {
		t.Errorf("500: %v", err)
	}
	if _, err := f.Fetch(context.Background(), srv.URL+"/html"); !errors.Is(err, ErrNotFeed) {
		t.Errorf("html: %v", err)
	}
	if _, err := f.Fetch(context.Background(), srv.URL+"/huge"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("huge: %v", err)
	}
}

// 接続できない・キャンセル済みの ctx はエラー(パニックや無限待ちにならない)。
func TestFetch_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	if _, err := (Fetcher{}).Fetch(context.Background(), url); err == nil {
		t.Error("閉じたサーバへの取得がエラーにならない")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Fetcher{}).Fetch(ctx, "http://127.0.0.1:1/"); err == nil {
		t.Error("キャンセル済み ctx がエラーにならない")
	}
}
