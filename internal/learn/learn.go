// Package learn は「いま学ぶと良さそうなこと」の候補を、手元の材料だけから規則ベース(LLM を使わず規則と閾値だけ)で出す。
//
// 材料は関心プロファイル(語ごとの出典別の数)と、セッションの人間の発話に訂正辞書を当てた結果。
// 提案には必ず理由(どの材料の何件か)を添え、発話の本文は出力に載せない(語と件数だけ)。LLM は使わない。
//
// 「索引に無い」の判定は索引のタイトル・要旨だけで、それは本文に記録が無い証明にならない。候補を出したあと
// Verify(evidence.go)で本文を照合し、発見(位置つき)・未発見・確認不能を分けて示す。ノートの本文も出力に載せない。
//
// 候補への回答(既知・不要・後で)は feedback.go。節と語の組で保存し、Apply が次回の提示から伏せる。
package learn

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pilefort/braindex/internal/interest"
	"github.com/pilefort/braindex/internal/render"
	"github.com/pilefort/braindex/internal/retro"
	"github.com/pilefort/braindex/internal/sessions"
)

// Options は閾値と件数。ゼロ値は既定に置き換える。
type Options struct {
	MinSessions         int     // 「触れているが索引に無い」に載せる最小セッション数(既定 3)
	MaxSessionRatio     float64 // 全セッションのうちこの割合を超えて出る語は汎用語として載せない(既定 0.1)
	MinCorrections      int     // 「訂正の文脈に繰り返し出る」に載せる最小の訂正発話数(既定 2)
	BoilerplateSessions int     // 同じ冒頭の発話がこの数以上のセッションに現れたら定型(機械実行・貼り付け)として除く(既定 3)
	Top                 int     // 各節の件数(0 で全件)
}

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
	Catalog  []render.Entry      // 索引の全行。「索引に無い」は窓に関係なく全行のタイトル・要旨で見る(古いノートに書いた語は除く)。本文は Verify が見る
	Sessions []sessions.Session  // 人間の発話を読む(Window の中だけ)
	Window   retro.Window        // 訂正を数える窓
	Dicts    []*retro.Dictionary // 訂正辞書(どれかに当たれば訂正)。空なら retro.Corrections() だけ
	Options  Options
}

// Item は提案 1 件。数はどの信号かで使う欄が違う(使わない欄は 0)。
type Item struct {
	Word        string    `json:"word"`
	Sessions    int       `json:"sessions,omitempty"`    // 語が出たセッション数
	Corrections int       `json:"corrections,omitempty"` // 語が周辺に出た訂正発話数
	Keeps       int       `json:"keeps,omitempty"`       // 語を含む keep の見出し数
	Evidence    *Evidence `json:"evidence,omitempty"`    // 本文照合の結果(Verify が付ける。照合前・訂正の文脈の節は nil)
	Deferred    string    `json:"deferred,omitempty"`    // 「後で」の期限が来て再提示した候補は、その回答日(Apply が付ける)
}

