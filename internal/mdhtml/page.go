package mdhtml

import (
	"regexp"
	"strings"
)

// urlRE は本文中の裸の URL(原型 answer_html.py の _URL_RE と同じ)。
var urlRE = regexp.MustCompile(`https?://[^\s<>"'|)]+`)

// css は回答 HTML に埋め込むスタイル。外部読み込み無し。data-theme="dark" で配色が切り替わる。
const css = `
:root{--bg:#f6f7f9;--panel:#fff;--ink:#1c2230;--sub:#5a6472;--mut:#8a93a3;--line:#e5e8ee;--line2:#eef1f5;
  --accent:#1c7ed6;--accent-soft:#e7f1fb;--code:#f0f2f6;
  --jp:"Hiragino Kaku Gothic ProN","Yu Gothic Medium","Yu Gothic",Meiryo,"Noto Sans JP",system-ui,sans-serif}
:root[data-theme="dark"]{--bg:#12151b;--panel:#1a1e26;--ink:#e8ebf1;--sub:#a6afbe;--mut:#727c8c;
  --line:#2a2f3a;--line2:#232833;--accent:#4dabf7;--accent-soft:#16283b;--code:#232833}
*{box-sizing:border-box}html,body{margin:0}
body{background:var(--bg);color:var(--ink);font-family:var(--jp);line-height:1.85;font-feature-settings:"palt" 1;
  -webkit-font-smoothing:antialiased}
.doc{max-width:820px;margin:0 auto;padding:44px 28px 140px}
.doc h1{font-size:26px;margin:.1em 0 .7em;line-height:1.4}
.doc h2{font-size:20px;margin:1.7em 0 .5em;padding-bottom:.25em;border-bottom:1px solid var(--line)}
.doc h3{font-size:16.5px;margin:1.3em 0 .4em}
.doc h4{font-size:14.5px;margin:1em 0 .3em;color:var(--sub)}
.doc p{margin:.7em 0}
.doc a{color:var(--accent);word-break:break-all}
.doc strong{font-weight:800}
.doc code{background:var(--code);padding:.12em .4em;border-radius:5px;font-size:.9em;
  font-family:ui-monospace,SFMono-Regular,Menlo,monospace}
.doc pre{background:var(--code);padding:14px 16px;border-radius:10px;overflow:auto;border:1px solid var(--line)}
.doc pre code{background:none;padding:0}
.doc ul,.doc ol{padding-left:1.4em;margin:.5em 0}.doc li{margin:.3em 0}
.doc hr{border:none;border-top:1px solid var(--line);margin:1.6em 0}
.doc blockquote{margin:.9em 0;padding:.5em 1em;border-left:3px solid var(--accent);background:var(--accent-soft);
  border-radius:0 8px 8px 0;color:var(--sub)}
.doc table{border-collapse:collapse;width:100%;margin:1em 0;font-size:13.5px}
.doc th,.doc td{border:1px solid var(--line);padding:7px 10px;text-align:left;vertical-align:top;overflow-wrap:anywhere}
.doc td code,.doc th code{word-break:break-all}
.doc th{background:var(--line2);font-weight:700;white-space:nowrap}
.doc tr:nth-child(even) td{background:var(--line2)}
.doc li.task{list-style:none;margin-left:-1.4em}
.doc li.task input{margin-right:.55em;width:15px;height:15px;vertical-align:-2px;cursor:pointer;
  accent-color:var(--accent)}
.doc li.task.done{color:var(--mut);text-decoration:line-through}
.tgl{position:fixed;top:14px;right:16px;border:1px solid var(--line);background:var(--panel);color:var(--sub);
  border-radius:9px;padding:7px 11px;cursor:pointer;font:inherit;font-size:13px}
.thr-bar{display:flex;gap:8px;justify-content:flex-end;margin:0 0 16px}
.thr-b{border:1px solid var(--line);background:var(--panel);color:var(--sub);border-radius:8px;
  padding:5px 10px;cursor:pointer;font:inherit;font-size:12.5px}
.thr-b[aria-pressed="false"]{opacity:.55;text-decoration:line-through}
.ent{border:1px solid var(--line);border-radius:12px;background:var(--panel);margin:0 0 14px}
.ent>summary{list-style:none;cursor:pointer;padding:12px 16px;display:flex;flex-wrap:wrap;
  align-items:baseline;gap:10px;border-radius:12px}
.ent>summary::-webkit-details-marker{display:none}
.ent>summary:hover{background:var(--line2)}
.ent[open]>summary{border-bottom:1px solid var(--line2);border-radius:12px 12px 0 0}
.ent-w{color:var(--mut);font-size:12.5px;font-variant-numeric:tabular-nums;white-space:nowrap}
.ent-q{font-weight:800;flex:1 1 12em;overflow-wrap:anywhere}
.ent-n{background:var(--accent);color:#fff;border-radius:999px;padding:1px 9px;font-size:11px;white-space:nowrap}
.ent-b{padding:2px 16px 10px}
.ent-b>:first-child{margin-top:.6em}
.ent-b h2{font-size:17px}
`

