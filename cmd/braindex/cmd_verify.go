package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/pilefort/braindex/internal/verify"
)

func init() {
	register(&command{
		name:    "verify",
		summary: "GitHub・arXiv・URL・引用の実在とセッションの実行記録を照合する",
		run:     runVerify,
	})
}

// newFetcher は照合に使う取得器。テストで固定レスポンスに差し替える。
var newFetcher = func() verify.Fetcher { return verify.NewHTTPFetcher() }

// runVerify は braindex verify [フラグ] <github|arxiv|url|quote|session> <引数...> を実行する。
// 外へ送るのは公開 URL・リポ名・arXiv ID だけ(GET のみ)。判定は実在と一致だけで、真偽の意味判断はしない。
// 出力は 1 件 1 行「種別 <TAB> 対象 <TAB> 判定 <TAB> 実測」。-json で配列。
// 終了コード: 0 全件 FOUND / 2 NOT FOUND・FAILED あり / 1 失敗(ERROR あり・引数の誤り)。
func runVerify(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("braindex verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var asJSON bool
	fs.BoolVar(&asJSON, "json", false, "結果を JSON 配列で出す")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex verify [フラグ] github <owner/repo ...>")
		fmt.Fprintln(stderr, "        braindex verify [フラグ] arxiv <ID ...>")
		fmt.Fprintln(stderr, "        braindex verify [フラグ] url <URL ...>")
		fmt.Fprintln(stderr, "        braindex verify [フラグ] quote <出典 URL> <逐語引用 ...>")
		fmt.Fprintln(stderr, "        braindex verify [フラグ] session <セッション ID または .jsonl パス>")
		fmt.Fprintln(stderr, "  session はローカルの実行記録を照合する。")
		fmt.Fprintln(stderr, "  一次ソースへ GET して実在・一致を照合する(本文は送らない)。quote は空白の揺れだけ許し、24 字未満は照合しない。")
		fmt.Fprintln(stderr, "  出力は 1 件 1 行(種別・対象・判定・実測のタブ区切り)。終了コード: 0 全件 FOUND / 2 NOT FOUND・FAILED あり / 1 失敗")
		fmt.Fprintln(stderr, "  GITHUB_TOKEN があれば GitHub API の認証に使う(任意・レート制限対策)")
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
	rest := fs.Args()
	if len(rest) < 2 {
		fs.Usage()
		return 1
	}
	kind, targets := rest[0], rest[1:]
	f := newFetcher()
	var results []verify.Result
	switch kind {
	case "session":
		if len(targets) != 1 {
			fmt.Fprintln(stderr, "braindex verify: session には対象を 1 つ指定してください")
			return 1
		}
		results = verify.Session(targets[0])
	case "github":
		for _, t := range targets {
			results = append(results, verify.GitHub(f, t))
		}
	case "arxiv":
		for _, t := range targets {
			results = append(results, verify.Arxiv(f, t))
		}
	case "url":
		for _, t := range targets {
			results = append(results, verify.URL(f, t))
		}
	case "quote":
		if len(targets) < 2 {
			fmt.Fprintln(stderr, "braindex verify: quote には <出典 URL> と引用文(1 つ以上)が必要")
			return 1
		}
		results = verify.Quotes(f, targets[0], targets[1:])
	default:
		fmt.Fprintf(stderr, "braindex verify: 種別は github / arxiv / url / quote / session のいずれか: %q\n", kind)
		return 1
	}

	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			fmt.Fprintf(stderr, "braindex verify: %v\n", err)
			return 1
		}
	} else {
		for _, r := range results {
			fmt.Fprintln(stdout, r.Line())
		}
	}
	code := 0
	for _, r := range results {
		switch r.Status {
		case verify.Error:
			return 1
		case verify.NotFound, verify.Failed:
			code = 2
		}
	}
	return code
}
