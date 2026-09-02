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
`

// js は配色の切り替え(localStorage に保存・初期値は OS の設定)と、チェックリストの消し込み
// (項目テキストをキーに localStorage へ保存。再生成しても同じテキストなら状態が残る)。
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
	`var txt=(li.textContent||'').replace(/\s+/g,' ').trim().slice(0,120);` +
	`var n=seen[txt]=(seen[txt]||0)+1;` +
	`var key='ans-task:'+document.title+':'+txt+(n>1?'#'+n:'');` +
	`function sync(){li.classList.toggle('done',cb.checked);}` +
	`try{var s=localStorage.getItem(key);if(s!==null)cb.checked=(s==='1');}catch(e){}` +
	`sync();` +
	`cb.addEventListener('change',function(){sync();` +
	`try{localStorage.setItem(key,cb.checked?'1':'0');}catch(e){}});})(cbs[i]);}})();`

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
func Page(md, title string) string {
	body := Linkify(Body(md))
	body = strings.ReplaceAll(body, `<input type="checkbox" disabled checked>`, `<input type="checkbox" checked>`)
	body = strings.ReplaceAll(body, `<input type="checkbox" disabled>`, `<input type="checkbox">`)
	return "<!doctype html>\n<html lang=\"ja\">\n<head>\n<meta charset=\"utf-8\">\n" +
		"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n" +
		"<title>" + escapeAttr(title) + "</title>\n<style>" + css + "</style>\n</head>\n<body>\n" +
		`<main class="doc">` + body + "</main>\n" + `<button id="t" class="tgl">◐ 表示</button>` + "\n" +
		"<script>" + js + "</script>\n</body>\n</html>\n"
}
