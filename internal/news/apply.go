package news

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Selection は HTML の「選別を書き出す」が出す JSON。
type Selection struct {
	Type       string               `json:"type"` // SelectionType
	Date       string               `json:"date"`
	Layer      string               `json:"layer"`
	ExportedAt string               `json:"exported_at"`
	Keeps      []Keep               `json:"keeps"`
	FeedStats  map[string]FeedStats `json:"feed_stats"`
}

// Keep は「残す」にした記事。
type Keep struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Link     string `json:"link"`
	Feed     string `json:"feed"`
	Category string `json:"category,omitempty"`
	Score    string `json:"score,omitempty"`   // 関心度(文字列。採点なしは空)
	Rescued  bool   `json:"rescued,omitempty"` // 「関心外と判定」から残した(採点の見逃し)
}

// FeedStats はダイジェスト 1 回のフィード別の数。shown/hidden は主要／折りたたみに出した数、
// kept/dropped は主要から残した／不要にした数、rescued は折りたたみから残した数。
type FeedStats struct {
	Shown   int `json:"shown"`
	Kept    int `json:"kept"`
	Dropped int `json:"dropped"`
	Hidden  int `json:"hidden"`
	Rescued int `json:"rescued"`
}

func (a FeedStats) add(b FeedStats) FeedStats {
	return FeedStats{a.Shown + b.Shown, a.Kept + b.Kept, a.Dropped + b.Dropped, a.Hidden + b.Hidden, a.Rescued + b.Rescued}
}

// ErrNotSelection は JSON が選別ファイルでないとき(type が違う・JSON でない)。
var ErrNotSelection = errors.New("選別 JSON でない")

// ParseSelection は選別 JSON を読む。type が SelectionType でなければ ErrNotSelection。
func ParseSelection(b []byte) (Selection, error) {
	var s Selection
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("%w: %v", ErrNotSelection, err)
	}
	if s.Type != SelectionType {
		return s, fmt.Errorf("%w: type=%q", ErrNotSelection, s.Type)
	}
	if s.FeedStats == nil {
		s.FeedStats = map[string]FeedStats{}
	}
	return s, nil
}

// Stats はダイジェスト単位の統計スナップショット(news/.stats.json)。キーは "<日付>_<層>"。
// 同じダイジェストの再書き出しは上書きになり、二重に数えない。
type Stats struct {
	Digests map[string]map[string]FeedStats `json:"digests"`
}

// StatsFile は Dir の下の統計ファイル。git 管理外。
const StatsFile = ".stats.json"

// LoadStats は統計ファイルを読む。無ければ空。
func LoadStats(path string) (Stats, error) {
	st := Stats{Digests: map[string]map[string]FeedStats{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return st, nil
		}
		return st, fmt.Errorf("統計ファイルを読めない: %w", err)
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return st, fmt.Errorf("統計ファイル %s: %w", path, err)
	}
	if st.Digests == nil {
		st.Digests = map[string]map[string]FeedStats{}
	}
	return st, nil
}

// Save は統計ファイルを書く(キー順で整形。同じ内容なら同じバイト列)。
func (st Stats) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(st); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// Totals はスナップショットをフィード別に合計する。
func (st Stats) Totals() map[string]FeedStats {
	tot := map[string]FeedStats{}
	for _, snap := range st.Digests {
		for f, d := range snap {
			tot[f] = tot[f].add(d)
		}
	}
	return tot
}

// PruneMinShown は間引き候補にするのに必要な「見た数」(主要＋折りたたみ)。
const PruneMinShown = 20

