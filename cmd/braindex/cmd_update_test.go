package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// initHub は雛形を展開した hub を返す(update の出発点)。
func initHub(t *testing.T) string {
	t.Helper()
	hub := filepath.Join(t.TempDir(), "hub")
	var so, se bytes.Buffer
	if code := dispatch([]string{"init", "-add", "all", hub}, &so, &se); code != 0 {
		t.Fatalf("init exit=%d\n%s", code, se.String())
	}
	return hub
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("読めない %s: %v", p, err)
	}
	return string(b)
}

// 足りないファイルは作り、利用者が編集したファイルは残して .new を隣に置く。索引も再生成する。
func TestUpdate_CatchesUpHub(t *testing.T) {
	hub := initHub(t)
	conventions := filepath.Join(hub, "docs", "conventions.md")
	readme := filepath.Join(hub, "README.md")
	if err := os.Remove(conventions); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(readme, []byte("私が書き換えた README\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var so, se bytes.Buffer
	code := dispatch([]string{"update", hub}, &so, &se)
	if code != 2 {
		t.Fatalf("exit=%d want 2(要対応あり)\nstdout=%s\nstderr=%s", code, so.String(), se.String())
	}
	if _, err := os.Stat(conventions); err != nil {
		t.Error("消したファイルが作り直されていない")
	}
	if got := read(t, readme); got != "私が書き換えた README\n" {
		t.Error("利用者の編集を壊している")
	}
	if _, err := os.Stat(readme + ".new"); err != nil {
		t.Error("README.md.new が置かれていない")
	}
	if _, err := os.Stat(filepath.Join(hub, "index", "catalog.md")); err != nil {
		t.Error("索引が再生成されていない(決定: 追従と索引の再生成をまとめて行う)")
	}
	if !strings.Contains(so.String(), "作成: docs/conventions.md") {
		t.Errorf("作成の記録が無い:\n%s", so.String())
	}
	if !strings.Contains(so.String(), "README.md.new") {
		t.Errorf(".new の案内が無い:\n%s", so.String())
	}
}

// 何も要対応が無ければ終了コード 0。
func TestUpdate_CleanHub(t *testing.T) {
	hub := initHub(t)
	var so, se bytes.Buffer
	if code := dispatch([]string{"update", hub}, &so, &se); code != 0 {
		t.Fatalf("exit=%d want 0\nstdout=%s\nstderr=%s", code, so.String(), se.String())
	}
}

// -dry-run は何も書かない。
func TestUpdate_DryRun(t *testing.T) {
	hub := initHub(t)
	readme := filepath.Join(hub, "README.md")
	if err := os.WriteFile(readme, []byte("編集\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var so, se bytes.Buffer
	code := dispatch([]string{"update", "-dry-run", hub}, &so, &se)
	if code != 2 {
		t.Fatalf("exit=%d want 2\n%s", code, se.String())
	}
	if _, err := os.Stat(readme + ".new"); err == nil {
		t.Error("-dry-run なのに .new を書いている")
	}
	if !strings.Contains(so.String(), "何も書いていない") {
		t.Errorf("-dry-run の断りが無い:\n%s", so.String())
	}
}

// -force は編集済みも上書きする。
func TestUpdate_Force(t *testing.T) {
	hub := initHub(t)
	readme := filepath.Join(hub, "README.md")
	if err := os.WriteFile(readme, []byte("編集\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var so, se bytes.Buffer
	if code := dispatch([]string{"update", "-force", hub}, &so, &se); code != 0 {
		t.Fatalf("exit=%d want 0\n%s", code, se.String())
	}
	if got := read(t, readme); got == "編集\n" {
		t.Error("-force なのに上書きしていない")
	}
}

// -repo は各プロジェクト側の骨格を追従する。索引は作らない。
func TestUpdate_Repo(t *testing.T) {
	dir := t.TempDir()
	var so, se bytes.Buffer
	if code := dispatch([]string{"update", "-repo", dir}, &so, &se); code != 0 {
		t.Fatalf("exit=%d want 0\n%s", code, se.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "docs", "decisions.md")); err != nil {
		t.Error("repo の骨格が作られていない")
	}
	if _, err := os.Stat(filepath.Join(dir, "index", "catalog.md")); err == nil {
		t.Error("-repo で索引を作っている")
	}
}

// 設定が変わるときは、古い版の braindex が読めなくなることを警告する(決定 2026-09-04)。
func TestUpdate_WarnsOnConfigChange(t *testing.T) {
	hub := initHub(t)
	cfg := filepath.Join(hub, "braindex.json")
	if err := os.WriteFile(cfg, []byte("{\n  \"root\": \"..\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var so, se bytes.Buffer
	dispatch([]string{"update", hub}, &so, &se)
	if !strings.Contains(se.String(), "他のマシン") {
		t.Errorf("版差の警告が無い:\nstderr=%s", se.String())
	}
}

// 存在しないディレクトリを渡したら、hub を丸ごと作らずに失敗する(init と混同しないため)。
func TestUpdate_MissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "typo")
	var so, se bytes.Buffer
	if code := dispatch([]string{"update", dir}, &so, &se); code != 1 {
		t.Errorf("exit=%d want 1\nstdout=%s", code, so.String())
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("存在しないディレクトリを作っている")
	}
	if !strings.Contains(se.String(), "braindex init") {
		t.Errorf("init への案内が無い: stderr=%s", se.String())
	}
}

// ディレクトリは 1 つまで。
func TestUpdate_TooManyArgs(t *testing.T) {
	var so, se bytes.Buffer
	if code := dispatch([]string{"update", "a", "b"}, &so, &se); code != 1 {
		t.Errorf("exit=%d want 1", code)
	}
}

// 段 0 の hub を update しても、足していない機能のファイルは作らない。追従した機能を 1 行出す。
func TestUpdate_FollowsFeaturesOfHub(t *testing.T) {
	hub := filepath.Join(t.TempDir(), "hub")
	var so, se bytes.Buffer
	if code := dispatch([]string{"init", "-add", "retro", hub}, &so, &se); code != 0 {
		t.Fatalf("init exit=%d\n%s", code, se.String())
	}
	if err := os.Remove(filepath.Join(hub, ".claude", "skills", "retro", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	so.Reset()
	se.Reset()
	if code := dispatch([]string{"update", hub}, &so, &se); code != 0 {
		t.Fatalf("exit=%d want 0\nstdout=%s\nstderr=%s", code, so.String(), se.String())
	}
	out := so.String()
	for _, want := range []string{"作成: .claude/skills/retro/SKILL.md", "追従した機能: retro(台帳の記録)"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout に %q が無い:\n%s", want, out)
		}
	}
	for _, p := range []string{"docs", "work", "news"} {
		if _, err := os.Stat(filepath.Join(hub, p)); err == nil {
			t.Errorf("足していない機能の %s を作った", p)
		}
	}
}

// 台帳に機能の記録が無い hub(旧版の init で作ったもの)は、存在するファイルから推定し、その旨を出す。
func TestUpdate_InfersFeaturesAndSays(t *testing.T) {
	hub := filepath.Join(t.TempDir(), "hub")
	var so, se bytes.Buffer
	if code := dispatch([]string{"init", "-add", "all", hub}, &so, &se); code != 0 {
		t.Fatalf("init exit=%d\n%s", code, se.String())
	}
	if err := os.RemoveAll(filepath.Join(hub, ".braindex")); err != nil { // 台帳ごと消す(旧 hub の再現)
		t.Fatal(err)
	}
	so.Reset()
	se.Reset()
	code := dispatch([]string{"update", hub}, &so, &se)
	if code == 1 {
		t.Fatalf("exit=1\nstderr=%s", se.String())
	}
	out := so.String()
	if !strings.Contains(out, "追従した機能: conventions, news, retro, review, schedule(台帳に記録が無いので") {
		t.Errorf("推定の 1 行が無い:\n%s", out)
	}
	if !strings.Contains(out, "追記 0") {
		t.Errorf("設定に足すものは無いはず:\n%s", out)
	}
}
