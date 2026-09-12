package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pilefort/braindex/internal/config"
	"github.com/pilefort/braindex/internal/interest"
	"github.com/pilefort/braindex/internal/learn"
	"github.com/pilefort/braindex/internal/retro"
)

func init() {
	register(&command{
		name:    "learn",
		summary: "学習の提案。関心プロファイルと訂正の文脈から「いま学ぶと良さそうなこと」の候補を理由つきで出す(手元の材料だけ・LLM なし)。learn answer で候補に既知・不要・後でと回答すると次回から伏せる",
		run:     runLearn,
	})
}

// learnAnswersPath は候補への回答の置き場(hub 相対)。
// work/ は揮発側(上書きされる・索引に載らない)で、週次レビューの記録 work/review/ と同じ扱い。
// 回答は本人の入力で再生成できないので、news の既読のような .gitignore の作業ファイルにはしない(決定 2026-09-06 → manual/learn.md「決めたこと」)。
const learnAnswersPath = "work/learn/answers.json"

type learnOptions struct {
	config      string // -config。hub の位置を兼ねるので必須
	date        string // -date。今日の固定(既定: 実行日)
	days        int    // -days。直近の日数(既定: 設定 news.profile_days → 14)
	sessions    string // -sessions。セッションログの置き場(既定: news.sessions_dir → retro.sessions_dir → ~/.claude/projects)
	allProjects bool   // -all-projects。root の外のセッションも数える
	top         int    // -top。各節の件数(既定 10。0 で全件)
	json        bool   // -json。JSON で出す
}

// addLearnInputFlags は材料を決めるフラグ(learn と learn answer で同じ。回答の対象を提示と同じ候補の集合にするため)。
func addLearnInputFlags(fs *flag.FlagSet, o *learnOptions) {
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json。そのディレクトリを hub とみなす)")
	fs.StringVar(&o.date, "date", "", "今日として使う日付 YYYY-MM-DD(既定: 実行日)。窓の基準")
	fs.IntVar(&o.days, "days", 0, "直近何日の索引とセッションを見るか(既定: 設定 news.profile_days → 14)")
	fs.StringVar(&o.sessions, "sessions", "", "セッションログの置き場(既定: 設定 news.sessions_dir → retro.sessions_dir → ~/.claude/projects)")
	fs.BoolVar(&o.allProjects, "all-projects", false, "root の外で交わしたセッションも数える(既定: root 配下だけ。設定 retro.all_projects と同じ)")
}

// runLearn は braindex learn を振り分ける。answer / answers はサブコマンド、それ以外は候補の提示。
func runLearn(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "answer":
			return runLearnAnswer(args[1:], stdout, stderr)
		case "answers":
			return runLearnAnswers(args[1:], stdout, stderr)
		}
	}
	return runLearnShow(args, stdout, stderr)
}

