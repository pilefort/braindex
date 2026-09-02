// Package approvals は work/APPROVALS.md(判断待ち)を扱う。
//
// 1 項目 = 1 判断で、見出し「## N. <決めたいこと>」の下に 5 欄(決めたいこと／なぜ今決めるか／選択肢／私の案／
// 決めないとどうなるか)を「**ラベル:** 値」で書く(書式は docs/conventions.md の APPROVALS 節)。
// このパッケージはその機械部分(解析・欠落の検査・フォーム・回答の反映)を担い、判断は人がする。
// 索引生成と同じく LLM は使わず、同じ入力から同じ出力を返す。
package approvals

import (
	"regexp"
	"strconv"
	"strings"
)

// 欄の正規ラベル。Item.Fields のキーに使う。
const (
	FieldWhat        = "決めたいこと"
	FieldWhyNow      = "なぜ今決めるか"
	FieldOptions     = "選択肢"
	FieldRecommend   = "私の案"
	FieldIfUndecided = "決めないとどうなるか"
	FieldHold        = "保留" // 「**保留（日付）:** コメント」。apply が付け、欄ではなく Holds に集める
)

// requiredFields は欠けると warning にする欄(選択肢は別に数える)。表示順でもある。
var requiredFields = []string{FieldWhat, FieldWhyNow, FieldRecommend, FieldIfUndecided}

// fieldAliases はラベルの言い換えを正規ラベルへ寄せる。
var fieldAliases = map[string]string{
	"背景": FieldWhyNow, "なぜ今": FieldWhyNow, "なぜ決めるか": FieldWhyNow,
	"なぜ今決める必要があるか": FieldWhyNow, "なぜ決める必要があるか": FieldWhyNow,
	"何を決めるか": FieldWhat, "決めたい事": FieldWhat,
	"決めないとどうなる": FieldIfUndecided, "決めなかった場合": FieldIfUndecided,
	"推奨": FieldRecommend, "私の推奨": FieldRecommend, "推奨案": FieldRecommend,
}

// Option は選択肢 1 つ。Key は A/B/…(大文字)、Label は案、Desc は得失(無ければ空)。
type Option struct {
	Key   string
	Label string
	Desc  string
}

// Item は判断 1 件。
type Item struct {
	N           int               // 見出しの番号(無ければ出現順で 1 から)
	Title       string            // 見出し(番号を除く)
	Fields      map[string]string // 正規ラベル → 値(複数行は改行で連結)
	Options     []Option          // 出現順
	Recommended string            // 私の案が指す選択肢の Key。選択肢に無い・案なし のとき空
	Reason      string            // 私の案の理由(「A — 理由」の後半)
	Warnings    []string          // 記載漏れ。空が正常
	Holds       []string          // 保留行(「**保留（日付）:** …」)。原文のまま
	Raw         string            // 見出し行から次の見出し直前まで(末尾の空行は落とし、改行 1 つで終える)
}

// Doc は APPROVALS.md 全体。
type Doc struct {
	Preamble string // 最初の見出しより前(H1 と説明文)。末尾の改行は落とす
	Items    []Item
}

var (
	headRe  = regexp.MustCompile(`^##\s+(?:(\d+)\s*[.．、)）]\s*)?(.+?)\s*$`)
	fieldRe = regexp.MustCompile(`^\*\*([^*]+?)\s*[:：]?\s*\*\*\s*[:：]?\s*(.*)$`)
	optRe   = regexp.MustCompile(`^\s*[-*+]\s+(?:([A-Za-z0-9])\s*[.．:：)）]\s*)?(.+?)\s*$`)
	recRe   = regexp.MustCompile(`^([A-Za-z0-9])(?:\s*[.．:：)）]|\s+[—―–-]|\s*$)\s*(.*)$`)
	// 「案 — 得失」の区切り。全角ダッシュ類か、空白で挟んだ --
	optSplitRe = regexp.MustCompile(`\s*[—―–]\s*|\s+--\s+`)
)

