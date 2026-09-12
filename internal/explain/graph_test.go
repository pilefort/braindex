package explain

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const graphMD = "<!-- graph: bar x=手法 y=Recall@1,Recall@3 unit=% -->\n" +
	"| 手法 | Recall@1 | Recall@3 |\n" +
	"|---|---|---|\n" +
	"| BM25 | 41.2 | 58.9 |\n" +
	"| STAIR | 55.0 | 71.3 |\n"

// 同じ表から 2 回描いてもバイト一致する(座標は小数 1 桁に丸めて出す)。
func TestGraph_決定的(t *testing.T) {
	a := mustRender(t, graphMD, Options{})
	for i := 0; i < 3; i++ {
		if mustRender(t, graphMD, Options{}) != a {
			t.Fatal("同じ表から違う SVG が出た")
		}
	}
	for _, n := range regexp.MustCompile(`[xy]="([0-9.-]+)"`).FindAllStringSubmatch(a, -1) {
		if d := strings.SplitN(n[1], ".", 2); len(d) == 2 && len(d[1]) > 1 {
			t.Errorf("座標の桁が 1 を超えている: %s", n[1])
		}
	}
}

// グラフの下に元の表を残す。指定の行(HTML コメント)は本文に出さない。
func TestGraph_表も残す(t *testing.T) {
	body := mainOf(t, mustRender(t, graphMD, Options{}))
	if !strings.Contains(body, `<figure id="graph-1" class="bx-graph">`) {
		t.Error("グラフの枠が無い")
	}
	if !strings.Contains(body, `<svg class="bx-chart"`) {
		t.Error("SVG が無い")
	}
	if !strings.Contains(body, "<td>41.2</td>") {
		t.Errorf("元の表が残っていない\n%s", body)
	}
	if strings.Contains(body, "graph:") || strings.Contains(body, "&lt;!--") {
		t.Errorf("指定の行が本文に出ている\n%s", body)
	}
	if i, j := strings.Index(body, "<svg"), strings.Index(body, "<table>"); i < 0 || j < i {
		t.Error("表がグラフの下に無い")
	}
}

// graph の指定が無い表はそのまま表として出す。
func TestGraph_指定の無い表は描かない(t *testing.T) {
	body := mainOf(t, mustRender(t, "| a | b |\n|---|---|\n| 1 | 2 |\n", Options{}))
	if strings.Contains(body, "bx-chart") {
		t.Error("指定の無い表を描いた")
	}
}

// y= を省くと、x 以外の数値列だけを拾う(文字の列は系列にしない)。
func TestGraph_y省略で数値列だけ拾う(t *testing.T) {
	md := "<!-- graph: line -->\n" +
		"| 年 | 件数 | 備考 |\n|---|---|---|\n| 2024 | 12 | 少ない |\n| 2025 | 30 | 増えた |\n"
	labels, ss, problems := buildChart(graphSpec{kind: "line"}, []string{"年", "件数", "備考"},
		[][]string{{"2024", "12", "少ない"}, {"2025", "30", "増えた"}})
	if len(problems) != 0 {
		t.Fatalf("問題が出た: %v", problems)
	}
	if len(ss) != 1 || ss[0].name != "件数" {
		t.Fatalf("系列が違う: %+v", ss)
	}
	if len(labels) != 2 || labels[0] != "2024" {
		t.Errorf("横軸の見出しが違う: %v", labels)
	}
	if body := mainOf(t, mustRender(t, md, Options{})); !strings.Contains(body, "bx-line") {
		t.Error("折れ線が描かれていない")
	}
}

