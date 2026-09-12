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
	"github.com/pilefort/braindex/internal/fsutil"
	"github.com/pilefort/braindex/internal/retro"
	"github.com/pilefort/braindex/internal/sessions"
)

func init() {
	register(&command{
		name:    "retro",
		summary: "セッションログの訂正率を測る(stats)・閾値超えを知らせる(check)・ダイジェストを一時ディレクトリに書く(extract)。本文は送らない",
		run:     runRetro,
	})
}

func retroUsage(w io.Writer) {
	fmt.Fprintln(w, "使い方: braindex retro <サブコマンド> [フラグ]")
	fmt.Fprintln(w, "  Claude Code のセッションログ(既定 ~/.claude/projects)を読み、人間の発話のうち訂正(辞書照合)の割合を出す。")
	fmt.Fprintln(w, "  判定は規則ベースで、本文はどこにも送らない。本文を書くのは extract だけで、書き先は OS の一時ディレクトリ(リポには書かない)。")
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
	config      string // -config。無くても動く(retro は hub を要らない)
	sessions    string // -sessions。セッションログの置き場(設定より優先)
	date        string // -date。今日の固定(-window-days の基準)
	since       string // -since。この日以降
	windowDays  int    // -window-days。直近 N 日
	by          string // -by。区分(コンマ区切り)
	allProjects bool   // -all-projects。root の外のセッションも数える
}

// runRetroStats は braindex retro stats を実行する。
func runRetroStats(args []string, stdout, stderr io.Writer) int {
	var o retroStatsOptions
	fs := flag.NewFlagSet("braindex retro stats", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json。無ければ既定値で動く)")
	fs.StringVar(&o.sessions, "sessions", "", "セッションログの置き場(既定: 設定 retro.sessions_dir → ~/.claude/projects)")
	fs.BoolVar(&o.allProjects, "all-projects", false, "root の外で交わしたセッションも数える(既定: root 配下だけ。設定 retro.all_projects と同じ)")
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
	env, err := loadRetroEnv(o.config, o.sessions, o.allProjects)
	if err != nil {
		return fail(err)
	}

	ss, sessWarns, err := sessions.Dir{Path: env.sessionsDir}.Sessions(sessions.Options{Since: w.Since, UnderRoot: env.underRoot})
	if err != nil {
		return fail(err)
	}
	warns := append(env.warnings, sessWarns...)
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
			fmt.Fprint(stdout, retro.Render("週", retro.ByWeek(items, localLoc), total))
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
	config      string  // -config
	sessions    string  // -sessions
	date        string  // -date。今日の固定
	windowDays  int     // -window-days(既定: 設定 retro.window_days)
	threshold   float64 // -threshold(既定: 設定 retro.threshold)
	quiet       bool    // -quiet。閾値超えのときだけ出力
	allProjects bool    // -all-projects。root の外のセッションも数える
}