// js は配色の切り替え(localStorage に保存・初期値は OS の設定)と、チェックリストの消し込み
// (項目テキストをキーに localStorage へ保存。再生成しても同じテキストなら状態が残る)。
// スレッドではエントリの id も鍵に混ぜる——混ぜないと、同じ文言の項目を含む回答を上に足したときに
// 出現順がずれ、古いチェックが新しい項目へ移る(codex 指摘 2026-09-06)。1 枚ものは鍵が変わらない。
// 原型にあった常駐サーバ向けの自動リロード(SSE)は持ち込まない(設計判断 2026-09-03)。
const js = `(function(){var t=document.getElementById('t');` +
	`function ap(v){if(v==='dark')document.documentElement.setAttribute('data-theme','dark');` +
	`else document.documentElement.removeAttribute('data-theme');}` +
	`try{var s=localStorage.getItem('ans-theme');if(s)ap(s);` +
	`else if(window.matchMedia&&matchMedia('(prefers-color-scheme: dark)').matches)ap('dark');}catch(e){}` +
	`if(t)t.addEventListener('click',function(){var d=document.documentElement.getAttribute('data-theme')==='dark';` +
	`ap(d?'light':'dark');try{localStorage.setItem('ans-theme',d?'light':'dark');}catch(e){}});})();` +
	`(function(){var seen={};var cbs=document.querySelectorAll('li.task>input[type=checkbox]');` +
	`for(var i=0;i<cbs.length;i++){(function(cb){var li=cb.parentElement;` +
	`var ent=li.closest?li.closest('details.ent'):null;var sc=ent?ent.id+':':'';` +
	`var txt=(li.textContent||'').replace(/\s+/g,' ').trim().slice(0,120);` +
	`var n=seen[sc+txt]=(seen[sc+txt]||0)+1;` +
	`var key='ans-task:'+document.title+':'+sc+txt+(n>1?'#'+n:'');` +
	`function sync(){li.classList.toggle('done',cb.checked);}` +
	`try{var s=localStorage.getItem(key);if(s!==null)cb.checked=(s==='1');}catch(e){}` +
	`sync();` +
	`cb.addEventListener('change',function(){sync();` +
	`try{localStorage.setItem(key,cb.checked?'1':'0');}catch(e){}});})(cbs[i]);}})();`

// threadJS はスレッド HTML(ThreadPage)だけに足す分。
//  1. 開閉の記憶: 畳んだ・開いたという操作を localStorage に覚え、次に開いたときその状態に戻す。
//     既読を自動で判定しない(2026-09-06 に「画面に 3 秒以上入ったら既読」を撤回。自己リロードと噛み合って
//     読んでいる最中にエントリが畳まれたため)。「新着」は一度も開閉していないエントリの印。
//     操作を拾うのは summary のクリックで、details の toggle イベントではない——Chromium は初期表示の
//     `<details open>` にも toggle を投げるので、toggle で記録すると読み込んだ瞬間に全エントリが
//     「操作済み」になり、「新着」が一度も出なかった(2026-09-06 実測 → docs/notes/common/details-toggle-on-load.md)。
//  2. 全部開く / 全部畳む のボタン。
//  3. 自己リロード: 常駐サーバを置かない代わりに 12 秒ごとに自分を読み直す(決定 2026-09-05)。
//     隠れているタブ・文字を選択中は止め、スクロール位置は復元する。ボタンで止められる。
//     入力欄を持つ HTML(approvals)には入れない。
const threadJS = `
(function(){
var es=document.querySelectorAll('details.ent');
if(!es.length)return;
var ns=(location.pathname.split('/').pop()||'thread');
function get(id){try{return localStorage.getItem('ans-open:'+ns+':'+id);}catch(e){return null;}}
function put(id,v){try{localStorage.setItem('ans-open:'+ns+':'+id,v?'1':'0');}catch(e){}}
function unbadge(d){var n=d.querySelector('.ent-n');if(n)n.parentNode.removeChild(n);}
function mark(d){put(d.id,d.open);unbadge(d);}
for(var i=0;i<es.length;i++){(function(d){
var s=get(d.id);
if(s!==null){d.open=(s==='1');unbadge(d);}
var sm=d.querySelector('summary');
if(sm)sm.addEventListener('click',function(){setTimeout(function(){mark(d);},0);});
})(es[i]);}
var o=document.getElementById('thr-open'),c=document.getElementById('thr-close');
function all(v){for(var i=0;i<es.length;i++){es[i].open=v;mark(es[i]);}}
if(o)o.addEventListener('click',function(){all(true);});
if(c)c.addEventListener('click',function(){all(false);});
})();
(function(){
var b=document.getElementById('thr-auto');
if(!b)return;
var ns=(location.pathname.split('/').pop()||'thread'),ak='ans-auto:'+ns,sk='ans-scroll:'+ns;
var on=true;try{if(localStorage.getItem(ak)==='0')on=false;}catch(e){}
function draw(){b.setAttribute('aria-pressed',on?'true':'false');
b.title=on?'12 秒ごとに読み直す':'自動更新は止めてある';}
draw();
b.addEventListener('click',function(){on=!on;try{localStorage.setItem(ak,on?'1':'0');}catch(e){}draw();});
if('scrollRestoration' in history)history.scrollRestoration='manual';
try{var y=sessionStorage.getItem(sk);if(y)window.scrollTo(0,parseInt(y,10)||0);}catch(e){}
addEventListener('scroll',function(){try{sessionStorage.setItem(sk,String(window.pageYOffset));}catch(e){}});
setInterval(function(){
if(!on||document.hidden)return;
var s=window.getSelection&&window.getSelection().toString();
if(s&&s.trim())return;
location.reload();
},12000);
})();`

