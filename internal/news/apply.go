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
	AddFeeds   []FeedRequest        `json:"add_feeds,omitempty"`
	Type       string               `json:"type"` // SelectionType
	Date       string               `json:"date"`
	Layer      string               `json:"layer"`
	ExportedAt string               `json:"exported_at"`
	Keeps      []Keep               `json:"keeps"`
	FeedStats  map[string]FeedStats `json:"feed_stats"`
	Reading    []ReadingUpdate      `json:"reading,omitempty"`
	Library    bool                 `json:"library,omitempty"`
}

// Keep は「残す」にした記事。
type Keep struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Link         string `json:"link"`
	Feed         string `json:"feed"`
	Category     string `json:"category,omitempty"`
	Score        string `json:"score,omitempty"`   // 関心度(文字列。採点なしは空)
	Rescued      bool   `json:"rescued,omitempty"` // 「関心外と判定」から残した(採点の見逃し)
	Summary      string `json:"summary,omitempty"`
	DisplayTitle string `json:"display_title,omitempty"` // 表示用の日本語訳。原見出しはTitleに保持する。
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

// Save は統計ファイルを書く(キー順で整形。同じ内容なら同じバイト列)。書き込みは原子的。
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
	return writeAtomic(path, buf.Bytes(), 0o644)
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
		fmt.Fprintf(&sb, "- [%s](%s) — %s", escapeTitle(title), markdownDestination(k.Link), k.Feed)
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

