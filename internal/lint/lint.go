// Package lint は work/ISSUE-*.md が規約の形を守っているかを規則ベース(LLM を使わず規則だけ)で検査する。
//
// 検査は純関数 Check で行い、ファイルの読み書きも git の呼び出しもしない(呼び出し側が渡す)。
// 同じ入力からは常に同じ指摘が同じ順で出る。索引には載せない(work/ は索引しない)。
//
// 何を見るか(docs/conventions.md の ISSUE の形):
//   - 先頭の見出しが「# ISSUE:」で始まる
//   - 節「## 現在の作業」「## 状態」がある(対象リスト・メモは任意)
//   - 「← いまここ」がちょうど 1 つ
//   - 「最終更新: YYYY-MM-DD」があり、形式が正しく、未来でなく、(StaleDays > 0 なら)古すぎない
//   - 「仕様: SPEC-<slug>.md」の参照先がある(Exists を渡したときだけ)
//   - git HEAD と比べて(HasPrev のときだけ)、チェック項目が消えていない・内容が変わったのに最終更新が同じでない
package lint

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pilefort/braindex/internal/textutil"
)

// Warning は 1 件の指摘。
type Warning struct {
	Path     string `json:"path"`               // 表示用のパス(呼び出し側が決める。/ 区切り)
	Line     int    `json:"line"`               // 1 始まりの行番号。ファイル全体への指摘は 0
	Msg      string `json:"msg"`                // 表示文。ノート検査では先頭に「[種別]」が付く
	Kind     string `json:"kind,omitempty"`     // ノート検査の種別(vague_quantifier など。ISSUE 検査では空)
	Severity string `json:"severity,omitempty"` // ノート検査の確度(warn / candidate。ISSUE 検査では空)
}

// Options は検査の入力。
type Options struct {
	Today     time.Time             // 最終更新の未来判定と経過日数の基準日
	StaleDays int                   // 最終更新からこの日数以上たっていたら指摘する。0 なら見ない
	Prev      []byte                // git HEAD の内容。HasPrev が false なら比較しない
	HasPrev   bool                  // Prev を比較に使うか(未追跡・git 無しのときは false)
	Exists    func(rel string) bool // ISSUE と同じディレクトリからの相対パスが存在するか(SPEC の参照先)。nil なら見ない
}

var (
	reHere     = regexp.MustCompile(`←\s*いまここ`)
	reLast     = regexp.MustCompile(`^最終更新[:：]\s*(\S+)`)
	reSpec     = regexp.MustCompile(`^仕様[:：]\s*(SPEC-[^\s（(]+\.md)`)
	reCheckbox = regexp.MustCompile(`^\s*[-*] \[[ xX]\] (.*)$`)
)

