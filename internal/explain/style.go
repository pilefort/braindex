package explain

// tocWide は目次を横に固定する画面幅。CSS とJS で同じ値を使う(片方だけ直すとずれる)。
// 本文は 820px を中央に置くので、その左に 240px の目次と余白が入る幅を境にする。
const tocWide = "(min-width:1360px)"

// css は mdhtml の共通 CSS の後ろに足す分。目次・図の見せ方を決める。
// 本文の行長。日本語は全角 1 文字が 1em なので、em が読みやすさの目安になる
// (35〜45 字が読みやすいとされる範囲)。図・表・グラフはこれより広く使ってよいので、
// 箱の幅(.doc.explain)と本文の幅(.bx-measure)を分ける。
const measure = "38em"

const css = `
.doc.explain{max-width:900px;padding-bottom:180px}
.doc.explain p,.doc.explain ul,.doc.explain ol,.doc.explain blockquote,
.doc.explain h1,.doc.explain h2,.doc.explain h3,.doc.explain h4{max-width:` + measure + `}
.doc.explain pre{max-width:100%}
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
@media ` + tocWide + `{.bx-toc{position:fixed;top:56px;left:calc(50% - 670px);width:240px;
  max-height:calc(100vh - 112px);overflow:auto;margin:0}}
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
