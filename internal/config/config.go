// Package config は設定ファイル braindex.json の最上位を扱う。
//
// いまは走査の設定(scan.Config)だけだが、後続のサブコマンド(review / retro / news)は
// ここに自分の節のフィールドを 1 つ足す形で設定を受け取る。
// 未知のキーはエラーにする(notes_dir のような打ち間違いを無言で無視しないため)。
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/pilefort/braindex/internal/scan"
)

// DefaultPath は設定ファイルの既定の置き場(カレントディレクトリ基準)。
const DefaultPath = "braindex.json"

// Config は braindex.json の内容。
type Config struct {
	scan.Config
}

// Load は path の設定ファイル(JSON)を読む。
// ファイルが無ければ found=false でゼロ値を返す(エラーにしない。既定パスの不在は正常)。
// 読めない・JSON が不正・未知のキーがある場合はエラー。
func Load(path string) (cfg Config, found bool, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, false, nil
		}
		return cfg, false, fmt.Errorf("設定ファイルを読めない: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, true, fmt.Errorf("設定ファイル %s: %w", path, err)
	}
	return cfg, true, nil
}
