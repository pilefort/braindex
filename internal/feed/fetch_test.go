package feed

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

// リダイレクトは maxRedirects 回まで。堂々巡りのフィードを無限に追わない。
func TestFetch_RedirectLimit(t *testing.T) {
	var hops atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops.Add(1)
		http.Redirect(w, r, "/next", http.StatusFound)
	}))
	defer srv.Close()

	_, err := Fetcher{}.Fetch(context.Background(), srv.URL+"/start")
	if err == nil || !strings.Contains(err.Error(), "リダイレクトが") {
		t.Fatalf("err=%v(リダイレクト上限のエラーを期待)", err)
	}
	if got := hops.Load(); got != int64(maxRedirects) {
		t.Errorf("要求 %d 回(%d 回で止まるはず)", got, maxRedirects)
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

func TestFetchLinklessIDIncludesFeed(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<rss><channel><item><title>Same</title></item><item><title>Linked</title><link>https://example.com/a</link></item></channel></rss>`))
	}))
	defer s.Close()
	fetch := func(path string) Document {
		d, err := (Fetcher{Client: s.Client()}).Fetch(context.Background(), s.URL+path)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	a, b, again := fetch("/a"), fetch("/b"), fetch("/a")
	if a.Entries[0].ID == b.Entries[0].ID || a.Entries[0].ID != again.Entries[0].ID {
		t.Fatal("linkless identity")
	}
	if a.Entries[1].ID != "2dce0a4c50441bfc" || b.Entries[1].ID != a.Entries[1].ID {
		t.Fatal("linked identity changed")
	}
}
