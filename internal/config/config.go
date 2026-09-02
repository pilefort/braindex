// Package config は設定ファイル braindex.json の最上位を扱う。
//
// いまは走査の設定(scan.Config)だけだが、後続のサブコマンド(review / retro / news)は
// ここに自分の節のフィールドを 1 つ足す形で設定を受け取る。
// 未知のキーと、オブジェクトの後ろに続く余分な内容はエラーにする(notes_dir のような打ち間違いや
// 壊れたファイルを無言で通さないため)。
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/pilefort/braindex/internal/review"
	"github.com/pilefort/braindex/internal/scan"
)

// DefaultPath は設定ファイルの既定の置き場(カレントディレクトリ基準)。
const DefaultPath = "braindex.json"

// Config は braindex.json の内容。
type Config struct {
	scan.Config
	Review review.Settings `json:"review"` // 週次レビュー(braindex review)の節。省略可
}

// Load は path の設定ファイル(JSON)を読む。
// ファイルが無ければ found=false でゼロ値を返す(エラーにしない。既定パスの不在は正常)。
// 読めない・JSON が不正・未知のキーがある・末尾に余分な内容がある場合はエラー
// (Decoder は先頭の 1 値しか読まないので、末尾は自分で確かめる)。
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
	if _, err := dec.Token(); err != io.EOF {
		return cfg, true, fmt.Errorf("設定ファイル %s: 末尾に余分な内容がある(JSON のオブジェクト 1 つだけを書く)", path)
	}
	return cfg, true, nil
}
