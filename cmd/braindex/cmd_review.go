package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	iofs "io/fs" // fs はフラグ集合の変数名に使っている
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/pilefort/braindex/internal/config"
	"github.com/pilefort/braindex/internal/fsutil"
	"github.com/pilefort/braindex/internal/review"
)

func init() {
	register(&command{
		name:    "review",
		summary: "週次レビューの下書き(work/review/YYYY-MM-DD.md)を作る。索引の増減・差分ファイル・放置 TODO・アーカイブ候補は埋め、判断の節は見出しだけ",
		run:     runReview,
	})
}

// reviewOptions は braindex review のコマンドライン。空は「未指定」。
type reviewOptions struct {
	config string // -config。hub の位置を兼ねるので必須(既定パスに無ければエラー)
	date   string // -date。今日の固定(既定: 実行日)
	since  string // -since。前回日(既定: 記録の最新ファイル名 → since_days 日前)
	out    string // -out。出力先(既定: <review.dir>/<今日>.md)
	stdout bool   // -stdout。ファイルに書かず標準出力へ
}

var reviewFileName = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.md$`)

// runReview は braindex review を実行する。
func runReview(args []string, stdout, stderr io.Writer) int {
	var o reviewOptions
	fs := flag.NewFlagSet("braindex review", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json。そのディレクトリを hub とみなす)")
	fs.StringVar(&o.date, "date", "", "今日として使う日付 YYYY-MM-DD(既定: 実行日)。出力ファイル名と閾値の基準。再現可能な出力が要るときに使う")
	fs.StringVar(&o.since, "since", "", "前回レビュー日 YYYY-MM-DD(既定: 記録の置き場にある今日より前で最新の YYYY-MM-DD.md → 無ければ review.since_days 日前)")
	fs.StringVar(&o.out, "out", "", "出力先(既定: 設定 review.dir の <今日>.md)。既にあれば書かない")
	fs.BoolVar(&o.stdout, "stdout", false, "ファイルに書かず標準出力に出す")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex review [-config braindex.json] [-date YYYY-MM-DD] [-since YYYY-MM-DD] [-out <path>] [-stdout]")
		fmt.Fprintln(stderr, "  hub のルートで実行し、週次レビューの下書きを work/review/<今日>.md に書く。機械節(索引の増減・")
		fmt.Fprintln(stderr, "  リポ別の差分ファイル・放置 TODO・アーカイブ候補)は埋まり、判断の節(ダイジェスト・アーカイブ・次アクション)は")
		fmt.Fprintln(stderr, "  見出しだけ。索引 index/catalog.md は読むだけで書き換えない(再生成は braindex)。git はあれば使う。")
		fmt.Fprintln(stderr, "  終了コード: 0 成功 / 1 失敗(何も書かない) / 2 警告つきで完了(git 管理外のリポなどを飛ばした)")
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
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "braindex review: 引数 %q は受け付けない(フラグだけを渡す)\n", fs.Args())
		fs.Usage()
		return 1
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "braindex review:", err)
		return 1
	}

	// 設定ファイル = hub の位置。無ければ動けない(索引生成と違い -root だけでは足りない)
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
	cfg, _, today, err := resolve(options{config: cfgPath, date: o.date})
	if err != nil {
		return fail(err)
	}
	hubDir := filepath.Dir(cfgPath)
	s := fc.Review.WithDefaults()
	reviewDir := filepath.Join(hubDir, filepath.FromSlash(s.Dir))

	since, note, err := resolveSince(o.since, reviewDir, s.Dir, today, s.SinceDays)
	if err != nil {
		return fail(err)
	}
	outPath := o.out
	if outPath == "" {
		outPath = filepath.Join(reviewDir, today+".md")
	}
	if !o.stdout {
		if _, err := os.Lstat(outPath); err == nil {
			return fail(fmt.Errorf("既にある: %s(判断を書き込んだ後の再実行で上書きしない。-stdout で標準出力に出すか、-out で別名を指定する)", outPath))
		} else if !errors.Is(err, iofs.ErrNotExist) {
			return fail(err)
		}
	}

	res, err := review.Build(review.Input{
		Today:      today,
		Since:      since,
		SinceNote:  note,
		Cfg:        cfg,
		HubDir:     hubDir,
		CatalogRel: defaultOut,
		Settings:   s,
	})
	if err != nil {
		return fail(err)
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(stderr, "braindex review: 警告:", w)
	}
	if o.stdout {
		if _, err := stdout.Write(res.Report); err != nil {
			return fail(err)
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return fail(err)
		}
		if err := fsutil.WriteAtomic(outPath, res.Report, 0o644); err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "review 下書き: %s(前回 %s)\n", outPath, since)
	}
	if len(res.Warnings) > 0 {
		fmt.Fprintf(stderr, "braindex review: 警告 %d 件(終了コード 2)\n", len(res.Warnings))
		return 2
	}
	return 0
}

// resolveSince は前回日を決める: -since > 記録の置き場にある今日より前で最新の YYYY-MM-DD.md > sinceDays 日前。
// note は根拠(出力の冒頭に書く)。
func resolveSince(flagSince, reviewDir, dirRel, today string, sinceDays int) (since, note string, err error) {
	if flagSince != "" {
		if _, err := time.Parse("2006-01-02", flagSince); err != nil {
			return "", "", fmt.Errorf("-since は YYYY-MM-DD で指定する: %q", flagSince)
		}
		return flagSince, "-since で指定", nil
	}
	if latest := latestReviewBefore(reviewDir, today); latest != "" {
		return latest, dirRel + "/" + latest + ".md", nil
	}
	since, err = review.DaysBefore(today, sinceDays)
	if err != nil {
		return "", "", err
	}
	return since, fmt.Sprintf("初回のため %d 日前", sinceDays), nil
}

// latestReviewBefore は dir にある YYYY-MM-DD.md のうち today より前で最新の日付を返す。無ければ ""。
// 置き場が無いのは初回なので正常。形だけ日付で実在しない日(2026-08-32.md)は他の名前と同じく無視する
// (採ると前回日に不正な日付が載り、git の --since/--until にもそのまま渡る)。
func latestReviewBefore(dir, today string) string {
	des, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best := ""
	for _, de := range des {
		name := de.Name()
		if de.IsDir() || !reviewFileName.MatchString(name) {
			continue
		}
		d := name[:len(name)-len(".md")]
		if _, err := time.Parse("2006-01-02", d); err != nil {
			continue
		}
		if d < today && d > best {
			best = d
		}
	}
	return best
}
