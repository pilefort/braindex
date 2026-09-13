package notetype

import (
	"regexp"
	"strings"
)

type Suggestion struct {
	Path      string   `json:"path"`
	Title     string   `json:"title"`
	Candidate string   `json:"candidate"`
	Axes      []string `json:"axes"`
	Terms     []string `json:"terms"`
}

var vocabulary = []struct {
	axis string
	re   *regexp.Regexp
}{
	{Failure, regexp.MustCompile(`(?i)失敗|事故|バグ|不具合|落とし穴|ハマ|障害|インシデント|原因|誤|pitfall|bug|incident|postmortem|トラブル|罠|注意点`)},
	{Howto, regexp.MustCompile(`(?i)手順|やり方|方法|使い方|セットアップ|導入|運用|ガイド|how ?to|howto|setup|guide|チートシート|レシピ|作り方|ワークフロー|手引`)},
	{Observation, regexp.MustCompile(`(?i)実測|調査|観測|計測|分析|検証|測定|比較|ベンチ|survey|research|analysis|評価|棚卸|レビュー|リサーチ`)},
}

// Suggest はタイトルとファイル名だけで候補を作る。軸の順は語表の順で固定する。
func Suggest(path, title, filename string) Suggestion {
	s := Suggestion{Path: path, Title: title, Axes: []string{}, Terms: []string{}}
	seen := map[string]bool{}
	for _, v := range vocabulary {
		terms := v.re.FindAllString(title+"\n"+filename, -1)
		if len(terms) == 0 {
			continue
		}
		s.Axes = append(s.Axes, v.axis)
		for _, term := range terms {
			key := strings.ToLower(term)
			if !seen[key] {
				seen[key] = true
				s.Terms = append(s.Terms, term)
			}
		}
	}
	if len(s.Axes) == 1 {
		s.Candidate = s.Axes[0]
	}
	return s
}
