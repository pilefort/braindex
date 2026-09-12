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

func TestSchedule_Install_Unix_LogDirectory(t *testing.T) {
	for _, dry := range []bool{true, false} {
		hub := schedHub(t, "")
		r := &fakeRunner{}
		args := []string{"install", "-config", filepath.Join(hub, "braindex.json")}
		if dry {
			args = append(args, "-dry-run")
		}
		code, _, se := execSchedule(t, "linux", r, args...)
		if code != 0 {
			t.Fatal(se)
		}
		info, err := os.Stat(filepath.Join(hub, ".braindex"))
		if dry {
			if !os.IsNotExist(err) {
				t.Fatalf("dry-run がフォルダを作った: %v", err)
			}
		} else if err != nil || !info.IsDir() {
			t.Fatalf("ログ用フォルダが無い: %v", err)
		} else if b, err := os.ReadFile(filepath.Join(hub, ".braindex", ".gitignore")); err != nil || string(b) != "schedule.log\n" {
			t.Fatalf("ログを git から外す .gitignore が無い: %q %v", b, err)
		}
	}
}

func TestSchedule_List_Unix_OldLine(t *testing.T) {
	hub := schedHub(t, "")
	r := &fakeRunner{}
	code, _, se := execSchedule(t, "linux", r, "install", "-config", filepath.Join(hub, "braindex.json"))
	if code != 0 {
		t.Fatal(se)
	}
	lines := schedule.BlockLines(r.calls[1].Stdin, hub)
	for i, line := range lines {
		start, end := strings.Index(line, " >> "), strings.Index(line, " # braindex:")
		if start >= 0 {
			lines[i] = line[:start] + line[end:]
		}
	}
	r.crontab = schedule.Merge("", hub, lines)
	var so, stderr bytes.Buffer
	code = runScheduleList([]string{"-config", filepath.Join(hub, "braindex.json")}, &so, &stderr)
	if code != 0 || !strings.Contains(so.String(), "install で登録し直すと揃う") {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, &so, &stderr)
	}
}

func TestSchedule_Windows_LegacyMigration(t *testing.T) {
	for _, sub := range []string{"install", "uninstall", "list"} {
		t.Run(sub, func(t *testing.T) {
			hub := schedHub(t, "")
			old := "braindex-" + filepath.Base(hub) + "-review"
			r := &fakeRunner{queryOK: map[string]bool{old: true}}
			code, so, se := execSchedule(t, "windows", r, sub, "-config", filepath.Join(hub, "braindex.json"), "-job", "review")
			if code != 0 {
				t.Fatal(se)
			}
			if sub == "list" {
				if !strings.Contains(so, "旧い名前で登録済み（install で移す）") {
					t.Fatal(so)
				}
				return
			}
			if len(r.calls) != 3 || r.calls[1].Args[0] != "/Query" || r.calls[1].Args[2] != old || r.calls[2].Args[0] != "/Delete" || r.calls[2].Args[3] != old {
				t.Fatalf("旧名の照会と削除: %+v", r.calls)
			}
			if sub == "install" && (r.calls[0].Args[0] != "/Create" || r.calls[0].Args[3] != schedule.TaskName(hub, "review")) {
				t.Fatalf("新名を先に登録: %+v", r.calls)
			}
			if sub == "uninstall" && (r.calls[0].Args[0] != "/Query" || r.calls[0].Args[2] != schedule.TaskName(hub, "review")) {
				t.Fatalf("新名も対象: %+v", r.calls)
			}
		})
	}
}

func TestSchedule_Windows_LegacyMigrationFailure(t *testing.T) {
	for _, fail := range []string{"/Create", "/Delete"} {
		t.Run(fail, func(t *testing.T) {
			hub := schedHub(t, "")
			r := &fakeRunner{queryOK: map[string]bool{schedule.LegacyTaskName(hub, "review"): true}, failOn: fail}
			code, so, se := execSchedule(t, "windows", r, "install", "-config", filepath.Join(hub, "braindex.json"), "-job", "review")
			if code != 1 || !strings.Contains(se, "失敗した") || strings.Contains(so, "件を登録した") {
				t.Fatalf("exit=%d stdout=%s stderr=%s", code, so, se)
			}
			if fail == "/Create" && len(r.calls) != 1 {
				t.Fatalf("登録失敗後は旧名に触らない: %+v", r.calls)
			}
			if fail == "/Delete" && len(r.calls) != 3 {
				t.Fatalf("新規登録後に旧名を照会して削除: %+v", r.calls)
			}
		})
	}
}

