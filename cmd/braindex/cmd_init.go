package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/pilefort/braindex/internal/template"
)

func init() {
	register(&command{
		name:    "init",
		summary: "hub の骨格を展開する(既定は索引の設定だけ。-add <機能> で規約・review・retro・news・schedule を足す。-repo は各リポの骨格)。既存ファイルは上書きしない",
		run:     runInit,
	})
}

// runInit は braindex init [-add 機能,...] [-list] [-repo] [dir] を実行する。dir 省略時はカレントディレクトリ。
//
// 既定は段 0(README・CLAUDE.md・.gitattributes・braindex.json の索引の設定だけ)。機能は -add で 1 つずつ足す
// (決定 2026-09-05「init は段 0 だけ配り、機能は init -add <機能> で足す」)。-add all は従来の一括展開。
// -repo は hub でなく各プロジェクトのリポ側の骨格(docs/notes・docs/decisions.md・work/)を置く。
// 既存ファイルは残すので再実行しても安全。braindex.json と .gitignore は既にあっても無い節・行だけ足す。
func runInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("braindex init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.Bool("repo", false, "hub でなく各プロジェクトのリポ側の骨格(docs/notes/{common,project}・docs/decisions.md・work/)を置く")
	add := fs.String("add", "", "足す機能(カンマ区切り)。conventions / review / retro / news / schedule / all。依存は自動で足す")
	list := fs.Bool("list", false, "機能と配布物の一覧を出して終わる")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex init [-add 機能,...] [-list] [-repo] [dir]")
		fmt.Fprintln(stderr, "  dir(既定: カレントディレクトリ)に hub の骨格を展開する。既定は段 0: README・CLAUDE.md・.gitattributes・")
		fmt.Fprintln(stderr, "  braindex.json(索引の設定だけ)。規約や週次レビューなどの機能は -add で足す(例: -add conventions,review)。")
		fmt.Fprintln(stderr, "  -add all で全部(従来の init と同じ)。一覧は -list。既存ファイルは残すので、再実行しても安全。")
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
	// 引数の検証は -list より先(誤った呼び方を一覧の表示で隠さない)
	dir := "."
	switch fs.NArg() {
	case 0:
	case 1:
		dir = fs.Arg(0)
	default:
		fmt.Fprintf(stderr, "braindex init: ディレクトリは 1 つまで(%d 個指定された)\n", fs.NArg())
		return 1
	}
	if *repo && *add != "" {
		fmt.Fprintln(stderr, "braindex init: -repo と -add は併用できない(機能は hub にだけ足す)")
		return 1
	}
	if *list {
		printFeatureList(stdout)
		return 0
	}

	var (
		res   template.Result
		err   error
		feats []template.Feature
		added []template.Feature
	)
	switch {
	case *repo:
		res, err = template.Install(dir, template.KindRepo)
	default:
		var req []template.Feature
		if *add != "" {
			if req, err = template.ParseFeatures(*add); err != nil {
				fmt.Fprintln(stderr, "braindex init:", err)
				return 1
			}
		}
		feats, added = template.Resolve(req)
		res, err = template.InstallFeatures(dir, feats)
	}
	// 途中で失敗しても、そこまでに作った／足した／残したものは列挙する(書いたものを無言にしない)
	for _, p := range res.Created {
		fmt.Fprintln(stdout, "作成:", p)
	}
	for _, p := range res.Merged {
		fmt.Fprintln(stdout, "追記(無い節・行を足した):", p)
	}
	for _, p := range res.Skipped {
		fmt.Fprintln(stdout, "保持(既存):", p)
	}
	if err != nil {
		fmt.Fprintln(stderr, "braindex init:", err)
		return 1
	}
	// 「足した」でなく「含めた」: 依存の配布物が既にある hub(再実行・先に conventions を入れた hub)では何も足さない
	if len(added) > 0 {
		fmt.Fprintf(stdout, "依存として含めた機能: %s\n", joinFeatures(added))
	}
	fmt.Fprintf(stdout, "braindex init: 作成 %d・追記 %d・保持 %d(%s)\n", len(res.Created), len(res.Merged), len(res.Skipped), dir)
	if *repo || len(res.Created)+len(res.Merged) == 0 {
		return 0
	}
	printNextSteps(stdout, feats)
	return 0
}

// printNextSteps は展開した機能に応じた「次」の案内を出す。
func printNextSteps(w io.Writer, feats []template.Feature) {
	has := map[template.Feature]bool{}
	for _, f := range feats {
		has[f] = true
	}
	fmt.Fprintln(w, "次: braindex.json の root を確認し(\"..\" は各リポの親ディレクトリ)、`braindex` を実行して index/catalog.md を作る")
	if has[template.FeatureConventions] {
		fmt.Fprintln(w, "  規約は docs/conventions.md。各リポの骨格は `braindex init -repo <リポ>`。記録の点検は `braindex lint`")
	}
	if has[template.FeatureReview] {
		fmt.Fprintln(w, "  週次レビューは `braindex review`(材料は work/review/ に置く)")
	}
	if has[template.FeatureRetro] {
		fmt.Fprintln(w, "  訂正率の確認は `braindex retro check`(閾値は braindex.json の retro 節)")
	}
	if has[template.FeatureNews] {
		fmt.Fprintln(w, "  取材先は news/feeds.example.json を元に news/feeds.json を書く。その後 `braindex news`")
	}
	if has[template.FeatureSchedule] {
		fmt.Fprintln(w, "  定期実行は `braindex schedule print` で中身を見てから `braindex schedule install`")
	}
	if has[template.FeatureConventions] || has[template.FeatureReview] || has[template.FeatureRetro] {
		fmt.Fprintln(w, "  日本語の推敲スキル(例: ja-tensaku)は同梱しない。要れば自分の .claude/skills/ に置く")
	}
	if len(feats) <= 1 { // core だけ
		fmt.Fprintln(w, "  機能を足すときは `braindex init -list` で一覧を見て、`braindex init -add conventions` から")
	}
}

// printFeatureList は -list の出力。機能ごとに 1 行の説明と、配るファイル・設定の節・依存。
func printFeatureList(w io.Writer) {
	fmt.Fprintln(w, "braindex init -add <機能> で足せる機能(段の順):")
	for _, f := range template.FeatureList() {
		summary, files, sections, deps, _ := template.FeatureInfo(f)
		fmt.Fprintf(w, "\n%s: %s\n", f, summary)
		if len(files) > 0 {
			fmt.Fprintf(w, "  ファイル: %s\n", strings.Join(files, ", "))
		}
		if len(sections) > 0 {
			fmt.Fprintf(w, "  設定の節: %s\n", strings.Join(sections, ", "))
		}
		if len(deps) > 0 {
			fmt.Fprintf(w, "  依存: %s\n", joinFeatures(deps))
		}
	}
	fmt.Fprintf(w, "\n%s: 上の全部(従来の braindex init と同じ配布物)\n", template.FeatureAll)
}

func joinFeatures(fs []template.Feature) string {
	var s []string
	for _, f := range fs {
		s = append(s, string(f))
	}
	return strings.Join(s, ", ")
}
