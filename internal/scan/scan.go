// Package scan はスキャン対象ファイルの発見を担う。
//
// 自動規則: root 直下の各ディレクトリ D について
//   - notes_dirs の各ディレクトリ N について D/N/**/*.md を再帰収集(種別は N の末尾セグメント + 相対位置)。
//     notes_dirs は braindex.json で複数指定でき、既定は ["docs/notes"](2026-09-02。wiki/ 派を受け入れるため。
//     規約の名前は docs のまま)。同じファイルが複数の指定から拾われたら先に書いた指定のラベルで 1 回だけ
//   - D/docs/decisions.md があれば 1 エントリ(種別 decisions。notes_dirs に含まれていても decisions が勝つ)
//   - root 相対パスのセグメントに archive を含むものは除外(root 自身のパスは見ない)
//   - docs/ を持たないディレクトリは自然にスキップ
//
// 例外規則(braindex.json の extra): 指定リポの起点から *.md を収集(リポ直下・research/・projects/ 等の規約外の置き場)。
//
//	再帰 extra の種別は kind/<起点直下のサブディレクトリ>(2026-08-27)。
package scan

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Config は braindex.json の内容。
type Config struct {
	Root      string      `json:"root"`
	NotesDirs []string    `json:"notes_dirs"` // 各リポのノート置き場(リポ相対・スラッシュ区切り)。複数可。空なら ["docs/notes"]
	Extra     []ExtraRule `json:"extra"`
}

// ExtraRule は自動規則で拾えない配置(リポ直下など規約外の置き場)を補う例外指定。
type ExtraRule struct {
	Repo      string   `json:"repo"`      // 対象リポ(root 直下のディレクトリ名)
	Path      string   `json:"path"`      // リポ内の起点。"." はリポ直下
	Recursive bool     `json:"recursive"` // false なら起点直下のみ
	Kind      string   `json:"kind"`      // catalog に載せる種別ラベル
	Exclude   []string `json:"exclude"`   // 除外パターン(path.Match のグロブ。"/" を含むなら起点からの相対パスに掛ける)
}

// File は発見した 1 ファイル。
type File struct {
	Repo string // リポ名(catalog の H2 見出し)
	Kind string // 種別ラベル(notes/common, notes/project, notes, notes/<sub>, decisions, ...)
	Rel  string // root 相対・スラッシュ区切りのパス
	Abs  string // 読み込み用の絶対パス
}

// DefaultNotesDir は notes_dirs 未指定時のノート置き場。
const DefaultNotesDir = "docs/notes"

// Scan は cfg に従って対象ファイルを発見する。
//
// 戻り値の warnings は「飛ばしたもの」の説明(読めないディレクトリ・存在しない extra の起点など)。
// 無言でスキップせず呼び出し側に伝え、走査自体は続ける。root が空・読めない場合は error。
func Scan(cfg Config) (files []File, warnings []string, err error) {
	if cfg.Root == "" {
		return nil, nil, errors.New("root が空")
	}
	notesDirs := cfg.NotesDirs
	if len(notesDirs) == 0 {
		notesDirs = []string{DefaultNotesDir}
	}
	rootAbs, err := filepath.Abs(cfg.Root)
	if err != nil {
		return nil, nil, err
	}
	for _, nd := range notesDirs {
		if err := checkRepoRelative("notes_dirs", nd); err != nil {
			return nil, nil, err
		}
	}
	for _, ex := range cfg.Extra {
		if ex.Repo == "" || strings.ContainsAny(ex.Repo, `/\\`) || ex.Repo == "." || ex.Repo == ".." {
			return nil, nil, fmt.Errorf("extra: repo は root 直下のディレクトリ名だけを書く: %q", ex.Repo)
		}
		// ラベルは「extra <repo> の path」。他の文言の "extra <repo>/<path>" と並んだとき /path が値に見えないように
		if err := checkRepoRelative("extra "+ex.Repo+" の path", ex.Path); err != nil {
			return nil, nil, err
		}
		for _, pat := range ex.Exclude {
			if _, err := path.Match(pat, ""); err != nil {
				return nil, nil, fmt.Errorf("extra %s/%s: exclude のパターンが不正: %q", ex.Repo, ex.Path, pat)
			}
		}
	}
	warn := func(format string, a ...any) {
		warnings = append(warnings, fmt.Sprintf(format, a...))
	}
	seen := map[string]bool{} // 同一ファイルの重複排除(絶対パス)。先に拾った方(自動規則 → extra の順)のラベルが勝つ

	// 自動規則: root 直下の各ディレクトリを走査
	entries, err := os.ReadDir(rootAbs)
	if err != nil {
		return nil, nil, fmt.Errorf("root を読めない: %w", err)
	}
	for _, de := range entries {
		if !de.IsDir() {
			continue
		}
		name := de.Name()
		if strings.HasPrefix(name, ".") {
			continue // .git などは対象外
		}
		repoDir := filepath.Join(rootAbs, name)

		// docs/decisions.md(notes_dirs より先に拾い、種別 decisions を優先する)
		decPath := filepath.Join(repoDir, "docs", "decisions.md")
		if fi, err := os.Stat(decPath); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				warn("%s: %s", relSlash(rootAbs, decPath), DescribeErr(err)) // 無いのは正常、読めないのは警告
			}
		} else if !fi.IsDir() {
			if f := mkFile(rootAbs, name, "decisions", decPath); !hasArchiveSeg(f.Rel) {
				files = append(files, f)
				seen[decPath] = true
			}
		}

		// notes_dirs の各 N について D/N/**/*.md
		for _, nd := range notesDirs {
			nd = strings.Trim(filepath.ToSlash(nd), "/")
			if nd == "" {
				continue
			}
			// 種別ラベルは各ディレクトリの末尾セグメント(docs/notes → notes、wiki → wiki)。既定の挙動は従来どおり
			label := path.Base(nd)
			notes := collectNotes(rootAbs, name, filepath.Join(repoDir, filepath.FromSlash(nd)), label, warn)
			for _, f := range notes {
				if seen[f.Abs] {
					continue
				}
				seen[f.Abs] = true
				files = append(files, f)
			}
		}
	}

	// 例外規則。自動規則で拾い済みのファイルは載せない(重複排除)
	for _, ex := range cfg.Extra {
		for _, f := range collectExtra(rootAbs, ex, warn) {
			if seen[f.Abs] {
				continue
			}
			seen[f.Abs] = true
			files = append(files, f)
		}
	}

	return files, warnings, nil
}

