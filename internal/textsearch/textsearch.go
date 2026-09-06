// Package textsearch はノート本文を規則ベースの文字列一致で検索し、当たった位置(ファイルと行)を返す。
//
// 索引(catalog.md)は「パスを引く」ためのもので、要旨 80 字は手がかりにすぎない。索引に語が無いことは
// 本文に記録が無い証明にならないので、本文そのものを読んで語を照合する口を持つ(設計レビュー補足 2026-09-06)。
//
// 読む範囲は索引と同じ走査規則(internal/scan)に従う。archive の下・設定が見に行かない場所は読まない。
// 走査や読み込みで確認できなかった範囲は Result.Gaps に残す——「一致なし」と「確認できなかった」を
// 呼び出し側が区別できるようにするため。同じ材料からは同じ結果が出る(Hits はパス→行の順)。
// 意味検索・埋め込み・LLM は使わない(決定 2026-08-19・2026-08-07)。本文はここで読むだけで、どこにも送らない。
package textsearch

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pilefort/braindex/internal/scan"
)

// Query は検索条件。
type Query struct {
	Terms     []string // 検索語(1 つ以上。空文字は誤り)。正規表現ではなく、そのままの文字列で照合する
	Any       bool     // true: どれか 1 語を含む行が当たり。false(既定): 全部の語を含む行だけ
	MatchCase bool     // true: 大小を区別する。既定は無視(ラテン文字だけに効く。かな・漢字は影響しない)
	WholeWord bool     // true: ラテン文字の語は前後が英数字・_・- でないときだけ当てる(go が google に当たらない)。かな・漢字の語には効かない
	Repo      string   // 空でなければ、このリポ(root 直下のディレクトリ名)だけを読む
	Kind      string   // 空でなければ、この種別(完全一致か "種別/" で始まるもの)だけを読む
	Limit     int      // 0 で無制限。並べたあと先頭 N 件だけを Hits に残す(Total は切る前の数)
}

// Hit は一致した 1 行。同じ行に複数の語があっても 1 件。
type Hit struct {
	Repo  string   // リポ名
	Kind  string   // 種別(索引と同じラベル)
	Path  string   // root 相対・スラッシュ区切り(索引のパスと同じ形)
	Line  int      // 行番号(1 始まり。LF・CRLF で数える)
	Col   int      // 最初に当たった語の位置(1 始まり・文字数。元の行の先頭から数える)
	Text  string   // 行の内容(前後の空白を除く。長い行は一致箇所の周辺だけにし、切った側に … を付ける)
	Terms []string // この行に含まれていた検索語(Query.Terms の順)
}

// Result は検索の結果。
type Result struct {
	Query    Query      // 実行した条件(そのまま)
	Files    int        // 最後まで読んで検索したファイル数
	Total    int        // 一致した行数(Limit で切る前)
	Hits     []Hit      // 一致した行(Path 昇順 → Line 昇順。Limit があれば先頭 N 件)
	Gaps     []scan.Gap // 確認できなかった範囲(走査で列挙できなかった範囲と、開けない・途中までしか読めなかったファイル。Rel 昇順)
	Warnings []string   // 飛ばしたものの説明(走査の警告と Gaps の分)。無言スキップにしない
}

// Complete は確認できなかった範囲が無いこと。false なら「一致なし」を「記録が無い」と読んではいけない。
func (r Result) Complete() bool { return len(r.Gaps) == 0 }

// ByTerm は検索語ごとに、その語を含む行を返す(語は Query.Terms のもの。当たらなかった語は載らない)。
// 複数の語を Any で一度に引き、語ごとに出典を見たいとき(学習候補の本文照合)に使う。
func (r Result) ByTerm() map[string][]Hit {
	out := map[string][]Hit{}
	for _, h := range r.Hits {
		for _, t := range h.Terms {
			out[t] = append(out[t], h)
		}
	}
	return out
}

// Validate は条件の誤りを返す。語が無い・空の語・Limit が負は誤り。
func (q Query) Validate() error {
	if len(q.Terms) == 0 {
		return errors.New("検索語が無い")
	}
	for _, t := range q.Terms {
		if t == "" {
			return errors.New("空の検索語がある")
		}
	}
	if q.Limit < 0 {
		return fmt.Errorf("limit は 0 以上: %d", q.Limit)
	}
	return nil
}

