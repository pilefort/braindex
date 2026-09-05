package news

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pilefort/braindex/internal/interest"
)

// catalogJSON は同梱の取材先目録(catalog.json)。docs/notes/project/news-feed-catalog-2026-09-05.md の 98 本を JSON にしたもの。
// tags はノートの分類そのまま(表示用)。keywords は照合語で、関心プロファイルの語(interest.Words の規則: ラテン文字は小文字・
// 3 文字以上・ストップワード除外)と完全一致で照合する。照合語は Claude が付けた推測で、記事の内容を実測して付けたものではない
// (決定 2026-09-05)。外れは試用で直す。
//
//go:embed catalog.json
var catalogJSON []byte

// CatalogEntry は取材先目録の 1 件。
type CatalogEntry struct {
	Genre    string   `json:"genre"`    // ジャンル(ノートの ### 見出し)
	Name     string   `json:"name"`     // 取材先の表示名。目録の中で一意
	URL      string   `json:"url"`      // フィードの URL(https)。目録の中で一意
	Tags     []string `json:"tags"`     // 分類(表示用)
	Keywords []string `json:"keywords"` // 照合語(関心プロファイルの語と完全一致で照合)
}

// Catalog は同梱の取材先目録を返す(目録の順)。壊れていれば panic(ビルドに同梱した静的データなので、テストで検査する)。
func Catalog() []CatalogEntry {
	es, err := parseCatalog(catalogJSON)
	if err != nil {
		panic("news: 同梱の目録が壊れている: " + err.Error())
	}
	return es
}

// parseCatalog は目録の JSON を読む。name・url の重複、http(s) でない url、照合語の無い行、照合語が語の規則を通らない行はエラー。
func parseCatalog(b []byte) ([]CatalogEntry, error) {
	var es []CatalogEntry
	if err := json.Unmarshal(b, &es); err != nil {
		return nil, err
	}
	names, urls := map[string]bool{}, map[string]bool{}
	for i, e := range es {
		if e.Name == "" || names[e.Name] {
			return nil, fmt.Errorf("%d 番目: name が空か重複: %q", i+1, e.Name)
		}
		if !strings.HasPrefix(e.URL, "https://") && !strings.HasPrefix(e.URL, "http://") {
			return nil, fmt.Errorf("%s: url が http(s) でない: %q", e.Name, e.URL)
		}
		if urls[normalizeURL(e.URL)] {
			return nil, fmt.Errorf("%s: url が重複: %q", e.Name, e.URL)
		}
		if len(e.Keywords) == 0 {
			return nil, fmt.Errorf("%s: keywords が無い", e.Name)
		}
		for _, k := range e.Keywords {
			// 語の規則を通らない照合語は決して当たらない(プロファイルに現れない)ので、書き損じとして弾く
			if ws := interest.Words(k); len(ws) != 1 || ws[0] != k {
				return nil, fmt.Errorf("%s: 照合語 %q は語の規則(小文字・3 文字以上・ストップワード以外)を通らない", e.Name, k)
			}
		}
		names[e.Name], urls[normalizeURL(e.URL)] = true, true
	}
	return es, nil
}

// normalizeURL は feeds.json と目録の URL を比べるための正規化(末尾スラッシュを落とす)。
func normalizeURL(u string) string { return strings.TrimRight(strings.TrimSpace(u), "/") }

// Suggestion は関心プロファイルに当たった取材先 1 件。
type Suggestion struct {
	CatalogEntry
	Score   float64  `json:"score"`   // 当たった語の重み(Term.Weight)の和
	Matched []string `json:"matched"` // 当たった語(重みの降順・同点は語の昇順)
}

// Suggest は目録のうち、プロファイルの語に照合語が当たり、かつ feeds に登録されていない取材先を点の降順で返す。
// top > 0 ならその件数まで。同じ材料からは同じ並びになる(点の降順・同点は name の昇順)。
func Suggest(catalog []CatalogEntry, p interest.Profile, feeds []Source, top int) []Suggestion {
	registered := map[string]bool{}
	for _, f := range feeds {
		registered[normalizeURL(f.URL)] = true
	}
	weight := map[string]float64{}
	for _, t := range p.Terms {
		weight[t.Word] = t.Weight
	}
	var out []Suggestion
	for _, e := range catalog {
		if registered[normalizeURL(e.URL)] {
			continue
		}
		s := Suggestion{CatalogEntry: e}
		for _, k := range e.Keywords {
			if w, ok := weight[k]; ok && w > 0 {
				s.Score += w
				s.Matched = append(s.Matched, k)
			}
		}
		if len(s.Matched) == 0 {
			continue
		}
		sort.SliceStable(s.Matched, func(i, j int) bool {
			wi, wj := weight[s.Matched[i]], weight[s.Matched[j]]
			if wi != wj {
				return wi > wj
			}
			return s.Matched[i] < s.Matched[j]
		})
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Name < out[j].Name
	})
	if top > 0 && len(out) > top {
		out = out[:top]
	}
	return out
}

// SuggestReport は braindex news suggest の出力。
type SuggestReport struct {
	Today       string       `json:"today"`
	Days        int          `json:"days"`
	Terms       int          `json:"terms"`       // プロファイルの語数
	CatalogSize int          `json:"catalog"`     // 目録の本数
	Registered  int          `json:"registered"`  // 目録のうち feeds.json に登録済みで除外した本数
	Suggestions []Suggestion `json:"suggestions"` // 候補(点の降順)
}

// BuildSuggestReport はプロファイル・目録・登録済みフィードから報告を作る。
func BuildSuggestReport(catalog []CatalogEntry, p interest.Profile, feeds []Source, top int) SuggestReport {
	registered := map[string]bool{}
	for _, f := range feeds {
		registered[normalizeURL(f.URL)] = true
	}
	n := 0
	for _, e := range catalog {
		if registered[normalizeURL(e.URL)] {
			n++
		}
	}
	return SuggestReport{
		Today: p.Today, Days: p.Days, Terms: len(p.Terms), CatalogSize: len(catalog), Registered: n,
		Suggestions: Suggest(catalog, p, feeds, top),
	}
}

// Marshal は Markdown。1 候補 1 行。発話の本文は載せない(語と数だけ)。
func (r SuggestReport) Marshal() []byte {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# 取材先の候補(%s・直近 %d 日)\n\n", r.Today, r.Days)
	fmt.Fprintf(&sb, "材料: 関心語 %d・目録 %d 本(登録済み %d 本を除外)\n\n", r.Terms, r.CatalogSize, r.Registered)
	if len(r.Suggestions) == 0 {
		sb.WriteString("当たる取材先なし(関心語に照合語が当たらない。`braindex news profile` で語を確かめる)\n")
		return []byte(sb.String())
	}
	for i, s := range r.Suggestions {
		fmt.Fprintf(&sb, "%d. **%s** — %s — 当たった語: %s — %s\n", i+1, s.Name, s.Genre, strings.Join(s.Matched, ", "), s.URL)
	}
	sb.WriteString("\n登録するには news/feeds.json に `{\"name\": \"…\", \"url\": \"…\"}` を足す。\n")
	return []byte(sb.String())
}

// JSON は構造体をそのまま。
func (r SuggestReport) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
