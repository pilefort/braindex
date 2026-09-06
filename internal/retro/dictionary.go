// Package retro は訂正率トリガのレトロスペクティブ(braindex retro)の判定と集計。
//
// 判定は規則ベース: 人間の発話に辞書(1 行 1 正規表現の平文)を当てるだけで、LLM は使わない。
// 判定の精度より「同じ基準で継続して測れる」を優先する(設計判断 2026-09-02)。
// 何の訂正かの分類はレトロスペクティブ本体(テンプレの skill)が担う。
package retro

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// 既定辞書。利用者は設定でファイルに差し替え(dictionary)たり、足し(dictionary_extra)たりできる。
//
//go:embed dict/corrections.txt dict/sentiment.txt
var defaultDicts embed.FS

// Dictionary は判定に使う定型句の集合。
type Dictionary struct {
	Name     string   // 表示用。既定は "corrections" / "sentiment"、ファイルから読んだものはファイル名
	Patterns []string // 辞書の行(順序を保つ。Match.Pattern に入る)
	res      []*regexp.Regexp
}

// Match は 1 発話に当たった辞書の行。
type Match struct {
	Dict    string // 辞書名
	Pattern string // 当たった行(正規表現の元)
	Text    string // 当たった文字列(最初の 1 つ)
}

// Parse は平文の辞書を読む。1 行 1 正規表現(Go の RE2)。# で始まる行と空行は無視し、前後の空白は除く。
// 先頭の BOM と CRLF は吸収する。正規表現が不正な行はエラー(辞書名と行番号を含む)。
func Parse(name, src string) (*Dictionary, error) {
	src = strings.TrimPrefix(src, "\uFEFF")
	d := &Dictionary{Name: name}
	for i, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		re, err := regexp.Compile(line)
		if err != nil {
			return nil, fmt.Errorf("辞書 %s の %d 行目: 正規表現が不正: %w", name, i+1, err)
		}
		d.Patterns = append(d.Patterns, line)
		d.res = append(d.res, re)
	}
	return d, nil
}

// Load はファイルの辞書を読む。辞書名はファイル名。
func Load(path string) (*Dictionary, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("辞書を読めない: %w", err)
	}
	return Parse(filepath.Base(path), string(b))
}

var (
	defaultsOnce sync.Once
	corrections  *Dictionary
	sentiment    *Dictionary
)

func loadDefaults() {
	corrections = mustEmbedded("corrections")
	sentiment = mustEmbedded("sentiment")
}

// mustEmbedded は埋め込み辞書を読む。埋め込みが壊れているのはプログラムの誤りなので panic。
func mustEmbedded(name string) *Dictionary {
	b, err := defaultDicts.ReadFile("dict/" + name + ".txt")
	if err != nil {
		panic("braindex retro: 埋め込み辞書が無い: " + err.Error())
	}
	d, err := Parse(name, string(b))
	if err != nil {
		panic("braindex retro: 埋め込み辞書が不正: " + err.Error())
	}
	return d
}

// Corrections は埋め込みの既定辞書(訂正)。訂正率の分子は、この辞書(か設定で差し替えた辞書)のヒットがある発話で数える。
func Corrections() *Dictionary {
	defaultsOnce.Do(loadDefaults)
	return corrections
}

// Sentiment は埋め込みの既定辞書(感情: 不満と好例)。率には入れない。
func Sentiment() *Dictionary {
	defaultsOnce.Do(loadDefaults)
	return sentiment
}

// Classify は text に当たる辞書の行を、辞書の順 → 行の順で返す。当たらなければ nil。
// 1 行につき Match は 1 つ(最初に当たった箇所)。純関数で、同じ入力からは同じ結果。
func Classify(text string, dicts ...*Dictionary) []Match {
	var out []Match
	for _, d := range dicts {
		if d == nil {
			continue
		}
		for i, re := range d.res {
			if loc := re.FindStringIndex(text); loc != nil {
				out = append(out, Match{Dict: d.Name, Pattern: d.Patterns[i], Text: text[loc[0]:loc[1]]})
			}
		}
	}
	return out
}
