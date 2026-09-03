package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/schedule"
)

// fakeRunner は schtasks / crontab の代わり。呼ばれたコマンドを覚え、決まった応答を返す。
type fakeRunner struct {
	calls   []schedule.Command
	crontab string          // crontab -l の応答。空なら「crontab が無い」として失敗を返す
	queryOK map[string]bool // schtasks /Query でタスクがあることにする名前
	failOn  string          // Display() にこの文字列を含むコマンドを失敗させる
}

func (f *fakeRunner) Run(c schedule.Command) (string, error) {
	f.calls = append(f.calls, c)
	if f.failOn != "" && strings.Contains(c.Display(), f.failOn) {
		return "スケジューラの出力", errors.New("exit status 1")
	}
	switch {
	case c.Name == "crontab" && len(c.Args) > 0 && c.Args[0] == "-l":
		if f.crontab == "" {
			return "no crontab for user", errors.New("exit status 1")
		}
		return f.crontab, nil
	case c.Name == "schtasks" && len(c.Args) > 0 && c.Args[0] == "/Query":
		if f.queryOK[c.Args[2]] {
			return "タスク名: " + c.Args[2], nil
		}
		return "ERROR: 指定されたタスクが存在しません。", errors.New("exit status 1")
	case c.Name == "schtasks" && len(c.Args) > 0 && c.Args[0] == "/Delete":
		if f.queryOK != nil && !f.queryOK[c.Args[3]] {
			return "ERROR: 指定されたタスクが存在しません。", errors.New("exit status 1")
		}
	}
	return "", nil
}

