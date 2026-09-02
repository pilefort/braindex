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
		summary: "セッションログの訂正率を測る(stats)・閾値超えを知らせる(check)。本文はどこにも書かず送らない",
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
	fmt.Fprintln(w, "  check   直近の窓の訂正率を閾値と比べて 1 行出す。超えたら終了コード 3(hook やスケジューラが分岐できる)")
	fmt.Fprintln(w, "  extract セッションごとの md ダイジェストと index.tsv を OS の一時ディレクトリに書く(レトロスペクティブ本体の材料)")
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
	case "check":
		return runRetroCheck(args[1:], stdout, stderr)
	case "extract":
		return runRetroExtract(args[1:], stdout, stderr)
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
		fmt.Fprintln(stderr, "  週の境界と窓の 0 時は実行環境のタイムゾーン。位置の区間は設定 retro.position_bins(既定 1-3,4-10,11-30,31-)。")
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
	today, err := retroToday(o.date)
	if err != nil {
		return fail(err)
	}
	w, label, err := retroWindow(o.since, o.windowDays, today)
	if err != nil {
		return fail(err)
	}
	env, err := loadRetroEnv(o.config, o.sessions)
	if err != nil {
		return fail(err)
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

// retroCheckOptions は braindex retro check のコマンドライン。
type retroCheckOptions struct {
	config     string  // -config
	sessions   string  // -sessions
	date       string  // -date。今日の固定
	windowDays int     // -window-days(既定: 設定 retro.window_days)
	threshold  float64 // -threshold(既定: 設定 retro.threshold)
	quiet      bool    // -quiet。閾値超えのときだけ出力
}

// runRetroCheck は braindex retro check を実行する。
// 終了コード: 0 閾値以下 / 1 失敗 / 2 閾値以下だが警告つき / 3 閾値超え(警告があっても 3。超えの合図を優先する)。
func runRetroCheck(args []string, stdout, stderr io.Writer) int {
	var o retroCheckOptions
	fs := flag.NewFlagSet("braindex retro check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json。無ければ既定値で動く)")
	fs.StringVar(&o.sessions, "sessions", "", "セッションログの置き場(既定: 設定 retro.sessions_dir → ~/.claude/projects)")
	fs.StringVar(&o.date, "date", "", "今日として使う日付 YYYY-MM-DD(既定: 実行日)。窓の基準")
	fs.IntVar(&o.windowDays, "window-days", 0, "直近 N 日を窓にする(既定: 設定 retro.window_days → 14)")
	fs.Float64Var(&o.threshold, "threshold", 0, "訂正率の閾値 0〜1(既定: 設定 retro.threshold → 0.08)")
	fs.BoolVar(&o.quiet, "quiet", false, "閾値を超えたときだけ出力する(警告も出さない。hook 向け)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex retro check [-config braindex.json] [-sessions DIR] [-date YYYY-MM-DD] [-window-days N] [-threshold 0.1] [-quiet]")
		fmt.Fprintln(stderr, "  直近の窓(既定 14 日)の訂正率を閾値(既定 8%)と比べて 1 行出す。本文は出さない。")
		fmt.Fprintln(stderr, "  組み込みの例:")
		fmt.Fprintln(stderr, "    Claude Code の hook(SessionStart)に braindex retro check -quiet を置くと、超えたときだけ 1 行がセッションに入る")
		fmt.Fprintln(stderr, "    cron / タスクスケジューラで週 1 回回し、終了コード 3 のときだけ通知コマンドへつなぐ")
		fmt.Fprintln(stderr, "  終了コード: 0 閾値以下 / 1 失敗 / 2 閾値以下だが警告つき(読めないログを飛ばした) / 3 閾値超え(警告があっても 3)")
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
		fmt.Fprintln(stderr, "braindex retro check:", err)
		return 1
	}
	if fs.NArg() > 0 {
		return fail(fmt.Errorf("引数 %q は受け付けない(フラグだけを渡す)", fs.Args()))
	}
	// 明示されたフラグだけが設定を上書きする(0 は「未指定」ではなく誤り)
	windowSet, thresholdSet := false, false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "window-days":
			windowSet = true
		case "threshold":
			thresholdSet = true
		}
	})
	if windowSet && o.windowDays < 1 {
		return fail(errors.New("-window-days は 1 以上"))
	}
	if thresholdSet && (o.threshold <= 0 || o.threshold > 1) {
		return fail(errors.New("-threshold は 0 より大きく 1 以下(0.08 = 8%)"))
	}
	today, err := retroToday(o.date)
	if err != nil {
		return fail(err)
	}
	env, err := loadRetroEnv(o.config, o.sessions)
	if err != nil {
		return fail(err)
	}
	days, thr := env.settings.WindowDays, env.settings.Threshold
	if windowSet {
		days = o.windowDays
	}
	if thresholdSet {
		thr = o.threshold
	}

	w := retro.Recent(today, days, retroLoc)
	ss, warns, err := sessions.Dir{Path: env.sessionsDir}.Sessions(sessions.Options{Since: w.Since})
	if err != nil {
		return fail(err)
	}
	if !o.quiet {
		for _, wn := range warns {
			fmt.Fprintln(stderr, "braindex retro check: 警告:", wn)
		}
	}
	total := retro.Total(retro.Judge(ss, w, env.dicts...))
	msg := fmt.Sprintf("直近 %d 日の訂正率 %s(発話 %d・訂正 %d)", days, total.Percent(), total.Utterances, total.Corrections)
	if total.Rate() > thr {
		fmt.Fprintf(stdout, "braindex retro check: %sが閾値 %.1f%% を超えた → レトロスペクティブの時期(braindex retro extract で材料を出す)\n", msg, thr*100)
		return 3
	}
	if !o.quiet {
		fmt.Fprintf(stdout, "braindex retro check: %sは閾値 %.1f%% 以下\n", msg, thr*100)
	}
	if len(warns) > 0 {
		if !o.quiet {
			fmt.Fprintf(stderr, "braindex retro check: 警告 %d 件(終了コード 2)\n", len(warns))
		}
		return 2
	}
	return 0
}

