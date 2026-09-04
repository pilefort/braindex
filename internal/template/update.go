package template

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// NewSuffix は、利用者が編集したファイルの隣に置く「今の版」の拡張子。
const NewSuffix = ".new"

// UpdateOptions は Update の振る舞いを変える。
type UpdateOptions struct {
	Force  bool // 編集済みのファイルも上書きする
	DryRun bool // 何も書かず、結果だけを返す
}

// Conflict は、利用者が編集していたので置き換えなかったファイル。
type Conflict struct {
	Path string // 利用者のファイル(そのまま残す)
	New  string // 隣に置いた今の版
}

// UpdateResult は Update の結果。パスは展開先からの相対・"/" 区切り・雛形の順(昇順)。
type UpdateResult struct {
	Created   []string   // 無かったので作った
	Updated   []string   // 配った版のままだったので今の版にした
	Unchanged []string   // 既に今の版と同じだった
	Conflicts []Conflict // 編集されていたので .new を置いた
}

// Update は dst の雛形由来ファイルを今の版に追いつかせる。Install が「無いものを足す」のに対し、
// Update は「既にあるものを今の版にする」。判定は台帳(LedgerPath)のハッシュで行い、
// 利用者が編集したファイルは上書きせず、隣に .new を置いて報告する(決定 2026-09-04)。
//
// 台帳は、実際に置いた(または既に一致していた)ファイルについてだけ進める。.new を置いたファイルは
// 記録を据え置く——次に走らせたときも「編集済み」と分かり、取り込み漏れを隠さないため。
func Update(dst string, kind Kind, opt UpdateOptions) (UpdateResult, error) {
	files, err := Files(kind)
	if err != nil {
		return UpdateResult{}, err
	}
	led, _, err := LoadLedger(dst)
	if err != nil {
		return UpdateResult{}, err
	}
	led.Kind = string(kind)

	var res UpdateResult
	for _, f := range files {
		target := filepath.Join(dst, filepath.FromSlash(f.Path))
		cur, err := os.ReadFile(target)
		switch {
		case err != nil && errors.Is(err, fs.ErrNotExist):
			res.Created = append(res.Created, f.Path)
		case err != nil:
			return res, err
		case bytes.Equal(cur, f.Content):
			// 既に今の版。書く必要は無いが、台帳は追いつかせる
			res.Unchanged = append(res.Unchanged, f.Path)
			led.Files[f.Path] = Hash(f.Content)
			continue
		case opt.Force || led.Files[f.Path] == Hash(cur):
			// 配った版のまま(または -force)。黙って今の版にする
			res.Updated = append(res.Updated, f.Path)
		default:
			// 利用者が編集している。現物は残し、今の版を隣に置く
			res.Conflicts = append(res.Conflicts, Conflict{Path: f.Path, New: f.Path + NewSuffix})
			if !opt.DryRun {
				if err := writeFile(target+NewSuffix, f.Content); err != nil {
					return res, err
				}
			}
			continue
		}
		if !opt.DryRun {
			if err := writeFile(target, f.Content); err != nil {
				return res, err
			}
		}
		led.Files[f.Path] = Hash(f.Content)
	}
	if !opt.DryRun {
		if err := SaveLedger(dst, led); err != nil {
			return res, err
		}
	}
	return res, nil
}

// writeFile は親ディレクトリを作ってから書く。
func writeFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
