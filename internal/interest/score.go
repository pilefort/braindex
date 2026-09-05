package interest

import "sort"

// Score は記事の関心度 0〜3 と、当たった語(プロファイルの重み降順)。
type Score struct {
	Value   int      `json:"value"`
	Matched []string `json:"matched,omitempty"`
}

// MaxScore は関心度の上限。原型の LLM 採点(0〜3)と同じ尺度にして、LLM 補助(news.llm)と差し替えられるようにする。
const MaxScore = 3

// Rater はプロファイルの重み表を 1 回作って使い回す採点器。記事ごとに Rate(p, text) を呼ぶと
// 呼ぶたびに map を作り直す(記事数 × 語数)ので、まとめて採点する側はこれを使う。
type Rater struct {
	weights map[string]float64
	max     float64 // プロファイルの最大重み(Terms は Weight 降順なので先頭)
}

// NewRater は p から採点器を作る。p が空なら Rate は常に 0 を返す(呼び出し側は採点無しとして扱う)。
func NewRater(p Profile) Rater {
	r := Rater{weights: make(map[string]float64, len(p.Terms))}
	for _, t := range p.Terms {
		r.weights[t.Word] = t.Weight
	}
	if len(p.Terms) > 0 {
		r.max = p.Terms[0].Weight
	}
	return r
}

// Empty はプロファイルが空(採点できない)なら true。
func (r Rater) Empty() bool { return len(r.weights) == 0 }

// Rate は text(見出し＋概要)の語とプロファイルの重みを照合して関心度を付ける。
//
// 当たった語の重みの和 raw を、プロファイルの最大重み M で割った q で段階にする:
// q = 0 → 0(当たり無し)、q < 0.5 → 1、q < 1.5 → 2、それ以上 → 3。
// 「プロファイルの一番強い語 1 つ分」を 2 の境界にするので、最強の語が 1 つ当たれば主要表示(show_min_score 既定 2)に入る。
func (r Rater) Rate(text string) Score {
	if r.Empty() {
		return Score{}
	}
	var s Score
	raw := 0.0
	for _, w := range Words(text) {
		if wt, ok := r.weights[w]; ok {
			raw += wt
			s.Matched = append(s.Matched, w)
		}
	}
	if raw == 0 || r.max <= 0 {
		return s
	}
	sort.SliceStable(s.Matched, func(i, j int) bool { return r.weights[s.Matched[i]] > r.weights[s.Matched[j]] })
	switch q := raw / r.max; {
	case q < 0.5:
		s.Value = 1
	case q < 1.5:
		s.Value = 2
	default:
		s.Value = MaxScore
	}
	return s
}

// Rate は 1 件だけ採点する(NewRater(p).Rate(text) と同じ)。まとめて採点するなら Rater を使う。
func Rate(p Profile, text string) Score {
	return NewRater(p).Rate(text)
}
