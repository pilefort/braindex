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
		summary: "hub リポの雛形を展開する(-repo で各プロジェクトのリポ側の骨格)。既存ファイルは上書きしない",
		run:     runInit,
	})
}

// runInit は braindex init [-repo] [dir] を実行する。dir 省略時はカレントディレクトリ。
// -repo を付けると hub でなく各プロジェクトのリポ側の骨格(docs/notes・docs/decisions.md・work/)を置く。
// 既存ファイルは残すので再実行しても安全。
func runInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("braindex init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.Bool("repo", false, "hub でなく各プロジェクトのリポ側の骨格(docs/notes/{common,project}・docs/decisions.md・work/)を置く")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex init [-repo] [dir]")
		fmt.Fprintln(stderr, "  dir(既定: カレントディレクトリ)に hub リポの雛形を展開する: README・CLAUDE.md・braindex.json・")
		fmt.Fprintln(stderr, "  docs/・work/(work/review/ を含む)・スキル 4 本(braindex-review・retro・record-lint・contradiction-scan)。")
		fmt.Fprintln(stderr, "  既存ファイルは残すので、再実行しても安全。")
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
	dir := "."
	switch fs.NArg() {
	case 0:
	case 1:
		dir = fs.Arg(0)
	default:
		fmt.Fprintf(stderr, "braindex init: ディレクトリは 1 つまで(%d 個指定された)\n", fs.NArg())
		return 1
	}

	kind := template.KindHub
	if *repo {
		kind = template.KindRepo
	}
	res, err := template.Install(dir, kind)
	// 途中で失敗しても、そこまでに作った／残したものは列挙する(書いたものを無言にしない)
	for _, p := range res.Created {
		fmt.Fprintln(stdout, "作成:", p)
	}
	for _, p := range res.Skipped {
		fmt.Fprintln(stdout, "保持(既存):", p)
	}
	if err != nil {
		fmt.Fprintln(stderr, "braindex init:", err)
		return 1
	}
	fmt.Fprintf(stdout, "braindex init: 作成 %d・保持 %d(%s)\n", len(res.Created), len(res.Skipped), dir)
	if len(res.Created) > 0 && kind == template.KindHub {
		fmt.Fprintln(stdout, "次: braindex.json の root を確認し(\"..\" は各リポの親ディレクトリ)、`braindex` を実行して index/catalog.md を作る")
		fmt.Fprintln(stdout, "  週次レビューと訂正率の確認を定期実行にするなら `braindex schedule install`(先に `braindex schedule print` で中身を見られる)")
	}
	return 0
}
