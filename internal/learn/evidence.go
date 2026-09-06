package learn

// 本文照合: 「索引に無い」候補の語を、ノートの本文で照合する。
//
// Build が「ノートに無い」と判定する材料は索引のタイトルと要旨だけで、本文は見ていない。要旨は先頭行の
// 80 字なので、本文の後半に書いてある語は「無い」側に落ちる。索引は手がかりであって不存在の証明ではない
// (設計レビュー補足 2026-09-06)。そこで候補を出したあと、本文検索(internal/textsearch)で語を照合し、
// 「本文で発見(位置つき)」「本文でも未発見(走査した範囲は全部読めた)」「確認不能(読めなかった範囲がある)」を分ける。
// 読めなかった範囲があるときは未発見を「無い」と断定しない——これがいちばん大事な区別。
//
// 候補の抽出(Build・入出力なし)と照合(Verify・本文を読む)は分け、検索の口は Searcher で差し替えられる。
// 出力に載せるのは出典の位置(パスと行)だけで、本文の行は Evidence に持たない(本文はどこにも書かず送らない)。

import (
	"github.com/pilefort/braindex/internal/scan"
	"github.com/pilefort/braindex/internal/textsearch"
)

// Section は節の識別子(JSON のキーと同じ)。候補は Section と Item.Word の組で識別する。
type Section string

const (
	SectionUnsettled      Section = "unsettled"        // 触れているが索引に無い
	SectionStumbles       Section = "stumbles"         // 訂正の文脈に繰り返し出る
	SectionReadNotWritten Section = "read_not_written" // 残した記事にあるが索引に無い
)

// Candidate は節をまたいで候補を 1 件ずつ扱うための組。Item は Report の中の項目を指す(書き込みは Report に反映される)。
type Candidate struct {
	Section Section
	Item    *Item
}

// Candidates は全節の候補を、節の順(unsettled → stumbles → read_not_written)・各節の並び順で返す。
// 本文照合と、後続の「候補への回答」が同じ識別(節・語)で候補を辿るための口。
func (r *Report) Candidates() []Candidate {
	var out []Candidate
	for i := range r.Unsettled {
		out = append(out, Candidate{SectionUnsettled, &r.Unsettled[i]})
	}
	for i := range r.Stumbles {
		out = append(out, Candidate{SectionStumbles, &r.Stumbles[i]})
	}
	for i := range r.ReadNotWritten {
		out = append(out, Candidate{SectionReadNotWritten, &r.ReadNotWritten[i]})
	}
	return out
}

// Status は本文照合の結果の区分。
type Status string

const (
	Found   Status = "found"   // 本文で発見(Locations に位置)。ノートはあり、索引の要旨から引けないだけ
	Absent  Status = "absent"  // 本文でも未発見。走査した範囲は全部読めたので、その範囲には無い
	Unknown Status = "unknown" // 確認不能。未発見だが読めなかった範囲があり、無いとは言えない
)

// Location は出典の位置。本文は持たない(パスと行だけ)。
type Location struct {
	Path string `json:"path"` // root 相対・スラッシュ区切り(索引・search と同じ形)
	Line int    `json:"line"` // 1 始まり
}

// Evidence は語 1 つの本文照合の結果(確認できた事実)。学習上の読みは持たない(表示側が Status で言い分ける)。
type Evidence struct {
	Status    Status     `json:"status"`
	Files     int        `json:"files,omitempty"`     // 語を含むファイル数
	Lines     int        `json:"lines,omitempty"`     // 語を含む行数
	Locations []Location `json:"locations,omitempty"` // 先頭 MaxLocations 件(パス → 行の順)
}

// Gap は確認できなかった範囲(scan.Gap と同じ内容。JSON のキーを小文字に揃えるために別に持つ)。
type Gap struct {
	Rel    string `json:"rel"`    // root 相対・スラッシュ区切り
	Dir    bool   `json:"dir"`    // true ならディレクトリ(配下すべてが確認不能)
	Reason string `json:"reason"` // 短い理由
}