// retroExtractOptions は braindex retro extract のコマンドライン。
type retroExtractOptions struct {
	config     string // -config
	sessions   string // -sessions
	date       string // -date。今日の固定
	since      string // -since
	windowDays int    // -window-days
	out        string // -out。出力先(既定: OS の一時ディレクトリの braindex-retro)
}

// runRetroExtract は braindex retro extract を実行する。
// セッションごとの md(窓の中の人間の発話・直前のアシスタント本文 300 字・訂正と感情の印)と index.tsv を出力先に書く。
// 既定の出力先は OS の一時ディレクトリ(セッションログには機微が含まれるので、リポには書かない)。
func runRetroExtract(args []string, stdout, stderr io.Writer) int {
	var o retroExtractOptions
	fs := flag.NewFlagSet("braindex retro extract", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json。無ければ既定値で動く)")
	fs.StringVar(&o.sessions, "sessions", "", "セッションログの置き場(既定: 設定 retro.sessions_dir → ~/.claude/projects)")
	fs.StringVar(&o.date, "date", "", "今日として使う日付 YYYY-MM-DD(既定: 実行日)。-window-days の基準")
	fs.StringVar(&o.since, "since", "", "この日以降の発話だけを書く YYYY-MM-DD(既定: 全期間)")
	fs.IntVar(&o.windowDays, "window-days", 0, "直近 N 日の発話だけを書く(-since と同時には使えない)")
	fs.StringVar(&o.out, "out", "", "出力先ディレクトリ(既定: OS の一時ディレクトリの braindex-retro)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex retro extract [-config braindex.json] [-sessions DIR] [-since YYYY-MM-DD | -window-days N] [-out DIR]")
		fmt.Fprintln(stderr, "  セッションごとの md ダイジェスト(sessions/<プロジェクト>/<開始日時>_<ID>.md)と index.tsv を書く。")
		fmt.Fprintln(stderr, "  ダイジェストは、窓の中の人間の発話ごとに「直前のアシスタント本文 300 字 → 発話(2000 字まで)」。")
		fmt.Fprintln(stderr, "  訂正辞書に当たった発話には ★、感情辞書に当たった発話には ☆ を見出しに付ける。")
		fmt.Fprintln(stderr, "  既定の出力先は OS の一時ディレクトリ。セッションログには機微が含まれるので、リポの中に -out を向けるときは自己責任で。")
		fmt.Fprintln(stderr, "  終了コード: 0 成功 / 1 失敗(何も書かない) / 2 警告つきで完了(読めないログを飛ばした)")
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
		fmt.Fprintln(stderr, "braindex retro extract:", err)
		return 1
	}
	if fs.NArg() > 0 {
		return fail(fmt.Errorf("引数 %q は受け付けない(フラグだけを渡す)", fs.Args()))
	}
	today, err := retroToday(o.date)
	if err != nil {
		return fail(err)
	}
	w, label, err := retroWindow(o.since, o.windowDays, today)
	if err != nil {
		return fail(err)
	}
	env, err := loadRetroEnv(o.config, o.sessions)
	if err != nil {
		return fail(err)
	}
	outDir := o.out
	if outDir == "" {
		outDir = filepath.Join(os.TempDir(), "braindex-retro")
	}

	ss, warns, err := sessions.Dir{Path: env.sessionsDir}.Sessions(sessions.Options{Since: w.Since})
	if err != nil {
		return fail(err)
	}
	for _, wn := range warns {
		fmt.Fprintln(stderr, "braindex retro extract: 警告:", wn)
	}
	res := retro.Extract(retro.Input{
		Sessions:    ss,
		Window:      w,
		WindowLabel: label,
		Corrections: env.dicts,
		Sentiment:   retro.Sentiment(),
		Loc:         retroLoc,
		Home:        env.home,
	})
	for _, f := range res.Files {
		p := filepath.Join(outDir, filepath.FromSlash(f.RelPath))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return fail(err)
		}
		if err := os.WriteFile(p, f.Content, 0o644); err != nil {
			return fail(err)
		}
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fail(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "index.tsv"), res.Index, 0o644); err != nil {
		return fail(err)
	}
	fmt.Fprintf(stdout, "braindex retro extract: %d セッション・発話 %d・訂正 %d → %s\n", res.Sessions, res.UserTurns, res.CorrectionTurns, outDir)
	if len(warns) > 0 {
		fmt.Fprintf(stderr, "braindex retro extract: 警告 %d 件(終了コード 2)\n", len(warns))
		return 2
	}
	return 0
}

// retroWindow は -since / -window-days から窓と表示用の見出しを決める(-since > -window-days > 全期間)。
func retroWindow(since string, windowDays int, today time.Time) (retro.Window, string, error) {
	if since != "" && windowDays != 0 {
		return retro.Window{}, "", errors.New("-since と -window-days は同時に使えない")
	}
	if windowDays < 0 {
		return retro.Window{}, "", errors.New("-window-days は 0 以上")
	}
	switch {
	case since != "":
		d, err := time.ParseInLocation("2006-01-02", since, retroLoc)
		if err != nil {
			return retro.Window{}, "", fmt.Errorf("-since は YYYY-MM-DD で指定する: %q", since)
		}
		return retro.Window{Since: d}, since + " 以降", nil
	case windowDays > 0:
		w := retro.Recent(today, windowDays, retroLoc)
		return w, fmt.Sprintf("%s 以降(%d 日)", w.Since.In(retroLoc).Format("2006-01-02"), windowDays), nil
	}
	return retro.Window{}, "全期間", nil
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
