package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pilefort/braindex/internal/mdhtml"
	"github.com/pilefort/braindex/internal/news"
)

// runNewsOverview は会話で書いた「まとめて概要」の Markdown を、記事ごとに仕分けできる HTML にして開く。
// 選別が終わったあとの流れ: まとめて概要を頼む → この画面で仕分ける → 詳しく知りたい分だけをまとめて頼む。
// 外へは何も送らない(ローカルのファイルを読んで、ローカルに書くだけ)。
func runNewsOverview(args []string, stdout, stderr io.Writer) int {
	var out string
	var noOpen bool
	flags := flag.NewFlagSet("braindex news overview", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&out, "out", "", "書き出す HTML のパス（既定は元の Markdown と同じ場所・同じ名前の .html）")
	flags.BoolVar(&noOpen, "no-open", false, "書くだけで開かない")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex news overview [-out FILE] [-no-open] <概要.md>")
		fmt.Fprintln(stderr, "記事の区切りは <!--"+news.OverviewMark+" id=記事ID question=相談ID link=URL--> の行。")
		fmt.Fprintln(stderr, "記事ごとに「詳しく知りたい / 概要で足りた / 興味なし」を選べ、詳しく知りたい分の相談文をまとめて作れる。")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	fail := func(err error) int { fmt.Fprintln(stderr, "braindex news overview:", err); return 1 }
	if flags.NArg() != 1 {
		flags.Usage()
		return 1
	}
	src := flags.Arg(0)
	b, err := os.ReadFile(src)
	if err != nil {
		return fail(err)
	}
	md := string(b)
	intro, arts := news.ParseOverview(md)
	if len(arts) == 0 {
		return fail(fmt.Errorf("記事の区切りが 1 つも無い: %s\n  各記事の前に <!--%s id=記事ID question=相談ID link=URL--> を置く", src, news.OverviewMark))
	}
	missing := 0
	for _, a := range arts {
		if a.ID == "" {
			missing++
		}
	}
	if missing > 0 {
		return fail(fmt.Errorf("%d 件の区切りに id が無い（登録コマンドを作れない）", missing))
	}
	if out == "" {
		out = strings.TrimSuffix(src, filepath.Ext(src)) + ".html"
	}
	abs, err := filepath.Abs(out)
	if err != nil {
		return fail(err)
	}
	// 保存の鍵は出力先のパスから作る。同じ概要を開き直すと前の仕分けが戻る
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.ToSlash(abs))))
	page := news.RenderOverview(hex.EncodeToString(sum[:8]), mdhtml.ExtractTitle(md, filepath.Base(src)), intro, arts)
	if err := os.WriteFile(abs, page, 0644); err != nil {
		return fail(err)
	}
	fmt.Fprintln(stdout, "概要の画面:", abs)
	fmt.Fprintf(stdout, "記事 %d 件。仕分けたら「詳しく知りたい分を頼む」で相談文を作れる\n", len(arts))
	if noOpen {
		return 0
	}
	if err := openInBrowser(abs); err != nil {
		fmt.Fprintln(stderr, "braindex news overview: 警告: ブラウザで開けない:", err)
		return 2
	}
	return 0
}
