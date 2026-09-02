package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newsHub は hub と、httptest で配る 2 本のフィード(a: 記事 2 件・b: 記事 1 件)と、壊れた 1 本(c: 404)の feeds.json を作る。
func newsHub(t *testing.T) (hub string, srv *httptest.Server) {
	t.Helper()
	_, hub = hubWithRepo(t)
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a.xml":
			w.Write([]byte(`<rss version="2.0"><channel><title>A</title>
			  <item><title>記事1</title><link>https://example.com/1</link><pubDate>Fri, 14 Aug 2026 10:00:00 GMT</pubDate><description>ゴルーチン の話</description></item>
			  <item><title>記事2</title><link>https://example.com/2</link></item></channel></rss>`))
		case "/b.atom":
			w.Write([]byte(`<feed xmlns="http://www.w3.org/2005/Atom"><title>B</title>
			  <entry><title>記事3</title><link href="https://example.com/3"/></entry></feed>`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	writeFile(t, filepath.Join(hub, "news", "feeds.json"), `[
	  {"name": "A", "url": "`+srv.URL+`/a.xml", "layer": "daily", "category": "tech"},
	  {"name": "B", "url": "`+srv.URL+`/b.atom", "layer": "weekly"},
	  {"name": "C", "url": "`+srv.URL+`/c.xml", "layer": "daily"}
	]`)
	return hub, srv
}

func newsFetch(t *testing.T, hub string, args ...string) (code int, so, se string) {
	t.Helper()
	var sob, seb bytes.Buffer
	// -no-score: 採点の出典(索引・実環境のセッションログ)を読まない。採点は TestNewsFetch_Scored で別に見る
	code = dispatch(append([]string{"news", "fetch", "-config", filepath.Join(hub, "braindex.json"), "-date", "2026-08-15", "-no-score"}, args...), &sob, &seb)
	return code, sob.String(), seb.String()
}

// 取得 → ダイジェスト → 既読。2 回目は新着なし。一部失敗は終了コード 2。
func TestNewsFetch_Flow(t *testing.T) {
	hub, _ := newsHub(t)
	code, so, se := newsFetch(t, hub)
	if code != 2 {
		t.Fatalf("exit=%d\n%s%s", code, so, se)
	}
	mustContain(t, "stdout", so, "A: 新着 2 / 全 2", "B: 新着 1 / 全 1", "news ダイジェスト:")
	mustContain(t, "stderr", se, "警告: C: HTTP 404", "取得失敗 1 本")
	out := filepath.Join(hub, "news", "digest_2026-08-15_all.md")
	want := `# ニュースダイジェスト 2026-08-15（all 層）

新着 3 件（フィード 2 本）・採点なし

## A（tech・新着 2 件）
- 2026-08-14 [記事1](https://example.com/1)
- [記事2](https://example.com/2)

## B（新着 1 件）
- [記事3](https://example.com/3)

## 取得失敗
- C: HTTP 404
`
	if got := readFile(t, out); got != want {
		t.Errorf("digest:\n%s\nwant:\n%s", got, want)
	}
	seen := readFile(t, filepath.Join(hub, "news", ".seen.json"))
	if strings.Count(seen, "2026-08-15") != 3 {
		t.Errorf("既読:\n%s", seen)
	}

	// 2 回目: 新着なし。前のダイジェストは上書きせず -2 を付ける
	code, so, _ = newsFetch(t, hub)
	mustContain(t, "stdout 2 回目", so, "A: 新着 0 / 全 2", "digest_2026-08-15_all-2.md")
	if got := readFile(t, filepath.Join(hub, "news", "digest_2026-08-15_all-2.md")); !strings.Contains(got, "新着 0 件（フィード 2 本）") || strings.Contains(got, "## A") {
		t.Errorf("2 回目:\n%s", got)
	}
	if readFile(t, out) != want {
		t.Error("1 回目のダイジェストが変わった")
	}

	// -replay: 既読を無視して全件、既読ファイルは変えない
	code, so, _ = newsFetch(t, hub, "-replay", "-stdout")
	mustContain(t, "replay", so, "A: 新着 2 / 全 2", "- [記事2](https://example.com/2)")
	if readFile(t, filepath.Join(hub, "news", ".seen.json")) != seen {
		t.Error("replay で既読が変わった")
	}
}

// -layer で絞る。層に無ければエラー。上限は層ごと(daily は 15 が既定なので cap_per_layer で 1 にして確かめる)。
func TestNewsFetch_Layer(t *testing.T) {
	hub, _ := newsHub(t)
	code, so, _ := newsFetch(t, hub, "-layer", "weekly", "-stdout")
	if code != 0 {
		t.Fatalf("exit=%d\n%s", code, so)
	}
	mustContain(t, "weekly", so, "（weekly 層）", "## B（新着 1 件）")
	if strings.Contains(so, "## A") || strings.Contains(so, "取得失敗") {
		t.Errorf("weekly に daily が混じる:\n%s", so)
	}

	code, _, se := newsFetch(t, hub, "-layer", "monthly")
	if code != 1 || !strings.Contains(se, `層 "monthly" のフィードが無い`) || !strings.Contains(se, "[daily weekly]") {
		t.Errorf("無い層: exit=%d %s", code, se)
	}

	writeFile(t, filepath.Join(hub, "braindex.json"), `{"root": "..", "news": {"cap_per_layer": {"daily": 1}}}`)
	_, so, _ = newsFetch(t, hub, "-layer", "daily", "-stdout")
	mustContain(t, "cap", so, "- 2026-08-14 [記事1](https://example.com/1)", "（上限 1 件を超えた 1 件は省略）")
	if strings.Contains(so, "記事2") {
		t.Errorf("上限を超えて書いた:\n%s", so)
	}
}

// 同じ入力(既読なし・-date 固定)から 2 回生成して一致する。
func TestNewsFetch_Deterministic(t *testing.T) {
	hub, _ := newsHub(t)
	_, so1, _ := newsFetch(t, hub, "-replay", "-stdout")
	_, so2, _ := newsFetch(t, hub, "-replay", "-stdout")
	if so1 != so2 {
		t.Errorf("2 回の出力が違う:\n%s\n---\n%s", so1, so2)
	}
}

func TestNewsFetch_Errors(t *testing.T) {
	hub, srv := newsHub(t)

	// 全フィード失敗: 終了コード 1・何も書かない
	writeFile(t, filepath.Join(hub, "news", "feeds.json"), `[{"name": "C", "url": "`+srv.URL+`/c.xml"}]`)
	code, _, se := newsFetch(t, hub)
	if code != 1 || !strings.Contains(se, "全フィードの取得に失敗") {
		t.Errorf("全滅: exit=%d %s", code, se)
	}
	if _, err := os.Stat(filepath.Join(hub, "news", "digest_2026-08-15_all.md")); err == nil {
		t.Error("全滅でもダイジェストを書いた")
	}
	if _, err := os.Stat(filepath.Join(hub, "news", ".seen.json")); err == nil {
		t.Error("全滅でも既読を書いた")
	}

	// feeds.json が無い
	os.Remove(filepath.Join(hub, "news", "feeds.json"))
	if code, _, se := newsFetch(t, hub); code != 1 || !strings.Contains(se, "フィード一覧が無い") {
		t.Errorf("feeds 無し: exit=%d %s", code, se)
	}

	// 設定ファイルが無い・日付の誤り・引数・サブコマンド
	var so, seb bytes.Buffer
	if code := dispatch([]string{"news", "fetch", "-config", filepath.Join(hub, "nope.json")}, &so, &seb); code != 1 || !strings.Contains(seb.String(), "設定ファイルが無い") {
		t.Errorf("config 無し: exit=%d %s", code, seb.String())
	}
	if code, _, se := newsFetch(t, hub, "-date", "2026/08/15"); code != 1 || !strings.Contains(se, "-date は YYYY-MM-DD") {
		t.Errorf("date: exit=%d %s", code, se)
	}
	if code, _, se := newsFetch(t, hub, "extra"); code != 1 || !strings.Contains(se, `引数 ["extra"] は受け付けない`) {
		t.Errorf("引数: exit=%d %s", code, se)
	}
	seb.Reset()
	if code := dispatch([]string{"news"}, &so, &seb); code != 1 || !strings.Contains(seb.String(), "使い方: braindex news") {
		t.Errorf("サブコマンド無し: exit=%d %s", code, seb.String())
	}
	seb.Reset()
	if code := dispatch([]string{"news", "nope"}, &so, &seb); code != 1 || !strings.Contains(seb.String(), `サブコマンド "nope" は無い`) {
		t.Errorf("未知のサブコマンド: exit=%d %s", code, seb.String())
	}
	seb.Reset()
	if code := dispatch([]string{"news", "fetch", "-h"}, &so, &seb); code != 0 || !strings.Contains(seb.String(), "使い方: braindex news fetch") {
		t.Errorf("-h: exit=%d %s", code, seb.String())
	}
}

// 採点あり: 補助ファイルの語で主要と関心外に分かれる。索引とセッションの置き場が無い警告は fetch の警告(終了コード 2)。
func TestNewsFetch_Scored(t *testing.T) {
	hub, _ := newsHub(t)
	writeFile(t, filepath.Join(hub, "news", "interests.md"), "ゴルーチン\n") // 記事1 の概要に当たる
	var so, se bytes.Buffer
	code := dispatch([]string{"news", "fetch", "-config", filepath.Join(hub, "braindex.json"), "-date", "2026-08-15", "-layer", "daily",
		"-stdout", "-sessions", filepath.Join(hub, "no-such-dir")}, &so, &se)
	if code != 2 {
		t.Fatalf("exit=%d\n%s%s", code, so.String(), se.String())
	}
	mustContain(t, "stderr", se.String(), "索引", "が無いので飛ばした", "セッションログの置き場", "警告 3 件(取得失敗 1 本・終了コード 2)")
	want := `# ニュースダイジェスト 2026-08-15（daily 層）

新着 2 件（フィード 1 本）・関心度 2 以上を主要表示

## A（tech・新着 2 件・主要 1 件）
- 2026-08-14 [記事1](https://example.com/1) ★2（ゴルーチン）
- 関心外と判定 1 件:
  - [記事2](https://example.com/2) ★0

## 取得失敗
- C: HTTP 404
`
	if !strings.Contains(so.String(), want) {
		t.Errorf("digest:\n%s\nwant:\n%s", so.String(), want)
	}

	// プロファイルが空なら採点なし
	os.Remove(filepath.Join(hub, "news", "interests.md"))
	so.Reset()
	se.Reset()
	dispatch([]string{"news", "fetch", "-config", filepath.Join(hub, "braindex.json"), "-date", "2026-08-15", "-layer", "daily", "-replay",
		"-stdout", "-sessions", filepath.Join(hub, "no-such-dir")}, &so, &se)
	mustContain(t, "empty profile", so.String(), "関心プロファイルが空なので採点なし", "・採点なし")
}

func TestUnusedPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.md")
	if got, _ := unusedPath(p); got != p {
		t.Errorf("無いとき: %s", got)
	}
	os.WriteFile(p, nil, 0o644)
	if got, _ := unusedPath(p); got != filepath.Join(dir, "d-2.md") {
		t.Errorf("1 つある: %s", got)
	}
	os.WriteFile(filepath.Join(dir, "d-2.md"), nil, 0o644)
	if got, _ := unusedPath(p); got != filepath.Join(dir, "d-3.md") {
		t.Errorf("2 つある: %s", got)
	}
}
