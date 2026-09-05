package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func execRetroCheck(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var so, se bytes.Buffer
	code = dispatch(append([]string{"retro", "check"}, args...), &so, &se)
	return code, so.String(), se.String()
}

// 窓に aaaa1111 の 3 発話(訂正 1・33.3%)が入り、既定の閾値 8% を超える → 1 行出して 3。
func TestRetroCheck_Over(t *testing.T) {
	fixUTC(t)
	code, so, se := execRetroCheck(t, "-sessions", retroTestdata, "-date", "2026-09-01")
	if code != 3 {
		t.Fatalf("exit=%d want 3\nstdout=%s\nstderr=%s", code, so, se)
	}
	want := "braindex retro check: 直近 14 日の訂正率 33.3%(発話 3・訂正 1)が閾値 8.0% を超えた → レトロスペクティブの時期(braindex retro extract で材料を出す)\n"
	if so != want {
		t.Errorf("stdout:\n want=%q\n  got=%q", want, so)
	}
	// 警告(JSON でない行)があっても、閾値超えの 3 を優先する
	if !strings.Contains(se, "警告") {
		t.Errorf("警告が stderr に出ない: %q", se)
	}
}

// 閾値を上げると超えない → 1 行出して、警告があるので 2。
func TestRetroCheck_Under(t *testing.T) {
	fixUTC(t)
	code, so, _ := execRetroCheck(t, "-sessions", retroTestdata, "-date", "2026-09-01", "-threshold", "0.5")
	if code != 2 {
		t.Fatalf("exit=%d want 2\n%s", code, so)
	}
	if want := "braindex retro check: 直近 14 日の訂正率 33.3%(発話 3・訂正 1)は閾値 50.0% 以下\n"; so != want {
		t.Errorf("stdout:\n want=%q\n  got=%q", want, so)
	}
}

// -quiet は閾値超えのときだけ出力する。終了コードは変わらない。
func TestRetroCheck_Quiet(t *testing.T) {
	fixUTC(t)
	code, so, se := execRetroCheck(t, "-sessions", retroTestdata, "-date", "2026-09-01", "-threshold", "0.5", "-quiet")
	if code != 2 || so != "" || se != "" {
		t.Errorf("quiet で超えない: exit=%d stdout=%q stderr=%q", code, so, se)
	}
	// 超えたときは 1 行だけ。testdata には警告(JSON でない行)があるが、-quiet では stderr にも出さない
	code, so, se = execRetroCheck(t, "-sessions", retroTestdata, "-date", "2026-09-01", "-quiet")
	if code != 3 || !strings.HasPrefix(so, "braindex retro check: 直近 14 日の訂正率 33.3%") || se != "" {
		t.Errorf("quiet で超える: exit=%d stdout=%q stderr=%q", code, so, se)
	}
}

// 設定 retro.threshold が 1 を超えていたら、率が届くことは無いので設定の誤りとして 1(フラグの -threshold と同じ範囲)。
// 範囲の検査は設定の読み込み(retro.Settings.Validate)で行うので、フラグで有効な値を与えても設定の誤りは誤りのまま。
func TestRetroCheck_ConfigThresholdOutOfRange(t *testing.T) {
	fixUTC(t)
	dir := t.TempDir()
	abs, err := filepath.Abs(retroTestdata)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "braindex.json"), `{"retro": {"sessions_dir": `+jsonString(abs)+`, "threshold": 1.5}}`)
	code, so, se := execRetroCheck(t, "-config", filepath.Join(dir, "braindex.json"), "-date", "2026-09-01")
	if code != 1 || so != "" || !strings.Contains(se, "retro.threshold") {
		t.Errorf("exit=%d want 1 stdout=%q stderr=%q", code, so, se)
	}
	// フラグで有効な値を与えても、設定の誤りは読み込み時に止まる
	code, _, se = execRetroCheck(t, "-config", filepath.Join(dir, "braindex.json"), "-date", "2026-09-01", "-threshold", "0.5")
	if code != 1 || !strings.Contains(se, "retro.threshold") {
		t.Errorf("フラグで上書き: exit=%d want 1 stderr=%q", code, se)
	}
}

// 窓に発話が無ければ超えない(率 0)。「訂正率 -」でなく発話が無いことを言う(初めての利用者が最初に見る文言)。
func TestRetroCheck_NoUtterances(t *testing.T) {
	fixUTC(t)
	code, so, _ := execRetroCheck(t, "-sessions", retroTestdata, "-date", "2026-09-01", "-window-days", "1")
	if code != 2 {
		t.Fatalf("exit=%d want 2\n%s", code, so)
	}
	if want := "braindex retro check: 直近 1 日に発話が無い(読んだセッションログ 3 件)。閾値 8.0% の判定は発話が入ってから\n"; so != want {
		t.Errorf("stdout:\n want=%q\n  got=%q", want, so)
	}
}

// 窓と閾値は設定 retro 節から。フラグが優先。窓 30 日には bbbb2222(08-10)の 2 発話も入る(5 発話・訂正 1 = 20.0%)
func TestRetroCheck_Config(t *testing.T) {
	fixUTC(t)
	dir := t.TempDir()
	abs, err := filepath.Abs(retroTestdata)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "braindex.json"), `{"retro": {"sessions_dir": `+jsonString(abs)+`, "window_days": 30, "threshold": 0.5}}`)
	code, so, _ := execRetroCheck(t, "-config", filepath.Join(dir, "braindex.json"), "-date", "2026-09-01")
	if code != 2 || !strings.HasPrefix(so, "braindex retro check: 直近 30 日の訂正率 20.0%(発話 5・訂正 1)は閾値 50.0% 以下") {
		t.Errorf("設定の窓と閾値: exit=%d stdout=%q", code, so)
	}
	code, so, _ = execRetroCheck(t, "-config", filepath.Join(dir, "braindex.json"), "-date", "2026-09-01", "-threshold", "0.15")
	if code != 3 || !strings.Contains(so, "閾値 15.0% を超えた") {
		t.Errorf("フラグの閾値が優先: exit=%d stdout=%q", code, so)
	}
}

func TestRetroCheck_Errors(t *testing.T) {
	fixUTC(t)
	cases := [][]string{
		{"-sessions", retroTestdata, "-threshold", "1.5"},
		{"-sessions", retroTestdata, "-threshold", "0"},
		{"-sessions", retroTestdata, "-window-days", "0"},
		{"-sessions", retroTestdata, "-date", "09-01"},
		{"-sessions", filepath.Join(t.TempDir(), "no-such-dir")},
		{"-sessions", retroTestdata, "extra-arg"},
	}
	for _, args := range cases {
		code, so, se := execRetroCheck(t, args...)
		if code != 1 {
			t.Errorf("%v: exit=%d want 1\nstdout=%s\nstderr=%s", args, code, so, se)
		}
		if !strings.Contains(se, "braindex retro check") {
			t.Errorf("%v: stderr にコマンド名が無い: %q", args, se)
		}
	}
	var so, se bytes.Buffer
	if code := dispatch([]string{"retro", "check", "-h"}, &so, &se); code != 0 || !strings.Contains(se.String(), "終了コード") {
		t.Errorf("-h: exit=%d stderr=%q", code, se.String())
	}
}
