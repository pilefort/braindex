package explain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile はテスト用の入力を書く。
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// mainOf は <main> の中(本文)だけを返す。ページ全体には mdhtml 共通の <script> と目次の JS が入るので、
// 本文に何が出たかを見るテストはここを見る。
func mainOf(t *testing.T, html string) string {
	t.Helper()
	i := strings.Index(html, "<main")
	j := strings.Index(html, "</main>")
	if i < 0 || j < i {
		t.Fatalf("<main> が無い:\n%s", html)
	}
	return html[i:j]
}

const sampleSVG = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 40">
  <style>.b{fill:#0072B2}</style>
  <rect class="b" x="0" y="0" width="40" height="40"><animate attributeName="width" to="80" dur="1s"/></rect>
</svg>`

// 同じ md と .svg からは 2 回生成してもバイト一致する(LLM を呼ばない・時刻を混ぜない)。
func TestRender_決定的(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fig1.svg", sampleSVG)
	md := "# 題\n\n## 節\n\n![図1: 見出しを検索単位にする](fig1.svg)\n\n### 小節\n\n本文。\n"
	a, pa := Render(md, "題", Options{BaseDir: dir})
	b, pb := Render(md, "題", Options{BaseDir: dir})
	if a != b {
		t.Error("同じ入力から違う HTML が出た")
	}
	if len(pa) != 0 || len(pb) != 0 {
		t.Errorf("問題が出た: %v %v", pa, pb)
	}
}

// ![図1: 説明](fig1.svg) は <figure> + 埋め込んだ <svg> + <figcaption> になる。alt はキャプションに出す。
func TestRender_図の埋め込み(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fig1.svg", sampleSVG)
	html, problems := Render("# 題\n\n![図1: 目次を検索単位にする](fig1.svg)\n", "題", Options{BaseDir: dir})
	if len(problems) != 0 {
		t.Fatalf("問題が出た: %v", problems)
	}
	for _, want := range []string{
		`<figure id="fig-1" class="bx-fig">`,
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 40">`,
		"<figcaption>図1: 目次を検索単位にする</figcaption>",
		"</figure>",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("%q が無い", want)
		}
	}
	if strings.Contains(html, `<img`) {
		t.Error("<img> のまま出ている(埋め込まれていない)")
	}
	if strings.Contains(html, "<?xml") {
		t.Error("XML 宣言が HTML に混ざっている")
	}
}

// 図が見つからないときは落ちずに、その場所に「図 fig1.svg が無い」と出し、問題として返す(終了コード 1 の材料)。
func TestRender_図が無い(t *testing.T) {
	html, problems := Render("# 題\n\n![図1: 説明](fig1.svg)\n", "題", Options{BaseDir: t.TempDir()})
	if !strings.Contains(html, "図 fig1.svg が無い") {
		t.Error("図の場所に印が出ていない")
	}
	if !strings.Contains(html, `<figure id="fig-1"`) {
		t.Error("figure の枠ごと消えている")
	}
	if len(problems) != 1 || problems[0] != "図 fig1.svg が無い" {
		t.Errorf("problems=%v", problems)
	}
}

// 埋め込む .svg からは <script> と on... 属性が消え、<style> と <animate> は残る。
func TestRender_埋め込みで危ないものを落とす(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fig1.svg", `<svg viewBox="0 0 10 10" onload="alert(1)">`+
		`<style>.a{fill:red}</style><script>alert(2)</script>`+
		`<rect width="10" height="10" onclick="x()"><animate attributeName="width" to="5" dur="1s"/></rect></svg>`)
	html, _ := Render("![図1: 説明](fig1.svg)\n", "題", Options{BaseDir: dir})
	body := mainOf(t, html)
	for _, ng := range []string{"<script", "onload", "onclick", "alert("} {
		if strings.Contains(body, ng) {
			t.Errorf("%q が残っている", ng)
		}
	}
	for _, want := range []string{"<style>.a{fill:red}</style>", "<animate", `attributeName="width"`} {
		if !strings.Contains(body, want) {
			t.Errorf("%q が消えている", want)
		}
	}
}

// 目次は ## と ### から作る。同じ見出しが 2 つあっても id は衝突しない(出現順の通し番号にする)。
func TestRender_目次(t *testing.T) {
	md := "# 題\n\n## 前提\n\n本文\n\n### 用語\n\n本文\n\n## 前提\n\n本文\n"
	html, _ := Render(md, "題", Options{})
	for _, want := range []string{
		`<h2 id="s1">前提</h2>`,
		`<h3 id="s1-1">用語</h3>`,
		`<h2 id="s2">前提</h2>`,
		`<li class="l2"><a href="#s1">前提</a></li>`,
		`<li class="l3"><a href="#s1-1">用語</a></li>`,
		`<li class="l2"><a href="#s2">前提</a></li>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("%q が無い\n%s", want, mainOf(t, html))
		}
	}
	if strings.Contains(html, "<h1 id=") {
		t.Error("# は目次に入れない(題名なので)")
	}
}

// 見出しが無ければ目次の枠は出さない。
func TestRender_見出しが無ければ目次を出さない(t *testing.T) {
	html, _ := Render("本文だけ\n", "題", Options{})
	if strings.Contains(mainOf(t, html), "bx-toc") {
		t.Error("空の目次が出ている")
	}
}

// 図でない画像(png・http)は今までどおり mdhtml の <img> に任せる。
func TestRender_図でない画像はそのまま(t *testing.T) {
	html, problems := Render("![外](https://example.com/a.svg)\n\n![写真](p.png)\n", "題", Options{})
	if len(problems) != 0 {
		t.Fatalf("問題が出た: %v", problems)
	}
	if strings.Count(html, "<img") != 2 {
		t.Errorf("<img> が 2 つ出ていない\n%s", mainOf(t, html))
	}
}

// 外部を読みに行く指定を HTML に出さない(自己完結)。
func TestRender_外部読み込みが無い(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fig1.svg", sampleSVG)
	html, _ := Render("# 題\n\n## 節\n\n![図1: 説明](fig1.svg)\n", "題", Options{BaseDir: dir})
	for _, ng := range []string{"<link", "<iframe", "src=\"http", "@import"} {
		if strings.Contains(html, ng) {
			t.Errorf("外部読み込み %q がある", ng)
		}
	}
}