// runLearnShow は braindex learn(候補の提示)を実行する。
//
// 材料は braindex news profile と同じ(索引・セッション・keep・補助。窓も news.profile_days を共有)。
// 訂正の判定は retro と同じ辞書(設定 retro.dictionary があればそれ、無ければ既定)。
// 候補を出したあと、回答(work/learn/answers.json)で伏せ、件数を切り、「索引に無い」語をノート本文で照合する
// (braindex search と同じ走査規則・同じ検索処理)。
// 終了コード: 0 成功 / 1 失敗 / 2 警告つき(索引やセッションの置き場が無い・本文を読めなかった範囲がある・回答ファイルが壊れているなど)。
func runLearnShow(args []string, stdout, stderr io.Writer) int {
	var o learnOptions
	fs := flag.NewFlagSet("braindex learn", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addLearnInputFlags(fs, &o)
	fs.IntVar(&o.top, "top", 10, "各節に出す件数(0 で全件)")
	fs.BoolVar(&o.json, "json", false, "Markdown でなく JSON で出す")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex learn [-config braindex.json] [-date YYYY-MM-DD] [-days N] [-sessions DIR] [-top N] [-json]")
		fmt.Fprintln(stderr, "        braindex learn answer <known|unwanted|later|clear> <語>...   候補に回答する(次回から伏せる)")
		fmt.Fprintln(stderr, "        braindex learn answers [-json]                                回答の一覧")
		fmt.Fprintln(stderr, "  「いま学ぶと良さそうなこと」の候補を 3 つの節で出す。材料は手元だけ(索引・セッションログ・news/keep・ノート本文)で、外には何も送らない。")
		fmt.Fprintln(stderr, "    触れているが索引に無い         … セッションに繰り返し出るのに索引にも keep にも無い語(既定: 3 セッション以上)")
		fmt.Fprintln(stderr, "    訂正の文脈に繰り返し出る       … 訂正の発話とその直前の発話に出る語(既定: 2 発話以上。辞書は retro と同じ)")
		fmt.Fprintln(stderr, "    残した記事にあるが索引に無い   … keep の見出しにあるのに索引に無い語")
		fmt.Fprintln(stderr, "  「索引に無い」語は索引と同じ走査規則でノート本文を照合し、本文で発見(パス:行)・本文でも未発見・確認不能(読めなかった範囲がある)を分けて出す。")
		fmt.Fprintln(stderr, "  回答済み(既知・不要・期限前の後で)の候補は伏せ、伏せた数だけを出す。回答は work/learn/answers.json に節と語の組で持つ。")
		fmt.Fprintln(stderr, "  出力は語と件数と出典の位置だけ(発話やノートの本文は載せない)。窓と材料は braindex news profile と同じ。")
		fmt.Fprintln(stderr, "  終了コード: 0 成功 / 1 失敗 / 2 警告つき(索引やセッションの置き場が無い・本文を読めなかった範囲がある・回答ファイルが読めない)")
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
		fmt.Fprintf(stderr, "braindex learn: 引数 %q は受け付けない(フラグだけを渡す。回答は braindex learn answer)\n", fs.Args())
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

	h, err := resolveLearnHub(o)
	if err != nil {
		return fail(err)
	}
	r, warnings, err := buildLearn(h, o)
	if err != nil {
		return fail(err)
	}
	// 回答で伏せる。回答ファイルが読めない(壊れている)ときは反映せずに出して警告にする——候補は出せるので止めない。
	// 伏せてから件数を切る(先に切ると伏せた分だけ欠ける)
	if fb, ferr := learn.LoadFeedbacks(h.answersPath()); ferr != nil {
		warnings = append(warnings, "回答を反映せずに出す: "+ferr.Error())
	} else {
		learn.Apply(&r, fb, h.today)
	}
	r.Truncate(o.top)
	// 「索引に無い」候補を本文で照合する。走査設定の解決は索引生成と同じ(設定ファイルの root)。
	// root が無い・走査できない hub では照合を飛ばして警告にし、候補は索引だけの判定のまま出す(落とさない)。
	if cfg, _, _, cerr := resolve(options{config: h.cfgPath}); cerr != nil {
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
	return learnWarnings("braindex learn", warnings, stderr)
}

// learnHubInfo は learn の各サブコマンドが共有する hub の位置と今日。
type learnHubInfo struct {
	fc      config.Config
	cfgPath string
	hubDir  string
	today   string
}

func (h learnHubInfo) answersPath() string {
	return filepath.Join(h.hubDir, filepath.FromSlash(learnAnswersPath))
}

// learnHub は設定を読み、hub の位置と今日を決める。
func resolveLearnHub(o learnOptions) (learnHubInfo, error) {
	cfgPath := o.config
	if cfgPath == "" {
		cfgPath = defaultConfig
	}
	fc, found, err := config.Load(cfgPath)
	if err != nil {
		return learnHubInfo{}, err
	}
	if !found {
		return learnHubInfo{}, fmt.Errorf("設定ファイルが無い: %s(hub のルートで実行するか、-config で指定する)", cfgPath)
	}
	today := o.date
	if today == "" {
		today = time.Now().Format("2006-01-02")
	} else if _, perr := time.Parse("2006-01-02", today); perr != nil {
		return learnHubInfo{}, fmt.Errorf("-date は YYYY-MM-DD で指定する: %q", today)
	}
	return learnHubInfo{fc: fc, cfgPath: cfgPath, hubDir: filepath.Dir(cfgPath), today: today}, nil
}

// buildLearn は材料を読んで候補を全件(Top 0)出す。件数は呼び出し側が回答で伏せてから切る。
func buildLearn(h learnHubInfo, o learnOptions) (learn.Report, []string, error) {
	in, warnings, err := loadProfileInput(h.fc, h.hubDir, h.today, o.days, o.sessions, o.allProjects)
	if err != nil {
		return learn.Report{}, nil, err
	}
	p, err := interest.Build(in)
	if err != nil {
		return learn.Report{}, nil, err
	}
	if err := h.fc.Retro.Validate(); err != nil {
		return learn.Report{}, nil, err
	}
	home, _ := os.UserHomeDir()
	dicts, err := loadRetroDictionaries(h.fc.Retro.WithDefaults(), h.hubDir, home)
	if err != nil {
		return learn.Report{}, nil, err
	}
	// 窓は interest.Build と同じものをそのまま使う。信号 1・3 と 2 が同じ材料を見るように揃える
	// (以前はここで日付を UTC で解き直していて、ローカルとの時差の分だけ窓がずれた。設計レビュー 2026-09-06 M3b)
	r := learn.Build(learn.Input{
		Profile:  p,
		Catalog:  in.Catalog,
		Sessions: in.Sessions,
		Window:   retro.Window{Since: in.Since, Until: in.Until},
		Dicts:    dicts,
		Options:  learn.Options{Top: 0},
	})
	return r, warnings, nil
}

// learnWarnings は警告を stderr に出し、あれば終了コード 2、無ければ 0 を返す。
func learnWarnings(prefix string, warnings []string, stderr io.Writer) int {
	for _, w := range warnings {
		fmt.Fprintln(stderr, prefix+": 警告:", w)
	}
	if len(warnings) > 0 {
		fmt.Fprintf(stderr, "%s: 警告 %d 件(終了コード 2)\n", prefix, len(warnings))
		return 2
	}
	return 0
}

type learnAnswerOptions struct {
	learnOptions        // 材料のフラグ。回答の対象を提示と同じ候補の集合にする
	section      string // -section。節を限る(既定: 語が候補に出ている全部の節)
	until        string // -until。後で の再提示日(既定: 今日から窓の日数後)
}

// runLearnAnswer は braindex learn answer <known|unwanted|later|clear> <語>... を実行する。
//
// 回答は「いま候補に出ている語」にだけ付けられる(綴りの誤りを黙って記録しないため)。候補の集合は提示と同じ材料から
// 全件(-top に関係なく)で作る。節を指定しなければ、その語が出ている全部の節に同じ回答を付ける。
// clear は候補に無い語でも消せる(古い回答の掃除)。全部の語を確かめてから 1 回だけ書く——1 語でも失敗したら何も書かない。
// 終了コード: 0 成功 / 1 失敗(候補に無い・回答ファイルが壊れている。何も書かない) / 2 警告つき(材料の置き場が無いなど。回答は書けている)。
func runLearnAnswer(args []string, stdout, stderr io.Writer) int {
	var o learnAnswerOptions
	fs := flag.NewFlagSet("braindex learn answer", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addLearnInputFlags(fs, &o.learnOptions)
	fs.StringVar(&o.section, "section", "", "節を限る(unsettled / stumbles / read_not_written。既定: 語が候補に出ている全部の節)")
	fs.StringVar(&o.until, "until", "", "later の再提示日 YYYY-MM-DD(既定: 今日から窓の日数(-days・既定 14)後)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex learn answer <known|unwanted|later|clear> [-config braindex.json] [-section 節] [-until YYYY-MM-DD] <語> [<語>...]")
		fmt.Fprintln(stderr, "  候補に回答を記録し、次回の braindex learn から伏せる。回答は work/learn/answers.json に節と語の組で持つ(本文は書かない)。")
		fmt.Fprintln(stderr, "    known     既知(もう学んだ・知っている)。解除するまで伏せる")
		fmt.Fprintln(stderr, "    unwanted  不要(学ぶ対象ではない)。解除するまで伏せる")
		fmt.Fprintln(stderr, "    later     後で。-until の日(既定: 窓の日数後)から再提示する")
		fmt.Fprintln(stderr, "    clear     回答を解除する(候補に無い語でも消せる)")
		fmt.Fprintln(stderr, "  語は出力に出ている綴りで指定する(大文字は畳む)。いま候補に無い語には付けられない(-top で隠れた分は対象)。")
		fmt.Fprintln(stderr, "  同じ語が複数の節に出ていれば全部に付く(-section で限る)。同じ節と語に回答し直すと置き換わる。")
		fmt.Fprintln(stderr, "  終了コード: 0 成功 / 1 失敗(候補に無い・回答ファイルが壊れている。何も書かない) / 2 警告つき(材料の置き場が無いなど)")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "フラグ:")
		fs.PrintDefaults()
	}
	// 動詞はフラグの前でも後でもよい(braindex learn answer known -section unsettled istio / -config x.json known istio)
	verb, rest := "", args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		verb, rest = rest[0], rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	words := fs.Args()
	if verb == "" && len(words) > 0 {
		verb, words = words[0], words[1:]
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "braindex learn answer:", err)
		return 1
	}
	if verb == "" {
		fmt.Fprintln(stderr, "braindex learn answer: 回答(known / unwanted / later / clear)と語を指定する")
		fs.Usage()
		return 1
	}
	clear := verb == "clear" || verb == "解除"
	var answer learn.Answer
	if !clear {
		a, err := learn.ParseAnswer(verb)
		if err != nil {
			fail(err)
			fs.Usage()
			return 1
		}
		answer = a
	}
	if len(words) == 0 {
		return fail(errors.New("語を 1 つ以上指定する(出力に出ている綴りで)"))
	}
	terms, err := learnTerms(words)
	if err != nil {
		return fail(err)
	}
	var section learn.Section
	if o.section != "" {
		if section, err = learn.ParseSection(o.section); err != nil {
			return fail(err)
		}
	}
	if o.until != "" {
		if answer != learn.Later {
			return fail(errors.New("-until は later だけに付ける"))
		}
		if _, perr := time.Parse("2006-01-02", o.until); perr != nil {
			return fail(fmt.Errorf("-until は YYYY-MM-DD で指定する: %q", o.until))
		}
	}

	h, err := resolveLearnHub(o.learnOptions)
	if err != nil {
		return fail(err)
	}
	path := h.answersPath()
	// 壊れた回答ファイルの上に書かない(本人の回答を失う)。候補の走査に入る前に確かめる
	if _, err := loadAnswersForWrite(path); err != nil {
		return fail(err)
	}
	targets := learn.Sections()
	if section != "" {
		targets = []learn.Section{section}
	}
	var lines, warnings []string
	// mutate は回答ファイルへの書き換えだけを行う(材料は読まない)。書き込み直前に読み直して
	// やり直すことがあるので、何度呼ばれても同じ結果になるようにする
	var mutate func(fb *learn.Feedbacks) error
	if clear {
		mutate = func(fb *learn.Feedbacks) error {
			lines = nil
			for _, w := range terms {
				n := 0
				for _, sec := range targets {
					if fb.Clear(sec, w) {
						n++
						lines = append(lines, fmt.Sprintf("解除: %s: %s", sec.Title(), w))
					}
				}
				if n == 0 {
					return fmt.Errorf("回答が無い: %s(回答の一覧は braindex learn answers)", w)
				}
			}
			return nil
		}
	} else {
		r, ws, err := buildLearn(h, o.learnOptions)
		if err != nil {
			return fail(err)
		}
		warnings = ws
		present := map[learn.Section]map[string]bool{}
		for _, c := range r.Candidates() {
			if present[c.Section] == nil {
				present[c.Section] = map[string]bool{}
			}
			present[c.Section][c.Item.Word] = true
		}
		until := ""
		if answer == learn.Later {
			until = o.until
			if until == "" {
				d, _ := time.Parse("2006-01-02", h.today)
				until = d.AddDate(0, 0, r.Days).Format("2006-01-02")
			}
			if until <= h.today {
				return fail(fmt.Errorf("-until は今日(%s)より後の日にする: %s", h.today, until))
			}
		}
		// 候補に当たるかは回答ファイルと関係なく決まるので、書き換えの前に確かめる
		hit := map[string][]learn.Section{}
		for _, w := range terms {
			for _, sec := range targets {
				if present[sec][w] {
					hit[w] = append(hit[w], sec)
				}
			}
			if len(hit[w]) == 0 {
				where := ""
				if section != "" {
					where = section.Title() + ": "
				}
				return fail(fmt.Errorf("候補に無い: %s%s(いまの候補は braindex learn -top 0 で見る。何も書いていない)", where, w))
			}
		}
		mutate = func(fb *learn.Feedbacks) error {
			lines = nil
			for _, w := range terms {
				for _, sec := range hit[w] {
					f := learn.Feedback{Section: sec, Word: w, Answer: answer, Date: h.today, Until: until}
					fb.Set(f)
					lines = append(lines, fmt.Sprintf("回答: %s: %s — %s", sec.Title(), w, f.Describe(h.today)))
				}
			}
			return nil
		}
	}
	if err := updateAnswers(path, mutate); err != nil {
		return fail(err)
	}
	for _, l := range lines {
		fmt.Fprintln(stdout, l)
	}
	fmt.Fprintf(stdout, "保存先: %s\n", path)
	return learnWarnings("braindex learn answer", warnings, stderr)
}

