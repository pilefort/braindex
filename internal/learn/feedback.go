package learn

// 候補への回答: 利用者が候補に返した「既知」「不要」「後で」を保存し、次回の提示で伏せる。
//
// Build は毎回ゼロから候補を出すので、一度「これは要らない」「もう学んだ」と判断した語が次の週も同じ節に出る。
// そこで回答を節と語の組(Candidates と同じ識別)で持ち、Apply が候補から伏せる。語の綴りだけで持たないのは、
// 同じ語が別の節に出たとき(索引に無いと言われた語が、後に訂正の文脈に出る)は別の問いなので、答えを引き継がないため。
//
// 期限の扱いは回答の意味で分ける。「既知」「不要」は利用者の判断そのもので、材料の数や本文照合の結果が変わっても
// 覆らないので期限を持たない(解除するまで伏せる)。「後で」は判断の先送りなので Until を持ち、その日から再提示する。
// 回答は本人だけが書く: Build・Apply・Verify は回答ファイルを書かず、通常の提示が判断を上書きすることはない。
// 回答した候補の根拠が変わった(索引に載った・本文に書いた)ときも回答は消さない——候補が出なくなれば回答は効かないだけで、
// 消すと窓の揺れで候補が戻ったときに同じ判断をもう一度させることになる(それがこの機能で防ぎたいこと)。

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pilefort/braindex/internal/fsutil"
)

// FeedbackVersion は回答ファイルの書式の版(changes.json と同じ考え方)。
const FeedbackVersion = 1

// Answer は回答の種類。
type Answer string

const (
	Known    Answer = "known"    // 既知: もう学んだ・知っている。解除するまで伏せる
	Unwanted Answer = "unwanted" // 不要: 学ぶ対象ではない。解除するまで伏せる
	Later    Answer = "later"    // 後で: いまは扱わない。Until の日から再提示する
)

// ParseAnswer は CLI の語(known / unwanted / later と、その日本語)を Answer にする。
func ParseAnswer(s string) (Answer, error) {
	switch s {
	case "known", "既知":
		return Known, nil
	case "unwanted", "不要":
		return Unwanted, nil
	case "later", "後で":
		return Later, nil
	}
	return "", fmt.Errorf("回答 %q は無い(known=既知 / unwanted=不要 / later=後で)", s)
}

// Valid は保存できる回答か。
func (a Answer) Valid() bool { return a == Known || a == Unwanted || a == Later }

// Label は表示用の日本語。
func (a Answer) Label() string {
	switch a {
	case Known:
		return "既知"
	case Unwanted:
		return "不要"
	case Later:
		return "後で"
	}
	return string(a)
}

// sections は節の順(出力と回答ファイルの並び)。
var sections = []Section{SectionUnsettled, SectionStumbles, SectionReadNotWritten}

// Sections は節を出力の順で返す。
func Sections() []Section { return append([]Section(nil), sections...) }

// ParseSection は節の識別子(JSON のキー)を Section にする。
func ParseSection(s string) (Section, error) {
	for _, sec := range sections {
		if string(sec) == s {
			return sec, nil
		}
	}
	return "", fmt.Errorf("節 %q は無い(unsettled / stumbles / read_not_written)", s)
}

// Title は節の見出し(出力と同じ)。
func (s Section) Title() string {
	switch s {
	case SectionUnsettled:
		return "触れているが索引に無い"
	case SectionStumbles:
		return "訂正の文脈に繰り返し出る"
	case SectionReadNotWritten:
		return "残した記事にあるが索引に無い"
	}
	return string(s)
}

func sectionOrder(s Section) int {
	for i, sec := range sections {
		if sec == s {
			return i
		}
	}
	return len(sections)
}

// Feedback は回答 1 件。節と語の組が識別子。
type Feedback struct {
	Section Section `json:"section"`
	Word    string  `json:"word"`
	Answer  Answer  `json:"answer"`
	Date    string  `json:"date"`            // 回答した日(YYYY-MM-DD)
	Until   string  `json:"until,omitempty"` // 後で: この日から再提示する(YYYY-MM-DD)。他の回答では空
}

// Active は today にこの回答が効く(候補を伏せる)か。「後で」は Until の前日まで、他は常に効く。
func (f Feedback) Active(today string) bool {
	if f.Answer == Later {
		return today < f.Until
	}
	return true
}

// Feedbacks は回答ファイルの全体。Answers は節の順 → 語の昇順・重複なし。
type Feedbacks struct {
	Version int        `json:"version"`
	Answers []Feedback `json:"answers"`
}

