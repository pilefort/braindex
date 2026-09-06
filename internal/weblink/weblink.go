// Package weblink は、生成物に載せてよいリンクかどうかを判定する。
//
// 判定は 1 か所に置く: Markdown → HTML(internal/mdhtml)・ニュースの選別 UI(internal/news)・
// 選別で残した見出しの記録(news/keep/YYYY-MM.md)の 3 か所で同じ規則を使うため。
// 規則を変えるときはここだけを直す(決定 2026-09-03「生成物のリンクは http(s) 以外を落とす」)。
package weblink

import "strings"

// Safe は生成物の href や記録に載せてよいリンクかを返す。
//
// 許すのは 2 つだけ:
//   - http:// と https:// で始まる絶対 URL
//   - スキームを持たないもの(相対パス・"#見出し" のような同一文書内リンク)
//
// javascript: や data: のように、開いただけでコードが動きうるスキームを落とすのが目的なので、
// スキームの無いリンク(文書内リンク・相対パス)は通す。mailto: や file: のような
// 他のスキームも落とす(生成物は「読むための HTML」で、外部アプリを起こす必要が無い)。
//
// Windows の絶対パス("C:/x/y.png"・"C:\x\y.png")は、コロンの前が 1 文字なのでスキームではなく
// ドライブ文字として扱い、相対パスと同じく通す。1 文字のスキームは URL に実在しない。
// (2026-09-06 に、画像とリンクの Windows パスが「スキーム c」と誤判定されて落ちていたのを直した)
func Safe(u string) bool {
	s := strings.TrimSpace(u)
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return true // スキームが無い(相対パス・#見出し)
	}
	// "://" より前に / ? # があれば、それはスキーム区切りのコロンではない
	// (例 "docs/a:b.md" や "?q=a:b")。その場合もスキーム無しとして扱う。
	if j := strings.IndexAny(s, "/?#"); j >= 0 && j < i {
		return true
	}
	if i == 1 && isASCIILetter(s[0]) {
		return true // Windows のドライブ文字
	}
	scheme := strings.ToLower(s[:i])
	return scheme == "http" || scheme == "https"
}

func isASCIILetter(c byte) bool { return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') }
