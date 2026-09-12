package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	iofs "io/fs" // fs はフラグ集合の変数名に使っている
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pilefort/braindex/internal/config"
	"github.com/pilefort/braindex/internal/feed"
	"github.com/pilefort/braindex/internal/fsutil"
	"github.com/pilefort/braindex/internal/news"
)

func init() {
	register(&command{
		name:    "news",
		summary: "ニュースサジェスト。news fetch でフィードを取得し、新着のダイジェスト(news/digest_<日付>_<層>.md)を書く",
		run:     runNews,
	})
}

// newsFetcher は fetch が使う取得器。テストで差し替える(ネットワークに出ないため)。
var newsFetcher news.Fetcher = feed.Fetcher{}

// saveSeen は既読の保存。テストで差し替える(保存の失敗を再現するため)。
var saveSeen = news.Seen.Save

// newNewsAnnotator は LLM 補助(news.llm = claude-cli)の呼び出し側を作る。claude CLI が PATH に無ければ news.ErrNoClaudeCLI。
// テストで差し替える(CLI を呼ばないため)。
var newNewsAnnotator = func(s news.Settings) (news.Annotator, error) {
	c := news.ClaudeCLI{Model: s.LLMModel, Timeout: time.Duration(s.LLMTimeoutSec) * time.Second}
	if err := c.Available(); err != nil {
		return nil, err
	}
	return c, nil
}

// runNews は braindex news <サブコマンド> を振り分ける(fetch / profile / apply / suggest)。
func runNews(args []string, stdout, stderr io.Writer) int {
	usage := func() {
		fmt.Fprintln(stderr, "使い方: braindex news <サブコマンド> [フラグ]")
		fmt.Fprintln(stderr, "  fetch    フィードを取得し、既読に無い記事のダイジェスト(Markdown)を書く")
		fmt.Fprintln(stderr, "  profile  関心プロファイル(語 → 重み・出典)を表示する")
		fmt.Fprintln(stderr, "  apply    HTML で書き出した選別 JSON を取り込む(keep に追記・統計を更新)")
		fmt.Fprintln(stderr, "  suggest  関心プロファイルに当たる取材先(RSS)を同梱の目録から候補として出す")
		fmt.Fprintln(stderr, "  reading  保存記事と相談・解説を読む。会話で作成した回答を記事へ登録する")
		fmt.Fprintln(stderr, "  overview 会話で書いた概要の Markdown を、記事ごとに仕分けできる HTML にして開く")
		fmt.Fprintln(stderr, "フラグは braindex news <サブコマンド> -h")
	}
	if len(args) == 0 {
		usage()
		return 1
	}
	switch args[0] {
	case "fetch":
		return runNewsFetch(args[1:], stdout, stderr)
	case "profile":
		return runNewsProfile(args[1:], stdout, stderr)
	case "apply":
		return runNewsApply(args[1:], stdout, stderr)
	case "suggest":
		return runNewsSuggest(args[1:], stdout, stderr)
	case "reading":
		return runNewsReading(args[1:], stdout, stderr)
	case "overview":
		return runNewsOverview(args[1:], stdout, stderr)
	case "-h", "-help", "--help":
		usage()
		return 0
	}
	fmt.Fprintf(stderr, "braindex news: サブコマンド %q は無い\n", args[0])
	usage()
	return 1
}

// newsFetchOptions は braindex news fetch のコマンドライン。空は「未指定」。
type newsFetchOptions struct {
	config      string // -config。hub の位置を兼ねるので必須(既定パスに無ければエラー)
	date        string // -date。今日の固定(既定: 実行日)
	layer       string // -layer。フィードの層(既定 all)
	replay      bool   // -replay。既読を無視して全件を出し、既読も更新しない
	stdout      bool   // -stdout。ファイルに書かず標準出力へ(既読は更新する)
	out         string // -out。出力先(既定: <news.dir>/digest_<日付>_<層>.md。既にあれば書かない)
	inbox       string // -inbox。選別 JSON を探すディレクトリ(既定 ~/Downloads)
	noOpen      bool   // -no-open。HTML を既定ブラウザで開かない
	noScore     bool   // -no-score。関心プロファイルで採点しない(全件を主要表示)
	noLLM       bool   // -no-llm。設定 news.llm が claude-cli でも LLM 補助を呼ばない
	sessions    string // -sessions。関心プロファイルのセッションログの置き場(news profile と同じ既定)
	allProjects bool   // -all-projects。root の外のセッションも数える
}

