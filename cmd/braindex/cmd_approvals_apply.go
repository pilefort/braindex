package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/pilefort/braindex/internal/approvals"
)

// runApprovalsApply は braindex approvals apply を実行する。
// 一時置き場の回答(-reply で差し替え可)を APPROVALS.md と docs/decisions.md に反映し、回答を .applied.json に改名する。
// 回答が無ければ何もせず 0 で終わる(セッション開始時に毎回呼べる)。
// 終了コード: 0 反映した・回答なし / 1 失敗(何も書かない)。
func runApprovalsApply(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("braindex approvals apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var f approvalsFileFlags
	var replyPath, decisionsPath, date string
	f.bind(fs)
	fs.StringVar(&replyPath, "reply", "", "反映する回答 JSON(既定: 置き場の approvals-<id>.reply.json)")
	fs.StringVar(&decisionsPath, "decisions", "", "決定を追記するファイル(既定: <hub>/docs/decisions.md)")
	fs.StringVar(&date, "date", "", "記録日 YYYY-MM-DD(既定: 今日)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex approvals apply [-file work/APPROVALS.md] [-reply <json>] [-decisions docs/decisions.md] [-date YYYY-MM-DD] [-dir <置き場>]")
		fmt.Fprintln(stderr, "  serve が受けた回答を反映する。選んだ項目は docs/decisions.md に 3 段(結論 → 理由 → 根拠)で追記して APPROVALS.md から消し、")
		fmt.Fprintln(stderr, "  保留は項目を残して「**保留（日付）:**」を付ける。反映した回答は .applied.json に改名する(2 回反映しない)。")
		fmt.Fprintln(stderr, "  回答が無ければ何もしない(終了コード 0)。終了コード: 0 反映した・回答なし / 1 失敗")
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
		fmt.Fprintln(stderr, "braindex approvals apply:", err)
		return 1
	}
	if fs.NArg() > 0 {
		return fail(fmt.Errorf("引数 %q は受け付けない(フラグだけを渡す)", fs.Args()))
	}
	today := date
	if today == "" {
		today = time.Now().Format("2006-01-02")
	} else if _, err := time.Parse("2006-01-02", today); err != nil {
		return fail(fmt.Errorf("-date は YYYY-MM-DD: %q", date))
	}

	p, err := approvals.Resolve(f.file, f.dir)
	if err != nil {
		return fail(err)
	}
	rename := replyPath == "" // 既定の置き場の回答だけ .applied に改名する(手で指定した JSON は動かさない)
	if replyPath == "" {
		replyPath = p.Reply
	}
	if decisionsPath == "" {
		decisionsPath = p.Decisions
	}
	rb, err := os.ReadFile(replyPath)
	if err != nil {
		if errors.Is(err, iofs.ErrNotExist) {
			fmt.Fprintf(stdout, "回答はない: %s\n", replyPath)
			return 0
		}
		return fail(fmt.Errorf("回答を読めない: %w", err))
	}
	var rep approvals.Reply
	if err := json.Unmarshal(rb, &rep); err != nil {
		return fail(fmt.Errorf("回答 %s: %w", replyPath, err))
	}
	ab, err := os.ReadFile(p.Approvals)
	if err != nil {
		return fail(fmt.Errorf("判断待ちのファイルを読めない: %w", err))
	}
	db, err := os.ReadFile(decisionsPath)
	if err != nil && !errors.Is(err, iofs.ErrNotExist) {
		return fail(fmt.Errorf("決定のファイルを読めない: %w", err))
	}

	res := approvals.Apply(ab, db, rep, today)
	// 記録する側(decisions.md)を先に書く。消す側(APPROVALS.md)を先に書くと、途中で失敗したとき
	// 決定がどちらのファイルにも残らない。逆順なら、失敗しても判断待ちがそのまま残る。
	if res.Decided > 0 {
		if err := os.MkdirAll(filepath.Dir(decisionsPath), 0o755); err != nil {
			return fail(err)
		}
		if err := os.WriteFile(decisionsPath, res.Decisions, 0o644); err != nil {
			return fail(err)
		}
	}
	if err := os.WriteFile(p.Approvals, res.Approvals, 0o644); err != nil {
		return fail(err)
	}
	if rename {
		if err := os.Rename(p.Reply, p.Applied); err != nil {
			fmt.Fprintf(stderr, "note: 回答を .applied.json に改名できない(%v)。次回 serve の前に消す\n", err)
		}
	}
	fmt.Fprintf(stdout, "反映: 決定 %d 件 → %s ／ 保留 %d 件 ／ %s を更新\n", res.Decided, decisionsPath, res.Held, p.Approvals)
	for _, line := range res.Summary {
		fmt.Fprintln(stdout, line)
	}
	if res.Decided > 0 {
		fmt.Fprintln(stdout, "next: decisions.md の見出しを結論文に整え、決定に沿って止まっていた作業を再開する")
	}
	return 0
}
