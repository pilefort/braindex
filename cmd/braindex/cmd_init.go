package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/pilefort/braindex/internal/template"
)

func init() {
	register(&command{
		name:    "init",
		summary: "hub リポの雛形を展開する(既存ファイルは上書きしない)",
		run:     runInit,
	})
}

// runInit は braindex init [dir] を実行する。dir 省略時はカレントディレクトリ。
// 既存ファイルは残すので再実行しても安全。
func runInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("braindex init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex init [dir]")
		fmt.Fprintln(stderr, "  dir(既定: カレントディレクトリ)に hub リポの雛形を展開する: README・CLAUDE.md・braindex.json・")
		fmt.Fprintln(stderr, "  docs/・work/(work/review/ を含む)・週次レビューのスキル。既存ファイルは残すので、再実行しても安全。")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	dir := "."
	switch fs.NArg() {
	case 0:
	case 1:
		dir = fs.Arg(0)
	default:
		fmt.Fprintf(stderr, "braindex init: ディレクトリは 1 つまで(%d 個指定された)\n", fs.NArg())
		return 1
	}

	res, err := template.Install(dir, template.KindHub)
	if err != nil {
		fmt.Fprintln(stderr, "braindex init:", err)
		return 1
	}
	for _, p := range res.Created {
		fmt.Fprintln(stdout, "作成:", p)
	}
	for _, p := range res.Skipped {
		fmt.Fprintln(stdout, "保持(既存):", p)
	}
	fmt.Fprintf(stdout, "braindex init: 作成 %d・保持 %d(%s)\n", len(res.Created), len(res.Skipped), dir)
	if len(res.Created) > 0 {
		fmt.Fprintln(stdout, "次: braindex.json の root を確認し(\"..\" は各リポの親ディレクトリ)、`braindex` を実行して index/catalog.md を作る")
	}
	return 0
}
