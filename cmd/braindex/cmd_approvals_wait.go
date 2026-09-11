package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"github.com/pilefort/braindex/internal/approvals"
)

// runApprovalsWait は braindex approvals wait を実行する。
//
// hook はフォームを切り離して起動するので、回答が届いてもアシスタントには何も伝わらない。
// wait はそのすき間を埋める。アシスタントがバックグラウンドで起動しておき、回答が届いたら
// 要約を出して終わる(終わったこと自体が、アシスタントへの知らせになる)。
//
// 待つのは hook がフォームを開いた時刻(hook-<id>.json の opened)以降に届いた回答。
// 開いてから wait が起動するまでに答えられても拾えるよう、起動時刻でなく開いた時刻を基準にする
// (印が無ければ起動時刻)。知らせた回答の受信時刻は印に reported として残し、2 回知らせない。
// 反映できたかは、apply が .applied.json に書き足した結果(警告の有無)で判断する。
// 終了コード: 0 回答が反映された / 1 失敗 / 2 回答は届いたが未反映 / 3 時間切れ(回答なし)。
func runApprovalsWait(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("braindex approvals wait", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var f approvalsFileFlags
	f.bind(fs)
	timeoutSec := fs.Float64("timeout", approvalsHookDefaultTimeout, "回答を待つ秒数(0 で無期限)。過ぎたら終了コード 3。既定は hook が開くフォームの待ち時間と同じ")
	intervalSec := fs.Float64("interval", 1, "回答 JSON を見に行く間隔(秒)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex approvals wait [-config braindex.json] [-file work/APPROVALS.md] [-dir <置き場>] [-timeout 秒] [-interval 秒]")
		fmt.Fprintln(stderr, "  hook が開いたフォームの回答を待ち、届いたら要約を出して終わる。アシスタントがバックグラウンドで起動し、")
		fmt.Fprintln(stderr, "  終わったことを「回答が届いた」知らせとして受け取る。同じ回答は 2 回知らせない。")
		fmt.Fprintln(stderr, "  終了コード: 0 回答が反映された / 1 失敗 / 2 回答は届いたが未反映 / 3 時間切れ")
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
		fmt.Fprintln(stderr, "braindex approvals wait:", err)
		return 1
	}
	if fs.NArg() > 0 {
		return fail(fmt.Errorf("引数 %q は受け付けない(フラグだけを渡す)", fs.Args()))
	}
	timeout, err := serveTimeout(*timeoutSec)
	if err != nil {
		return fail(err)
	}
	if math.IsNaN(*intervalSec) || *intervalSec <= 0 || *intervalSec > 3600 {
		return fail(fmt.Errorf("-interval は 0 より大きく 3600 以下の秒数: %v", *intervalSec))
	}
	interval := time.Duration(*intervalSec * float64(time.Second))
	p, err := resolveApprovalsPaths(f)
	if err != nil {
		return fail(err)
	}

	statePath := approvalsHookStatePath(p)
	since, reported := waitBaseline(statePath, time.Now())
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	// serve -apply は受信の直後に反映して回答を applied へ移す。reply だけが見えたら、
	// 反映が終わるのを少し待ってから「未反映」と判断する。
	grace := 10 * interval
	var replySeen time.Time
	for {
		if rep, ok := newAnswer(p.Applied, since, reported); ok {
			markReported(statePath, rep.ReceivedAt)
			if rep.Result != nil && len(rep.Result.Warnings) > 0 {
				fmt.Fprintf(stdout, "回答は届いたが未反映の項目がある: %s\n", p.Applied)
				for _, w := range rep.Result.Warnings {
					fmt.Fprintln(stdout, w)
				}
				for _, line := range summarizeReply(rep) {
					fmt.Fprintln(stdout, line)
				}
				fmt.Fprintf(stdout, "next: 回答の題と %s の題を突き合わせ、braindex approvals apply -reply %s で反映し直す\n", p.Approvals, p.Applied)
				return 2
			}
			fmt.Fprintf(stdout, "回答が届いた(反映済み): %s\n", p.Applied)
			for _, line := range summarizeReply(rep) {
				fmt.Fprintln(stdout, line)
			}
			fmt.Fprintf(stdout, "next: %s の追記を読み、決定に沿って続ける\n", p.Decisions)
			return 0
		}
		if rep, ok := newAnswer(p.Reply, since, reported); ok {
			if replySeen.IsZero() {
				replySeen = time.Now()
			} else if time.Since(replySeen) >= grace {
				markReported(statePath, rep.ReceivedAt)
				fmt.Fprintf(stdout, "回答は届いたが未反映: %s\n", p.Reply)
				for _, line := range summarizeReply(rep) {
					fmt.Fprintln(stdout, line)
				}
				fmt.Fprintln(stdout, "next: braindex approvals apply")
				return 2
			}
		} else {
			replySeen = time.Time{}
		}
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			fmt.Fprintf(stderr, "braindex approvals wait: %g 秒待っても回答なし(終了コード 3)\n", *timeoutSec)
			return 3
		}
		time.Sleep(interval)
	}
}

// waitBaseline は待ち始めの基準を返す。hook の印があればフォームを開いた時刻、無ければ now。
// 受信時刻(RFC 3339)は秒までしか持たないので、now は秒に切り捨てる(同じ秒の回答を落とさない)。
// 2 つめは前回の wait が知らせた回答の受信時刻(無ければゼロ値)。
func waitBaseline(statePath string, now time.Time) (since, reported time.Time) {
	since = now.Truncate(time.Second)
	st, ok := readHookState(statePath)
	if !ok {
		return since, time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, st.Opened); err == nil {
		since = t
	}
	if t, err := time.Parse(time.RFC3339, st.Reported); err == nil {
		reported = t
	}
	return since, reported
}

// newAnswer は path の回答 JSON が「since 以降に届き、まだ知らせていない」ものなら返す。
func newAnswer(path string, since, reported time.Time) (approvals.Reply, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return approvals.Reply{}, false
	}
	var rep approvals.Reply
	if err := json.Unmarshal(b, &rep); err != nil {
		return rep, false // 壊れた JSON は次の周回で読み直す
	}
	at, err := time.Parse(time.RFC3339, rep.ReceivedAt)
	if err != nil {
		return rep, false // 受信時刻の無い JSON は手で書いたもので、フォームの回答ではない
	}
	if at.Before(since) || (!reported.IsZero() && !at.After(reported)) {
		return rep, false
	}
	return rep, true
}

// markReported は知らせた回答の受信時刻を hook の印に書き足す(次の wait が同じ回答で終わらないように)。
func markReported(statePath, receivedAt string) {
	st, _ := readHookState(statePath)
	st.Reported = receivedAt
	writeHookState(statePath, st)
}
