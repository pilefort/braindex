package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pilefort/braindex/internal/config"
	"github.com/pilefort/braindex/internal/schedule"
)

// runner は OS のスケジューラを起動する層。テストはここをモックに差し替え、schtasks や crontab を呼ばない。
type runner interface {
	Run(schedule.Command) (output string, err error)
}

type execRunner struct{}

func (execRunner) Run(c schedule.Command) (string, error) {
	cmd := exec.Command(c.Name, c.Args...)
	if c.Stdin != "" {
		cmd.Stdin = strings.NewReader(c.Stdin)
	}
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// テストが差し替える口(既存の localLoc と同じ形)。
var (
	scheduleRunner runner = execRunner{}
	scheduleGOOS          = runtime.GOOS
	scheduleExe           = os.Executable
)

func init() {
	register(&command{
		name:    "schedule",
		summary: "定期実行を OS のスケジューラに登録する(install)・消す(uninstall)・状態を見る(list)・コマンドだけ出す(print)",
		run:     runSchedule,
	})
}

func scheduleUsage(w io.Writer) {
	fmt.Fprintln(w, "使い方: braindex schedule <サブコマンド> [フラグ]")
	fmt.Fprintln(w, "  設定 braindex.json の schedule 節に書いたジョブを、この OS のスケジューラに登録する。")
	fmt.Fprintln(w, "  Windows は schtasks、macOS・Linux は crontab(# BEGIN braindex <hub> で囲んだブロックだけを書き換える)。")
	fmt.Fprintln(w, "  登録できるのは braindex 自身のサブコマンドだけで、任意のコマンドは受け付けない。")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "サブコマンド:")
	fmt.Fprintln(w, "  list      設定のジョブと、OS 側に登録されているかを並べる")
	fmt.Fprintln(w, "  print     登録に使うコマンドを出すだけ(何も変えない)")
	fmt.Fprintln(w, "  install   実際に登録する。再実行しても二重にならない")
	fmt.Fprintln(w, "  uninstall 登録を消す")
	fmt.Fprintln(w, "  各サブコマンドの -h で詳細")
}

// runSchedule は braindex schedule <サブコマンド> を振り分ける。
func runSchedule(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		scheduleUsage(stderr)
		return 1
	}
	switch args[0] {
	case "list":
		return runScheduleList(args[1:], stdout, stderr)
	case "print":
		return runSchedulePrint(args[1:], stdout, stderr)
	case "install":
		return runScheduleInstall(args[1:], stdout, stderr)
	case "uninstall":
		return runScheduleUninstall(args[1:], stdout, stderr)
	case "-h", "-help", "--help", "help":
		scheduleUsage(stderr)
		return 0
	}
	fmt.Fprintf(stderr, "braindex schedule: 不明なサブコマンド %q\n", args[0])
	scheduleUsage(stderr)
	return 1
}

// scheduleOptions は 4 つのサブコマンドに共通のコマンドライン。
type scheduleOptions struct {
	config string // -config
	job    string // -job。1 本だけを対象にする
	dryRun bool   // -dry-run(install / uninstall のみ)
}

// scheduleFlagSet は共通のフラグを登録した FlagSet を返す。lines は -h に出す説明。
func scheduleFlagSet(sub string, o *scheduleOptions, withDryRun bool, stderr io.Writer, lines []string) *flag.FlagSet {
	fs := flag.NewFlagSet("braindex schedule "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json)")
	fs.StringVar(&o.job, "job", "", "ジョブ名。指定するとその 1 本だけを扱う(既定: 設定の全部)")
	if withDryRun {
		fs.BoolVar(&o.dryRun, "dry-run", false, "実際には登録せず、実行するコマンドを出す")
	}
	fs.Usage = func() {
		for _, l := range lines {
			fmt.Fprintln(stderr, l)
		}
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "フラグ:")
		fs.PrintDefaults()
	}
	return fs
}

// scheduleEnv は 4 つのサブコマンドが使う下ごしらえの結果。
type scheduleEnv struct {
	hub    string         // 設定ファイルのあるディレクトリ(絶対パス)
	exe    string         // braindex 自身(絶対パス)。定期実行の環境は PATH が違うので頼らない
	all    []schedule.Job // 設定の全ジョブ
	target []schedule.Job // -job で絞ったあとの対象
	names  []string       // target の名前
}

