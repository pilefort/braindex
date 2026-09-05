// Package learn は「いま学ぶと良さそうなこと」の候補を、手元の材料だけから決定論で出す。
//
// 材料は関心プロファイル(語ごとの出典別の数)と、セッションの人間の発話に訂正辞書を当てた結果。
// 提案には必ず理由(どの材料の何件か)を添え、発話の本文は出力に載せない(語と件数だけ)。LLM は使わない。
package learn

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pilefort/braindex/internal/interest"
	"github.com/pilefort/braindex/internal/retro"
	"github.com/pilefort/braindex/internal/sessions"
)

// Options は閾値と件数。ゼロ値は既定に置き換える。
type Options struct {
	MinSessions         int     // 「触れているがノートに無い」に載せる最小セッション数(既定 3)
	MaxSessionRatio     float64 // 全セッションのうちこの割合を超えて出る語は汎用語として載せない(既定 0.1)
	MinCorrections      int     // 「訂正の文脈に繰り返し出る」に載せる最小の訂正発話数(既定 2)
	BoilerplateSessions int     // 同じ冒頭の発話がこの数以上のセッションに現れたら定型(機械実行・貼り付け)として除く(既定 3)
	Top                 int     // 各節の件数(0 で全件)
}

// boilerplatePrefixRunes は定型の判定に使う冒頭の長さ(文字)。空白は 1 つに畳んでから切る。
const boilerplatePrefixRunes = 120

func (o Options) withDefaults() Options {
	if o.MinSessions <= 0 {
		o.MinSessions = 3
	}
	if o.MaxSessionRatio <= 0 {
		o.MaxSessionRatio = 0.1
	}
	if o.MinCorrections <= 0 {
		o.MinCorrections = 2
	}
	if o.BoilerplateSessions <= 0 {
		o.BoilerplateSessions = 3
	}
	if o.Top < 0 {
		o.Top = 0
	}
	return o
}

// Input は Build の材料。
type Input struct {
	Profile  interest.Profile    // 関心プロファイル(Term.Counts の出典別の数を使う)
	Sessions []sessions.Session  // 人間の発話を読む(Window の中だけ)
	Window   retro.Window        // 訂正を数える窓
	Dicts    []*retro.Dictionary // 訂正辞書(どれかに当たれば訂正)。空なら retro.Corrections() だけ
	Options  Options
}

// Item は提案 1 件。数はどの信号かで使う欄が違う(使わない欄は 0)。
type Item struct {
	Word        string `json:"word"`
	Sessions    int    `json:"sessions,omitempty"`    // 語が出たセッション数
	Corrections int    `json:"corrections,omitempty"` // 語が周辺に出た訂正発話数
	Keeps       int    `json:"keeps,omitempty"`       // 語を含む keep の見出し数
}

// Report は提案の全体。各節は数の降順(同数は語の昇順)。
type Report struct {
	Today          string         `json:"today"`
	Days           int            `json:"days"`
	Sources        map[string]int `json:"sources"`          // index / sessions / keep(プロファイルから) と corrections(窓内の訂正発話数)
	Unsettled      []Item         `json:"unsettled"`        // 触れているがノートに無い
	Stumbles       []Item         `json:"stumbles"`         // 訂正の文脈に繰り返し出る
	ReadNotWritten []Item         `json:"read_not_written"` // 残した記事にあるがノートに無い
}