// Find は節と語の回答を返す。
func (fb Feedbacks) Find(section Section, word string) (Feedback, bool) {
	for _, f := range fb.Answers {
		if f.Section == section && f.Word == word {
			return f, true
		}
	}
	return Feedback{}, false
}

// Set は回答を足す。同じ節と語があれば置き換える(前の回答は残さない)。
func (fb *Feedbacks) Set(f Feedback) {
	fb.Clear(f.Section, f.Word)
	fb.Answers = append(fb.Answers, f)
	sortFeedbacks(fb.Answers)
}

// Clear は節と語の回答を消す。あったかどうかを返す。
func (fb *Feedbacks) Clear(section Section, word string) bool {
	for i, f := range fb.Answers {
		if f.Section == section && f.Word == word {
			fb.Answers = append(fb.Answers[:i], fb.Answers[i+1:]...)
			return true
		}
	}
	return false
}

func sortFeedbacks(xs []Feedback) {
	sort.SliceStable(xs, func(i, j int) bool {
		if a, b := sectionOrder(xs[i].Section), sectionOrder(xs[j].Section); a != b {
			return a < b
		}
		return xs[i].Word < xs[j].Word
	})
}

// JSON は回答ファイルの形(インデント 2・LF・末尾に改行 1 つ)。同じ内容なら常に同じバイト列になる。
func (fb Feedbacks) JSON() []byte {
	out := Feedbacks{Version: FeedbackVersion, Answers: make([]Feedback, len(fb.Answers))}
	copy(out.Answers, fb.Answers)
	sortFeedbacks(out.Answers)
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		panic(err) // 文字列と整数だけの構造体なので失敗しない
	}
	return append(b, '\n')
}

// Markdown は回答の一覧。today は「後で」の期限切れを言うために使う。
func (fb Feedbacks) Markdown(today string) []byte {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# 学習候補への回答（%d 件）\n\n", len(fb.Answers))
	if len(fb.Answers) == 0 {
		sb.WriteString("（なし）\n")
		return []byte(sb.String())
	}
	for _, f := range fb.Answers {
		fmt.Fprintf(&sb, "- %s: %s — %s\n", f.Section.Title(), f.Word, f.Describe(today))
	}
	return []byte(sb.String())
}

// Describe は回答の説明。「既知（日付）」「後で（日付 に回答・日付 から再提示[・期限切れ]）」。
func (f Feedback) Describe(today string) string {
	if f.Answer != Later {
		return fmt.Sprintf("%s（%s）", f.Answer.Label(), f.Date)
	}
	s := fmt.Sprintf("後で（%s に回答・%s から再提示", f.Date, f.Until)
	if !f.Active(today) {
		s += "・期限切れ"
	}
	return s + "）"
}

// ParseFeedbacks は JSON() が書いた形を読み戻す。braindex が書く形でなければエラーにする(手で編集された・別の版が書いた)。
// 壊れた記録を無言で読み飛ばすと、次の保存で本人の回答を失うので、読めないことを呼び出し側に伝える。
func ParseFeedbacks(b []byte) (Feedbacks, error) {
	if len(bytes.TrimSpace(b)) == 0 {
		return Feedbacks{}, errors.New("空(回答を全部消すならファイルごと消す)")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var fb Feedbacks
	if err := dec.Decode(&fb); err != nil {
		return Feedbacks{}, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return Feedbacks{}, errors.New("JSON の末尾に余分な内容がある")
	}
	if fb.Version != FeedbackVersion {
		return Feedbacks{}, fmt.Errorf("版 %d は読めない(この braindex が書くのは版 %d)", fb.Version, FeedbackVersion)
	}
	seen := map[string]bool{}
	for i, f := range fb.Answers {
		if _, err := ParseSection(string(f.Section)); err != nil {
			return Feedbacks{}, fmt.Errorf("answers[%d]: %w", i, err)
		}
		if f.Word == "" || strings.ContainsAny(f.Word, " \t\r\n") {
			return Feedbacks{}, fmt.Errorf("answers[%d]: word %q は 1 語でない", i, f.Word)
		}
		key := string(f.Section) + "\x00" + f.Word
		if seen[key] {
			return Feedbacks{}, fmt.Errorf("answers[%d]: %s の %q が 2 回ある", i, f.Section, f.Word)
		}
		seen[key] = true
		if !f.Answer.Valid() {
			return Feedbacks{}, fmt.Errorf("answers[%d] %s: answer %q は無い(known / unwanted / later)", i, f.Word, f.Answer)
		}
		if _, err := time.Parse("2006-01-02", f.Date); err != nil {
			return Feedbacks{}, fmt.Errorf("answers[%d] %s: date は YYYY-MM-DD: %q", i, f.Word, f.Date)
		}
		switch {
		case f.Answer == Later && f.Until == "":
			return Feedbacks{}, fmt.Errorf("answers[%d] %s: later には until(再提示する日)が要る", i, f.Word)
		case f.Answer != Later && f.Until != "":
			return Feedbacks{}, fmt.Errorf("answers[%d] %s: until は later だけに書く", i, f.Word)
		case f.Until != "":
			if _, err := time.Parse("2006-01-02", f.Until); err != nil {
				return Feedbacks{}, fmt.Errorf("answers[%d] %s: until は YYYY-MM-DD: %q", i, f.Word, f.Until)
			}
		}
	}
	if fb.Answers == nil {
		fb.Answers = []Feedback{}
	}
	sortFeedbacks(fb.Answers)
	return fb, nil
}

// LoadFeedbacks は path の回答を読む。無ければ空(Version 0・エラーなし)を返す(回答したことが無い hub は普通に起きる)。
// 読めない・壊れている場合はエラー。
func LoadFeedbacks(path string) (Feedbacks, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Feedbacks{}, nil
		}
		return Feedbacks{}, err // PathError がパスを持つので包み直さない
	}
	fb, err := ParseFeedbacks(b)
	if err != nil {
		return Feedbacks{}, fmt.Errorf("%s: %w", path, err)
	}
	return fb, nil
}

