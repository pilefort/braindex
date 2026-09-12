package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pilefort/braindex/internal/explain"
	"github.com/pilefort/braindex/internal/mdhtml"
)

func init() {
	register(&command{
		name:    "explain",
		summary: "解説の md を目次・図・グラフつきの自己完結 HTML にして開く(一時置き場・14 日で自動削除)",
		run:     runExplain,
	})
}

// runExplain は braindex explain [フラグ] <md> を実行する。
// 解説の md を、目次・埋め込んだ図・表から描いたグラフを備えた HTML にして書き、既定ブラウザで開く。
// 置き場所・TTL・開き方は answer と同じ(一時置き場は表示用で、正本の md と .svg は docs/notes/ に置く)。
// 終了コード: 0 成功 / 1 失敗(図が見つからない等、本文に印を出した問題を含む)。
func runExplain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("braindex explain", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var out string
	var noOpen bool
	var ttlDays float64
	fs.StringVar(&out, "out", "", "出力する HTML のパス(既定: 一時置き場の <同名>.html)")
	fs.BoolVar(&noOpen, "no-open", false, "書くだけで開かない")
	fs.Float64Var(&ttlDays, "ttl-days", 14, "一時置き場でこの日数より古いファイルを実行時に消す(0 で消さない)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex explain [フラグ] <md>")
		fmt.Fprintln(stderr, "  論文・記事の解説の md を、目次・図・グラフつきの自己完結 HTML にして開く。")
		fmt.Fprintln(stderr, "  図は別の .svg に置き、md からは ![図1: 説明](fig1.svg) で参照する(中身を HTML に埋め込む)。")
		fmt.Fprintln(stderr, `  図の色は本文に合わせる: <svg class="bxfig"> にして fill="var(--fg)" のように名前で書く(値は書かない)。`)
		fmt.Fprintln(stderr, "  使える名前: fg fg2 mono edge groove surf / c1〜c4(系列の色) / s1〜s4(系列の面)")
		fmt.Fprintln(stderr, "  表の直前に <!-- graph: bar x=手法 y=Recall@1 unit=% --> を置くと、その表から棒/折れ線を描く。")
		fmt.Fprintln(stderr, "  正本の md と .svg は docs/notes/ に置く(HTML は表示用の一時物)。終了コード: 0 成功 / 1 失敗")
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
		fmt.Fprintln(stderr, "braindex explain: -ttl-days は 0 以上")
		return 1
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 1
	}
	src := fs.Arg(0)
	raw, err := os.ReadFile(src)
	if err != nil {
		fmt.Fprintf(stderr, "braindex explain: %v\n", err)
		return 1
	}
	md := strings.TrimPrefix(string(raw), "\uFEFF") // UTF-8 BOM を落とす
	dir := answersDir()
	if ttlDays > 0 {
		limit := answerNow().Add(-time.Duration(ttlDays * 24 * float64(time.Hour)))
		removed := removeFiles(dir, func(_ string, fi os.FileInfo) bool { return fi.ModTime().Before(limit) })
		if len(removed) > 0 {
			fmt.Fprintf(stdout, "braindex explain: %g 日より古い %d ファイルを消した (%s)\n", ttlDays, len(removed), dir)
		}
	}
	// 図の .svg と本文の相対パスは md の置き場所から解決する(HTML は別の場所に書かれる)。
	srcDir := filepath.Dir(src)
	if abs, aerr := filepath.Abs(srcDir); aerr == nil {
		srcDir = abs
	}
	title := mdhtml.ExtractTitle(md, filepath.Base(src))
	page, problems := explain.Render(md, title, explain.Options{BaseDir: srcDir})
	if out == "" {
		base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
		out = filepath.Join(dir, base+".html")
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fmt.Fprintf(stderr, "braindex explain: %v\n", err)
		return 1
	}
	if err := os.WriteFile(out, []byte(page), 0o644); err != nil {
		fmt.Fprintf(stderr, "braindex explain: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "braindex explain: 書いた %s\n", out)
	for _, p := range problems {
		fmt.Fprintf(stderr, "braindex explain: %s\n", p)
	}
	if !noOpen {
		abs, aerr := filepath.Abs(out)
		if aerr != nil {
			abs = out
		}
		if oerr := openInBrowser(abs); oerr != nil {
			fmt.Fprintf(stderr, "braindex explain: 開けなかった: %v(ブラウザで %s を開いてください)\n", oerr, abs)
			return 1
		}
	}
	if len(problems) > 0 {
		return 1 // HTML は書けているが、印の出た箇所を直してほしい
	}
	return 0
}
