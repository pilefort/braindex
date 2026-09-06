package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pilefort/braindex/internal/diagnose"
)

func init() {
	register(&command{
		name:    "diagnose",
		summary: "いま何を走査対象にし、何が読めて、何が索引から抜けているかを示す(索引もノートも書き換えない)",
		run:     runDiagnose,
	})
}

// runDiagnose は braindex diagnose [-config] [-root] [-catalog] [-date] [-path <root 相対>] [-json] を実行する。
// 索引の生成と同じ設定・同じ走査で「いま索引を作ったらどうなるか」を出し、保存済みの索引(既定: 設定ファイルと
// 同じディレクトリの index/catalog.md)と突き合わせる。索引も元ノートも書き換えない。
// 終了コード: 0 問題なし / 1 失敗(フラグ・設定・root の誤り。診断は出さない) / 2 要確認(読めなかった範囲・警告・
// 索引の欠落や不一致がある。診断は出す)。
func runDiagnose(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("braindex diagnose", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o options
	var query string
	var asJSON bool
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json。無ければフラグだけで動き、-root が必須)")
	fs.StringVar(&o.root, "root", "", "走査のルート(設定ファイルの root より優先)")
	fs.StringVar(&o.out, "catalog", "", "保存済みの索引のパス(既定: 設定ファイルと同じディレクトリの index/catalog.md)")
	fs.StringVar(&o.date, "date", "", "診断日 YYYY-MM-DD(既定: 今日)。再現可能な出力が要るときに使う")
	fs.StringVar(&query, "path", "", "このパス(root 相対)がいまの設定で対象か・いま見つかるか・索引に載っているかも示す")
	fs.BoolVar(&asJSON, "json", false, "JSON で出す(テキストと同じ内容)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex diagnose [-config <設定>] [-root <ルート>] [-catalog <索引>] [-date YYYY-MM-DD] [-path <root 相対>] [-json]")
		fmt.Fprintln(stderr, "  索引の生成と同じ設定で走査し、いま索引に載る件数・読めなかった範囲・警告と、保存済みの索引との差を示す。")
		fmt.Fprintln(stderr, "  索引に行が無いことを「ノートが無い」と読む前に、対象・除外・確認不能を確かめるための道具。索引もノートも書き換えない。")
		fmt.Fprintln(stderr, "  終了コード: 0 問題なし / 1 失敗 / 2 要確認(読めなかった範囲・警告・索引の欠落や不一致)")
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
		fmt.Fprintf(stderr, "braindex diagnose: 位置引数は受け付けない: %q(パスを問うときは -path)\n", fs.Args())
		return 1
	}

	cfg, catalogPath, date, err := resolve(o)
	if err != nil {
		fmt.Fprintln(stderr, "braindex diagnose:", err)
		return 1
	}
	rep, err := diagnose.Build(diagnose.Input{
		ConfigFile:  configFileUsed(o),
		Cfg:         cfg,
		CatalogPath: catalogPath,
		Date:        date,
		Path:        query,
	})
	if err != nil {
		fmt.Fprintln(stderr, "braindex diagnose:", err)
		return 1
	}
	if asJSON {
		b, err := diagnose.JSON(rep)
		if err != nil {
			fmt.Fprintln(stderr, "braindex diagnose:", err)
			return 1
		}
		stdout.Write(b)
	} else {
		stdout.Write(diagnose.Render(rep))
	}
	if len(rep.Problems) > 0 {
		fmt.Fprintf(stderr, "braindex diagnose: 要確認 %d 件(終了コード 2)\n", len(rep.Problems))
		return 2
	}
	return 0
}

// configFileUsed は resolve が読んだ設定ファイルのパスを返す。既定パスに無ければ ""(フラグだけで動いた)。
// resolve は明示した -config の不在を誤りにするので、ここに来る時点で明示分は必ずある。
func configFileUsed(o options) string {
	p := o.config
	if p == "" {
		p = defaultConfig
	}
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		return ""
	}
	return filepath.Clean(p)
}
