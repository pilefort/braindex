package mdhtml

import (
	"math"
	"strconv"
	"strings"
)

// 色の決まり: 文字と背景の組み合わせは、明るい配色でも暗い配色でもコントラスト比 4.5 以上にする。
// 線・図形・大きい文字は 3.0 以上。利用者の指摘 2026-09-12「視認性の悪い色や、コントラストの低い
// 色と背景の組み合わせを使わないで欲しい」。実際の比は contrast_test.go が測る。

// Contrast は 2 色のコントラスト比を返す(WCAG 2.1 の定義。1〜21)。
// 受けるのは #rgb と #rrggbb。読めない文字は 0 として扱う。
func Contrast(a, b string) float64 {
	la, lb := relLuminance(a), relLuminance(b)
	hi, lo := math.Max(la, lb), math.Min(la, lb)
	return (hi + 0.05) / (lo + 0.05)
}

// relLuminance は相対輝度(WCAG 2.1)。
func relLuminance(hex string) float64 {
	h := strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) < 6 {
		return 0
	}
	ch := func(i int) float64 {
		v, err := strconv.ParseInt(h[i:i+2], 16, 0)
		if err != nil {
			return 0
		}
		c := float64(v) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(0) + 0.7152*ch(2) + 0.0722*ch(4)
}
