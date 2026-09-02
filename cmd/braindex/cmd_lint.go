package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pilefort/braindex/internal/lint"
	"github.com/pilefort/braindex/internal/scan"
)

func init() {
	register(&command{
		name:    "lint",
		summary: "work/ISSUE-*.md が規約の形か検査する(索引には載せない)",
		run:     runLint,
	})
}

// lintTarget は検査する 1 ファイル。
type lintTarget struct {
	display string // 表示用パス(/ 区切り)
	path    string // 実パス
}

// runLint は braindex lint [フラグ] [パス ...] を実行する。
// パスを渡せばそのファイル(ディレクトリなら直下の ISSUE-*.md)を、渡さなければ root 直下の各リポの
// work/ISSUE-*.md を検査する。指摘は stdout に「パス:行: 内容」で出す。
// 終了コード: 0 指摘なし / 1 失敗(フラグ・root・パスの誤り) / 2 指摘あり。
func runLint(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("braindex lint", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o options
	var staleDays int
	var noGit bool
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json)。パスを渡さないときの root の取得に使う")
	fs.StringVar(&o.root, "root", "", "走査のルート。直下の各リポの work/ISSUE-*.md を検査する(設定ファイルの root より優先)")
	fs.StringVar(&o.date, "date", "", "基準日 YYYY-MM-DD(既定: 今日)。最終更新の未来判定と経過日数に使う")
	fs.IntVar(&staleDays, "stale-days", 0, "最終更新からこの日数以上たった ISSUE を指摘する(0 で見ない)")
	fs.BoolVar(&noGit, "no-git", false, "git HEAD との比較(チェック項目の消失・最終更新の据え置き)をしない")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex lint [フラグ] [パス ...]")
		fmt.Fprintln(stderr, "  パスを渡せばそのファイル(ディレクトリなら直下の ISSUE-*.md)を、渡さなければ root 直下の各リポの work/ISSUE-*.md を検査する。")
		fmt.Fprintln(stderr, "  指摘は stdout に「パス:行: 内容」で出す。終了コード: 0 指摘なし / 1 失敗 / 2 指摘あり")
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
	if staleDays < 0 {
		fmt.Fprintln(stderr, "braindex lint: -stale-days は 0 以上")
		return 1
	}
	today := time.Now()
	if o.date != "" {
		t, err := time.Parse("2006-01-02", o.date)
		if err != nil {
			fmt.Fprintf(stderr, "braindex lint: -date は YYYY-MM-DD で指定する: %q\n", o.date)
			return 1
		}
		today = t
	}

	var targets []lintTarget
	var err error
	if fs.NArg() > 0 {
		targets, err = lintTargetsFromPaths(fs.Args())
	} else {
		targets, err = lintTargetsFromRoot(o)
	}
	if err != nil {
		fmt.Fprintln(stderr, "braindex lint:", err)
		return 1
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].display < targets[j].display })

	var warnings []lint.Warning
	compared := 0
	for _, t := range targets {
		content, rerr := os.ReadFile(t.path)
		if rerr != nil {
			warnings = append(warnings, lint.Warning{Path: t.display, Msg: "読めない: " + scan.DescribeErr(rerr)})
			continue
		}
		dir := filepath.Dir(t.path)
		opt := lint.Options{
			Today:     today,
			StaleDays: staleDays,
			Exists: func(rel string) bool {
				_, serr := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
				return serr == nil
			},
		}
		if !noGit {
			if prev, ok := lint.HeadContent(t.path); ok {
				opt.Prev, opt.HasPrev = prev, true
				compared++
			}
		}
		warnings = append(warnings, lint.Check(t.display, content, opt)...)
	}
	for _, w := range warnings {
		if w.Line > 0 {
			fmt.Fprintf(stdout, "%s:%d: %s\n", w.Path, w.Line, w.Msg)
		} else {
			fmt.Fprintf(stdout, "%s: %s\n", w.Path, w.Msg)
		}
	}
	summary := fmt.Sprintf("braindex lint: %d ファイル・指摘 %d 件", len(targets), len(warnings))
	if compared > 0 {
		summary += fmt.Sprintf("・HEAD 比較 %d 件", compared)
	}
	fmt.Fprintln(stdout, summary)
	if len(warnings) > 0 {
		return 2
	}
	return 0
}

// lintTargetsFromPaths は引数のパスを検査対象にする。ディレクトリなら直下の ISSUE-*.md。存在しないパスは誤り。
func lintTargetsFromPaths(paths []string) ([]lintTarget, error) {
	var ts []lintTarget
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %s", filepath.ToSlash(p), scan.DescribeErr(err))
		}
		if !fi.IsDir() {
			ts = append(ts, lintTarget{display: filepath.ToSlash(p), path: p})
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(p, "ISSUE-*.md")) // パターンは固定なので誤りにならない
		for _, m := range matches {
			ts = append(ts, lintTarget{display: filepath.ToSlash(m), path: m})
		}
	}
	return ts, nil
}

// lintTargetsFromRoot は索引と同じ規則で root を決め、直下の各リポ(. で始まるものは除く)の work/ISSUE-*.md を対象にする。
// 表示パスは root 相対(<リポ>/work/ISSUE-x.md)。
func lintTargetsFromRoot(o options) ([]lintTarget, error) {
	cfg, _, _, err := resolve(o)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(cfg.Root)
	if err != nil {
		return nil, fmt.Errorf("root を読めない: %w", err)
	}
	var ts []lintTarget
	for _, de := range entries {
		if !de.IsDir() || strings.HasPrefix(de.Name(), ".") {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(cfg.Root, de.Name(), "work", "ISSUE-*.md"))
		for _, m := range matches {
			ts = append(ts, lintTarget{display: de.Name() + "/work/" + filepath.Base(m), path: m})
		}
	}
	return ts, nil
}
