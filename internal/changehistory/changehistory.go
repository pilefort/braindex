// Package changehistory は、ノートの本文が変わったことを索引とは別に記録する(index/changes.json)。
//
// 索引(catalog.md)の行は日付・種別・タイトル・要旨・パスだけなので、本文の後半だけを直した更新では行が変わらない。
// そこで、本文から計算した内容ハッシュと「その内容を最初に見た日」を索引と同じディレクトリの changes.json に持ち、
// 次の生成でハッシュが違えば「本文が変わった」と分かるようにする(設計レビュー補足 2026-09-06「ノートの日付と、
// 内容が変わった時点を分ける」)。
//
// 索引には入れない。索引は同じ入力から常にバイト一致(決定性)でなければならないが、観測日は実行した日で決まる。
// 記録日(索引の日付)も変えない——いつ書いた知識かという情報を失わないため。mtime は使わない(git が保存しないので
// clone ごとに変わる。決定 2026-09-02)。ハッシュは内容が変わったことを見分ける値であって、意味のある変更かどうかは
// 判定しない。
//
// 初回(記録が無い)は、全件を「今日変わった」と捏造せず、観測日を空(不明)で記録する。
// 走査で読めなかった範囲(scan.Gap)にある記録は前回のまま据え置く——読めなかっただけで、消えたとも変わったとも言えないため。
package changehistory

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/pilefort/braindex/internal/fsutil"
	"github.com/pilefort/braindex/internal/scan"
)

const (
	// FileName は記録のファイル名。索引(catalog.md)と同じディレクトリに置く。
	FileName = "changes.json"
	// Version は書式の版。読み手が書式の変化に気づくための目印(update の台帳と同じ考え方)。
	Version = 1
)

// Note は今回の走査で本文を読めたノート 1 件(catalog.Build が渡す)。
type Note struct {
	Path string // root 相対・スラッシュ区切り(索引の Path と同じ)
	Hash string // Hash(本文)
}

// Entry は記録の 1 件。
type Entry struct {
	Path     string `json:"path"`
	Hash     string `json:"hash"`
	Observed string `json:"observed,omitempty"` // この内容を最初に見た日(YYYY-MM-DD)。空なら不明(記録を始めた時点で既にあった)
	Missing  string `json:"missing,omitempty"`  // 見当たらなくなった日(YYYY-MM-DD)。空なら今も見えている
}

// History は記録全体。Notes は Path 昇順・重複なし。
type History struct {
	Version int     `json:"version"`
	Notes   []Entry `json:"notes"`
}

// Known は記録があるか(false は zero 値＝記録が無い。次の Update は初回として扱う)。
func (h History) Known() bool { return h.Version != 0 }

// Present は今も見えている記録(Missing が空)だけを返す。
func (h History) Present() []Entry {
	var out []Entry
	for _, e := range h.Notes {
		if e.Missing == "" {
			out = append(out, e)
		}
	}
	return out
}

