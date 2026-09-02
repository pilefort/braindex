package mdhtml

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// -update で golden を今の出力で書き直す(差分は git diff で見る)。
var update = flag.Bool("update", false, "golden ファイルを更新する")

func readSample(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "sample.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		gl, wl := strings.Split(got, "\n"), strings.Split(string(want), "\n")
		for i := 0; i < len(gl) || i < len(wl); i++ {
			var g, w string
			if i < len(gl) {
				g = gl[i]
			}
			if i < len(wl) {
				w = wl[i]
			}
			if g != w {
				t.Fatalf("%s の %d 行目が違う\n got: %s\nwant: %s", name, i+1, g, w)
			}
		}
		t.Fatalf("%s が違う", name)
	}
}

// 本文の golden は原型 md_to_html.py(Python)の出力から作った(2026-09-03)。移植が原型と一致することの検証を兼ねる。
func TestBody_Golden(t *testing.T) {
	checkGolden(t, "sample.body.html", Body(readSample(t))+"\n")
}

func TestPage_Golden(t *testing.T) {
	md := readSample(t)
	checkGolden(t, "sample.page.html", Page(md, ExtractTitle(md, "sample.md")))
}

// 同じ入力からは同じバイト列(決定性)。
func TestPage_Deterministic(t *testing.T) {
	md := readSample(t)
	a := Page(md, ExtractTitle(md, "sample.md"))
	b := Page(md, ExtractTitle(md, "sample.md"))
	if a != b {
		t.Fatal("同じ入力で出力が違う")
	}
}

func TestExtractTitle(t *testing.T) {
	cases := []struct{ md, file, want string }{
		{"# 見出し  \n本文", "a.md", "見出し"},
		{"本文\n\n# 後の見出し\n", "a.md", "後の見出し"},
		{"## 二段目だけ\n", "memo.md", "memo"},
		{"", "memo.txt", "memo.txt"},
	}
	for _, c := range cases {
		if got := ExtractTitle(c.md, c.file); got != c.want {
			t.Errorf("ExtractTitle(%q, %q) = %q, want %q", c.md, c.file, got, c.want)
		}
	}
}

func TestBody_CRLF(t *testing.T) {
	if Body("a\r\nb\r\n\r\n- c\r\n") != Body("a\nb\n\n- c\n") {
		t.Fatal("CRLF と LF で出力が違う")
	}
}

func TestInline_Italic(t *testing.T) {
	cases := []struct{ in, want string }{
		{"*a*", "<em>a</em>"},
		{"2*3*4", "2*3*4"},
		{"**b** と *i*", "<strong>b</strong> と <em>i</em>"},
		{"x *", "x *"},
		{"**", "**"},
		{"*a**", "*a**"},
		{"日本語*強調*", "日本語*強調*"},
		{"(*a*)", "(<em>a</em>)"},
	}
	for _, c := range cases {
		if got := inline(c.in); got != c.want {
			t.Errorf("inline(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 原型の linkify は Markdown リンクの href の中まで再リンクして HTML を壊した。移植ではタグと <a>・<code>・<pre> の中を触らない。
func TestLinkify(t *testing.T) {
	in := `<p>see <a href="https://u.example/x" target="_blank" rel="noopener">t</a> and https://v.example/y <code>https://c.example/</code></p>` +
		`<pre><code>https://p.example/</code></pre>`
	want := `<p>see <a href="https://u.example/x" target="_blank" rel="noopener">t</a> and ` +
		`<a href="https://v.example/y" target="_blank" rel="noopener">https://v.example/y</a> <code>https://c.example/</code></p>` +
		`<pre><code>https://p.example/</code></pre>`
	if got := Linkify(in); got != want {
		t.Fatalf("Linkify:\n got: %s\nwant: %s", got, want)
	}
}

func TestPage_NoExternalLoads(t *testing.T) {
	p := Page(readSample(t), "t")
	for _, bad := range []string{"<link ", "src=\"http", "EventSource", "@import"} {
		if strings.Contains(p, bad) {
			t.Errorf("外部読み込み・常駐サーバ向けの記述 %q を含む", bad)
		}
	}
	if !strings.Contains(p, `<input type="checkbox" checked>`) || strings.Contains(p, "disabled") {
		t.Error("Page のチェックボックスはクリック可能(disabled 無し)であるべき")
	}
}
