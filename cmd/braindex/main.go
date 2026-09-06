// braindex — 複数リポを横断する知識の索引 (index/catalog.md) を決定的に再生成する CLI。
//
// 引数無し(フラグのみ)なら索引を生成する: scan(スキャン対象の発見) → extract(タイトル・日付・
// 要旨の抽出) → render(catalog.md 生成)。hub リポ(索引を置くリポ)のルートで実行する。
// 最初の引数がサブコマンド名(init など)なら、そのサブコマンドを実行する(commands.go)。
//
// 終了コード:
//   - 0: 成功
//   - 1: 失敗(フラグの誤り・設定・root が読めない等。索引は書かない)
//   - 2: 警告つきで完了(読めないファイルや存在しない extra を飛ばした。索引は書く)
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

	"github.com/pilefort/braindex/internal/catalog"
	"github.com/pilefort/braindex/internal/changehistory"
	"github.com/pilefort/braindex/internal/config"
	"github.com/pilefort/braindex/internal/fsutil"
	"github.com/pilefort/braindex/internal/scan"
)

const (
	defaultConfig = config.DefaultPath // カレントディレクトリ基準
	defaultOut    = "index/catalog.md" // 設定ファイルのディレクトリ基準(設定が無ければカレント)
)

// options は索引生成のコマンドラインで与える値。空は「未指定」。
type options struct {
	config  string // -config。未指定なら既定 braindex.json(無くてもよい)
	root    string // -root。設定ファイルの root より優先
	out     string // -out。未指定なら設定ファイルと同じディレクトリの index/catalog.md
	date    string // -date。未指定なら今日
	version bool   // -version。版を 1 行出して終わる
}

func main() {
	os.Exit(dispatch(os.Args[1:], os.Stdout, os.Stderr))
}

// dispatch は最初の引数が登録済みのサブコマンド名ならそれを実行し、そうでなければフラグを解釈して
// 索引を生成する。未登録の語は parseArgs が位置引数として拒否する(終了コード 1)。
func dispatch(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		if c, ok := commands[args[0]]; ok {
			return c.run(args[1:], stdout, stderr)
		}
	}
	o, code, done := parseArgs(args, stderr)
	if done {
		return code
	}
	if o.version {
		fmt.Fprintln(stdout, versionLine())
		return 0
	}
	return run(o, stdout, stderr)
}

// parseArgs は索引生成のコマンドラインを解釈する。-h、解釈できないフラグ、位置引数のときはメッセージを
// stderr に出し、done=true と終了コード(-h は 0、それ以外は 1)を返す。
// flag パッケージ既定の ExitOnError は誤りで 2 を返すが、2 は「警告つきで完了」に使っているので区別する。
// 位置引数は受け付けない(登録済みのサブコマンドは dispatch が先に拾う。それ以外の語が黙って通常の走査に
// 入らないようにする)。
func parseArgs(args []string, stderr io.Writer) (o options, code int, done bool) {
	fs := flag.NewFlagSet("braindex", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json。無ければフラグだけで動き、-root が必須)")
	fs.StringVar(&o.root, "root", "", "走査のルート。直下の各ディレクトリを 1 リポとみなす(設定ファイルの root より優先)")
	fs.StringVar(&o.out, "out", "", "索引の出力先(既定: 設定ファイルと同じディレクトリの index/catalog.md)")
	fs.StringVar(&o.date, "date", "", "先頭行に載せる生成日 YYYY-MM-DD(既定: 今日)。再現可能な出力が要るときに使う")
	fs.BoolVar(&o.version, "version", false, "入っている braindex の版を 1 行出して終わる")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方:")
		fmt.Fprintln(stderr, "  braindex [フラグ]              索引(index/catalog.md)を生成する")
		fmt.Fprintln(stderr, "  braindex <コマンド> [引数]     サブコマンドを実行する(フラグは braindex <コマンド> -h)")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "フラグ:")
		fs.PrintDefaults()
		if len(commands) > 0 {
			fmt.Fprintln(stderr)
			fmt.Fprintln(stderr, "コマンド:")
			printCommands(stderr)
		}
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return o, 0, true
		}
		return o, 1, true // fs.Parse が誤りと使い方を stderr に書いている
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "braindex: 引数 %q は受け付けない(サブコマンドの一覧は braindex -h。索引生成はフラグだけを渡す)\n", fs.Args())
		fs.Usage()
		return o, 1, true
	}
	return o, 0, false
}

