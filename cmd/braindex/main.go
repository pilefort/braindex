// braindex — 複数リポを横断する知識の索引 (index/catalog.md) を決定的に再生成する CLI。
//
// scan(スキャン対象の発見) → extract(タイトル・日付・要旨の抽出) → render(catalog.md 生成)
// の順に処理する。hub リポ(索引を置くリポ)のルートで実行する。
//
// 終了コード:
//   - 0: 成功
//   - 1: 失敗(フラグの誤り・設定・root が読めない等。索引は書かない)
//   - 2: 警告つきで完了(読めないファイルや存在しない extra を飛ばした。索引は書く)
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/pilefort/braindex/internal/catalog"
	"github.com/pilefort/braindex/internal/scan"
)

const (
	defaultConfig = "braindex.json"    // カレントディレクトリ基準
	defaultOut    = "index/catalog.md" // 設定ファイルのディレクトリ基準(設定が無ければカレント)
)

// options はコマンドラインで与える値。空は「未指定」。
type options struct {
	config string // -config。未指定なら既定 braindex.json(無くてもよい)
	root   string // -root。設定ファイルの root より優先
	out    string // -out。未指定なら設定ファイルと同じディレクトリの index/catalog.md
	date   string // -date。未指定なら今日
}

func main() {
	o, code, done := parseArgs(os.Args[1:], os.Stderr)
	if done {
		os.Exit(code)
	}
	os.Exit(run(o, os.Stdout, os.Stderr))
}

// parseArgs はコマンドラインを解釈する。-h、解釈できないフラグ、位置引数のときはメッセージを stderr に
// 出し、done=true と終了コード(-h は 0、それ以外は 1)を返す。
// flag パッケージ既定の ExitOnError は誤りで 2 を返すが、2 は「警告つきで完了」に使っているので区別する。
// 位置引数はサブコマンド未実装のうちは受け付けない(`braindex init -root x` が黙って通常の走査に入らないように)。
func parseArgs(args []string, stderr io.Writer) (o options, code int, done bool) {
	fs := flag.NewFlagSet("braindex", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json。無ければフラグだけで動き、-root が必須)")
	fs.StringVar(&o.root, "root", "", "走査のルート。直下の各ディレクトリを 1 リポとみなす(設定ファイルの root より優先)")
	fs.StringVar(&o.out, "out", "", "索引の出力先(既定: 設定ファイルと同じディレクトリの index/catalog.md)")
	fs.StringVar(&o.date, "date", "", "先頭行に載せる生成日 YYYY-MM-DD(既定: 今日)。再現可能な出力が要るときに使う")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return o, 0, true
		}
		return o, 1, true // fs.Parse が誤りと使い方を stderr に書いている
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "braindex: 引数 %q は受け付けない(フラグだけを渡す)\n", fs.Args())
		fs.Usage()
		return o, 1, true
	}
	return o, 0, false
}

// run は終了コードを返す。メッセージは stdout / stderr に書く(テストから差し替えられるように引数で受ける)。
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
	for _, w := range res.Warnings {
		fmt.Fprintln(stderr, "braindex: 警告:", w)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		fmt.Fprintln(stderr, "braindex:", err)
		return 1
	}
	if err := os.WriteFile(outPath, res.Catalog, 0o644); err != nil {
		fmt.Fprintln(stderr, "braindex:", err)
		return 1
	}
	fmt.Fprintf(stdout, "catalog 生成: %d 件 → %s\n", res.Entries, outPath)
	if len(res.Warnings) > 0 {
		fmt.Fprintf(stderr, "braindex: 警告 %d 件(終了コード 2)\n", len(res.Warnings))
		return 2
	}
	return 0
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
	cfg, found, err := loadConfig(cfgPath)
	if err != nil {
		return cfg, "", "", err
	}
	if explicit && !found {
		return cfg, "", "", fmt.Errorf("設定ファイルが見つからない: %s", cfgPath)
	}
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

// loadConfig は設定ファイル(JSON)を読む。ファイルが無ければ found=false でゼロ値を返す(エラーにしない)。
// 未知のキーと、オブジェクトの後ろに続く余分な内容はエラーにする(notes_dir のような打ち間違いや
// 壊れたファイルを無言で通さないため。Decoder は先頭の 1 値しか読まないので末尾を自分で確かめる)。
func loadConfig(path string) (cfg scan.Config, found bool, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, false, nil
		}
		return cfg, false, fmt.Errorf("設定ファイルを読めない: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, true, fmt.Errorf("設定ファイル %s: %w", path, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return cfg, true, fmt.Errorf("設定ファイル %s: 末尾に余分な内容がある(JSON のオブジェクト 1 つだけを書く)", path)
	}
	return cfg, true, nil
}

// joinIfRelative は p が相対パスなら base と結合し、絶対パスならそのまま返す。
func joinIfRelative(base, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}