// Parse は APPROVALS.md を解析する。壊れた入力でもエラーにせず、欠落は Item.Warnings に出す。
func Parse(md []byte) Doc {
	lines := splitLines(md)
	var d Doc
	var pre []string
	var cur *rawItem
	flush := func() {
		if cur != nil {
			d.Items = append(d.Items, cur.finish())
			cur = nil
		}
	}
	for _, line := range lines {
		if m := headRe.FindStringSubmatch(line); m != nil {
			flush()
			n := len(d.Items) + 1
			// 番号が無い(m[1] == "")か桁あふれなら Atoi がエラーを返すので、出現順のまま
			if v, err := strconv.Atoi(m[1]); err == nil {
				n = v
			}
			cur = &rawItem{n: n, title: strings.TrimSpace(m[2]), raw: []string{line}}
			continue
		}
		if cur == nil {
			pre = append(pre, line)
		} else {
			cur.body = append(cur.body, line)
			cur.raw = append(cur.raw, line)
		}
	}
	flush()
	d.Preamble = strings.TrimRight(strings.Join(pre, "\n"), "\n")
	return d
}

// splitLines は BOM を除去し CRLF/CR を LF に正規化して行に分割する(internal/extract と同じ規則)。
func splitLines(content []byte) []string {
	// UTF-8 BOM (EF BB BF) を除去。ソースに BOM リテラルを置かず、バイトで判定する。
	if len(content) >= 3 && content[0] == 0xEF && content[1] == 0xBB && content[2] == 0xBF {
		content = content[3:]
	}
	s := string(content)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}

type rawItem struct {
	n     int
	title string
	body  []string
	raw   []string
}

// finish は 1 項目の行を欄・選択肢・推奨・保留・warning に分解する。
func (r *rawItem) finish() Item {
	it := Item{N: r.n, Title: r.title, Fields: map[string]string{}}
	it.Raw = strings.TrimRight(strings.Join(r.raw, "\n"), "\n") + "\n"
	key, inOpts := "", false
	for _, line := range r.body {
		s := strings.TrimSpace(line)
		if s == "" || s == "---" {
			continue
		}
		if m := fieldRe.FindStringSubmatch(s); m != nil {
			k := strings.TrimSpace(m[1])
			if a, ok := fieldAliases[k]; ok {
				k = a
			}
			v := strings.TrimSpace(m[2])
			if strings.HasPrefix(k, FieldHold) {
				it.Holds = append(it.Holds, s)
				key, inOpts = "", false
				continue
			}
			if k == FieldOptions {
				inOpts, key = true, ""
				continue
			}
			inOpts, key = false, k
			it.Fields[k] = v
			continue
		}
		if inOpts {
			if m := optRe.FindStringSubmatch(line); m != nil {
				k := strings.ToUpper(m[1])
				if k == "" {
					k = string(rune('A' + len(it.Options)))
				}
				label, desc := splitOption(m[2])
				it.Options = append(it.Options, Option{Key: k, Label: label, Desc: desc})
				continue
			}
			if n := len(it.Options); n > 0 { // 選択肢の続きの行(インデントした説明)
				it.Options[n-1].Desc = strings.TrimSpace(it.Options[n-1].Desc + " " + s)
				continue
			}
		}
		if key != "" {
			it.Fields[key] = strings.TrimSpace(it.Fields[key] + "\n" + s)
		}
	}
	if m := recRe.FindStringSubmatch(it.Fields[FieldRecommend]); m != nil {
		k := strings.ToUpper(m[1])
		if it.hasOption(k) {
			it.Recommended = k
			it.Reason = strings.TrimSpace(m[2])
		} else if it.Fields[FieldRecommend] != "" {
			it.Warnings = append(it.Warnings, FieldRecommend+" "+k+" が選択肢に無い")
		}
	}
	var missing []string
	for _, f := range requiredFields {
		if it.Fields[f] == "" {
			missing = append(missing, f+" が未記載")
		}
	}
	if len(it.Options) < 2 {
		missing = append(missing, FieldOptions+" が 1 つ以下（A/B… で 2 つ以上、各案の得失つきで列挙する）")
	}
	it.Warnings = append(missing, it.Warnings...)
	return it
}

func (it *Item) hasOption(key string) bool {
	_, ok := it.Option(key)
	return ok
}

// Option は Key の選択肢を返す。無ければ ok=false。
func (it *Item) Option(key string) (o Option, ok bool) {
	for _, o := range it.Options {
		if o.Key == key {
			return o, true
		}
	}
	return Option{}, false
}

// splitOption は「案 — 利点／欠点」を案と得失に分ける。
func splitOption(s string) (label, desc string) {
	if loc := optSplitRe.FindStringIndex(s); loc != nil {
		return strings.TrimSpace(s[:loc[0]]), strings.TrimSpace(s[loc[1]:])
	}
	return strings.TrimSpace(s), ""
}
