package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// runApprovalsStatus は braindex approvals status を実行する。
// id・置き場・項目数・未反映の回答の有無・記載漏れを表示する。書き込みはしない。
// 終了コード: 0 / 1 失敗 / 2 記載漏れか未反映の回答がある(直すか apply する)。
func runApprovalsStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("braindex approvals status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var f approvalsFileFlags
	f.bind(fs)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex approvals status [-config braindex.json] [-file work/APPROVALS.md] [-dir <置き場>]")
		fmt.Fprintln(stderr, "  判断待ちの件数・記載漏れ・未反映の回答の有無を表示する(書き込みなし)。")
		fmt.Fprintln(stderr, "  終了コード: 0 / 1 失敗 / 2 記載漏れか未反映の回答がある")
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
		fmt.Fprintf(stderr, "braindex approvals status: 引数 %q は受け付けない(フラグだけを渡す)\n", fs.Args())
		return 1
	}
	d, p, err := loadApprovals(f)
	if err != nil {
		fmt.Fprintln(stderr, "braindex approvals status:", err)
		return 1
	}
	exists := func(path string) string {
		if _, err := os.Stat(path); err == nil {
			return "あり"
		}
		return "なし"
	}
	fmt.Fprintf(stdout, "id=%s project=%s\n", p.ID, p.Project)
	fmt.Fprintf(stdout, "approvals=%s items=%d\n", p.Approvals, len(d.Items))
	fmt.Fprintf(stdout, "decisions=%s (%s)\n", p.Decisions, exists(p.Decisions))
	hasReply := false
	replyState := "なし"
	if _, err := os.Stat(p.Reply); err == nil {
		hasReply = true
		replyState = "未反映の回答あり → braindex approvals apply"
	}
	fmt.Fprintf(stdout, "reply=%s (%s)\n", p.Reply, replyState)
	for _, it := range d.Items {
		fmt.Fprintf(stdout, "[%d] %s: 選択肢 %d", it.N, it.Title, len(it.Options))
		if it.Recommended != "" {
			fmt.Fprintf(stdout, "・私の案 %s", it.Recommended)
		}
		if len(it.Holds) > 0 {
			fmt.Fprintf(stdout, "・保留 %d", len(it.Holds))
		}
		fmt.Fprintln(stdout)
	}
	warned := printApprovalWarnings(d, stderr)
	return approvalsStatusExitCode(warned, hasReply)
}

// approvalsStatusExitCode は status の終了コードを決める。
// 判定は表示文字列(replyState 等)を比較せず、真偽値(記載漏れの件数・回答ファイルの有無)だけで行う。
// 表示の文言を変えても判定は壊れない。
func approvalsStatusExitCode(warned int, hasReply bool) int {
	if warned > 0 || hasReply {
		return 2
	}
	return 0
}
