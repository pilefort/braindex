package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pilefort/braindex/internal/approvals"
	"github.com/pilefort/braindex/internal/config"
)

// approvalsHookDefaultTimeout は起動したフォームが回答を待つ既定の秒数。
// 答えないまま放置されたフォームを畳み、待ち受けプロセスを残さないための上限。
const approvalsHookDefaultTimeout = 3600

// approvalsHookReason は フォームを開いたときにアシスタントへ返す指示。
// -reason で上書きできる(利用側の規約に合わせた文面にしたいとき。上書きした文には wait の起動も含める)。
//
// フォームは切り離して起動するので、回答が届いてもアシスタントには伝わらない。
// 回答で続きに戻れるよう、待つコマンド(approvals wait)をアシスタントに起動させる。
const approvalsHookReason = "判断待ちのフォームを既定ブラウザで開きました(%d 件)。" +
	"何を判断してほしいか・どう回答するかを利用者に伝えてください。リンクを置くだけで終えないこと。" +
	"回答が届いたら続きに戻れるよう、発話を終える前に %s をバックグラウンドで起動してください" +
	"(回答が届くと要約を出して終わります)。"

// approvalsHookStdin と approvalsHookSpawn はテストで差し替える。
var (
	approvalsHookStdin io.Reader = os.Stdin
	approvalsHookSpawn           = spawnApprovalsForm
)

// hookInput は Stop フックが stdin に渡す JSON のうち、使う項目だけ。
//
// cwd はフックを呼んだ側の作業ディレクトリ。判断待ちの場所をここから決めるので、
// 呼び出し側が渡さないときはこのプロセスのカレントで代用する。
// stop_hook_active は「すでにフックの指示で続きを書いている」印。真のときは
// 指示を返さない(返すと終わりのない往復になる)。
type hookInput struct {
	CWD            string `json:"cwd"`
	StopHookActive bool   `json:"stop_hook_active"`
}

