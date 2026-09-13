package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/pilefort/braindex/internal/textsearch"
)

func init() {
	register(&command{
		name:    "search",
		summary: "ノート本文を語の一致で検索し、当たった位置(パス:行)と確認できなかった範囲を出す(索引と同じ走査規則・手元だけ)",
		run:     runSearch,
	})
}

// runSearch は braindex search [フラグ] <語> [語...] を実行する。
//
// 索引(catalog.md)は読まず、索引と同じ走査規則(設定の root・notes_dirs・extra)で対象を列挙して本文を読む。
// 出力は 1 件 1 行「パス:行: 行の内容」。読めなかった範囲は「確認できなかった範囲」として結果に出す——
// 「一致なし」と「読めていないので分からない」を分けるため。本文は手元で読むだけで、外には送らない。
// 終了コード: 0 成功(一致なしも 0) / 1 失敗(語が無い・root が無い等) / 2 警告つき(確認できなかった範囲がある)。
func runSearch(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("braindex search", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o options
	var q textsearch.Query
	var asJSON bool
	fs.StringVar(&o.config, "config", "", "設定ファイルのパス(既定: カレントの braindex.json。無ければフラグだけで動き、-root が必須)")
	fs.StringVar(&o.root, "root", "", "走査のルート(設定ファイルの root より優先)")
	fs.StringVar(&q.Repo, "repo", "", "このリポ(root 直下のディレクトリ名)だけを読む")
	fs.StringVar(&q.Kind, "kind", "", "この種別だけを読む(完全一致か「種別/」で始まるもの。notes は notes/common も含む)")
	fs.StringVar(&q.Type, "type", "", "内容の種別 失敗|手順|観測|未記入（failure|howto|observation|none も可）。ノート先頭 10 行の「種別:」行で絞る")
	fs.BoolVar(&q.Any, "any", false, "どれか 1 語を含む行を当たりにする(既定: 全部の語を含む行だけ)")
	fs.BoolVar(&q.MatchCase, "case", false, "大小を区別する(既定: 無視。かな・漢字には関係ない)")
	fs.BoolVar(&q.WholeWord, "w", false, "ラテン文字の語は前後が英数字・_・- でないときだけ当てる(go が google に当たらない)。かな・漢字の語には効かない")
	fs.IntVar(&q.Limit, "limit", 0, "出す件数の上限(パス→行の順で先頭 N 件。0 で全件)")
	fs.BoolVar(&asJSON, "json", false, "JSON で出す(terms・files・total・hits・gaps・complete)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex search [フラグ] <語> [語...]")
		fmt.Fprintln(stderr, "  索引と同じ走査規則で各リポのノート本文を読み、語をそのまま(正規表現でなく)照合して「パス:行: 内容」を出す。")
		fmt.Fprintln(stderr, "  索引の要旨に無い語も本文にあれば当たる。読めなかった範囲は「確認できなかった範囲」として出し、一致なしと区別する。")
		fmt.Fprintln(stderr, "  本文は手元で読むだけで外には送らない。意味検索は使わない。")
		fmt.Fprintln(stderr, "  終了コード: 0 成功(一致なしも 0) / 1 失敗 / 2 確認できなかった範囲がある")
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
		fmt.Fprintln(stderr, "braindex search:", err)
		return 1
	}
	q.Terms = fs.Args()
	if err := q.Validate(); err != nil {
		fmt.Fprintln(stderr, "braindex search:", err)
		fs.Usage()
		return 1
	}
	// root と設定の解決は索引生成と同じ(フラグ > 設定ファイル > 既定)。出力先と生成日は使わない
	cfg, _, _, err := resolve(o)
	if err != nil {
		return fail(err)
	}
	res, err := textsearch.Run(cfg, q)
	if err != nil {
		return fail(err)
	}
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(searchJSON(res)); err != nil {
			return fail(err)
		}
	} else {
		stdout.Write(renderSearch(res))
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(stderr, "braindex search: 警告:", w)
	}
	if len(res.Warnings) > 0 {
		fmt.Fprintf(stderr, "braindex search: 警告 %d 件(終了コード 2)\n", len(res.Warnings))
		return 2
	}
	return 0
}

