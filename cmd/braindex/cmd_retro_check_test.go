package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func execRetroCheck(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var so, se bytes.Buffer
	code = dispatch(append([]string{"retro", "check"}, args...), &so, &se)
	return code, so.String(), se.String()
}

// 窓に aaaa1111 の 3 発話(訂正 1・33.3%)が入り、既定の閾値 8% を超える → 1 行出して 3。
// 基準期間(直前の 8 週)は発話 2 件で材料不足なので、閾値だけで判定する(従来動作)。
func TestRetroCheck_Over(t *testing.T) {
	fixUTC(t)
	code, so, se := execRetroCheck(t, "-sessions", retroTestdata, "-date", "2026-09-01")
	if code != 3 {
		t.Fatalf("exit=%d want 3\nstdout=%s\nstderr=%s", code, so, se)
	}
	want := "braindex retro check: 直近 14 日の訂正率 33.3%(発話 3・訂正 1) / 基準 8 週は材料不足(発話 2・50 未満) / 閾値 8.0% → 閾値を超えた。レトロスペクティブの時期(braindex retro extract で材料を出す)\n"
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
	if want := "braindex retro check: 直近 14 日の訂正率 33.3%(発話 3・訂正 1) / 基準 8 週は材料不足(発話 2・50 未満) / 閾値 50.0% → 閾値以下\n"; so != want {
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
	if code != 2 || !strings.Contains(so, "直近 30 日の訂正率 20.0%(発話 5・訂正 1)") || !strings.Contains(so, "閾値 50.0% → 閾値以下") {
		t.Errorf("設定の窓と閾値: exit=%d stdout=%q", code, so)
	}
	code, so, _ = execRetroCheck(t, "-config", filepath.Join(dir, "braindex.json"), "-date", "2026-09-01", "-threshold", "0.15")
	if code != 3 || !strings.Contains(so, "閾値 15.0% → 閾値を超えた。") {
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

// writeSessionLog はテスト用のセッションログを 1 本書く。turns 件の人間の発話のうち先頭 hits 件を訂正にする。
// 実ログを持ち込まずに基準期間の材料(50 発話以上)を作るために使う。
func writeSessionLog(t *testing.T, dir, id string, day time.Time, turns, hits int) {
	t.Helper()
	var sb strings.Builder
	for i := 0; i < turns; i++ {
		text := "索引を作って"
		if i < hits {
			text = "違う、そこではない"
		}
		ts := day.Add(time.Duration(i) * time.Minute).UTC().Format("2006-01-02T15:04:05.000Z")
		fmt.Fprintf(&sb, `{"type":"user","cwd":"/work/repo-x","version":"2.1.258","timestamp":%s,"message":{"role":"user","content":%s},"uuid":"u%d"}`+"\n",
			jsonString(ts), jsonString(text), i)
	}
	writeFile(t, filepath.Join(dir, "-work-repo-x", id+".jsonl"), sb.String())
}

// 閾値を超えても、基準期間と同水準なら鳴らさない(決定 2026-09-06 → manual/retro.md「決めたこと」)。
// 基準 8%・直近 9% は 2SE(≈5.4pt)の中なので「同水準」、直近 20% は外なので鳴らす。
func TestRetroCheck_基準期間と比べて鳴らすか決める(t *testing.T) {
	fixUTC(t)
	sessionsDir := t.TempDir()
	// 基準期間(窓の直前の 8 週): 2026-08-01 に 500 発話・訂正 40 = 8.0%
	writeSessionLog(t, sessionsDir, "base0001", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), 500, 40)
	// 直近の窓(14 日): 2026-08-25 に 100 発話・訂正 9 = 9.0%
	writeSessionLog(t, sessionsDir, "recent01", time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC), 100, 9)

	code, so, se := execRetroCheck(t, "-sessions", sessionsDir, "-date", "2026-09-01")
	if code != 0 {
		t.Fatalf("exit=%d want 0\nstdout=%s\nstderr=%s", code, so, se)
	}
	want := "braindex retro check: 直近 14 日の訂正率 9.0%(発話 100・訂正 9) / 基準 8.0%(発話 500・8 週) / 閾値 8.0% → 閾値は超えたが基準と同水準(鳴らさない)\n"
	if so != want {
		t.Errorf("同水準:\n want=%q\n  got=%q", want, so)
	}

	// 直近を 20% に差し替えると基準からも上振れて鳴る
	writeSessionLog(t, sessionsDir, "recent01", time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC), 100, 20)
	code, so, se = execRetroCheck(t, "-sessions", sessionsDir, "-date", "2026-09-01")
	if code != 3 {
		t.Fatalf("exit=%d want 3\nstdout=%s\nstderr=%s", code, so, se)
	}
	if !strings.Contains(so, "訂正率 20.0%(発話 100・訂正 20) / 基準 8.0%(発話 500・8 週) / 閾値 8.0% → 閾値を超えた。") {
		t.Errorf("鳴らす: %q", so)
	}
}

