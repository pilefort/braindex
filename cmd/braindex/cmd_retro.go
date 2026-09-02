package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pilefort/braindex/internal/config"
	"github.com/pilefort/braindex/internal/retro"
	"github.com/pilefort/braindex/internal/sessions"
)

// retroLoc は週の境界と窓の 0 時を決めるタイムゾーン(既定: 実行環境のローカル)。テストが UTC に差し替える。
var retroLoc = time.Local

func init() {
	register(&command{
		name:    "retro",
		summary: "セッションログの訂正率を測る(retro stats)。本文はどこにも書かず送らない",
		run:     runRetro,
	})
}

func retroUsage(w io.Writer) {
	fmt.Fprintln(w, "使い方: braindex retro <サブコマンド> [フラグ]")
	fmt.Fprintln(w, "  Claude Code のセッションログ(既定 ~/.claude/projects)を読み、人間の発話のうち訂正(辞書照合)の割合を出す。")
	fmt.Fprintln(w, "  判定は決定論で、発話の本文はどこにも書かず送らない。")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "サブコマンド:")
	fmt.Fprintln(w, "  stats   発話数・訂正数・率を、プロジェクト別／週別／セッション内位置の区間別の表で出す")
	fmt.Fprintln(w, "  各サブコマンドの -h で詳細")
}

// runRetro は braindex retro <サブコマンド> を振り分ける。
func runRetro(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		retroUsage(stderr)
		return 1
	}
	switch args[0] {
	case "stats":
		return runRetroStats(args[1:], stdout, stderr)
	case "-h", "-help", "--help", "help":
		retroUsage(stderr)
		return 0
	}
	fmt.Fprintf(stderr, "braindex retro: 不明なサブコマンド %q\n", args[0])
	retroUsage(stderr)
	return 1
}

// retroStatsOptions は braindex retro stats のコマンドライン。空は「未指定」。
type retroStatsOptions struct {
	config     string // -config。無くても動く(retro は hub を要らない)
	sessions   string // -sessions。セッションログの置き場(設定より優先)
	date       string // -date。今日の固定(-window-days の基準)
	since      string // -since。この日以降
	windowDays int    // -window-days。直近 N 日
	by         string // -by。区分(コンマ区切り)
}

// runRetroStats は braindex retro stats を実行する。
func runRetroStats(args []string, stdout, stderr io.Writer) int {
	var o retroStatsOptions
	fs := flag.NewFlagSet("braindex retro stats", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json。無ければ既定値で動く)")
	fs.StringVar(&o.sessions, "sessions", "", "セッションログの置き場(既定: 設定 retro.sessions_dir → ~/.claude/projects)")
	fs.StringVar(&o.date, "date", "", "今日として使う日付 YYYY-MM-DD(既定: 実行日)。-window-days の基準")
	fs.StringVar(&o.since, "since", "", "この日以降の発話だけを数える YYYY-MM-DD(既定: 全期間)")
	fs.IntVar(&o.windowDays, "window-days", 0, "直近 N 日の発話だけを数える(-since と同時には使えない)")
	fs.StringVar(&o.by, "by", "project", "区分: project / week / position。コンマ区切りで複数(その順に表を出す)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex retro stats [-config braindex.json] [-sessions DIR] [-since YYYY-MM-DD | -window-days N] [-by project,week,position]")
		fmt.Fprintln(stderr, "  人間の発話数・訂正(辞書に当たった発話)数・率を Markdown の表で出す。本文は出さない。")
		fmt.Fprintln(stderr, "  週の境界と窓の 0 時は実行環境のタイムゾーン。位置の区間は設定 retro.position_bins(既定 1-10,11-30,31-)。")
		fmt.Fprintln(stderr, "  終了コード: 0 成功 / 1 失敗 / 2 警告つきで完了(読めないログを飛ばした)")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "フラグ:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "braindex retro stats:", err)
		return 1
	}
	if fs.NArg() > 0 {
		return fail(fmt.Errorf("引数 %q は受け付けない(フラグだけを渡す)", fs.Args()))
	}
	var bys []string
	for _, b := range strings.Split(o.by, ",") {
		b = strings.TrimSpace(b)
		switch b {
		case "project", "week", "position":
			bys = append(bys, b)
		default:
			return fail(fmt.Errorf("-by は project / week / position のどれか(コンマ区切り可): %q", b))
		}
	}
	if o.since != "" && o.windowDays != 0 {
		return fail(errors.New("-since と -window-days は同時に使えない"))
	}
	if o.windowDays < 0 {
		return fail(errors.New("-window-days は 0 以上"))
	}
	today, err := retroToday(o.date)
	if err != nil {
		return fail(err)
	}
	env, err := loadRetroEnv(o.config, o.sessions)
	if err != nil {
		return fail(err)
	}

	// 窓: -since > -window-days > 全期間
	var w retro.Window
	label := "全期間"
	switch {
	case o.since != "":
		d, err := time.ParseInLocation("2006-01-02", o.since, retroLoc)
		if err != nil {
			return fail(fmt.Errorf("-since は YYYY-MM-DD で指定する: %q", o.since))
		}
		w.Since = d
		label = o.since + " 以降"
	case o.windowDays > 0:
		w = retro.Recent(today, o.windowDays, retroLoc)
		label = fmt.Sprintf("%s 以降(%d 日)", w.Since.In(retroLoc).Format("2006-01-02"), o.windowDays)
	}

	ss, warns, err := sessions.Dir{Path: env.sessionsDir}.Sessions(sessions.Options{Since: w.Since})
	if err != nil {
		return fail(err)
	}
	for _, wn := range warns {
		fmt.Fprintln(stderr, "braindex retro stats: 警告:", wn)
	}
	items := retro.Judge(ss, w, env.dicts...)
	total := retro.Total(items)
	fmt.Fprintf(stdout, "braindex retro stats: 窓 %s・発話 %d・訂正 %d・率 %s\n", label, total.Utterances, total.Corrections, total.Percent())
	for _, b := range bys {
		fmt.Fprintln(stdout)
		switch b {
		case "project":
			fmt.Fprint(stdout, retro.Render("プロジェクト", retro.ByProject(items, env.home), total))
		case "week":
			fmt.Fprint(stdout, retro.Render("週", retro.ByWeek(items, retroLoc), total))
		case "position":
			fmt.Fprint(stdout, retro.Render("位置", retro.ByPosition(items, env.bins), total))
		}
	}
	if len(warns) > 0 {
		fmt.Fprintf(stderr, "braindex retro stats: 警告 %d 件(終了コード 2)\n", len(warns))
		return 2
	}
	return 0
}

