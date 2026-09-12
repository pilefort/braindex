package main

import (
	"context"
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
	"github.com/pilefort/braindex/internal/config"
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
	{"apply", "受けた回答を docs/decisions.md(3 段で追記)と APPROVALS.md(消し込み・保留)に反映する", runApprovalsApply},
	{"status", "id・置き場・項目数・未反映の回答・記載漏れを表示する(書き込みなし)", runApprovalsStatus},
	{"hook", "停止フックから呼ぶ。判断待ちが残っていれば serve を切り離して起動する(同じ内容では一度だけ)", runApprovalsHook},
	{"wait", "hook が開いたフォームの回答を待ち、届いたら要約を出して終わる(アシスタントがバックグラウンドで起動する)", runApprovalsWait},
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
	file   string // -file。APPROVALS.md(既定: 設定 approvals.file → work/APPROVALS.md)
	dir    string // -dir。回答 JSON の置き場(既定: OS の一時ディレクトリの braindex-approvals)
	config string // -config。braindex.json(既定: カレントの braindex.json。無くてもよい)
}

func (f *approvalsFileFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&f.file, "file", "", "判断待ちのファイル(既定: 設定 approvals.file。無ければ work/APPROVALS.md)")
	fs.StringVar(&f.dir, "dir", "", "回答 JSON の置き場(既定: OS の一時ディレクトリの braindex-approvals)")
	fs.StringVar(&f.config, "config", "", "設定ファイル(既定: カレントの braindex.json。無くてもよい)")
}

// settings は braindex.json の approvals 節を既定込みで返す。
//
// 設定ファイルが「既定の置き場に無い」のは正常(フラグと既定だけで動く)。
// -config で明示したのに無いときだけエラーにする(打ち間違いを黙って無視しないため)。
// 戻り値の 2 つめは設定ファイルのパス。設定に書いた相対パスは「設定ファイルのある場所」を
// 基準に解く(コマンドを打ったカレント基準にすると、hub の外から呼んだときに壊れる)。
// 戻り値の 3 つめは設定ファイルが見つかったか。見つからないときは設定を当てにせず
// 従来どおりの既定(カレント相対の work/APPROVALS.md と、APPROVALS.md から推定した hub の
// docs/decisions.md)を使う——ここで設定側の既定を混ぜると、設定ファイルの無い hub で
// decisions.md が相対パスのまま使われる。
func (f approvalsFileFlags) settings() (approvals.Settings, string, bool, error) {
	path := f.config
	if path == "" {
		path = config.DefaultPath
	}
	cfg, found, err := config.Load(path)
	if err != nil {
		return approvals.Settings{}, path, false, err
	}
	if !found {
		if f.config != "" {
			return approvals.Settings{}, path, false, fmt.Errorf("設定ファイルが無い: %s(hub のルートで実行するか、-config で指定する)", f.config)
		}
		return approvals.Settings{}.WithDefaults(), path, false, nil
	}
	if err := cfg.Approvals.Validate(); err != nil {
		return approvals.Settings{}, path, true, err
	}
	return cfg.Approvals.WithDefaults(), path, true, nil
}

// resolveApprovalsPaths は フラグ > 設定 > 既定 の順で置き場を決める。
//
// serve / status / apply のすべてがここを通す。apply だけ approvals.Resolve を直に呼ぶと、
// -file を渡さないときに filepath.Abs("") ＝ カレントが APPROVALS.md 扱いになり、
// Paths.ID が serve と別物になって「回答があるのに『回答はない』」で取りこぼす。
//
// 設定に書いた相対パスは file も decisions も同じ基準(braindex.json のある場所)で解く。
// 基準を 2 つ持つと、APPROVALS.md を work/ 直下以外に置いた瞬間に追記先がずれる
// (approvals.Resolve は親ディレクトリ名が "work" のときだけ 1 段上がるため)。
// 既存の cmd_news.go・cmd_review.go も設定の相対パスを hub 基準で解いており、それに揃えている。
func resolveApprovalsPaths(f approvalsFileFlags) (approvals.Paths, error) {
	s, cfgPath, found, err := f.settings()
	if err != nil {
		return approvals.Paths{}, err
	}
	base := filepath.Dir(cfgPath) // 設定ファイルのある場所 = hub
	if abs, aerr := filepath.Abs(base); aerr == nil {
		base = abs // 相対のままだと status の出力だけ体裁が崩れ、(あり) の判定もカレント基準になる
	}
	fromHub := func(v string) string {
		v = filepath.FromSlash(v)
		if v == "" || filepath.IsAbs(v) {
			return v
		}
		return filepath.Join(base, v)
	}
	file := f.file
	if file == "" {
		if found {
			file = fromHub(s.File)
		} else {
			file = filepath.FromSlash(approvals.DefaultFile) // 設定が無ければ従来どおりカレント相対
		}
	}
	p, err := approvals.Resolve(file, f.dir)
	if err != nil {
		return p, err
	}
	// 設定の decisions を採るのは、解決した APPROVALS.md が設定の hub の下にあるときだけ。
	// -file で別プロジェクト(spoke)の APPROVALS.md を捌くときは、決定も相手側に書く
	// (braindex init -repo は各プロジェクトに work/APPROVALS.md と docs/decisions.md の
	// 両方を作るので、hub のカレントから spoke を捌くのは想定内の使い方)。
	// 設定が無いときも Resolve の既定(推定した hub の docs/decisions.md)のままにする。
	if found && underDir(base, p.Approvals) {
		if d := fromHub(s.Decisions); d != "" {
			p.Decisions = d
		}
	}
	return p, nil
}

