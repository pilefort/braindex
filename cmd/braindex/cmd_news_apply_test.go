package main

import (
	"bytes"
	"errors"
	"github.com/pilefort/braindex/internal/news"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fetch → HTML の選別 JSON(手で置く) → apply → keep と統計 → 次の fetch のプロファイルと脚注に効く、の一巡。
func TestNewsApply_EndToEnd(t *testing.T) {
	hub, _ := newsHub(t)
	inbox := filepath.Join(hub, "downloads")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}

	// 1 回目の fetch(採点なし)
	if code, _, _ := newsFetch(t, hub, "-layer", "daily", "-inbox", inbox); code != 2 { // C の 404 で 2
		t.Fatalf("fetch 1: exit=%d", code)
	}
	// ブラウザで「残す」にした体で選別 JSON を置く(記事1 を残す・記事2 は不要)
	sel := `{"type": "braindex-news-selection", "date": "2026-08-15", "layer": "daily", "exported_at": "2026-08-15T10:00:00Z",
	 "keeps": [{"id": "` + feedIDOf("https://example.com/1") + `", "title": "記事1", "link": "https://example.com/1", "feed": "A", "category": "tech", "score": "", "rescued": false}],
	 "feed_stats": {"A": {"shown": 2, "kept": 1, "dropped": 1, "hidden": 0, "rescued": 0}}}`
	writeFile(t, filepath.Join(inbox, "braindex-news-selection_2026-08-15_daily_20260815100000.json"), sel)

	// apply
	var so, se bytes.Buffer
	code := dispatch([]string{"news", "apply", "-config", filepath.Join(hub, "braindex.json"), "-inbox", inbox}, &so, &se)
	if code != 0 {
		t.Fatalf("apply exit=%d\n%s", code, se.String())
	}
	mustContain(t, "apply stdout", so.String(), "news: 取り込み: braindex-news-selection_2026-08-15_daily_20260815100000.json（残す 1 件）", "news apply 完了")
	keep := readFile(t, filepath.Join(hub, "news", "keep", "2026-08.md"))
	mustContain(t, "keep", keep, "# 選別済みニュース 2026-08", "## 2026-08-15（daily）", "- [記事1](https://example.com/1) — A")
	if _, err := os.Stat(filepath.Join(hub, "news", ".stats.json")); err != nil {
		t.Error("統計ファイルが無い")
	}
	if _, err := os.Stat(filepath.Join(hub, "news", ".ingested", "braindex-news-selection_2026-08-15_daily_20260815100000.json")); err != nil {
		t.Error("取り込み済みへ移っていない")
	}
	if m, _ := filepath.Glob(filepath.Join(inbox, "*.json")); len(m) != 0 {
		t.Errorf("inbox に残った: %v", m)
	}

	// 2 回目の fetch(採点あり・-replay): keep の見出し「記事1」から語「記事」が関心語になり(数字は語にならない)、
	// 記事1・記事2 とも主要(★2)。脚注に累積
	so.Reset()
	se.Reset()
	code = dispatch([]string{"news", "fetch", "-config", filepath.Join(hub, "braindex.json"), "-date", "2026-08-16", "-layer", "daily",
		"-replay", "-no-open", "-inbox", inbox, "-sessions", filepath.Join(hub, "no-such-dir")}, &so, &se)
	if code != 2 {
		t.Fatalf("fetch 2: exit=%d\n%s%s", code, so.String(), se.String())
	}
	md := readFile(t, filepath.Join(hub, "news", "digest_2026-08-16_daily.md"))
	mustContain(t, "digest 2", md, "・関心度 2 以上を主要表示", "## A（tech・新着 2 件・主要 2 件）", "[記事1](https://example.com/1) ★2（記事）", "[記事2](https://example.com/2) ★2（記事）")
	h := readFile(t, filepath.Join(hub, "news", "digest_2026-08-16_daily.html"))
	mustContain(t, "html 2", h, "<b>選別の反映状況（累積）:</b><br>A: 残す 1 / 見た 2")
	if strings.Contains(h, "間引き候補") {
		t.Error("見た数が足りないのに間引き候補が出た")
	}

	// apply の誤り: 引数・設定なし・-h
	if code := dispatch([]string{"news", "apply", "x"}, &so, &se); code != 1 {
		t.Errorf("引数: exit=%d", code)
	}
	se.Reset()
	if code := dispatch([]string{"news", "apply", "-config", filepath.Join(hub, "nope.json")}, &so, &se); code != 1 || !strings.Contains(se.String(), "設定ファイルが無い") {
		t.Errorf("config: exit=%d %s", code, se.String())
	}
	se.Reset()
	if code := dispatch([]string{"news", "apply", "-h"}, &so, &se); code != 0 || !strings.Contains(se.String(), "使い方: braindex news apply") {
		t.Errorf("-h: exit=%d", code)
	}
}

// -inbox を指定しないと既定の ~/Downloads から取り込む。テストが実ユーザーのホームを触っていないことも同時に見る
// (newsHub がホームを一時ディレクトリに差し替える。差し替えが外れたらここで止まる)。
func TestNewsFetch_DefaultInbox(t *testing.T) {
	hub, _ := newsHub(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(home, os.TempDir()) {
		t.Fatalf("ホームが一時ディレクトリの外を指している(実ユーザーの Downloads を触る): %s", home)
	}
	sel := filepath.Join(home, "Downloads", "braindex-news-selection_2026-08-15_weekly_20260815100000.json")
	writeFile(t, sel, `{"type": "braindex-news-selection", "date": "2026-08-15", "layer": "weekly",
	 "keeps": [{"id": "z", "title": "よその見出し", "link": "https://elsewhere/1", "feed": "Z"}],
	 "feed_stats": {"Z": {"shown": 1, "kept": 1}}}`)

	code, so, se := newsFetch(t, hub, "-layer", "weekly")
	if code != 0 {
		t.Fatalf("exit=%d\n%s%s", code, so, se)
	}
	mustContain(t, "stdout", so, "news: 取り込み: braindex-news-selection_2026-08-15_weekly_20260815100000.json（残す 1 件）")
	if _, err := os.Stat(sel); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("取り込んだのに Downloads に残っている: %v", err)
	}
	mustContain(t, "keep", readFile(t, filepath.Join(hub, "news", "keep", "2026-08.md")), "- [よその見出し](https://elsewhere/1) — Z")
}
func TestNewsApplyAddFeeds(t *testing.T) {
	hub, _ := newsHub(t)
	e := news.Catalog()[0]
	q := "compiler"
	writeFile(t, filepath.Join(hub, "news", "inbox", news.SelectionPrefix+"feeds.json"), `{"type":"braindex-news-selection","date":"2026-09-13","layer":"daily","exported_at":"2026-09-13T10:00:00Z","keeps":[],"feed_stats":{},"reading":[],"library":false,"add_feeds":[{"url":"`+e.URL+`"},{"url":"`+news.SearchFeedURL(q)+`","query":"`+q+`"}]}`)
	var so, se bytes.Buffer
	code := dispatch([]string{"news", "apply", "-config", filepath.Join(hub, "braindex.json")}, &so, &se)
	if code != 0 {
		t.Fatalf("exit=%d %s", code, se.String())
	}
	ss, err := news.LoadFeeds(filepath.Join(hub, "news", "feeds.json"))
	if err != nil || len(ss) != 5 {
		t.Fatalf("feeds=%v %v", ss, err)
	}
	mustContain(t, "stdout", so.String(), "取材先 2 本を feeds.json に足した", "残す 0 件・取材先 2 本を追加")
}
