package schedule

import (
	"strings"
	"testing"
	"unicode/utf16"
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

// 空白や cmd のメタ文字を含む引数は二重引用符で囲む(hub と braindex は既に囲んである)。
// 囲まないと cmd が区切りやリダイレクトとして解釈し、黙って別のコマンドが走る。
func TestInstallPlan_Windows_引数を引用する(t *testing.T) {
	j := Job{Name: "news", Args: []string{"news", "fetch", "-out", `D:\out dir\a&b.md`}, When: "daily:07:30"}
	got, err := InstallPlan("windows", winHub, winExe, []Job{j}, "")
	if err != nil {
		t.Fatalf("InstallPlan: %v", err)
	}
	want := `cmd /c cd /d "C:\Users\u\hub" && "C:\Users\u\go\bin\braindex.exe" news fetch -out "D:\out dir\a&b.md"`
	if run := got[0].Args[len(got[0].Args)-1]; run != want {
		t.Errorf("got= %q\nwant=%q", run, want)
	}
	// 二重引用符を含む引数は /TR の中で表せない。黙って壊れた行を作らずエラーにする
	bad := Job{Name: "news", Args: []string{"news", `a"b`}, When: "daily:07:30"}
	if _, err := InstallPlan("windows", winHub, winExe, []Job{bad}, ""); err == nil {
		t.Error("二重引用符を含む引数はエラーにする")
	}
}

// schtasks の /TR は 261 文字まで。黙って切られると別のコマンドが走るので、こちらでエラーにする。
func TestInstallPlan_Windows_実行行が長すぎる(t *testing.T) {
	long := `C:\Users\u\` + strings.Repeat("a", 300) // ASCII で余裕を持って超える(単位に依らず落ちる入力)
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

// 終了マーカーが消えた壊れたブロックでも、利用者が書き足した最終行を落とさない。
func TestInstallPlan_Unix_終了マーカーが無い(t *testing.T) {
	const h, exe = "/home/u/hub", "/home/u/go/bin/braindex"
	existing := "keep\n# BEGIN braindex " + h + "\n0 9 * * 1 x # braindex:review\n手で足した行\n"
	got, err := InstallPlan("linux", h, exe, []Job{jobReview}, existing)
	if err != nil {
		t.Fatalf("InstallPlan: %v", err)
	}
	if !strings.Contains(got[0].Stdin, "手で足した行") {
		t.Errorf("利用者が書き足した行を落としている: got=%q", got[0].Stdin)
	}
	if !strings.HasPrefix(got[0].Stdin, "keep\n") {
		t.Errorf("ブロックより前の行は残す: got=%q", got[0].Stdin)
	}
	if n := strings.Count(got[0].Stdin, "# braindex:review"); n != 1 {
		t.Errorf("review の行は 1 本: got=%q", got[0].Stdin)
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

func TestUninstallPlan_Unix_NoTarget(t *testing.T) {
	for _, existing := range []string{"", "keep\r\n", Merge("", hub+"-other", []string{"x"})} {
		for _, names := range [][]string{nil, {"review"}} {
			got, err := UninstallPlan("linux", hub, names, existing)
			if err != nil || len(got) != 0 {
				t.Errorf("got=%+v err=%v", got, err)
			}
		}
	}
	got, err := UninstallPlan("linux", hub, []string{"review"}, Merge("", hub, []string{"x # braindex:retro"}))
	if err != nil || len(got) != 0 {
		t.Errorf("got=%+v err=%v", got, err)
	}
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

// trOf は Windows の install 計画から /TR の実行行を取り出す。
func trOf(t *testing.T, hub string) (string, error) {
	t.Helper()
	cmds, err := InstallPlan("windows", hub, winExe, []Job{jobReview}, "")
	if err != nil {
		return "", err
	}
	for _, c := range cmds {
		for i, a := range c.Args {
			if a == "/TR" && i+1 < len(c.Args) {
				return c.Args[i+1], nil
			}
		}
	}
	t.Fatalf("/TR が無い: %+v", cmds)
	return "", nil
}

// 実行行の長さは**文字数**で数える。バイト数で数えると、日本語を含むパスの利用者が、
// 実際は上限に収まる行を「超える」と誤って拒否される(2026-09-03: 264 バイト / 228 文字の実例で発覚)。
func TestInstallPlan_Windows_実行行の長さは文字数で数える(t *testing.T) {
	const base = `C:\h`
	s, err := trOf(t, base)
	if err != nil {
		t.Fatalf("短い hub なら通る: %v", err)
	}
	n := len(utf16.Encode([]rune(s)))
	if n >= maxTR {
		t.Fatalf("前提が崩れている(短い hub で既に上限): %d", n)
	}
	pad := maxTR - n

	// 境界: ちょうど maxTR は通し、1 文字超えたら弾く。
	if _, err := trOf(t, base+strings.Repeat("a", pad)); err != nil {
		t.Errorf("ちょうど %d 文字は通す: %v", maxTR, err)
	}
	if _, err := trOf(t, base+strings.Repeat("a", pad+1)); err == nil {
		t.Errorf("%d 文字は弾く", maxTR+1)
	}

	// 単位の取り違えの検出: 同じ文字数を日本語(1 文字 3 バイト・BMP 内)にすると、
	// バイト数で数えている実装ではここで落ちる(長さは maxTR ちょうどで、収まっている)。
	jp := base + strings.Repeat("あ", pad)
	got, err := trOf(t, jp)
	if err != nil {
		t.Fatalf("UTF-16 で %d に収まっているのに弾いた(バイト数で数えている): %v", maxTR, err)
	}
	if c, b := len(utf16.Encode([]rune(got))), len(got); c != maxTR || b <= maxTR {
		t.Errorf("この入力は「UTF-16 は上限ちょうど・バイト数は超過」であるべき: UTF-16=%d バイト=%d", c, b)
	}

	// 非 BMP(絵文字)は 1 ルーン＝2 UTF-16 コード単位。ルーン数で数えると過小に見えるので、
	// 「上限内と判定したのに schtasks に黙って切られる」側へ倒れる。危険な向きなので弾く。
	emoji := base + strings.Repeat("\U0001F600", pad) // ルーン数は pad、UTF-16 では 2*pad
	if _, err := trOf(t, emoji); err == nil {
		t.Errorf("非 BMP はルーン数でなく UTF-16 コード単位で数える(ルーン数だと上限内に見えるが実際は超える)")
	}
}