// warnFunc は走査中に飛ばしたものを報告する。
type warnFunc func(format string, a ...any)

// checkRepoRelative は設定のパス(notes_dirs・extra.path)がリポ内の相対パスであることを確かめる。
// ".." セグメントはリポの外へ出てしまい、root 相対でない行が索引に載る。絶対パスは filepath.Join が
// 先頭の区切りを捨てて相対扱いにするので外へは出ないが、書いた場所とは別の場所(リポの中)を指す。
// どちらも設定の誤りとして弾く。"" と "." はリポ直下の意味で許す。
func checkRepoRelative(what, p string) error {
	if p == "" || p == "." {
		return nil
	}
	if filepath.IsAbs(p) || filepath.IsAbs(filepath.FromSlash(p)) || strings.HasPrefix(p, "/") {
		return fmt.Errorf("%s: 絶対パスは書けない(リポ内の相対パスにする): %q", what, p)
	}
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if seg == ".." {
			return fmt.Errorf("%s: \"..\" でリポの外を指せない: %q", what, p)
		}
	}
	return nil
}

// collectNotes は notesDir 以下の *.md を再帰収集する。archive セグメントは除外。
// label は直下の種別ラベル(サブディレクトリ配下は label/<先頭セグメント>)。
// notesDir が無いのは「そのリポにノートが無い」だけなので警告しない。読めない場合は警告する。
func collectNotes(rootAbs, repo, notesDir, label string, warn warnFunc) []File {
	var out []File
	info, err := os.Stat(notesDir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			warn("%s: %s", relSlash(rootAbs, notesDir), DescribeErr(err))
		}
		return out
	}
	if !info.IsDir() {
		warn("%s: ディレクトリではない", relSlash(rootAbs, notesDir))
		return out
	}
	filepath.WalkDir(notesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			warn("%s: %s", relSlash(rootAbs, path), DescribeErr(err)) // 読めないものは警告して飛ばす
			return nil
		}
		if d.IsDir() {
			if d.Name() == "archive" {
				return filepath.SkipDir // アーカイブは普段の検索から外す
			}
			return nil
		}
		if !isMarkdown(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(notesDir, path)
		if err != nil {
			warn("%s: %s", relSlash(rootAbs, path), DescribeErr(err))
			return nil
		}
		if f := mkFile(rootAbs, repo, kindFromRel(rel, label), path); !hasArchiveSeg(f.Rel) {
			out = append(out, f)
		}
		return nil
	})
	return out
}

