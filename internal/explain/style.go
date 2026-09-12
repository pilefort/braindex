package explain

// tocWide は目次を横に固定する画面幅。CSS とJS で同じ値を使う(片方だけ直すとずれる)。
// 本文は 820px を中央に置くので、その左に 240px の目次と余白が入る幅を境にする。
const tocWide = "(min-width:1360px)"

// css は mdhtml の共通 CSS の後ろに足す分。目次・図の見せ方を決める。
const css = `
.doc.explain{padding-bottom:180px}
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
`

// js は mdhtml の共通 JS の後ろに足す分。
//  1. 狭い画面では目次を畳む(広い画面では横に固定されるので開いたまま)。
//     CSS だけでは details の開閉を画面幅で変えられないので JS で外す。JS が動かなくても開いたまま読める。
//  2. 読んでいる節の見出しを目次で示す(画面の上端を越えた最後の見出し)。
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
})();`