// baseline_weeks: 0 は「基準を使わない」。従来どおり閾値だけで判定し、1 行にも基準は出さない。
func TestRetroCheck_基準を使わない設定(t *testing.T) {
	fixUTC(t)
	sessionsDir := t.TempDir()
	writeSessionLog(t, sessionsDir, "base0001", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), 500, 40)
	writeSessionLog(t, sessionsDir, "recent01", time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC), 100, 9)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "braindex.json"), `{"retro": {"sessions_dir": `+jsonString(sessionsDir)+`, "baseline_weeks": 0}}`)

	code, so, se := execRetroCheck(t, "-config", filepath.Join(dir, "braindex.json"), "-date", "2026-09-01")
	if code != 3 {
		t.Fatalf("exit=%d want 3\nstdout=%s\nstderr=%s", code, so, se)
	}
	want := "braindex retro check: 直近 14 日の訂正率 9.0%(発話 100・訂正 9) / 閾値 8.0% → 閾値を超えた。レトロスペクティブの時期(braindex retro extract で材料を出す)\n"
	if so != want {
		t.Errorf("基準なし:\n want=%q\n  got=%q", want, so)
	}
}

// 鳴らしたのに窓の中に所見ノートが無ければ 1 行の末尾に添える。状態ファイルは持たず、
// ノートの有無そのものを見る(設計レビュー 2026-09-06 M7)。
func TestRetroCheck_所見ノートが無ければ添える(t *testing.T) {
	fixUTC(t)
	sessionsDir := t.TempDir()
	writeSessionLog(t, sessionsDir, "recent01", time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC), 100, 20)
	hub := t.TempDir()
	writeFile(t, filepath.Join(hub, "braindex.json"),
		`{"root": "..", "retro": {"sessions_dir": `+jsonString(sessionsDir)+`, "all_projects": true}}`)
	cfg := filepath.Join(hub, "braindex.json")
	// docs/notes/ がある hub(規約を取り込んである側)でだけ所見ノートを確かめる。
	// 置き場ごと無い hub は docs/notes の規約を採っていないので、無いことを言わない
	writeFile(t, filepath.Join(hub, "docs", "notes", ".gitkeep"), "")

	code, so, se := execRetroCheck(t, "-config", cfg, "-date", "2026-09-01")
	if code != 3 {
		t.Fatalf("exit=%d want 3\nstdout=%s\nstderr=%s", code, so, se)
	}
	if !strings.Contains(so, "所見ノート docs/notes/retro-YYYY-MM-DD.md が窓の中に無い") {
		t.Errorf("所見ノートが無いと言っていない: %q", so)
	}

	// 窓の外(14 日より前)のノートでは足りない
	writeFile(t, filepath.Join(hub, "docs", "notes", "retro-2026-08-01.md"), "# 振り返り 2026-08-01\n")
	_, so, _ = execRetroCheck(t, "-config", cfg, "-date", "2026-09-01")
	if !strings.Contains(so, "窓の中に無い") {
		t.Errorf("窓の外のノートを数えている: %q", so)
	}

	// 窓の中(2026-08-18 以降)のノートがあれば添えない。サブディレクトリでもよい
	writeFile(t, filepath.Join(hub, "docs", "notes", "project", "retro-2026-08-28.md"), "# 振り返り 2026-08-28\n")
	code, so, _ = execRetroCheck(t, "-config", cfg, "-date", "2026-09-01")
	if code != 3 {
		t.Fatalf("exit=%d want 3", code)
	}
	if strings.Contains(so, "窓の中に無い") {
		t.Errorf("窓の中のノートを見ていない: %q", so)
	}

	// docs/notes/ ごと無い hub には言わない(規約を採っていない)
	bare := t.TempDir()
	writeFile(t, filepath.Join(bare, "braindex.json"),
		`{"root": "..", "retro": {"sessions_dir": `+jsonString(sessionsDir)+`, "all_projects": true}}`)
	_, so, _ = execRetroCheck(t, "-config", filepath.Join(bare, "braindex.json"), "-date", "2026-09-01")
	if strings.Contains(so, "窓の中に無い") {
		t.Errorf("置き場ごと無い hub に言っている: %q", so)
	}
}
