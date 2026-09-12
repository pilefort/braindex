package explain

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// 数値のグラフは md の表から機械が描く(決定 2026-09-12 → manual/tools.md「決めたこと」)。手で描くと棒の長さを目分量で決めることになり、
// 本文の数値と食い違っても気づけない。表の数値から目盛りと長さを計算すれば、その食い違いは起きない。
// 描くのは棒と折れ線の 2 種類だけで、概念図は手書きの .svg のまま。

// 系列の色は 5 つで、色相は Okabe-Ito（色覚の差でも見分けられる並び）から取る。
// **明るい配色と暗い配色で別の値を使う。** Okabe-Ito をそのまま白い背景に置くと、橙(#E69F00)が 2.1、
// 空(#56B4E9)が 2.2 しかなく、細い折れ線が背景に沈む(利用者の指摘 2026-09-12)。
// 明るい配色では暗く寄せ、暗い配色では元の明るい値を使う。どちらも背景との比は 3.0 以上、
// 系列どうしの見分けやすさは Okabe-Ito と同等以上にする。実際の値は contrast_test.go が測る。
//
// 色は SVG に直接書かず CSS の class で当てる（→ style.go の seriesCSS）。配色の切り替えに追従させるため。
var (
	graphColorsLight = []string{"#0067A0", "#D55E00", "#007656", "#C86D9F", "#3B4047"}
	graphColorsDark  = []string{"#56B4E9", "#E69F00", "#009E73", "#CC79A7", "#C9D1DC"}
)

// seriesClass は k 番目の系列に当てる class。色の数を超えたら先頭へ戻る。
func seriesClass(kind string, k int) string {
	return "bx-" + kind + strconv.Itoa(k%len(graphColorsLight))
}

// lineDashes は系列が色の数を超えたときの線種。6 つ目からは色が一巡するので、線種でも見分けられるようにする。
var lineDashes = []string{"", "7 4", "2 3", "11 4 2 4", "1 5", "9 3 2 3"}

// graphDirRE は表の直前に置く指定の行。HTML コメントなので GitHub では本文に出ない。
//
//	<!-- graph: bar x=手法 y=Recall@1,Recall@3 unit=% -->
var graphDirRE = regexp.MustCompile(`^<!--\s*graph:\s*(.*?)\s*-->$`)

// graphSpec は指定の中身。
type graphSpec struct {
	kind string   // bar / line
	x    string   // 横軸にする列名(空なら 1 列目)
	y    []string // 描く列名(空なら x 以外の数値列すべて)
	unit string   // 目盛りに付ける単位
}

// parseGraphDirective は行が graph の指定なら中身を返す。読めない語は problems に積む。
func parseGraphDirective(line string) (graphSpec, []string, bool) {
	m := graphDirRE.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return graphSpec{}, nil, false
	}
	var spec graphSpec
	var problems []string
	for i, f := range strings.Fields(m[1]) {
		k, v, has := strings.Cut(f, "=")
		if i == 0 && !has {
			spec.kind = k
			continue
		}
		switch k {
		case "x":
			spec.x = v
		case "y":
			for _, n := range strings.Split(v, ",") {
				if n = strings.TrimSpace(n); n != "" {
					spec.y = append(spec.y, n)
				}
			}
		case "unit":
			spec.unit = v
		default:
			problems = append(problems, "graph の指定 "+f+" は読めない(使えるのは x= y= unit=)")
		}
	}
	if spec.kind != "bar" && spec.kind != "line" {
		problems = append(problems, "graph: "+spec.kind+" は描けない(bar か line)")
		return spec, problems, false
	}
	return spec, problems, true
}

// tableSepRE は表の 2 行目(|---|---|)。mdhtml と同じ読み方をする。
var tableSepRE = regexp.MustCompile(`^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)+\|?\s*$`)

// isTableStart は lines[i] が表の 1 行目かを見る。
func isTableStart(lines []string, i int) bool {
	return i >= 0 && i < len(lines) && strings.Contains(lines[i], "|") &&
		i+1 < len(lines) && tableSepRE.MatchString(lines[i+1])
}