// hookOutput は Stop フックが stdout に返す JSON。
type hookOutput struct {
	SystemMessage string `json:"systemMessage,omitempty"`
	Decision      string `json:"decision,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

// hookState は「この内容の判断待ちはもうフォームにした」という印。
// Reported は approvals wait が最後に知らせた回答の受信時刻(同じ回答を 2 回知らせないため)。
type hookState struct {
	SHA      string `json:"sha"`
	PID      int    `json:"pid"`
	Opened   string `json:"opened"`
	Reported string `json:"reported,omitempty"`
}

// runApprovalsHook は braindex approvals hook を実行する。
//
// 判断待ちが残ったまま発話が終わったときに呼ばれ、残っていればフォームを起動する。
// 会話を止めないため、何が起きても終了コードは 0 を返す(判断待ちが無い・道具が揃わない・
// 起動に失敗した、のいずれも「黙って通す」)。異常は stderr に 1 行だけ書く。
func runApprovalsHook(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("approvals hook", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var ff approvalsFileFlags
	ff.bind(fs)
	timeout := fs.Float64("timeout", approvalsHookDefaultTimeout, "起動するフォームが回答を待つ秒数(0 で無期限)")
	reason := fs.String("reason", "", "フォームを開いたときにアシスタントへ返す指示(既定は組み込みの文)")
	noOpen := fs.Bool("no-open", false, "ブラウザを開かずに起動する(動作確認・画面の無い環境向け)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex approvals hook [フラグ]")
		fmt.Fprintln(stderr, "  stdin の JSON(cwd / stop_hook_active)を読み、判断待ちが残っていれば")
		fmt.Fprintln(stderr, "  approvals serve -apply を切り離して起動する。エディタの停止フックから呼ぶ。")
		fmt.Fprintln(stderr, "  同じ内容では一度しか開かない。終了コードは常に 0(会話を止めない)。")
		fmt.Fprintln(stderr, "  アシスタントへの指示には、回答を待つ approvals wait の起動を含める。")
		fmt.Fprintln(stderr)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 0
	}

	in := readHookInput(approvalsHookStdin)
	locateApprovals(&ff, in.CWD)

	doc, p, err := loadApprovals(ff)
	if err != nil {
		return 0 // 判断待ちのファイルが無い hub でも黙って通す
	}
	if len(doc.Items) == 0 {
		return 0
	}
	body, err := os.ReadFile(p.Approvals)
	if err != nil {
		return 0
	}
	sum := sha256.Sum256(body)
	sha := hex.EncodeToString(sum[:])[:12]
	statePath := approvalsHookStatePath(p)
	if hookAlreadyOpened(statePath, sha) {
		return 0
	}

	pid, err := approvalsHookSpawn(p, *timeout, *noOpen)
	if err != nil {
		fmt.Fprintf(stderr, "braindex approvals hook: フォームを起動できない: %v\n", err)
		return 0
	}
	writeHookState(statePath, hookState{SHA: sha, PID: pid, Opened: time.Now().Format(time.RFC3339)})

	out := hookOutput{SystemMessage: fmt.Sprintf("判断待ち %d 件のフォームを開きました(既定ブラウザ)", len(doc.Items))}
	if !in.StopHookActive {
		out.Decision = "block"
		out.Reason = *reason
		if out.Reason == "" {
			out.Reason = fmt.Sprintf(approvalsHookReason, len(doc.Items), approvalsWaitCommand(p))
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return 0
	}
	fmt.Fprint(stdout, string(b))
	return 0
}

// approvalsWaitCommand は アシスタントに起動させる approvals wait のコマンド行を返す。
//
// コマンド名は hook 自身が呼ばれた名前(os.Args[0])を使う。停止フックに絶対パスで登録している
// 環境では、アシスタントのシェルでも `braindex` だけでは見つからないことがあるため。
// 置き場は -file(と既定以外の -dir)で明示し、アシスタントのカレントに依らず同じ回答を待たせる。
func approvalsWaitCommand(p approvals.Paths) string {
	cmd := quoteIfSpace(os.Args[0]) + ` approvals wait -file "` + p.Approvals + `"`
	if dir := filepath.Dir(p.Reply); dir != approvals.DefaultDir() {
		cmd += ` -dir "` + dir + `"`
	}
	return "`" + cmd + "`"
}

func quoteIfSpace(s string) string {
	if strings.ContainsAny(s, " \t") {
		return `"` + s + `"`
	}
	return s
}

// readHookInput は stdin の JSON を読む。読めない・空でも既定値で続ける
// (フックの入力が変わっても、判断待ちの有無だけは見に行けるようにする)。
func readHookInput(r io.Reader) hookInput {
	var in hookInput
	if r == nil {
		return in
	}
	b, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil || len(b) == 0 {
		return in
	}
	_ = json.Unmarshal(b, &in)
	return in
}

// locateApprovals は -file が無いときに、フックを呼んだ側の cwd から判断待ちの場所を決める。
//
// hook はエディタが選んだディレクトリで動くとは限らないので、渡された cwd を基準にする。
// 設定ファイルがそこにあれば拾う(置き場を変えている hub でも当たるように)。
func locateApprovals(ff *approvalsFileFlags, cwd string) {
	if ff.file != "" || cwd == "" {
		return
	}
	ff.file = filepath.Join(cwd, filepath.FromSlash(approvals.DefaultFile))
	if ff.config == "" {
		if cfg := filepath.Join(cwd, config.DefaultPath); fileExists(cfg) {
			ff.config = cfg
		}
	}
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// approvalsHookStatePath は「開いた印」の置き場。回答 JSON と同じ一時置き場に置く
// (hub の中に置くと索引や git に混ざる)。
func approvalsHookStatePath(p approvals.Paths) string {
	return filepath.Join(filepath.Dir(p.Reply), "hook-"+p.ID+".json")
}

// readHookState は開いた印を読む。無い・壊れているときは false。
func readHookState(statePath string) (hookState, bool) {
	b, err := os.ReadFile(statePath)
	if err != nil {
		return hookState{}, false
	}
	var st hookState
	if err := json.Unmarshal(b, &st); err != nil {
		return hookState{}, false
	}
	return st, true
}

// hookAlreadyOpened は この内容の判断待ちを既にフォームにしたかを返す。
//
// 内容が同じうちは開き直さない(発話のたびにタブが増えるのを防ぐ)。回答が入れば
// APPROVALS.md が変わるので、次の判断待ちではまた開く。
func hookAlreadyOpened(statePath, sha string) bool {
	st, ok := readHookState(statePath)
	return ok && st.SHA == sha
}

func writeHookState(statePath string, st hookState) {
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		return
	}
	b, err := json.Marshal(st)
	if err != nil {
		return
	}
	_ = os.WriteFile(statePath, b, 0o644)
}

// spawnApprovalsForm は approvals serve -apply を切り離して起動し、PID を返す。
//
// フック自身は回答を待たない(待つと発話が終わらない)。子は親と別のプロセスグループで動くので、
// フックのプロセスが終わってもフォームは開いたまま残る。
func spawnApprovalsForm(p approvals.Paths, timeout float64, noOpen bool) (int, error) {
	self, err := os.Executable()
	if err != nil {
		return 0, err
	}
	args := []string{"approvals", "serve", "-apply", "-file", p.Approvals,
		"-timeout", strconv.FormatFloat(timeout, 'f', -1, 64)}
	if dir := filepath.Dir(p.Reply); dir != approvals.DefaultDir() {
		args = append(args, "-dir", dir)
	}
	if noOpen {
		args = append(args, "-no-open")
	}
	c := exec.Command(self, args...)
	c.Dir = p.Project
	detachProcess(c)
	if err := c.Start(); err != nil {
		return 0, err
	}
	pid := c.Process.Pid
	_ = c.Process.Release() // 待たないので、親側の後始末だけ済ませる
	return pid, nil
}