// runRetroCheck は braindex retro check を実行する。
// 終了コード: 0 閾値以下 / 1 失敗 / 2 閾値以下だが警告つき / 3 閾値超え(警告があっても 3。超えの合図を優先する)。
func runRetroCheck(args []string, stdout, stderr io.Writer) int {
	var o retroCheckOptions
	fs := flag.NewFlagSet("braindex retro check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json。無ければ既定値で動く)")
	fs.StringVar(&o.sessions, "sessions", "", "セッションログの置き場(既定: 設定 retro.sessions_dir → ~/.claude/projects)")
	fs.BoolVar(&o.allProjects, "all-projects", false, "root の外で交わしたセッションも数える(既定: root 配下だけ。設定 retro.all_projects と同じ)")
	fs.StringVar(&o.date, "date", "", "今日として使う日付 YYYY-MM-DD(既定: 実行日)。窓の基準")
	fs.IntVar(&o.windowDays, "window-days", 0, "直近 N 日を窓にする(既定: 設定 retro.window_days → 14)")
	fs.Float64Var(&o.threshold, "threshold", 0, "訂正率の閾値 0〜1(既定: 設定 retro.threshold → 0.08)")
	fs.BoolVar(&o.quiet, "quiet", false, "閾値を超えたときだけ出力する(警告も出さない。hook 向け)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex retro check [-config braindex.json] [-sessions DIR] [-date YYYY-MM-DD] [-window-days N] [-threshold 0.1] [-quiet]")
		fmt.Fprintln(stderr, "  直近の窓(既定 14 日)の訂正率を閾値(既定 8%)と比べて 1 行出す。本文は出さない。")
		fmt.Fprintln(stderr, "  組み込みの例:")
		fmt.Fprintln(stderr, "    Claude Code の hook(SessionStart)に braindex retro check -quiet || true を置くと、超えたときだけ 1 行がセッションに入る")
		fmt.Fprintln(stderr, "    cron / タスクスケジューラで週 1 回回し、終了コード 3 のときだけ通知コマンドへつなぐ")
		fmt.Fprintln(stderr, "  終了コード: 0 閾値以下 / 1 失敗 / 2 閾値以下だが警告つき(読めないログを飛ばした) / 3 閾値超え(警告があっても 3)")
		fmt.Fprintln(stderr, "  終了コード 3 は hook 以外(スケジューラ等)向け。Claude Code の hook は終了コード 0 の stdout だけを文脈に入れるので、hook では || true で 0 に落とす")
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
	env, err := loadRetroEnv(o.config, o.sessions, o.allProjects)
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

	w := retro.Recent(today, days, localLoc)
	weeks := env.settings.Baseline()
	base := retro.Baseline(w, weeks)
	// セッションの読み込みは 1 回。基準期間まで遡って読む(基準を使わないときは窓の起点から)
	since := w.Since
	if !base.Since.IsZero() {
		since = base.Since
	}
	ss, sessWarns, err := sessions.Dir{Path: env.sessionsDir}.Sessions(sessions.Options{Since: since, UnderRoot: env.underRoot})
	if err != nil {
		return fail(err)
	}
	warns := append(env.warnings, sessWarns...)
	if !o.quiet {
		for _, wn := range warns {
			fmt.Fprintln(stderr, "braindex retro check: 警告:", wn)
		}
	}
	total := retro.Total(retro.Judge(ss, w, env.dicts...))
	if total.Utterances == 0 {
		// 「訂正率 -(発話 0・訂正 0)は閾値以下」は判定したように読める。判定の材料が無いことを言う(終了コードは閾値以下と同じ)
		if !o.quiet {
			fmt.Fprintf(stdout, "braindex retro check: 直近 %d 日に発話が無い(読んだセッションログ %d 件)。閾値 %.1f%% の判定は発話が入ってから\n", days, len(ss), thr*100)
		}
		if len(warns) > 0 {
			if !o.quiet {
				fmt.Fprintf(stderr, "braindex retro check: 警告 %d 件(終了コード 2)\n", len(warns))
			}
			return 2
		}
		return 0
	}
	baseTotal := retro.Count{}
	if !base.Since.IsZero() {
		baseTotal = retro.Total(retro.Judge(ss, base, env.dicts...))
	}
	verdict := retro.Compare(total, baseTotal, thr)
	msg := fmt.Sprintf("直近 %d 日の訂正率 %s(発話 %d・訂正 %d)%s / 閾値 %.1f%% → %s",
		days, total.Percent(), total.Utterances, total.Corrections,
		baselineClause(base, baseTotal, weeks), thr*100, verdictText(verdict))
	if verdict == retro.Exceed {
		// 鳴らしたのに窓の中に所見ノートが無ければ添える。機械節が毎回動いていても、
		// 所見が残っていなければ振り返りの回路は動いていない(設計レビュー 2026-09-06 M7)
		if !hasRetroNoteInWindow(env.hubDir, w.Since, today.AddDate(0, 0, 1)) {
			msg += "（所見ノート docs/notes/retro-YYYY-MM-DD.md が窓の中に無い）"
		}
		fmt.Fprintf(stdout, "braindex retro check: %s\n", msg)
		return 3
	}
	if !o.quiet {
		fmt.Fprintf(stdout, "braindex retro check: %s\n", msg)
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
	config      string // -config
	sessions    string // -sessions
	date        string // -date。今日の固定
	since       string // -since
	windowDays  int    // -window-days
	out         string // -out。出力先(既定: OS の一時ディレクトリの braindex-retro)
	allProjects bool   // -all-projects。root の外のセッションも数える
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
	fs.BoolVar(&o.allProjects, "all-projects", false, "root の外で交わしたセッションも数える(既定: root 配下だけ。設定 retro.all_projects と同じ)")
	fs.StringVar(&o.date, "date", "", "今日として使う日付 YYYY-MM-DD(既定: 実行日)。-window-days の基準")
	fs.StringVar(&o.since, "since", "", "この日以降の発話だけを書く YYYY-MM-DD(既定: 全期間)")
	fs.IntVar(&o.windowDays, "window-days", 0, "直近 N 日の発話だけを書く(-since と同時には使えない)")
	fs.StringVar(&o.out, "out", "", "出力先ディレクトリ(既定: OS の一時ディレクトリの braindex-retro)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex retro extract [-config braindex.json] [-sessions DIR] [-since YYYY-MM-DD | -window-days N] [-out DIR]")
		fmt.Fprintln(stderr, "  セッションごとの md ダイジェスト(sessions/<プロジェクト>/<開始日時>_<ID>.md)と index.tsv を書く。")
		fmt.Fprintln(stderr, "  ダイジェストは、窓の中の人間の発話ごとに「直前のアシスタント本文 300 字 → 発話(2000 字まで)」。")
		fmt.Fprintln(stderr, "  訂正辞書に当たった発話には ★、感情辞書に当たった発話には ☆ を見出しに付ける。")
		fmt.Fprintln(stderr, "  出力先の sessions/ と index.tsv は実行のたびに書き直す(前回の分は消える。出力先の他のファイルは触らない)。")
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
	env, err := loadRetroEnv(o.config, o.sessions, o.allProjects)
	if err != nil {
		return fail(err)
	}
	outDir := o.out
	if outDir == "" {
		outDir = filepath.Join(os.TempDir(), "braindex-retro")
	}

	ss, sessWarns, err := sessions.Dir{Path: env.sessionsDir}.Sessions(sessions.Options{Since: w.Since, UnderRoot: env.underRoot})
	if err != nil {
		return fail(err)
	}
	warns := append(env.warnings, sessWarns...)
	for _, wn := range warns {
		fmt.Fprintln(stderr, "braindex retro extract: 警告:", wn)
	}
	res := retro.Extract(retro.Input{
		Sessions:    ss,
		Window:      w,
		WindowLabel: label,
		Corrections: env.dicts,
		Sentiment:   retro.Sentiment(),
		Loc:         localLoc,
		Home:        env.home,
	})
	if err := writeExtractOutput(outDir, res); err != nil {
		return fail(err)
	}
	fmt.Fprintf(stdout, "braindex retro extract: %d セッション・発話 %d・訂正 %d → %s\n", res.Sessions, res.UserTurns, res.CorrectionTurns, outDir)
	if len(warns) > 0 {
		fmt.Fprintf(stderr, "braindex retro extract: 警告 %d 件(終了コード 2)\n", len(warns))
		return 2
	}
	return 0
}

// writeExtractOutput は全件を一時保存してから旧ファイルを退避し、今回分を配置する。
// 配置に失敗したら旧ファイルを戻す。旧ファイルの削除は配置が全件成功した後だけ行う。
// 一時ファイルも本人だけが読める権限にする。
func writeExtractOutput(outDir string, res retro.Result) (retErr error) {
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(outDir, ".retro-")
	if err != nil {
		return err
	}
	preserve := false
	defer func() {
		if !preserve {
			retErr = errors.Join(retErr, os.RemoveAll(stage))
		}
	}()
	files := append([]retro.DigestFile(nil), res.Files...)
	files = append(files, retro.DigestFile{RelPath: "index.tsv", Content: res.Index})
	seen := map[string]bool{}
	for _, f := range files {
		rel := filepath.Clean(filepath.FromSlash(f.RelPath))
		if !filepath.IsLocal(rel) || (rel != "index.tsv" && !strings.HasPrefix(rel, "sessions"+string(filepath.Separator))) {
			return fmt.Errorf("不正な出力パス: %s", f.RelPath)
		}
		if seen[rel] {
			return fmt.Errorf("出力パスが重複: %s", f.RelPath)
		}
		seen[rel] = true
		p := filepath.Join(stage, "new", rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return err
		}
		if err := fsutil.WriteAtomic(p, f.Content, 0o600); err != nil {
			return err
		}
	}
	// 前回だけに存在したダイジェストも退避する。無関係な出力先のファイルには触れない。
	var old []string
	err = filepath.WalkDir(filepath.Join(outDir, "sessions"), func(p string, d os.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, err := filepath.Rel(outDir, p)
			if err != nil {
				return err
			}
			old = append(old, rel)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if info, err := os.Lstat(filepath.Join(outDir, "index.tsv")); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("index.tsv が通常ファイルではない")
		}
		old = append(old, "index.tsv")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var backed, installed []string
	rollback := func(cause error) error {
		var restoreErr error
		for i := len(installed) - 1; i >= 0; i-- {
			restoreErr = errors.Join(restoreErr, os.Remove(filepath.Join(outDir, installed[i])))
		}
		for i := len(backed) - 1; i >= 0; i-- {
			rel := backed[i]
			restoreErr = errors.Join(restoreErr, os.Rename(filepath.Join(stage, "old", rel), filepath.Join(outDir, rel)))
		}
		if restoreErr != nil {
			preserve = true
			return errors.Join(cause, fmt.Errorf("旧ファイルの復元に失敗。退避先 %s: %w", stage, restoreErr))
		}
		return cause
	}
	for _, rel := range old {
		backup := filepath.Join(stage, "old", rel)
		if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
			return rollback(err)
		}
		if err := os.Rename(filepath.Join(outDir, rel), backup); err != nil {
			return rollback(err)
		}
		backed = append(backed, rel)
	}
	for _, f := range files {
		rel := filepath.FromSlash(f.RelPath)
		p := filepath.Join(outDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return rollback(err)
		}
		if err := os.Rename(filepath.Join(stage, "new", rel), p); err != nil {
			return rollback(err)
		}
		installed = append(installed, rel)
	}
	return nil
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
		d, err := time.ParseInLocation("2006-01-02", since, localLoc)
		if err != nil {
			return retro.Window{}, "", fmt.Errorf("-since は YYYY-MM-DD で指定する: %q", since)
		}
		return retro.Window{Since: d}, since + " 以降", nil
	case windowDays > 0:
		w := retro.Recent(today, windowDays, localLoc)
		return w, fmt.Sprintf("%s 以降(%d 日)", w.Since.In(localLoc).Format("2006-01-02"), windowDays), nil
	}
	return retro.Window{}, "全期間", nil
}

