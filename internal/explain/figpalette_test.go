package explain

import (
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/mdhtml"
)

// figNames は図が使える色の名前。図の書き手はこの名前を var(--fg) のように書くだけでよく、
// 色の値は書かない。名前を増やすときはここにも足す——図の書き手に見せる約束なので、
// CSS だけ直しても伝わらない。
var figNames = []string{
	"fg", "fg2", "mono", "edge", "groove", "surf",
	"c1", "c2", "c3", "c4",
	"s1", "s2", "s3", "s4",
}

var (
	// figBlockRE は図のパレットの定義。1 つ目の括りが空でなければ暗い配色の分。
	figBlockRE  = regexp.MustCompile(`(?s)(:root\[data-theme="dark"\] )?\.bx-fig svg\.bxfig\{([^}]*)\}`)
	rootLightRE = regexp.MustCompile(`(?s):root\{([^}]*)\}`)
	rootDarkRE  = regexp.MustCompile(`(?s):root\[data-theme="dark"\]\{([^}]*)\}`)
	declRE      = regexp.MustCompile(`--([a-z0-9-]+):([^;}]+)`)
	varRefRE    = regexp.MustCompile(`^var\(--([a-z0-9-]+)\)$`)
)

// figPalette は生成したページから図のパレットを読み、明・暗それぞれの実際の色を返す。
// 本文の変数への別名(var(--ink) など)はその配色の値に置き換える。
// 値を Go 側に書き写すと CSS だけ直したときにテストが素通りするので、生成物から読む。
func figPalette(t *testing.T, html string) (light, dark map[string]string) {
	t.Helper()
	readVars := func(s string) map[string]string {
		m := map[string]string{}
		for _, kv := range declRE.FindAllStringSubmatch(s, -1) {
			m[kv[1]] = strings.TrimSpace(kv[2])
		}
		return m
	}
	group := func(re *regexp.Regexp) map[string]string {
		m := re.FindStringSubmatch(html)
		if m == nil {
			t.Fatalf("%s が見つからない", re)
		}
		return readVars(m[1])
	}
	baseLight, baseDark := group(rootLightRE), group(rootDarkRE)

	var figLight, figDark map[string]string
	for _, m := range figBlockRE.FindAllStringSubmatch(html, -1) {
		if m[1] == "" {
			figLight = readVars(m[2])
		} else {
			figDark = readVars(m[2])
		}
	}
	if figLight == nil || figDark == nil {
		t.Fatal("図のパレットが明・暗の 2 組そろっていない")
	}

	resolve := func(v string, base map[string]string) string {
		if r := varRefRE.FindStringSubmatch(v); r != nil {
			if got, ok := base[r[1]]; ok {
				return got
			}
			t.Errorf("別名の先が本文に無い: %s", v)
		}
		return v
	}
	light, dark = map[string]string{}, map[string]string{}
	for k, v := range figLight {
		light[k] = resolve(v, baseLight)
		dark[k] = resolve(v, baseDark) // 別名は暗い配色では暗い側の値になる
	}
	for k, v := range figDark {
		dark[k] = resolve(v, baseDark)
	}
	light["bg"], dark["bg"] = baseLight["bg"], baseDark["bg"]
	return light, dark
}

// figPage は図を 1 枚埋めたページを返す。
func figPage(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "fig1.svg", `<svg class="bxfig" viewBox="0 0 10 10" xmlns="http://www.w3.org/2000/svg">`+
		`<text class="t" x="0" y="8" fill="var(--fg)">あ</text></svg>`)
	html, problems := Render("# 題\n\n![図1: 例](fig1.svg)\n", "題", Options{BaseDir: dir})
	if len(problems) != 0 {
		t.Fatalf("問題が出た: %v", problems)
	}
	return html
}

