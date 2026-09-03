package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sessions パッケージの架空ログ(4 セッション・人間の発話 6・訂正 1・JSON でない行 1)を使う。
const retroTestdata = "../../internal/sessions/testdata/projects"

// 週と窓の境界はタイムゾーンで決まるので、テストの間は UTC に固定する。
func fixUTC(t *testing.T) {
	t.Helper()
	prev := retroLoc
	retroLoc = time.UTC
	t.Cleanup(func() { retroLoc = prev })
}

func execRetroStats(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var so, se bytes.Buffer
	code = dispatch(append([]string{"retro", "stats"}, args...), &so, &se)
	return code, so.String(), se.String()
}

// 既定はプロジェクト別。架空ログには JSON でない行があるので、警告つきの 2 で終わる。
func TestRetroStats_ByProject(t *testing.T) {
	fixUTC(t)
	code, so, se := execRetroStats(t, "-sessions", retroTestdata)
	if code != 2 {
		t.Fatalf("exit=%d want 2\nstdout=%s\nstderr=%s", code, so, se)
	}
	want := strings.Join([]string{
		"braindex retro stats: 窓 全期間・発話 6・訂正 1・率 16.7%",
		"",
		"| プロジェクト | 発話 | 訂正 | 率 |",
		"|---|---:|---:|---:|",
		"| /work/repo-a | 3 | 1 | 33.3% |",
		"| /work/repo-b | 2 | 0 | 0.0% |",
		"| -work-repo-b | 1 | 0 | 0.0% |",
		"| 合計 | 6 | 1 | 16.7% |",
		"",
	}, "\n")
	if so != want {
		t.Errorf("stdout:\n want=%q\n  got=%q", want, so)
	}
	if !strings.Contains(se, "警告") || !strings.Contains(se, "JSON でない 1 行") {
		t.Errorf("警告が stderr に出ない: %q", se)
	}
	// 本文は出さない
	if strings.Contains(so, "索引を作って") || strings.Contains(se, "索引を作って") {
		t.Error("発話の本文が出力に混じる")
	}
}

func TestRetroStats_SinceAndWeek(t *testing.T) {
	fixUTC(t)
	code, so, _ := execRetroStats(t, "-sessions", retroTestdata, "-since", "2026-08-15", "-by", "week")
	if code != 2 {
		t.Fatalf("exit=%d want 2\n%s", code, so)
	}
	if !strings.HasPrefix(so, "braindex retro stats: 窓 2026-08-15 以降・発話 3・訂正 1・率 33.3%\n") {
		t.Errorf("見出し: %q", so)
	}
	if !strings.Contains(so, "| 週 | 発話 | 訂正 | 率 |\n|---|---:|---:|---:|\n| 2026-W34 | 3 | 1 | 33.3% |\n| 合計 | 3 | 1 | 33.3% |\n") {
		t.Errorf("週別の表: %q", so)
	}
}

func TestRetroStats_WindowDaysAndPosition(t *testing.T) {
	fixUTC(t)
	code, so, _ := execRetroStats(t, "-sessions", retroTestdata, "-date", "2026-09-01", "-window-days", "14", "-by", "position")
	if code != 2 {
		t.Fatalf("exit=%d want 2\n%s", code, so)
	}
	if !strings.HasPrefix(so, "braindex retro stats: 窓 2026-08-18 以降(14 日)・発話 3・訂正 1・率 33.3%\n") {
		t.Errorf("見出し: %q", so)
	}
	if !strings.Contains(so, "| 位置 | 発話 | 訂正 | 率 |\n|---|---:|---:|---:|\n| 1-3 | 3 | 1 | 33.3% |\n| 4-10 | 0 | 0 | - |\n| 11-30 | 0 | 0 | - |\n| 31- | 0 | 0 | - |\n| 合計 | 3 | 1 | 33.3% |\n") {
		t.Errorf("位置別の表: %q", so)
	}
}

func TestRetroStats_MultipleBy(t *testing.T) {
	fixUTC(t)
	_, so, _ := execRetroStats(t, "-sessions", retroTestdata, "-by", "week,project")
	wi, pi := strings.Index(so, "| 週 |"), strings.Index(so, "| プロジェクト |")
	if wi < 0 || pi < 0 || wi > pi {
		t.Errorf("-by の順に表を出す: %q", so)
	}
}

