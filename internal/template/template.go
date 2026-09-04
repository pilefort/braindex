// Package template は braindex init が展開する雛形を持つ。
//
// 雛形は //go:embed でバイナリに埋め込む(go install 1 回で全部入り)。置き場は
// templates/<kind>/...(kind: hub / repo)。ドットファイル(.gitattributes・.gitkeep・.claude/)も
// 含めるため all: 接頭辞で埋め込む。
//
// Install は既存ファイルを上書きしない。利用者の編集を壊さず、再実行しても安全にするため。
// 展開先に何が足りないかだけを補い、作った／残したの一覧を返す。
//
// 置いたファイルの内容ハッシュは台帳(LedgerPath)に記録する。後から Update が「配った版のまま
// なのか、利用者が編集したのか」をこれで見分ける(ledger.go・update.go)。
package template

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed all:templates
var templates embed.FS

// Kind は雛形の種別。
type Kind string

const (
	KindHub  Kind = "hub"  // 索引を置く hub リポ(README・CLAUDE.md・braindex.json・docs/・work/・skill)
	KindRepo Kind = "repo" // 各プロジェクトのリポ(docs/notes・docs/decisions.md・work/)
)

// File は雛形の 1 ファイル。Path は展開先からの相対パス("/" 区切り)。
type File struct {
	Path    string
	Content []byte
}

// Files は kind の雛形を Path の昇順で返す。無い種別はエラー。
func Files(kind Kind) ([]File, error) {
	base := path.Join("templates", string(kind))
	if st, err := fs.Stat(templates, base); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("雛形が無い: kind=%q(hub か repo)", kind)
	}
	var out []File
	err := fs.WalkDir(templates, base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := templates.ReadFile(p)
		if err != nil {
			return err
		}
		out = append(out, File{Path: strings.TrimPrefix(p, base+"/"), Content: b})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// Result は Install の結果。パスは展開先からの相対・"/" 区切り・昇順。
type Result struct {
	Created []string // 新しく作ったファイル
	Skipped []string // 既に存在したので触らなかったファイル
}

// Install は dst に雛形を展開する。dst が無ければ作る。既存ファイルは残して Skipped に積む。
// 途中で書けなかった場合はそこまでの Result とエラーを返す。
func Install(dst string, kind Kind) (Result, error) {
	files, err := Files(kind)
	if err != nil {
		return Result{}, err
	}
	led, _, err := LoadLedger(dst)
	if err != nil {
		return Result{}, err
	}
	led.Kind = string(kind)

	var res Result
	for _, f := range files {
		target := filepath.Join(dst, filepath.FromSlash(f.Path))
		if _, err := os.Lstat(target); err == nil {
			res.Skipped = append(res.Skipped, f.Path) // 既存は台帳に記録しない(素性が分からない)
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			_ = SaveLedger(dst, led) // ここまでに置いたものは記録してから返す
			return res, err          // PathError がパスを持つので包み直さない
		}
		if err := writeFile(target, f.Content); err != nil {
			_ = SaveLedger(dst, led)
			return res, err
		}
		res.Created = append(res.Created, f.Path)
		led.Files[f.Path] = Hash(f.Content)
	}
	if err := SaveLedger(dst, led); err != nil {
		return res, err
	}
	return res, nil
}
