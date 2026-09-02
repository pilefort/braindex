package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/verify"
)

type fakeFetcher map[string]*verify.Response

func (f fakeFetcher) Get(url string) (*verify.Response, error) {
	if r, ok := f[url]; ok {
		return r, nil
	}
	return nil, errors.New("dial: 接続できない")
}

func stubVerify(t *testing.T, f fakeFetcher) {
	t.Helper()
	orig := newFetcher
	newFetcher = func() verify.Fetcher { return f }
	t.Cleanup(func() { newFetcher = orig })
}

const quotePage = "<p>Every claim carries a quote, checked against the source.</p>"

func fixture() fakeFetcher {
	return fakeFetcher{
		"https://api.github.com/repos/a/b":    {Status: 200, Body: `{"full_name":"a/b","stargazers_count":5,"created_at":"2026-01-01T00:00:00Z"}`},
		"https://api.github.com/repos/a/none": {Status: 404, Body: `{"message":"Not Found"}`},
		"https://arxiv.org/abs/1":             {Status: 200, Body: "<title>[1] T</title>"},
		"https://ok.example/":                 {Status: 200, FinalURL: "https://ok.example/", Body: quotePage},
	}
}

// 全件 FOUND は 0。1 件 1 行のタブ区切りで実測を出す。
func TestVerify_AllFound(t *testing.T) {
	stubVerify(t, fixture())
	var so, se bytes.Buffer
	if code := dispatch([]string{"verify", "github", "a/b"}, &so, &se); code != 0 {
		t.Fatalf("exit=%d want 0\n%s", code, se.String())
	}
	if so.String() != "github\ta/b\tFOUND\ta/b stars=5 created=2026-01-01T00:00:00Z\n" {
		t.Errorf("出力が違う: %q", so.String())
	}
	so.Reset()
	if code := dispatch([]string{"verify", "arxiv", "1"}, &so, &se); code != 0 || !strings.Contains(so.String(), "FOUND\t[1] T") {
		t.Errorf("arxiv: exit=%d out=%q", code, so.String())
	}
	so.Reset()
	if code := dispatch([]string{"verify", "url", "https://ok.example/"}, &so, &se); code != 0 || !strings.Contains(so.String(), "FOUND\tHTTP 200") {
		t.Errorf("url: exit=%d out=%q", code, so.String())
	}
}

// NOT FOUND が混じれば 2。ERROR が混じれば 1(NOT FOUND より優先)。
func TestVerify_ExitCodes(t *testing.T) {
	stubVerify(t, fixture())
	var so, se bytes.Buffer
	if code := dispatch([]string{"verify", "github", "a/b", "a/none"}, &so, &se); code != 2 {
		t.Errorf("NOT FOUND あり: exit=%d want 2\n%s", code, so.String())
	}
	if !strings.Contains(so.String(), "github\ta/none\tNOT FOUND\tHTTP 404") {
		t.Errorf("NOT FOUND の行が無い: %q", so.String())
	}
	so.Reset()
	if code := dispatch([]string{"verify", "github", "a/none", "a/down"}, &so, &se); code != 1 {
		t.Errorf("ERROR あり: exit=%d want 1\n%s", code, so.String())
	}
}

// quote は出典 URL を 1 回取り、各引用を照合する。24 字未満は ERROR。
func TestVerify_Quote(t *testing.T) {
	stubVerify(t, fixture())
	var so, se bytes.Buffer
	code := dispatch([]string{"verify", "quote", "https://ok.example/",
		"Every claim carries a quote, checked against the source.",
		"Each claim comes with a quote that is checked against its source."}, &so, &se)
	if code != 2 {
		t.Fatalf("exit=%d want 2\n%s", code, so.String())
	}
	lines := strings.Split(strings.TrimSpace(so.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "quote\tEvery claim") || !strings.Contains(lines[0], "\tFOUND\t") ||
		!strings.Contains(lines[1], "\tNOT FOUND\t") {
		t.Errorf("出力が違う: %q", so.String())
	}
	if code := dispatch([]string{"verify", "quote", "https://ok.example/", "short"}, &so, &se); code != 1 {
		t.Errorf("短い引用は ERROR → exit 1 のはず: %d", code)
	}
	if code := dispatch([]string{"verify", "quote", "https://ok.example/"}, &so, &se); code != 1 {
		t.Errorf("引用なしは exit 1 のはず: %d", code)
	}
}

// -json は配列。HTML エスケープしない(URL の & をそのまま)。
func TestVerify_JSON(t *testing.T) {
	f := fixture()
	f["https://ok.example/?a=1&b=2"] = &verify.Response{Status: 200, FinalURL: "https://ok.example/?a=1&b=2"}
	stubVerify(t, f)
	var so, se bytes.Buffer
	if code := dispatch([]string{"verify", "-json", "url", "https://ok.example/?a=1&b=2"}, &so, &se); code != 0 {
		t.Fatalf("exit=%d\n%s", code, se.String())
	}
	var got []verify.Result
	if err := json.Unmarshal(so.Bytes(), &got); err != nil {
		t.Fatalf("JSON でない: %v\n%s", err, so.String())
	}
	if len(got) != 1 || got[0].Kind != "url" || got[0].Status != verify.Found || got[0].Target != "https://ok.example/?a=1&b=2" {
		t.Errorf("JSON の中身が違う: %+v", got)
	}
	if strings.Contains(so.String(), `&`) || !strings.Contains(so.String(), `a=1&b=2`) {
		t.Error("& が HTML エスケープされている")
	}
}

// 種別なし・未知の種別・対象なしは 1。
func TestVerify_Usage(t *testing.T) {
	stubVerify(t, fixture())
	var so, se bytes.Buffer
	for _, args := range [][]string{{"verify"}, {"verify", "github"}, {"verify", "wiki", "x"}} {
		if code := dispatch(args, &so, &se); code != 1 {
			t.Errorf("%v: exit=%d want 1", args, code)
		}
	}
}