// Save は回答を path に書く。書き切ってから置き換える(fsutil.WriteAtomic)ので、途中で落ちても前回の回答は残る。
func (fb Feedbacks) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsutil.WriteAtomic(path, fb.JSON(), 0o644)
}

// Hidden は回答で伏せた候補 1 件(語と材料の数は残す。JSON の読み手が「何を伏せたか」を辿れるように)。
type Hidden struct {
	Section Section `json:"section"`
	Item    Item    `json:"item"`
	Answer  Answer  `json:"answer"`
	Date    string  `json:"date"`
	Until   string  `json:"until,omitempty"`
}

// FeedbackSummary は 1 回の提示で回答をどう反映したか(Apply が付ける)。
type FeedbackSummary struct {
	Hidden   []Hidden `json:"hidden"`   // 伏せた候補(節の順 → 各節の並び順)
	Known    int      `json:"known"`    // うち既知
	Unwanted int      `json:"unwanted"` // うち不要
	Later    int      `json:"later"`    // うち後で(期限前)
	Expired  int      `json:"expired"`  // 「後で」の期限が来て再提示した候補の数(伏せていない)
}

// Apply は回答 fb を r に反映する。today に効く回答がある候補を各節から外して r.Feedback に積み、
// 「後で」の期限が来た候補はそのまま残して Item.Deferred に回答日を付ける(戻ってきた理由が読めるように)。
// r は書き換えるが、fb は読むだけ(回答を書くのは本人の操作だけ)。Truncate より前に呼ぶ。
func Apply(r *Report, fb Feedbacks, today string) {
	s := &FeedbackSummary{Hidden: []Hidden{}}
	keep := func(section Section, items []Item) []Item {
		out := items[:0:0]
		for _, it := range items {
			f, ok := fb.Find(section, it.Word)
			if !ok {
				out = append(out, it)
				continue
			}
			if !f.Active(today) {
				it.Deferred = f.Date
				s.Expired++
				out = append(out, it)
				continue
			}
			s.Hidden = append(s.Hidden, Hidden{Section: section, Item: it, Answer: f.Answer, Date: f.Date, Until: f.Until})
			switch f.Answer {
			case Known:
				s.Known++
			case Unwanted:
				s.Unwanted++
			case Later:
				s.Later++
			}
		}
		return out
	}
	r.Unsettled = keep(SectionUnsettled, r.Unsettled)
	r.Stumbles = keep(SectionStumbles, r.Stumbles)
	r.ReadNotWritten = keep(SectionReadNotWritten, r.ReadNotWritten)
	r.Feedback = s
}

// feedbackLine は「回答済み:」の行。伏せたものも再提示したものも無ければ空。
func (r Report) feedbackLine() string {
	s := r.Feedback
	if s == nil || (len(s.Hidden) == 0 && s.Expired == 0) {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "回答済み: 伏せた %d 件（既知 %d・不要 %d・後で %d）", len(s.Hidden), s.Known, s.Unwanted, s.Later)
	if s.Expired > 0 {
		fmt.Fprintf(&b, "・「後で」の期限が来て再提示 %d 件", s.Expired)
	}
	return b.String() + "\n"
}