// underDir は path が dir と同じか、その下にあるかを返す。
func underDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// loadApprovals は APPROVALS.md を読み、解析結果と置き場を返す。
func loadApprovals(f approvalsFileFlags) (approvals.Doc, approvals.Paths, error) {
	p, err := resolveApprovalsPaths(f)
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
// 終了コード: 0 回答を受け取った / 1 失敗 / 3 時間切れ(回答なし)。
// 2 は使わない(リポの規約で 2 は「警告つき完了」。時間切れは完了していない)。
func runApprovalsServe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("braindex approvals serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var f approvalsFileFlags
	var timeoutSec float64
	var noOpen, apply bool
	var decisionsPath string
	f.bind(fs)
	fs.Float64Var(&timeoutSec, "timeout", 0, "回答を待つ秒数(0 で無期限)。過ぎたら終了コード 3")
	fs.BoolVar(&noOpen, "no-open", false, "ブラウザを開かず URL を表示するだけ")
	fs.BoolVar(&apply, "apply", false, "回答を受けたら続けて反映する(braindex approvals apply と同じ)。聞く→反映を 1 コマンドで済ませる")
	fs.StringVar(&decisionsPath, "decisions", "", "-apply のとき決定を追記するファイル(既定: <hub>/docs/decisions.md)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex approvals serve [-config braindex.json] [-file work/APPROVALS.md] [-timeout 秒] [-no-open] [-apply] [-decisions docs/decisions.md] [-dir <置き場>]")
		fmt.Fprintln(stderr, "  判断待ちをフォームにして 127.0.0.1 の空きポートで配信し、既定ブラウザで開く。「決定を送信」を 1 回受けたら")
		fmt.Fprintln(stderr, "  回答を <置き場>/approvals-<id>.reply.json に書いて終わる(常駐しない)。反映は braindex approvals apply(-apply で続けて行う)。")
		fmt.Fprintln(stderr, "  終了コード: 0 回答あり / 1 失敗 / 3 時間切れ(-apply のときは反映の失敗も 1)")
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
	// -timeout を明示していなければ設定の approvals.timeout_sec を使う(フラグ > 設定 > 既定)
	explicitTimeout := false
	fs.Visit(func(fl *flag.Flag) {
		if fl.Name == "timeout" {
			explicitTimeout = true
		}
	})
	if !explicitTimeout {
		s, _, _, serr := f.settings()
		if serr != nil {
			return fail(serr)
		}
		timeoutSec = float64(s.TimeoutSec)
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
		// このまま回答を受けると同じパスに書くので、前の回答は失われる
		fmt.Fprintf(stderr, "note: 未反映の回答がある(このまま回答すると上書きする) → 先に braindex approvals apply: %s\n", p.Reply)
	}
	nonce := approvals.NewNonce()
	now := time.Now()
	html := approvals.RenderForm(d, approvals.Meta{
		Project:     filepath.Base(p.Project),
		Path:        p.Approvals,
		Nonce:       nonce,
		GeneratedAt: now.Format("2006-01-02 15:04"),
	})
	// 回答の書き込みは Serve に任せる(応答を返す前に書く)。ここで受け取ってから書くと、
	// 書けなかったときにブラウザ側は完了表示のままになる。
	rep, err := approvals.Serve(context.Background(), approvals.ServeOptions{
		HTML:      html,
		Nonce:     nonce,
		Timeout:   timeout,
		ReplyPath: p.Reply,
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
			fmt.Fprintf(stderr, "braindex approvals serve: %g 秒待っても回答なし(終了コード 3)\n", timeoutSec)
			return 3
		}
		return fail(err)
	}
	fmt.Fprintf(stdout, "reply: %s\n", p.Reply)
	for _, line := range summarizeReply(rep) {
		fmt.Fprintln(stdout, line)
	}
	if apply {
		return applyReply(p, "", decisionsPath, time.Now().Format("2006-01-02"), stdout, stderr)
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
	return startAndReap(c)
}

// startAndReap は c.Start() のあと、終了を待たずに呼び出し元へ返る。子の終了はバックグラウンドの
// goroutine が Wait() で拾う(reap する)。
//
// Start() だけだと、子が終わっても親が Wait() を呼ぶまでゾンビのまま残る。openBrowser の呼び出し元は
// 回答が届くまで待ち続ける(最大で -timeout の秒数)ので、xdg-open / open のような「起動したらすぐ終わる」
// 子でも、待っている間ずっとゾンビが残ってしまう。goroutine で reap するだけで、呼び出し元は待たない。
func startAndReap(c *exec.Cmd) error {
	if err := c.Start(); err != nil {
		return err
	}
	go func() { _ = c.Wait() }()
	return nil
}
