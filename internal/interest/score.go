package interest

import "sort"

// Score は記事の関心度 0〜3 と、当たった語(プロファイルの重み降順)。
type Score struct {
	Value   int      `json:"value"`
	Matched []string `json:"matched,omitempty"`
}

// MaxScore は関心度の上限。原型の LLM 採点(0〜3)と同じ尺度にして、後続の LLM 補助と差し替えられるようにする。
const MaxScore = 3

// Rate は text(見出し＋概要)の語とプロファイルの重みを照合して関心度を付ける。
//
// 当たった語の重みの和 raw を、プロファイルの最大重み M で割った r で段階にする:
// r = 0 → 0(当たり無し)、r < 0.5 → 1、r < 1.5 → 2、それ以上 → 3。
// 「プロファイルの一番強い語 1 つ分」を 2 の境界にするので、最強の語が 1 つ当たれば主要表示(show_min_score 既定 2)に入る。
// プロファイルが空なら常に 0(呼び出し側は採点無しとして扱う)。
func Rate(p Profile, text string) Score {
	if len(p.Terms) == 0 {
		return Score{}
	}
	weights := make(map[string]float64, len(p.Terms))
	for _, t := range p.Terms {
		weights[t.Word] = t.Weight
	}
	max := p.Terms[0].Weight // Terms は Weight 降順
	var s Score
	raw := 0.0
	for _, w := range Words(text) {
		if wt, ok := weights[w]; ok {
			raw += wt
			s.Matched = append(s.Matched, w)
		}
	}
	if raw == 0 || max <= 0 {
		return s
	}
	sort.SliceStable(s.Matched, func(i, j int) bool { return weights[s.Matched[i]] > weights[s.Matched[j]] })
	switch r := raw / max; {
	case r < 0.5:
		s.Value = 1
	case r < 1.5:
		s.Value = 2
	default:
		s.Value = MaxScore
	}
	return s
}