// 図が使う名前は、明るい配色ですべて定義されている。1 つでも欠けると、
// その名前を書いた図は色が付かないまま出る(var() が解決できないと黒に落ちる)。
func TestFigPalette_名前がすべて定義されている(t *testing.T) {
	light, dark := figPalette(t, figPage(t))
	for _, n := range figNames {
		if light[n] == "" {
			t.Errorf("明るい配色に --%s が無い", n)
		}
		if dark[n] == "" {
			t.Errorf("暗い配色に --%s が無い", n)
		}
	}
	// 余分な系列色を出さない。名前の一覧に載っていない色は、書き手が知らないまま増える
	for _, pal := range []map[string]string{light, dark} {
		for k := range pal {
			if (strings.HasPrefix(k, "c") || strings.HasPrefix(k, "s")) && !contains(figNames, k) {
				t.Errorf("名前の一覧に無い色が出ている: --%s", k)
			}
		}
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// 図の色は、明るい配色でも暗い配色でも背景から浮く。
// 下限は文字 4.5・図形 3.0(WCAG 2.1 の AA)。この検査が無いと、2026-09-12 に起きた事故
// ——暗い配色前提の図を明るい本文に入れて比が 1.13 になる——が、今度は本文側の値で再発する。
func TestFigPalette_背景から浮く(t *testing.T) {
	light, dark := figPalette(t, figPage(t))
	for name, pal := range map[string]map[string]string{"明るい配色": light, "暗い配色": dark} {
		for _, c := range []struct {
			names []string
			min   float64
			what  string
		}{
			// 系列の色は短い見出しの文字にも使うので、図形ではなく文字の下限で見る
			// (2026-09-12 の決定。棒グラフの色を流用していたときは 3.19〜3.61 だった)
			{[]string{"fg", "fg2", "mono", "c1", "c2", "c3", "c4"}, 4.5, "文字"},
			{[]string{"edge"}, 3.0, "図形"},
		} {
			for _, n := range c.names {
				if r := mdhtml.Contrast(pal[n], pal["bg"]); r < c.min {
					t.Errorf("%s: %s の --%s(%s) と背景(%s) の比が %.2f(%.1f 以上にする)",
						name, c.what, n, pal[n], pal["bg"], r, c.min)
				}
			}
		}
	}
}

// 図の系列どうしも、色覚の型が違って見分けられる。判定は本文のグラフと同じ
// (隔たり 38 以上、または明るさ比 1.5 以上。基準の出どころは contrast_test.go)。
func TestFigPalette_系列どうしが見分けられる(t *testing.T) {
	light, dark := figPalette(t, figPage(t))
	names := []string{"c1", "c2", "c3", "c4"}
	for name, pal := range map[string]map[string]string{"明るい配色": light, "暗い配色": dark} {
		for i := 0; i < len(names); i++ {
			for j := i + 1; j < len(names); j++ {
				a, b := pal[names[i]], pal[names[j]]
				d := math.Min(cvdDistance(a, b, protanope), cvdDistance(a, b, deuteranope))
				if l := mdhtml.Contrast(a, b); d < 38 && l < 1.5 {
					t.Errorf("%s: --%s(%s) と --%s(%s) が見分けにくい(隔たり %.0f・明るさ比 %.2f)",
						name, names[i], a, names[j], b, d, l)
				}
			}
		}
	}
}

// 本文にもある色は本文の変数への別名にする。値を書くと、本文の配色を直したときに図だけ取り残される。
func TestFigPalette_本文にある色は別名にする(t *testing.T) {
	html := figPage(t)
	m := figBlockRE.FindStringSubmatch(html)
	if m == nil {
		t.Fatal("図のパレットが無い")
	}
	for _, n := range []string{"fg", "fg2", "edge", "groove", "surf"} {
		want := "--" + n + ":var(--"
		if !strings.Contains(m[2], want) {
			t.Errorf("--%s が本文の変数への別名になっていない: %s", n, m[2])
		}
	}
}

// 面(--s1〜)は系列の色を薄くしたもので、同じ色から作る。別の値を持つと、枠線と面の色がずれる。
func TestFigPalette_面は系列の色から作る(t *testing.T) {
	light, dark := figPalette(t, figPage(t))
	for name, pal := range map[string]map[string]string{"明るい配色": light, "暗い配色": dark} {
		for i := 1; i <= 4; i++ {
			c := pal["c"+string(rune('0'+i))]
			s := pal["s"+string(rune('0'+i))]
			if !strings.HasPrefix(s, "rgba("+rgbOf(c)+",") {
				t.Errorf("%s: --s%d(%s) が --c%d(%s) と違う色から来ている", name, i, s, i, c)
			}
		}
	}
}

// rgbOf は #rrggbb を "r,g,b" にする(rgba() の中身と突き合わせるため)。
func rgbOf(hex string) string {
	s := rgba(hex, 1)
	return strings.TrimSuffix(strings.TrimPrefix(s, "rgba("), ",1)")
}
