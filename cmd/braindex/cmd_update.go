package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pilefort/braindex/internal/template"
)

func init() {
	register(&command{
		name:    "update",
		summary: "hub を今の braindex に追いつかせる(雛形の追従＋索引の再生成。-repo は雛形だけ)。編集したファイルは上書きせず .new を隣に置く",
		run:     runUpdate,
	})
}

// runUpdate は braindex update [-repo] [-dry-run] [-force] [dir] を実行する。
//
// init が「まだ無いものを足す」のに対し、update は「既にあるものを今の版にする」。CLI に機能を足しても、
// 既に立ち上がっている hub には雛形・スキル・設定の改良が届かないため(init は既存ファイルを上書きしない)。
//
// 判定は台帳(.braindex/template.json)のハッシュで行う。配った版のままなら黙って今の版にし、利用者が
// 編集していれば現物を残して隣に .new を置く。台帳が無い hub は、既存ファイルを全部「編集済み」として扱う。
// 追従するのは台帳に記録された機能(init -add で足したもの)の分だけ。記録の無い hub は存在するファイルから
// 機能を推定し、その旨を 1 行出す(決定 2026-09-05 → manual/init-update.md「決めたこと」)。
//
// 終了コード: 0 要対応なし / 1 失敗 / 2 要対応あり(.new を置いた・索引生成が警告を出した)。
func runUpdate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("braindex update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.Bool("repo", false, "hub でなく各プロジェクトのリポ側の骨格を追従する(索引は再生成しない)")
	dry := fs.Bool("dry-run", false, "何も書かず、何が変わるかだけを出す")
	force := fs.Bool("force", false, "利用者が編集したファイルも今の版で上書きする(.new を置かない)。ただし braindex.json と .gitignore は節・行を足すだけ")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex update [-repo] [-dry-run] [-force] [dir]")
		fmt.Fprintln(stderr, "  dir(既定: カレントディレクトリ)の雛形由来ファイルを、今の braindex の版に追いつかせ、")
		fmt.Fprintln(stderr, "  続けて索引を再生成する。利用者が編集したファイルは上書きせず、隣に .new を置く。")
		fmt.Fprintln(stderr, "  -force でも braindex.json と .gitignore は上書きせず、無い節・行を足すだけ(root や利用者が足した行を消さない。決定 2026-09-05 → manual/init-update.md「決めたこと」)。")
		fmt.Fprintln(stderr, "  終了コード: 0 要対応なし / 1 失敗 / 2 要対応あり(.new を置いた・索引生成が警告)")
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
	dir := "."
	switch fs.NArg() {
	case 0:
	case 1:
		dir = fs.Arg(0)
	default:
		fmt.Fprintf(stderr, "braindex update: ディレクトリは 1 つまで(%d 個指定された)\n", fs.NArg())
		return 1
	}

	// 無いディレクトリは作らない。update は「既にあるものを今の版にする」担当で、
	// 何も無いところに骨格を置くのは init の担当。打ち間違えた行き先に hub が丸ごとできると気づきにくい。
	if fi, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) { // ローカルの fs(FlagSet)が io/fs を隠すので os 側の同じ番兵を使う
			fmt.Fprintf(stderr, "braindex update: %s が無い。新しく作るなら braindex init\n", dir)
			return 1
		}
		fmt.Fprintln(stderr, "braindex update:", err)
		return 1
	} else if !fi.IsDir() {
		fmt.Fprintf(stderr, "braindex update: %s はディレクトリでない\n", dir)
		return 1
	}

	kind := template.KindHub
	if *repo {
		kind = template.KindRepo
	}
	res, err := template.Update(dir, kind, template.UpdateOptions{Force: *force, DryRun: *dry})
	// 途中で失敗しても、そこまでの結果は列挙する(書いたものを無言にしない)
	for _, p := range res.Created {
		fmt.Fprintln(stdout, "作成:", p)
	}
	for _, p := range res.Updated {
		fmt.Fprintln(stdout, "更新:", p)
	}
	for _, p := range res.Merged {
		fmt.Fprintln(stdout, "追記(無い節・行を足した):", p)
	}
	for _, p := range res.HomeCreated {
		fmt.Fprintln(stdout, "作成(ホーム):", template.HomeDisplayPath(p))
	}
	for _, p := range res.HomeUpdated {
		fmt.Fprintln(stdout, "更新(ホーム):", template.HomeDisplayPath(p))
	}
	for _, p := range res.HomeMerged {
		fmt.Fprintln(stdout, "追記(ホーム):", template.HomeDisplayPath(p))
	}
	for _, p := range res.HomeSkipped {
		fmt.Fprintln(stdout, "そのまま(ホーム):", template.HomeDisplayPath(p))
	}
	for _, c := range res.HomeConflicts {
		fmt.Fprintf(stdout, "保持(編集済み): %s → %s に今の版を置いた\n", template.HomeDisplayPath(c.Path), template.HomeDisplayPath(c.New))
	}
	if len(res.Unknown) > 0 {
		fmt.Fprintf(stderr, "braindex update: 警告: 台帳 %s に今の版が知らない機能 %s がある(新しい版の braindex が足したもの)。"+
			"その機能は追従していない。先に `go install` で braindex を更新すること\n", template.LedgerPath, strings.Join(res.Unknown, ", "))
	}
	for _, c := range res.Conflicts {
		label := "保持(編集済み)"
		if slices.Contains(res.Merged, c.Path) {
			label = "保持(編集済み・無い節は足した)" // 節を足したうえで .new も置く(節の中の新しいキーは足さないため)
		}
		note := ""
		if len(c.Missing) > 0 {
			note = "(雛形にあって無いキー: " + strings.Join(c.Missing, ", ") + ")"
		}
		fmt.Fprintf(stdout, "%s: %s → %s に今の版を置いた%s\n", label, c.Path, c.New, note)
	}
	if err != nil {
		fmt.Fprintln(stderr, "braindex update:", err)
		return 1
	}
	if kind == template.KindHub {
		if res.AgentsInferred {
			fmt.Fprintln(stdout, "対応先: 台帳に記録が無いので claude だけと推定")
		}
		names := template.FeatureNames(res.Features)
		label := "台帳の記録"
		if res.Inferred {
			label = "台帳に記録が無いので、存在するファイルと設定の節から推定"
		}
		if len(names) == 0 {
			fmt.Fprintf(stdout, "追従した機能: core だけ(%s)。機能を足すなら braindex init -add\n", label)
		} else {
			fmt.Fprintf(stdout, "追従した機能: %s(%s)\n", strings.Join(names, ", "), label)
		}
	}
	homeCount := len(res.HomeCreated) + len(res.HomeUpdated) + len(res.HomeMerged) + len(res.HomeSkipped) + len(res.HomeConflicts)
	fmt.Fprintf(stdout, "braindex update: 作成 %d・更新 %d・追記 %d・そのまま %d・編集済み %d・ホーム %d 件(%s)\n",
		len(res.Created), len(res.Updated), len(res.Merged), len(res.Unchanged), len(res.Conflicts), homeCount, dir)
	if *dry {
		fmt.Fprintln(stdout, "  -dry-run のため何も書いていない")
	}
	if len(res.Conflicts)+len(res.HomeConflicts) > 0 {
		fmt.Fprintln(stdout, "  .new は今の版。中身を見て、要るところだけ自分のファイルに取り込む(取り込んだら .new は消してよい)")
	}

	// 設定が変わるときの版差の注意(決定 2026-09-04「未知キーはエラーのまま据え置き、update が警告する」 → manual/init-update.md「決めたこと」)。
	// 新しい節の入った braindex.json を古い版の braindex で読むと、未知キーのエラーで全コマンドが止まる。
	if touchesConfig(res) {
		fmt.Fprintln(stderr, "braindex update: 警告: braindex.json が変わる。新しい節を取り込むと、"+
			"古い版の braindex は設定を読めず(未知のキーはエラー)索引生成を含む全コマンドが止まる。"+
			"他のマシンの braindex も `go install` で先に更新すること")
	}

	code := 0
	if len(res.Conflicts)+len(res.HomeConflicts) > 0 {
		code = 2
	}
	if kind == template.KindRepo || *dry {
		return code // 各リポに索引は無い。-dry-run では何も書かない
	}
	switch run(options{config: filepath.Join(dir, defaultConfig)}, stdout, stderr) {
	case 1:
		return 1
	case 2:
		code = 2
	}
	return code
}

// touchesConfig は、この更新で braindex.json が作られる／変わる／.new が置かれるかを返す。
func touchesConfig(res template.UpdateResult) bool {
	const cfg = "braindex.json"
	for _, p := range res.Created {
		if p == cfg {
			return true
		}
	}
	for _, p := range res.Updated {
		if p == cfg {
			return true
		}
	}
	for _, p := range res.Merged {
		if p == cfg {
			return true
		}
	}
	for _, c := range res.Conflicts {
		if c.Path == cfg {
			return true
		}
	}
	return false
}
