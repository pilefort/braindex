package catalog

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/pilefort/braindex/internal/scan"

	"github.com/pilefort/braindex/internal/textutil"
)

// Coverage は索引がどこまで確認できたかの記録。
//
// 索引に行が無いことは「ノートが無い」証明にならない——走査で読めなかった範囲があれば、その中のノートは
// 載っていないだけで、あるかどうかは分からない。読み手(週次レビュー・後続の機能)が「削除」と「確認不能」を
// 分けられるよう、索引の先頭に記録し、ここで読み戻す(設計レビュー補足 2026-09-06)。
type Coverage struct {
	Known bool       // false なら記録が無い(この記録を書く前の版で生成した索引)。完全性は不明
	Gaps  []scan.Gap // 読めなかった範囲(Rel 昇順)。Known で空なら、走査した範囲は全部確認できた
}

// Complete は「記録があり、読めなかった範囲が無い」。
func (c Coverage) Complete() bool { return c.Known && len(c.Gaps) == 0 }

// Gap は rel(root 相対)を含む読めなかった範囲を返す。無ければ false。
func (c Coverage) Gap(rel string) (scan.Gap, bool) {
	for _, g := range c.Gaps {
		if g.Covers(rel) {
			return g, true
		}
	}
	return scan.Gap{}, false
}

// 索引に書く形。先頭の説明行の直後・最初のリポ見出しの前に置く。
//
//	走査: 読めなかった範囲なし
//
// または
//
//	走査: 読めなかった範囲 2 件（この範囲のノートは載っていない。無いのか読めないのかは分からない）
//	- 読めなかった: alpha/docs/notes/locked/ — permission denied
//	- 読めなかった: beta/docs/notes/x.md — permission denied
//
// ディレクトリは末尾の "/" で表す。表の行と「## 」見出しだけを見る読み手(indexdata.ParseCatalog)は
// これらの行を読み飛ばすので、行の形式は変わらない。
const (
	coveragePrefix = "走査: "
	coverageNone   = "読めなかった範囲なし"
	coverageSome   = "読めなかった範囲 %d 件"
	coverageNote   = "（この範囲のノートは載っていない。無いのか読めないのかは分からない）"
	gapPrefix      = "- 読めなかった: "
	gapSep         = " — "
)

// coverageLines は走査の記録の行(末尾に改行)。
func coverageLines(gaps []scan.Gap) string {
	if len(gaps) == 0 {
		return coveragePrefix + coverageNone + "\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, coveragePrefix+coverageSome+coverageNote+"\n", len(gaps))
	for _, g := range gaps {
		rel := g.Rel
		if g.Dir {
			rel += "/"
		}
		reason := strings.Join(strings.Fields(g.Reason), " ") // 改行や連続空白を 1 行に畳む
		b.WriteString(gapPrefix + rel + gapSep + reason + "\n")
	}
	return b.String()
}

// withCoverage は render が書いた catalog.md の先頭(説明行の直後)に走査の記録を差し込む。
func withCoverage(md []byte, cov Coverage) []byte {
	lines := []byte(coverageLines(cov.Gaps))
	// 説明行と最初のリポ見出しの間の空行の直前に入れる。リポが 1 つも無ければ末尾
	idx := bytes.Index(md, []byte("\n\n## "))
	if idx < 0 {
		out := append([]byte(nil), md...)
		if len(out) > 0 && out[len(out)-1] != '\n' {
			out = append(out, '\n')
		}
		return append(out, lines...)
	}
	out := make([]byte, 0, len(md)+len(lines))
	out = append(out, md[:idx+1]...)
	out = append(out, lines...)
	out = append(out, md[idx+1:]...)
	return out
}

// ParseCoverage は catalog.md から走査の記録を読み戻す。記録が無ければ Known=false(この記録を書く前の版で
// 生成した索引。完全性は不明)。記録の行が braindex の書く形でなければエラー(手で編集された)。
func ParseCoverage(b []byte) (Coverage, error) {
	var cov Coverage
	want := 0
	inList := false
	for i, line := range splitLines(b) {
		if strings.HasPrefix(line, "## ") {
			break // 記録は先頭の説明行の中にある
		}
		t := strings.TrimRight(line, " \t")
		if strings.HasPrefix(t, coveragePrefix) {
			if cov.Known {
				return Coverage{}, fmt.Errorf("catalog %d 行目: 走査の記録が 2 回ある", i+1)
			}
			cov.Known = true
			rest := strings.TrimPrefix(t, coveragePrefix)
			if rest == coverageNone {
				continue
			}
			if _, err := fmt.Sscanf(rest, coverageSome, &want); err != nil || want <= 0 {
				return Coverage{}, fmt.Errorf("catalog %d 行目: 走査の記録を読めない: %q", i+1, t)
			}
			inList = true
			continue
		}
		if !inList {
			continue
		}
		if !strings.HasPrefix(t, gapPrefix) {
			break // 一覧の終わり
		}
		rel, reason, _ := strings.Cut(strings.TrimPrefix(t, gapPrefix), gapSep)
		dir := strings.HasSuffix(rel, "/")
		rel = strings.TrimSuffix(rel, "/")
		if rel == "" {
			return Coverage{}, fmt.Errorf("catalog %d 行目: 読めなかった範囲のパスが空: %q", i+1, t)
		}
		cov.Gaps = append(cov.Gaps, scan.Gap{Rel: rel, Dir: dir, Reason: reason})
	}
	if inList && len(cov.Gaps) != want {
		return Coverage{}, fmt.Errorf("catalog: 走査の記録が %d 件と言うのに一覧は %d 件", want, len(cov.Gaps))
	}
	return cov, nil
}

// splitLines は BOM を除去し CRLF/CR を LF に正規化して行に分割する(indexdata と同じ規則)。
func splitLines(content []byte) []string {
	return textutil.SplitLines(content)
}