func TestSchedule_Uninstall_Windows_BothNames(t *testing.T) {
	hub := schedHub(t, "")
	current, old := schedule.TaskName(hub, "review"), schedule.LegacyTaskName(hub, "review")
	r := &fakeRunner{queryOK: map[string]bool{current: true, old: true}}
	code, _, se := execSchedule(t, "windows", r, "uninstall", "-config", filepath.Join(hub, "braindex.json"), "-job", "review")
	if code != 0 || len(r.calls) != 4 {
		t.Fatalf("exit=%d stderr=%s calls=%+v", code, se, r.calls)
	}
	for i, name := range []string{current, old} {
		if r.calls[i*2].Args[0] != "/Query" || r.calls[i*2].Args[2] != name || r.calls[i*2+1].Args[0] != "/Delete" || r.calls[i*2+1].Args[3] != name {
			t.Fatalf("新旧両方を照会して削除: %+v", r.calls)
		}
	}
}

func TestSchedule_List_Windows_CurrentAndLegacy(t *testing.T) {
	hub := schedHub(t, "")
	r := &fakeRunner{queryOK: map[string]bool{schedule.TaskName(hub, "review"): true, schedule.LegacyTaskName(hub, "review"): true}}
	code, so, se := execSchedule(t, "windows", r, "list", "-config", filepath.Join(hub, "braindex.json"), "-job", "review")
	if code != 0 || !strings.Contains(so, "登録済み") || strings.Contains(so, "旧い名前") || len(r.calls) != 1 {
		t.Fatalf("exit=%d stdout=%s stderr=%s calls=%+v", code, so, se, r.calls)
	}
}

