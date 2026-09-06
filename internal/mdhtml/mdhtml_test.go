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

// NUL は行内コードの退避に使う番兵と同じ文字なので、解析前に落とす。
// 落とさないと入力中の `\x00<数字>\x00` を退避済みコードの目印と誤認し、
// 退避表の範囲外を引いて落ちる(移植前は mdhtml.go の復帰処理で index out of range。2026-09-03 実測)。
func TestBody_NUL(t *testing.T) {
	got := Body("\x00999\x00 と `コード`\n")
	if strings.Contains(got, "\x00") {
		t.Errorf("NUL が出力に残っている: %q", got)
	}
	if want := "<p>999 と <code>コード</code></p>"; got != want {
		t.Errorf("Body = %q, want %q", got, want)
	}
}

// 退避表の範囲外を指す目印が万一残っても落とさず、そのまま出す。
func TestInline_StashOutOfRange(t *testing.T) {
	if got := inline("\x0099\x00"); got != "\x0099\x00" {
		t.Errorf("inline = %q, want 入力のまま", got)
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

// Markdown リンクの href に載せるのは http(s) と、スキームの無いもの(相対パス・#見出し)だけ。
// javascript: などをそのまま href に出すと、開いただけでコードが動く(決定 2026-09-03)。
func TestInline_リンクのスキームを絞る(t *testing.T) {
	cases := []struct{ in, want string }{
		{"[表示](javascript:alert)", "表示"}, // 括弧を含む URL は既存の正規表現が途中で切るので、ここでは使わない
		{"[表示](JavaScript:void)", "表示"},
		{"[表示](data:text/html;base64,xxx)", "表示"},
		{"[表示](https://example.com/a)", `<a href="https://example.com/a" target="_blank" rel="noopener">表示</a>`},
		{"[節へ](#見出し)", `<a href="#見出し" target="_blank" rel="noopener">節へ</a>`},
		{"[ノート](docs/notes/a.md)", `<a href="docs/notes/a.md" target="_blank" rel="noopener">ノート</a>`},
	}
	for _, c := range cases {
		if got := inline(c.in); got != c.want {
			t.Errorf("inline(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 画像記法 ![alt](src) は <img> にする。src は http(s)・相対パス・ローカルの絶対パスの 3 通り。
// ローカルの絶対パス(Windows のドライブ文字・/ 始まり)は file:// の URL にする(HTML は一時置き場に書かれ、
// Markdown と同じ場所に無い。ブラウザが "C:/..." を素のまま file と解釈するかは環境次第なので明示する)。
// 直す前は `!` が本文に残り、Windows のパスはリンクの判定にも落ちて文字だけになっていた(2026-09-06 実測)。
func TestInline_画像(t *testing.T) {
	cases := []struct{ in, want string }{
		{"![図](https://example.com/a.png)", `<img src="https://example.com/a.png" alt="図">`},
		{"![図](imgs/a.png)", `<img src="imgs/a.png" alt="図">`},
		{"![図](C:/work/a.png)", `<img src="file:///C:/work/a.png" alt="図">`},
		{`![図](C:\work\a.png)`, `<img src="file:///C:/work/a.png" alt="図">`},
		{"![図](/home/u/a.png)", `<img src="file:///home/u/a.png" alt="図">`},
		{"![](a.png)", `<img src="a.png" alt="">`},
		{"![図](a b.png)", `<img src="a%20b.png" alt="図">`},
		{`![a "b"](x.png)`, `<img src="x.png" alt="a &quot;b&quot;">`},
		{"![**太字**](x.png)", `<img src="x.png" alt="**太字**">`}, // alt の中は記法として解釈しない
		{"![`c`](x.png)", `<img src="x.png" alt="c">`},         // 退避した行内コードも文字に戻す
		{"![図](javascript:alert)", "図"},                        // リンクと同じ判定で落とす
		{"![図](data:image/png;base64,xxx)", "図"},
		{"[![図](a.png)](https://example.com/)", `<a href="https://example.com/" target="_blank" rel="noopener"><img src="a.png" alt="図"></a>`},
		{"前 ![図](a.png) 後 [b](c.md)", `前 <img src="a.png" alt="図"> 後 <a href="c.md" target="_blank" rel="noopener">b</a>`},
	}
	for _, c := range cases {
		if got := inline(c.in); got != c.want {
			t.Errorf("inline(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Windows の絶対パスはリンクでも通す(ドライブ文字はスキームではない)。
// href は画像と同じく file:// にする。素のまま出すと、file:// で開いた HTML からは
// 相対パスとして解決されて開けない(実測 2026-09-06)。
func TestInline_Windowsのパスをリンクに出す(t *testing.T) {
	cases := map[string]string{
		"[台帳](C:/work/README.md)": `<a href="file:///C:/work/README.md" target="_blank" rel="noopener">台帳</a>`,
		`[台帳](C:\work\README.md)`: `<a href="file:///C:/work/README.md" target="_blank" rel="noopener">台帳</a>`,
		"[根](/srv/x.md)":          `<a href="file:///srv/x.md" target="_blank" rel="noopener">根</a>`,
		"[隣](docs/x.md)":          `<a href="docs/x.md" target="_blank" rel="noopener">隣</a>`, // 相対はそのまま
	}
	for in, want := range cases {
		if got := inline(in); got != want {
			t.Errorf("inline(%q) = %q, want %q", in, got, want)
		}
	}
}

// 画像だけの行は段落に包む。同じ入力からは同じ出力。
func TestBody_画像(t *testing.T) {
	md := "本文\n\n![図](C:/work/a.png)\n\n- 項目 ![小](b.png)\n"
	want := "<p>本文</p>\n" +
		`<p><img src="file:///C:/work/a.png" alt="図"></p>` + "\n" +
		`<ul><li>項目 <img src="b.png" alt="小"></li></ul>`
	got := Body(md)
	if got != want {
		t.Errorf("Body:\n got: %s\nwant: %s", got, want)
	}
	if Body(md) != got {
		t.Fatal("同じ入力で出力が違う")
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
