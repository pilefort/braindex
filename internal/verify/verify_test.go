package verify

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeFetcher は URL → 固定レスポンス。無い URL はエラー(ネットワークに出ない)。
type fakeFetcher map[string]*Response

func (f fakeFetcher) Get(url string) (*Response, error) {
	if r, ok := f[url]; ok {
		return r, nil
	}
	return nil, errors.New("dial: 接続できない")
}

// 以下の固定入力は原型 test_verify.py(2026-08-16)のケースを写した。

func TestParseGitHub(t *testing.T) {
	g, err := ParseGitHub(`{"full_name":"a/b","stargazers_count":123,"created_at":"2026-08-13T11:56:32Z"}`)
	if err != nil || g.FullName != "a/b" || g.Stars != 123 || g.CreatedAt != "2026-08-13T11:56:32Z" {
		t.Errorf("ParseGitHub = %+v, %v", g, err)
	}
	if _, err := ParseGitHub(`{"message":"Not Found"}`); err == nil || err.Error() != "Not Found" {
		t.Errorf("message を error にするはず: %v", err)
	}
	if _, err := ParseGitHub(`not json`); err == nil {
		t.Error("JSON でない入力は error のはず")
	}
}

func TestParseArxivTitle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"<html><head><title>[2607.24653] Kimi K3: Open Frontier Intelligence</title></head></html>", "[2607.24653] Kimi K3: Open Frontier Intelligence"},
		{"<title>\n  [2608.09802] SWE-Bench ProMax\n</title>", "[2608.09802] SWE-Bench ProMax"},
		{"<TITLE>upper</TITLE>", "upper"},
		{"<html></html>", ""},
	}
	for _, c := range cases {
		if got := ParseArxivTitle(c.in); got != c.want {
			t.Errorf("ParseArxivTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStripHTML(t *testing.T) {
	in := "<html><head><style>p{color:red}</style><script>var x=1;</script></head>" +
		"<body><p>Hello &amp; world</p></body></html>"
	if got := StripHTML(in); got != "Hello & world" {
		t.Errorf("StripHTML = %q", got)
	}
}

// 判定表: 空白の揺れは許す・言い換えは弾く・24 字未満は拒否。
func TestCheckQuoteText(t *testing.T) {
	const page = "<p>Every   claim\n carries a quote, checked against the source.</p>"
	cases := []struct {
		page, quote, want string
	}{
		{page, "Every claim carries a quote, checked against the source.", Found},
		{"<p>Something entirely different lives here on this page.</p>", "Every claim carries a quote, checked against the source.", NotFound},
		{page, "Each claim comes with a quote that is checked against its source.", NotFound},
		{"<p>abc def</p>", "abc def", Error},
		// 文字数はバイトでなく文字で数える(日本語 23 字は拒否・24 字は照合)
		{"<p>" + strings.Repeat("あ", 24) + "</p>", strings.Repeat("あ", 23), Error},
		{"<p>" + strings.Repeat("あ", 24) + "</p>", strings.Repeat("あ", 24), Found},
	}
	for _, c := range cases {
		if got, _ := CheckQuoteText(c.page, c.quote); got != c.want {
			t.Errorf("CheckQuoteText(%q) = %s, want %s", c.quote, got, c.want)
		}
	}
}

// 出典ページの &nbsp; や全角空白も「空白の揺れ」として畳む(原型 Python の \s は Unicode の空白を含む)。
// これを ASCII の空白だけで畳むと、日本語のページで実在する引用が NOT FOUND になる。
func TestCheckQuoteText_UnicodeSpace(t *testing.T) {
	page := "<p>これは&nbsp;テストの文章です。全角空白　を含む長い引用の照合。</p>"
	quote := "これは テストの文章です。全角空白 を含む長い引用の照合。"
	if got, detail := CheckQuoteText(page, quote); got != Found {
		t.Errorf("CheckQuoteText = %s (%s), want %s", got, detail, Found)
	}
	// 引用の側に全角空白・改行が入っていても同じ。
	if got, _ := CheckQuoteText(page, "これは\nテストの文章です。全角空白　を含む長い引用の照合。"); got != Found {
		t.Errorf("引用側の空白の揺れで %s になった", got)
	}
	if got := StripHTML("a b　c"); got != "a b c" {
		t.Errorf("StripHTML = %q, want %q", got, "a b c")
	}
}

// 各照合の判定表(固定レスポンス・ネットワークに出ない)。
func TestChecks(t *testing.T) {
	f := fakeFetcher{
		"https://api.github.com/repos/a/b":    {Status: 200, Body: `{"full_name":"a/b","stargazers_count":5,"created_at":"2026-01-01T00:00:00Z"}`},
		"https://api.github.com/repos/a/none": {Status: 404, Body: `{"message":"Not Found"}`},
		"https://api.github.com/repos/a/rate": {Status: 403, Body: `{"message":"rate limit"}`},
		"https://arxiv.org/abs/1":             {Status: 200, Body: "<title>[1] T</title>"},
		"https://arxiv.org/abs/2":             {Status: 404, Body: ""},
		"https://arxiv.org/abs/3":             {Status: 200, Body: "<html></html>"},
		"https://ok.example/":                 {Status: 200, FinalURL: "https://ok.example/index", Body: "<p>Every claim carries a quote, checked against the source.</p>"},
		"https://gone.example/":               {Status: 410, Body: ""},
	}
	cases := []struct {
		got    Result
		status string
		detail string
	}{
		{GitHub(f, "a/b"), Found, "a/b stars=5 created=2026-01-01T00:00:00Z"},
		{GitHub(f, "a/none"), NotFound, "HTTP 404"},
		{GitHub(f, "a/rate"), Error, "HTTP 403"},
		{GitHub(f, "a/down"), Error, "dial: 接続できない"},
		{Arxiv(f, "1"), Found, "[1] T"},
		{Arxiv(f, "2"), NotFound, "HTTP 404"},
		{Arxiv(f, "3"), NotFound, "要旨ページに <title> が無い"},
		{URL(f, "https://ok.example/"), Found, "HTTP 200 https://ok.example/index"},
		{URL(f, "https://gone.example/"), NotFound, "HTTP 410"},
		{URL(f, "https://down.example/"), Error, "dial: 接続できない"},
	}
	for _, c := range cases {
		if c.got.Status != c.status || c.got.Detail != c.detail {
			t.Errorf("%s %s: got %s / %q, want %s / %q", c.got.Kind, c.got.Target, c.got.Status, c.got.Detail, c.status, c.detail)
		}
	}

	qs := Quotes(f, "https://ok.example/", []string{
		"Every claim carries a quote, checked against the source.",
		"Each claim comes with a quote that is checked against its source.",
		"short",
	})
	if len(qs) != 3 || qs[0].Status != Found || qs[1].Status != NotFound || qs[2].Status != Error {
		t.Errorf("Quotes = %+v", qs)
	}
	if qs := Quotes(f, "https://gone.example/", []string{"x"}); qs[0].Status != Error || qs[0].Detail != "HTTP 410 https://gone.example/" {
		t.Errorf("取得失敗時の quote は Error のはず: %+v", qs[0])
	}
	if qs := Quotes(f, "https://down.example/", []string{"x"}); qs[0].Status != Error {
		t.Errorf("接続失敗時の quote は Error のはず: %+v", qs[0])
	}
}

func TestResultLine(t *testing.T) {
	r := Result{Kind: "url", Target: "https://x/", Status: Found, Detail: "HTTP 200 https://x/"}
	if r.Line() != "url\thttps://x/\tFOUND\tHTTP 200 https://x/" {
		t.Errorf("Line = %q", r.Line())
	}
}

// HTTPFetcher は GET だけを送り、UA を付け、リダイレクト後の最終 URL を返す(ローカルのテストサーバ宛て)。
func TestHTTPFetcher(t *testing.T) {
	var method, ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/r":
			http.Redirect(w, r, "/final", http.StatusFound)
		default:
			method, ua = r.Method, r.Header.Get("User-Agent")
			w.WriteHeader(200)
			_, _ = w.Write([]byte("body"))
		}
	}))
	defer srv.Close()
	res, err := NewHTTPFetcher().Get(srv.URL + "/r")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 200 || res.Body != "body" || res.FinalURL != srv.URL+"/final" {
		t.Errorf("Response = %+v", res)
	}
	if method != http.MethodGet || ua != userAgent {
		t.Errorf("method=%s ua=%s", method, ua)
	}
}
