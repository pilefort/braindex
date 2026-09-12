package explain

import (
	"strconv"
	"strings"
)

// tocWide は目次を横に固定する画面幅。CSS と JS で同じ値を使う(片方だけ直すとずれる)。
// この幅からは目次を画面の左端に置き、本文をその右へずらす。中央に置いたまま左へ回り込ませると、
// 本文との間が数 px しか空かない(2026-09-12 の指摘・実測で 8px だった)。
const tocWide = "(min-width:1240px)"

// tocCenter は本文を中央に戻せる画面幅。ここまで広ければ、中央の本文の左にも目次が余裕で入る。
const tocCenter = "(min-width:1560px)"

// measure は本文の行長。日本語は全角 1 文字が 1rem(16px)なので、rem が読みやすさの目安になる
// (35〜45 字が読みやすいとされる範囲)。**em にしない**——em は要素自身の文字の大きさが基準なので、
// 見出し(26px)だけ行長が 1.6 倍になり、段落と右端がそろわなくなる(2026-09-12 の指摘)。
// 図・表・グラフはこれより広く使ってよいので、箱の幅(.doc.explain)と本文の幅を分ける。
const measure = "40rem"

var css = seriesCSS() + figCSS() + `
.doc.explain{max-width:900px;padding-bottom:180px}
.doc.explain p,.doc.explain ul,.doc.explain ol,.doc.explain blockquote,
.doc.explain h1,.doc.explain h2,.doc.explain h3,.doc.explain h4{max-width:` + measure + `}
.doc.explain pre{max-width:100%}
.doc.explain p,.doc.explain li,.doc.explain blockquote,
.doc.explain h1,.doc.explain h2,.doc.explain h3,.doc.explain h4,.bx-toc a{
  word-break:auto-phrase;line-break:strict;text-wrap:pretty}
.doc.explain td{word-break:auto-phrase;line-break:strict}
.bx-tw{overflow-x:auto;max-width:100%}
.bx-tw table{margin:1em 0}
.doc.explain .bx-ref{text-decoration:none;border-bottom:1px dotted var(--accent);word-break:keep-all}
.bx-fig:target,.bx-graph:target{outline:2px solid var(--accent);outline-offset:10px;border-radius:6px}
.bx-toc{border:1px solid var(--line);border-radius:12px;background:var(--panel);padding:4px 12px;
  margin:0 0 26px;font-size:13px;line-height:1.6}
.bx-toc>summary{cursor:pointer;color:var(--sub);font-weight:700;padding:7px 0;list-style:none}
.bx-toc>summary::-webkit-details-marker{display:none}
.bx-toc>summary::before{content:"▸ "}
.bx-toc[open]>summary::before{content:"▾ "}
.bx-toc ul{list-style:none;margin:.2em 0 .7em;padding:0}
.bx-toc li{margin:.1em 0}
.bx-toc li.l3{padding-left:1.1em;font-size:12.5px}
.bx-toc a{color:var(--sub);text-decoration:none;display:block;padding:2px 7px;
  border-left:2px solid transparent;border-radius:0 6px 6px 0;overflow-wrap:anywhere}
.bx-toc a:hover{background:var(--line2)}
.bx-toc a.cur{color:var(--accent);border-left-color:var(--accent);background:var(--accent-soft);font-weight:700}
@media ` + tocWide + `{
  .bx-toc{position:fixed;top:56px;left:28px;width:224px;max-height:calc(100vh - 112px);overflow:auto;margin:0}
  .doc.explain{margin-left:300px;margin-right:auto}}
@media ` + tocCenter + `{
  .bx-toc{left:calc(50% - 700px)}
  .doc.explain{margin-left:auto}}
.bx-fig{margin:1.8em 0}
.bx-fig svg{display:block;max-width:100%;height:auto;margin:0 auto}
.bx-fig figcaption{margin-top:.6em;color:var(--sub);font-size:13px;text-align:center}
.bx-miss{border:1px dashed var(--line);border-radius:10px;padding:22px;text-align:center;
  color:var(--mut);background:var(--line2)}
.bx-graph{margin:1.8em 0}
.bx-chart{display:block;width:100%;height:auto}
.bx-chart text{font-family:var(--jp)}
.bx-chart .bx-grid{stroke:var(--line)}
.bx-chart .bx-zero{stroke:var(--sub)}
.bx-chart .bx-tick{fill:var(--mut);font-size:11px}
.bx-chart .bx-lab,.bx-chart .bx-leg{fill:var(--sub);font-size:12px}
.bx-chart .bx-line{fill:none;stroke-width:2.2;stroke-linejoin:round;stroke-linecap:round}
.bx-chart .bx-bar{transform-box:fill-box;transform-origin:bottom;animation:bx-grow .7s cubic-bezier(.2,.7,.3,1) both}
@keyframes bx-grow{from{transform:scaleY(0)}to{transform:scaleY(1)}}
@media (prefers-reduced-motion:reduce){
  .bx-fig svg,.bx-fig svg *{animation:none!important;transition:none!important}
  .bx-chart .bx-bar{animation:none}}
`

