package news

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pilefort/braindex/internal/weblink"
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

// SelectionDatePattern は選別 JSON の date に許す形。date は keep ファイル(news/keep/YYYY-MM.md)の
// パスの一部になるので、数字とハイフンだけに限る。選別 JSON はブラウザのダウンロード先という信用境界の
// 外から拾うため、`../../x` のような値で置き場の外に書かせない(設計レビュー 2026-09-06 H4)。
var SelectionDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// FeedNames は feeds.json の取材先を名前の集合にする(Ingest の feed_stats の照合用)。
func FeedNames(srcs []Source) map[string]bool {
	m := make(map[string]bool, len(srcs))
	for _, s := range srcs {
		m[s.Name] = true
	}
	return m
}

// checkFeedStats は feed_stats のうち、取材先の名前が known にあって数が 0 以上の項目だけを返す。
// 併せて落とした項目の数を返す。known が nil のときは名前を照合しない(feeds.json を読めなかったとき)。
func checkFeedStats(in map[string]FeedStats, known map[string]bool) (map[string]FeedStats, int) {
	out := make(map[string]FeedStats, len(in))
	dropped := 0
	for name, d := range in {
		if known != nil && !known[name] {
			dropped++
			continue
		}
		if d.Shown < 0 || d.Kept < 0 || d.Dropped < 0 || d.Hidden < 0 || d.Rescued < 0 {
			dropped++
			continue
		}
		out[name] = d
	}
	return out, dropped
}

// Ingest は dirs にある選別 JSON(SelectionPrefix*.json)を名前順に取り込み、keep に追記し、統計を上書きし、
// ファイルを newsDir/.ingested/ へ移す。取り込んだ件数分のメッセージを返す。形式が違うファイルは飛ばして伝える。
// 同じ記事(リンク)が keep ファイルに既にあれば追記しない。統計はダイジェスト("<日付>_<層>")単位で上書き。
// known は feeds.json の取材先の名前(FeedNames)。feed_stats はこの名前にある項目だけ数える。nil なら照合しない。
func Ingest(newsDir string, dirs []string, known map[string]bool) (msgs []string, err error) {
	var paths []string
	for _, d := range dirs {
		// glob ではなく走査する: 置き場の名前に [ や * が入っていてもパターンとして解釈されない。
		// os.ReadDir はファイル名の昇順で返す。名前に時刻が入るので昇順 = 時刻順で、後勝ちで最新が残る
		des, rerr := os.ReadDir(d)
		if rerr != nil {
			if !errors.Is(rerr, fs.ErrNotExist) { // 無い置き場は黙って飛ばす。読めない置き場は伝える(取り込みは続ける)
				msgs = append(msgs, fmt.Sprintf("置き場を読めない(%v): %s", rerr, d))
			}
			continue
		}
		for _, de := range des {
			n := de.Name()
			if de.IsDir() || !strings.HasPrefix(n, SelectionPrefix) || !strings.HasSuffix(n, ".json") {
				continue
			}
			paths = append(paths, filepath.Join(d, n))
		}
	}
	if len(paths) == 0 {
		return msgs, nil
	}
	statsPath := filepath.Join(newsDir, StatsFile)
	st, err := LoadStats(statsPath)
	if err != nil {
		return nil, err
	}
	done := 0 // 取り込めた選別 JSON の数
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
		// date が使えない選別 JSON は取り込まず、取り込み済みへも移さない。
		// 人が中を見て消せるように元の場所に残す(勝手に .ingested/ へ隠すと気づけない)。
		if !SelectionDatePattern.MatchString(sel.Date) {
			msgs = append(msgs, fmt.Sprintf("選別 JSON %s: date %q が YYYY-MM-DD でない → 取り込まない", filepath.Base(p), sel.Date))
			continue
		}
		date, layer := sel.Date, sel.Layer
		if layer == "" {
			layer = "unknown"
		}
		stats, dropped := checkFeedStats(sel.FeedStats, known)
		if dropped > 0 {
			msgs = append(msgs, fmt.Sprintf("選別 JSON %s: feed_stats の %d 項目を落とした(feeds.json に無い取材先か、負の数)", filepath.Base(p), dropped))
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
		st.Digests[date+"_"+layer] = stats
		ingested := filepath.Join(newsDir, IngestedDir)
		if err := os.MkdirAll(ingested, 0o755); err != nil {
			return msgs, err
		}
		if err := moveFile(p, filepath.Join(ingested, filepath.Base(p))); err != nil {
			return msgs, fmt.Errorf("取り込み済みへ移せない: %w", err)
		}
		msgs = append(msgs, fmt.Sprintf("取り込み: %s（残す %d 件）", filepath.Base(p), len(sel.Keeps)))
		done++
	}
	// 1 つも取り込めなかったら統計は触らない。中身が全部 type 違い・date 違いのときに
	// 空の .stats.json だけができるのを避ける(何も取り込まなかった回は何も残さない)。
	if done == 0 {
		return msgs, nil
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
		// keep は git 管理の蓄積側なので、載せるリンクは http(s) だけにする(決定 2026-09-03)。
		// 落とす扱いはリンクの無い記事と同じ: 記録しない(題名だけ書くと、次回の重複判定に引っかからず毎回増える)。
		if k.Link == "" || !weblink.Safe(k.Link) || bytes.Contains(existing, []byte("]("+k.Link+")")) {
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

// 不要ばかり付く取材先を主要表示から下ろす条件(決定 2026-09-06)。
const (
	// DemoteMinJudged は減点の判定に必要な「残す／不要を選んだ数」。これ未満は材料不足として下げない。
	DemoteMinJudged = 10
	// DemoteDropRate はこれを超える不要率の取材先を下げる。
	DemoteDropRate = 0.8
	// DemotedMaxScore は下げた取材先の点の上限(interest.MaxScore の半分)。
	DemotedMaxScore = 1
)

// DemotedFeeds は不要率の高い取材先の名前を返す。
//
// 不要率 = 不要 ÷ (残す ＋ 不要 ＋ 救済)。「見た数」でなく「選んだ数」で割るのは、
// 折りたたみに入って目に入らなかった記事を不要と数えないため。
//
// 代償を承知で入れている: いったん下がると折りたたみ側に回って keep に入らないので、
// 不要率が下がる機会も減る(下がりっぱなしになりうる)。間引き候補(PruneCandidates)と違って
// 人の手を待たずに効くので、効果は試用して見る。
func DemotedFeeds(totals map[string]FeedStats) map[string]bool {
	out := map[string]bool{}
	for name, d := range totals {
		judged := d.Kept + d.Dropped + d.Rescued
		if judged < DemoteMinJudged {
			continue
		}
		if float64(d.Dropped)/float64(judged) > DemoteDropRate {
			out[name] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