// runNewsFetch は braindex news fetch を実行する。
//
// 終了コード: 0 成功 / 1 失敗(出力先が既にある・-out と -stdout の同時指定・別の braindex news が動いている・全フィードの取得失敗・保存失敗を含む。書き込み済みの場合もある) /
// 2 警告つきで完了(一部のフィードが取得できなかった・採点の出典(索引・セッションの置き場)が無かった・
// 選別や統計を取り込めなかった・残留したロックを外した・別の日の未完了が残っている)。
//
// 保存物は news/.lock.json で排他し、md → html → 既読 の書き始めから終わりまでを news/.pending.json に記録する
// (中断からの立て直し → internal/news/run.go)。
func runNewsFetch(args []string, stdout, stderr io.Writer) int {
	var o newsFetchOptions
	fs := flag.NewFlagSet("braindex news fetch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json。そのディレクトリを hub とみなす)")
	fs.StringVar(&o.date, "date", "", "今日として使う日付 YYYY-MM-DD(既定: 実行日)。出力ファイル名と既読の日付に使う")
	fs.StringVar(&o.layer, "layer", news.LayerAll, "取得するフィードの層(feeds.json の layer)。all は全件")
	fs.BoolVar(&o.replay, "replay", false, "既読を無視して全記事を出し、既読も更新しない(見出しの再生成用)")
	fs.BoolVar(&o.stdout, "stdout", false, "Markdown をファイルに書かず標準出力に出す(進捗は stderr。既読は更新する。HTML は作らず開かない)。-out とは同時に指定できない")
	fs.StringVar(&o.out, "out", "", "Markdown の出力先(既定: 設定 news.dir の digest_<日付>_<層>.md。既にあれば書かずに終了コード 1)。HTML は拡張子を .html にした同名。-stdout とは同時に指定できない")
	fs.StringVar(&o.inbox, "inbox", "", "選別 JSON を探すディレクトリ(既定: ~/Downloads。<news.dir>/inbox はいつも見る)")
	fs.BoolVar(&o.noOpen, "no-open", false, "HTML を既定ブラウザで開かない(定期実行やテスト用)")
	fs.BoolVar(&o.noScore, "no-score", false, "関心プロファイルで採点しない(全件を主要表示・出典を読まない)")
	fs.BoolVar(&o.noLLM, "no-llm", false, "LLM 補助(設定 news.llm = claude-cli の翻訳＋採点)を呼ばない(語の一致の点だけで出す)")
	fs.StringVar(&o.sessions, "sessions", "", "関心プロファイルが読むセッションログの置き場(既定: news profile と同じ)")
	fs.BoolVar(&o.allProjects, "all-projects", false, "root の外で交わしたセッションも数える(既定: root 配下だけ。設定 retro.all_projects と同じ)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex news fetch [-config braindex.json] [-date YYYY-MM-DD] [-layer <層>] [-replay] [-stdout] [-out <path>] [-no-open] [-no-score] [-no-llm] [-sessions DIR]")
		fmt.Fprintln(stderr, "  hub のルートで実行し、news/feeds.json のフィードを GET して、既読(news/.seen.json)に無い記事を")
		fmt.Fprintln(stderr, "  news/digest_<日付>_<層>.md(記録用)と同名の .html(選別 UI・既定ブラウザで開く)に書く。")
		fmt.Fprintln(stderr, "  関心プロファイル(braindex news profile)で採点し、関心度 news.show_min_score 以上を主要表示、未満を「関心外と判定」に")
		fmt.Fprintln(stderr, "  折りたたむ。HTML の「選別を書き出す」が出す JSON は braindex news apply が取り込む。")
		fmt.Fprintln(stderr, "  外へ出る通信はフィードの GET だけ(セッション本文は送らない)。HTML は外部の JS / CSS を参照しない。")
		fmt.Fprintln(stderr, "  LLM 補助(opt-in): 設定 news.llm を \"claude-cli\" にすると、claude CLI をヘッドレスで呼んで英語見出しの翻訳と関心度(0〜3)を付け、")
		fmt.Fprintln(stderr, "  語の一致の点に重ねる(バッジの説明に LLM と出る)。渡すのは見出し・概要・プロファイルの語・keep の見出しだけ。")
		fmt.Fprintln(stderr, "  結果は news/.llm_cache.json に覚えて同じ記事を 2 回聞かない。CLI が無い・失敗した分は語の点のまま(警告・終了コード 2)。")
		fmt.Fprintln(stderr, "  途中で止まった回は news/.pending.json に記録が残り、同じ日をもう一度実行すると書き直して既読まで進める(完了した回は「既にある」で止まる)。")
		fmt.Fprintln(stderr, "  並行起動は news/.lock.json で片方だけにする(1 時間より古い残留は外して進む)。")
		fmt.Fprintln(stderr, "  既読の保存に失敗した場合も、ダイジェストは書き込み済み。同じ日を再実行すると未完了の記録から書き直す。")
		fmt.Fprintln(stderr, "  終了コード: 0 成功 / 1 失敗(出力先が既にある・-out と -stdout の同時指定・別の braindex news が動いている・全フィードの取得失敗・保存失敗。書き込み済みの場合もある) / 2 警告つきで完了(一部のフィードが取得できなかった・")
		fmt.Fprintln(stderr, "  採点の出典(索引・セッションの置き場)が無かった・選別や統計を取り込めなかった・残留したロックを外した・別の日の未完了が残っている)")
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
		fmt.Fprintf(stderr, "braindex news fetch: 引数 %q は受け付けない(フラグだけを渡す)\n", fs.Args())
		fs.Usage()
		return 1
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "braindex news fetch:", err)
		return 1
	}
	if o.stdout && o.out != "" {
		// -stdout はファイルに書かないので -out は使われない。黙って無視せず、フラグの誤りとして拒否する
		return fail(errors.New("-out と -stdout は同時に指定できない(-stdout はファイルに書かないので -out は使われない)"))
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
	if o.layer == "" {
		return fail(errors.New("-layer が空(all か feeds.json の layer を指定する)"))
	}
	hubDir := filepath.Dir(cfgPath)
	if err := fc.News.Validate(); err != nil {
		return fail(err)
	}
	s := fc.News.WithDefaults()
	newsDir := filepath.Join(hubDir, filepath.FromSlash(s.Dir))

	all, err := news.LoadFeeds(filepath.Join(hubDir, filepath.FromSlash(s.Feeds)))
	if err != nil {
		return fail(err)
	}
	srcs := news.FilterLayer(all, o.layer)
	if len(srcs) == 0 {
		return fail(fmt.Errorf("層 %q のフィードが無い(feeds.json にある層: %v)", o.layer, news.Layers(all)))
	}
	// 排他: 定期実行と手動が重なっても、保存物(既読・統計・keep・ダイジェスト)を触るのは片方だけ。
	// 残留(前の実行が落ちた)を外して進んだときは警告にする
	unlock, stale, err := news.Lock(newsDir, "fetch")
	if err != nil {
		return fail(err)
	}
	defer unlock()
	lockWarning := 0
	if stale != "" {
		fmt.Fprintln(stderr, "braindex news fetch: 警告:", stale)
		lockWarning = 1
	}
	seenPath := filepath.Join(newsDir, news.SeenFile)
	seen, err := news.LoadSeen(seenPath)
	if err != nil {
		return fail(err)
	}

	// -stdout のときは標準出力をダイジェスト専用にし、進捗は stderr へ出す(リダイレクトでそのまま読めるように)。
	progress := stdout
	if o.stdout {
		progress = stderr
	}

	// 出力先は取得の前に確かめる。既にあれば書かない(braindex review と同じ規則・決定 2026-09-03 → manual/news.md「決めたこと」)。
	// 取得の後に落とすと、既読だけ進んで手元に何も残らない回ができる。
	// 例外は前回の fetch が途中で止まった出力先(news/.pending.json に記録が残っている): 完了していないので書き直す。
	pendingPath := filepath.Join(newsDir, news.PendingFile)
	pendingWarning := 0
	pending, err := news.LoadPending(pendingPath)
	if err != nil {
		// 記録が壊れていても今日の新着は出す。前回の出力先は「既にある」の規則に戻る(書き直さない)
		fmt.Fprintf(stderr, "braindex news fetch: 警告: %v\n", err)
		pendingWarning++
		pending = news.Pending{}
	}
	outPath := o.out
	if outPath == "" {
		outPath = filepath.Join(newsDir, fmt.Sprintf("digest_%s_%s.md", today, o.layer))
	}
	var resume news.PendingRun
	resuming := false
	if !o.stdout {
		// 記録に残すので絶対パスにする(次回が別のカレントディレクトリから動いても同じ出力先と分かる)
		if outPath, err = filepath.Abs(outPath); err != nil {
			return fail(err)
		}
		resume, resuming = pending.Find(outPath)
		if _, serr := os.Lstat(outPath); serr == nil && !resuming {
			return fail(fmt.Errorf("既にある: %s(同じ日の 2 回目は上書きしない。-out で別名を指定するか、-stdout で標準出力に出す)", outPath))
		} else if serr != nil && !errors.Is(serr, iofs.ErrNotExist) {
			return fail(serr)
		}
	}
	// 書き直す回は、その日に付いた既読の印を外してから数え直す。既読を書いた後・記録を消す前に止まると
	// 既読だけが進んで記録が残り、そのまま数えると新着 0 件になって、書けていたダイジェストを消してしまう
	// (外部レビュー 2026-09-12)。外した印は下の Collect が付け直す。
	if resuming && !o.replay && resume.Date != "" {
		seen.Forget(resume.Date)
	}
	// 別の回の未完了は、この実行では完了させられない。伝えて、記録は残す
	for _, r := range pending.Runs {
		if resuming && r.Primary() == resume.Primary() {
			continue
		}
		fmt.Fprintf(stderr, "braindex news fetch: 警告: 前回の news fetch(%s・%s 層・%s 開始)は完了していない: %s(既読が進んでいないので、その記事は次も新着に出る)。`%s` で書き直して完了する。要らなければ %s から消す\n",
			r.Date, r.Layer, r.Started, r.Primary(), resumeCommand(r), pendingPath)
		pendingWarning++
	}

	// 前回の選別 JSON を取り込む(keep と統計に反映。今回の関心プロファイルにも効く)。
	// 取り込めなくても今日の新着は出す。ここで止めると、壊れた JSON が 1 つ残っているだけで
	// ダイジェストが出なくなる(リポの規約: 完了できるものは警告つき完了の 2)。
	ingestWarning := 0 // 取り込みの警告(選別 JSON・統計)。ダイジェストは書くので終了コード 2 に数える
	if err := ingestSelections(newsDir, news.FeedNames(all), o.inbox, progress); err != nil {
		fmt.Fprintf(stderr, "braindex news fetch: 警告: 選別を取り込めない(keep と統計は前回のまま): %v\n", err)
		ingestWarning++
	}
	stats, err := news.LoadStats(filepath.Join(newsDir, news.StatsFile))
	if err != nil {
		// 統計はフィード別の採否の表示に使うだけなので、読めなくても新着は出す
		fmt.Fprintf(stderr, "braindex news fetch: 警告: %v(統計なしで続ける)\n", err)
		ingestWarning++
	}

	results := news.Collect(context.Background(), newsFetcher, srcs, seen, today, o.replay)
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(stderr, "braindex news fetch: 警告: %s: %v\n", r.Source.Name, r.Err)
		} else {
			fmt.Fprintf(progress, "%s: 新着 %d / 全 %d\n", r.Source.Name, len(r.New), len(r.Entries))
		}
	}
	if news.AllFailed(results) {
		return fail(errors.New("全フィードの取得に失敗した(ネットワークとフィードの URL を確認する)"))
	}

	// 採点(関心プロファイル)。出典が無い警告は fetch の警告として数える
	var ranking news.Ranking
	var profileTerms []string
	var demoted map[string]bool // 上限を下げる取材先。LLM の点を重ねた後にもう一度効かせる
	profileWarnings := 0
	if !o.noScore {
		p, ws, err := loadProfile(fc, hubDir, today, 0, o.sessions, o.allProjects)
		if err != nil {
			return fail(err)
		}
		for _, w := range ws {
			fmt.Fprintln(stderr, "braindex news fetch: 警告:", w)
		}
		profileWarnings = len(ws)
		// 不要ばかり付く取材先は点の上限を下げて主要表示から下ろす(決定 2026-09-06 → manual/news.md「決めたこと」)
		demoted = news.DemotedFeeds(stats.Totals())
		ranking = news.Rank(results, p, demoted)
		if ranking == nil {
			fmt.Fprintln(stdout, "関心プロファイルが空なので採点なし(全件を主要表示)")
		} else if len(demoted) > 0 {
			fmt.Fprintf(stdout, "不要が多い取材先 %d 本は関心度の上限を %d に下げた(残す／不要 %d 件以上・不要率 %.0f%% 超)\n",
				len(demoted), news.DemotedMaxScore, news.DemoteMinJudged, news.DemoteDropRate*100)
		}
		for _, t := range p.Terms {
			profileTerms = append(profileTerms, t.Word)
		}
	}

	// LLM 補助(opt-in)。翻訳と関心度を語の点に重ねる。失敗はその分を語の点のままにして警告に数える
	var annotations news.Annotations
	llmWarnings := 0
	if s.LLM == news.LLMClaudeCLI && !o.noLLM && !o.noScore { // -no-score は「採点しない」なので LLM の採点も止める
		ann, ws, err := annotateWithLLM(s, newsDir, results, profileTerms, progress)
		if err != nil {
			return fail(err)
		}
		for _, w := range ws {
			fmt.Fprintln(stderr, "braindex news fetch: 警告: LLM 補助:", w)
		}
		llmWarnings = len(ws)
		if ann != nil {
			annotations = ann
			// LLM の点は語の点を上書きするので、下げた取材先の上限はここでもう一度かける
			// (かけないと「上限を下げた」と言いながら主要表示に出る)
			ranking = news.CapDemoted(news.ApplyAnnotations(ranking, results, ann), results, demoted)
		}
	}
	do := news.DigestOptions{Layer: o.layer, Today: today, Cap: s.Cap(o.layer), Ranking: ranking, MinScore: s.MinScore(), Totals: stats.Totals(), Annotations: annotations}
	// 関心外と判定した記事から日替わりで数件を拾い上げる(意図しない発見のため)。同じ日なら何度作り直しても同じ記事。
	do.Serendipity = news.PickSerendipity(results, ranking, s.MinScore(), s.SerendipityCount(), today)
	reading, readingErr := news.LoadReading(newsDir)
	if readingErr != nil {
		fmt.Fprintln(stderr, "braindex news fetch: 警告: 保存記事の一覧を読めない:", readingErr)
		ingestWarning++
	} else {
		do.Reading = &reading
	}
	digest := news.Digest(results, do)
	openWarning := 0
	var htmlPath string
	if o.stdout {
		if _, err := stdout.Write(digest); err != nil {
			return fail(err)
		}
	} else {
		// md(記録用)と html(選別 UI)を同名で書く。md の名前は上で確かめてある(既にあればここへ来ない)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return fail(err)
		}
		// 選別の保存先を先に作っておく。HTML の保存ダイアログで辿れるようにするため
		// (無いディレクトリはダイアログで選べない。設計レビュー 2026-09-06 H4)
		if err := os.MkdirAll(filepath.Join(newsDir, "inbox"), 0o755); err != nil {
			fmt.Fprintf(stderr, "braindex news fetch: 警告: 選別の保存先を作れない: %v\n", err)
		}
		htmlPath = strings.TrimSuffix(outPath, filepath.Ext(outPath)) + ".html"
		if resuming && len(resume.Outputs) > 1 {
			htmlPath = resume.Outputs[1] // 前回と同じ名前に書き直す(-out が .html のときの連番を増やさない)
		} else if strings.EqualFold(htmlPath, outPath) {
			// -out に .html を渡された場合。同じ名前に書くと md を消してしまうので、md と同じ連番の規則で別名にする
			// (Windows は大文字小文字を区別しないので .HTML も同じ扱い)。md はまだ書いていないので名前を予約して避ける
			htmlPath, err = unusedPath(htmlPath, outPath)
			if err != nil {
				return fail(err)
			}
		}
		// 書き始める前に記録を置く。md → html → 既読 の途中で止まっても、次の fetch がこの記録を見て書き直す
		pending.Put(news.PendingRun{Op: "fetch", Started: time.Now().Format(time.RFC3339), Date: today, Layer: o.layer, Outputs: []string{outPath, htmlPath}})
		if err := pending.Save(pendingPath); err != nil {
			return fail(fmt.Errorf("未完了の記録を書けない: %w", err))
		}
		if resuming {
			fmt.Fprintf(stdout, "前回の news fetch(%s 開始)は途中で止まっていた: %s を書き直して既読まで進める\n", resume.Started, outPath)
		}
		if err := fsutil.WriteAtomic(outPath, digest, 0o644); err != nil {
			return fail(err)
		}
		if readingErr == nil {
			if err := news.WriteReading(newsDir, reading); err != nil {
				fmt.Fprintln(stderr, "braindex news fetch: 警告: 保存記事の一覧を表示できない:", err)
				ingestWarning++
			} else if rel, err := filepath.Rel(filepath.Dir(htmlPath), filepath.Join(newsDir, news.ReadingHTML)); err == nil {
				do.LibraryHref = (&url.URL{Path: filepath.ToSlash(rel)}).String()
			}
		}
		if err := fsutil.WriteAtomic(htmlPath, news.RenderHTML(results, do), 0o644); err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "news ダイジェスト: %s\n", outPath)
		fmt.Fprintf(stdout, "news 選別 UI: %s\n", htmlPath)
	}

	// 既読はダイジェストを書けた後に更新する(書けなかった新着が既読になって消えないように)
	if !o.replay {
		pruned, err := seen.Prune(today, s.SeenDays)
		if err != nil {
			return fail(err)
		}
		if err := saveSeen(pruned, seenPath); err != nil {
			return fail(err)
		}
	}

	if !o.stdout {
		// 既読まで書けた = 完了。記録を消す(消せなくても保存物は揃っているので、次回が今日の分を書き直すだけ。警告にとどめる)
		pending.Remove(outPath)
		if err := pending.Save(pendingPath); err != nil {
			fmt.Fprintf(stderr, "braindex news fetch: 警告: 未完了の記録を消せない(次回は今日の分を書き直してから進む): %v\n", err)
			pendingWarning++
		}
		// ブラウザは保存物を全部書いた後に開く(開くのに手間取っても、ここで止められても、食い違いは残らない)
		if !o.noOpen {
			if err := openInBrowser(htmlPath); err != nil {
				fmt.Fprintln(stderr, "braindex news fetch: 警告:", err)
				openWarning = 1
			}
		}
	}

	if n := len(news.Failed(results)) + profileWarnings + openWarning + ingestWarning + llmWarnings + lockWarning + pendingWarning; n > 0 {
		fmt.Fprintf(stderr, "braindex news fetch: 警告 %d 件(取得失敗 %d 本・終了コード 2)\n", n, len(news.Failed(results)))
		return 2
	}
	return 0
}

