package retro

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pilefort/braindex/internal/sessions"
)

// Window は集計の期間 [Since, Until)。ゼロ値は無制限。
type Window struct {
	Since time.Time
	Until time.Time
}

// Contains は t が窓に入るか。時刻の無い発話(ゼロ値)は、窓が無制限のときだけ入る(置く場所が決められないため)。
func (w Window) Contains(t time.Time) bool {
	if t.IsZero() {
		return w.Since.IsZero() && w.Until.IsZero()
	}
	if !w.Since.IsZero() && t.Before(w.Since) {
		return false
	}
	if !w.Until.IsZero() && !t.Before(w.Until) {
		return false
	}
	return true
}

// Recent は「now の日(loc の 0 時)の days 日前」以降の窓。日の途中で何度実行しても、その日の間は同じ窓になる。
// days が 0 以下なら無制限。
func Recent(now time.Time, days int, loc *time.Location) Window {
	if days <= 0 {
		return Window{}
	}
	n := now.In(loc)
	day := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)
	return Window{Since: day.AddDate(0, 0, -days)}
}

// Item は判定済みの人間の発話 1 つ。本文は持たない(数値と位置だけ。集計の出力に本文が混じらないように)。
type Item struct {
	Time    time.Time
	Project string // セッションの cwd(表示時に sessions.DisplayPath で "~" に置き換える)
	Session string // セッション ID
	Index   int    // セッション内で何番目の人間の発話か(1 始まり)
	Hit     bool   // 訂正辞書に当たったか
}

// Judge は窓に入る人間の発話を辞書で判定する(どれかの辞書に当たれば Hit)。順序はセッションの順 → 発話の順。
func Judge(ss []sessions.Session, w Window, dicts ...*Dictionary) []Item {
	var out []Item
	for _, s := range ss {
		for _, t := range s.HumanTurns() {
			if !w.Contains(t.Time) {
				continue
			}
			out = append(out, Item{Time: t.Time, Project: s.Project, Session: s.ID, Index: t.Index, Hit: len(Classify(t.Text, dicts...)) > 0})
		}
	}
	return out
}

// Count は 1 区分の集計。
type Count struct {
	Key         string // 区分(プロジェクト・週・位置の区間・"合計")
	Utterances  int    // 人間の発話数(分母)
	Corrections int    // 訂正ヒットのある発話数(分子)
}

// Rate は訂正率(0〜1)。発話が無ければ 0。
func (c Count) Rate() float64 {
	if c.Utterances == 0 {
		return 0
	}
	return float64(c.Corrections) / float64(c.Utterances)
}

func (c *Count) add(it Item) {
	c.Utterances++
	if it.Hit {
		c.Corrections++
	}
}

// Total は全体の合計(Key は "合計")。
func Total(items []Item) Count {
	c := Count{Key: "合計"}
	for _, it := range items {
		c.add(it)
	}
	return c
}

// groupBy は key が返す区分ごとに数える。ok が false の発話は数えない。
func groupBy(items []Item, key func(Item) (string, bool)) []Count {
	m := map[string]*Count{}
	for _, it := range items {
		k, ok := key(it)
		if !ok {
			continue
		}
		c := m[k]
		if c == nil {
			c = &Count{Key: k}
			m[k] = c
		}
		c.add(it)
	}
	out := make([]Count, 0, len(m))
	for _, c := range m {
		out = append(out, *c)
	}
	return out
}

// ByProject はプロジェクト別。発話数の多い順、同数は Key の昇順。cwd の先頭の home は "~" に置き換える。
func ByProject(items []Item, home string) []Count {
	out := groupBy(items, func(it Item) (string, bool) { return sessions.DisplayPath(it.Project, home), true })
	sort.Slice(out, func(i, j int) bool {
		if out[i].Utterances != out[j].Utterances {
			return out[i].Utterances > out[j].Utterances
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// ByWeek は ISO 週別("2026-W32")。古い順。週の境界は loc で決める。時刻の無い発話は入れない。
func ByWeek(items []Item, loc *time.Location) []Count {
	out := groupBy(items, func(it Item) (string, bool) {
		if it.Time.IsZero() {
			return "", false
		}
		y, wk := it.Time.In(loc).ISOWeek()
		return fmt.Sprintf("%04d-W%02d", y, wk), true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Bin はセッション内位置の区間 [Lo, Hi]。Hi が 0 なら上限なし。
type Bin struct {
	Lo    int
	Hi    int
	Label string // 設定に書かれた文字列そのまま("1-10"・"31-")
}

func (b Bin) contains(i int) bool { return i >= b.Lo && (b.Hi == 0 || i <= b.Hi) }

// ParseBins は "1-10,11-30,31-" の形を読む。各区間は "下限-上限" か "下限-"(上限なし)。
// 下限は 1 以上、上限は下限以上。区間は重ねない前提(重ねても検査はしない)。
func ParseBins(s string) ([]Bin, error) {
	var out []Bin
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		lo, hi, ok := strings.Cut(part, "-")
		if !ok || lo == "" {
			return nil, fmt.Errorf("位置の区間 %q: \"下限-上限\" か \"下限-\" の形で書く", part)
		}
		l, err := strconv.Atoi(lo)
		if err != nil || l < 1 {
			return nil, fmt.Errorf("位置の区間 %q: 下限は 1 以上の整数", part)
		}
		b := Bin{Lo: l, Label: part}
		if hi != "" {
			h, err := strconv.Atoi(hi)
			if err != nil || h < l {
				return nil, fmt.Errorf("位置の区間 %q: 上限は下限以上の整数", part)
			}
			b.Hi = h
		}
		out = append(out, b)
	}
	return out, nil
}

// ByPosition はセッション内位置(Index)の区間別。区間の順で、発話が無い区間も 0 で出す。どの区間にも入らない発話は数えない。
func ByPosition(items []Item, bins []Bin) []Count {
	out := make([]Count, len(bins))
	for i, b := range bins {
		out[i].Key = b.Label
	}
	for _, it := range items {
		for i, b := range bins {
			if b.contains(it.Index) {
				out[i].add(it)
			}
		}
	}
	return out
}

// Render は集計を Markdown の表にする(レビュー記録やノートにそのまま貼れる形)。
// 率は小数 1 桁の %。発話が無い行は "-"。最後に total の行を置く。
func Render(header string, rows []Count, total Count) string {
	var b strings.Builder
	fmt.Fprintf(&b, "| %s | 発話 | 訂正 | 率 |\n|---|---:|---:|---:|\n", header)
	for _, r := range rows {
		writeRow(&b, r)
	}
	writeRow(&b, total)
	return b.String()
}

// Percent は率の表示("16.7%")。発話が無ければ "-"。
func (c Count) Percent() string {
	if c.Utterances == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", c.Rate()*100)
}

func writeRow(b *strings.Builder, c Count) {
	fmt.Fprintf(b, "| %s | %d | %d | %s |\n", c.Key, c.Utterances, c.Corrections, c.Percent())
}
