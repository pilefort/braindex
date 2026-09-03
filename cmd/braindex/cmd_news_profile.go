package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	iofs "io/fs" // fs はフラグ集合の変数名に使っている
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pilefort/braindex/internal/config"
	"github.com/pilefort/braindex/internal/interest"
	"github.com/pilefort/braindex/internal/news"
	"github.com/pilefort/braindex/internal/review"
	"github.com/pilefort/braindex/internal/sessions"
)

// newsProfileOptions は braindex news profile のコマンドライン。空・0 は「未指定」。
type newsProfileOptions struct {
	config   string // -config。hub の位置を兼ねるので必須
	date     string // -date。今日の固定(既定: 実行日)
	days     int    // -days。直近の日数(既定: 設定 news.profile_days → 14)
	sessions string // -sessions。セッションログの置き場(既定: news.sessions_dir → retro.sessions_dir → ~/.claude/projects)
	top      int    // -top。表に出す語数(既定 100。0 で全件)
	json     bool   // -json。JSON で出す
}

var keepFileName = regexp.MustCompile(`^(\d{4}-\d{2})\.md$`)

// runNewsProfile は braindex news profile を実行する。
//
// 4 つの出典(索引の直近差分・セッション・keep 履歴・補助ファイル)を読んで関心プロファイルを標準出力に書く。
// 出典が無い(索引が無い・セッションの置き場が無い)ときは警告して飛ばし、残りで作る(終了コード 2)。
// セッション本文は読むだけで、どこにも書かず送らない。
func runNewsProfile(args []string, stdout, stderr io.Writer) int {
	var o newsProfileOptions
	fs := flag.NewFlagSet("braindex news profile", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json。そのディレクトリを hub とみなす)")
	fs.StringVar(&o.date, "date", "", "今日として使う日付 YYYY-MM-DD(既定: 実行日)。窓の基準")
	fs.IntVar(&o.days, "days", 0, "直近何日の索引とセッションを見るか(既定: 設定 news.profile_days → 14)")
	fs.StringVar(&o.sessions, "sessions", "", "セッションログの置き場(既定: 設定 news.sessions_dir → retro.sessions_dir → ~/.claude/projects)")
	fs.IntVar(&o.top, "top", 100, "表に出す語数(0 で全件)")
	fs.BoolVar(&o.json, "json", false, "JSON で出す(全件)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex news profile [-config braindex.json] [-date YYYY-MM-DD] [-days N] [-sessions DIR] [-top N] [-json]")
		fmt.Fprintln(stderr, "  関心プロファイル(語 → 重み・出典)を標準出力に書く。出典は 索引の直近差分(index/catalog.md)・直近のセッション内容・")
		fmt.Fprintln(stderr, "  選別で残した見出し(news/keep/YYYY-MM.md)・補助の関心ファイル(news/interests.md・1 行 1 語)。")
		fmt.Fprintln(stderr, "  重みは出典ごとに最大を 1 に正規化した値の和。決定論で、LLM は使わない。セッション本文は読むだけで送らない。")
		fmt.Fprintln(stderr, "  終了コード: 0 成功 / 1 失敗 / 2 警告つきで完了(索引やセッションの置き場が無く、その出典を飛ばした)")
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
		fmt.Fprintf(stderr, "braindex news profile: 引数 %q は受け付けない(フラグだけを渡す)\n", fs.Args())
		fs.Usage()
		return 1
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "braindex news profile:", err)
		return 1
	}
	var warnings []string
	warn := func(format string, a ...any) { warnings = append(warnings, fmt.Sprintf(format, a...)) }

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
	s := fc.News.WithDefaults()
	newsDir := filepath.Join(hubDir, filepath.FromSlash(s.Dir))
	days := o.days
	if days <= 0 {
		days = s.ProfileDays
	}
	in := interest.Input{Today: today, Days: days}

	// 出典 1: 索引
	catalogPath := filepath.Join(hubDir, filepath.FromSlash(defaultOut))
	if b, err := os.ReadFile(catalogPath); err == nil {
		in.Catalog, err = review.ParseCatalog(b)
		if err != nil {
			return fail(fmt.Errorf("%s: %w", catalogPath, err))
		}
	} else if errors.Is(err, iofs.ErrNotExist) {
		warn("索引 %s が無いので飛ばした(braindex で生成する)", catalogPath)
	} else {
		return fail(err)
	}

	// 出典 2: セッション
	sessDir := o.sessions
	if sessDir == "" {
		sessDir = s.SessionsDir
	}
	if sessDir == "" {
		sessDir = fc.Retro.SessionsDir
	}
	if sessDir == "" {
		sessDir, err = sessions.DefaultDir()
		if err != nil {
			return fail(err)
		}
	}
	since, err := time.Parse("2006-01-02", today)
	if err != nil {
		return fail(err)
	}
	since = since.AddDate(0, 0, -days)
	if _, err := os.Stat(sessDir); errors.Is(err, iofs.ErrNotExist) {
		warn("セッションログの置き場 %s が無いので飛ばした", sessDir)
	} else if err != nil {
		return fail(err)
	} else {
		ss, ws, err := sessions.Dir{Path: sessDir}.Sessions(sessions.Options{Since: since})
		if err != nil {
			return fail(err)
		}
		warnings = append(warnings, ws...)
		in.Sessions = ss
	}

	// 出典 3: keep 履歴
	keepDir := filepath.Join(newsDir, news.KeepDir)
	if names, err := os.ReadDir(keepDir); err == nil {
		sort.Slice(names, func(i, j int) bool { return names[i].Name() < names[j].Name() })
		for _, de := range names {
			m := keepFileName.FindStringSubmatch(de.Name())
			if m == nil || de.IsDir() {
				continue
			}
			b, err := os.ReadFile(filepath.Join(keepDir, de.Name()))
			if err != nil {
				return fail(err)
			}
			in.Keeps = append(in.Keeps, interest.ParseKeep(m[1], string(b))...)
		}
	} else if !errors.Is(err, iofs.ErrNotExist) {
		return fail(err)
	}

	// 出典 4: 補助ファイル
	if b, err := os.ReadFile(filepath.Join(newsDir, news.InterestsFile)); err == nil {
		in.Extra = strings.Split(string(b), "\n")
	} else if !errors.Is(err, iofs.ErrNotExist) {
		return fail(err)
	}

	p, err := interest.Build(in)
	if err != nil {
		return fail(err)
	}
	var out []byte
	if o.json {
		out, err = p.JSON()
		if err != nil {
			return fail(err)
		}
	} else {
		out = p.Marshal(o.top)
	}
	if _, err := stdout.Write(out); err != nil {
		return fail(err)
	}
	for _, w := range warnings {
		fmt.Fprintln(stderr, "braindex news profile: 警告:", w)
	}
	if len(warnings) > 0 {
		fmt.Fprintf(stderr, "braindex news profile: 警告 %d 件(終了コード 2)\n", len(warnings))
		return 2
	}
	return 0
}