// run は索引を生成し、終了コードを返す。メッセージは stdout / stderr に書く(テストから差し替えられるように引数で受ける)。
//
// 索引と一緒に、本文の変更の記録(索引と同じディレクトリの changes.json)も更新する。索引の行は本文の後半だけの
// 変更では変わらないので、本文のハッシュと観測日を別に持つ(changehistory)。記録が読めない(壊れている)ときは
// 警告して記録を触らず、索引だけ書く——黙って作り直すと前回の観測を失うため。記録を書けなかったときも警告に
// とどめる(索引は書けているので失敗にしない。観測は次の生成で追いつく)。どちらも終了コード 2。
func run(o options, stdout, stderr io.Writer) int {
	cfg, outPath, genDate, err := resolve(o)
	if err != nil {
		fmt.Fprintln(stderr, "braindex:", err)
		return 1
	}
	res, err := catalog.Build(cfg, genDate)
	if err != nil {
		fmt.Fprintln(stderr, "braindex:", err)
		return 1
	}
	warnings := res.Warnings
	warn := func(format string, a ...any) {
		w := fmt.Sprintf(format, a...)
		warnings = append(warnings, w)
		fmt.Fprintln(stderr, "braindex: 警告:", w)
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(stderr, "braindex: 警告:", w)
	}
	// 記録は索引を書く前に読む(壊れていれば、索引は書くが記録は据え置く)
	histPath := filepath.Join(filepath.Dir(outPath), changehistory.FileName)
	prev, herr := changehistory.Load(histPath)
	if herr != nil {
		warn("本文の変更の記録を読めない: %v(記録は更新しない。直すか、ファイルごと消して観測をやり直す)", herr)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		fmt.Fprintln(stderr, "braindex:", err)
		return 1
	}
	if err := fsutil.WriteAtomic(outPath, res.Catalog, 0o644); err != nil {
		fmt.Fprintln(stderr, "braindex:", err)
		return 1
	}
	fmt.Fprintf(stdout, "catalog 生成: %d 件 → %s\n", res.Entries, outPath)
	if herr == nil {
		next, rep := changehistory.Update(prev, res.Notes, res.Coverage.Gaps, genDate)
		if err := changehistory.Save(histPath, next); err != nil {
			warn("本文の変更の記録を書けない: %v(次の生成で観測し直す)", err)
		} else {
			fmt.Fprintln(stdout, describeChanges(rep, histPath))
		}
	}
	if len(warnings) > 0 {
		fmt.Fprintf(stderr, "braindex: 警告 %d 件(終了コード 2)\n", len(warnings))
		return 2
	}
	return 0
}

// describeChanges は本文の変更の記録を更新した結果を 1 行にする。
func describeChanges(rep changehistory.Report, path string) string {
	if rep.Initial {
		return fmt.Sprintf("本文の観測を開始: %d 件を記録(いつ変わったかは不明)→ %s", rep.Total, path)
	}
	var b strings.Builder
	if rep.New+rep.Changed+rep.Reappeared+rep.Missing == 0 {
		b.WriteString("本文の変更: なし")
	} else {
		fmt.Fprintf(&b, "本文の変更: 変更 %d・新規 %d・見当たらない %d", rep.Changed, rep.New, rep.Missing)
		if rep.Reappeared > 0 {
			fmt.Fprintf(&b, "・再出現 %d", rep.Reappeared)
		}
	}
	if rep.Held > 0 {
		fmt.Fprintf(&b, "(読めなかった範囲の %d 件は前回のまま)", rep.Held)
	}
	fmt.Fprintf(&b, " → %s", path)
	return b.String()
}

// resolve はフラグと設定ファイルを合成して、走査設定・出力先・生成日を決める。
//
// 優先順位はフラグ > 設定ファイル > 既定値。設定ファイル内の相対パス(root)は設定ファイルの
// ディレクトリ基準、フラグの相対パスはカレントディレクトリ基準で解決する。
func resolve(o options) (cfg scan.Config, outPath, genDate string, err error) {
	cfgPath, explicit := o.config, o.config != ""
	if !explicit {
		cfgPath = defaultConfig
	}
	fc, found, err := config.Load(cfgPath)
	if err != nil {
		return cfg, "", "", err
	}
	if explicit && !found {
		return cfg, "", "", fmt.Errorf("設定ファイルが見つからない: %s", cfgPath)
	}
	cfg = fc.Config
	baseDir := "."
	if found {
		baseDir = filepath.Dir(cfgPath)
	}

	switch {
	case o.root != "":
		cfg.Root = o.root
	case cfg.Root != "":
		cfg.Root = joinIfRelative(baseDir, filepath.FromSlash(cfg.Root))
	default:
		where := fmt.Sprintf("設定ファイル %s に root が無く", cfgPath)
		if !found {
			where = fmt.Sprintf("設定ファイル %s が無く", cfgPath)
		}
		return cfg, "", "", fmt.Errorf("root が未指定: %s、-root も無い(-root を渡すか、設定ファイルに root を書く)", where)
	}

	outPath = o.out
	if outPath == "" {
		outPath = filepath.Join(baseDir, filepath.FromSlash(defaultOut))
	}

	genDate = o.date
	if genDate == "" {
		genDate = time.Now().Format("2006-01-02")
	} else if _, perr := time.Parse("2006-01-02", genDate); perr != nil {
		return cfg, "", "", fmt.Errorf("-date は YYYY-MM-DD で指定する: %q", genDate)
	}
	return cfg, outPath, genDate, nil
}

// joinIfRelative は p が相対パスなら base と結合し、絶対パスならそのまま返す。
func joinIfRelative(base, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}
