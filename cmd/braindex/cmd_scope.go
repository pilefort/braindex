package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pilefort/braindex/internal/notetype"
	"github.com/pilefort/braindex/internal/scan"
	"github.com/pilefort/braindex/internal/scope"
)

func init() {
	register(&command{
		name:    "scope",
		summary: "横断の矛盾検査の走査対象を索引から列挙・絞り込み・chunk 分割する(判定はしない)",
		run:     runScope,
	})
}

// runScope は braindex scope (-topic 語 [-repo 名] | -repo 名 | -dir パス | -full) [-size N] [-json] を実行する。
// 索引(既定: 設定ファイルと同じディレクトリの index/catalog.md。-catalog で上書き)を読み、走査対象を chunk に分けて出す。
// 終了コード: 0 / 1 失敗(フラグ・索引の誤り) / 2 対象が 2 件未満(突き合わせられない)。
func runScope(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("braindex scope", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o options
	var catalog, topic, repo, dir, noteType string
	var full, asJSON bool
	var size int
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json)。索引の場所の取得に使う")
	fs.StringVar(&o.root, "root", "", "内容の種別を読むルート（設定の root より優先）")
	fs.StringVar(&noteType, "type", "", "内容の種別 失敗|手順|観測|未記入（failure|howto|observation|none も可）で追加の絞り込み")
	fs.StringVar(&catalog, "catalog", "", "索引(catalog.md)のパス(既定: 設定ファイルと同じディレクトリの index/catalog.md)")
	fs.StringVar(&topic, "topic", "", "タイトル・要旨・パスにこの語を含む行だけ(大小無視)")
	fs.StringVar(&repo, "repo", "", "この見出し(リポ名)の行だけ")
	fs.StringVar(&dir, "dir", "", "索引を使わず、このディレクトリ配下の *.md を列挙する(archive と . で始まるディレクトリの配下は除く)")
	fs.BoolVar(&full, "full", false, "全件")
	fs.IntVar(&size, "size", scope.DefaultChunkSize, "chunk あたりの件数")
	fs.BoolVar(&asJSON, "json", false, "JSON で出す(mode・n_entries・chunks)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex scope (-topic <語> [-repo <名>] | -repo <名> | -dir <パス> | -full) [-size N] [-json]")
		fmt.Fprintln(stderr, "  -topic と -repo は併用可。両方の条件を満たす行だけを出す。-dir と -full は単独指定。")
		fmt.Fprintln(stderr, "  索引から矛盾検査の走査対象を列挙・絞り込み・chunk 分割して出す。矛盾の判定はしない(実ファイルを読むのは人かエージェント)。")
		fmt.Fprintln(stderr, "  終了コード: 0 / 1 失敗 / 2 対象が 2 件未満")
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
		fmt.Fprintf(stderr, "braindex scope: 位置引数は受け付けない: %q\n", fs.Args())
		return 1
	}
	modes := 0
	for _, on := range []bool{topic != "" || repo != "", dir != "", full} {
		if on {
			modes++
		}
	}
	if modes != 1 {
		fmt.Fprintln(stderr, "braindex scope: -topic / -repo / -dir / -full を指定する（併用できるのは -topic と -repo のみ）")
		return 1
	}
	if size <= 0 {
		fmt.Fprintln(stderr, "braindex scope: -size は 1 以上")
		return 1
	}

	normalized, typeErr := notetype.Normalize(noteType)
	if typeErr != nil {
		fmt.Fprintln(stderr, "braindex scope:", typeErr)
		return 1
	}
	root := ""
	if normalized != "" && dir == "" {
		cfg, _, _, err := resolve(o)
		if err != nil {
			fmt.Fprintf(stderr, "braindex scope: -type には root が要る（-root か設定の root）: %v\n", err)
			return 1
		}
		root = cfg.Root
	}
	var content []byte
	if dir == "" {
		if catalog == "" {
			p, err := scopeCatalogPath(o)
			if err != nil {
				fmt.Fprintln(stderr, "braindex scope:", err)
				return 1
			}
			catalog = p
		}
		b, err := os.ReadFile(catalog)
		if err != nil {
			fmt.Fprintf(stderr, "braindex scope: 索引を読めない: %s: %s(hub で braindex を実行して作る)\n", filepath.ToSlash(catalog), scan.DescribeErr(err))
			return 1
		}
		content = b
	}
	res, err := scope.Build(content, scope.Options{Topic: topic, Repo: repo, Dir: dir, Size: size, Type: normalized, Root: root})
	if err != nil {
		fmt.Fprintln(stderr, "braindex scope:", err)
		return 1
	}
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			fmt.Fprintln(stderr, "braindex scope:", err)
			return 1
		}
	} else {
		stdout.Write(scope.Render(res))
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(stderr, "braindex scope: 警告:", w)
	}
	if res.Entries < 2 {
		fmt.Fprintf(stderr, "braindex scope: 対象が %d 件で突き合わせられない(2 件以上要る)。話題・範囲を広げる\n", res.Entries)
		return 2
	}
	if len(res.Warnings) > 0 {
		return 2
	}
	return 0
}

// scopeCatalogPath は -catalog が無いときの索引の場所を返す。設定ファイルがあればそのディレクトリ、
// 無ければカレント基準の index/catalog.md。
//
// -type の無い scope は root を使わないので、ここでは resolve() を使わない。
func scopeCatalogPath(o options) (string, error) {
	cfgPath, explicit := o.config, o.config != ""
	if !explicit {
		cfgPath = defaultConfig
	}
	st, err := os.Stat(cfgPath)
	if err == nil && !st.IsDir() {
		return filepath.Join(filepath.Dir(cfgPath), filepath.FromSlash(defaultOut)), nil
	}
	if explicit {
		return "", fmt.Errorf("設定ファイルが見つからない: %s", cfgPath)
	}
	return filepath.FromSlash(defaultOut), nil
}
