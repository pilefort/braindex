package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/pilefort/braindex/internal/config"
	"github.com/pilefort/braindex/internal/interest"
	"github.com/pilefort/braindex/internal/learn"
	"github.com/pilefort/braindex/internal/retro"
)

func init() {
	register(&command{
		name:    "learn",
		summary: "学習の提案。関心プロファイルと訂正の文脈から「いま学ぶと良さそうなこと」の候補を理由つきで出す(手元の材料だけ・LLM なし)",
		run:     runLearn,
	})
}

type learnOptions struct {
	config      string // -config。hub の位置を兼ねるので必須
	date        string // -date。今日の固定(既定: 実行日)
	days        int    // -days。直近の日数(既定: 設定 news.profile_days → 14)
	sessions    string // -sessions。セッションログの置き場(既定: news.sessions_dir → retro.sessions_dir → ~/.claude/projects)
	allProjects bool   // -all-projects。root の外のセッションも数える
	top         int    // -top。各節の件数(既定 10。0 で全件)
	json        bool   // -json。JSON で出す
}

// runLearn は braindex learn を実行する。
//
// 材料は braindex news profile と同じ(索引・セッション・keep・補助。窓も news.profile_days を共有)。
// 訂正の判定は retro と同じ辞書(設定 retro.dictionary があればそれ、無ければ既定)。
// 候補を出したあと、「索引に無い」語をノート本文で照合する(braindex search と同じ走査規則・同じ検索処理)。
// 終了コード: 0 成功 / 1 失敗 / 2 警告つき(索引やセッションの置き場が無い・本文を読めなかった範囲があるなど)。
func runLearn(args []string, stdout, stderr io.Writer) int {
	var o learnOptions
	fs := flag.NewFlagSet("braindex learn", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json。そのディレクトリを hub とみなす)")
	fs.StringVar(&o.date, "date", "", "今日として使う日付 YYYY-MM-DD(既定: 実行日)。窓の基準")
	fs.IntVar(&o.days, "days", 0, "直近何日の索引とセッションを見るか(既定: 設定 news.profile_days → 14)")
	fs.StringVar(&o.sessions, "sessions", "", "セッションログの置き場(既定: 設定 news.sessions_dir → retro.sessions_dir → ~/.claude/projects)")
	fs.BoolVar(&o.allProjects, "all-projects", false, "root の外で交わしたセッションも数える(既定: root 配下だけ。設定 retro.all_projects と同じ)")
	fs.IntVar(&o.top, "top", 10, "各節に出す件数(0 で全件)")
	fs.BoolVar(&o.json, "json", false, "Markdown でなく JSON で出す")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex learn [-config braindex.json] [-date YYYY-MM-DD] [-days N] [-sessions DIR] [-top N] [-json]")
		fmt.Fprintln(stderr, "  「いま学ぶと良さそうなこと」の候補を 3 つの節で出す。材料は手元だけ(索引・セッションログ・news/keep・ノート本文)で、外には何も送らない。")
		fmt.Fprintln(stderr, "    触れているが索引に無い         … セッションに繰り返し出るのに索引にも keep にも無い語(既定: 3 セッション以上)")
		fmt.Fprintln(stderr, "    訂正の文脈に繰り返し出る       … 訂正の発話とその直前の発話に出る語(既定: 2 発話以上。辞書は retro と同じ)")
		fmt.Fprintln(stderr, "    残した記事にあるが索引に無い   … keep の見出しにあるのに索引に無い語")
		fmt.Fprintln(stderr, "  「索引に無い」語は索引と同じ走査規則でノート本文を照合し、本文で発見(パス:行)・本文でも未発見・確認不能(読めなかった範囲がある)を分けて出す。")
		fmt.Fprintln(stderr, "  出力は語と件数と出典の位置だけ(発話やノートの本文は載せない)。窓と材料は braindex news profile と同じ。")
		fmt.Fprintln(stderr, "  終了コード: 0 成功 / 1 失敗 / 2 警告つき(索引やセッションの置き場が無い・本文を読めなかった範囲がある)")
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
		fmt.Fprintf(stderr, "braindex learn: 引数 %q は受け付けない(フラグだけを渡す)\n", fs.Args())
		fs.Usage()
		return 1
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "braindex learn:", err)
		return 1
	}
	if o.top < 0 {
		return fail(fmt.Errorf("-top は 0 以上: %d", o.top))
	}

	cfgPath := o.config
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
	today := o.date
	if today == "" {
		today = time.Now().Format("2006-01-02")
	} else if _, perr := time.Parse("2006-01-02", today); perr != nil {
		return fail(fmt.Errorf("-date は YYYY-MM-DD で指定する: %q", today))
	}
	hubDir := filepath.Dir(cfgPath)
	in, warnings, err := loadProfileInput(fc, hubDir, today, o.days, o.sessions, o.allProjects)
	if err != nil {
		return fail(err)
	}
	p, err := interest.Build(in)
	if err != nil {
		return fail(err)
	}
	if err := fc.Retro.Validate(); err != nil {
		return fail(err)
	}
	home, _ := os.UserHomeDir()
	dicts, err := loadRetroDictionaries(fc.Retro.WithDefaults(), hubDir, home)
	if err != nil {
		return fail(err)
	}
	// 窓は interest.Build と同じものをそのまま使う。信号 1・3 と 2 が同じ材料を見るように揃える
	// (以前はここで日付を UTC で解き直していて、ローカルとの時差の分だけ窓がずれた。設計レビュー 2026-09-06 M3b)
	r := learn.Build(learn.Input{
		Profile:  p,
		Catalog:  in.Catalog,
		Sessions: in.Sessions,
		Window:   retro.Window{Since: in.Since, Until: in.Until},
		Dicts:    dicts,
		Options:  learn.Options{Top: o.top},
	})
	// 「索引に無い」候補を本文で照合する。走査設定の解決は索引生成と同じ(設定ファイルの root)。
	// root が無い・走査できない hub では照合を飛ばして警告にし、候補は索引だけの判定のまま出す(落とさない)。
	if cfg, _, _, cerr := resolve(options{config: cfgPath}); cerr != nil {
		warnings = append(warnings, "本文照合を飛ばした: "+cerr.Error())
	} else if verr := learn.Verify(&r, learn.BodySearcher(cfg), learn.VerifyOptions{}); verr != nil {
		warnings = append(warnings, "本文照合を飛ばした: "+verr.Error())
	} else if r.Verification != nil {
		warnings = append(warnings, r.Verification.Warnings...)
	}
	var out []byte
	if o.json {
		out, err = r.JSON()
		if err != nil {
			return fail(err)
		}
	} else {
		out = r.Marshal()
	}
	if _, err := stdout.Write(out); err != nil {
		return fail(err)
	}
	for _, w := range warnings {
		fmt.Fprintln(stderr, "braindex learn: 警告:", w)
	}
	if len(warnings) > 0 {
		fmt.Fprintf(stderr, "braindex learn: 警告 %d 件(終了コード 2)\n", len(warnings))
		return 2
	}
	return 0
}
