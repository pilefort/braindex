package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/pilefort/braindex/internal/approvals"
)

func init() {
	register(&command{
		name:    "approvals",
		summary: "work/APPROVALS.md(判断待ち)を HTML フォームで聞き、回答を docs/decisions.md に反映する",
		run:     runApprovals,
	})
}

// approvalsSubs は braindex approvals <サブ> の一覧(表示順)。
var approvalsSubs = []struct {
	name, summary string
	run           func(args []string, stdout, stderr io.Writer) int
}{
	{"serve", "フォームを 127.0.0.1 で配信して既定ブラウザで開き、回答を 1 回受けて一時置き場に書く", runApprovalsServe},
}

// approvalsOnReady はテスト用のフック。serve が待ち受けを始めた URL を受け取る。
var approvalsOnReady func(url string)

func approvalsUsage(stderr io.Writer) {
	fmt.Fprintln(stderr, "使い方: braindex approvals <サブコマンド> [フラグ]")
	fmt.Fprintln(stderr, "  work/APPROVALS.md の判断待ち(1 項目 1 判断・5 欄)をブラウザのフォームで聞き、答えを記録する。")
	fmt.Fprintln(stderr, "  外部送信なし(受け口は 127.0.0.1 の空きポートだけ・常駐しない)。")
	fmt.Fprintln(stderr)
	fmt.Fprintln(stderr, "サブコマンド:")
	for _, s := range approvalsSubs {
		fmt.Fprintf(stderr, "  %-8s %s\n", s.name, s.summary)
	}
}

// runApprovals は braindex approvals <サブ> を振り分ける。
func runApprovals(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		approvalsUsage(stderr)
		return 1
	}
	switch args[0] {
	case "-h", "-help", "--help", "help": // braindex retro と同じ受け方
		approvalsUsage(stderr)
		return 0
	}
	for _, s := range approvalsSubs {
		if s.name == args[0] {
			return s.run(args[1:], stdout, stderr)
		}
	}
	fmt.Fprintf(stderr, "braindex approvals: サブコマンド %q は無い\n", args[0])
	approvalsUsage(stderr)
	return 1
}

// approvalsFileFlags は各サブコマンド共通の置き場のフラグ。
type approvalsFileFlags struct {
	file string // -file。APPROVALS.md(既定: work/APPROVALS.md)
	dir  string // -dir。回答 JSON の置き場(既定: OS の一時ディレクトリの braindex-approvals)
}

func (f *approvalsFileFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&f.file, "file", filepath.Join("work", "APPROVALS.md"), "判断待ちのファイル")
	fs.StringVar(&f.dir, "dir", "", "回答 JSON の置き場(既定: OS の一時ディレクトリの braindex-approvals)")
}

// loadApprovals は APPROVALS.md を読み、解析結果と置き場を返す。
func loadApprovals(f approvalsFileFlags) (approvals.Doc, approvals.Paths, error) {
	p, err := approvals.Resolve(f.file, f.dir)
	if err != nil {
		return approvals.Doc{}, p, err
	}
	b, err := os.ReadFile(p.Approvals)
	if err != nil {
		return approvals.Doc{}, p, fmt.Errorf("判断待ちのファイルを読めない: %w", err)
	}
	return approvals.Parse(b), p, nil
}

// printApprovalWarnings は記載漏れを stderr に出し、件数を返す。
func printApprovalWarnings(d approvals.Doc, stderr io.Writer) int {
	n := 0
	for _, it := range d.Items {
		for _, w := range it.Warnings {
			fmt.Fprintf(stderr, "warning: [%d] %s: %s\n", it.N, it.Title, w)
			n++
		}
	}
	return n
}

