package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/changehistory"
)

// 本文の変更の記録は、索引と同じディレクトリの changes.json に書く。
// 初回は全件を観測日不明で記録し、本文だけを直して再生成すると索引はバイト一致のまま記録の観測日だけが動く。
// 消せば「見当たらない」、同じ内容で戻せば「再出現」。
func TestRun_本文の変更を索引の隣に記録する(t *testing.T) {
	root := makeRoot(t)
	note := filepath.Join(root, "repo-a", "docs", "notes", "a.md")
	out := filepath.Join(t.TempDir(), "index", "catalog.md")
	hist := filepath.Join(filepath.Dir(out), changehistory.FileName)

	// 1 回目: 初回
	so, _ := runOK(t, options{root: root, out: out, date: "2026-01-03"})
	if !strings.Contains(so, "本文の観測を開始: 1 件を記録(いつ変わったかは不明)→ "+hist) {
		t.Errorf("初回の 1 行が無い: %s", so)
	}
	h, err := changehistory.Load(hist)
	if err != nil || len(h.Notes) != 1 || h.Notes[0].Path != "repo-a/docs/notes/a.md" || h.Notes[0].Observed != "" {
		t.Fatalf("初回の記録: %+v %v", h, err)
	}
	catalog1, _ := os.ReadFile(out)

	// 2 回目: 変更なし。索引も記録もバイト一致
	hist1, _ := os.ReadFile(hist)
	so, _ = runOK(t, options{root: root, out: out, date: "2026-01-04"})
	if !strings.Contains(so, "本文の変更: なし → "+hist) {
		t.Errorf("変更なしの 1 行が無い: %s", so)
	}
	if b, _ := os.ReadFile(hist); !bytes.Equal(b, hist1) {
		t.Errorf("変更が無いのに記録が変わった:\n%s", b)
	}

	// 3 回目: 要旨より後ろの本文だけを直す。索引は同じ(生成日以外)で、記録の観測日が生成日になる
	writeFile(t, note, "# A\n\n結論: a\n記録日: 2026-01-02\n\n本文を書き足した\n")
	so, _ = runOK(t, options{root: root, out: out, date: "2026-01-05"})
	if !strings.Contains(so, "本文の変更: 変更 1・新規 0・見当たらない 0 → "+hist) {
		t.Errorf("変更の 1 行が無い: %s", so)
	}
	catalog3, _ := os.ReadFile(out)
	if strings.ReplaceAll(string(catalog3), "2026-01-05", "2026-01-03") != string(catalog1) {
		t.Errorf("本文だけの変更で索引が変わった:\n%s", catalog3)
	}
	h, _ = changehistory.Load(hist)
	if h.Notes[0].Observed != "2026-01-05" {
		t.Errorf("観測日が生成日でない: %+v", h.Notes)
	}
	hash3 := h.Notes[0].Hash

	// 4 回目: 消す
	if err := os.Remove(note); err != nil {
		t.Fatal(err)
	}
	so, _ = runOK(t, options{root: root, out: out, date: "2026-01-06"})
	if !strings.Contains(so, "本文の変更: 変更 0・新規 0・見当たらない 1 → ") {
		t.Errorf("見当たらないの 1 行が無い: %s", so)
	}
	h, _ = changehistory.Load(hist)
	if len(h.Notes) != 1 || h.Notes[0].Missing != "2026-01-06" || h.Notes[0].Hash != hash3 {
		t.Errorf("見当たらない記録: %+v", h.Notes)
	}

	// 5 回目: 同じ内容で戻す。観測日は動かない
	writeFile(t, note, "# A\n\n結論: a\n記録日: 2026-01-02\n\n本文を書き足した\n")
	so, _ = runOK(t, options{root: root, out: out, date: "2026-01-07"})
	if !strings.Contains(so, "本文の変更: 変更 0・新規 0・見当たらない 0・再出現 1 → ") {
		t.Errorf("再出現の 1 行が無い: %s", so)
	}
	h, _ = changehistory.Load(hist)
	if h.Notes[0].Missing != "" || h.Notes[0].Observed != "2026-01-05" {
		t.Errorf("再出現の記録: %+v", h.Notes)
	}
}

// 壊れた記録は警告して据え置き(索引は書く・終了コード 2)。黙って作り直して前回の観測を失わない。
func TestRun_壊れた記録は警告して据え置く(t *testing.T) {
	root := makeRoot(t)
	out := filepath.Join(t.TempDir(), "index", "catalog.md")
	hist := filepath.Join(filepath.Dir(out), changehistory.FileName)
	writeFile(t, hist, "{broken")
	var so, se bytes.Buffer
	if code := run(options{root: root, out: out, date: "2026-01-03"}, &so, &se); code != 2 {
		t.Errorf("exit=%d want 2\nstderr=%s", code, se.String())
	}
	if !strings.Contains(se.String(), "本文の変更の記録を読めない") || !strings.Contains(se.String(), hist) {
		t.Errorf("stderr に記録のパスと理由が無い: %s", se.String())
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("索引は書くべき: %v", err)
	}
	if b, _ := os.ReadFile(hist); string(b) != "{broken" {
		t.Errorf("壊れた記録を上書きした: %s", b)
	}
	if strings.Contains(so.String(), "本文の") {
		t.Errorf("記録を更新していないのに結果の行が出ている: %s", so.String())
	}
}

// 同じ root・同じ生成日なら、別の hub で作っても・同じ hub で 2 回作っても、索引と記録はバイト一致する。
func TestRun_記録も生成日を固定すればバイト一致(t *testing.T) {
	root := makeRoot(t)
	read := func(hub string) (string, string) {
		out := filepath.Join(hub, "index", "catalog.md")
		runOK(t, options{root: root, out: out, date: "2026-01-03"})
		c, _ := os.ReadFile(out)
		h, _ := os.ReadFile(filepath.Join(hub, "index", changehistory.FileName))
		return string(c), string(h)
	}
	hubA, hubB := t.TempDir(), t.TempDir()
	ca, ha := read(hubA)
	cb, hb := read(hubB)
	ca2, ha2 := read(hubA)
	if ca != cb || ca != ca2 {
		t.Errorf("索引がバイト不一致")
	}
	if ha != hb || ha != ha2 || ha == "" {
		t.Errorf("記録がバイト不一致:\n%s\n---\n%s\n---\n%s", ha, hb, ha2)
	}
}
