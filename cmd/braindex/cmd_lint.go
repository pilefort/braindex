package main

import (
	"encoding/json"
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
		summary: "work/ISSUE-*.md が規約の形か、ノートが曖昧でないかを検査する(索引には載せない)",
		run:     runLint,
	})
}

// lintTarget は検査する 1 ファイル。
type lintTarget struct {
	display string // 表示用パス(/ 区切り)
	path    string // 実パス
}

// 検査の種別。
const (
	kindAuto  = ""      // パスで判別する(ISSUE-*.md は issue、それ以外は note)
	kindIssue = "issue" // work/ISSUE-*.md の形の検査
	kindNote  = "note"  // ノートの曖昧さ検査
)

// isIssuePath は ISSUE-*.md か(ファイル名だけで見る)。
func isIssuePath(p string) bool {
	base := filepath.Base(p)
	return strings.HasPrefix(base, "ISSUE-") && strings.HasSuffix(base, ".md")
}

// runLint は braindex lint [フラグ] [パス ...] を実行する。
// パスを渡せばそのファイル(ディレクトリなら直下の ISSUE-*.md。-kind note なら直下の *.md)を、渡さなければ root の
// 各リポ(repo_depth 段下。既定は直下)の work/ISSUE-*.md を検査する。ISSUE-*.md は形の検査、それ以外の .md はノートの曖昧さ検査(-kind で固定できる)。
// 指摘は stdout に「パス:行: 内容」(ノート検査は内容の先頭に「[種別]」)で出す。-json なら指摘の配列を JSON で出す。
// 終了コード: 0 指摘なし / 1 失敗(フラグ・root・パスの誤り) / 2 指摘あり。
func runLint(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("braindex lint", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o options
	var staleDays int
	var noGit, asJSON bool
	var kind, glossary string
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json)。パスを渡さないときの root の取得に使う")
	fs.StringVar(&o.root, "root", "", "走査のルート。各リポ(設定の repo_depth 段下。既定は直下)の work/ISSUE-*.md を検査する(設定ファイルの root より優先)")
	fs.StringVar(&o.date, "date", "", "基準日 YYYY-MM-DD(既定: 今日)。最終更新の未来判定と経過日数に使う")
	fs.IntVar(&staleDays, "stale-days", 0, "最終更新からこの日数以上たった ISSUE を指摘する(0 で見ない)")
	fs.BoolVar(&noGit, "no-git", false, "git HEAD との比較(チェック項目の消失・最終更新の据え置き)をしない")
	fs.StringVar(&kind, "kind", kindAuto, "検査の種別 issue|note(既定: ISSUE-*.md は issue、それ以外の .md は note)")
	fs.StringVar(&glossary, "glossary", "", "ノート検査で未定義用語を突き合わせる用語集のパス(既定: ノートのあるリポの docs/glossary.md。無ければ見ない)")
	fs.BoolVar(&asJSON, "json", false, "指摘を JSON の配列で出す(path・line・msg・kind・severity)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex lint [フラグ] [パス ...]")
		fmt.Fprintln(stderr, "  パスを渡せばそのファイル(ディレクトリなら直下の ISSUE-*.md。-kind note なら直下の *.md)を、渡さなければ root の各リポ")
		fmt.Fprintln(stderr, "  (設定の repo_depth 段下。既定は直下)の work/ISSUE-*.md を検査する。ISSUE-*.md は規約の形を、それ以外の .md は曖昧さ(数量詞・日付なし・出典なき数字・裸のヘッジ・")
		fmt.Fprintln(stderr, "  なぜ欠落・根拠欠落・未定義用語)を見る。指摘は stdout に「パス:行: 内容」で出す。終了コード: 0 指摘なし / 1 失敗 / 2 指摘あり")
		fmt.Fprintln(stderr, "  decisions.md は ## 見出し直下の失効・一部失効行の書式、絶対日付、後継見出しの実在(前方一致)も検査する")
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
	if kind != kindAuto && kind != kindIssue && kind != kindNote {
		fmt.Fprintf(stderr, "braindex lint: -kind は issue か note: %q\n", kind)
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
		targets, err = lintTargetsFromPaths(fs.Args(), kind == kindNote)
	} else {
		var ws []string
		targets, ws, err = lintTargetsFromRoot(o)
		for _, w := range ws {
			fmt.Fprintln(stderr, "braindex lint:", w)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "braindex lint:", err)
		return 1
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].display < targets[j].display })

	var glossaryContent []byte
	if glossary != "" {
		glossaryContent, err = os.ReadFile(glossary)
		if err != nil {
			fmt.Fprintf(stderr, "braindex lint: 用語集を読めない: %s: %s\n", filepath.ToSlash(glossary), scan.DescribeErr(err))
			return 1
		}
	}

	warnings := []lint.Warning{}
	compared := 0
	for _, t := range targets {
		content, rerr := os.ReadFile(t.path)
		if rerr != nil {
			warnings = append(warnings, lint.Warning{Path: t.display, Msg: "読めない: " + scan.DescribeErr(rerr)})
			continue
		}
		dir := filepath.Dir(t.path)
		k := kind
		if k == kindAuto {
			k = kindNote
			if isIssuePath(t.path) {
				k = kindIssue
			}
		}
		if k == kindNote {
			nopt := lint.NoteOptions{Glossary: glossaryContent, HasGlossary: glossary != ""}
			if glossary == "" {
				if g, ok := findGlossary(dir); ok {
					if b, gerr := os.ReadFile(g); gerr == nil {
						nopt.Glossary, nopt.HasGlossary = b, true
					}
				}
			}
			warnings = append(warnings, lint.CheckNote(t.display, content, nopt)...)
			continue
		}
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
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(warnings); err != nil {
			fmt.Fprintln(stderr, "braindex lint:", err)
			return 1
		}
		if len(warnings) > 0 {
			return 2
		}
		return 0
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

// lintTargetsFromPaths は引数のパスを検査対象にする。ディレクトリなら直下の ISSUE-*.md(notes なら直下の *.md)。存在しないパスは誤り。
func lintTargetsFromPaths(paths []string, notes bool) ([]lintTarget, error) {
	pattern := "ISSUE-*.md"
	if notes {
		pattern = "*.md"
	}
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
		matches, _ := filepath.Glob(filepath.Join(p, pattern)) // パターンは固定なので誤りにならない
		for _, m := range matches {
			ts = append(ts, lintTarget{display: filepath.ToSlash(m), path: m})
		}
	}
	return ts, nil
}