// Hash は本文の内容ハッシュ(sha256 の 16 進)。BOM を除き、CRLF と CR を LF に揃えてから計算する
// (extract と同じ正規化。checkout の改行変換や BOM の付け外しを「本文の変更」に数えないため)。
func Hash(content []byte) string {
	content = bytes.TrimPrefix(content, []byte{0xEF, 0xBB, 0xBF})
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	content = bytes.ReplaceAll(content, []byte("\r"), []byte("\n"))
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// Report は Update が何をしたかの件数。stdout の 1 行に使う。
type Report struct {
	Initial    bool // 記録が無かった(全件を観測日不明で記録した)
	Total      int  // 記録の件数(見当たらないものを含む)
	New        int  // 記録に無かったノート(観測日は今日)
	Changed    int  // ハッシュが変わったノート(観測日は今日)
	Reappeared int  // 見当たらなかったノートが同じ内容で戻った(観測日は前のまま)
	Missing    int  // 今回見当たらなくなったノート
	Held       int  // 読めなかった範囲にあるので前回のまま据え置いたノート
}

// Update は前回の記録 prev と今回の走査 seen から新しい記録を作る。today は観測日(YYYY-MM-DD)。
//
// 規則(パスごと):
//   - 記録に無い → 観測日は today。ただし prev が無い(初回)なら空(不明)——「今日変わった」と捏造しない
//   - 記録と同じハッシュ → 観測日は前のまま。見当たらなかったものが戻ったなら Missing を消す
//   - 記録と違うハッシュ → 観測日は today
//   - 記録にあるが今回読めていない → 読めなかった範囲(gaps)の中なら前回のまま据え置く。
//     範囲の外なら Missing に today(既に付いていればそのまま)
//
// 同じ prev・seen・gaps・today からは常に同じ記録になる。
func Update(prev History, seen []Note, gaps []scan.Gap, today string) (History, Report) {
	rep := Report{Initial: !prev.Known()}
	old := make(map[string]Entry, len(prev.Notes))
	for _, e := range prev.Notes {
		old[e.Path] = e
	}
	next := History{Version: Version, Notes: []Entry{}}
	now := make(map[string]bool, len(seen))
	for _, n := range seen {
		if n.Path == "" || now[n.Path] {
			continue // 重複は先勝ち(scan は重複排除済みなので通常は来ない)
		}
		now[n.Path] = true
		e := Entry{Path: n.Path, Hash: n.Hash}
		p, ok := old[n.Path]
		switch {
		case !ok:
			if !rep.Initial {
				e.Observed = today
				rep.New++
			}
		case p.Hash != n.Hash:
			e.Observed = today
			rep.Changed++
		default:
			e.Observed = p.Observed
			if p.Missing != "" {
				rep.Reappeared++
			}
		}
		next.Notes = append(next.Notes, e)
	}
	for _, p := range prev.Notes {
		if now[p.Path] {
			continue
		}
		switch {
		case inGap(gaps, p.Path):
			if p.Missing == "" {
				rep.Held++
			}
		case p.Missing == "":
			p.Missing = today
			rep.Missing++
		}
		next.Notes = append(next.Notes, p)
	}
	sortEntries(next.Notes)
	rep.Total = len(next.Notes)
	return next, rep
}

func inGap(gaps []scan.Gap, rel string) bool {
	for _, g := range gaps {
		if g.Covers(rel) {
			return true
		}
	}
	return false
}

func sortEntries(es []Entry) {
	sort.SliceStable(es, func(i, j int) bool { return es[i].Path < es[j].Path })
}

// Marshal は記録を JSON(インデント 2・LF・末尾に改行 1 つ)にする。同じ内容なら常に同じバイト列になる。
func (h History) Marshal() []byte {
	out := History{Version: Version, Notes: make([]Entry, len(h.Notes))}
	copy(out.Notes, h.Notes)
	sortEntries(out.Notes)
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		panic(err) // 文字列と整数だけの構造体なので失敗しない
	}
	return append(b, '\n')
}

// Parse は Marshal が書いた JSON を読み戻す。braindex が書く形でなければエラーにする(手で編集された・別の版が書いた)。
// 壊れた記録を無言で読み飛ばすと、次の Save で前回の観測を失うので、読めないことを呼び出し側に伝える。
func Parse(b []byte) (History, error) {
	if len(bytes.TrimSpace(b)) == 0 {
		return History{}, errors.New("空(記録を消すならファイルごと消す)")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var h History
	if err := dec.Decode(&h); err != nil {
		return History{}, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return History{}, errors.New("JSON の末尾に余分な内容がある")
	}
	if h.Version != Version {
		return History{}, fmt.Errorf("版 %d は読めない(この braindex が書くのは版 %d)", h.Version, Version)
	}
	seen := make(map[string]bool, len(h.Notes))
	for i, e := range h.Notes {
		if e.Path == "" {
			return History{}, fmt.Errorf("notes[%d]: path が空", i)
		}
		if seen[e.Path] {
			return History{}, fmt.Errorf("notes[%d]: path %q が 2 回ある", i, e.Path)
		}
		seen[e.Path] = true
		if e.Hash == "" {
			return History{}, fmt.Errorf("notes[%d] %s: hash が空", i, e.Path)
		}
		for name, d := range map[string]string{"observed": e.Observed, "missing": e.Missing} {
			if d == "" {
				continue
			}
			if _, err := time.Parse("2006-01-02", d); err != nil {
				return History{}, fmt.Errorf("notes[%d] %s: %s は YYYY-MM-DD: %q", i, e.Path, name, d)
			}
		}
	}
	if h.Notes == nil {
		h.Notes = []Entry{}
	}
	sortEntries(h.Notes)
	return h, nil
}

// Load は path の記録を読む。無ければ zero 値(Known() が false)を返し、エラーにしない
// (記録を持たない hub で初めて生成するのは普通に起きる)。読めない・壊れている場合はエラー。
func Load(path string) (History, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return History{}, nil
		}
		return History{}, err // PathError がパスを持つので包み直さない
	}
	h, err := Parse(b)
	if err != nil {
		return History{}, fmt.Errorf("%s: %w", path, err)
	}
	return h, nil
}

// Save は記録を path に書く。書き切ってから置き換える(fsutil.WriteAtomic)ので、途中で落ちても前回の記録は残る。
// 同時に 2 つの生成が走っても、読み手が半端な記録を読むことはない(どちらか一方の完全な記録になる)。
func Save(path string, h History) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsutil.WriteAtomic(path, h.Marshal(), 0o644)
}
