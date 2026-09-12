package mdhtml

import (
	"regexp"
	"strings"
	"testing"
)

// 視認性の下限。WCAG 2.1 の AA に合わせる(利用者の指摘 2026-09-12「視認性の悪い色や、
// コントラストの低い色と背景の組み合わせを使わないで欲しい」)。
const (
	minText    = 4.5 // 本文・小さい文字
	minGraphic = 3.0 // 線・図形・大きい文字
)

// varRE は CSS から --名前:#色 を拾う。
var varRE = regexp.MustCompile(`--([a-z0-9-]+):(#[0-9a-fA-F]{3,6})`)

// themeColors は css の :root(明) と :root[data-theme="dark"](暗) の変数を読む。
// 定数を Go 側に書き写すと、CSS だけ直したときにテストが素通りするので、CSS そのものから読む。
func themeColors(t *testing.T) (light, dark map[string]string) {
	t.Helper()
	i := strings.Index(css, `:root[data-theme="dark"]`)
	if i < 0 {
		t.Fatal("暗い配色の指定が見つからない")
	}
	read := func(s string) map[string]string {
		m := map[string]string{}
		for _, kv := range varRE.FindAllStringSubmatch(s, -1) {
			m[kv[1]] = kv[2]
		}
		return m
	}
	light, dark = read(css[:i]), read(css[i:])
	if len(light) < 8 || len(dark) < 8 {
		t.Fatalf("変数が読めていない: 明 %d 件 / 暗 %d 件", len(light), len(dark))
	}
	return light, dark
}

// 文字と背景の組み合わせは、明・暗のどちらでも 4.5 以上にする。
func TestContrast_文字と背景(t *testing.T) {
	light, dark := themeColors(t)
	pairs := []struct{ fg, bg, where string }{
		{"ink", "bg", "本文"},
		{"ink", "panel", "枠の中の本文"},
		{"ink", "line2", "表の見出し・縞"},
		{"sub", "bg", "補足・図の説明"},
		{"sub", "panel", "枠の中の補足"},
		{"sub", "accent-soft", "引用の中の文字"},
		{"mut", "bg", "目盛り・薄い文字"},
		{"mut", "panel", "枠の中の薄い文字"},
		{"accent", "bg", "リンク"},
		{"accent", "panel", "枠の中のリンク"},
		{"accent", "accent-soft", "目次の現在地"},
		{"on-accent", "accent", "色の上に載せる文字(新着の印)"},
	}
	for name, theme := range map[string]map[string]string{"明": light, "暗": dark} {
		for _, p := range pairs {
			fg, ok1 := theme[p.fg]
			bg, ok2 := theme[p.bg]
			if !ok1 || !ok2 {
				t.Errorf("%s: 変数が無い --%s / --%s", name, p.fg, p.bg)
				continue
			}
			if c := Contrast(fg, bg); c < minText {
				t.Errorf("%s %s: --%s(%s) と --%s(%s) の比が %.2f(%.1f 以上にする)",
					name, p.where, p.fg, fg, p.bg, bg, c, minText)
			}
		}
	}
}

// 枠線のような「文字でないもの」も、背景との差が無いと形が見えない。
func TestContrast_線と背景(t *testing.T) {
	light, dark := themeColors(t)
	for name, theme := range map[string]map[string]string{"明": light, "暗": dark} {
		if c := Contrast(theme["line"], theme["bg"]); c >= minGraphic {
			continue // 見えていれば何も言わない
		} else if c < 1.08 {
			// 枠線は薄くてよいが、まったく差が無いと枠が消える
			t.Errorf("%s: --line(%s) と --bg(%s) の差が %.2f しかない", name, theme["line"], theme["bg"], c)
		}
	}
}
