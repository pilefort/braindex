package interest

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pilefort/braindex/internal/render"
	"github.com/pilefort/braindex/internal/sessions"
)

// 出典の名前。表示順もこの順。
const (
	SourceIndex    = "index"    // 索引の直近差分(ノートのタイトル・種別・リポ名)
	SourceSessions = "sessions" // 直近のセッション内容
	SourceKeep     = "keep"     // 選別で残したニュースの見出し
	SourceExtra    = "extra"    // 補助の関心ファイル(1 行 1 語)
)

var sourceOrder = []string{SourceIndex, SourceSessions, SourceKeep, SourceExtra}

// Term は関心語 1 つ。
type Term struct {
	Word   string             `json:"word"`
	Weight float64            `json:"weight"` // 出典ごとに最大を 1 に正規化した値の和(0 < Weight <= 出典数)
	Counts map[string]float64 `json:"counts"` // 出典 → 生の数(index: 新しさで重み付けした件数、sessions: セッション数、keep: 見出し数、extra: 1)
}

// Profile は関心プロファイル。Terms は Weight 降順(同点は語の昇順)。
type Profile struct {
	Today   string         `json:"today"`
	Days    int            `json:"days"`
	Sources map[string]int `json:"sources"` // 出典 → 材料の数(index: 窓内のノート数、sessions: 窓内のセッション数、keep: 見出し数、extra: 補助ファイルの行数)
	Terms   []Term         `json:"terms"`
}

// Weight は語の重み。無ければ 0。
func (p Profile) Weight(word string) float64 {
	for _, t := range p.Terms {
		if t.Word == word {
			return t.Weight
		}
	}
	return 0
}

// Input は Build の材料。日付の窓による絞り込みは Build が行う。
type Input struct {
	Today      string             // YYYY-MM-DD
	Days       int                // 直近何日を窓にするか(index と sessions)。keep は KeepMonths か月
	KeepMonths int                // keep 履歴を遡る月数。0 なら 3
	Catalog    []render.Entry     // 索引の全行(Date が窓の外なら使わない)
	Sessions   []sessions.Session // セッション(Turn.Time が窓の外の発話は使わない)
	Keeps      []Keep             // keep 履歴の見出し
	Extra      []string           // 補助ファイルの行
}

// Keep は keep 履歴の見出し 1 件。Month はファイル名の YYYY-MM。
type Keep struct {
	Month string
	Title string
}

