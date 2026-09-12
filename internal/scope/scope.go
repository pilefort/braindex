// Package scope は横断の矛盾検査(skill contradiction-scan)の走査対象を規則ベースで列挙・絞り込み・分割する。
//
// 索引(index/catalog.md)を読み、話題・リポで絞り、chunk(1 束をサブエージェント 1 体が読む単位)に分ける。
// 矛盾の判定はしない(それは実ファイルを全文読む人かエージェントの仕事)。ここが出すのは対象の場所(パス)であって
// 本文ではない。同じ入力からは常に同じ出力が出る。
package scope

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pilefort/braindex/internal/extract"
	"github.com/pilefort/braindex/internal/render"
	"github.com/pilefort/braindex/internal/review"
)

// DefaultChunkSize は chunk あたりの既定の件数。
const DefaultChunkSize = 12

// Entry は走査対象の 1 件(索引の 1 行と同じ列)。
type Entry struct {
	Repo    string `json:"repo"`
	Date    string `json:"date"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Path    string `json:"path"` // 索引と同じ root 相対(-dir のときは渡されたディレクトリと結合した形。どちらもそのまま開ける)
}

// Result は Build の結果。
type Result struct {
	Mode    string    `json:"mode"` // full / topic:<語> / repo:<名> / dir:<パス>
	Entries int       `json:"n_entries"`
	Chunks  [][]Entry `json:"chunks"`
}

// Options は絞り込みの指定。Topic・Repo・Dir はいずれか 1 つ(全部空なら全件)。
type Options struct {
	Topic string // タイトル・要旨・パスの部分一致(大小無視)
	Repo  string // H2 見出し(リポ名)の完全一致
	Dir   string // 索引を使わず、このディレクトリ配下の *.md を列挙する
	Size  int    // chunk あたりの件数。0 以下なら DefaultChunkSize
}

// ParseCatalog は catalog.md を Entry の列にする(internal/review の読み手を使う。形式の二重定義を避ける)。
func ParseCatalog(b []byte) ([]Entry, error) {
	rs, err := review.ParseCatalog(b)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(rs))
	for _, r := range rs {
		out = append(out, fromRender(r))
	}
	return out, nil
}

func fromRender(r render.Entry) Entry {
	return Entry{Repo: r.Repo, Date: r.Date, Kind: r.Kind, Title: r.Title, Summary: r.Summary, Path: r.Path}
}

// EnumerateDir は dir 配下の *.md(再帰・パス昇順)を Entry にする。タイトルと日付は索引と同じ規則で本文から取る。
// リポ名は dir の名前、種別は "dir"、パスは渡された dir と結合した形(/ 区切り)。呼び出し元のカレントからそのまま開ける。
//
// 走査規則: archive セグメントとドットで始まるディレクトリは降りない(退避したノートと .git 配下を対象にしない)。
// ただし起点の dir 自身には掛けない。掛けると archive やドットディレクトリを直接渡したときに全件消えるため。
// archive の除外は索引(scan.collectNotes)と同じ。ドットの除外はそれより広い——索引がドットを見るのは
// root 直下のリポ選びだけなので、docs/notes/.drafts/ のような置き場の中の隠しディレクトリは索引には載る(2026-09-04 実測)。
func EnumerateDir(dir string) ([]Entry, error) {
	fi, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%s はディレクトリでない(-dir にはディレクトリを渡す。1 ファイルだけ検査したいときも、そのファイルがあるディレクトリを渡す)", filepath.ToSlash(dir))
	}
	repo := filepath.Base(filepath.Clean(dir))
	var out []Entry
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p == dir {
				return nil // 起点自身には除外規則を掛けない
			}
			if d.Name() == "archive" || strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir // 索引と同じ規則(archive は退避先・. で始まるのは .git など)
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return rerr
		}
		content, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		m := extract.Extract(d.Name(), content, "dir")
		out = append(out, Entry{Repo: repo, Date: m.Date, Kind: "dir", Title: m.Title, Summary: m.Summary, Path: filepath.ToSlash(filepath.Join(dir, rel))})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// Filter は repo(完全一致)と topic(タイトル・要旨・パスの部分一致・大小無視)で絞る。順は保つ。
func Filter(entries []Entry, topic, repo string) []Entry {
	out := make([]Entry, 0, len(entries))
	t := strings.ToLower(topic)
	for _, e := range entries {
		if repo != "" && e.Repo != repo {
			continue
		}
		if t != "" && !strings.Contains(strings.ToLower(e.Title), t) &&
			!strings.Contains(strings.ToLower(e.Summary), t) &&
			!strings.Contains(strings.ToLower(e.Path), t) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// Chunk は entries を size 件ずつに分ける。空なら空。
func Chunk(entries []Entry, size int) [][]Entry {
	if size <= 0 {
		size = DefaultChunkSize
	}
	out := [][]Entry{}
	for i := 0; i < len(entries); i += size {
		j := i + size
		if j > len(entries) {
			j = len(entries)
		}
		out = append(out, entries[i:j])
	}
	return out
}

// Build は catalog(索引の内容。Dir 指定時は使わない)から走査対象を作る。
func Build(catalog []byte, o Options) (Result, error) {
	var entries []Entry
	var err error
	mode := "full"
	if o.Dir != "" {
		entries, err = EnumerateDir(o.Dir)
		mode = "dir:" + filepath.ToSlash(o.Dir)
	} else {
		entries, err = ParseCatalog(catalog)
	}
	if err != nil {
		return Result{}, err
	}
	if o.Repo != "" {
		mode = "repo:" + o.Repo
	}
	if o.Topic != "" {
		mode = "topic:" + o.Topic
	}
	entries = Filter(entries, o.Topic, o.Repo)
	return Result{Mode: mode, Entries: len(entries), Chunks: Chunk(entries, o.Size)}, nil
}

// Render は結果を人が読む Markdown にする。chunk ごとにパス・タイトル・日付の一覧。
func Render(r Result) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# braindex scope: %s\n", r.Mode)
	fmt.Fprintf(&b, "対象 %d 件 / %d chunk\n\n", r.Entries, len(r.Chunks))
	for i, ch := range r.Chunks {
		fmt.Fprintf(&b, "## chunk %d (%d 件)\n", i+1, len(ch))
		for _, e := range ch {
			var meta []string
			for _, s := range []string{e.Repo, e.Kind, e.Date} {
				if s != "" {
					meta = append(meta, s)
				}
			}
			fmt.Fprintf(&b, "- [%s] %s  —  %s\n", strings.Join(meta, " "), e.Title, e.Path)
		}
		b.WriteString("\n")
	}
	return []byte(b.String())
}
