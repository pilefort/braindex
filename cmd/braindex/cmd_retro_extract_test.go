package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func execRetroExtract(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var so, se bytes.Buffer
	code = dispatch(append([]string{"retro", "extract"}, args...), &so, &se)
	return code, so.String(), se.String()
}

// -out にセッションごとの md と index.tsv を書く。要約は 1 行。架空ログには JSON でない行があるので 2。
func TestRetroExtract_Out(t *testing.T) {
	fixUTC(t)
	out := filepath.Join(t.TempDir(), "retro")
	code, so, se := execRetroExtract(t, "-sessions", retroTestdata, "-out", out)
	if code != 2 {
		t.Fatalf("exit=%d want 2\nstdout=%s\nstderr=%s", code, so, se)
	}
	if want := "braindex retro extract: 3 セッション・発話 6・訂正 1 → " + out + "\n"; so != want {
		t.Errorf("要約:\n want=%q\n  got=%q", want, so)
	}
	idx, err := os.ReadFile(filepath.Join(out, "index.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(idx), "start\tproject\tsession\tuser_turns\tcorrections\tfile\n") || !strings.Contains(string(idx), "2026-08-20 01:00\t/work/repo-a\taaaa1111\t3\t1\tsessions/_work_repo-a/20260820_0100_aaaa1111.md\n") {
		t.Errorf("index.tsv: %q", string(idx))
	}
	md, err := os.ReadFile(filepath.Join(out, "sessions", "_work_repo-a", "20260820_0100_aaaa1111.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(md)
	// 直前のアシスタント本文は、前の発話以降の 2 発話を " / " でつないだ 1 行(時刻は最後の発話)。ツール名と回数を添える
	if !strings.Contains(s, "### [08-20 01:02] #2 USER ★ 訂正候補: 違う, そうじゃな, じゃなくて\n← ASSISTANT [08-20 01:01]: はい、作ります。 / できました。 [tools: Read×2, Bash]\n違う、そうじゃなくて docs だけ\n") {
		t.Errorf("ダイジェストの発話: %q", s)
	}
	// system-reminder の中身は本文に出ない
	if strings.Contains(s, "これは注意書き") {
		t.Errorf("reminder が混じる: %q", s)
	}
	if strings.Contains(s, "\r\n") {
		t.Error("CRLF が混じる")
	}
}

// 既定の出力先は OS の一時ディレクトリの braindex-retro(リポには書かない)。
func TestRetroExtract_DefaultOut(t *testing.T) {
	fixUTC(t)
	code, so, _ := execRetroExtract(t, "-sessions", retroTestdata, "-since", "2026-08-15")
	if code != 2 {
		t.Fatalf("exit=%d want 2\n%s", code, so)
	}
	want := "braindex retro extract: 1 セッション・発話 3・訂正 1 → " + filepath.Join(os.TempDir(), "braindex-retro") + "\n"
	if so != want {
		t.Errorf("要約:\n want=%q\n  got=%q", want, so)
	}
}

// 2 回書いても同じ内容(決定性)。
func TestRetroExtract_Deterministic(t *testing.T) {
	fixUTC(t)
	out1, out2 := filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")
	execRetroExtract(t, "-sessions", retroTestdata, "-out", out1)
	execRetroExtract(t, "-sessions", retroTestdata, "-out", out2)
	for _, rel := range []string{"index.tsv", filepath.Join("sessions", "_work_repo-a", "20260820_0100_aaaa1111.md")} {
		a, err1 := os.ReadFile(filepath.Join(out1, rel))
		b, err2 := os.ReadFile(filepath.Join(out2, rel))
		if err1 != nil || err2 != nil || !bytes.Equal(a, b) {
			t.Errorf("%s: 2 回の出力が一致しない(err=%v/%v)", rel, err1, err2)
		}
	}
}

func TestRetroExtract_Errors(t *testing.T) {
	fixUTC(t)
	cases := [][]string{
		{"-sessions", retroTestdata, "-since", "2026-08-15", "-window-days", "7"},
		{"-sessions", retroTestdata, "-since", "bad"},
		{"-sessions", filepath.Join(t.TempDir(), "no-such-dir")},
		{"-sessions", retroTestdata, "extra-arg"},
	}
	for _, args := range cases {
		code, so, se := execRetroExtract(t, args...)
		if code != 1 {
			t.Errorf("%v: exit=%d want 1\nstdout=%s\nstderr=%s", args, code, so, se)
		}
		if !strings.Contains(se, "braindex retro extract") {
			t.Errorf("%v: stderr にコマンド名が無い: %q", args, se)
		}
	}
	var so, se bytes.Buffer
	if code := dispatch([]string{"retro", "extract", "-h"}, &so, &se); code != 0 || !strings.Contains(se.String(), "一時ディレクトリ") {
		t.Errorf("-h: exit=%d stderr=%q", code, se.String())
	}
}