// runApprovalsServe は braindex approvals serve を実行する。
// 終了コード: 0 回答を受け取った / 1 失敗 / 2 時間切れ(回答なし)。
func runApprovalsServe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("braindex approvals serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var f approvalsFileFlags
	var timeoutSec float64
	var noOpen bool
	f.bind(fs)
	fs.Float64Var(&timeoutSec, "timeout", 0, "回答を待つ秒数(0 で無期限)。過ぎたら終了コード 2")
	fs.BoolVar(&noOpen, "no-open", false, "ブラウザを開かず URL を表示するだけ")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex approvals serve [-file work/APPROVALS.md] [-timeout 秒] [-no-open] [-dir <置き場>]")
		fmt.Fprintln(stderr, "  判断待ちをフォームにして 127.0.0.1 の空きポートで配信し、既定ブラウザで開く。「決定を送信」を 1 回受けたら")
		fmt.Fprintln(stderr, "  回答を <置き場>/approvals-<id>.reply.json に書いて終わる(常駐しない)。反映は braindex approvals apply。")
		fmt.Fprintln(stderr, "  終了コード: 0 回答あり / 1 失敗 / 2 時間切れ")
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
	fail := func(err error) int {
		fmt.Fprintln(stderr, "braindex approvals serve:", err)
		return 1
	}
	if fs.NArg() > 0 {
		return fail(fmt.Errorf("引数 %q は受け付けない(フラグだけを渡す)", fs.Args()))
	}
	timeout, err := serveTimeout(timeoutSec)
	if err != nil {
		return fail(err)
	}
	d, p, err := loadApprovals(f)
	if err != nil {
		return fail(err)
	}
	printApprovalWarnings(d, stderr)
	if len(d.Items) == 0 {
		fmt.Fprintf(stdout, "判断待ちはない: %s\n", p.Approvals)
		return 0
	}
	if _, err := os.Stat(p.Reply); err == nil {
		fmt.Fprintf(stderr, "note: 未反映の回答がある → 先に braindex approvals apply: %s\n", p.Reply)
	}
	nonce := approvals.NewNonce()
	now := time.Now()
	html := approvals.RenderForm(d, approvals.Meta{
		Project:     filepath.Base(p.Project),
		Path:        p.Approvals,
		Nonce:       nonce,
		GeneratedAt: now.Format("2006-01-02 15:04"),
	})
	rep, err := approvals.Serve(context.Background(), approvals.ServeOptions{
		HTML:    html,
		Nonce:   nonce,
		Timeout: timeout,
		OnReady: func(url string) {
			fmt.Fprintf(stdout, "form: %s (%d 件・id=%s)\n", url, len(d.Items), p.ID)
			if approvalsOnReady != nil {
				approvalsOnReady(url)
			}
			if !noOpen {
				if err := openBrowser(url); err != nil {
					fmt.Fprintf(stderr, "note: ブラウザを開けない(%v)。上の URL を手で開く\n", err)
				}
			}
		},
	})
	if err != nil {
		if errors.Is(err, approvals.ErrTimeout) {
			fmt.Fprintf(stderr, "braindex approvals serve: %g 秒待っても回答なし(終了コード 2)\n", timeoutSec)
			return 2
		}
		return fail(err)
	}
	if err := writeReply(p.Reply, rep); err != nil {
		return fail(err)
	}
	fmt.Fprintf(stdout, "reply: %s\n", p.Reply)
	for _, line := range summarizeReply(rep) {
		fmt.Fprintln(stdout, line)
	}
	fmt.Fprintln(stdout, "next: braindex approvals apply")
	return 0
}

// serveTimeout は -timeout の秒数を Duration にする(0 は無期限)。負・NaN・Duration に収まらない値は誤りとして返す。
// そのまま time.Duration に変換すると、負も桁あふれも Timeout <= 0 になり、「無期限で待つ」と区別が付かない。
func serveTimeout(sec float64) (time.Duration, error) {
	switch {
	case math.IsNaN(sec) || sec < 0:
		return 0, fmt.Errorf("-timeout は 0 以上の秒数(0 で無期限): %v", sec)
	case sec > float64(math.MaxInt64)/float64(time.Second):
		return 0, fmt.Errorf("-timeout が大きすぎる(%v 秒)。無期限にするなら 0", sec)
	}
	return time.Duration(sec * float64(time.Second)), nil
}

// writeReply は回答を JSON で書く(置き場が無ければ作る)。
func writeReply(path string, rep approvals.Reply) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rep, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// summarizeReply は回答を 1 項目 1 行にする。
func summarizeReply(rep approvals.Reply) []string {
	var out []string
	for _, it := range rep.Items {
		s := fmt.Sprintf("[%d] %s → %s", it.N, it.Title, it.Choice)
		if it.Comment != "" {
			s += "（" + strings.ReplaceAll(it.Comment, "\n", " ") + "）"
		}
		out = append(out, s)
	}
	return out
}

// openBrowser は既定ブラウザで url を開く。
func openBrowser(url string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		c = exec.Command("open", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	return c.Start()
}
