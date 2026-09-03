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
	var out string
	var noOpen, showDir, purge bool
	var ttlDays float64
	fs.StringVar(&out, "out", "", "出力する HTML のパス(既定: 一時置き場の <同名>.html)")
	fs.BoolVar(&noOpen, "no-open", false, "書くだけで開かない")
	fs.Float64Var(&ttlDays, "ttl-days", 14, "一時置き場でこの日数より古いファイルを実行時に消す(0 で消さない)")
	fs.BoolVar(&showDir, "dir", false, "一時置き場のパスを表示して終わる")
	fs.BoolVar(&purge, "purge", false, "一時置き場の中を今すぐ全部消して終わる")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex answer [フラグ] <md>")
		fmt.Fprintln(stderr, "  Markdown を自己完結 HTML にして書き、既定ブラウザで開く。出力先の既定は一時置き場で、古いものは実行のたびに消える。")
		fmt.Fprintln(stderr, "  残す価値のある回答は md を docs/notes/ に置いてから渡す(HTML は表示用の一時物)。終了コード: 0 成功 / 1 失敗")
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
	dir := answersDir()
	if showDir {
		if err := os.MkdirAll(dir, 0o755); err != nil {
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
	title := mdhtml.ExtractTitle(md, filepath.Base(src))
	if out == "" {
		base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
		out = filepath.Join(dir, base+".html")
	}
	if ttlDays > 0 {
		limit := time.Now().Add(-time.Duration(ttlDays * 24 * float64(time.Hour)))
		removed := removeFiles(dir, func(_ string, fi os.FileInfo) bool { return fi.ModTime().Before(limit) })
		if len(removed) > 0 {
			fmt.Fprintf(stdout, "braindex answer: %g 日より古い %d ファイルを消した (%s)\n", ttlDays, len(removed), dir)
		}
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fmt.Fprintf(stderr, "braindex answer: %v\n", err)
		return 1
	}
	if err := os.WriteFile(out, []byte(mdhtml.Page(md, title)), 0o644); err != nil {
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