// Linkify は HTML 本文中の裸の URL を <a> にする。タグの中(属性値)と、既存の <a>・<code>・<pre> の中は触らない。
// 原型は本文全体に正規表現をかけていたため、Markdown リンクの href の中まで再リンクして HTML を壊していた
// (2026-09-03 に確認)。移植でその穴を塞いだ。
func Linkify(h string) string {
	var b strings.Builder
	i := 0
	for i < len(h) {
		j := strings.IndexByte(h[i:], '<')
		if j < 0 {
			b.WriteString(linkText(h[i:]))
			break
		}
		b.WriteString(linkText(h[i : i+j]))
		i += j
		rest := h[i:]
		skipTo := ""
		switch {
		case strings.HasPrefix(rest, "<a "), strings.HasPrefix(rest, "<a>"):
			skipTo = "</a>"
		case strings.HasPrefix(rest, "<code"):
			skipTo = "</code>"
		case strings.HasPrefix(rest, "<pre"):
			skipTo = "</pre>"
		}
		if skipTo != "" {
			if k := strings.Index(rest, skipTo); k >= 0 {
				b.WriteString(rest[:k+len(skipTo)])
				i += k + len(skipTo)
				continue
			}
		}
		k := strings.IndexByte(rest, '>')
		if k < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:k+1])
		i += k + 1
	}
	return b.String()
}

func linkText(s string) string {
	return urlRE.ReplaceAllStringFunc(s, func(u string) string {
		return `<a href="` + u + `" target="_blank" rel="noopener">` + u + `</a>`
	})
}

// Page は Markdown を自己完結の HTML 文書にする(CSS・JS 埋め込み・外部読み込み無し)。
// チェックボックスは表示専用(disabled)でなくクリック可能にし、消し込みを JS が localStorage に残す。
func Page(md, title string) string { return PageWith(md, title, Options{}) }

// PageWith は Page に変換の設定を渡す形。相対パスの基準(Options.BaseDir)を指定できる。
func PageWith(md, title string, opt Options) string {
	return Shell(title, RenderBody(md, opt), Parts{})
}

// RenderBody は Markdown を本文の HTML にする(裸の URL のリンク化と、チェックボックスの有効化まで)。
// Shell と組にして使う。explain のように本文を自分で組み立てるコマンドが、この 2 つを直に呼ぶ。
func RenderBody(md string, opt Options) string {
	body := Linkify(BodyWith(md, opt))
	body = strings.ReplaceAll(body, `<input type="checkbox" disabled checked>`, `<input type="checkbox" checked>`)
	return strings.ReplaceAll(body, `<input type="checkbox" disabled>`, `<input type="checkbox">`)
}

// Parts は Shell に足す追加分。ゼロ値が answer の 1 枚もの。
type Parts struct {
	CSS       string // 共通の css の後ろに足す(同じ指定は後勝ちで上書きできる)
	JS        string // 共通の js の後ろに足す
	MainClass string // <main> の class に足す語(既定の "doc" は必ず付く)
}

// Shell は本文の HTML を 1 枚の文書に包む。
func Shell(title, main string, p Parts) string {
	cls := "doc"
	if p.MainClass != "" {
		cls += " " + p.MainClass
	}
	return "<!doctype html>\n<html lang=\"ja\">\n<head>\n<meta charset=\"utf-8\">\n" +
		"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n" +
		"<title>" + escapeAttr(title) + "</title>\n<style>" + css + p.CSS + "</style>\n</head>\n<body>\n" +
		`<main class="` + cls + `">` + main + "</main>\n" + `<button id="t" class="tgl">◐ 表示</button>` + "\n" +
		"<script>" + js + p.JS + "</script>\n</body>\n</html>\n"
}
