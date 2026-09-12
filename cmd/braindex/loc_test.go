package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeSessionLogAt は cwd を指定してテスト用のセッションログを 1 本書く。
// turns 件の人間の発話のうち先頭 hits 件を訂正にする。
func writeSessionLogAt(t *testing.T, dir, slug, cwd, id string, day time.Time, turns, hits int) {
	t.Helper()
	var sb strings.Builder
	for i := 0; i < turns; i++ {
		text := "索引を作って"
		if i < hits {
			text = "違う、そこではない"
		}
		ts := day.Add(time.Duration(i) * time.Minute).UTC().Format("2006-01-02T15:04:05.000Z")
		fmt.Fprintf(&sb, `{"type":"user","cwd":%s,"version":"2.1.258","timestamp":%s,"message":{"role":"user","content":%s},"uuid":"u%d"}`+"\n",
			jsonString(cwd), jsonString(ts), jsonString(text), i)
	}
	writeFile(t, filepath.Join(dir, slug, id+".jsonl"), sb.String())
}

// 設定の root が相対パス（hub から見た ".."）でも、その配下で交わしたセッションを数える。
// hub のカレントで既定の braindex.json を使うと設定の置き場が "." になり、sessionRoot が
// ".." を返す。sessions 側の filepath.Rel は絶対パスの cwd と突き合わせて失敗するので、
// 配下のセッションまで「root の外」として全件除外される(Codex レビュー 2026-09-12)。
func TestSessionRoot_相対のrootでも配下のセッションを数える(t *testing.T) {
	fixUTC(t)
	parent := t.TempDir()
	hub := filepath.Join(parent, "hub")
	sessionsDir := t.TempDir()
	writeSessionLogAt(t, sessionsDir, "repo-x", filepath.Join(parent, "repo-x"), "recent01",
		time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC), 100, 20)
	writeFile(t, filepath.Join(hub, "braindex.json"),
		`{"root": "..", "retro": {"sessions_dir": `+jsonString(sessionsDir)+`}}`)
	t.Chdir(hub) // 既定の braindex.json を使う(設定の置き場が "." になる)

	code, so, se := execRetroCheck(t, "-date", "2026-09-01")
	if strings.Contains(se, "の外のセッション") {
		t.Errorf("root の配下なのに除外した:\nstderr=%s", se)
	}
	if !strings.Contains(so, "発話 100") {
		t.Errorf("配下のセッションを数えていない: exit=%d\nstdout=%s\nstderr=%s", code, so, se)
	}
}

// sessionRoot が返す対象ディレクトリは絶対パス。セッションログの cwd は絶対パスなので、
// 相対のまま返すと突き合わせようがない。
func TestSessionRoot_絶対パスで返す(t *testing.T) {
	dir, why := sessionRoot("..", "hub", false, true)
	if why != "" {
		t.Fatalf("why=%q", why)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("相対パスを返した: %q", dir)
	}
}
