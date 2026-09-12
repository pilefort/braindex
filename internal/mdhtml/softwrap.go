package mdhtml

import (
	"strings"
	"unicode/utf8"
)

// joinLines は段落・引用の中の行をつなぐ。
//
// 既定(Options.SoftWrap が false)は 1 行 1 <br>。原型の answer_html.py からの動きで、
// 回答の md はそれを前提に書かれている。
//
// SoftWrap のときは、エディタの折り返しで切れた行を次の行と続け、文の終わりの改行だけ <br> にする。
// 長い解説の md は 1 段落が 100 文字くらいで折り返して書かれるので、全部を <br> にすると
// 出来上がりの行長(40 文字)と合わず、文の途中で不自然に切れる(2026-09-12 の指摘)。
func joinLines(html, raw []string, opt Options) string {
	if !opt.SoftWrap {
		return strings.Join(html, "<br>")
	}
	var b strings.Builder
	for i, s := range html {
		if i > 0 {
			b.WriteString(softSep(raw[i-1], raw[i]))
		}
		b.WriteString(s)
	}
	return b.String()
}

// softSep は前の行と次の行のつなぎ方を返す。
//
//   - 行末に空白 2 つを置いた行の後ろは <br>(Markdown の「ここで改行」の書き方)
//   - 文の終わり(。．！？!? ：: と、その後ろの閉じ記号)で終わる行の後ろも <br>。
//     解説の md では「結論:／理由:／根拠:」のように 1 行 1 文で書く箇所があり、そこは改行を残す
//   - それ以外は続ける。日本語どうしは詰め、英数字が絡むときだけ空白を 1 つ入れる
//
// 空白を自分で決めるのは、ブラウザが日本語の間の改行も空白にするため(Chromium 151 で実測
// 2026-09-12。「あいうえお\nかきくけこ」は 5px 広くなった)。改行のまま出すと字間が空く。
func softSep(prev, next string) string {
	if strings.HasSuffix(prev, "  ") {
		return "<br>"
	}
	p := strings.TrimSpace(prev)
	if endsSentence(p) {
		return "<br>"
	}
	last, _ := utf8.DecodeLastRuneInString(p)
	first, _ := utf8.DecodeRuneInString(strings.TrimSpace(next))
	if isWide(last) && isWide(first) {
		return ""
	}
	return " "
}

// endsSentence は行が文の終わりで終わっているかを見る。閉じ括弧・引用符は先に外す。
func endsSentence(s string) bool {
	s = strings.TrimRight(s, "」』）】〉》\"'”’)*_`")
	r, _ := utf8.DecodeLastRuneInString(s)
	switch r {
	case '。', '．', '！', '？', '!', '?', '：', ':':
		return true
	}
	return false
}

// isWide は全角の文字(かな・漢字・全角記号)かを返す。East Asian Width の W/F をおおまかに見る。
// 正確な表は要らない——日本語どうしの境目で空白を入れないための判定なので、
// かな・漢字・和文の記号を拾えれば足りる。
func isWide(r rune) bool {
	switch {
	case r >= 0x3000 && r <= 0x30FF: // 和文の記号・ひらがな・カタカナ
		return true
	case r >= 0x3400 && r <= 0x4DBF: // 漢字(拡張 A)
		return true
	case r >= 0x4E00 && r <= 0x9FFF: // 漢字
		return true
	case r >= 0xF900 && r <= 0xFAFF: // 漢字(互換)
		return true
	case r >= 0xFF01 && r <= 0xFF60: // 全角英数・記号
		return true
	case r >= 0xFFE0 && r <= 0xFFE6: // 全角の通貨記号など
		return true
	case r >= 0x20000 && r <= 0x2FA1F: // 漢字(拡張 B 以降)
		return true
	}
	return false
}