// renderSearch は人が読む形。先頭に条件と対象、続けて 1 件 1 行「パス:行: 内容」、最後に確認できなかった範囲。
//
//	# braindex search: 索引 決定性（全部の語を含む行・大小無視）
//	対象 4 ファイル・一致 2 行（2 ファイル）
//	repo-a/docs/notes/long.md:40: 同じ入力からは…
//	確認できなかった範囲 1 件（この中に一致があるかは分からない）
//	- repo-b/docs/notes/locked/ — permission denied
func renderSearch(r textsearch.Result) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# braindex search: %s（%s）\n", strings.Join(r.Query.Terms, " "), describeQuery(r.Query))
	scope := ""
	if r.Query.Repo != "" || r.Query.Kind != "" || r.Query.Type != "" {
		var parts []string
		if r.Query.Type != "" {
			parts = append(parts, "内容の種別 "+r.Query.Type)
		}
		if r.Query.Repo != "" {
			parts = append(parts, "リポ "+r.Query.Repo)
		}
		if r.Query.Kind != "" {
			parts = append(parts, "種別 "+r.Query.Kind)
		}
		scope = "（" + strings.Join(parts, "・") + "）"
	}
	files := map[string]bool{}
	for _, h := range r.Hits {
		files[h.Path] = true
	}
	switch {
	case r.Total == 0 && r.Complete():
		fmt.Fprintf(&b, "対象 %d ファイル%s・一致なし（走査した範囲は全部確認できた）\n", r.Files, scope)
	case r.Total == 0:
		fmt.Fprintf(&b, "対象 %d ファイル%s・一致なし（ただし確認できなかった範囲がある。無いとは言えない）\n", r.Files, scope)
	case len(r.Hits) < r.Total:
		fmt.Fprintf(&b, "対象 %d ファイル%s・一致 %d 行（先頭 %d 行だけ出す）\n", r.Files, scope, r.Total, len(r.Hits))
	default:
		fmt.Fprintf(&b, "対象 %d ファイル%s・一致 %d 行（%d ファイル）\n", r.Files, scope, r.Total, len(files))
	}
	for _, h := range r.Hits {
		fmt.Fprintf(&b, "%s:%d: %s\n", h.Path, h.Line, h.Text)
	}
	if len(r.Gaps) > 0 {
		fmt.Fprintf(&b, "確認できなかった範囲 %d 件（この中に一致があるかは分からない）\n", len(r.Gaps))
		for _, g := range r.Gaps {
			rel := g.Rel
			if g.Dir {
				rel += "/"
			}
			fmt.Fprintf(&b, "- %s — %s\n", rel, strings.Join(strings.Fields(g.Reason), " "))
		}
	}
	return []byte(b.String())
}

// describeQuery は条件を短く言い直す(見出し用)。
func describeQuery(q textsearch.Query) string {
	parts := []string{"全部の語を含む行"}
	if len(q.Terms) == 1 {
		parts = []string{"語を含む行"}
	} else if q.Any {
		parts = []string{"どれか 1 語を含む行"}
	}
	if q.MatchCase {
		parts = append(parts, "大小区別")
	} else {
		parts = append(parts, "大小無視")
	}
	if q.WholeWord {
		parts = append(parts, "語の境界あり")
	}
	return strings.Join(parts, "・")
}

// searchOut は -json の形。走査の Gap は小文字のキーに揃えて出す(scan.Gap には JSON タグが無い)。
type searchOut struct {
	Type      string      `json:"type"`
	Terms     []string    `json:"terms"`
	Any       bool        `json:"any"`
	MatchCase bool        `json:"match_case"`
	WholeWord bool        `json:"whole_word"`
	Repo      string      `json:"repo,omitempty"`
	Kind      string      `json:"kind,omitempty"`
	Files     int         `json:"files"`
	Total     int         `json:"total"`
	Hits      []searchHit `json:"hits"`
	Gaps      []searchGap `json:"gaps"`
	Complete  bool        `json:"complete"`
	Warnings  []string    `json:"warnings,omitempty"`
}

type searchHit struct {
	Type  string   `json:"type"`
	Repo  string   `json:"repo"`
	Kind  string   `json:"kind"`
	Path  string   `json:"path"`
	Line  int      `json:"line"`
	Col   int      `json:"col"`
	Text  string   `json:"text"`
	Terms []string `json:"terms"`
}

type searchGap struct {
	Rel    string `json:"rel"`
	Dir    bool   `json:"dir"`
	Reason string `json:"reason"`
}

func searchJSON(r textsearch.Result) searchOut {
	out := searchOut{
		Type:  r.Query.Type,
		Terms: r.Query.Terms, Any: r.Query.Any, MatchCase: r.Query.MatchCase, WholeWord: r.Query.WholeWord,
		Repo: r.Query.Repo, Kind: r.Query.Kind,
		Files: r.Files, Total: r.Total, Hits: []searchHit{}, Gaps: []searchGap{}, Complete: r.Complete(), Warnings: r.Warnings,
	}
	for _, h := range r.Hits {
		out.Hits = append(out.Hits, searchHit{Type: h.Type, Repo: h.Repo, Kind: h.Kind, Path: h.Path, Line: h.Line, Col: h.Col, Text: h.Text, Terms: h.Terms})
	}
	for _, g := range r.Gaps {
		out.Gaps = append(out.Gaps, searchGap{Rel: g.Rel, Dir: g.Dir, Reason: g.Reason})
	}
	return out
}