// readTable は lines[i] から始まる表を読む。見出し・行・元の md の行・表の次の位置を返す。
func readTable(lines []string, i int) (header []string, rows [][]string, raw []string, next int) {
	header = splitRow(lines[i])
	next = i + 2
	for next < len(lines) && strings.Contains(lines[next], "|") && strings.TrimSpace(lines[next]) != "" {
		rows = append(rows, splitRow(lines[next]))
		next++
	}
	return header, rows, lines[i:next], next
}

// splitRow は表の 1 行を | で分割する(前後の空セルを落とし、各セルを trim する)。
func splitRow(line string) []string {
	cells := strings.Split(strings.TrimSpace(line), "|")
	if len(cells) > 0 && strings.TrimSpace(cells[0]) == "" {
		cells = cells[1:]
	}
	if len(cells) > 0 && strings.TrimSpace(cells[len(cells)-1]) == "" {
		cells = cells[:len(cells)-1]
	}
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

// parseNum は表のセルを数値として読む。桁区切りの , と末尾の % は落とす。読めなければ欠測。
func parseNum(s string) (float64, bool) {
	t := strings.TrimSpace(s)
	t = strings.ReplaceAll(t, ",", "")
	t = strings.TrimSpace(strings.TrimSuffix(t, "%"))
	if t == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(t, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// series は 1 本の系列。vals の NaN は欠測。
type series struct {
	name string
	vals []float64
}

// buildChart は表から描くものを決める。x 列・y 列の既定はここで埋める。
func buildChart(spec graphSpec, header []string, rows [][]string) (labels []string, ss []series, problems []string) {
	find := func(name string) int {
		for i, h := range header {
			if strings.TrimSpace(h) == name {
				return i
			}
		}
		return -1
	}
	xi := 0
	if spec.x != "" {
		if i := find(spec.x); i >= 0 {
			xi = i
		} else {
			problems = append(problems, "graph: x= の列「"+spec.x+"」が表に無い(1 列目で描く)")
		}
	}
	var cols []int
	if len(spec.y) > 0 {
		for _, n := range spec.y {
			if i := find(n); i >= 0 {
				cols = append(cols, i)
			} else {
				problems = append(problems, "graph: y= の列「"+n+"」が表に無い")
			}
		}
	} else {
		for c := range header {
			if c != xi && isNumericCol(rows, c) {
				cols = append(cols, c)
			}
		}
	}
	if len(cols) == 0 {
		problems = append(problems, "graph: 描ける数値の列が無い(表だけ出す)")
		return nil, nil, problems
	}
	for _, c := range cols {
		ss = append(ss, series{name: strings.TrimSpace(header[c])})
	}
	for _, row := range rows {
		vals := make([]float64, len(cols))
		any := false
		for k, c := range cols {
			vals[k] = math.NaN()
			if c < len(row) {
				if v, ok := parseNum(row[c]); ok {
					vals[k], any = v, true
				}
			}
		}
		if !any {
			continue // 数値がひとつも無い行は飛ばす
		}
		label := ""
		if xi < len(row) {
			label = strings.TrimSpace(row[xi])
		}
		labels = append(labels, label)
		for k := range ss {
			ss[k].vals = append(ss[k].vals, vals[k])
		}
	}
	if len(labels) == 0 {
		problems = append(problems, "graph: 数値として読める行が無い(表だけ出す)")
		return nil, nil, problems
	}
	return labels, ss, problems
}

// isNumericCol は列が数値の列かを見る。空でないセルの過半数が数値なら数値の列とする
// (途中に欠測が混じっても拾えるように。文字だけの列は 0 件なので外れる)。
func isNumericCol(rows [][]string, c int) bool {
	num, non := 0, 0
	for _, r := range rows {
		if c >= len(r) || strings.TrimSpace(r[c]) == "" {
			continue
		}
		if _, ok := parseNum(r[c]); ok {
			num++
		} else {
			non++
		}
	}
	return num > non && num > 0
}

// グラフの寸法。viewBox で置くので単位は px でなく座標。
const (
	chartW  = 760.0
	plotH   = 310.0
	padL    = 66.0
	padR    = 18.0
	tickNum = 4 // 目盛りの目安の本数
)

// chartSVG は表から棒か折れ線の SVG を組み立てる。描けなければ空文字(表だけを出す)。
func chartSVG(spec graphSpec, header []string, rows [][]string) (string, []string) {
	labels, ss, problems := buildChart(spec, header, rows)
	if len(ss) == 0 {
		return "", problems
	}
	lo, hi, step := niceScale(ss)
	dec := decimalsFor(step)

	// 凡例は系列が 2 つ以上のときだけ。折り返した行数の分だけ上の余白を増やす。
	legend := [][]int{}
	if len(ss) > 1 {
		legend = legendRows(ss)
	}
	padT := 14.0 + float64(len(legend))*19.0
	axisY := padT + plotH
	plotW := chartW - padL - padR
	groupW := plotW / float64(len(labels))

	maxLabel := 0.0
	for _, l := range labels {
		if w := textWidth(l, 12); w > maxLabel {
			maxLabel = w
		}
	}
	rotate := maxLabel > groupW-6
	padB := 34.0
	if rotate {
		padB = 26 + math.Min(96, maxLabel*0.5)
	}
	h := axisY + padB

	yOf := func(v float64) float64 { return axisY - (v-lo)/(hi-lo)*plotH }
	xOf := func(i int) float64 { return padL + (float64(i)+0.5)*groupW }

	var b strings.Builder
	b.WriteString(`<svg class="bx-chart" viewBox="0 0 ` + f1(chartW) + " " + f1(h) +
		`" role="img" xmlns="http://www.w3.org/2000/svg">`)

	// 目盛りと横線。v を足し込むと丸めが積もるので、本数を数えてから掛けて出す
	for t := 0; t <= int(math.Round((hi-lo)/step)); t++ {
		v := lo + float64(t)*step
		y := yOf(v)
		b.WriteString(`<line class="bx-grid" x1="` + f1(padL) + `" y1="` + f1(y) +
			`" x2="` + f1(chartW-padR) + `" y2="` + f1(y) + `"/>`)
		b.WriteString(`<text class="bx-tick" x="` + f1(padL-8) + `" y="` + f1(y+4) +
			`" text-anchor="end">` + escapeText(strconv.FormatFloat(v, 'f', dec, 64)+spec.unit) + `</text>`)
	}
	zero := yOf(0)
	if lo < 0 && hi > 0 {
		b.WriteString(`<line class="bx-zero" x1="` + f1(padL) + `" y1="` + f1(zero) +
			`" x2="` + f1(chartW-padR) + `" y2="` + f1(zero) + `"/>`)
	}

	// 横軸の見出し
	for i, l := range labels {
		x, y := xOf(i), axisY+18
		attr := ` text-anchor="middle"`
		if rotate {
			attr = ` text-anchor="end" transform="rotate(-30 ` + f1(x) + " " + f1(y) + `)"`
		}
		b.WriteString(`<text class="bx-lab" x="` + f1(x) + `" y="` + f1(y) + `"` + attr + `>` +
			escapeText(l) + `</text>`)
	}

	if spec.kind == "bar" {
		inner := groupW * 0.76
		bw := inner / float64(len(ss))
		delay := 0.0
		for i := range labels {
			for k, s := range ss {
				v := s.vals[i]
				if math.IsNaN(v) {
					continue // 欠測は棒を描かない
				}
				x := xOf(i) - inner/2 + float64(k)*bw
				y0, y1 := yOf(v), zero // 0 の線から伸ばす(負の値は下へ)
				b.WriteString(`<rect class="bx-bar ` + seriesClass("fill", k) + `" x="` + f1(x+1) + `" y="` + f1(math.Min(y0, y1)) +
					`" width="` + f1(math.Max(bw-2, 1)) + `" height="` + f1(math.Abs(y1-y0)) +
					`" style="animation-delay:` + strconv.FormatFloat(delay, 'f', 2, 64) + `s"/>`)
				delay = math.Min(delay+0.03, 0.6)
			}
		}
	} else {
		dash := len(ss) >= 6 // 色が一巡するので線種でも見分けられるようにする
		for k, s := range ss {
			var pts []string
			for i, v := range s.vals {
				if math.IsNaN(v) {
					continue // 欠測は飛ばして次の点とつなぐ
				}
				pts = append(pts, f1(xOf(i))+","+f1(yOf(v)))
			}
			attr := ""
			if dash && lineDashes[k%len(lineDashes)] != "" {
				attr = ` stroke-dasharray="` + lineDashes[k%len(lineDashes)] + `"`
			}
			b.WriteString(`<polyline class="bx-line ` + seriesClass("stroke", k) + `" points="` +
				strings.Join(pts, " ") + `"` + attr + `/>`)
			for i, v := range s.vals {
				if math.IsNaN(v) {
					continue
				}
				b.WriteString(`<circle class="bx-dot ` + seriesClass("fill", k) + `" cx="` + f1(xOf(i)) +
					`" cy="` + f1(yOf(v)) + `" r="3.2"/>`)
			}
		}
	}

	// 凡例
	for row, idxs := range legend {
		x := padL
		y := 14.0 + float64(row)*19.0
		for _, k := range idxs {
			b.WriteString(`<rect class="` + seriesClass("fill", k) + `" x="` + f1(x) + `" y="` + f1(y-9) +
				`" width="12" height="12" rx="3"/>`)
			b.WriteString(`<text class="bx-leg" x="` + f1(x+17) + `" y="` + f1(y+1) + `">` +
				escapeText(ss[k].name) + `</text>`)
			x += legendItemW(ss[k].name)
		}
	}
	b.WriteString("</svg>")
	return b.String(), problems
}

// niceScale は目盛りの下端・上端・間隔を決める。棒の長さを読み違えないよう 0 を必ず含める。
func niceScale(ss []series) (lo, hi, step float64) {
	lo, hi = 0, 0
	for _, s := range ss {
		for _, v := range s.vals {
			if math.IsNaN(v) {
				continue
			}
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
	}
	if hi <= lo {
		hi = lo + 1
	}
	raw := (hi - lo) / tickNum
	exp := math.Floor(math.Log10(raw))
	base := math.Pow(10, exp)
	switch f := raw / base; {
	case f <= 1:
		step = base
	case f <= 2:
		step = 2 * base
	case f <= 2.5:
		step = 2.5 * base
	case f <= 5:
		step = 5 * base
	default:
		step = 10 * base
	}
	lo = math.Floor(lo/step) * step
	hi = math.Ceil(hi/step) * step
	return lo, hi, step
}

// decimalsFor は目盛りの文字に出す小数桁。間隔より細かくは出さない。
func decimalsFor(step float64) int {
	d := int(math.Ceil(-math.Log10(step)))
	if d < 0 {
		d = 0
	}
	if d > 3 {
		d = 3
	}
	return d
}

// legendRows は凡例を行に割る(1 行に収まらなければ折り返す)。
func legendRows(ss []series) [][]int {
	var rows [][]int
	cur, x := []int{}, 0.0
	for k := range ss {
		w := legendItemW(ss[k].name)
		if len(cur) > 0 && x+w > chartW-padL-padR {
			rows = append(rows, cur)
			cur, x = []int{}, 0
		}
		cur = append(cur, k)
		x += w
	}
	return append(rows, cur)
}

func legendItemW(name string) float64 { return 17 + textWidth(name, 12) + 20 }

// textWidth は文字の幅の見当。全角はほぼ字面どおり、ASCII はその半分強で見る
// (凡例と横軸の見出しを重ねないための当たり判定なので、正確さより決定性を取る)。
func textWidth(s string, size float64) float64 {
	w := 0.0
	for _, r := range s {
		if r < 0x80 {
			w += size * 0.55
		} else {
			w += size
		}
	}
	return w
}

// f1 は座標を小数 1 桁で出す。浮動小数の表記揺れを防ぎ、同じ表からは同じ SVG が出る(SPEC「決定性のため」)。
func f1(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }
