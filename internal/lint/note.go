package lint

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/pilefort/braindex/internal/extract"
)

// ノート(docs/notes・docs/decisions.md・work のメモ)の曖昧さ検査。
//
// 幻覚の温床になる書き方を規則ベースで検出する。価値判断はしない(判断は skill record-lint の手順で人かエージェントが行う)。
// 検出は 2 段階の確度で返す:
//   - warn      … 機械的に高確度(曖昧な数量詞・日付なし・失効行の違反)。ほぼそのまま指摘してよい
//   - candidate … ヒューリスティックで低確度(出典なき数字・裸のヘッジ・なぜ欠落・根拠欠落・未定義用語)。本文の意味で真偽を確かめる候補
//
// 語彙表(数量詞・ヘッジ・出典マーカー)はこのファイルに埋め込む。日本語のみ。

// 確度。
const (
	SeverityWarn      = "warn"
	SeverityCandidate = "candidate"
)

// 種別(JSON の kind。表示の見出しは kindLabel)。
const (
	KindVagueQuantifier = "vague_quantifier"
	KindNoDate          = "no_date"
	KindUncitedFigure   = "uncited_figure"
	KindBareHedge       = "bare_hedge"
	KindMissingWhy      = "missing_why"
	KindMissingEvidence = "missing_evidence"
	KindUndefinedTerm   = "undefined_term"
	KindSupersedeFormat = "supersede_format"
	KindSupersedeDate   = "supersede_date"
	KindSupersedeTarget = "supersede_target"
)

var kindLabel = map[string]string{
	KindVagueQuantifier: "曖昧な数量詞",
	KindNoDate:          "日付なし",
	KindUncitedFigure:   "出典なき数字(候補)",
	KindBareHedge:       "裸のヘッジ(候補)",
	KindMissingWhy:      "なぜ欠落(候補)",
	KindMissingEvidence: "根拠欠落(候補)",
	KindUndefinedTerm:   "未定義用語(候補)",
	KindSupersedeFormat: "失効行の書式",
	KindSupersedeDate:   "失効行の日付",
	KindSupersedeTarget: "失効行の後継",
}

// NoteOptions はノート検査の入力。
type NoteOptions struct {
	Glossary    []byte // 用語集の内容。HasGlossary が false なら未定義用語は見ない
	HasGlossary bool
}

// 曖昧な数量詞(数値・日付に置き換えるべき「ぼかし語」)。汎用すぎる語(高い/低い/大きい)は文脈で正当な用法が多いので入れない。
var vagueQuantifiers = []string{
	// 量
	"多い", "多く", "少ない", "少なく", "たくさん", "ほとんど", "多数", "大量",
	"わずか", "数多く", "いくつか", "ある程度", "大半", "少数", "大幅",
	// 頻度
	"よく", "しばしば", "たまに", "頻繁", "まれ", "ときどき", "時々",
	// 時期
	"最近", "近頃", "このごろ", "先日", "いつか",
	// 程度
	"かなり", "急増", "急減", "相当", "ずいぶん", "非常に", "とても", "すごく", "めっちゃ",
}

// 裸の確信度ヘッジ。明示タグ(推測/未確認)も出典も無いまま事実の主張に付いていたら候補。
var hedgeTokens = []string{
	"たぶん", "多分", "おそらく", "恐らく",
	"かもしれない", "かも知れない", "かもしれません",
	"だろう", "でしょう", "と思う", "と思われ", "と思います",
	"気がする", "はず", "らしい", "ようだ", "っぽい",
}