// Ingest は dirs にある選別 JSON(SelectionPrefix*.json)を dirs をまたいで基底名の昇順に取り込み、
// keep に追記し、統計を上書きし、ファイルを newsDir/.ingested/ へ移す。取り込んだ件数分のメッセージを返す。
// 形式が違うファイルは飛ばして伝える。同じ記事(リンク)が keep ファイルに既にあれば追記しない。
// 統計はダイジェスト("<日付>_<層>")単位で上書き。
// known は feeds.json の取材先の名前(FeedNames)。feed_stats はこの名前にある項目だけ数える。nil なら照合しない。
//
// 途中で止まっても再実行で揃う: 選別 JSON 1 つごとに keep → 統計 → 取り込み済みへ移す、の順で書く。
// 移す前に止まれば次回また同じ JSON を読み、keep はリンクで重複を除き、統計は同じキーに同じ数を上書きするので
// 同じ結果になる。逆順(移してから統計)だと、移した後に統計を書けずに止まったとき、その選別の数は二度と拾えない
// (置き場から消えているので次回は読まない)。並行起動の排他は呼び出し側の Lock。
func Ingest(newsDir string, dirs []string, known map[string]bool, feedsPath string, catalog []CatalogEntry) (msgs []string, err error) {
	var paths []string
	for _, d := range dirs {
		// glob ではなく走査する: 置き場の名前に [ や * が入っていてもパターンとして解釈されない。
		// os.ReadDir はファイル名の昇順で返すが、それは置き場ごとの順でしかない。
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
	// 置き場をまたぐと上のループの順(dirs の順が先に効く)だけでは名前順にならない。
	// 集め終えてから基底名で並べ替える: 名前に時刻が入るので昇順 = 時刻順で、後勝ちで最新が残る。
	sort.Slice(paths, func(i, j int) bool { return filepath.Base(paths[i]) < filepath.Base(paths[j]) })
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
		reading, err := LoadReading(newsDir)
		if err != nil {
			return msgs, err
		}
		reading.Merge(sel)
		kept := 0
		if len(sel.Keeps) > 0 && !sel.Library {
			month := date
			if len(month) >= 7 {
				month = month[:7]
			}
			kept, err = appendKeeps(filepath.Join(newsDir, KeepDir, month+".md"), month, sel.Keeps, date, layer)
			if err != nil {
				return msgs, err
			}
		}
		// 統計は 1 つ取り込むごとに書く(移す前に)。1 つも取り込めなかった回は統計を触らないので、
		// 中身が全部 type 違い・date 違いのときに空の .stats.json だけができることもない
		if !sel.Library {
			st.Digests[date+"_"+layer] = stats
		}
		if err := st.Save(statsPath); err != nil {
			return msgs, err
		}
		var added []string
		feedsFailed := false
		if len(sel.AddFeeds) > 0 && !sel.Library {
			var feedMsgs []string
			added, feedMsgs, err = AddFeeds(feedsPath, catalog, sel.AddFeeds, sel.Layer, sel.Date)
			msgs = append(msgs, feedMsgs...)
			if err != nil {
				msgs = append(msgs, fmt.Sprintf("選別 JSON %s: 取材先を登録できない（次回再取り込み）: %v", filepath.Base(p), err))
				feedsFailed = true
			}
			if len(added) > 0 {
				msgs = append(msgs, fmt.Sprintf("取材先 %d 本を feeds.json に足した（次回の fetch から取る）", len(added)))
				for _, e := range catalog {
					if !e.IsGeneralNews() {
						continue
					}
					generalAdded := false
					for _, name := range added {
						if name == e.Name {
							generalAdded = true
							break
						}
					}
					if generalAdded {
						msgs = append(msgs, "一般ニュースは層 general に入れた。定期実行に `-layer general` の行を足すか、`-layer all` で取る")
						break
					}
				}
			}
		}
		if feedsFailed {
			continue
		}
		if err := reading.Save(newsDir); err != nil {
			return msgs, err
		}
		if err := WriteReading(newsDir, reading); err != nil {
			return msgs, err
		}
		ingested := filepath.Join(newsDir, IngestedDir)
		if err := os.MkdirAll(ingested, 0o755); err != nil {
			return msgs, err
		}
		if err := moveFile(p, filepath.Join(ingested, filepath.Base(p))); err != nil {
			return msgs, fmt.Errorf("取り込み済みへ移せない: %w", err)
		}
		if sel.Library {
			msgs = append(msgs, fmt.Sprintf("取り込み: %s（読書状態と相談を反映）", filepath.Base(p)))
		} else if len(sel.AddFeeds) == 0 {
			msgs = append(msgs, fmt.Sprintf("取り込み: %s（残す %d 件）", filepath.Base(p), kept))
		} else {
			msgs = append(msgs, fmt.Sprintf("取り込み: %s（残す %d 件・取材先 %d 本を追加）", filepath.Base(p), kept, len(added)))
		}
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
	if err := writeAtomic(dst, b, 0o644); err != nil {
		return err
	}
	return os.Remove(src)
}

// appendKeeps は keep ファイルに、まだ無いリンクの記事だけ足して書き直す。ファイルが無ければ見出しから作る。
// 追記(O_APPEND)でなく全体を原子的に書き直すのは、途中で止まったときに書きかけの行を残さないため
// (リンクの欠けた行は次回の重複判定に掛からず、同じ記事がもう 1 行増える)。既にある部分はバイト列のまま写す。
// 戻り値は実際に足した件数(fresh の数)。keeps の件数をそのまま返すと、リンクが無い・安全でない・
// 重複で落とした分も数えてしまい、呼び出し側の「残す N 件」のメッセージと食い違う。
func appendKeeps(path, month string, keeps []Keep, date, layer string) (int, error) {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}
	perm := fs.FileMode(0o644)
	if fi, serr := os.Stat(path); serr == nil { // 利用者の版管理下のファイルなので、権限は今のまま保つ
		perm = fi.Mode().Perm()
	}
	var fresh []Keep
	seen := make(map[string]bool, len(keeps))
	for _, k := range keeps {
		// keep は git 管理の蓄積側なので、載せるリンクは http(s) だけにする(決定 2026-09-03 → manual/design.md「決めたこと」)。
		// 落とす扱いはリンクの無い記事と同じ: 記録しない(題名だけ書くと、次回の重複判定に引っかからず毎回増える)。
		if k.Link == "" || !weblink.Safe(k.Link) || seen[k.Link] ||
			bytes.Contains(existing, []byte("]("+k.Link+")")) ||
			bytes.Contains(existing, []byte("](<"+k.Link+">)")) {
			continue
		}
		seen[k.Link] = true
		fresh = append(fresh, k)
	}
	if len(fresh) == 0 {
		return 0, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}
	var buf bytes.Buffer
	if len(existing) == 0 {
		fmt.Fprintf(&buf, "# 選別済みニュース %s\n", month)
	} else {
		buf.Write(existing)
	}
	buf.WriteString(KeepMarkdown(fresh, date, layer))
	if err := writeAtomic(path, buf.Bytes(), perm); err != nil {
		return 0, err
	}
	return len(fresh), nil
}

// 不要ばかり付く取材先を主要表示から下ろす条件(決定 2026-09-06 → manual/news.md「決めたこと」)。
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