// scheduleSetup は設定を読み、hub と braindex 自身の絶対パスを決め、対象ジョブを選ぶ。
func scheduleSetup(sub string, o scheduleOptions, stderr io.Writer) (scheduleEnv, int) {
	fail := func(err error) (scheduleEnv, int) {
		fmt.Fprintf(stderr, "braindex schedule %s: %v\n", sub, err)
		return scheduleEnv{}, 1
	}
	cfgPath := o.config
	if cfgPath == "" {
		cfgPath = defaultConfig
	}
	fc, found, err := config.Load(cfgPath)
	if err != nil {
		return fail(err)
	}
	if !found {
		return fail(fmt.Errorf("設定ファイルが無い: %s(hub のルートで実行するか、-config で指定する)", cfgPath))
	}
	s := fc.Schedule.WithDefaults()
	if err := s.Validate(commandNames()); err != nil {
		return fail(err)
	}
	// hub は設定ファイルの位置。スケジューラは作業ディレクトリを持たないので絶対パスにする
	hub, err := filepath.Abs(filepath.Dir(cfgPath))
	if err != nil {
		return fail(err)
	}
	exe, err := scheduleExe()
	if err != nil {
		return fail(fmt.Errorf("braindex 自身の置き場が分からない: %w", err))
	}
	if exe, err = filepath.Abs(exe); err != nil {
		return fail(err)
	}
	env := scheduleEnv{hub: hub, exe: exe, all: s.Jobs, target: s.Jobs}
	if o.job != "" {
		j, ok := s.Find(o.job)
		if !ok {
			return fail(fmt.Errorf("設定に無いジョブ: %q(あるのは %s)", o.job, strings.Join(jobNames(s.Jobs), "・")))
		}
		env.target = []schedule.Job{j}
	}
	env.names = jobNames(env.target)
	return env, 0
}

func jobNames(jobs []schedule.Job) []string {
	names := make([]string, 0, len(jobs))
	for _, j := range jobs {
		names = append(names, j.Name)
	}
	return names
}

// readCrontab は現在の crontab を読む。crontab を一度も書いていない利用者では crontab -l が
// 終了コード 1 になるので、その出力(schedule.IsNoCrontab)だけ「空の crontab」として扱う。
//
// それ以外の失敗(権限・一時的な失敗)まで空と畳むと、読みが失敗しつつ書きが通る状況で、
// 書き戻し(crontab -)が利用者の crontab を全消しする。決定 2026-09-03(A') → manual/schedule.md「決めたこと」。
func readCrontab() (string, error) {
	out, err := scheduleRunner.Run(schedule.ReadCrontab())
	if err == nil {
		return out, nil
	}
	if schedule.IsNoCrontab(out) {
		return "", nil // crontab をまだ作っていない。空として続ける
	}
	detail := strings.TrimSpace(out)
	if detail == "" {
		detail = err.Error()
	}
	return "", fmt.Errorf("crontab を読めない: %s"+
		"(読めないまま書き戻すと、既にある行を消してしまう。"+
		"crontab をまだ作っていない環境でこれが出るなら、`crontab -e` で空の crontab を作ってから実行する)", detail)
}

// runCommands はコマンド列を順に実行する。1 つでも失敗したらそこで止め、終了コード 1 を返す。
// 2 は使わない(リポの規約で 2 は「警告つき完了」。登録できていないのは失敗)。
func runCommands(sub string, cmds []schedule.Command, stderr io.Writer) int {
	for _, c := range cmds {
		out, err := scheduleRunner.Run(c)
		if err != nil {
			fmt.Fprintf(stderr, "braindex schedule %s: 失敗した: %s\n%v\n", sub, c.Display(), err)
			if s := strings.TrimSpace(out); s != "" {
				fmt.Fprintln(stderr, s)
			}
			return 1
		}
	}
	return 0
}