// Build は材料から提案を作る。同じ材料からは同じ結果になる。
func Build(in Input) Report {
	o := in.Options.withDefaults()
	dicts := in.Dicts
	if len(dicts) == 0 {
		dicts = []*retro.Dictionary{retro.Corrections()}
	}
	r := Report{Today: in.Profile.Today, Days: in.Profile.Days, Sources: map[string]int{}}
	for _, k := range []string{interest.SourceIndex, interest.SourceSessions, interest.SourceKeep} {
		r.Sources[k] = in.Profile.Sources[k]
	}

	// 信号 1・3: プロファイルの出典別の数から
	totalSessions := in.Profile.Sources[interest.SourceSessions]
	for _, t := range in.Profile.Terms {
		idx := t.Counts[interest.SourceIndex]
		ses := int(t.Counts[interest.SourceSessions])
		keep := int(t.Counts[interest.SourceKeep])
		if idx > 0 {
			continue
		}
		generic := totalSessions > 0 && float64(ses)/float64(totalSessions) > o.MaxSessionRatio
		if ses >= o.MinSessions && keep == 0 && !generic {
			r.Unsettled = append(r.Unsettled, Item{Word: t.Word, Sessions: ses})
		}
		if keep >= 1 {
			r.ReadNotWritten = append(r.ReadNotWritten, Item{Word: t.Word, Keeps: keep})
		}
	}

	// 信号 2: 訂正発話と、その直前の人間の発話に出る語
	exclude := map[string]bool{}
	for _, d := range dicts {
		for _, p := range d.Patterns {
			for _, w := range interest.Words(p) {
				exclude[w] = true
			}
		}
	}
	type acc struct {
		corrections int
		sessions    map[string]bool
	}
	// 定型の検出: 同じ冒頭の発話が何セッションに現れるか(窓の中だけ)
	prefixSessions := map[string]map[string]bool{}
	for _, s := range in.Sessions {
		for _, t := range s.HumanTurns() {
			if !in.Window.Contains(t.Time) {
				continue
			}
			k := prefix(t.Text)
			if prefixSessions[k] == nil {
				prefixSessions[k] = map[string]bool{}
			}
			prefixSessions[k][s.ID] = true
		}
	}
	words := map[string]*acc{}
	total, boiler := 0, 0
	for _, s := range in.Sessions {
		turns := s.HumanTurns()
		for i, t := range turns {
			if !in.Window.Contains(t.Time) || len(retro.Classify(t.Text, dicts...)) == 0 {
				continue
			}
			if len(prefixSessions[prefix(t.Text)]) >= o.BoilerplateSessions {
				boiler++
				continue
			}
			total++
			seen := map[string]bool{}
			ctx := t.Text
			if i > 0 {
				ctx += "\n" + turns[i-1].Text
			}
			for _, w := range interest.Words(ctx) {
				if exclude[w] || seen[w] {
					continue
				}
				seen[w] = true
				a := words[w]
				if a == nil {
					a = &acc{sessions: map[string]bool{}}
					words[w] = a
				}
				a.corrections++
				a.sessions[s.ID] = true
			}
		}
	}
	r.Sources["corrections"] = total
	r.Sources["boilerplate"] = boiler
	for w, a := range words {
		if a.corrections >= o.MinCorrections {
			r.Stumbles = append(r.Stumbles, Item{Word: w, Corrections: a.corrections, Sessions: len(a.sessions)})
		}
	}

	sortItems(r.Unsettled, func(x Item) int { return x.Sessions })
	sortItems(r.Stumbles, func(x Item) int { return x.Corrections })
	sortItems(r.ReadNotWritten, func(x Item) int { return x.Keeps })
	r.Unsettled = top(r.Unsettled, o.Top)
	r.Stumbles = top(r.Stumbles, o.Top)
	r.ReadNotWritten = top(r.ReadNotWritten, o.Top)
	return r
}

// prefix は定型の判定に使う鍵。空白を 1 つに畳み、先頭 boilerplatePrefixRunes 文字で切る。
func prefix(text string) string {
	t := strings.Join(strings.Fields(text), " ")
	rs := []rune(t)
	if len(rs) > boilerplatePrefixRunes {
		rs = rs[:boilerplatePrefixRunes]
	}
	return string(rs)
}

func sortItems(xs []Item, key func(Item) int) {
	sort.SliceStable(xs, func(i, j int) bool {
		if key(xs[i]) != key(xs[j]) {
			return key(xs[i]) > key(xs[j])
		}
		return xs[i].Word < xs[j].Word
	})
}

func top(xs []Item, n int) []Item {
	if n > 0 && len(xs) > n {
		return xs[:n]
	}
	return xs
}

// Marshal は Markdown にする。
func (r Report) Marshal() []byte {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# 学習の提案 %s（直近 %d 日）\n\n", r.Today, r.Days)
	fmt.Fprintf(&sb, "材料: ノート %d・セッション %d・訂正 %d 発話・keep %d\n\n",
		r.Sources[interest.SourceIndex], r.Sources[interest.SourceSessions], r.Sources["corrections"], r.Sources[interest.SourceKeep])
	section := func(title, reading string, items []Item, line func(Item) string) {
		fmt.Fprintf(&sb, "## %s（%d）\n\n%s\n\n", title, len(items), reading)
		if len(items) == 0 {
			sb.WriteString("（なし）\n\n")
			return
		}
		for _, it := range items {
			fmt.Fprintf(&sb, "- %s — %s\n", it.Word, line(it))
		}
		sb.WriteString("\n")
	}
	section("触れているがノートに無い", "会話では繰り返し出るのに、索引にも keep にも無い語。理解が定着していない候補。ノートに 1 本書くか、学び直す。",
		r.Unsettled, func(it Item) string { return fmt.Sprintf("セッション %d 本", it.Sessions) })
	section("訂正の文脈に繰り返し出る", "訂正の発話とその直前の発話に出る語。つまずきの周辺にある候補。前提や使い方を確かめる。",
		r.Stumbles, func(it Item) string {
			return fmt.Sprintf("訂正 %d 発話・セッション %d 本", it.Corrections, it.Sessions)
		})
	section("残した記事にあるがノートに無い", "keep した記事の見出しにあるのに、索引に無い語。読んで残したが自分の言葉にしていない候補。",
		r.ReadNotWritten, func(it Item) string { return fmt.Sprintf("keep %d 件", it.Keeps) })
	return []byte(sb.String())
}

// JSON は構造体をそのまま出す。
func (r Report) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
