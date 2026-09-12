package mdhtml

import (
	"strings"
	"testing"
)

// 既定は今までどおり 1 行 1 <br>(answer の md の書き方を変えない)。
func TestSoftWrap_既定は改行をそのまま出す(t *testing.T) {
	got := Body("あいうえお\nかきくけこ\n")
	if got != "<p>あいうえお<br>かきくけこ</p>" {
		t.Errorf("got=%q", got)
	}
}

// SoftWrap では、文の途中で切れた行を次の行と続ける。
// 日本語どうしは詰め、英数字が絡むときだけ空白を 1 つ入れる。
func TestSoftWrap_文の途中の改行はつなぐ(t *testing.T) {
	cases := []struct {
		md   string
		want string
	}{
		{"解説の md 1 本と、隣に置いた図の\n.svg から HTML が出ます\n",
			"<p>解説の md 1 本と、隣に置いた図の .svg から HTML が出ます</p>"},
		{"文章と図は\n私が書きます\n", "<p>文章と図は私が書きます</p>"},
		{"a long english\nsentence here\n", "<p>a long english sentence here</p>"},
		{"日本語のあとに English\nが続く行\n", "<p>日本語のあとに English が続く行</p>"},
	}
	for _, c := range cases {
		if got := BodyWith(c.md, Options{SoftWrap: true}); got != c.want {
			t.Errorf("md=%q\n got=%q\nwant=%q", c.md, got, c.want)
		}
	}
}

// 文の終わりで終わる行の後ろは改行を残す(「結論:／理由:／根拠:」のような 1 行 1 文の書き方を壊さない)。
func TestSoftWrap_文の終わりは改行を残す(t *testing.T) {
	md := "結論: 目次を入力に入れる。\n理由: 構造を覚えなくてよい。\n根拠: 論文の表 4\n"
	got := BodyWith(md, Options{SoftWrap: true})
	if strings.Count(got, "<br>") != 2 {
		t.Errorf("<br> が 2 つでない: %q", got)
	}
}

// 行末の空白 2 つは、どこでも改行にする(Markdown の書き方)。
func TestSoftWrap_行末の空白2つで改行(t *testing.T) {
	got := BodyWith("ここで切る  \n次の行\n", Options{SoftWrap: true})
	if got != "<p>ここで切る<br>次の行</p>" {
		t.Errorf("got=%q", got)
	}
}

// 引用の中でも同じように扱う。
func TestSoftWrap_引用(t *testing.T) {
	got := BodyWith("> 引用の途中で\n> 折り返した行。\n> 次の文\n", Options{SoftWrap: true})
	if got != "<blockquote>引用の途中で折り返した行。<br>次の文</blockquote>" {
		t.Errorf("got=%q", got)
	}
}

// 閉じ括弧や強調の記号が後ろに付いていても、文の終わりと分かる。
func TestSoftWrap_文末の記号(t *testing.T) {
	cases := map[string]bool{
		"終わりです。":           true,
		"終わりです。）":          true,
		"**強調した文です。**":     true,
		"見てほしいのは次の 3 点です:": true,
		"途中の行なので":          false,
		"0.05":             false, // 小数点は文の終わりではない
	}
	for in, want := range cases {
		if got := endsSentence(in); got != want {
			t.Errorf("endsSentence(%q)=%v want %v", in, got, want)
		}
	}
}