// Check は path の内容 content を検査し、指摘を行番号順(ファイル全体への指摘 = 0 が先頭)に返す。
func Check(path string, content []byte, o Options) []Warning {
	lines := splitLines(content)
	var ws []Warning
	add := func(line int, format string, a ...any) {
		ws = append(ws, Warning{Path: path, Line: line, Msg: fmt.Sprintf(format, a...)})
	}

	// 見出し
	if h1 := firstH1(lines); h1 < 0 {
		add(0, "見出し「# ISSUE: <タスク名>」が無い")
	} else if !isIssueH1(lines[h1]) {
		add(h1+1, "先頭の見出しが「# ISSUE:」で始まらない: %s", lines[h1])
	}

	// 必須の節
	for _, sec := range []string{"現在の作業", "状態"} {
		if !hasSection(lines, sec) {
			add(0, "節「## %s」が無い", sec)
		}
	}

	// 現在地
	var here []int
	for i, l := range lines {
		if reHere.MatchString(l) {
			here = append(here, i+1)
		}
	}
	switch {
	case len(here) == 0:
		add(0, "「← いまここ」が無い(次の 1 手に付ける。全部済みなら ISSUE を消す)")
	case len(here) > 1:
		add(here[1], "「← いまここ」が %d 個ある(1 つにする)", len(here))
	}

	// 最終更新
	lastLine, lastRaw := findLastUpdated(lines)
	hasDate := false
	if lastLine == 0 {
		add(0, "「最終更新: YYYY-MM-DD」が無い")
	} else if d, err := time.Parse("2006-01-02", lastRaw); err != nil {
		add(lastLine, "最終更新の日付が YYYY-MM-DD でない: %q", lastRaw)
	} else {
		hasDate = true
		today := dateOnly(o.Today)
		if d.After(today) {
			add(lastLine, "最終更新が未来の日付: %s", lastRaw)
		} else if o.StaleDays > 0 {
			if days := int(today.Sub(d).Hours() / 24); days >= o.StaleDays {
				add(lastLine, "最終更新 %s から %d 日たっている(%d 日以上で指摘)", lastRaw, days, o.StaleDays)
			}
		}
	}

	// 仕様の参照先(最初の「仕様:」行だけ)
	if o.Exists != nil {
		for i, l := range lines {
			if m := reSpec.FindStringSubmatch(l); m != nil {
				if !o.Exists(m[1]) {
					add(i+1, "仕様 %s が無い(ISSUE と同じディレクトリに置く)", m[1])
				}
				break
			}
		}
	}

	// HEAD との比較
	if o.HasPrev {
		prev := splitLines(o.Prev)
		cur := map[string]bool{}
		for _, item := range checklistItems(lines) {
			cur[item] = true
		}
		for _, item := range checklistItems(prev) { // HEAD での出現順
			if !cur[item] {
				add(0, "HEAD にあったチェック項目が消えた: %q", item)
			}
		}
		if hasDate {
			if _, prevRaw := findLastUpdated(prev); prevRaw == lastRaw && !sameText(lines, prev) {
				add(lastLine, "内容が変わったのに最終更新 %s が HEAD と同じ", lastRaw)
			}
		}
	}

	sort.SliceStable(ws, func(i, j int) bool { return ws[i].Line < ws[j].Line })
	return ws
}

// splitLines は BOM を除去し CRLF/CR を LF に正規化して行に分割する(internal/extract と同じ規則)。
func splitLines(content []byte) []string {
	return textutil.SplitLines(content)
}

func firstH1(lines []string) int {
	for i, l := range lines {
		if strings.HasPrefix(l, "# ") {
			return i
		}
	}
	return -1
}

// isIssueH1 は「# ISSUE: …」か「# ISSUE：…」(全角コロン)。
func isIssueH1(l string) bool {
	t := strings.TrimSpace(strings.TrimPrefix(l, "# "))
	return strings.HasPrefix(t, "ISSUE:") || strings.HasPrefix(t, "ISSUE：")
}

// hasSection は「## <sec>」で始まる見出しがあるか(「## 状態（PR 単位）」のように補足が続いてもよい)。
func hasSection(lines []string, sec string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, "## ") && strings.HasPrefix(strings.TrimSpace(l[3:]), sec) {
			return true
		}
	}
	return false
}

// findLastUpdated は最後の「最終更新:」行の行番号(1 始まり。無ければ 0)と日付の文字列を返す。
func findLastUpdated(lines []string) (line int, raw string) {
	for i, l := range lines {
		if m := reLast.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			line, raw = i+1, dateToken(m[1])
		}
	}
	return line, raw
}

// dateToken は「2026-09-02（補足）」のように日付の直後に補足が続く書き方を許し、日付の部分だけを返す。
// 「2026-09-021」のように数字が続くものは日付とみなさず、そのまま返す(形式の指摘になる)。
func dateToken(s string) string {
	if len(s) > 10 && isISODate(s[:10]) && (s[10] < '0' || s[10] > '9') {
		return s[:10]
	}
	return s
}

func isISODate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// checklistItems はチェック項目(- [ ] / - [x])の本文を出現順に重複なしで返す。
// 「← いまここ」の印と前後の空白は除く(印の移動やチェックの変化を「消失」と数えないため)。
func checklistItems(lines []string) []string {
	seen := map[string]bool{}
	var items []string
	for _, l := range lines {
		m := reCheckbox.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		text := strings.TrimSpace(reHere.ReplaceAllString(m[1], ""))
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		items = append(items, text)
	}
	return items
}

// sameText は正規化後の本文が同じか。
func sameText(a, b []string) bool {
	return strings.Join(a, "\n") == strings.Join(b, "\n")
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