// printCommandList は print と -dry-run の出力(実行するコマンドと、標準入力に流す内容)。
func printCommandList(cmds []schedule.Command, stdout io.Writer) {
	for _, c := range cmds {
		fmt.Fprintln(stdout, c.Display())
		if c.Stdin != "" {
			fmt.Fprintln(stdout, "--- 標準入力に流す内容 ---")
			fmt.Fprint(stdout, c.Stdin)
			fmt.Fprintln(stdout, "--- ここまで ---")
		}
	}
}

func runScheduleInstall(args []string, stdout, stderr io.Writer) int {
	var o scheduleOptions
	fs := scheduleFlagSet("install", &o, true, stderr, []string{
		"使い方: braindex schedule install [-config braindex.json] [-job 名前] [-dry-run]",
		"  設定 schedule.jobs のジョブを OS のスケジューラに登録する。再実行しても二重にならない。",
		"  hub と braindex の絶対パスを埋め込むので、hub や braindex を移したら登録し直す。",
		"  終了コード: 0 登録した / 1 フラグ・設定の誤り、またはスケジューラ側が失敗した",
	})
	if code, ok := parseScheduleArgs(fs, args, "install", stderr); !ok {
		return code
	}
	env, code := scheduleSetup("install", o, stderr)
	if code != 0 {
		return code
	}
	existing := ""
	if !schedule.IsWindows(scheduleGOOS) {
		var rerr error
		if existing, rerr = readCrontab(); rerr != nil {
			fmt.Fprintln(stderr, "braindex schedule install:", rerr)
			return 1
		}
	}
	cmds, err := schedule.InstallPlan(scheduleGOOS, env.hub, env.exe, env.target, existing)
	if err != nil {
		fmt.Fprintln(stderr, "braindex schedule install:", err)
		return 1
	}
	if o.dryRun {
		printCommandList(cmds, stdout)
		return 0
	}
	if code := runCommands("install", cmds, stderr); code != 0 {
		return code
	}
	for _, j := range env.target {
		if schedule.IsWindows(scheduleGOOS) {
			fmt.Fprintf(stdout, "登録: %s %s → タスク %s\n", j.Name, j.When, schedule.TaskName(env.hub, j.Name))
		} else {
			fmt.Fprintf(stdout, "登録: %s %s → crontab\n", j.Name, j.When)
		}
	}
	fmt.Fprintf(stdout, "%d 件を登録した(hub %s)。braindex schedule list で確かめられる\n", len(env.target), env.hub)
	return 0
}

func runScheduleUninstall(args []string, stdout, stderr io.Writer) int {
	var o scheduleOptions
	fs := scheduleFlagSet("uninstall", &o, true, stderr, []string{
		"使い方: braindex schedule uninstall [-config braindex.json] [-job 名前] [-dry-run]",
		"  この hub の登録を消す。-job でジョブを 1 本だけ消す。",
		"  crontab では # BEGIN braindex <hub> のブロックだけを触り、他の行は残す。",
		"  終了コード: 0 消した / 1 フラグ・設定の誤り、またはスケジューラ側が失敗した",
	})
	if code, ok := parseScheduleArgs(fs, args, "uninstall", stderr); !ok {
		return code
	}
	env, code := scheduleSetup("uninstall", o, stderr)
	if code != 0 {
		return code
	}
	var cmds []schedule.Command
	if schedule.IsWindows(scheduleGOOS) {
		cmds = schedule.UninstallTasks(env.hub, env.names)
	} else {
		names := env.names
		if o.job == "" {
			names = nil // 名前を渡さなければブロックごと消す(利用者が書き足した行も含めて掃除する)
		}
		existing, rerr := readCrontab()
		if rerr != nil {
			fmt.Fprintln(stderr, "braindex schedule uninstall:", rerr)
			return 1
		}
		var err error
		if cmds, err = schedule.UninstallPlan(scheduleGOOS, env.hub, names, existing); err != nil {
			fmt.Fprintln(stderr, "braindex schedule uninstall:", err)
			return 1
		}
	}
	if o.dryRun {
		printCommandList(cmds, stdout)
		return 0
	}
	// Windows は登録が無いタスクの削除も失敗になる。1 本ずつ試し、失敗は「未登録」として続ける
	if schedule.IsWindows(scheduleGOOS) {
		for i, c := range cmds {
			if _, err := scheduleRunner.Run(c); err != nil {
				fmt.Fprintf(stdout, "未登録: %s(消すものが無い)\n", env.names[i])
				continue
			}
			fmt.Fprintf(stdout, "解除: %s → タスク %s\n", env.names[i], schedule.TaskName(env.hub, env.names[i]))
		}
		return 0
	}
	if code := runCommands("uninstall", cmds, stderr); code != 0 {
		return code
	}
	if o.job == "" {
		fmt.Fprintf(stdout, "解除: この hub の登録をすべて消した(%s)\n", env.hub)
	} else {
		fmt.Fprintf(stdout, "解除: %s → crontab から消した\n", o.job)
	}
	return 0
}