func TestRetroStats_Config(t *testing.T) {
	fixUTC(t)
	dir := t.TempDir()
	abs, err := filepath.Abs(retroTestdata)
	if err != nil {
		t.Fatal(err)
	}
	// 位置の区間・辞書の差し替え(dictionary)と追加(dictionary_extra)は設定から。辞書のパスは設定ファイルのディレクトリ基準
	writeFile(t, filepath.Join(dir, "dict", "mine.txt"), "分かりました\n")
	writeFile(t, filepath.Join(dir, "dict", "extra.txt"), "教えて\n")
	writeFile(t, filepath.Join(dir, "braindex.json"), `{"retro": {"sessions_dir": `+jsonString(abs)+`, "position_bins": "1-2,3-", "dictionary": "dict/mine.txt", "dictionary_extra": "dict/extra.txt"}}`)
	code, so, se := execRetroStats(t, "-config", filepath.Join(dir, "braindex.json"), "-by", "position")
	if code != 2 {
		t.Fatalf("exit=%d want 2\nstdout=%s\nstderr=%s", code, so, se)
	}
	// 既定辞書は使われない(「違う」は当たらない)。mine の「分かりました」と extra の「教えて」が bbbb2222 の 2 発話に当たる
	if !strings.HasPrefix(so, "braindex retro stats: 窓 全期間・発話 6・訂正 2・率 33.3%\n") {
		t.Errorf("見出し: %q", so)
	}
	if !strings.Contains(so, "| 1-2 | 5 | 2 | 40.0% |\n| 3- | 1 | 0 | 0.0% |\n") {
		t.Errorf("設定の区間: %q", so)
	}
}

// 設定の retro 節の範囲外の値(threshold が 0〜1 の外・window_days が負)は、既定値に丸めず 1 で止まる。
func TestRetroStats_ConfigOutOfRange(t *testing.T) {
	fixUTC(t)
	abs, err := filepath.Abs(retroTestdata)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ retro, want string }{
		{`"threshold": 5`, "retro.threshold"},
		{`"threshold": -1`, "retro.threshold"},
		{`"window_days": -3`, "retro.window_days"},
	}
	for _, c := range cases {
		p := filepath.Join(t.TempDir(), "braindex.json")
		writeFile(t, p, `{"retro": {"sessions_dir": `+jsonString(abs)+`, `+c.retro+`}}`)
		code, so, se := execRetroStats(t, "-config", p)
		if code != 1 {
			t.Errorf("%s: exit=%d want 1\nstdout=%s\nstderr=%s", c.retro, code, so, se)
		}
		if !strings.Contains(se, c.want) {
			t.Errorf("%s: stderr にキー名 %q が無い: %q", c.retro, c.want, se)
		}
		if so != "" {
			t.Errorf("%s: 失敗時に stdout へ書かない: %q", c.retro, so)
		}
	}
}

func TestRetroStats_Errors(t *testing.T) {
	fixUTC(t)
	cases := [][]string{
		{"-sessions", retroTestdata, "-by", "foo"},
		{"-sessions", retroTestdata, "-since", "2026/08/15"},
		{"-sessions", retroTestdata, "-since", "2026-08-15", "-window-days", "7"},
		{"-sessions", retroTestdata, "-window-days", "-1"},
		{"-sessions", filepath.Join(t.TempDir(), "no-such-dir")},
		{"-sessions", retroTestdata, "extra-arg"},
		{"-config", filepath.Join(t.TempDir(), "no-such.json")},
	}
	for _, args := range cases {
		code, so, se := execRetroStats(t, args...)
		if code != 1 {
			t.Errorf("%v: exit=%d want 1\nstdout=%s\nstderr=%s", args, code, so, se)
		}
		if !strings.Contains(se, "braindex retro") {
			t.Errorf("%v: stderr にコマンド名が無い: %q", args, se)
		}
	}
	// サブコマンド無し・不明なサブコマンドは 1。-h は 0
	var so, se bytes.Buffer
	if code := dispatch([]string{"retro"}, &so, &se); code != 1 || !strings.Contains(se.String(), "使い方") {
		t.Errorf("retro だけ: exit=%d stderr=%q", code, se.String())
	}
	se.Reset()
	if code := dispatch([]string{"retro", "bogus"}, &so, &se); code != 1 || !strings.Contains(se.String(), "bogus") {
		t.Errorf("不明なサブコマンド: exit=%d stderr=%q", code, se.String())
	}
	se.Reset()
	if code := dispatch([]string{"retro", "-h"}, &so, &se); code != 0 {
		t.Errorf("-h: exit=%d", code)
	}
	if code := dispatch([]string{"retro", "stats", "-h"}, &so, &se); code != 0 {
		t.Errorf("stats -h: exit=%d", code)
	}
}

// 設定ファイルの既定パス(カレントの braindex.json)が無くても動く(retro は hub を要らない)
func TestRetroStats_NoConfig(t *testing.T) {
	fixUTC(t)
	abs, err := filepath.Abs(retroTestdata)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir()) // braindex.json の無いカレントで実行する
	if code, so, se := execRetroStats(t, "-sessions", abs); code != 2 {
		t.Errorf("exit=%d want 2\nstdout=%s\nstderr=%s", code, so, se)
	}
}

// jsonString は s を JSON 文字列リテラルにする(Windows のパスの \ をエスケープする)。
func jsonString(s string) string {
	return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
}
