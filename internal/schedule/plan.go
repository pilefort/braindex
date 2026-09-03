package schedule

import (
	"fmt"
	"strings"
)

// Command は OS のスケジューラへ渡す 1 回の起動。このパッケージは組み立てるだけで、実行はしない。
type Command struct {
	Name  string   // 実行ファイル(schtasks / crontab)
	Args  []string // 引数
	Stdin string   // 空でなければ標準入力に流す(crontab -)
}

// Display は print と -dry-run で見せる 1 行。標準入力に流す内容は別に見せるので、ここには入れない。
func (c Command) Display() string {
	parts := make([]string, 0, len(c.Args)+1)
	parts = append(parts, c.Name)
	for _, a := range c.Args {
		if strings.ContainsAny(a, " \t") {
			a = `"` + a + `"`
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}

// maxTR は schtasks /TR に渡せる文字数の上限。超えると schtasks が黙って切るので、こちらでエラーにする。
const maxTR = 261

// IsWindows は goos が Windows かを返す(呼び出し側は runtime.GOOS を渡す)。
func IsWindows(goos string) bool { return goos == "windows" }

// TaskName は Windows のタスク名 "braindex-<hub のフォルダ名>-<ジョブ名>" を返す。
// フォルダ名の英数とハイフン以外はハイフンに畳む(タスク名に使えない文字とパス区切りを落とすため)。
func TaskName(hub, job string) string {
	base := baseName(hub)
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	base = strings.Trim(b.String(), "-")
	if base == "" {
		base = "hub"
	}
	return "braindex-" + base + "-" + job
}

// baseName はパスの最後の要素を返す。goos を跨いで同じ結果にするため、区切りは / と \ の両方を見る
// (filepath は実行環境の OS の区切りしか見ないので使わない)。
func baseName(p string) string {
	p = strings.TrimRight(p, `/\`)
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// windowsRun は schtasks の /TR に渡す実行行を作る。
// スケジューラは作業ディレクトリを指定できないので、cmd で hub に移動してから braindex を呼ぶ。
func windowsRun(hub, exe string, j Job) (string, error) {
	var b strings.Builder
	b.WriteString(`cmd /c cd /d "`)
	b.WriteString(hub)
	b.WriteString(`" && "`)
	b.WriteString(exe)
	b.WriteString(`"`)
	for _, a := range j.Args {
		q, err := windowsArg(a)
		if err != nil {
			return "", fmt.Errorf("ジョブ %s: %w", j.Name, err)
		}
		b.WriteString(" ")
		b.WriteString(q)
	}
	run := b.String()
	if len(run) > maxTR {
		return "", fmt.Errorf("ジョブ %s: 実行行が %d 文字で schtasks の上限 %d を超える(hub か braindex の置き場を短いパスに移す): %s",
			j.Name, len(run), maxTR, run)
	}
	return run, nil
}

// windowsArg は /TR の実行行に置く引数 1 つを返す。
//
// cmd は引用符の外では & | < > ^ ( ) を区切りやリダイレクトとして解釈するので、
// それらと空白を含む引数は二重引用符で囲む(cron 側の shellQuote に相当する)。
// 引用符そのものは囲みの中で表せないので、黙って壊れた行を作らずエラーにする。
func windowsArg(a string) (string, error) {
	if strings.Contains(a, `"`) {
		return "", fmt.Errorf("引数に二重引用符は使えない: %q", a)
	}
	if a == "" || strings.ContainsAny(a, " \t&|<>^()") {
		return `"` + a + `"`, nil
	}
	return a, nil
}

// InstallPlan は jobs を登録するコマンド列を返す。
//
// existing は crontab を使う OS で現在の crontab 全文(Windows では無視する)。
// Windows はジョブ 1 本につき 1 コマンド、それ以外は crontab を 1 回書き戻す 1 コマンドになる。
func InstallPlan(goos, hub, exe string, jobs []Job, existing string) ([]Command, error) {
	if IsWindows(goos) {
		cmds := make([]Command, 0, len(jobs))
		for _, j := range jobs {
			w, err := ParseWhen(j.When)
			if err != nil {
				return nil, fmt.Errorf("ジョブ %s: %w", j.Name, err)
			}
			run, err := windowsRun(hub, exe, j)
			if err != nil {
				return nil, err
			}
			args := []string{"/Create", "/F", "/TN", TaskName(hub, j.Name)}
			args = append(args, w.SchtasksArgs()...)
			args = append(args, "/TR", run)
			cmds = append(cmds, Command{Name: "schtasks", Args: args})
		}
		return cmds, nil
	}
	lines, err := cronLines(hub, exe, jobs, existing)
	if err != nil {
		return nil, err
	}
	return []Command{{Name: "crontab", Args: []string{"-"}, Stdin: Merge(existing, hub, lines)}}, nil
}

// cronLines は書き戻すブロックの中身を作る。
//
// 既存ブロックの行は位置を保ったまま差し替え、jobs に無いジョブの行はそのまま残す
// (-job で 1 本だけ登録し直しても、他のジョブの登録が消えたり順序が入れ替わったりしない)。
func cronLines(hub, exe string, jobs []Job, existing string) ([]string, error) {
	byName := make(map[string]Job, len(jobs))
	for _, j := range jobs {
		byName[j.Name] = j
	}
	used := make(map[string]bool, len(jobs))
	var lines []string
	for _, l := range BlockLines(existing, hub) {
		n := JobOfLine(l)
		j, ok := byName[n]
		if !ok {
			lines = append(lines, l) // 別のジョブの行・利用者が書き足した行は触らない
			continue
		}
		nl, err := CronLine(hub, exe, j)
		if err != nil {
			return nil, fmt.Errorf("ジョブ %s: %w", j.Name, err)
		}
		lines = append(lines, nl)
		used[n] = true
	}
	for _, j := range jobs {
		if used[j.Name] {
			continue
		}
		nl, err := CronLine(hub, exe, j)
		if err != nil {
			return nil, fmt.Errorf("ジョブ %s: %w", j.Name, err)
		}
		lines = append(lines, nl)
	}
	return lines, nil
}

// UninstallPlan は crontab から names のジョブの行を落としたコマンドを返す。
//
// names を渡したときは、その行だけを落とし、ブロックの中の見覚えのない行(利用者が書き足した行)は残す。
// names が空のときは hub のブロックごと消すので、見覚えのない行も一緒に消える。
func UninstallPlan(goos, hub string, names []string, existing string) ([]Command, error) {
	if IsWindows(goos) {
		return nil, fmt.Errorf("Windows の解除は UninstallTasks を使う")
	}
	var lines []string
	if len(names) > 0 {
		drop := make(map[string]bool, len(names))
		for _, n := range names {
			drop[n] = true
		}
		for _, l := range BlockLines(existing, hub) {
			if drop[JobOfLine(l)] {
				continue
			}
			lines = append(lines, l)
		}
	}
	return []Command{{Name: "crontab", Args: []string{"-"}, Stdin: Merge(existing, hub, lines)}}, nil
}

// UninstallTasks は Windows で名前を指定してタスクを消すコマンド列を返す。
func UninstallTasks(hub string, names []string) []Command {
	cmds := make([]Command, 0, len(names))
	for _, n := range names {
		cmds = append(cmds, Command{Name: "schtasks", Args: []string{"/Delete", "/F", "/TN", TaskName(hub, n)}})
	}
	return cmds
}

// QueryTask は Windows で登録の有無を調べるコマンドを返す(終了コード 0 なら登録済み)。
func QueryTask(hub, name string) Command {
	return Command{Name: "schtasks", Args: []string{"/Query", "/TN", TaskName(hub, name)}}
}

// ReadCrontab は現在の crontab を読むコマンドを返す(crontab が無い利用者では失敗するので、失敗は空として扱う)。
func ReadCrontab() Command {
	return Command{Name: "crontab", Args: []string{"-l"}}
}
