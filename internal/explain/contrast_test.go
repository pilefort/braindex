package explain

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/mdhtml"
)

// 明・暗それぞれの背景。mdhtml の :root と :root[data-theme="dark"] の --bg と同じ値。
const (
	lightBG = "#f6f7f9"
	darkBG  = "#12151b"
)

// グラフの色は背景から浮く。下限は 3.0（WCAG 2.1 の図形の基準）。
// 利用者の指摘 2026-09-12「視認性の悪い色や、コントラストの低い色と背景の組み合わせを使わないで欲しい」。
func TestGraphColors_背景から浮く(t *testing.T) {
	for _, set := range []struct {
		name, bg string
		colors   []string
	}{
		{"明るい配色", lightBG, graphColorsLight},
		{"暗い配色", darkBG, graphColorsDark},
	} {
		for _, c := range set.colors {
			if r := mdhtml.Contrast(c, set.bg); r < 3.0 {
				t.Errorf("%s: %s と背景 %s の比が %.2f(3.0 以上にする)", set.name, c, set.bg, r)
			}
		}
	}
}

// 明るい配色と暗い配色は同じ数の色を持つ。seriesClass が色の数で割るので、ずれると片方が足りなくなる。
func TestGraphColors_同じ数(t *testing.T) {
	if len(graphColorsLight) != len(graphColorsDark) {
		t.Fatalf("色の数が違う: 明 %d / 暗 %d", len(graphColorsLight), len(graphColorsDark))
	}
}

// 系列どうしは、色覚の違いがあっても見分けられる。
//
// 判定は 2 つのどちらかを満たすこと: (1) P 型・D 型の見え方を模擬した色の隔たりが 38 以上、
// (2) 明るさの比が 1.5 以上。基準の値は参照している Okabe-Ito の 5 色（#0072B2・#D55E00・#009E73・
// #CC79A7・#56B4E9）を測って決めた。その並びで最も近いのは青と赤紫で、隔たり 38・明るさ比 1.69 だった。
// つまりこのテストは「参照の並びと同じくらいは離れている」ことを見る。
func TestGraphColors_系列どうしが見分けられる(t *testing.T) {
	for _, set := range []struct {
		name   string
		colors []string
	}{
		{"明るい配色", graphColorsLight},
		{"暗い配色", graphColorsDark},
	} {
		for i := 0; i < len(set.colors); i++ {
			for j := i + 1; j < len(set.colors); j++ {
				a, b := set.colors[i], set.colors[j]
				d := math.Min(cvdDistance(a, b, protanope), cvdDistance(a, b, deuteranope))
				l := mdhtml.Contrast(a, b)
				if d < 38 && l < 1.5 {
					t.Errorf("%s: %s と %s が見分けにくい(色の隔たり %.0f・明るさ比 %.2f)",
						set.name, a, b, d, l)
				}
			}
		}
	}
}

// 参照にしている Okabe-Ito の並びが、上のテストの基準を満たすことを確かめる
// (基準をそこから決めたので、緩めすぎ・厳しすぎの歯止めになる)。
func TestGraphColors_基準は参照の並びに合わせてある(t *testing.T) {
	ref := []string{"#0072B2", "#D55E00", "#009E73", "#CC79A7", "#56B4E9"}
	minD := math.Inf(1)
	for i := 0; i < len(ref); i++ {
		for j := i + 1; j < len(ref); j++ {
			a, b := ref[i], ref[j]
			d := math.Min(cvdDistance(a, b, protanope), cvdDistance(a, b, deuteranope))
			minD = math.Min(minD, d)
			if d < 38 && mdhtml.Contrast(a, b) < 1.5 {
				t.Errorf("参照の並びが基準を満たさない: %s と %s(隔たり %.0f)", a, b, d)
			}
		}
	}
	// 参照の並びで最も近い 2 色の隔たりが基準そのもの。ここが大きく離れたら基準が緩すぎ・厳しすぎ
	if minD < 35 || minD > 45 {
		t.Errorf("参照の並びの最小の隔たりが %.0f。テストの基準(38)を見直す", minD)
	}
}

// cvdDistance は 2 色を色覚の型ごとの見え方に写してから、RGB の隔たりを測る。
// 写し方は Viénot ら(1999)の線形近似。
func cvdDistance(a, b string, m [3][3]float64) float64 {
	ar, ag, ab := simulate(a, m)
	br, bg, bb := simulate(b, m)
	dr, dg, db := ar-br, ag-bg, ab-bb
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

var (
	// protanope は P 型(赤の錐体が無い)、deuteranope は D 型(緑の錐体が無い)の見え方。
	protanope   = [3][3]float64{{0, 2.02344, -2.52581}, {0, 1, 0}, {0, 0, 1}}
	deuteranope = [3][3]float64{{1, 0, 0}, {0.494207, 0, 1.24827}, {0, 0, 1}}

	rgbToLMS = [3][3]float64{
		{17.8824, 43.5161, 4.11935},
		{3.45565, 27.1554, 3.86714},
		{0.0299566, 0.184309, 1.46709},
	}
	lmsToRGB = [3][3]float64{
		{0.080944, -0.130504, 0.116721},
		{-0.0102485, 0.0540194, -0.113615},
		{-0.000365294, -0.00412163, 0.693513},
	}
)

// simulate は色を、その型の見え方に写した 0〜255 の RGB で返す。
func simulate(hex string, m [3][3]float64) (float64, float64, float64) {
	v := toLinear(hex)
	v = apply(rgbToLMS, v)
	v = apply(m, v)
	v = apply(lmsToRGB, v)
	out := [3]float64{}
	for i, x := range v {
		out[i] = toSRGB(x) * 255
	}
	return out[0], out[1], out[2]
}

func apply(m [3][3]float64, v [3]float64) [3]float64 {
	var out [3]float64
	for r := 0; r < 3; r++ {
		out[r] = m[r][0]*v[0] + m[r][1]*v[1] + m[r][2]*v[2]
	}
	return out
}

func toLinear(hex string) [3]float64 {
	h := strings.TrimPrefix(hex, "#")
	var out [3]float64
	for i := 0; i < 3; i++ {
		n, err := strconv.ParseInt(h[i*2:i*2+2], 16, 0)
		if err != nil {
			continue
		}
		c := float64(n) / 255
		if c <= 0.04045 {
			out[i] = c / 12.92
		} else {
			out[i] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}
	return out
}

func toSRGB(c float64) float64 {
	c = math.Max(0, math.Min(1, c))
	if c <= 0.0031308 {
		return 12.92 * c
	}
	return 1.055*math.Pow(c, 1/2.4) - 0.055
}