func runSchedulePrint(args []string, stdout, stderr io.Writer) int {
	var o scheduleOptions
	fs := scheduleFlagSet("print", &o, false, stderr, []string{
		"使い方: braindex schedule print [-config braindex.json] [-job 名前]",
		"  登録に使うコマンドを出すだけで、何も変えない(自分で打ちたいとき・別のスケジューラに写したいとき用)。",
	})
	if code, ok := parseScheduleArgs(fs, args, "print", stderr); !ok {
		return code
	}
	env, code := scheduleSetup("print", o, stderr)
	if code != 0 {
		return code
	}
	existing := ""
	if !schedule.IsWindows(scheduleGOOS) {
		var rerr error
		if existing, rerr = readCrontab(); rerr != nil {
			fmt.Fprintln(stderr, "braindex schedule print:", rerr)
			return 1
		}
	}
	cmds, err := schedule.InstallPlan(scheduleGOOS, env.hub, env.exe, env.target, existing)
	if err != nil {
		fmt.Fprintln(stderr, "braindex schedule print:", err)
		return 1
	}
	printCommandList(cmds, stdout)
	return 0
}

func runScheduleList(args []string, stdout, stderr io.Writer) int {
	var o scheduleOptions
	fs := scheduleFlagSet("list", &o, false, stderr, []string{
		"使い方: braindex schedule list [-config braindex.json] [-job 名前]",
		"  設定のジョブと、この OS に登録されているかを並べる。何も変えない。",
	})
	if code, ok := parseScheduleArgs(fs, args, "list", stderr); !ok {
		return code
	}
	env, code := scheduleSetup("list", o, stderr)
	if code != 0 {
		return code
	}
	installed := map[string]bool{}
	if schedule.IsWindows(scheduleGOOS) {
		for _, j := range env.target {
			if _, err := scheduleRunner.Run(schedule.QueryTask(env.hub, j.Name)); err == nil {
				installed[j.Name] = true
			}
		}
	} else {
		existing, rerr := readCrontab()
		if rerr != nil {
			fmt.Fprintln(stderr, "braindex schedule list:", rerr)
			return 1
		}
		for _, l := range schedule.BlockLines(existing, env.hub) {
			if n := schedule.JobOfLine(l); n != "" {
				installed[n] = true
			}
		}
	}
	fmt.Fprintf(stdout, "hub: %s\n", env.hub)
	width := 0
	for _, j := range env.target {
		if len(j.Name) > width {
			width = len(j.Name)
		}
	}
	for _, j := range env.target {
		state := "未登録"
		if installed[j.Name] {
			state = "登録済み"
		}
		fmt.Fprintf(stdout, "  %-*s  %-18s  %s  braindex %s\n",
			width, j.Name, j.When, padDisplay(state, 8), strings.Join(j.Args, " "))
	}
	return 0
}

// padDisplay は端末の桁を揃えるために、全角を 2 桁と数えて幅 width まで空白を足す。
// %-*s はバイト数で数えるので、日本語の語(未登録・登録済み)を並べると桁がずれる。
func padDisplay(s string, width int) string {
	w := 0
	for _, r := range s {
		if r < 0x80 {
			w++
		} else {
			w += 2
		}
	}
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// parseScheduleArgs はフラグを解析する。ok=false のとき code を返して終わる。
func parseScheduleArgs(fs *flag.FlagSet, args []string, sub string, stderr io.Writer) (int, bool) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, false
		}
		return 1, false
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "braindex schedule %s: 引数 %q は受け付けない(フラグだけを渡す)\n", sub, fs.Args())
		fs.Usage()
		return 1, false
	}
	return 0, true
}