// Run は cfg の走査規則で対象を列挙し、本文を検索する。走査できない(root が空・読めない)ときだけ error。
func Run(cfg scan.Config, q Query) (Result, error) {
	if err := q.Validate(); err != nil {
		return Result{}, err
	}
	sc, err := scan.Scan(cfg)
	if err != nil {
		return Result{}, err
	}
	return Search(sc, q)
}

// Search は走査結果 sc のファイルを読んで検索する(走査済みの結果を使い回す口)。
// sc.Gaps と sc.Warnings は結果に引き継ぎ、開けなかったファイルを Gaps に足す。
func Search(sc scan.Result, q Query) (Result, error) {
	if err := q.Validate(); err != nil {
		return Result{}, err
	}
	m := newMatcher(q)
	res := Result{Query: q, Warnings: append([]string(nil), sc.Warnings...)}
	gaps := append([]scan.Gap(nil), sc.Gaps...)
	for _, f := range sc.Files {
		if !m.wants(f) {
			continue
		}
		hits, err := searchFile(f, m)
		res.Hits = append(res.Hits, hits...)
		if err != nil {
			reason := scan.DescribeErr(err)
			gaps = append(gaps, scan.Gap{Rel: f.Rel, Reason: reason})
			res.Warnings = append(res.Warnings, f.Rel+": "+reason)
			continue
		}
		res.Files++
	}
	// リポで絞ったときは、他のリポの確認できなかった範囲はこの検索に関係ない
	if q.Repo != "" {
		kept := gaps[:0]
		for _, g := range gaps {
			if g.Rel == q.Repo || strings.HasPrefix(g.Rel, q.Repo+"/") {
				kept = append(kept, g)
			}
		}
		gaps = kept
	}
	res.Gaps = scan.SortGaps(gaps)
	sort.SliceStable(res.Hits, func(i, j int) bool {
		if res.Hits[i].Path != res.Hits[j].Path {
			return res.Hits[i].Path < res.Hits[j].Path
		}
		return res.Hits[i].Line < res.Hits[j].Line
	})
	res.Total = len(res.Hits)
	if q.Limit > 0 && len(res.Hits) > q.Limit {
		res.Hits = res.Hits[:q.Limit]
	}
	if res.Hits == nil {
		res.Hits = []Hit{}
	}
	return res, nil
}

// maxText は Hit.Text に残す最大の文字数。超える行は一致箇所の周辺だけにする(数 MB の 1 行を丸ごと返さない)。
const maxText = 200

// matcher は 1 行に検索語を当てる。
type matcher struct {
	q     Query
	terms []string // 照合用の語(大小無視なら小文字化済み)
}

func newMatcher(q Query) *matcher {
	m := &matcher{q: q, terms: make([]string, len(q.Terms))}
	for i, t := range q.Terms {
		if q.MatchCase {
			m.terms[i] = t
		} else {
			m.terms[i] = strings.ToLower(t)
		}
	}
	return m
}

// wants は f が Repo・Kind の絞り込みに入るか。
func (m *matcher) wants(f scan.File) bool {
	if m.q.Repo != "" && f.Repo != m.q.Repo {
		return false
	}
	if m.q.Kind != "" && f.Kind != m.q.Kind && !strings.HasPrefix(f.Kind, m.q.Kind+"/") {
		return false
	}
	return true
}

// match は line(改行を除いた 1 行)に語を当て、当たったら最初の一致の文字位置(0 始まり)と含まれていた語を返す。
func (m *matcher) match(line string) (col int, terms []string, ok bool) {
	target := line
	if !m.q.MatchCase {
		// strings.ToLower は 1 文字を 1 文字に写す(文字数を変えない)ので、小文字化した側で見つけた位置の
		// 文字数は、元の行での文字位置と一致する
		target = strings.ToLower(line)
	}
	first := -1
	for i, t := range m.terms {
		idx := m.find(target, t)
		if idx < 0 {
			if !m.q.Any {
				return 0, nil, false
			}
			continue
		}
		terms = append(terms, m.q.Terms[i])
		if first < 0 || idx < first {
			first = idx
		}
	}
	if first < 0 {
		return 0, nil, false
	}
	return utf8.RuneCountInString(target[:first]), terms, true
}