// js は mdhtml の共通 JS の後ろに足す分。
//  1. 狭い画面では目次を畳む(広い画面では横に固定されるので開いたまま)。
//     CSS だけでは details の開閉を画面幅で変えられないので JS で外す。JS が動かなくても開いたまま読める。
//  2. 読んでいる節の見出しを目次で示す(画面の上端を越えた最後の見出し)。
//  3. prefers-reduced-motion の環境では、埋め込んだ図の動きを止める。CSS の animation は
//     上の @media が止めるが、SVG の <animate>(SMIL)は CSS では止まらないので pauseAnimations を呼ぶ。
const js = `
(function(){
var d=document.getElementById('bx-toc');if(!d)return;
try{if(window.matchMedia&&!matchMedia('` + tocWide + `').matches)d.removeAttribute('open');}catch(e){}
var as=d.querySelectorAll('a[href^="#"]'),ps=[];
for(var i=0;i<as.length;i++){var h=document.getElementById(as[i].getAttribute('href').slice(1));
if(h)ps.push([h,as[i]]);}
if(!ps.length)return;
function upd(){var cur=ps[0][1];
for(var i=0;i<ps.length;i++){if(ps[i][0].getBoundingClientRect().top<=80)cur=ps[i][1];}
for(var i=0;i<ps.length;i++){var a=ps[i][1];if((a===cur)!==a.classList.contains('cur'))a.classList.toggle('cur');}}
upd();addEventListener('scroll',upd);addEventListener('resize',upd);
})();
(function(){
var m=false;try{m=window.matchMedia&&matchMedia('(prefers-reduced-motion: reduce)').matches;}catch(e){}
if(!m)return;
var ss=document.querySelectorAll('.bx-fig svg');
for(var i=0;i<ss.length;i++){if(ss[i].pauseAnimations)ss[i].pauseAnimations();}
})();`

// seriesCSS は系列の色を出す。明るい配色と暗い配色で値が違うので、SVG に直接書かず class で当てる。
// 塗り(棒・点・凡例の印)と線(折れ線)を別の class に分けてあるのは、`.bx-line{fill:none}` と
// 取り合いにならないようにするため。
func seriesCSS() string {
	var b strings.Builder
	write := func(prefix string, colors []string) {
		for i, c := range colors {
			n := strconv.Itoa(i)
			b.WriteString(prefix + ".bx-fill" + n + "{fill:" + c + "}\n")
			b.WriteString(prefix + ".bx-stroke" + n + "{stroke:" + c + "}\n")
		}
	}
	write("", graphColorsLight)
	write(`:root[data-theme="dark"] `, graphColorsDark)
	return b.String()
}

// figMono は図の中の等幅の文字(ファイル名・コマンド)の色。明るい配色・暗い配色の順。
// 本文に対応する変数が無いので、図のためにここで持つ。値は contrast_test.go が測る。
var figMono = [2]string{"#0f6b8f", "#7dcfff"}

// figSurfAlpha は系列の面(--s1〜)の濃さ。明るい配色・暗い配色の順。
// 暗い配色で濃いのは、暗い地の上では同じ濃さだと面が見えないため。
var figSurfAlpha = [2]float64{0.09, 0.12}

// figCSS は図(svg.bxfig)が使える色の名前を出す。
//
// **図の側は名前だけを書き、色の値は書かない。** 値を図ごとに書き写すと、書き忘れた図が
// 本文と違う配色のまま出る(2026-09-12 に実際に起きた。暗い配色前提の図を明るい本文へ入れて
// 文字と背景の比が 1.13 になった)。ここに 1 か所だけ置けば、図はそれを参照するだけで済む。
//
// 本文にある色は本文の変数へ寄せてあるので、明暗の切り替えに自動でついてくる。
// 図だけが使う色(等幅の文字・系列・系列の面)は明暗の 2 組を出す。
func figCSS() string {
	const alias = "--fg:var(--ink);--fg2:var(--sub);--edge:var(--mut);" +
		"--groove:var(--line);--surf:var(--line2);"
	return ".bx-fig svg.bxfig{" + alias + figVars(0) + "}\n" +
		`:root[data-theme="dark"] .bx-fig svg.bxfig{` + figVars(1) + "}\n"
}

// figVars は図だけが使う色を 1 組分並べる。i は 0 が明るい配色、1 が暗い配色。
func figVars(i int) string {
	colors := graphColorsLight
	if i == 1 {
		colors = graphColorsDark
	}
	var b strings.Builder
	b.WriteString("--mono:" + figMono[i] + ";")
	for n, c := range colors {
		b.WriteString("--c" + strconv.Itoa(n+1) + ":" + c + ";")
	}
	// 面は系列の色を薄く敷く。枠線と同じ色にするため、別の値を持たない。
	for n, c := range colors[:len(colors)-1] {
		b.WriteString("--s" + strconv.Itoa(n+1) + ":" + rgba(c, figSurfAlpha[i]) + ";")
	}
	return b.String()
}

// rgba は #rrggbb を rgba(r,g,b,a) にする。読めない色は黒として扱う(CSS が壊れないようにする)。
func rgba(hex string, a float64) string {
	h := strings.TrimPrefix(hex, "#")
	ch := func(k int) string {
		if len(h) < k+2 {
			return "0"
		}
		v, err := strconv.ParseInt(h[k:k+2], 16, 0)
		if err != nil {
			return "0"
		}
		return strconv.FormatInt(v, 10)
	}
	return "rgba(" + ch(0) + "," + ch(2) + "," + ch(4) + "," +
		strconv.FormatFloat(a, 'f', -1, 64) + ")"
}
