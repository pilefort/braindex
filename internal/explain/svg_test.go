package explain

import (
	"strings"
	"testing"
)

// <svg> の前後にある宣言は落とし、中身はそのまま通す。
func TestSanitizeSVG_取り出す範囲(t *testing.T) {
	got := SanitizeSVG("<?xml version=\"1.0\"?>\n<!DOCTYPE svg>\n<svg viewBox=\"0 0 1 1\"><rect/></svg>\n")
	want := `<svg viewBox="0 0 1 1"><rect/></svg>`
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
	if SanitizeSVG("これは SVG ではない") != "" {
		t.Error("<svg> が無いのに中身を返した")
	}
}

// <script> は中身ごと消える。on... で始まる属性も消える。<style>・<animate> は残る。
func TestSanitizeSVG_落とすものと残すもの(t *testing.T) {
	in := `<svg onload="a()" viewBox="0 0 4 4">` +
		`<style>.a{fill:red}</style>` +
		`<script type="text/javascript">var x = 1 < 2;</script>` +
		`<rect ONCLICK='b()' width="4"><animate attributeName="width" to="2"/></rect>` +
		`<script/>` +
		`</svg>`
	got := SanitizeSVG(in)
	for _, ng := range []string{"script", "onload", "ONCLICK", "a()", "b()", "var x"} {
		if strings.Contains(got, ng) {
			t.Errorf("%q が残っている: %s", ng, got)
		}
	}
	for _, want := range []string{`<svg viewBox="0 0 4 4">`, "<style>.a{fill:red}</style>",
		`<rect width="4">`, `<animate attributeName="width" to="2"/>`} {
		if !strings.Contains(got, want) {
			t.Errorf("%q が消えている: %s", want, got)
		}
	}
}

// 属性値の中の > や、コメント・CDATA の中の > でタグの終わりを見誤らない
// (引用符の中の > は HTML でもそのまま置ける)。
func TestSanitizeSVG_終端の見誤り(t *testing.T) {
	in := `<svg viewBox="0 0 4 4"><!-- a > b --><text font-family="a>b">x</text>` +
		`<style><![CDATA[.a{content:">"}]]></style></svg>`
	got := SanitizeSVG(in)
	for _, want := range []string{"<!-- a > b -->", `font-family="a>b"`, `<![CDATA[.a{content:">"}]]>`, ">x</text>"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q が無い: %s", want, got)
		}
	}
}

// 閉じない <script> は末尾まで落とす(途中で戻ると中身が出てしまう)。
func TestSanitizeSVG_閉じないscript(t *testing.T) {
	got := SanitizeSVG(`<svg><script>alert(1)</svg>`)
	if strings.Contains(got, "alert") {
		t.Errorf("中身が残っている: %s", got)
	}
}

// 同じ入力からは同じ出力(属性の並びを組み直しても順番は変えない)。
func TestSanitizeSVG_決定的(t *testing.T) {
	in := `<svg a="1" b='2' c viewBox="0 0 1 1"><g fill="red" stroke="blue"/></svg>`
	first := SanitizeSVG(in)
	for i := 0; i < 3; i++ {
		if SanitizeSVG(in) != first {
			t.Fatal("同じ入力から違う出力が出た")
		}
	}
	if !strings.Contains(first, `<svg a="1" b="2" c viewBox="0 0 1 1">`) {
		t.Errorf("属性の並びが変わっている: %s", first)
	}
}