// 桁区切りの , と末尾の % は落として数値として読む。読めないセルは欠測。
func TestGraph_数値の読み方(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"1,234", 1234, true},
		{"12%", 12, true},
		{" 41.2 ", 41.2, true},
		{"-3", -3, true},
		{"", 0, false},
		{"n/a", 0, false},
		{"約 5", 0, false},
	}
	for _, c := range cases {
		got, ok := parseNum(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseNum(%q)=%v,%v want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// 数値がひとつも無い行は飛ばす。一部だけ欠けている行は残し、その系列の点だけ描かない。
func TestGraph_数値にならない行を飛ばす(t *testing.T) {
	spec := graphSpec{kind: "bar", y: []string{"A", "B"}} // 列の選び方は別のテストで見る
	labels, ss, problems := buildChart(spec, []string{"手法", "A", "B"},
		[][]string{{"BM25", "10", "20"}, {"小計", "-", "n/a"}, {"STAIR", "30", "-"}})
	if len(problems) != 0 {
		t.Fatalf("問題が出た: %v", problems)
	}
	if len(labels) != 2 || labels[0] != "BM25" || labels[1] != "STAIR" {
		t.Fatalf("行が飛ばされていない: %v", labels)
	}
	if !math.IsNaN(ss[1].vals[1]) {
		t.Errorf("欠測のセルが数値になっている: %v", ss[1].vals)
	}
	if ss[0].vals[1] != 30 {
		t.Errorf("残る行の値が違う: %v", ss[0].vals)
	}
}

// y= を省いたときの「数値列」は、空でないセルの過半数が数値として読める列
// (途中の欠測では外れず、文字だけの列は入らない)。
func TestGraph_数値列の見分け(t *testing.T) {
	rows := [][]string{{"a", "10", "少ない", "1"}, {"b", "-", "多い", "2"}, {"c", "30", "n/a", "x"}}
	cases := []struct {
		col  int
		want bool
		why  string
	}{
		{0, false, "文字だけの列"},
		{1, true, "欠測が 1 つ混じった数値の列"},
		{2, false, "文字の列(数値は 0 件)"},
		{3, true, "数値が過半数"},
	}
	for _, c := range cases {
		if got := isNumericCol(rows, c.col); got != c.want {
			t.Errorf("列 %d(%s)=%v want %v", c.col, c.why, got, c.want)
		}
	}
}

// 系列が 6 つ以上になると、折れ線は線種でも見分けられるようにする(色は 5 色で一巡するため)。
func TestGraph_系列が6つ以上で線種が変わる(t *testing.T) {
	head := func(n int) (string, string) {
		cols, cells := "| x |", "|---|"
		row := "| a |"
		for i := 1; i <= n; i++ {
			cols += " s" + strconv.Itoa(i) + " |"
			cells += "---|"
			row += " " + strconv.Itoa(i*10) + " |"
		}
		return "<!-- graph: line -->\n" + cols + "\n" + cells + "\n" + row + "\n", row
	}
	md5, _ := head(5)
	if body := mainOf(t, mustRender(t, md5, Options{})); strings.Contains(body, "stroke-dasharray") {
		t.Error("5 系列で線種を変えている(色だけで見分けられる)")
	}
	md6, _ := head(6)
	body := mainOf(t, mustRender(t, md6, Options{}))
	if !strings.Contains(body, "stroke-dasharray") {
		t.Errorf("6 系列で線種が変わっていない\n%s", body)
	}
}

// 棒は読み込み時に伸びる。動きを嫌う設定では止める。
func TestGraph_棒が伸びる(t *testing.T) {
	html := mustRender(t, graphMD, Options{})
	if !strings.Contains(html, `class="bx-bar bx-fill0"`) || !strings.Contains(html, "animation:bx-grow") {
		t.Error("棒が伸びる指定が無い")
	}
	if !strings.Contains(html, "@media (prefers-reduced-motion:reduce)") ||
		!strings.Contains(html, ".bx-chart .bx-bar{animation:none}") {
		t.Error("動きを止める指定が無い")
	}
}

// 目盛りは 0 を含み、切りのよい間隔にする(棒の長さを読み違えないため)。
func TestGraph_目盛り(t *testing.T) {
	lo, hi, step := niceScale([]series{{vals: []float64{41.2, 58.9, 71.3}}})
	if lo != 0 {
		t.Errorf("下端が 0 でない: %v", lo)
	}
	if hi < 71.3 {
		t.Errorf("上端が最大値に届いていない: %v", hi)
	}
	if math.Mod(hi, step) != 0 {
		t.Errorf("上端が間隔の倍数でない: hi=%v step=%v", hi, step)
	}
	if lo2, hi2, _ := niceScale([]series{{vals: []float64{-5, 3}}}); lo2 > -5 || hi2 < 3 {
		t.Errorf("負の値が入らない: %v %v", lo2, hi2)
	}
}

// 描けない指定は、その旨を問題に積んで表だけ出す(落とさない)。
func TestGraph_描けない指定(t *testing.T) {
	html, problems := Render("<!-- graph: pie -->\n| a | b |\n|---|---|\n| 1 | 2 |\n", "題", Options{})
	if len(problems) != 1 || !strings.Contains(problems[0], "pie") {
		t.Errorf("problems=%v", problems)
	}
	body := mainOf(t, html)
	if strings.Contains(body, "bx-chart") {
		t.Error("描けない種類で SVG を出した")
	}
	if !strings.Contains(body, "<td>1</td>") {
		t.Errorf("表が消えている\n%s", body)
	}
}

// 指定の次の行に表が無ければ、指定の行を捨てて問題に積む。
func TestGraph_表が無い指定(t *testing.T) {
	html, problems := Render("<!-- graph: bar -->\n\n本文\n", "題", Options{})
	if len(problems) != 1 || !strings.Contains(problems[0], "表が無い") {
		t.Errorf("problems=%v", problems)
	}
	if body := mainOf(t, html); strings.Contains(body, "graph:") {
		t.Errorf("指定の行が本文に出ている\n%s", body)
	}
}

// 無い列名を指した y= は問題にする(黙って別の列を描かない)。
func TestGraph_無い列を指したとき(t *testing.T) {
	_, problems := Render("<!-- graph: bar y=無い列 -->\n| a | b |\n|---|---|\n| 1 | 2 |\n", "題", Options{})
	if len(problems) != 2 {
		t.Fatalf("problems=%v", problems)
	}
	if !strings.Contains(problems[0], "無い列") {
		t.Errorf("列名の指摘が無い: %v", problems)
	}
}
