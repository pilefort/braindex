package schedule

import (
	"crypto/sha256"
	"fmt"
	"path"
	"strings"
	"unicode/utf16"
)

// Command は OS のスケジューラへ渡す 1 回の起動。このパッケージは組み立てるだけで、実行はしない。
type Command struct {
	Name  string   // 実行ファイル(schtasks / crontab)
	Args  []string // 引数
	Stdin string   // 空でなければ標準入力に流す(crontab -)
	Env   []string // 空でなければ "KEY=VALUE" を実行環境に足す(出力の文言を固定するとき用)
}

// Display は print と -dry-run で見せる 1 行。標準入力に流す内容と Env は入れない(実行するコマンドの見た目を変えないため)。
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

// maxTR は schtasks /TR に渡せる長さの上限。超えると schtasks が黙って切るので、こちらでエラーにする。
// 数えるのは Windows のコマンドラインと同じ **UTF-16 コード単位**で、バイト数ではない。
// バイト数で数えると C:\Users\山田\... のような日本語を含むパスの利用者が、実際は収まる行を弾かれる
// (2026-09-03 実測: 同じ行が 264 バイト / 228 文字)。
// ルーン数でも BMP 内なら一致するが、絵文字などの非 BMP 文字は 1 ルーン＝2 コード単位なので、
// ルーン数だと過小に数えて「上限内と判定したのに schtasks に切られる」側へ倒れる。危険な向きを避ける。
// 上限の出典: Microsoft Learn「schtasks create」の /tr の説明「The path name must not exceed 262 characters」
// (https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/schtasks-create 。2026-09-13 確認)。
// 単位は「文字」。261 は 262 より 1 だけ厳しい側で、既存の値を据え置く。
const maxTR = 261

// IsWindows は goos が Windows かを返す(呼び出し側は runtime.GOOS を渡す)。
func IsWindows(goos string) bool { return goos == "windows" }

// TaskName は絶対パスのハッシュを含む Windows のタスク名を返す。
// hub は呼び出し側で絶対パスにする。OS に依存せず大小文字と区切りを揃える。
func TaskName(hub, job string) string {
	normalized := path.Clean(strings.ToLower(strings.ReplaceAll(hub, `\`, "/")))
	sum := sha256.Sum256([]byte(normalized))
	return LegacyTaskName(normalized, fmt.Sprintf("%x-%s", sum[:4], job))
}

// LegacyTaskName は移行前のタスク名を返す。
// フォルダ名の英数とハイフン以外はハイフンに畳む(タスク名に使えない文字とパス区切りを落とすため)。
func LegacyTaskName(hub, job string) string {
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
	if n := len(utf16.Encode([]rune(run))); n > maxTR {
		return "", fmt.Errorf("ジョブ %s: 実行行が %d 文字で schtasks の上限 %d を超える(hub か braindex の置き場を短いパスに移す): %s",
			j.Name, n, maxTR, run)
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
// 消す対象が無ければコマンドを返さず、crontab を新規作成しない。
func UninstallPlan(goos, hub string, names []string, existing string) ([]Command, error) {
	if IsWindows(goos) {
		return nil, fmt.Errorf("Windows の解除は UninstallTasks を使う")
	}
	_, inside, _, found := splitBlock(existing, hub)
	if !found {
		return nil, nil
	}
	var lines []string
	if len(names) > 0 {
		drop := make(map[string]bool, len(names))
		for _, n := range names {
			drop[n] = true
		}
		removed := false
		for _, l := range inside {
			if drop[JobOfLine(l)] {
				removed = true
				continue
			}
			lines = append(lines, l)
		}
		if !removed {
			return nil, nil
		}
	}
	return []Command{{Name: "crontab", Args: []string{"-"}, Stdin: Merge(existing, hub, lines)}}, nil
}

// UninstallTasks は Windows で名前を指定してタスクを消すコマンド列を返す。
func UninstallTasks(hub string, names []string) []Command {
	cmds := make([]Command, 0, 2*len(names))
	for _, n := range names {
		cmds = append(cmds, Command{Name: "schtasks", Args: []string{"/Delete", "/F", "/TN", TaskName(hub, n)}})
		cmds = append(cmds, Command{Name: "schtasks", Args: []string{"/Delete", "/F", "/TN", LegacyTaskName(hub, n)}})
	}
	return cmds
}

// QueryTask は Windows で登録の有無を調べるコマンドを返す(終了コード 0 なら登録済み)。
func QueryTask(hub, name string) Command {
	return QueryNamedTask(TaskName(hub, name))
}

// QueryNamedTask は新旧いずれかのタスク名を指定して照会する。
func QueryNamedTask(task string) Command {
	return Command{Name: "schtasks", Args: []string{"/Query", "/TN", task}}
}

// ReadCrontab は現在の crontab を読むコマンドを返す。
// crontab を一度も書いていない利用者では終了コード 1 になり、その判別は出力の文言でしかできないので、
// LC_ALL=C を付けて文言を英語に固定する(判別は IsNoCrontab)。
func ReadCrontab() Command {
	return Command{Name: "crontab", Args: []string{"-l"}, Env: []string{"LC_ALL=C"}}
}

// IsNoCrontab は crontab -l の失敗が「その利用者の crontab がまだ無い」ことかを、出力の文言で判別する。
// macOS(BSD cron)は "crontab: no crontab for <user>"、Linux(cronie / vixie-cron)は "no crontab for <user>"。
// ReadCrontab が LC_ALL=C を付けるので、環境の言語設定では変わらない。
//
// この判別を捨てて「失敗は全部 crontab が空」と畳むと、読めなかっただけの回に書き戻し(crontab -)が走り、
// 利用者の crontab を全消しする。逆に「失敗は全部エラー」にすると、crontab がまだ無い利用者が install できない。
// 文言の違う cron 実装(未確認: busybox)ではエラー側に倒れるだけで、全消しにはならない。
//
// 「no crontab を含む」で見ると緩すぎる。呼び出し側は stdout が空のときだけ stderr を渡す。
// 別の診断を「無い」と誤判定して全消ししないよう、行頭一致＋単一行に絞る
// (2026-09-03 実測・macOS 15: 終了コード 1・"crontab: no crontab for <user>" の 1 行 39 バイトのみ)。
func IsNoCrontab(output string) bool {
	s := strings.ToLower(strings.TrimSpace(output))
	if strings.Contains(s, "\n") {
		return false // 本文が混ざっている＝「crontab が無い」ではない
	}
	return strings.HasPrefix(s, "no crontab for ") || strings.HasPrefix(s, "crontab: no crontab for ")
}
