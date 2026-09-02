package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	iofs "io/fs" // fs はフラグ集合の変数名に使っている
	"os"
	"path/filepath"
	"time"

	"github.com/pilefort/braindex/internal/config"
	"github.com/pilefort/braindex/internal/feed"
	"github.com/pilefort/braindex/internal/news"
)

func init() {
	register(&command{
		name:    "news",
		summary: "ニュースサジェスト。news fetch でフィードを取得し、新着のダイジェスト(news/digest_<日付>_<層>.md)を書く",
		run:     runNews,
	})
}

// newsFetcher は fetch が使う取得器。テストで差し替える(ネットワークに出ないため)。
var newsFetcher news.Fetcher = feed.Fetcher{}

// runNews は braindex news <サブコマンド> を振り分ける。いまは fetch だけ(apply / profile は後続で足す)。
func runNews(args []string, stdout, stderr io.Writer) int {
	usage := func() {
		fmt.Fprintln(stderr, "使い方: braindex news <サブコマンド> [フラグ]")
		fmt.Fprintln(stderr, "  fetch    フィードを取得し、既読に無い記事のダイジェスト(Markdown)を書く")
		fmt.Fprintln(stderr, "フラグは braindex news <サブコマンド> -h")
	}
	if len(args) == 0 {
		usage()
		return 1
	}
	switch args[0] {
	case "fetch":
		return runNewsFetch(args[1:], stdout, stderr)
	case "-h", "-help", "--help":
		usage()
		return 0
	}
	fmt.Fprintf(stderr, "braindex news: サブコマンド %q は無い\n", args[0])
	usage()
	return 1
}

// newsFetchOptions は braindex news fetch のコマンドライン。空は「未指定」。
type newsFetchOptions struct {
	config string // -config。hub の位置を兼ねるので必須(既定パスに無ければエラー)
	date   string // -date。今日の固定(既定: 実行日)
	layer  string // -layer。フィードの層(既定 all)
	replay bool   // -replay。既読を無視して全件を出し、既読も更新しない
	stdout bool   // -stdout。ファイルに書かず標準出力へ(既読は更新する)
	out    string // -out。出力先(既定: <news.dir>/digest_<日付>_<層>.md。既にあれば -2, -3 … を付ける)
}