// PruneCandidates は見た数が minShown 以上なのに、主要からも折りたたみからも一度も残していないフィード(名前昇順)。
func PruneCandidates(totals map[string]FeedStats, minShown int) []string {
	var out []string
	for f, d := range totals {
		if d.Shown+d.Hidden >= minShown && d.Kept+d.Rescued == 0 {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// KeepMarkdown は keep ファイル(news/keep/YYYY-MM.md)に追記する節。行の形 `- [見出し](リンク) — フィード` は
// interest.ParseKeep が読む(関心プロファイルの出典 3)。
func KeepMarkdown(keeps []Keep, date, layer string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "\n## %s（%s）\n\n", date, layer)
	for _, k := range keeps {
		title := k.Title
		if title == "" {
			title = "(無題)"
		}
		fmt.Fprintf(&sb, "- [%s](%s) — %s", escapeTitle(title), k.Link, k.Feed)
		if k.Rescued {
			sb.WriteString("（関心外から救済）")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// IngestedDir は Dir の下。取り込み済みの選別 JSON を移す先。git 管理外。
const IngestedDir = ".ingested"

// Ingest は dirs にある選別 JSON(SelectionPrefix*.json)を名前順に取り込み、keep に追記し、統計を上書きし、
// ファイルを newsDir/.ingested/ へ移す。取り込んだ件数分のメッセージを返す。形式が違うファイルは飛ばして伝える。
// 同じ記事(リンク)が keep ファイルに既にあれば追記しない。統計はダイジェスト("<日付>_<層>")単位で上書き。
func Ingest(newsDir string, dirs []string) (msgs []string, err error) {
	var paths []string
	for _, d := range dirs {
		m, _ := filepath.Glob(filepath.Join(d, SelectionPrefix+"*.json"))
		sort.Strings(m) // 名前に時刻が入るので昇順 = 時刻順。後勝ちで最新が残る
		paths = append(paths, m...)
	}
	if len(paths) == 0 {
		return nil, nil
	}
	statsPath := filepath.Join(newsDir, StatsFile)
	st, err := LoadStats(statsPath)
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return msgs, err
		}
		sel, err := ParseSelection(b)
		if err != nil {
			msgs = append(msgs, fmt.Sprintf("飛ばした(%v): %s", err, filepath.Base(p)))
			continue
		}
		date, layer := sel.Date, sel.Layer
		if date == "" {
			date = "unknown"
		}
		if layer == "" {
			layer = "unknown"
		}
		if len(sel.Keeps) > 0 {
			month := date
			if len(month) >= 7 {
				month = month[:7]
			}
			if err := appendKeeps(filepath.Join(newsDir, KeepDir, month+".md"), month, sel.Keeps, date, layer); err != nil {
				return msgs, err
			}
		}
		st.Digests[date+"_"+layer] = sel.FeedStats
		ingested := filepath.Join(newsDir, IngestedDir)
		if err := os.MkdirAll(ingested, 0o755); err != nil {
			return msgs, err
		}
		if err := moveFile(p, filepath.Join(ingested, filepath.Base(p))); err != nil {
			return msgs, fmt.Errorf("取り込み済みへ移せない: %w", err)
		}
		msgs = append(msgs, fmt.Sprintf("取り込み: %s（残す %d 件）", filepath.Base(p), len(sel.Keeps)))
	}
	if err := st.Save(statsPath); err != nil {
		return msgs, err
	}
	return msgs, nil
}

// osRename はテストで差し替える(ドライブをまたぐ失敗を再現するため)。
var osRename = os.Rename

// moveFile は src を dst へ移す。
// 選別 JSON の置き場(ブラウザのダウンロード先)と hub が別のドライブ・別のファイルシステムにあると
// os.Rename は失敗するので、そのときは中身を写してから元を消す。
func moveFile(src, dst string) error {
	if err := osRename(src, dst); err == nil {
		return nil
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		return err
	}
	return os.Remove(src)
}

// appendKeeps は keep ファイルに、まだ無いリンクの記事だけ追記する。ファイルが無ければ見出しから作る。
func appendKeeps(path, month string, keeps []Keep, date, layer string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var fresh []Keep
	for _, k := range keeps {
		if k.Link == "" || bytes.Contains(existing, []byte("]("+k.Link+")")) {
			continue
		}
		fresh = append(fresh, k)
	}
	if len(fresh) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if len(existing) == 0 {
		fmt.Fprintf(f, "# 選別済みニュース %s\n", month)
	}
	_, err = f.WriteString(KeepMarkdown(fresh, date, layer))
	return err
}