// Build は材料から関心プロファイルを作る。同じ材料からは同じ結果になる。
//
// 重み: 出典ごとに語の生の数を数え、出典内の最大値で割って 0〜1 にし、語ごとに出典の値を足す。
// 出典間で数の桁が違う(セッションは数百、索引は数十)ので、そのまま足すとセッションだけで決まってしまう。
// index の生の数は「新しいほど高い」係数(窓の端で 1、今日で 2)を件数に掛けたもの。
func Build(in Input) (Profile, error) {
	today, err := time.Parse("2006-01-02", in.Today)
	if err != nil {
		return Profile{}, fmt.Errorf("today は YYYY-MM-DD: %q", in.Today)
	}
	days := in.Days
	if days <= 0 {
		days = 14
	}
	months := in.KeepMonths
	if months <= 0 {
		months = 3
	}
	since := today.AddDate(0, 0, -days)
	sinceDate := since.Format("2006-01-02")
	keepSince := today.AddDate(0, -months, 0).Format("2006-01")

	counts := map[string]map[string]float64{} // 出典 → 語 → 生の数
	for _, s := range sourceOrder {
		counts[s] = map[string]float64{}
	}
	p := Profile{Today: in.Today, Days: days, Sources: map[string]int{}}

	// index: 窓内のノート。語は タイトル・種別・リポ名 から。新しいほど係数が高い
	for _, e := range in.Catalog {
		if e.Date < sinceDate || e.Date > in.Today {
			continue
		}
		d, err := time.Parse("2006-01-02", e.Date)
		if err != nil {
			continue
		}
		age := today.Sub(d).Hours() / 24
		coef := 1 + (float64(days)-age)/float64(days) // 1〜2
		p.Sources[SourceIndex]++
		for _, w := range Words(strings.Join([]string{e.Title, e.Kind, e.Repo}, " ")) {
			counts[SourceIndex][w] += coef
		}
	}

	// sessions: 窓内の発話があるセッション。語は 1 セッションにつき 1 回(出現セッション数)
	for _, s := range in.Sessions {
		var sb strings.Builder
		for _, t := range s.Turns {
			if t.Time.IsZero() || t.Time.Before(since) || t.Time.After(today.AddDate(0, 0, 1)) {
				continue
			}
			sb.WriteString(t.Text)
			sb.WriteString("\n")
		}
		if sb.Len() == 0 {
			continue
		}
		p.Sources[SourceSessions]++
		for _, w := range Words(StripURLs(sb.String())) {
			counts[SourceSessions][w]++
		}
	}

	// keep: 直近 months か月の見出し。語は見出し数
	for _, k := range in.Keeps {
		if k.Month < keepSince {
			continue
		}
		p.Sources[SourceKeep]++
		for _, w := range Words(k.Title) {
			counts[SourceKeep][w]++
		}
	}

	// extra: 1 行 1 語(句)。空行と # 始まりは読まない。行を語の規則に通す(記事の側も同じ規則で語にするので、
	// 規則を通らない書き方だと照合できない)。規則で語にならない行は小文字に畳んでそのまま語にする
	for _, line := range in.Extra {
		line = strings.TrimSpace(strings.TrimPrefix(line, bom))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ws := Words(line)
		if len(ws) == 0 {
			ws = []string{strings.ToLower(line)}
		}
		p.Sources[SourceExtra]++
		for _, w := range ws {
			counts[SourceExtra][w] = 1
		}
	}

	// 正規化して合算
	terms := map[string]*Term{}
	for _, s := range sourceOrder {
		max := 0.0
		for _, c := range counts[s] {
			if c > max {
				max = c
			}
		}
		for w, c := range counts[s] {
			t := terms[w]
			if t == nil {
				t = &Term{Word: w, Counts: map[string]float64{}}
				terms[w] = t
			}
			t.Counts[s] = c
			t.Weight += c / max
		}
	}
	for _, t := range terms {
		t.Weight = round3(t.Weight)
		for s, c := range t.Counts {
			t.Counts[s] = round3(c)
		}
		p.Terms = append(p.Terms, *t)
	}
	sort.Slice(p.Terms, func(i, j int) bool {
		a, b := p.Terms[i], p.Terms[j]
		if a.Weight != b.Weight {
			return a.Weight > b.Weight
		}
		return a.Word < b.Word
	})
	return p, nil
}

func round3(f float64) float64 {
	return float64(int64(f*1000+0.5)) / 1000
}

// Marshal は人が読む表(Markdown・LF)。top 件まで。top <= 0 なら全件。
func (p Profile) Marshal(top int) []byte {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# 関心プロファイル %s（直近 %d 日）\n\n", p.Today, p.Days)
	fmt.Fprintf(&sb, "材料: ノート %d・セッション %d・keep %d・補助 %d ／ 語 %d\n\n",
		p.Sources[SourceIndex], p.Sources[SourceSessions], p.Sources[SourceKeep], p.Sources[SourceExtra], len(p.Terms))
	sb.WriteString("| 語 | 重み | index | sessions | keep | extra |\n|---|---:|---:|---:|---:|---:|\n")
	for i, t := range p.Terms {
		if top > 0 && i >= top {
			fmt.Fprintf(&sb, "\n（上位 %d 語。残り %d 語は -top で増やす）\n", top, len(p.Terms)-top)
			break
		}
		fmt.Fprintf(&sb, "| %s | %.3f |", t.Word, t.Weight)
		for _, s := range sourceOrder {
			if c, ok := t.Counts[s]; ok {
				fmt.Fprintf(&sb, " %s |", trimFloat(c))
			} else {
				sb.WriteString(" |")
			}
		}
		sb.WriteString("\n")
	}
	return []byte(sb.String())
}

// JSON は機械向け(整形済み・LF)。
func (p Profile) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func trimFloat(f float64) string {
	s := fmt.Sprintf("%.3f", f)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return s
}

var keepLine = regexp.MustCompile(`^- \[(.+?)\]\(\S*\)`)

// bom は UTF-8 の BOM。Windows の編集で付くことがあり、付いたままだと 1 行目の解析が外れる(決定 2026-08-07)。
const bom = "\uFEFF"

// ParseKeep は keep ファイル(news/keep/YYYY-MM.md)の本文から見出しを取る。行の形は `- [見出し](リンク)`(原型と同じ)。
func ParseKeep(month string, text string) []Keep {
	text = strings.TrimPrefix(text, bom)
	var out []Keep
	for _, line := range strings.Split(text, "\n") {
		if m := keepLine.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
			out = append(out, Keep{Month: month, Title: m[1]})
		}
	}
	return out
}