var (
	// 日付表記(ISO / スラッシュ / 和式)
	reDate = regexp.MustCompile(`\d{4}-\d{1,2}-\d{1,2}|\d{4}/\d{1,2}/\d{1,2}|\d{4}\s*年\s*\d{1,2}\s*月`)
	// 出典・根拠を示すマーカー(同じ行にあれば「出典あり」)
	reCitation = regexp.MustCompile(`出典|根拠|参照|実測|由来|→|https?://|\[\[|\]\(|\.md|\.py|\.csv|\.json|§`)
	// 単位つきの数(数字による主張)
	reNumUnit = regexp.MustCompile(`[+\-]?\d+(?:\.\d+)?\s*(?:%|％|倍|割|件|人|本|回|万|千|億|pt|ポイント|dB)`)
	// 小数(前後に数字と . が続かないもの)。Go の正規表現に先読み・後読みが無いので前後 1 文字を取り込んで判定する
	reDecimal = regexp.MustCompile(`(?:^|[^\d.])\d+\.\d+(?:[^\d.]|$)`)
	// 理由を示す語
	reReason = regexp.MustCompile(`理由|なぜ|ため|から|背景|根拠|ので|によって|狙い|目的`)
	// 根拠行(行頭の「根拠:」。太字・全角コロン可)
	reEvidenceLine = regexp.MustCompile(`(?m)^\s*(?:\*\*)?根拠(?:\*\*)?\s*[:：]`)
	// 明示的な未検証フラグ
	reUncertaintyTag = regexp.MustCompile(`推測|未確認|要確認|要出典`)
	// 未定義用語の抽出源: 鉤括弧の語 / [[wiki-link]] / 英大文字始まりの語
	reQuotedTerm  = regexp.MustCompile(`「([^」\n]{1,24})」`)
	reWikiLink    = regexp.MustCompile(`\[\[([^\]\n|]+)`)
	reEnglishTerm = regexp.MustCompile(`\b([A-Z][A-Za-z0-9]{2,})\b`)
)

// CheckNote は path のノート content を検査し、指摘を行番号順(ファイル全体への指摘 = 0 が先頭)・種別順に返す。
// path は docs/decisions.md の判別(末尾が decisions.md なら全ブロックを決定として見る)と表示に使う。
func CheckNote(path string, content []byte, o NoteOptions) []Warning {
	lines := splitLines(content)
	text := strings.Join(lines, "\n")
	skip := fencedLines(lines)
	var ws []Warning
	add := func(line int, kind, severity, format string, a ...any) {
		ws = append(ws, Warning{
			Path: path, Line: line, Kind: kind, Severity: severity,
			Msg: "[" + kindLabel[kind] + "] " + fmt.Sprintf(format, a...),
		})
	}

	// 曖昧な数量詞(warn)
	for i, l := range lines {
		if skip[i] {
			continue
		}
		for _, w := range vagueQuantifiers {
			for n := strings.Count(l, w); n > 0; n-- {
				add(i+1, KindVagueQuantifier, SeverityWarn, "〔%s〕 %s", w, strings.TrimSpace(l))
			}
		}
	}

	// 日付なし(warn・文書全体)。規約は「ファイル名の YYYYMMDD か本文の日付」なので、ファイル名も数える。
	// 規則を二重定義しないよう、索引と同じ extract に本文を渡さず(＝ファイル名だけで)日付を引かせる。
	if !reDate.MatchString(text) && extract.Extract(baseName(path), nil, "").Date == "" {
		add(0, KindNoDate, SeverityWarn, "本文にもファイル名にも日付(YYYY-MM-DD 等)が無い")
	}

	// 出典なき数字(candidate)
	for i, l := range lines {
		if skip[i] {
			continue
		}
		s := strings.TrimSpace(l)
		// 表の行は構造化データの提示で、出典は周辺の地の文に書かれる想定なので見ない
		if s == "" || strings.HasPrefix(s, "|") || isDateOnly(s) {
			continue
		}
		if isNumClaim(l) && !reCitation.MatchString(l) {
			add(i+1, KindUncitedFigure, SeverityCandidate, "%s", s)
		}
	}

	// 裸のヘッジ(candidate。1 行 1 件)
	for i, l := range lines {
		if skip[i] {
			continue
		}
		s := strings.TrimSpace(l)
		if s == "" || strings.HasPrefix(s, "|") || reUncertaintyTag.MatchString(l) || reCitation.MatchString(l) {
			continue
		}
		for _, w := range hedgeTokens {
			if strings.Contains(l, w) {
				add(i+1, KindBareHedge, SeverityCandidate, "〔%s〕 %s", w, s)
				break
			}
		}
	}

	// なぜ欠落・根拠欠落(candidate。決定ブロックだけ)
	for _, b := range decisionBlocks(lines, path) {
		if !reReason.MatchString(b.body) {
			add(b.line, KindMissingWhy, SeverityCandidate, "## %s(理由/なぜ/背景が読み取れない)", b.head)
		}
		if !reEvidenceLine.MatchString(b.body) {
			add(b.line, KindMissingEvidence, SeverityCandidate, "## %s(根拠: 行が無い。notes/データ/URL/「会話 YYYY-MM-DD」のいずれかへ)", b.head)
		}
	}

	// 失効行(warn。decisions.md の ## 直下だけ)
	if baseName(path) == "decisions.md" {
		checkSupersedeLines(lines, skip, add)
	}

	// 未定義用語(candidate。用語集があるときだけ)
	if o.HasGlossary {
		glossary := string(o.Glossary)
		for _, t := range candidateTerms(text) {
			if !strings.Contains(glossary, t) {
				add(0, KindUndefinedTerm, SeverityCandidate, "用語「%s」が用語集に見当たらない", t)
			}
		}
	}

	sort.SliceStable(ws, func(i, j int) bool {
		if ws[i].Line != ws[j].Line {
			return ws[i].Line < ws[j].Line
		}
		return ws[i].Kind < ws[j].Kind
	})
	return ws
}

