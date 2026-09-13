package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/pilefort/braindex/internal/config"
	"github.com/pilefort/braindex/internal/news"
)

// newsSuggestOptions は braindex news suggest のコマンドライン。空・0 は「未指定」。
type newsSuggestOptions struct {
	config      string // -config。hub の位置を兼ねるので必須
	date        string // -date。今日の固定(既定: 実行日)
	days        int    // -days。直近の日数(既定: 設定 news.profile_days → news.DefaultProfileDays)
	sessions    string // -sessions。セッションログの置き場(news profile と同じ既定)
	allProjects bool   // -all-projects。root の外のセッションも数える
	top         int    // -top。出す候補数(既定 10。0 で全件)
	json        bool   // -json
}

// runNewsSuggest は braindex news suggest を実行する。
//
// 関心プロファイル(news profile と同じ材料・同じ窓)の語と、同梱の取材先目録の照合語を手元で突き合わせ、
// まだ feeds.json に無い取材先を当たった語つきで出す。通信はせず、LLM も使わない。
// feeds.json が無ければ「登録済みなし」として進める(警告・終了コード 2)。
func runNewsSuggest(args []string, stdout, stderr io.Writer) int {
	var o newsSuggestOptions
	fs := flag.NewFlagSet("braindex news suggest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json。そのディレクトリを hub とみなす)")
	fs.StringVar(&o.date, "date", "", "今日として使う日付 YYYY-MM-DD(既定: 実行日)。窓の基準")
	fs.IntVar(&o.days, "days", 0, fmt.Sprintf("直近何日の索引とセッションを見るか(既定: 設定 news.profile_days → %d)", news.DefaultProfileDays))
	fs.StringVar(&o.sessions, "sessions", "", "セッションログの置き場(既定: news profile と同じ)")
	fs.BoolVar(&o.allProjects, "all-projects", false, "root の外で交わしたセッションも数える(既定: root 配下だけ。設定 retro.all_projects と同じ)")
	fs.IntVar(&o.top, "top", 10, "出す候補数(0 で全件)")
	fs.BoolVar(&o.json, "json", false, "JSON で出す")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex news suggest [-config braindex.json] [-date YYYY-MM-DD] [-days N] [-sessions DIR] [-top N] [-json]")
		fmt.Fprintln(stderr, "  直近の会話・索引・keep から作った関心プロファイル(braindex news profile)の語に当たる取材先(RSS)を、")
		fmt.Fprintln(stderr, "  同梱の取材先目録から候補として出す。news/feeds.json に登録済みのものは除く。")
		fmt.Fprintln(stderr, "  照合は手元で行い、通信も LLM もしない。出力は当たった語と数だけで、発話の本文は載せない。")
		fmt.Fprintln(stderr, "  終了コード: 0 成功 / 1 失敗 / 2 警告つきで完了(索引・セッションの置き場・feeds.json が無く飛ばした)")
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
		fmt.Fprintf(stderr, "braindex news suggest: 引数 %q は受け付けない(フラグだけを渡す)\n", fs.Args())
		fs.Usage()
		return 1
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "braindex news suggest:", err)
		return 1
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
	if o.top < 0 {
		return fail(fmt.Errorf("-top は 0 以上: %d", o.top))
	}
	today := o.date
	if today == "" {
		today = time.Now().Format("2006-01-02")
	} else if _, perr := time.Parse("2006-01-02", today); perr != nil {
		return fail(fmt.Errorf("-date は YYYY-MM-DD で指定する: %q", today))
	}
	hubDir := filepath.Dir(cfgPath)
	p, warnings, err := loadProfile(fc, hubDir, today, o.days, o.sessions, o.allProjects)
	if err != nil {
		return fail(err)
	}

	s := fc.News.WithDefaults()
	feedsPath := filepath.Join(hubDir, filepath.FromSlash(s.Feeds))
	var feeds []news.Source
	if _, serr := os.Stat(feedsPath); errors.Is(serr, iofs.ErrNotExist) {
		warnings = append(warnings, fmt.Sprintf("フィード一覧 %s が無いので登録済みなしとして進めた", feedsPath))
	} else if serr != nil {
		return fail(serr)
	} else if feeds, err = news.LoadFeeds(feedsPath); err != nil {
		return fail(err)
	}

	r := news.BuildSuggestReport(news.Catalog(), p, feeds, o.top)
	var out []byte
	if o.json {
		if out, err = r.JSON(); err != nil {
			return fail(err)
		}
	} else {
		out = bytes.Replace(r.Marshal(), []byte("を足す。"), []byte("を足すか、`news fetch` の選別画面で「追加する」を選ぶ。"), 1)
	}
	if _, err := stdout.Write(out); err != nil {
		return fail(err)
	}
	for _, w := range warnings {
		fmt.Fprintln(stderr, "braindex news suggest: 警告:", w)
	}
	if len(warnings) > 0 {
		fmt.Fprintf(stderr, "braindex news suggest: 警告 %d 件(終了コード 2)\n", len(warnings))
		return 2
	}
	return 0
}