// lintTargetsFromRoot は索引と同じ規則で root とリポを決め(scan.ListRepos。repo_depth 段下・. で始まるものは除く)、
// 各リポの work/ISSUE-*.md を対象にする。表示パスは root 相対(<リポ>/work/ISSUE-x.md)。
// 列挙できなかった group は warnings に積む(そこにリポが無いのか読めなかったのかは分からないので、無言にしない)。
func lintTargetsFromRoot(o options) (ts []lintTarget, warnings []string, err error) {
	cfg, _, _, err := resolve(o)
	if err != nil {
		return nil, nil, err
	}
	repos, gaps, err := scan.ListRepos(cfg.Root, cfg.Depth())
	if err != nil {
		return nil, nil, err
	}
	for _, g := range gaps {
		warnings = append(warnings, fmt.Sprintf("%s: %s", g.Rel, g.Reason))
	}
	for _, r := range repos {
		matches, _ := filepath.Glob(filepath.Join(r.Dir, "work", "ISSUE-*.md"))
		for _, m := range matches {
			ts = append(ts, lintTarget{display: r.Name + "/work/" + filepath.Base(m), path: m})
		}
	}
	return ts, warnings, nil
}

// findGlossary は dir から親へさかのぼり、最初に見つかった docs/glossary.md のパスを返す(ノートのあるリポの用語集)。
func findGlossary(dir string) (string, bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for d := abs; ; {
		g := filepath.Join(d, "docs", "glossary.md")
		if fi, err := os.Stat(g); err == nil && !fi.IsDir() {
			return g, true
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", false
		}
		d = parent
	}
}