// resumeCommand は未完了の記録 r を書き直して完了させるコマンド。出力先が既定の名前(digest_<日付>_<層>.md)でなければ -out も付ける。
func resumeCommand(r news.PendingRun) string {
	cmd := fmt.Sprintf("braindex news fetch -date %s -layer %s", r.Date, r.Layer)
	if out := r.Primary(); filepath.Base(out) != fmt.Sprintf("digest_%s_%s.md", r.Date, r.Layer) {
		cmd += " -out " + out
	}
	return cmd
}

// unusedPath は path が無ければそのまま、あれば拡張子の前に -2, -3 … を付けた未使用の名前を返す。
// reserved はこれから書く名前(まだ無いが使えない)。大文字小文字は区別しない(Windows に合わせる)。
// 使うのは -out に .html を渡された場合だけ: md と html を同じ名前に書くと md を消してしまう。
// 同じ日の 2 回目そのものは、出力先が既にあれば書かない(決定 2026-09-03 → manual/news.md「決めたこと」)。
func unusedPath(path string, reserved ...string) (string, error) {
	ext := filepath.Ext(path)
	base := path[:len(path)-len(ext)]
	for i := 1; i < 1000; i++ {
		p := path
		if i > 1 {
			p = fmt.Sprintf("%s-%d%s", base, i, ext)
		}
		if isReserved(p, reserved) {
			continue
		}
		if _, err := os.Lstat(p); errors.Is(err, iofs.ErrNotExist) {
			return p, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("空いている名前が無い: %s", path)
}

func isReserved(p string, reserved []string) bool {
	for _, r := range reserved {
		if strings.EqualFold(p, r) {
			return true
		}
	}
	return false
}

// annotateWithLLM は claude CLI で新着に翻訳と関心度を付け、キャッシュ(news/.llm_cache.json)に合流させて返す。
// 返す警告は 呼び出し側の不在(CLI 無し)・バッチの失敗・キャッシュの保存失敗。キャッシュが壊れているときだけ error(消せば直る旨を伝える)。
// CLI が無いときは新しく聞かないが、読み込んだキャッシュは返す(前回までの訳と点は効かせる)。
func annotateWithLLM(s news.Settings, newsDir string, results []news.Result, terms []string, progress io.Writer) (news.Annotations, []string, error) {
	cachePath := filepath.Join(newsDir, news.LLMCacheFile)
	cache, err := news.LoadAnnotations(cachePath)
	if err != nil {
		return nil, nil, err
	}
	a, err := newNewsAnnotator(s)
	if err != nil {
		return cache, []string{fmt.Sprintf("%v(キャッシュ済みの分と語の一致の点で続ける。設定 news.llm を off にすれば出なくなる)", err)}, nil
	}
	keeps, err := readKeeps(newsDir)
	if err != nil {
		return nil, nil, err
	}
	examples := make([]string, 0, len(keeps))
	for _, k := range keeps {
		examples = append(examples, k.Title)
	}
	rep := news.Annotate(context.Background(), a, results, cache, news.AnnotateOptions{
		Terms:    terms,
		Examples: examples,
	})
	var ws []string
	if rep.Failed > 0 {
		ws = append(ws, fmt.Sprintf("%d バッチ失敗(その分は語の一致の点のまま): %s", rep.Failed, strings.Join(rep.Errors, " / ")))
	}
	if rep.Requested > 0 {
		retried := ""
		if rep.Retried > 0 {
			retried = fmt.Sprintf("・訳が返らず %d 件を聞き直し", rep.Retried)
		}
		fmt.Fprintf(progress, "LLM 補助: %d 件を聞いて %d 件に注釈%s(キャッシュ合計 %d 件)\n", rep.Requested, rep.Annotated, retried, len(cache))
	}
	if err := cache.Save(cachePath); err != nil {
		ws = append(ws, fmt.Sprintf("キャッシュを書けない(次回も同じ記事を聞く): %v", err))
	}
	return cache, ws, nil
}