// Report は提案の全体。各節は数の降順(同数は語の昇順)。
type Report struct {
	Today          string           `json:"today"`
	Days           int              `json:"days"`
	Sources        map[string]int   `json:"sources"`                // index / sessions / keep(プロファイルから) と corrections(窓内の訂正発話数)
	Unsettled      []Item           `json:"unsettled"`              // 触れているが索引に無い
	Stumbles       []Item           `json:"stumbles"`               // 訂正の文脈に繰り返し出る
	ReadNotWritten []Item           `json:"read_not_written"`       // 残した記事にあるが索引に無い
	Verification   *Verification    `json:"verification,omitempty"` // 本文照合の要約(Verify が付ける。照合前は nil)
	Feedback       *FeedbackSummary `json:"feedback,omitempty"`     // 回答の反映(Apply が付ける。回答を読まなかったときは nil)
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

	// 「索引に無い」の判定は索引の全行で見る(プロファイルの index は窓内のノートしか数えないため)
	noted := map[string]bool{}
	for _, e := range in.Catalog {
		for _, w := range interest.Words(interest.StripURLs(e.Title + " " + e.Summary)) {
			noted[w] = true
		}
	}

	// 信号 1・3: プロファイルの出典別の数から
	totalSessions := in.Profile.Sources[interest.SourceSessions]
	for _, t := range in.Profile.Terms {
		idx := t.Counts[interest.SourceIndex]
		ses := int(t.Counts[interest.SourceSessions])
		keep := int(t.Counts[interest.SourceKeep])
		if idx > 0 || noted[t.Word] {
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

	// 信号 2: 訂正発話と、その直前の人間の発話に出る語。
	// 辞書の引き金になった語そのもの(Match.Text とそれを含む語)は学ぶ対象ではないので除く。
	exclude := map[string]bool{}
	triggers := []string{}
	for _, d := range dicts {
		for _, p := range d.Patterns {
			for _, w := range interest.Words(p) {
				exclude[w] = true
				triggers = append(triggers, w)
			}
		}
	}
	isTrigger := func(w string) bool {
		if exclude[w] {
			return true
		}
		for _, t := range triggers {
			if strings.Contains(w, t) {
				return true
			}
		}
		return false
	}
	type acc struct {
		corrections int
		sessions    map[string]bool
	}
	// 定型(機械が流し込んだ指示)の印を付ける。判定は読み取り層と共有する——別々に持つと、
	// 同じログから retro と learn で違う数が出る(設計レビュー 2026-09-06 M11)。
	// 印は冪等なので、読み取り時に付いていても付け直してよい
	sessions.MarkBoilerplate(in.Sessions, o.BoilerplateSessions)
	// 1 パス目: 窓の中の訂正発話が当てた語を全部 exclude に集める。
	// 数えながら足すと、後のセッションで足された語が前のセッションでは効かず、
	// セッションの並び順で出力が変わる(設計レビュー 2026-09-06 M3c)。
	for _, s := range in.Sessions {
		for _, t := range s.HumanTurns() {
			if !in.Window.Contains(t.Time) {
				continue
			}
			ms := retro.Classify(t.Text, dicts...)
			if len(ms) == 0 {
				continue
			}
			if t.Boilerplate {
				continue // 定型は数えないので、除外語も取らない
			}
			for _, m := range ms {
				for _, w := range interest.Words(m.Text) {
					exclude[w] = true
				}
			}
		}
	}

	// 2 パス目: 数える。exclude はもう動かない
	words := map[string]*acc{}
	total, boiler := 0, 0
	for _, s := range in.Sessions {
		turns := s.HumanTurns()
		for i, t := range turns {
			if !in.Window.Contains(t.Time) || len(retro.Classify(t.Text, dicts...)) == 0 {
				continue
			}
			if t.Boilerplate {
				boiler++
				continue
			}
			total++
			seen := map[string]bool{}
			ctx := t.Text
			if i > 0 {
				ctx += "\n" + turns[i-1].Text
			}
			for _, w := range interest.Words(interest.StripURLs(ctx)) {
				if isTrigger(w) || seen[w] {
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
	r.Truncate(o.Top)
	return r
}

// Truncate は各節を先頭 n 件にする(0 で全件)。回答を伏せる(Apply)なら、その後に呼ぶ——
// 先に切ると、伏せた分だけ件数が欠けて出る。
func (r *Report) Truncate(n int) {
	r.Unsettled = top(r.Unsettled, n)
	r.Stumbles = top(r.Stumbles, n)
	r.ReadNotWritten = top(r.ReadNotWritten, n)
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
//
// 各項目は「語 — 材料の数／本文照合の結果」。前半が候補の理由、後半が本文で確認できた事実(位置はパス:行)。
// 学習上の読み(どうするか)は節の説明にだけ書き、項目行には事実しか載せない。
func (r Report) Marshal() []byte {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# 学習の提案 %s（直近 %d 日）\n\n", r.Today, r.Days)
	fmt.Fprintf(&sb, "材料: ノート %d・セッション %d・訂正 %d 発話・keep %d\n",
		r.Sources[interest.SourceIndex], r.Sources[interest.SourceSessions], r.Sources["corrections"], r.Sources[interest.SourceKeep])
	sb.WriteString(r.verificationLines())
	sb.WriteString(r.feedbackLine())
	sb.WriteString("\n")
	section := func(sec Section, reading string, items []Item, line func(Item) string) {
		fmt.Fprintf(&sb, "## %s（%d）\n\n%s\n\n", sec.Title(), len(items), reading)
		if len(items) == 0 {
			sb.WriteString("（なし）\n\n")
			return
		}
		for _, it := range items {
			fmt.Fprintf(&sb, "- %s — %s", it.Word, line(it))
			if it.Evidence != nil {
				sb.WriteString("／" + it.Evidence.text())
			}
			if it.Deferred != "" {
				fmt.Fprintf(&sb, "／後で（%s に回答）の期限が来たので再提示", it.Deferred)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
	section(SectionUnsettled,
		"会話では繰り返し出るのに、索引（タイトル・要旨）にも keep にも無い語。本文でも未発見なら理解が定着していない候補（ノートに 1 本書くか、学び直す）。本文で発見なら、ノートはあるが要旨から引けない（読み直すか、要旨に語を出す）。",
		r.Unsettled, func(it Item) string { return fmt.Sprintf("セッション %d 本", it.Sessions) })
	section(SectionStumbles, "訂正の発話とその直前の発話に出る語。つまずきの周辺にある候補。前提や使い方を確かめる。",
		r.Stumbles, func(it Item) string {
			return fmt.Sprintf("訂正 %d 発話・セッション %d 本", it.Corrections, it.Sessions)
		})
	section(SectionReadNotWritten,
		"keep した記事の見出しにあるのに、索引に無い語。本文でも未発見なら、読んで残したが自分の言葉にしていない候補。本文で発見なら、書いてはいるが要旨から引けない。",
		r.ReadNotWritten, func(it Item) string { return fmt.Sprintf("keep %d 件", it.Keeps) })
	return []byte(sb.String())
}

// verificationLines は「本文照合:」の行(と確認できなかった範囲の一覧)。
// 照合していない・語が無い・全部読めた・読めなかった範囲がある、の 4 通りを言い分ける。
func (r Report) verificationLines() string {
	v := r.Verification
	switch {
	case v == nil:
		return "本文照合: なし（索引だけの判定。「索引に無い」を「ノートに無い」と読まない）\n"
	case v.Words == 0:
		return "本文照合: 照合する語なし\n"
	case v.Complete:
		return fmt.Sprintf("本文照合: %d ファイルを読んだ・確認できなかった範囲なし\n", v.Files)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "本文照合: %d ファイルを読んだ・確認できなかった範囲 %d 件（この中にあるかは分からない）\n", v.Files, len(v.Gaps))
	for _, g := range v.Gaps {
		rel := g.Rel
		if g.Dir {
			rel += "/"
		}
		fmt.Fprintf(&b, "- %s — %s\n", rel, strings.Join(strings.Fields(g.Reason), " "))
	}
	return b.String()
}

// text は項目行に付ける本文照合の結果。事実(区分・数・位置)だけで、本文の行は載せない。
func (e Evidence) text() string {
	switch e.Status {
	case Found:
		locs := make([]string, len(e.Locations))
		for i, l := range e.Locations {
			locs[i] = fmt.Sprintf("%s:%d", l.Path, l.Line)
		}
		s := fmt.Sprintf("本文で発見（%d 行・%d ファイル）: %s", e.Lines, e.Files, strings.Join(locs, ", "))
		if e.Lines > len(e.Locations) {
			s += " ほか"
		}
		return s
	case Absent:
		return "本文でも未発見（走査した範囲は全部読めた）"
	case Unknown:
		return "確認不能（読めなかった範囲がある）"
	}
	return "未照合"
}

// JSON は構造体をそのまま出す。
func (r Report) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