func TestSchedule_Install_Unix_LogDirectoryFailure(t *testing.T) {
	hub := schedHub(t, "")
	if err := os.WriteFile(filepath.Join(hub, ".braindex"), []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{}
	code, _, se := execSchedule(t, "linux", r, "install", "-config", filepath.Join(hub, "braindex.json"))
	if code != 1 || !strings.Contains(se, "ログ用フォルダを作れない") || len(r.calls) != 1 {
		t.Fatalf("exit=%d stderr=%s calls=%+v", code, se, r.calls)
	}
}

// fakeRunner は schtasks / crontab の代わり。呼ばれたコマンドを覚え、決まった応答を返す。
type fakeRunner struct {
	calls   []schedule.Command
	crontab string          // crontab -l の応答。空なら「crontab が無い」として失敗を返す
	queryOK map[string]bool // schtasks /Query でタスクがあることにする名前
	failOn  string          // Display() にこの文字列を含むコマンドを失敗させる

	// crontabErr が空でなければ、crontab -l はこの出力と error を返す(「crontab が無い」以外の失敗)。
	crontabErr    string
	crontabStderr string
}

func (f *fakeRunner) Run(c schedule.Command) (string, string, error) {
	f.calls = append(f.calls, c)
	if f.failOn != "" && strings.Contains(c.Display(), f.failOn) {
		return "", "スケジューラの出力", errors.New("exit status 1")
	}
	switch {
	case c.Name == "crontab" && len(c.Args) > 0 && c.Args[0] == "-l":
		if f.crontabErr != "" {
			return "", f.crontabErr, errors.New("exit status 1")
		}
		if f.crontab == "" {
			return "", "no crontab for user", errors.New("exit status 1")
		}
		return f.crontab, f.crontabStderr, nil
	case c.Name == "schtasks" && len(c.Args) > 0 && c.Args[0] == "/Query":
		if f.queryOK[c.Args[2]] {
			return "タスク名: " + c.Args[2], "", nil
		}
		return "", "ERROR: 指定されたタスクが存在しません。", errors.New("exit status 1")
	case c.Name == "schtasks" && len(c.Args) > 0 && c.Args[0] == "/Delete":
		if f.queryOK != nil && !f.queryOK[c.Args[3]] {
			return "", "ERROR: 指定されたタスクが存在しません。", errors.New("exit status 1")
		}
	}
	return "", "", nil
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
	if len(r.calls) != 4 {
		t.Fatalf("登録 2 回と旧名の照会 2 回: got=%d (%+v)", len(r.calls), r.calls)
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
	if len(r.calls) != 2 || r.calls[0].Args[3] != schedule.TaskName(hub, "retro") {
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

// スケジューラ側の失敗は 1(登録できていないので失敗。2 は「警告つき完了」に取っておく)。
func TestSchedule_Install_スケジューラの失敗(t *testing.T) {
	hub := schedHub(t, "")
	r := &fakeRunner{failOn: "/Create"}
	code, _, se := execSchedule(t, "windows", r, "install", "-config", filepath.Join(hub, "braindex.json"))
	if code != 1 {
		t.Fatalf("exit=%d want 1\n%s", code, se)
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

// 状態の語は「未登録」(3 文字)と「登録済み」(4 文字)で幅が違う。後ろの列の開始位置を揃える。
func TestSchedule_List_桁が揃う(t *testing.T) {
	hub := schedHub(t, "")
	r := &fakeRunner{crontab: "# BEGIN braindex " + hub + "\n0 9 * * 1 x # braindex:retro\n# END braindex " + hub + "\n"}
	code, so, se := execSchedule(t, "linux", r, "list", "-config", filepath.Join(hub, "braindex.json"))
	if code != 0 {
		t.Fatalf("exit=%d\n%s", code, se)
	}
	// 全角を 2 桁と数えた「braindex」の開始位置
	col := func(l string) int {
		i := strings.Index(l, "braindex ")
		if i < 0 {
			t.Fatalf("コマンドの列が無い: %q", l)
		}
		w := 0
		for _, ru := range l[:i] {
			if ru < 0x80 {
				w++
			} else {
				w += 2
			}
		}
		return w
	}
	lines := strings.Split(strings.TrimSpace(so), "\n")
	if len(lines) != 3 {
		t.Fatalf("hub の 1 行 + ジョブ 2 行: got=%d\n%s", len(lines), so)
	}
	if a, b := col(lines[1]), col(lines[2]); a != b {
		t.Errorf("コマンドの開始位置が揃っていない(%d 桁目と %d 桁目):\n%s", a, b, so)
	}
}

func TestSchedule_Uninstall_Windows(t *testing.T) {
	hub := schedHub(t, "")
	r := &fakeRunner{queryOK: map[string]bool{schedule.TaskName(hub, "review"): true}}
	code, so, se := execSchedule(t, "windows", r, "uninstall", "-config", filepath.Join(hub, "braindex.json"))
	if code != 0 {
		t.Fatalf("exit=%d\n%s", code, se)
	}
	if len(r.calls) != 5 || r.calls[0].Args[0] != "/Query" || r.calls[1].Args[0] != "/Delete" || r.calls[2].Args[0] != "/Query" {
		t.Fatalf("照会し、登録済みの 1 本だけ消す: got=%+v", r.calls)
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

// crontab を一度も書いていない利用者でも、手作業なしで登録できる(「no crontab」の失敗だけ空として扱う)。
// 決定 A'。ここを「失敗は全部エラー」にすると、初回の利用者は crontab -e で空の crontab を作らないと使えない。
func TestSchedule_Install_crontabがまだ無い(t *testing.T) {
	hub := schedHub(t, "")
	for _, msg := range []string{
		"crontab: no crontab for someone", // macOS(BSD cron)
		"no crontab for someone",          // Linux(cronie / vixie-cron)
	} {
		r := &fakeRunner{crontabErr: msg}
		code, _, se := execSchedule(t, "linux", r, "install", "-config", filepath.Join(hub, "braindex.json"))
		if code != 0 {
			t.Fatalf("%q: exit=%d want 0\n%s", msg, code, se)
		}
		if len(r.calls) != 2 || r.calls[1].Args[0] != "-" {
			t.Fatalf("%q: crontab -l → crontab - の順に呼ぶ: got=%+v", msg, r.calls)
		}
	}
}

// 「crontab が無い」以外の理由で読めないときは、書き戻すと全消しになるので止まる。
// とくに uninstall(-job なし)は空文字を書き戻すので、そのまま進むと利用者の crontab が消える。
func TestSchedule_crontabを読めないときは全部止まる(t *testing.T) {
	hub := schedHub(t, "")
	for _, sub := range []string{"install", "uninstall", "list", "print"} {
		r := &fakeRunner{crontabErr: "crontab: you are not authorized to use cron"}
		code, _, se := execSchedule(t, "linux", r, sub, "-config", filepath.Join(hub, "braindex.json"))
		if code != 1 {
			t.Errorf("%s: exit=%d want 1\n%s", sub, code, se)
		}
		mustContain(t, sub+" の stderr", se, "crontab を読めない", "not authorized", "既にある行を消してしまう", "crontab -e")
		for _, c := range r.calls {
			if c.Name == "crontab" && len(c.Args) > 0 && c.Args[0] == "-" {
				t.Errorf("%s: 読めなかったのに書き戻している(全消しになる): %+v", sub, r.calls)
			}
		}
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

func TestSchedule_Uninstall_Windows_DeleteFailure(t *testing.T) {
	hub := schedHub(t, "")
	r := &fakeRunner{queryOK: map[string]bool{schedule.TaskName(hub, "review"): true}, failOn: "/Delete"}
	code, so, se := execSchedule(t, "windows", r, "uninstall", "-config", filepath.Join(hub, "braindex.json"), "-job", "review")
	if code != 1 || strings.Contains(so, "未登録") || !strings.Contains(se, "失敗した") {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, so, se)
	}
}

func TestSchedule_Uninstall_Unix_NoTarget(t *testing.T) {
	hub := schedHub(t, "")
	for _, existing := range []string{"", "keep\r\n", schedule.Merge("", hub, []string{"x # braindex:retro"})} {
		r := &fakeRunner{crontab: existing}
		code, so, se := execSchedule(t, "linux", r, "uninstall", "-config", filepath.Join(hub, "braindex.json"), "-job", "review")
		if code != 0 || len(r.calls) != 1 || !strings.Contains(so, "未登録") {
			t.Errorf("exit=%d calls=%+v stdout=%s stderr=%s", code, r.calls, so, se)
		}
	}
}

func TestSchedule_List_Unix_Drift(t *testing.T) {
	hub := schedHub(t, "")
	r := &fakeRunner{crontab: schedule.Merge("", hub, []string{"0 0 * * * old # braindex:review"})}
	code, so, se := execSchedule(t, "linux", r, "list", "-config", filepath.Join(hub, "braindex.json"))
	if code != 0 || !strings.Contains(so, "設定と異なる") || !strings.Contains(so, "install") || len(r.calls) != 1 {
		t.Fatalf("exit=%d stdout=%s stderr=%s calls=%+v", code, so, se, r.calls)
	}
}

func TestSchedule_List_Unix_Current(t *testing.T) {
	hub := schedHub(t, "")
	r := &fakeRunner{}
	// execSchedule が設定した実行ファイルの位置を使って、一致する登録を作る。
	code, _, se := execSchedule(t, "linux", r, "install", "-config", filepath.Join(hub, "braindex.json"))
	if code != 0 {
		t.Fatal(se)
	}
	r.crontab = r.calls[1].Stdin
	r.calls = nil
	var so, stderr bytes.Buffer
	code = runScheduleList([]string{"-config", filepath.Join(hub, "braindex.json")}, &so, &stderr)
	if code != 0 || strings.Contains(so.String(), "設定と異なる") || strings.Count(so.String(), "登録済み") != 2 || len(r.calls) != 1 {
		t.Fatalf("exit=%d stdout=%s stderr=%s calls=%+v", code, &so, &stderr, r.calls)
	}
}

func TestSchedule_Install_Unix_StderrNotWritten(t *testing.T) {
	hub := schedHub(t, "")
	r := &fakeRunner{crontab: "keep\n", crontabStderr: "diagnostic\n"}
	code, _, se := execSchedule(t, "linux", r, "install", "-config", filepath.Join(hub, "braindex.json"))
	if code != 0 || len(r.calls) != 2 {
		t.Fatalf("exit=%d stderr=%s calls=%+v", code, se, r.calls)
	}
	if !strings.HasPrefix(r.calls[1].Stdin, "keep\n") || strings.Contains(r.calls[1].Stdin, "diagnostic") {
		t.Fatalf("書き戻す本文=%q", r.calls[1].Stdin)
	}
}

func TestSchedule_Uninstall_Unix_NoBlock(t *testing.T) {
	hub := schedHub(t, "")
	for _, existing := range []string{"", "keep\n"} {
		r := &fakeRunner{crontab: existing}
		code, so, se := execSchedule(t, "linux", r, "uninstall", "-config", filepath.Join(hub, "braindex.json"))
		if code != 0 || len(r.calls) != 1 || !strings.Contains(so, "未登録") || strings.Contains(so, "解除:") {
			t.Fatalf("exit=%d stdout=%s stderr=%s calls=%+v", code, so, se, r.calls)
		}
	}
}

// スケジューラの代わりにテスト自身を子プロセスとして起動する。
func TestExecRunner_StdoutOnly(t *testing.T) {
	if os.Getenv("BRAINDEX_SCHEDULE_HELPER") == "1" {
		_, _ = os.Stdout.WriteString("cron body\n")
		_, _ = os.Stderr.WriteString("diagnostic\n")
		os.Exit(0)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	out, diagnostic, err := (execRunner{}).Run(schedule.Command{Name: exe, Args: []string{"-test.run=^TestExecRunner_StdoutOnly$"}, Env: []string{"BRAINDEX_SCHEDULE_HELPER=1"}})
	if err != nil || out != "cron body\n" || diagnostic != "diagnostic\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}