// loadAnswersForWrite は書き換えるために回答ファイルを読む。壊れていたら上に書かずに失敗する
// (全件置換なので、壊れたファイルを空と見なして書くと本人の回答を全部失う)。直すか消すかは本人が決める。
func loadAnswersForWrite(path string) (learn.Feedbacks, error) {
	fb, err := learn.LoadFeedbacks(path)
	if err != nil {
		return learn.Feedbacks{}, fmt.Errorf("%w(直すか、ファイルごと消してから回答する)", err)
	}
	return fb, nil
}

// updateAnswers は path の回答を読み、mutate で書き換えて保存する。
//
// 保存は全件置換(fsutil.WriteAtomic)なので、読んでから書くまでの間に別のプロセスが
// 別の語へ回答していると、そのまま書けば後勝ちで相手の回答が消える。読んだ時点の中身と
// 書く直前の中身を比べ、変わっていたら読み直した方へ mutate をやり直してから書く。
// 読み直しから置き換えまでの隙間は残るが、候補の走査と人が打つ時間ぶんの窓は無くなる。
func updateAnswers(path string, mutate func(*learn.Feedbacks) error) error {
	fb, err := loadAnswersForWrite(path)
	if err != nil {
		return err
	}
	before := string(fb.JSON()) // JSON() は同じ内容なら常に同じバイト列なので、そのまま突き合わせに使える
	if err := mutate(&fb); err != nil {
		return err
	}
	cur, err := loadAnswersForWrite(path)
	if err != nil {
		return err
	}
	if string(cur.JSON()) != before {
		if err := mutate(&cur); err != nil {
			return err
		}
		fb = cur
	}
	return fb.Save(path)
}