// schedHub は braindex.json を置いた一時 hub を作る。cfg が空なら {}(既定のジョブ)。
func schedHub(t *testing.T, cfg string) string {
	t.Helper()
	dir := t.TempDir()
	if cfg == "" {
		cfg = "{}"
	}
	if err := os.WriteFile(filepath.Join(dir, "braindex.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// execSchedule は goos と runner を差し替えて braindex schedule を実行する。
func execSchedule(t *testing.T, goos string, r *fakeRunner, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	oldGOOS, oldRunner, oldExe := scheduleGOOS, scheduleRunner, scheduleExe
	t.Cleanup(func() { scheduleGOOS, scheduleRunner, scheduleExe = oldGOOS, oldRunner, oldExe })
	scheduleGOOS = goos
	scheduleRunner = r
	exe := filepath.Join(t.TempDir(), "braindex")
	scheduleExe = func() (string, error) { return exe, nil }
	var so, se bytes.Buffer
	code = dispatch(append([]string{"schedule"}, args...), &so, &se)
	return code, so.String(), se.String()
}

// Windows は既定の 2 本を schtasks /Create で登録する。
func TestSchedule_Install_Windows(t *testing.T) {
	hub := schedHub(t, "")
	r := &fakeRunner{}
	code, so, se := execSchedule(t, "windows", r, "install", "-config", filepath.Join(hub, "braindex.json"))
	if code != 0 {
		t.Fatalf("exit=%d\nstdout=%s\nstderr=%s", code, so, se)
	}
	if len(r.calls) != 2 {
		t.Fatalf("schtasks は 2 回: got=%d (%+v)", len(r.calls), r.calls)
	}
	for i, want := range []string{"review", "retro"} {
		c := r.calls[i]
		if c.Name != "schtasks" || c.Args[0] != "/Create" || c.Args[1] != "/F" {
			t.Errorf("calls[%d]: got=%+v", i, c)
		}
		if got := c.Args[3]; got != schedule.TaskName(hub, want) {
			t.Errorf("calls[%d] のタスク名: got=%q want=%q", i, got, schedule.TaskName(hub, want))
		}
		// 実行行には hub と braindex 自身の絶対パスが入る(定期実行の環境は PATH が違う)
		tr := c.Args[len(c.Args)-1]
		if !strings.Contains(tr, hub) || !strings.Contains(tr, "braindex") {
			t.Errorf("calls[%d] の /TR: got=%q", i, tr)
		}
	}
	if !strings.Contains(so, "登録: review") || !strings.Contains(so, "2 件を登録した") {
		t.Errorf("stdout:\n%s", so)
	}
}

// Unix は crontab を読んでからブロックを足して書き戻す。既存の行は残す。
func TestSchedule_Install_Unix(t *testing.T) {
	hub := schedHub(t, "")
	r := &fakeRunner{crontab: "0 0 * * * /usr/bin/backup\n"}
	code, so, se := execSchedule(t, "linux", r, "install", "-config", filepath.Join(hub, "braindex.json"))
	if code != 0 {
		t.Fatalf("exit=%d\nstdout=%s\nstderr=%s", code, so, se)
	}
	if len(r.calls) != 2 || r.calls[0].Args[0] != "-l" || r.calls[1].Args[0] != "-" {
		t.Fatalf("crontab -l → crontab - の順に呼ぶ: got=%+v", r.calls)
	}
	in := r.calls[1].Stdin
	for _, want := range []string{"/usr/bin/backup", "# BEGIN braindex " + hub, "# braindex:review", "# braindex:retro"} {
		if !strings.Contains(in, want) {
			t.Errorf("crontab に %q が無い:\n%s", want, in)
		}
	}
}

// -dry-run は登録せず、実行するはずのコマンドを出す(crontab の読み取りだけは要る)。
func TestSchedule_Install_DryRun(t *testing.T) {
	hub := schedHub(t, "")
	r := &fakeRunner{}
	code, so, se := execSchedule(t, "windows", r, "install", "-config", filepath.Join(hub, "braindex.json"), "-dry-run")
	if code != 0 {
		t.Fatalf("exit=%d\n%s", code, se)
	}
	if len(r.calls) != 0 {
		t.Errorf("-dry-run では何も実行しない: got=%+v", r.calls)
	}
	if !strings.Contains(so, "schtasks /Create") || strings.Contains(so, "登録:") {
		t.Errorf("コマンドだけを出す:\n%s", so)
	}
}

// -job で 1 本だけを扱う。設定に無い名前はフラグの誤りとして 1。
func TestSchedule_Install_Job(t *testing.T) {
	hub := schedHub(t, "")
	r := &fakeRunner{}
	code, so, se := execSchedule(t, "windows", r, "install", "-config", filepath.Join(hub, "braindex.json"), "-job", "retro")
	if code != 0 {
		t.Fatalf("exit=%d\n%s", code, se)
	}
	if len(r.calls) != 1 || r.calls[0].Args[3] != schedule.TaskName(hub, "retro") {
		t.Fatalf("retro だけ登録する: got=%+v", r.calls)
	}
	if !strings.Contains(so, "1 件を登録した") {
		t.Errorf("stdout:\n%s", so)
	}
	code, _, se = execSchedule(t, "windows", &fakeRunner{}, "install", "-config", filepath.Join(hub, "braindex.json"), "-job", "なし")
	if code != 1 {
		t.Fatalf("exit=%d want 1", code)
	}
	if !strings.Contains(se, "設定に無いジョブ") {
		t.Errorf("stderr:\n%s", se)
	}
}

// スケジューラ側の失敗は 2(設定の誤り 1 と分ける)。
func TestSchedule_Install_スケジューラの失敗(t *testing.T) {
	hub := schedHub(t, "")
	r := &fakeRunner{failOn: "/Create"}
	code, _, se := execSchedule(t, "windows", r, "install", "-config", filepath.Join(hub, "braindex.json"))
	if code != 2 {
		t.Fatalf("exit=%d want 2\n%s", code, se)
	}
	for _, want := range []string{"失敗した", "schtasks /Create", "スケジューラの出力"} {
		if !strings.Contains(se, want) {
			t.Errorf("stderr に %q が無い:\n%s", want, se)
		}
	}
}

// 設定が無い hub では動かない(hub の位置が決まらないため)。
func TestSchedule_設定が無い(t *testing.T) {
	dir := t.TempDir()
	code, _, se := execSchedule(t, "windows", &fakeRunner{}, "install", "-config", filepath.Join(dir, "braindex.json"))
	if code != 1 {
		t.Fatalf("exit=%d want 1", code)
	}
	if !strings.Contains(se, "設定ファイルが無い") {
		t.Errorf("stderr:\n%s", se)
	}
}

// 任意のコマンドは登録しない。braindex のサブコマンドでない args は設定の誤り。
func TestSchedule_任意のコマンドは登録しない(t *testing.T) {
	hub := schedHub(t, `{"schedule":{"jobs":[{"name":"bad","args":["curl","http://example.com"],"when":"daily:09:00"}]}}`)
	r := &fakeRunner{}
	code, _, se := execSchedule(t, "linux", r, "install", "-config", filepath.Join(hub, "braindex.json"))
	if code != 1 {
		t.Fatalf("exit=%d want 1", code)
	}
	if !strings.Contains(se, "サブコマンドでない") {
		t.Errorf("stderr:\n%s", se)
	}
	if len(r.calls) != 0 {
		t.Errorf("何も実行しない: got=%+v", r.calls)
	}
}

func TestSchedule_List_Windows(t *testing.T) {
	hub := schedHub(t, "")
	r := &fakeRunner{queryOK: map[string]bool{schedule.TaskName(hub, "review"): true}}
	code, so, se := execSchedule(t, "windows", r, "list", "-config", filepath.Join(hub, "braindex.json"))
	if code != 0 {
		t.Fatalf("exit=%d\n%s", code, se)
	}
	if !strings.Contains(so, "review") || !strings.Contains(so, "登録済み") {
		t.Errorf("review は登録済みと出す:\n%s", so)
	}
	if !strings.Contains(so, "未登録") {
		t.Errorf("retro は未登録と出す:\n%s", so)
	}
	if !strings.Contains(so, hub) {
		t.Errorf("hub を出す:\n%s", so)
	}
}

func TestSchedule_List_Unix(t *testing.T) {
	hub := schedHub(t, "")
	r := &fakeRunner{crontab: "# BEGIN braindex " + hub + "\n0 9 * * 1 x # braindex:retro\n# END braindex " + hub + "\n"}
	code, so, se := execSchedule(t, "linux", r, "list", "-config", filepath.Join(hub, "braindex.json"))
	if code != 0 {
		t.Fatalf("exit=%d\n%s", code, se)
	}
	lines := strings.Split(strings.TrimSpace(so), "\n")
	if len(lines) != 3 {
		t.Fatalf("hub の 1 行 + ジョブ 2 行: got=%d\n%s", len(lines), so)
	}
	if !strings.Contains(lines[1], "未登録") || !strings.Contains(lines[2], "登録済み") {
		t.Errorf("crontab にある retro だけ登録済み:\n%s", so)
	}
}

func TestSchedule_Uninstall_Windows(t *testing.T) {
	hub := schedHub(t, "")
	r := &fakeRunner{queryOK: map[string]bool{schedule.TaskName(hub, "review"): true}}
	code, so, se := execSchedule(t, "windows", r, "uninstall", "-config", filepath.Join(hub, "braindex.json"))
	if code != 0 {
		t.Fatalf("exit=%d\n%s", code, se)
	}
	if len(r.calls) != 2 || r.calls[0].Args[0] != "/Delete" {
		t.Fatalf("2 本とも消しにいく: got=%+v", r.calls)
	}
	// 登録が無いタスクの削除は失敗するが、消すものが無いだけなので成功のまま続ける
	if !strings.Contains(so, "解除: review") || !strings.Contains(so, "未登録: retro") {
		t.Errorf("stdout:\n%s", so)
	}
}

func TestSchedule_Uninstall_Unix(t *testing.T) {
	hub := schedHub(t, "")
	existing := "keep\n# BEGIN braindex " + hub + "\n0 9 * * 1 x # braindex:review\n5 9 * * 1 y # braindex:retro\n# END braindex " + hub + "\n"
	// 名前を指定しなければブロックごと消える
	r := &fakeRunner{crontab: existing}
	code, so, se := execSchedule(t, "linux", r, "uninstall", "-config", filepath.Join(hub, "braindex.json"))
	if code != 0 {
		t.Fatalf("exit=%d\n%s", code, se)
	}
	if got := r.calls[1].Stdin; got != "keep\n" {
		t.Errorf("ブロックごと消す: got=%q", got)
	}
	if !strings.Contains(so, "すべて消した") {
		t.Errorf("stdout:\n%s", so)
	}
	// -job なら残りの行はそのまま
	r = &fakeRunner{crontab: existing}
	if code, _, se = execSchedule(t, "linux", r, "uninstall", "-config", filepath.Join(hub, "braindex.json"), "-job", "review"); code != 0 {
		t.Fatalf("exit=%d\n%s", code, se)
	}
	in := r.calls[1].Stdin
	if strings.Contains(in, "# braindex:review") || !strings.Contains(in, "# braindex:retro") || !strings.Contains(in, "keep") {
		t.Errorf("review だけ消す:\n%s", in)
	}
}

// print は何も変えない。
func TestSchedule_Print(t *testing.T) {
	hub := schedHub(t, "")
	r := &fakeRunner{}
	code, so, se := execSchedule(t, "windows", r, "print", "-config", filepath.Join(hub, "braindex.json"))
	if code != 0 {
		t.Fatalf("exit=%d\n%s", code, se)
	}
	if len(r.calls) != 0 {
		t.Errorf("print は何も実行しない: got=%+v", r.calls)
	}
	if n := strings.Count(so, "schtasks /Create"); n != 2 {
		t.Errorf("2 本分のコマンドを出す(%d 件):\n%s", n, so)
	}
}

// crontab を一度も書いていない利用者でも登録できる(crontab -l の失敗は空として扱う)。
func TestSchedule_Install_crontabが無い(t *testing.T) {
	hub := schedHub(t, "")
	r := &fakeRunner{} // crontab が空 → crontab -l は失敗を返す
	code, _, se := execSchedule(t, "linux", r, "install", "-config", filepath.Join(hub, "braindex.json"))
	if code != 0 {
		t.Fatalf("exit=%d\n%s", code, se)
	}
	in := r.calls[1].Stdin
	if !strings.HasPrefix(in, "# BEGIN braindex "+hub) {
		t.Errorf("空の crontab にブロックだけを書く:\n%s", in)
	}
}

// サブコマンドの誤り・引数の付け足しは 1。
func TestSchedule_使い方の誤り(t *testing.T) {
	hub := schedHub(t, "")
	cases := [][]string{
		{},
		{"なし"},
		{"list", "余分な引数", "-config", filepath.Join(hub, "braindex.json")},
	}
	for _, args := range cases {
		if code, _, _ := execSchedule(t, "windows", &fakeRunner{}, args...); code != 1 {
			t.Errorf("schedule %v: exit=%d want 1", args, code)
		}
	}
	if code, _, _ := execSchedule(t, "windows", &fakeRunner{}, "-h"); code != 0 {
		t.Errorf("-h は 0: exit=%d", code)
	}
}
