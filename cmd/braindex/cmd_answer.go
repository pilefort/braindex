package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pilefort/braindex/internal/fsutil"
	"github.com/pilefort/braindex/internal/mdhtml"
)

func init() {
	register(&command{
		name:    "answer",
		summary: "Markdown の回答を自己完結 HTML にして既定ブラウザで開く(一時置き場・14 日で自動削除)",
		run:     runAnswer,
	})
}

// answersDir は回答 HTML の既定の置き場所。OS の一時ディレクトリ配下で、実行のたびに古いものを消す(HTML は一時物)。テストで差し替える。
var answersDir = func() string { return filepath.Join(os.TempDir(), "braindex-answers") }

// runAnswer は braindex answer [フラグ] <md> を実行する。
// <md> を自己完結 HTML にして書き、既定ブラウザで開く。出力先の既定は一時置き場 <OS の一時 dir>/braindex-answers/<同名>.html。
// 実行のたびに置き場所の -ttl-days より古いファイルを消す。残す価値のある回答は md を docs/notes/ に置いてから渡す。
// 終了コード: 0 成功 / 1 失敗。
func runAnswer(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("braindex answer", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var out, topic, question string
	var noOpen, showDir, purge bool
	var ttlDays float64
	fs.StringVar(&out, "out", "", "出力する HTML のパス(既定: 一時置き場の <同名>.html)")
	fs.StringVar(&topic, "append", "", "この話題のスレッド(一時置き場の <話題>.md)の先頭に足す")
	fs.StringVar(&question, "q", "", "そのエントリの見出しに出すユーザーの質問(逐語・-append と併用)")
	fs.BoolVar(&noOpen, "no-open", false, "書くだけで開かない")
	fs.Float64Var(&ttlDays, "ttl-days", 14, "一時置き場でこの日数より古いファイルを実行時に消す(0 で消さない)")
	fs.BoolVar(&showDir, "dir", false, "一時置き場のパスを表示して終わる")
	fs.BoolVar(&purge, "purge", false, "一時置き場の中を今すぐ全部消して終わる")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex answer [フラグ] <md>")
		fmt.Fprintln(stderr, "  Markdown を自己完結 HTML にして書き、既定ブラウザで開く。出力先の既定は一時置き場で、古いものは実行のたびに消える。")
		fmt.Fprintln(stderr, "  残す価値のある回答は md を docs/notes/ に置いてから渡す(HTML は表示用の一時物)。終了コード: 0 成功 / 1 失敗")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "  -append <話題> を付けると 1 つの話題にスレッドとして重ねる。新しい回答が先頭に積まれ、")
		fmt.Fprintln(stderr, "  一度見たエントリは次に開いたとき畳まれる。スレッドも一時置き場に置く(残すなら docs/notes/ へ)。")
		fmt.Fprintln(stderr, "    braindex answer -append 索引の設計 -q \"走査の順番は決まってる？\" ans.md")
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
	if ttlDays < 0 {
		fmt.Fprintln(stderr, "braindex answer: -ttl-days は 0 以上")
		return 1
	}
	if question != "" && topic == "" {
		fmt.Fprintln(stderr, "braindex answer: -q は -append と一緒に使う(スレッドのエントリの見出しになる)")
		return 1
	}
	dir := answersDir()
	if showDir {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			fmt.Fprintf(stderr, "braindex answer: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, dir)
		return 0
	}
	if purge {
		removed := removeFiles(dir, func(string, os.FileInfo) bool { return true })
		fmt.Fprintf(stdout, "braindex answer: %d ファイルを消した (%s)\n", len(removed), dir)
		return 0
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 1
	}
	src := fs.Arg(0)
	raw, err := os.ReadFile(src)
	if err != nil {
		fmt.Fprintf(stderr, "braindex answer: %v\n", err)
		return 1
	}
	md := strings.TrimPrefix(string(raw), "\uFEFF") // UTF-8 BOM を落とす
	// 古いものの掃除はスレッドを読む前に済ませる(期限切れのスレッドへの追記は、新しいスレッドとして始まる)。
	if ttlDays > 0 {
		limit := answerNow().Add(-time.Duration(ttlDays * 24 * float64(time.Hour)))
		removed := removeFiles(dir, func(_ string, fi os.FileInfo) bool { return fi.ModTime().Before(limit) })
		if len(removed) > 0 {
			fmt.Fprintf(stdout, "braindex answer: %g 日より古い %d ファイルを消した (%s)\n", ttlDays, len(removed), dir)
		}
	}
	base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	title := mdhtml.ExtractTitle(md, filepath.Base(src))
	// md に書かれた相対パス(画像・リンク)は md の置き場所から解決する。HTML は一時置き場に書かれるので、
	// 相対のままでは開けない(2026-09-12 実測)。
	srcDir := filepath.Dir(src)
	if abs, aerr := filepath.Abs(srcDir); aerr == nil {
		srcDir = abs
	}
	opt := mdhtml.Options{BaseDir: srcDir}
	page := func(md, title string) string { return mdhtml.PageWith(md, title, opt) }
	threadPageFn := func(md, title string) string { return mdhtml.ThreadPageWith(md, title, opt) }
	if topic != "" {
		name, nerr := threadName(topic)
		if nerr != nil {
			fmt.Fprintf(stderr, "braindex answer: %v\n", nerr)
			return 1
		}
		threadPath := filepath.Join(dir, name+".md")
		thread, terr := appendToThread(threadPath, name, question, md, answerNow())
		if terr != nil {
			fmt.Fprintf(stderr, "braindex answer: %v\n", terr)
			return 1
		}
		fmt.Fprintf(stdout, "braindex answer: 足した %s\n", threadPath)
		md, base, page = thread, name, threadPageFn
		title, _ = mdhtml.ParseThread(thread)
	} else if mdhtml.IsThread(md) {
		// スレッドの .md をそのまま渡されたら、エントリを足さずに描き直す(表示だけ作り直したいとき)。
		page = threadPageFn
		if t, _ := mdhtml.ParseThread(md); t != "" {
			title = t
		}
	}
	if out == "" {
		out = filepath.Join(dir, base+".html")
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
		fmt.Fprintf(stderr, "braindex answer: %v\n", err)
		return 1
	}
	if err := fsutil.WriteAtomic(out, []byte(page(md, title)), 0o600); err != nil {
		fmt.Fprintf(stderr, "braindex answer: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "braindex answer: 書いた %s\n", out)
	if noOpen {
		return 0
	}
	abs, err := filepath.Abs(out)
	if err != nil {
		abs = out
	}
	if err := openInBrowser(abs); err != nil {
		fmt.Fprintf(stderr, "braindex answer: 開けなかった: %v(ブラウザで %s を開いてください)\n", err, abs)
		return 1
	}
	return 0
}

// answerNow はスレッドのエントリに刻む日時と TTL の基準。テストで差し替える。
var answerNow = time.Now

// threadName は -append の話題名をファイル名に使える形にする。
// 一時置き場の外へ書かせないため、パス区切りと Windows で使えない文字を弾く。
func threadName(topic string) (string, error) {
	t := strings.TrimSuffix(strings.TrimSpace(topic), ".md")
	if t == "" {
		return "", errors.New("-append の話題名が空")
	}
	if t == "." || t == ".." || strings.ContainsAny(t, `/\:*?"<>|`) {
		return "", fmt.Errorf("-append の話題名にパス区切りや記号は使えない: %s", topic)
	}
	return t, nil
}

// appendToThread はスレッド .md の先頭に新しいエントリを足して書き戻し、書いた .md 全体を返す。
// スレッドが無ければ新しく作る。エントリの本文からは先頭の `# 見出し` を落とす
// (エントリごとに h1 が並ばないように。1 回目はそれがスレッドのタイトルになる)。
func appendToThread(path, name, question, md string, now time.Time) (string, error) {
	prev, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	h1, body := mdhtml.SplitH1(md)
	fallback := h1
	if fallback == "" {
		fallback = name
	}
	e := mdhtml.Entry{At: now.Format(time.RFC3339), Q: question, Body: body}
	thread := mdhtml.Prepend(string(prev), fallback, e)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := fsutil.WriteAtomic(path, []byte(thread), 0o600); err != nil {
		return "", err
	}
	return thread, nil
}

// removeFiles は dir 直下の通常ファイルのうち should が真のものを消し、消した名前を昇順で返す。
// サブディレクトリは触らない。dir が無ければ何もしない。消せないファイル(開かれている等)は次回に回す。
func removeFiles(dir string, should func(path string, fi os.FileInfo) bool) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var removed []string
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		fi, err := e.Info()
		if err != nil || !should(p, fi) {
			continue
		}
		if os.Remove(p) == nil {
			removed = append(removed, e.Name())
		}
	}
	sort.Strings(removed)
	return removed
}
