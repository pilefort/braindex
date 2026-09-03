package schedule

import (
	"strings"
	"testing"
)

const (
	winHub = `C:\Users\u\hub`
	winExe = `C:\Users\u\go\bin\braindex.exe`
)

func TestTaskName(t *testing.T) {
	cases := []struct {
		hub, job, want string
	}{
		{`C:\Users\u\hub`, "review", "braindex-hub-review"},
		{`C:\Users\u\hub\`, "review", "braindex-hub-review"},
		{"/home/u/brain", "retro", "braindex-brain-retro"},
		{`C:\Users\u\my hub`, "review", "braindex-my-hub-review"}, // 使えない文字はハイフンに畳む
		{`C:\Users\u\脳brain`, "review", "braindex-brain-review"},  // 前後に付いたハイフンは落とす
		{`C:\Users\u\第二の脳`, "review", "braindex-hub-review"},      // 畳んだ結果が空なら hub
		{"/", "review", "braindex-hub-review"},                    // 名前が残らないときも hub
	}
	for _, c := range cases {
		if got := TaskName(c.hub, c.job); got != c.want {
			t.Errorf("TaskName(%q,%q): got=%q want=%q", c.hub, c.job, got, c.want)
		}
	}
}

// Windows はジョブ 1 本につき schtasks 1 回。/F を付けて再実行で上書きする(冪等)。
func TestInstallPlan_Windows(t *testing.T) {
	got, err := InstallPlan("windows", winHub, winExe, []Job{jobReview, jobRetro}, "")
	if err != nil {
		t.Fatalf("InstallPlan: %v", err)
	}
	want := []Command{
		{Name: "schtasks", Args: []string{"/Create", "/F", "/TN", "braindex-hub-review",
			"/SC", "WEEKLY", "/D", "MON", "/ST", "09:00",
			"/TR", `cmd /c cd /d "C:\Users\u\hub" && "C:\Users\u\go\bin\braindex.exe" review`}},
		{Name: "schtasks", Args: []string{"/Create", "/F", "/TN", "braindex-hub-retro",
			"/SC", "WEEKLY", "/D", "MON", "/ST", "09:05",
			"/TR", `cmd /c cd /d "C:\Users\u\hub" && "C:\Users\u\go\bin\braindex.exe" retro check`}},
	}
	assertCommands(t, got, want)
}

func TestInstallPlan_Windows_毎日(t *testing.T) {
	j := Job{Name: "news", Args: []string{"news", "fetch", "-layer", "daily"}, When: "daily:07:30"}
	got, err := InstallPlan("windows", winHub, winExe, []Job{j}, "")
	if err != nil {
		t.Fatalf("InstallPlan: %v", err)
	}
	want := []Command{
		{Name: "schtasks", Args: []string{"/Create", "/F", "/TN", "braindex-hub-news",
			"/SC", "DAILY", "/ST", "07:30",
			"/TR", `cmd /c cd /d "C:\Users\u\hub" && "C:\Users\u\go\bin\braindex.exe" news fetch -layer daily`}},
	}
	assertCommands(t, got, want)
}

// schtasks の /TR は 261 文字まで。黙って切られると別のコマンドが走るので、こちらでエラーにする。
func TestInstallPlan_Windows_実行行が長すぎる(t *testing.T) {
	long := `C:\Users\u\` + strings.Repeat("あ", 200)
	_, err := InstallPlan("windows", long, winExe, []Job{jobReview}, "")
	if err == nil {
		t.Fatal("上限を超えたらエラーにする")
	}
	for _, want := range []string{"261", "review"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("エラー文に %q を含める: %v", want, err)
		}
	}
}

// Windows 以外は crontab を 1 回書き戻す。既存の行は残す。
func TestInstallPlan_Unix(t *testing.T) {
	existing := "0 0 * * * /usr/bin/backup\n"
	for _, goos := range []string{"linux", "darwin"} {
		got, err := InstallPlan(goos, "/home/u/hub", "/home/u/go/bin/braindex", []Job{jobReview, jobRetro}, existing)
		if err != nil {
			t.Fatalf("InstallPlan(%s): %v", goos, err)
		}
		if len(got) != 1 {
			t.Fatalf("InstallPlan(%s): コマンドは 1 つ: got=%d", goos, len(got))
		}
		if got[0].Name != "crontab" || len(got[0].Args) != 1 || got[0].Args[0] != "-" {
			t.Errorf("InstallPlan(%s): crontab - に書き戻す: got=%+v", goos, got[0])
		}
		want := existing +
			"# BEGIN braindex /home/u/hub\n" +
			"0 9 * * 1 cd '/home/u/hub' && '/home/u/go/bin/braindex' 'review' # braindex:review\n" +
			"5 9 * * 1 cd '/home/u/hub' && '/home/u/go/bin/braindex' 'retro' 'check' # braindex:retro\n" +
			"# END braindex /home/u/hub\n"
		if got[0].Stdin != want {
			t.Errorf("InstallPlan(%s) の標準入力:\n got=%q\nwant=%q", goos, got[0].Stdin, want)
		}
	}
}

// 2 回登録しても crontab は同じ。-job で 1 本だけ登録し直しても、他のジョブの行と順序は変わらない。
func TestInstallPlan_Unix_冪等と部分更新(t *testing.T) {
	const h, exe = "/home/u/hub", "/home/u/go/bin/braindex"
	first, err := InstallPlan("linux", h, exe, []Job{jobReview, jobRetro}, "keep\n")
	if err != nil {
		t.Fatalf("InstallPlan: %v", err)
	}
	second, err := InstallPlan("linux", h, exe, []Job{jobReview, jobRetro}, first[0].Stdin)
	if err != nil {
		t.Fatalf("InstallPlan: %v", err)
	}
	if first[0].Stdin != second[0].Stdin {
		t.Errorf("2 回目で変わった:\n1回目=%q\n2回目=%q", first[0].Stdin, second[0].Stdin)
	}
	// review だけ時刻を変えて登録し直す
	moved := Job{Name: "review", Args: []string{"review"}, When: "weekly:fri:18:00"}
	got, err := InstallPlan("linux", h, exe, []Job{moved}, first[0].Stdin)
	if err != nil {
		t.Fatalf("InstallPlan: %v", err)
	}
	lines := BlockLines(got[0].Stdin, h)
	if len(lines) != 2 {
		t.Fatalf("行数: got=%d want=2 (%q)", len(lines), got[0].Stdin)
	}
	if JobOfLine(lines[0]) != "review" || !strings.HasPrefix(lines[0], "0 18 * * 5 ") {
		t.Errorf("review の行を同じ位置で差し替える: got=%q", lines[0])
	}
	if JobOfLine(lines[1]) != "retro" {
		t.Errorf("対象でない retro の行は残す: got=%q", lines[1])
	}
}

func TestUninstallPlan_Unix(t *testing.T) {
	const h = "/home/u/hub"
	existing := "keep\n# BEGIN braindex " + h + "\n" +
		"0 9 * * 1 x # braindex:review\n5 9 * * 1 y # braindex:retro\n手で足した行\n" +
		"# END braindex " + h + "\n"
	// 名前を渡さなければブロックごと消える
	got, err := UninstallPlan("linux", h, nil, existing)
	if err != nil {
		t.Fatalf("UninstallPlan: %v", err)
	}
	if got[0].Stdin != "keep\n" {
		t.Errorf("ブロックごと消す: got=%q", got[0].Stdin)
	}
	// 1 本だけ消すときは、他の行を残す
	got, err = UninstallPlan("linux", h, []string{"review"}, existing)
	if err != nil {
		t.Fatalf("UninstallPlan: %v", err)
	}
	if strings.Contains(got[0].Stdin, "# braindex:review") {
		t.Errorf("review を消す: got=%q", got[0].Stdin)
	}
	for _, keep := range []string{"# braindex:retro", "手で足した行", "keep"} {
		if !strings.Contains(got[0].Stdin, keep) {
			t.Errorf("%q が残っていない: got=%q", keep, got[0].Stdin)
		}
	}
	if _, err := UninstallPlan("windows", h, nil, ""); err == nil {
		t.Error("Windows は UninstallTasks を使うのでエラーにする")
	}
}

func TestUninstallTasks(t *testing.T) {
	got := UninstallTasks(winHub, []string{"review", "retro"})
	want := []Command{
		{Name: "schtasks", Args: []string{"/Delete", "/F", "/TN", "braindex-hub-review"}},
		{Name: "schtasks", Args: []string{"/Delete", "/F", "/TN", "braindex-hub-retro"}},
	}
	assertCommands(t, got, want)
}

func TestQueryTask(t *testing.T) {
	got := QueryTask(winHub, "review")
	if got.Name != "schtasks" || strings.Join(got.Args, " ") != "/Query /TN braindex-hub-review" {
		t.Errorf("got=%+v", got)
	}
}

func TestCommand_Display(t *testing.T) {
	c := Command{Name: "schtasks", Args: []string{"/TN", "braindex-hub-review", "/TR", `cmd /c cd /d "C:\h" && "b" review`}}
	want := `schtasks /TN braindex-hub-review /TR "cmd /c cd /d "C:\h" && "b" review"`
	if got := c.Display(); got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

func TestIsWindows(t *testing.T) {
	if !IsWindows("windows") || IsWindows("linux") || IsWindows("darwin") {
		t.Error("IsWindows の判定が違う")
	}
}

func assertCommands(t *testing.T, got, want []Command) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("コマンド数: got=%d want=%d (got=%+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Name != want[i].Name || got[i].Stdin != want[i].Stdin ||
			strings.Join(got[i].Args, "\x00") != strings.Join(want[i].Args, "\x00") {
			t.Errorf("コマンド %d:\n got=%+v\nwant=%+v", i, got[i], want[i])
		}
	}
}