// retroToday は -date(YYYY-MM-DD・retroLoc の 0 時)か、無ければ今。
func retroToday(date string) (time.Time, error) {
	if date == "" {
		return time.Now(), nil
	}
	t, err := time.ParseInLocation("2006-01-02", date, retroLoc)
	if err != nil {
		return time.Time{}, fmt.Errorf("-date は YYYY-MM-DD で指定する: %q", date)
	}
	return t, nil
}

// retroEnv は設定から決めた実行環境。
type retroEnv struct {
	settings    retro.Settings
	sessionsDir string
	dicts       []*retro.Dictionary // 判定に使う辞書(dictionary か既定辞書、それに dictionary_extra)
	bins        []retro.Bin
	home        string // 表示でホームを "~" に置き換える(取れなければ "")
}

// loadRetroEnv は設定ファイル(無ければ既定値)とフラグから実行環境を決める。
// 設定ファイル内のパスは "~" を展開し、相対なら設定ファイルのディレクトリ基準。
func loadRetroEnv(cfgPath, sessionsFlag string) (retroEnv, error) {
	var env retroEnv
	explicit := cfgPath != ""
	if !explicit {
		cfgPath = defaultConfig
	}
	fc, found, err := config.Load(cfgPath)
	if err != nil {
		return env, err
	}
	if explicit && !found {
		return env, fmt.Errorf("設定ファイルが見つからない: %s", cfgPath)
	}
	baseDir := "."
	if found {
		baseDir = filepath.Dir(cfgPath)
	}
	env.home, _ = os.UserHomeDir() // 取れなければ "" のまま("~" の展開と表示の置換をしないだけ)
	if err := fc.Retro.Validate(); err != nil {
		return env, err
	}
	s := fc.Retro.WithDefaults()
	env.settings = s

	switch {
	case sessionsFlag != "":
		env.sessionsDir = sessionsFlag
	case s.SessionsDir != "":
		env.sessionsDir = retro.ResolvePath(s.SessionsDir, baseDir, env.home)
	default:
		d, err := sessions.DefaultDir()
		if err != nil {
			return env, err
		}
		env.sessionsDir = d
	}

	if s.Dictionary != "" {
		d, err := retro.Load(retro.ResolvePath(s.Dictionary, baseDir, env.home))
		if err != nil {
			return env, fmt.Errorf("設定 retro.dictionary: %w", err)
		}
		env.dicts = append(env.dicts, d)
	} else {
		env.dicts = append(env.dicts, retro.Corrections())
	}
	if s.DictionaryExtra != "" {
		d, err := retro.Load(retro.ResolvePath(s.DictionaryExtra, baseDir, env.home))
		if err != nil {
			return env, fmt.Errorf("設定 retro.dictionary_extra: %w", err)
		}
		env.dicts = append(env.dicts, d)
	}
	bins, err := retro.ParseBins(s.PositionBins)
	if err != nil {
		return env, fmt.Errorf("設定 retro.position_bins: %w", err)
	}
	env.bins = bins
	return env, nil
}
