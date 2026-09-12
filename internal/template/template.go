// Package template は braindex init が展開する雛形を持つ。
//
// 雛形は //go:embed でバイナリに埋め込む(go install 1 回で全部入り)。置き場は
// templates/<kind>/...(kind: hub / repo)。ドットファイル(.gitattributes・.gitkeep・.claude/)も
// 含めるため all: 接頭辞で埋め込む。
//
// Install は既存ファイルを上書きしない。利用者の編集を壊さず、再実行しても安全にするため。
// 展開先に何が足りないかだけを補い、作った／残したの一覧を返す。hub の配布物は機能(feature.go)ごとに
// 分かれていて、braindex init -add <機能> で 1 つずつ足せる(決定 2026-09-05 → manual/init-update.md「決めたこと」)。
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
	Merged  []string // 既にあったので、無い節・行だけ足したファイル(braindex.json・.gitignore)
	Skipped []string // 既に存在したので触らなかったファイル
}

// Install は dst に雛形を展開する。dst が無ければ作る。既存ファイルは残して Skipped に積む。
// hub は全機能(all)を配る(従来の init と同じ)。機能を選ぶなら InstallFeatures。
// 途中で書けなかった場合はそこまでの Result とエラーを返す。
func Install(dst string, kind Kind) (Result, error) {
	if kind == KindHub {
		return InstallFeatures(dst, []Feature{FeatureAll})
	}
	files, err := Files(kind)
	if err != nil {
		return Result{}, err
	}
	return install(dst, kind, files, nil)
}

// InstallFeatures は hub に feats の配布物を足す。依存(review → conventions)と core は自動で足す。
// braindex.json と .gitignore は既にあっても上書きせず、無い節・行だけ足して Merged に積む
// (同じ機能を 2 回足しても安全・利用者の編集は残る)。足した機能は台帳の Features に記録する。
func InstallFeatures(dst string, feats []Feature) (Result, error) {
	if err := checkFeatures(feats); err != nil {
		return Result{}, err
	}
	feats, _ = Resolve(feats)
	files, err := FeatureFiles(feats)
	if err != nil {
		return Result{}, err
	}
	return install(dst, KindHub, files, feats)
}

func install(dst string, kind Kind, files []File, feats []Feature) (Result, error) {
	led, _, err := LoadLedger(dst)
	if err != nil {
		return Result{}, err
	}
	led.Kind = string(kind)
	if kind == KindHub {
		if led.Features == nil {
			// 機能の記録を持たない旧版の台帳(または台帳なし)。既にある機能を推定して落とさない
			inferred, err := InferFeatures(dst)
			if err != nil {
				return Result{}, err
			}
			led.Features = FeatureNames(inferred)
		}
		led.Features = mergeNames(led.Features, FeatureNames(feats))
	}

	var res Result
	for _, f := range files {
		target := filepath.Join(dst, filepath.FromSlash(f.Path))
		cur, err := os.ReadFile(target)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			_ = SaveLedger(dst, led) // ここまでに置いたものは記録してから返す
			return res, err          // PathError がパスを持つので包み直さない
		}
		if err == nil {
			// 既存。braindex.json と .gitignore だけは無い節・行を足す。台帳は「配った版のまま」だった
			// ときだけ進める(利用者が編集した後に足した版を記録すると、update が編集を見分けられなくなる)
			merged, changed, merr := mergeExisting(f.Path, cur, feats)
			if merr != nil {
				_ = SaveLedger(dst, led)
				return res, merr // BuildConfig がパスを添える。ここで包むと "braindex.json: braindex.json: ..." になる
			}
			if !changed {
				res.Skipped = append(res.Skipped, f.Path)
				continue
			}
			if err := writeFile(target, merged); err != nil {
				_ = SaveLedger(dst, led)
				return res, err
			}
			res.Merged = append(res.Merged, f.Path)
			if led.Files[f.Path] == Hash(cur) {
				led.Files[f.Path] = Hash(merged)
			}
			continue
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

// mergeExisting は既存ファイルに足すものがあるかを返す。braindex.json は feats の節、.gitignore は雛形の行。
// それ以外のファイルは触らない(changed=false)。
func mergeExisting(rel string, cur []byte, feats []Feature) ([]byte, bool, error) {
	switch rel {
	case ConfigPath:
		if feats == nil {
			return nil, false, nil
		}
		out, changed, err := BuildConfig(cur, feats)
		if err != nil {
			return nil, false, err
		}
		return out, changed, nil
	case GitignorePath:
		tmpl, err := templates.ReadFile(path.Join("templates", string(KindHub), GitignorePath))
		if err != nil {
			return nil, false, err
		}
		out, changed := MergeGitignore(cur, tmpl)
		return out, changed, nil
	}
	return nil, false, nil
}

// mergeNames は 2 つの名前の列を重複なく昇順に合わせる。
func mergeNames(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]string(nil), a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