// retroToday は -date(YYYY-MM-DD・localLoc の 0 時)か、無ければ今(loc.go の todayOrNow)。
func retroToday(date string) (time.Time, error) {
	t, err := todayOrNow(date)
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
	home        string   // 表示でホームを "~" に置き換える(取れなければ "")
	hubDir      string   // 設定ファイルのディレクトリ。設定ファイルが無ければ ""
	underRoot   string   // この配下のセッションだけ数える(空なら絞らない)
	warnings    []string // 環境を決める段で出た警告(セッションの警告の前に出す)
}

// loadRetroEnv は設定ファイル(無ければ既定値)とフラグから実行環境を決める。
// 設定ファイル内のパスは "~" を展開し、相対なら設定ファイルのディレクトリ基準。
// allProjects はフラグ -all-projects。設定 retro.all_projects と同じで、true なら root の外のセッションも数える。
func loadRetroEnv(cfgPath, sessionsFlag string, allProjects bool) (retroEnv, error) {
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
	if found {
		env.hubDir = baseDir
	}
	root, why := sessionRoot(fc.Config.Root, baseDir, s.AllProjects || allProjects, found)
	env.underRoot = root
	if why != "" {
		env.warnings = append(env.warnings, why)
	}

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

	dicts, err := loadRetroDictionaries(s, baseDir, env.home)
	if err != nil {
		return env, err
	}
	env.dicts = dicts
	bins, err := retro.ParseBins(s.PositionBins)
	if err != nil {
		return env, fmt.Errorf("設定 retro.position_bins: %w", err)
	}
	env.bins = bins
	return env, nil
}

