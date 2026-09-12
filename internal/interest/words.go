// Package interest は関心プロファイル(語 → 重み・出典)を決定的に作る(braindex news profile)。
//
// 出典は 4 種: 索引の直近差分(最近書いたノートのタイトル・種別・リポ名)、直近のセッション内容(人間の発話とアシスタント本文)、
// 選別で「残す」にしたニュースの見出し(keep 履歴)、補助の関心ファイル(1 行 1 語)。
// 語の抽出は形態素解析なしの規則(ラテン文字列・カタカナ連続・漢字連続・識別子)で、埋め込みのストップワードを除く。
// 意味検索・埋め込みは使わない(設計判断 2026-08-19 の範囲を news にも適用)。LLM も使わない。
// セッション本文はここで読むだけで、どこにも書かず送らない。
package interest

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// 語の規則。
const (
	minLatin  = 3 // ラテン文字の語の最短(md・fn のような 2 文字は雑音が多い)。例外は shortLatin
	minKana   = 2 // カタカナ連続の最短
	minKanji  = 2 // 漢字連続の最短
	maxKanji  = 6 // 漢字連続の最長。超える連続は 1 語にできない(複合語が長すぎる)ので捨てる
	maxLatin  = 40
	maxKanaLn = 30
)

var latinRe = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_\-]*`)

// shortLatin は 2 文字でも語として拾うもの。2 文字は雑音が多いので原則落とすが、
// 領域を名指しする語がそこに落ちると、その領域の関心をまったく拾えなくなる
// (設計レビュー 2026-09-06 M10)。足すのは「その語が出たら話題が特定できる」ものだけ。
var shortLatin = map[string]bool{
	"go": true, "ai": true, "ci": true, "ui": true, "db": true,
}

// Words は text から語を取り出す。同じ語が複数回あっても 1 回ずつ返す(出現順)。
// ラテン文字は小文字に畳む。数字だけ・ストップワード・長すぎる語は除く。
// URL の中の語(https・com など)はストップワードで落ちる範囲でしか除かないので、呼び出し側で URL を先に落としてよい。
func Words(text string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(w string) {
		if w == "" || seen[w] || IsStopword(w) {
			return
		}
		seen[w] = true
		out = append(out, w)
	}
	// ラテン文字・数字・_・- の並び(識別子を含む)
	for _, m := range latinRe.FindAllString(text, -1) {
		m = strings.Trim(m, "_-")
		lower := strings.ToLower(m)
		if n := utf8.RuneCountInString(m); (n < minLatin && !shortLatin[lower]) || len(m) > maxLatin {
			continue
		}
		add(lower)
	}
	// カタカナ連続・漢字連続
	runes := []rune(text)
	for i := 0; i < len(runes); {
		r := runes[i]
		switch {
		case isKatakana(r):
			j := i
			for j < len(runes) && isKatakana(runes[j]) {
				j++
			}
			w := strings.Trim(string(runes[i:j]), "ー")
			if n := utf8.RuneCountInString(w); n >= minKana && n <= maxKanaLn {
				add(w)
			}
			i = j
		case unicode.Is(unicode.Han, r):
			j := i
			for j < len(runes) && unicode.Is(unicode.Han, runes[j]) {
				j++
			}
			if n := j - i; n >= minKanji && n <= maxKanji {
				add(string(runes[i:j]))
			}
			i = j
		default:
			i++
		}
	}
	return out
}

// isKatakana は全角カタカナと長音。中黒(U+30FB)は含めない(語の区切りとして扱う)。
func isKatakana(r rune) bool {
	// 中黒(U+30FB)はカタカナのコードブロックに入るが、語の区切りとして扱う(決定 2026-09-03 → manual/news.md「決めたこと」)。
	// 除かないと「ファイル・フォルダ」が 1 語として残り、「ファイル」「フォルダ」が
	// どちらもストップワードなのに語彙に入る。
	if r == '・' {
		return false
	}
	// 長音(U+30FC)もこの範囲(0x30A0〜0x30FF)に入るので、別立ての判定は要らない
	// (#42 と #73 で二度指摘された冗長な分岐。TestIsKatakana_長音は範囲判定だけでtrueになる で確認済み)。
	return r >= 0x30A0 && r <= 0x30FF
}

var urlRe = regexp.MustCompile(`https?://\S+`)

// StripURLs は URL を落とす(語の抽出の前処理。ドメインやパスの断片を関心語にしない)。
func StripURLs(text string) string {
	return urlRe.ReplaceAllString(text, " ")
}

// IsStopword は関心を表さない語(埋め込みの一覧)か。
func IsStopword(w string) bool {
	return stopwords[w]
}
