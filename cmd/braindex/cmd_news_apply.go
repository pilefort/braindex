package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pilefort/braindex/internal/config"
	"github.com/pilefort/braindex/internal/news"
)

// selectionDirs は選別 JSON を探す場所: <news.dir>/inbox と、指定があればそのディレクトリ、無ければ ~/Downloads。
// ブラウザのダウンロード先は環境で違うので、-inbox で差し替えられるようにする。
func selectionDirs(newsDir, inbox string) ([]string, error) {
	dirs := []string{filepath.Join(newsDir, "inbox")}
	if inbox != "" {
		return append(dirs, inbox), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("ホームディレクトリが分からない(-inbox で選別 JSON の置き場を指定する): %w", err)
	}
	return append(dirs, filepath.Join(home, "Downloads")), nil
}

// ingestSelections は選別 JSON を取り込み、結果を stdout に書く(news apply と news fetch の冒頭が共有)。
// known は feeds.json の取材先の名前。選別 JSON の feed_stats をこの名前で照合する(nil なら照合しない)。
func ingestSelections(newsDir string, known map[string]bool, inbox string, stdout io.Writer, feedsPath string, catalog []news.CatalogEntry) error {
	dirs, err := selectionDirs(newsDir, inbox)
	if err != nil {
		return err
	}
	msgs, err := news.Ingest(newsDir, dirs, known, feedsPath, catalog)
	for _, m := range msgs {
		fmt.Fprintln(stdout, "news:", m)
	}
	return err
}

// runNewsApply は braindex news apply を実行する。選別 JSON の取り込みだけを行う。
func runNewsApply(args []string, stdout, stderr io.Writer) int {
	var cfgPath, inbox string
	fs := flag.NewFlagSet("braindex news apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cfgPath, "config", "", "設定ファイルのパス(既定: カレントの braindex.json。そのディレクトリを hub とみなす)")
	fs.StringVar(&inbox, "inbox", "", "選別 JSON を探すディレクトリ(既定: ~/Downloads。<news.dir>/inbox はいつも見る)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex news apply [-config braindex.json] [-inbox DIR]")
		fmt.Fprintln(stderr, "  HTML の「選択と相談を保存」（旧「選別を書き出す」）で保存した JSON(braindex-news-selection_*.json)を <news.dir>/inbox と -inbox(既定 ~/Downloads)")
		fmt.Fprintln(stderr, "  から取り込む。「残す」は news/keep/YYYY-MM.md に追記(同じリンクは 1 回)、フィード別の数は news/.stats.json に")
		fmt.Fprintln(stderr, "  ダイジェスト単位で上書き保存(同じ日の再書き出しは二重に数えない)。取り込んだ JSON は news/.ingested/ へ移す。")
		fmt.Fprintln(stderr, "  news fetch の冒頭でも同じ取り込みが動くので、通常は別に実行しなくてよい。")
		fmt.Fprintln(stderr, "  1 つの JSON ごとに keep → 統計 → 取材先の登録 → 読書 → 取り込み済みへ移す、の順に書くので、途中で止まっても再実行で揃う(keep は同じリンクを 2 回足さない)。")
		fmt.Fprintln(stderr, "  fetch と同じロック(news/.lock.json)を取る。")
		fmt.Fprintln(stderr, "  終了コード: 0 成功(取り込むものが無くても 0) / 1 失敗(別の braindex news が動いている、を含む)")
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
		fmt.Fprintf(stderr, "braindex news apply: 引数 %q は受け付けない(フラグだけを渡す)\n", fs.Args())
		fs.Usage()
		return 1
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "braindex news apply:", err)
		return 1
	}
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
	s := fc.News.WithDefaults()
	hubDir := filepath.Dir(cfgPath)
	newsDir := filepath.Join(hubDir, filepath.FromSlash(s.Dir))
	// 排他: fetch と同じロック。並行して取り込むと同じ keep を 2 回足す
	unlock, stale, err := news.Lock(newsDir, "apply")
	if err != nil {
		return fail(err)
	}
	defer unlock()
	if stale != "" {
		fmt.Fprintln(stderr, "braindex news apply: 警告:", stale)
	}
	// feed_stats の照合に取材先の名前が要る。読めなくても取り込みは続ける(名前の照合だけ落ちる)。
	var known map[string]bool
	feedsPath := filepath.Join(hubDir, filepath.FromSlash(s.Feeds))
	if srcs, ferr := news.LoadFeeds(feedsPath); ferr != nil {
		fmt.Fprintf(stderr, "braindex news apply: 警告: %v(feed_stats の取材先名は照合しない)\n", ferr)
	} else {
		known = news.FeedNames(srcs)
	}
	if err := ingestSelections(newsDir, known, inbox, stdout, feedsPath, news.Catalog()); err != nil {
		return fail(err)
	}
	fmt.Fprintln(stdout, "news apply 完了")
	if _, err := os.Stat(filepath.Join(newsDir, news.ReadingHTML)); err == nil {
		fmt.Fprintln(stdout, "保存記事と相談の一覧:", filepath.Join(newsDir, news.ReadingHTML))
	}
	return 0
}
