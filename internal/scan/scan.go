// Package scan はスキャン対象ファイルの発見を担う。
//
// 自動規則: root 直下の各ディレクトリ D について
//   - notes_dirs の各ディレクトリ N について D/N/**/*.md を再帰収集(種別は N の末尾セグメント + 相対位置)。
//     notes_dirs は braindex.json で複数指定でき、既定は ["docs/notes"](2026-09-02。wiki/ 派を受け入れるため。
//     規約の名前は docs のまま)。同じファイルが複数の指定から拾われたら先に書いた指定のラベルで 1 回だけ
//   - D/docs/decisions.md があれば 1 エントリ(種別 decisions。notes_dirs に含まれていても decisions が勝つ)
//   - パスセグメントに archive を含むものは除外
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
	Exclude   []string `json:"exclude"`   // 除外するファイル名
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
		return nil, nil, errors.New("root が未指定(-root を渡すか、設定ファイルに root を書く)")
	}
	notesDirs := cfg.NotesDirs
	if len(notesDirs) == 0 {
		notesDirs = []string{DefaultNotesDir}
	}
	rootAbs, err := filepath.Abs(cfg.Root)
	if err != nil {
		return nil, nil, err
	}
	warn := func(format string, a ...any) {
		warnings = append(warnings, fmt.Sprintf(format, a...))
	}

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

		seen := map[string]bool{} // 同一ファイルの重複排除(絶対パス)。先に拾った方のラベルが勝つ

		// docs/decisions.md(notes_dirs より先に拾い、種別 decisions を優先する)
		decPath := filepath.Join(repoDir, "docs", "decisions.md")
		if fi, err := os.Stat(decPath); err == nil && !fi.IsDir() && !hasArchiveSeg(decPath) {
			files = append(files, mkFile(rootAbs, name, "decisions", decPath))
			seen[decPath] = true
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

	// 例外規則
	for _, ex := range cfg.Extra {
		files = append(files, collectExtra(rootAbs, ex, warn)...)
	}

	return files, warnings, nil
}

// warnFunc は走査中に飛ばしたものを報告する。
type warnFunc func(format string, a ...any)

// collectNotes は notesDir 以下の *.md を再帰収集する。archive セグメントは除外。
// label は直下の種別ラベル(サブディレクトリ配下は label/<先頭セグメント>)。
// notesDir が無いのは「そのリポにノートが無い」だけなので警告しない。読めない場合は警告する。
func collectNotes(rootAbs, repo, notesDir, label string, warn warnFunc) []File {
	var out []File
	info, err := os.Stat(notesDir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			warn("%s: %v", notesDir, err)
		}
		return out
	}
	if !info.IsDir() {
		return out
	}
	filepath.WalkDir(notesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			warn("%s: %v", path, err) // 読めないものは警告して飛ばす
			return nil
		}
		if d.IsDir() {
			if d.Name() == "archive" {
				return filepath.SkipDir // アーカイブは普段の検索から外す
			}
			return nil
		}
		if !isMarkdown(d.Name()) || hasArchiveSeg(path) {
			return nil
		}
		rel, err := filepath.Rel(notesDir, path)
		if err != nil {
			warn("%s: %v", path, err)
			return nil
		}
		out = append(out, mkFile(rootAbs, repo, kindFromRel(rel, label), path))
		return nil
	})
	return out
}

// collectExtra は例外規則に従ってファイルを収集する。
// 起点が無い・読めないのは設定の誤りなので警告する(自動規則の notesDir 不在とは違う)。
func collectExtra(rootAbs string, ex ExtraRule, warn warnFunc) []File {
	base := filepath.Join(rootAbs, ex.Repo, filepath.FromSlash(ex.Path))
	var out []File
	if info, err := os.Stat(base); err != nil {
		warn("extra %s/%s: %v", ex.Repo, ex.Path, err)
		return out
	} else if !info.IsDir() {
		warn("extra %s/%s: ディレクトリではない", ex.Repo, ex.Path)
		return out
	}

	add := func(path, name, kind string) {
		if !isMarkdown(name) || excluded(name, ex.Exclude) || hasArchiveSeg(path) {
			return
		}
		out = append(out, mkFile(rootAbs, ex.Repo, kind, path))
	}

	if ex.Recursive {
		filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				warn("%s: %v", path, err)
				return nil
			}
			if d.IsDir() {
				if d.Name() == "archive" {
					return filepath.SkipDir
				}
				return nil
			}
			add(path, d.Name(), extraKind(ex.Kind, base, path))
			return nil
		})
		return out
	}

	des, err := os.ReadDir(base)
	if err != nil {
		warn("%s: %v", base, err)
		return out
	}
	for _, de := range des {
		if de.IsDir() {
			continue
		}
		add(filepath.Join(base, de.Name()), de.Name(), ex.Kind)
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
	rel, err := filepath.Rel(rootAbs, absPath)
	if err != nil {
		rel = absPath
	}
	return File{
		Repo: repo,
		Kind: kind,
		Rel:  filepath.ToSlash(rel),
		Abs:  absPath,
	}
}

func isMarkdown(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".md")
}

func excluded(name string, list []string) bool {
	for _, e := range list {
		if name == e {
			return true
		}
	}
	return false
}

// hasArchiveSeg はパスのいずれかのセグメントが archive かを判定する。
func hasArchiveSeg(path string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(path), "/") {
		if seg == "archive" {
			return true
		}
	}
	return false
}