// Verification は 1 回の本文照合の要約。
type Verification struct {
	Done     bool     `json:"done"`               // 照合を実行したか(語が無くて本文を読まなかったときも true)
	Words    int      `json:"words"`              // 照合した語数
	Files    int      `json:"files"`              // 最後まで読んで検索したファイル数
	Gaps     []Gap    `json:"gaps"`               // 確認できなかった範囲(Rel 昇順)
	Complete bool     `json:"complete"`           // 確認できなかった範囲が無いこと(Gaps が空)。false なら未発見を「無い」と読んではいけない
	Warnings []string `json:"warnings,omitempty"` // 飛ばしたものの説明(走査の警告と Gaps の分)
}

// Searcher は本文検索の口。通常は BodySearcher(走査設定)で作り、テストでは差し替える。
type Searcher func(q textsearch.Query) (textsearch.Result, error)

// BodySearcher は cfg の走査規則(索引と同じ)で本文を読む Searcher を返す。
func BodySearcher(cfg scan.Config) Searcher {
	return func(q textsearch.Query) (textsearch.Result, error) { return textsearch.Run(cfg, q) }
}

// VerifyOptions は照合の設定。ゼロ値は既定に置き換える。
type VerifyOptions struct {
	MaxLocations int // 各語に残す出典位置の数(既定 3)
}

const defaultMaxLocations = 3

// Verify は r の「索引に無い」候補(触れているが索引に無い・残した記事にあるが索引に無い)の語を本文で照合し、
// 各項目の Evidence と r.Verification を埋める。訂正の文脈の節は「ノートに無い」とは言っていないので照合しない。
//
// 本文は 1 回の検索(全語を Any・語の境界あり・大小無視)で読む。search が失敗したら(root が無い等)その error を
// 返し、r には何も書かない——呼び出し側は照合前の候補をそのまま出し、照合できなかったと伝える。
// 同じ材料からは同じ結果になる(位置は textsearch の順 = パス → 行)。
func Verify(r *Report, search Searcher, o VerifyOptions) error {
	if o.MaxLocations <= 0 {
		o.MaxLocations = defaultMaxLocations
	}
	var targets []Candidate
	var terms []string
	seen := map[string]bool{}
	for _, c := range r.Candidates() {
		if c.Section == SectionStumbles {
			continue
		}
		targets = append(targets, c)
		if !seen[c.Item.Word] {
			seen[c.Item.Word] = true
			terms = append(terms, c.Item.Word)
		}
	}
	if len(terms) == 0 {
		r.Verification = &Verification{Done: true, Gaps: []Gap{}, Complete: true}
		return nil
	}
	res, err := search(textsearch.Query{Terms: terms, Any: true, WholeWord: true})
	if err != nil {
		return err
	}
	v := &Verification{Done: true, Words: len(terms), Files: res.Files, Gaps: []Gap{}, Complete: res.Complete(), Warnings: res.Warnings}
	for _, g := range res.Gaps {
		v.Gaps = append(v.Gaps, Gap{Rel: g.Rel, Dir: g.Dir, Reason: g.Reason})
	}
	by := res.ByTerm()
	for _, c := range targets {
		c.Item.Evidence = evidenceFor(by[c.Item.Word], res.Complete(), o.MaxLocations)
	}
	r.Verification = v
	return nil
}

// evidenceFor は語 1 つの当たり(パス → 行の順)を Evidence にまとめる。本文(Hit.Text)は持ち込まない。
func evidenceFor(hits []textsearch.Hit, complete bool, maxLocations int) *Evidence {
	if len(hits) == 0 {
		if complete {
			return &Evidence{Status: Absent}
		}
		return &Evidence{Status: Unknown}
	}
	e := &Evidence{Status: Found, Lines: len(hits)}
	files := map[string]bool{}
	for _, h := range hits {
		files[h.Path] = true
		if len(e.Locations) < maxLocations {
			e.Locations = append(e.Locations, Location{Path: h.Path, Line: h.Line})
		}
	}
	e.Files = len(files)
	return e
}