// runNewsFetch は braindex news fetch を実行する。
//
// 終了コード: 0 成功 / 1 失敗(全フィードの取得失敗を含む。何も書かない) / 2 警告つきで完了(一部のフィードが取得できなかった)。
func runNewsFetch(args []string, stdout, stderr io.Writer) int {
	var o newsFetchOptions
	fs := flag.NewFlagSet("braindex news fetch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json。そのディレクトリを hub とみなす)")
	fs.StringVar(&o.date, "date", "", "今日として使う日付 YYYY-MM-DD(既定: 実行日)。出力ファイル名と既読の日付に使う")
	fs.StringVar(&o.layer, "layer", news.LayerAll, "取得するフィードの層(feeds.json の layer)。all は全件")
	fs.BoolVar(&o.replay, "replay", false, "既読を無視して全記事を出し、既読も更新しない(見出しの再生成用)")
	fs.BoolVar(&o.stdout, "stdout", false, "ファイルに書かず標準出力に出す(既読は更新する)")
	fs.StringVar(&o.out, "out", "", "出力先(既定: 設定 news.dir の digest_<日付>_<層>.md。既にあれば -2, -3 … を付けて別名にする)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex news fetch [-config braindex.json] [-date YYYY-MM-DD] [-layer <層>] [-replay] [-stdout] [-out <path>]")
		fmt.Fprintln(stderr, "  hub のルートで実行し、news/feeds.json のフィードを GET して、既読(news/.seen.json)に無い記事を")
		fmt.Fprintln(stderr, "  news/digest_<日付>_<層>.md に書く。外へ出る通信はフィードの GET だけ。")
		fmt.Fprintln(stderr, "  終了コード: 0 成功 / 1 失敗(全フィードの取得失敗を含む。何も書かない) / 2 一部のフィードが取得できなかった")
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
		fmt.Fprintf(stderr, "braindex news fetch: 引数 %q は受け付けない(フラグだけを渡す)\n", fs.Args())
		fs.Usage()
		return 1
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "braindex news fetch:", err)
		return 1
	}

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
	today := o.date
	if today == "" {
		today = time.Now().Format("2006-01-02")
	} else if _, perr := time.Parse("2006-01-02", today); perr != nil {
		return fail(fmt.Errorf("-date は YYYY-MM-DD で指定する: %q", today))
	}
	if o.layer == "" {
		return fail(errors.New("-layer が空(all か feeds.json の layer を指定する)"))
	}
	hubDir := filepath.Dir(cfgPath)
	s := fc.News.WithDefaults()
	newsDir := filepath.Join(hubDir, filepath.FromSlash(s.Dir))

	all, err := news.LoadFeeds(filepath.Join(hubDir, filepath.FromSlash(s.Feeds)))
	if err != nil {
		return fail(err)
	}
	srcs := news.FilterLayer(all, o.layer)
	if len(srcs) == 0 {
		return fail(fmt.Errorf("層 %q のフィードが無い(feeds.json にある層: %v)", o.layer, news.Layers(all)))
	}
	seenPath := filepath.Join(newsDir, news.SeenFile)
	seen, err := news.LoadSeen(seenPath)
	if err != nil {
		return fail(err)
	}

	results := news.Collect(context.Background(), newsFetcher, srcs, seen, today, o.replay)
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(stderr, "braindex news fetch: 警告: %s: %v\n", r.Source.Name, r.Err)
		} else {
			fmt.Fprintf(stdout, "%s: 新着 %d / 全 %d\n", r.Source.Name, len(r.New), len(r.Entries))
		}
	}
	if news.AllFailed(results) {
		return fail(errors.New("全フィードの取得に失敗した(ネットワークとフィードの URL を確認する)"))
	}

	digest := news.Digest(results, o.layer, today, s.Cap(o.layer))
	if o.stdout {
		if _, err := stdout.Write(digest); err != nil {
			return fail(err)
		}
	} else {
		outPath := o.out
		if outPath == "" {
			outPath = filepath.Join(newsDir, fmt.Sprintf("digest_%s_%s.md", today, o.layer))
		}
		outPath, err = unusedPath(outPath)
		if err != nil {
			return fail(err)
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return fail(err)
		}
		if err := os.WriteFile(outPath, digest, 0o644); err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "news ダイジェスト: %s\n", outPath)
	}

	// 既読はダイジェストを書けた後に更新する(書けなかった新着が既読になって消えないように)
	if !o.replay {
		pruned, err := seen.Prune(today, s.SeenDays)
		if err != nil {
			return fail(err)
		}
		if err := pruned.Save(seenPath); err != nil {
			return fail(err)
		}
	}

	if failed := news.Failed(results); len(failed) > 0 {
		fmt.Fprintf(stderr, "braindex news fetch: 取得失敗 %d 本(終了コード 2)\n", len(failed))
		return 2
	}
	return 0
}

// unusedPath は path が無ければそのまま、あれば拡張子の前に -2, -3 … を付けた未使用の名前を返す。
// 同じ日に 2 回取得したとき、前の回の新着(既読になっている)を上書きで失わないため。
func unusedPath(path string) (string, error) {
	ext := filepath.Ext(path)
	base := path[:len(path)-len(ext)]
	for i := 1; i < 1000; i++ {
		p := path
		if i > 1 {
			p = fmt.Sprintf("%s-%d%s", base, i, ext)
		}
		if _, err := os.Lstat(p); errors.Is(err, iofs.ErrNotExist) {
			return p, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("空いている名前が無い: %s", path)
}
