package template

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
	Merged    []string   // 編集されていたが、無い節・行だけ足した(braindex.json・.gitignore)
	Unchanged []string   // 既に今の版と同じだった
	Conflicts []Conflict // 編集されていたので .new を置いた
	Features  []Feature  // 追従の対象にした機能(core を含む・段の順)
	Inferred  bool       // 台帳に機能の記録が無く、存在するファイルから推定した
}

// Update は dst の雛形由来ファイルを今の版に追いつかせる。Install が「無いものを足す」のに対し、
// Update は「既にあるものを今の版にする」。判定は台帳(LedgerPath)のハッシュで行い、
// 利用者が編集したファイルは上書きせず、隣に .new を置いて報告する(決定 2026-09-04)。
//
// hub は台帳の Features に記録された機能の分だけ追従する(足していない機能のファイルは作らない・決定 2026-09-05)。
// 記録の無い hub(機能を持たない旧版で作ったもの)は、存在するファイルと設定の節から機能を推定し、台帳に書く。
// braindex.json と .gitignore は、編集済みでも無い節・行だけは足す(Merged)。braindex.json はそのうえで
// 今の版と違えば .new も置く(節の中の新しいキーは足さないため)。.gitignore は行が揃えば .new を置かない。
//
// 台帳は、実際に置いた(または既に一致していた)ファイルについてだけ進める。.new を置いたファイルは
// 記録を据え置く——次に走らせたときも「編集済み」と分かり、取り込み漏れを隠さないため。
func Update(dst string, kind Kind, opt UpdateOptions) (UpdateResult, error) {
	led, _, err := LoadLedger(dst)
	if err != nil {
		return UpdateResult{}, err
	}
	led.Kind = string(kind)

	var res UpdateResult
	var files []File
	if kind == KindHub {
		feats, inferred, err := hubFeatures(dst, led)
		if err != nil {
			return UpdateResult{}, err
		}
		res.Features, res.Inferred = feats, inferred
		led.Features = FeatureNames(feats)
		if files, err = FeatureFiles(feats); err != nil {
			return UpdateResult{}, err
		}
	} else if files, err = Files(kind); err != nil {
		return UpdateResult{}, err
	}

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
			// 利用者が編集している。braindex.json・.gitignore は無い節・行を足し、それ以外は現物を残して今の版を隣に置く
			merged, changed, merr := mergeExisting(f.Path, cur, res.Features)
			if merr != nil {
				return res, fmt.Errorf("%s: %w", f.Path, merr)
			}
			if changed {
				res.Merged = append(res.Merged, f.Path)
				if !opt.DryRun {
					if err := writeFile(target, merged); err != nil {
						return res, err
					}
				}
			}
			if f.Path == GitignorePath {
				continue // 行が揃えば十分。雛形そのものを .new で置いても読む価値が無い
			}
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

// hubFeatures は hub が持つ機能を返す。台帳に記録があればそれ(core と依存を足す)。無ければ推定する。
func hubFeatures(dst string, led Ledger) (feats []Feature, inferred bool, err error) {
	if led.Features != nil {
		var req []Feature
		for _, n := range led.Features {
			req = append(req, Feature(n))
		}
		feats, _ = Resolve(req)
		return feats, false, nil
	}
	req, err := InferFeatures(dst)
	if err != nil {
		return nil, false, err
	}
	feats, _ = Resolve(req)
	return feats, true, nil
}

// InferFeatures は台帳に機能の記録が無い hub について、存在するファイルと braindex.json の節から
// 足してある機能を推定する(core は含めない・段の順)。機能の配布物(.gitignore を除く)か設定の節が
// 1 つでもあれば、その機能は足してあるとみなす。
func InferFeatures(dst string) ([]Feature, error) {
	keys := map[string]bool{}
	if b, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(ConfigPath))); err == nil {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("%s: %w", ConfigPath, err)
		}
		for k := range m {
			keys[k] = true
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	var out []Feature
	for _, f := range featureOrder {
		if f == FeatureCore {
			continue
		}
		spec := features[f]
		found := false
		for _, p := range spec.Files {
			if p == GitignorePath {
				continue // 誰の .gitignore でもありうる
			}
			if _, err := os.Lstat(filepath.Join(dst, filepath.FromSlash(p))); err == nil {
				found = true
				break
			}
		}
		for _, k := range spec.Sections {
			if keys[k] {
				found = true
			}
		}
		if found {
			out = append(out, f)
		}
	}
	return out, nil
}

// writeFile は親ディレクトリを作ってから書く。
func writeFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