// collectExtra は例外規則に従ってファイルを収集する。
// 起点が無い・読めない・archive の下にあるのは設定の誤りなので警告する(自動規則の notesDir 不在とは違う)。
func collectExtra(rootAbs string, ex ExtraRule, warn warnFunc) []File {
	base := filepath.Join(rootAbs, ex.Repo, filepath.FromSlash(ex.Path))
	var out []File
	// 起点自体が archive セグメントの下なら、archive の除外規則で全件落ちる。設定の誤りなので無言にしない
	if hasArchiveSeg(path.Join(ex.Repo, ex.Path)) {
		warn("extra %s/%s: パスに archive を含むので全件除外(載せるなら archive の外に置く)", ex.Repo, ex.Path)
		return out
	}
	if info, err := os.Stat(base); err != nil {
		warn("extra %s/%s: %s", ex.Repo, ex.Path, DescribeErr(err))
		return out
	} else if !info.IsDir() {
		warn("extra %s/%s: ディレクトリではない", ex.Repo, ex.Path)
		return out
	}

	// relFromBase は起点からの相対パス(スラッシュ区切り)。exclude の "/" 入りパターンはこれに掛ける
	add := func(path, name, relFromBase, kind string) {
		if !isMarkdown(name) || excluded(name, relFromBase, ex.Exclude) {
			return
		}
		if f := mkFile(rootAbs, ex.Repo, kind, path); !hasArchiveSeg(f.Rel) {
			out = append(out, f)
		}
	}

	if ex.Recursive {
		filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				warn("%s: %s", relSlash(rootAbs, path), DescribeErr(err))
				return nil
			}
			if d.IsDir() {
				if d.Name() == "archive" {
					return filepath.SkipDir
				}
				return nil
			}
			rel, err := filepath.Rel(base, path)
			if err != nil {
				warn("%s: %v", path, err)
				return nil
			}
			add(path, d.Name(), filepath.ToSlash(rel), extraKind(ex.Kind, base, path))
			return nil
		})
		return out
	}

	des, err := os.ReadDir(base)
	if err != nil {
		warn("%s: %s", relSlash(rootAbs, base), DescribeErr(err))
		return out
	}
	for _, de := range des {
		if de.IsDir() {
			continue
		}
		add(filepath.Join(base, de.Name()), de.Name(), de.Name(), ex.Kind)
	}
	return out
}

// extraKind は再帰 extra の種別を返す。起点直下なら kind そのまま、サブディレクトリ配下なら
// "kind/<先頭セグメント>"（docs/notes の kindFromRel と同じ規則。research/topic-a・projects/alpha 等）。
func extraKind(kind, base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return kind
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) <= 1 {
		return kind
	}
	return kind + "/" + parts[0]
}

// kindFromRel は notesDir からの相対パスに応じた種別を返す。
// 直下なら label(既定 "notes")、サブディレクトリ配下なら "label/<先頭セグメント>"。
func kindFromRel(rel, label string) string {
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	if len(parts) <= 1 {
		return label
	}
	return label + "/" + parts[0]
}

func mkFile(rootAbs, repo, kind, absPath string) File {
	return File{
		Repo: repo,
		Kind: kind,
		Rel:  relSlash(rootAbs, absPath),
		Abs:  absPath,
	}
}

// relSlash は絶対パスを root 相対・スラッシュ区切りにする(catalog の Path と同じ形。警告のパスにも使う)。
func relSlash(rootAbs, absPath string) string {
	rel, err := filepath.Rel(rootAbs, absPath)
	if err != nil {
		rel = absPath
	}
	return filepath.ToSlash(rel)
}

func isMarkdown(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".md")
}

// excluded は exclude パターンに当たるかを判定する。パターンは path.Match のグロブ
// (ワイルドカード無しなら完全一致と同じ)。"/" を含むパターンは起点からの相対パス、
// 含まないパターンはファイル名に掛ける。不正なパターンは Scan の入口で弾いてあるので、ここでは無視する。
func excluded(name, relFromBase string, patterns []string) bool {
	for _, pat := range patterns {
		target := name
		if strings.Contains(pat, "/") {
			target = relFromBase
		}
		if ok, _ := path.Match(pat, target); ok {
			return true
		}
	}
	return false
}

// hasArchiveSeg は root 相対パス(スラッシュ区切り)のいずれかのセグメントが archive かを判定する。
// 絶対パスに掛けると root 自身が archive ディレクトリの下にあるとき全件除外されるので、必ず相対パスを渡す。
func hasArchiveSeg(rel string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if seg == "archive" {
			return true
		}
	}
	return false
}

// DescribeErr は警告向けにエラーを短く言い直す。*fs.PathError はパスを繰り返さないよう Err だけにし、
// 存在しない場合は OS ごとの文言でなく「存在しない」にする。
func DescribeErr(err error) string {
	if errors.Is(err, fs.ErrNotExist) {
		return "存在しない"
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}