// loadRetroDictionaries は訂正辞書を読む: 設定 retro.dictionary があればそれ(無ければ既定の辞書)、retro.dictionary_extra があれば足す。
// 相対パスは baseDir 基準、~ は home に展開する。retro と learn が共有する(辞書の解決規則を 1 か所に置く)。
func loadRetroDictionaries(s retro.Settings, baseDir, home string) ([]*retro.Dictionary, error) {
	var out []*retro.Dictionary
	if s.Dictionary != "" {
		d, err := retro.Load(retro.ResolvePath(s.Dictionary, baseDir, home))
		if err != nil {
			return nil, fmt.Errorf("設定 retro.dictionary: %w", err)
		}
		out = append(out, d)
	} else {
		out = append(out, retro.Corrections())
	}
	if s.DictionaryExtra != "" {
		d, err := retro.Load(retro.ResolvePath(s.DictionaryExtra, baseDir, home))
		if err != nil {
			return nil, fmt.Errorf("設定 retro.dictionary_extra: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// baselineClause は check の 1 行に挟む基準期間の部分。基準を使わないときは空。
func baselineClause(base retro.Window, total retro.Count, weeks int) string {
	if base.Since.IsZero() {
		return ""
	}
	if total.Utterances < retro.MinBaselineTurns {
		return fmt.Sprintf(" / 基準 %d 週は材料不足(発話 %d・%d 未満)", weeks, total.Utterances, retro.MinBaselineTurns)
	}
	return fmt.Sprintf(" / 基準 %s(発話 %d・%d 週)", total.Percent(), total.Utterances, weeks)
}

// verdictText は判定の言い方。鳴らすときだけ次の一手を書く。
func verdictText(v retro.Verdict) string {
	switch v {
	case retro.Exceed:
		return "閾値を超えた。レトロスペクティブの時期(braindex retro extract で材料を出す)"
	case retro.SameAsBaseline:
		return "閾値は超えたが基準と同水準(鳴らさない)"
	default:
		return "閾値以下"
	}
}