// learnTerms は打たれた語を候補と同じ規則(interest.Words)で 1 語にする。候補の語はラテン文字を小文字に畳んであるので、
// Kubernetes と打っても kubernetes に当たる。同じ語は 1 回にする。
func learnTerms(args []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, a := range args {
		ws := interest.Words(a)
		if len(ws) != 1 {
			return nil, fmt.Errorf("%q は候補の語として扱えない(出力に出ている 1 語をそのまま指定する)", a)
		}
		if !seen[ws[0]] {
			seen[ws[0]] = true
			out = append(out, ws[0])
		}
	}
	return out, nil
}

// runLearnAnswers は braindex learn answers(回答の一覧)を実行する。材料は読まない(回答ファイルだけ)。
func runLearnAnswers(args []string, stdout, stderr io.Writer) int {
	var o learnOptions
	fs := flag.NewFlagSet("braindex learn answers", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json。そのディレクトリを hub とみなす)")
	fs.StringVar(&o.date, "date", "", "今日として使う日付 YYYY-MM-DD(既定: 実行日)。後で の期限切れの判定に使う")
	fs.BoolVar(&o.json, "json", false, "Markdown でなく JSON(回答ファイルと同じ形)で出す")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex learn answers [-config braindex.json] [-date YYYY-MM-DD] [-json]")
		fmt.Fprintln(stderr, "  work/learn/answers.json の回答を節ごとに一覧する。解除は braindex learn answer clear <語>。")
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
		fmt.Fprintln(stderr, "braindex learn answers:", err)
		return 1
	}
	if fs.NArg() > 0 {
		return fail(fmt.Errorf("引数 %q は受け付けない(フラグだけを渡す)", fs.Args()))
	}
	h, err := resolveLearnHub(o)
	if err != nil {
		return fail(err)
	}
	fb, err := learn.LoadFeedbacks(h.answersPath())
	if err != nil {
		return fail(err)
	}
	out := fb.Markdown(h.today)
	if o.json {
		out = fb.JSON()
	}
	if _, err := stdout.Write(out); err != nil {
		return fail(err)
	}
	return 0
}