// find は s の中の term の最初の位置(バイト)を返す。WholeWord ならラテン文字の語の境界を見る。無ければ -1。
func (m *matcher) find(s, term string) int {
	from := 0
	for {
		i := strings.Index(s[from:], term)
		if i < 0 {
			return -1
		}
		i += from
		if !m.q.WholeWord || boundaryOK(s, i, i+len(term)) {
			return i
		}
		_, w := utf8.DecodeRuneInString(s[i:])
		from = i + w
	}
}

// boundaryOK は s[i:j] の語がラテン文字の語として区切られているか。
// 語の先頭が英数字なら直前は英数字・_・- でないこと、末尾が英数字なら直後も同じ。
// かな・漢字の側には境界を引かない——連続して書かれるので「全索引化」の中の「索引」も語の一致として扱う。
func boundaryOK(s string, i, j int) bool {
	first, _ := utf8.DecodeRuneInString(s[i:j])
	last, _ := utf8.DecodeLastRuneInString(s[i:j])
	if isLatinWord(first) && i > 0 {
		if p, _ := utf8.DecodeLastRuneInString(s[:i]); isLatinWord(p) {
			return false
		}
	}
	if isLatinWord(last) && j < len(s) {
		if n, _ := utf8.DecodeRuneInString(s[j:]); isLatinWord(n) {
			return false
		}
	}
	return true
}

// isLatinWord はラテン文字の語を構成する文字(interest.Words の識別子の規則と同じ: 英字・数字・_・-)。
func isLatinWord(r rune) bool {
	if r == '_' || r == '-' {
		return true
	}
	if r < utf8.RuneSelf {
		return ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')
	}
	return unicode.Is(unicode.Latin, r) || unicode.IsDigit(r)
}

// searchFile は f を行ごとに読んで検索する。開けない・途中で読めなくなったときは、それまでの一致と error を返す。
// bufio.Scanner は 1 行の長さに上限があるので使わず、ReadBytes で 1 行ずつ読む(1 行が数 MB でも落ちない。
// 使うメモリは一番長い 1 行の分だけ)。
func searchFile(f scan.File, m *matcher) ([]Hit, error) {
	fh, err := os.Open(f.Abs)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	br := bufio.NewReaderSize(fh, 1<<20)
	var hits []Hit
	lineNo := 0
	for {
		raw, rerr := br.ReadBytes('\n')
		if len(raw) > 0 {
			lineNo++
			if lineNo == 1 && len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
				raw = raw[3:] // UTF-8 BOM は本文ではない(1 文字目の位置がずれないように落とす)
			}
			line := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
			if col, terms, ok := m.match(line); ok {
				hits = append(hits, Hit{
					Repo:  f.Repo,
					Kind:  f.Kind,
					Path:  f.Rel,
					Line:  lineNo,
					Col:   col + 1,
					Text:  excerpt(line, col),
					Terms: terms,
				})
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				return hits, nil
			}
			return hits, fmt.Errorf("途中までしか読めなかった(%d 行目まで): %w", lineNo, rerr)
		}
	}
}

// excerpt は行の表示用の抜粋。前後の空白を除き、maxText 文字を超えるなら一致箇所(col・0 始まりの文字位置)の
// 周辺だけを残して、切った側に … を付ける。
func excerpt(line string, col int) string {
	runes := []rune(line)
	// 先頭の空白を落とす分だけ col を詰める
	start := 0
	for start < len(runes) && unicode.IsSpace(runes[start]) {
		start++
	}
	end := len(runes)
	for end > start && unicode.IsSpace(runes[end-1]) {
		end--
	}
	runes = runes[start:end]
	col -= start
	if col < 0 {
		col = 0
	}
	if len(runes) <= maxText {
		return string(runes)
	}
	// 一致箇所が窓の前寄りに入るように、少し手前から maxText 文字
	from := col - maxText/4
	if from < 0 {
		from = 0
	}
	to := from + maxText
	if to > len(runes) {
		to = len(runes)
		from = to - maxText
	}
	s := string(runes[from:to])
	if from > 0 {
		s = "…" + s
	}
	if to < len(runes) {
		s += "…"
	}
	return s
}