// fencedLines はコードフェンス(```)の内側と、フェンス記号の行そのものを true にする(検査から除く)。
func fencedLines(lines []string) []bool {
	out := make([]bool, len(lines))
	inside := false
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimLeft(l, " \t"), "```") {
			inside = !inside
			out[i] = true
			continue
		}
		out[i] = inside
	}
	return out
}

// baseName は表示用パス(/ 区切り。Windows の \ も来うる)の最後の要素を返す。
func baseName(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// isDateOnly は「- 2026-08-12」のように日付だけの行か。
func isDateOnly(s string) bool {
	t := strings.TrimSpace(strings.TrimLeft(s, "#-*> "))
	loc := reDate.FindStringIndex(t)
	return loc != nil && loc[0] == 0 && loc[1] == len(t)
}

// isNumClaim は単位つきの数か小数を含むか(数字による主張らしい行)。
func isNumClaim(l string) bool {
	return reNumUnit.MatchString(l) || reDecimal.MatchString(l)
}

type block struct {
	line int    // 見出しの行番号(1 始まり)
	head string // 見出しの文(## を除く)
	body string
}

// decisionBlocks は ## 見出しでブロックに分け、決定らしいもの(decisions.md 内の全ブロック / 「記録日」「採用日」の語を持つブロック)を返す。
// 語の有無だけで見るのは、日付を書き損ねた決定こそ検査したいため(日付の形を条件にすると素通りする)。
func decisionBlocks(lines []string, path string) []block {
	isDecisions := strings.HasSuffix(strings.ReplaceAll(path, "\\", "/"), "decisions.md")
	var blocks []block
	var cur *block
	var buf []string
	flush := func() {
		if cur != nil {
			cur.body = strings.Join(buf, "\n")
			blocks = append(blocks, *cur)
		}
	}
	for i, l := range lines {
		if strings.HasPrefix(l, "## ") {
			flush()
			cur = &block{line: i + 1, head: strings.TrimSpace(l[3:])}
			buf = nil
			continue
		}
		if cur != nil {
			buf = append(buf, l)
		}
	}
	flush()
	var out []block
	for _, b := range blocks {
		if isDecisions || strings.Contains(b.body, "記録日") || strings.Contains(b.body, "採用日") {
			out = append(out, b)
		}
	}
	return out
}

// candidateTerms は鉤括弧の語・[[wiki-link]]・英大文字始まりの語を重複なし昇順で返す。
func candidateTerms(text string) []string {
	seen := map[string]bool{}
	for _, re := range []*regexp.Regexp{reQuotedTerm, reWikiLink, reEnglishTerm} {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			if t := strings.TrimSpace(m[1]); t != "" {
				seen[t] = true
			}
		}
	}
	terms := make([]string, 0, len(seen))
	for t := range seen {
		terms = append(terms, t)
	}
	sort.Strings(terms)
	return terms
}
