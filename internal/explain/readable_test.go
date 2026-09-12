package explain

import (
	"strings"
	"testing"
)

// 本文の「図1」は図へのリンクになる。無い番号は素のまま。figcaption の中の「図1:」は自分へのリンクにしない。
func TestRender_図番号からリンクする(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fig1.svg", sampleSVG)
	md := "# 題\n\n図1 のとおり。図 1 も同じ。図9 は無い。\n\n![図1: 目次](fig1.svg)\n\n次の節でも図1を使う。\n"
	body := mainOf(t, mustRender(t, md, Options{BaseDir: dir}))
	if n := strings.Count(body, `<a class="bx-ref" href="#fig-1">`); n != 3 {
		t.Errorf("図1 へのリンクが %d 個(3 個のはず)\n%s", n, body)
	}
	if !strings.Contains(body, `href="#fig-1">図 1</a>`) {
		t.Error("「図 1」(間に空白)がリンクになっていない")
	}
	if strings.Contains(body, `href="#fig-9"`) {
		t.Error("存在しない図9 にリンクを張っている")
	}
	fig := body[strings.Index(body, "<figure"):]
	if strings.Contains(fig[:strings.Index(fig, "</figure>")], "bx-ref") {
		t.Error("figcaption の図番号が自分へのリンクになっている")
	}
}

// 参照する図が 1 枚も無ければ、本文の「図1」はそのまま(リンクにしない)。
func TestRender_図が無ければ番号を触らない(t *testing.T) {
	body := mainOf(t, mustRender(t, "図1 を見よ。\n", Options{}))
	if strings.Contains(body, "bx-ref") {
		t.Errorf("図が無いのにリンクを張った: %s", body)
	}
}

// 表は横スクロールできる箱に入れる(広い表が本文の幅を押し広げないように)。
func TestRender_表を横スクロールの箱に入れる(t *testing.T) {
	md := "| 手法 | Recall |\n|---|---|\n| BM25 | 41.2 |\n"
	body := mainOf(t, mustRender(t, md, Options{}))
	if !strings.Contains(body, `<div class="bx-tw"><table>`) || !strings.Contains(body, "</table></div>") {
		t.Errorf("表が箱に入っていない: %s", body)
	}
}

// コードブロックの中の表らしき文字は箱に入れない(本文の文字はエスケープ済みなので取り違えない)。
func TestRender_コードの中の表は触らない(t *testing.T) {
	body := mainOf(t, mustRender(t, "```\n<table>\n```\n", Options{}))
	if strings.Contains(body, "bx-tw") {
		t.Errorf("コードの中を表として扱った: %s", body)
	}
}

// 読む幅と、動きを止める指定が HTML に入っている(実際の見え方はブラウザ検査で見る)。
func TestRender_読みやすさの指定(t *testing.T) {
	html := mustRender(t, "# 題\n\n本文\n", Options{})
	if !strings.HasSuffix(measure, "rem") {
		// em は要素自身の文字の大きさが基準なので、見出しだけ行長が広がって右端がそろわない
		t.Errorf("行長の単位が rem でない: %s", measure)
	}
	for _, want := range []string{
		".doc.explain p,", "max-width:" + measure, // 本文の行長
		"word-break:auto-phrase",                 // 日本語を語句の切れ目で折り返す
		"@media (prefers-reduced-motion:reduce)", // CSS のアニメーション
		"pauseAnimations",                        // SVG の <animate>(SMIL)
		".bx-tw{overflow-x:auto",                 // 表の横スクロール
	} {
		if !strings.Contains(html, want) {
			t.Errorf("%q が無い", want)
		}
	}
}

func mustRender(t *testing.T, md string, opt Options) string {
	t.Helper()
	html, problems := Render(md, "題", opt)
	if len(problems) != 0 {
		t.Fatalf("問題が出た: %v", problems)
	}
	return html
}
