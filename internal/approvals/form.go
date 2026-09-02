package approvals

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
)

// Meta はフォームの見出し・照合に使う情報。同じ Meta と Doc からは同じ HTML が出る(nonce を固定すればテストでバイト一致)。
type Meta struct {
	Project     string // hub の名前(タイトルに出す)
	Path        string // APPROVALS.md の表示用パス
	Nonce       string // 起動ごとの照合値。フォームが POST に載せ、serve が照合する
	GeneratedAt string // 生成日時(表示用)
}

// RenderForm は判断待ちを自己完結の HTML フォームにする。CSS/JS は埋め込み、外部読み込みは無い。
// 項目ごとに 5 欄の表示・選択肢のラジオ(A/B/…・その他・保留)・自由記述を置き、「決定を送信」で
// 同じ配信元の /reply へ JSON を POST する。送れないときは JSON を表示してチャットに貼れるようにする。
func RenderForm(d Doc, m Meta) []byte {
	var b strings.Builder
	title := "承認待ち — " + m.Project
	metaJSON, _ := json.Marshal(map[string]string{"nonce": m.Nonce})
	b.WriteString("<!doctype html>\n<html lang=\"ja\">\n<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	fmt.Fprintf(&b, "<title>%s</title>\n<style>%s</style>\n</head>\n<body>\n<main class=\"doc\">\n", html.EscapeString(title), formCSS)
	fmt.Fprintf(&b, "<h1>%s</h1>\n", html.EscapeString(title))
	b.WriteString("<p class=\"lead\">各項目について、<b>選択肢を 1 つ選ぶ</b>（必要なら下の欄に補足）→ 最後に <b>決定を送信</b>。" +
		"送信すると <code>braindex approvals apply</code> が <code>docs/decisions.md</code> に理由ごと記録し、APPROVALS.md から消します。" +
		"決めかねるときは「保留」を選び、欲しい情報や質問を書いてください。</p>\n")
	fmt.Fprintf(&b, "<p class=\"meta\">%s ／ 生成 %s</p>\n", html.EscapeString(m.Path), html.EscapeString(m.GeneratedAt))
	if len(d.Items) == 0 {
		b.WriteString("<p class=\"empty\">承認待ちはありません。</p>\n")
	} else {
		if pre := preambleBody(d.Preamble); pre != "" {
			fmt.Fprintf(&b, "<div class=\"why\">%s</div>\n", inline(pre))
		}
		for i := range d.Items {
			renderItem(&b, &d.Items[i], i+1)
		}
	}
	b.WriteString("</main>\n")
	if len(d.Items) > 0 {
		b.WriteString("<div class=\"bar\"><button id=\"send\">決定を送信</button><span id=\"st\" class=\"st\"></span>" +
			"<textarea id=\"fb\" hidden></textarea></div>\n")
	}
	fmt.Fprintf(&b, "<script id=\"meta\" type=\"application/json\">%s</script>\n<script>%s</script>\n</body>\n</html>\n", metaJSON, formJS)
	return []byte(b.String())
}

// preambleBody は前文から H1 行を除いた説明文を返す(「（なし…」の行も除く)。
func preambleBody(pre string) string {
	var out []string
	for _, l := range strings.Split(pre, "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "# ") || strings.HasPrefix(t, "（なし") {
			continue
		}
		out = append(out, t)
	}
	return strings.Join(out, "\n")
}

var displayFields = []struct{ key, label string }{
	{FieldWhat, "何を決めるか"}, {FieldWhyNow, "なぜ今決めるか"}, {FieldIfUndecided, "決めないとどうなるか"},
}

// renderItem は項目 1 件を描く。pos は 1 から数えた出現位置で、ラジオのグループ名と id に使う。
// 見出しの番号(it.N)は重なりうる(番号を振り忘れた見出しは出現順で補うため)ので、DOM の識別子には使わない。
// 重なると 2 つの項目が同じラジオグループになり、後の項目を選んだ瞬間に前の選択が外れる。
// it.N は表示と POST の data-n に残す(apply は題名で照合し、題名が空のときだけ番号を見る)。
func renderItem(b *strings.Builder, it *Item, pos int) {
	fmt.Fprintf(b, "<section class=\"ap\" data-n=\"%d\" data-title=\"%s\" id=\"i%d\">\n<h2><span class=\"num\">%d</span>%s</h2>\n",
		it.N, html.EscapeString(it.Title), pos, it.N, html.EscapeString(it.Title))
	b.WriteString("<div class=\"kv\">")
	for _, f := range displayFields {
		if v := it.Fields[f.key]; v != "" {
			fmt.Fprintf(b, "<div class=\"k\">%s</div><div class=\"v\">%s</div>", f.label, inline(v))
		} else {
			fmt.Fprintf(b, "<div class=\"k\">%s</div><div class=\"v miss\">（未記載）</div>", f.label)
		}
	}
	b.WriteString("</div>\n")
	if len(it.Warnings) > 0 {
		var ws []string
		for _, w := range it.Warnings {
			ws = append(ws, html.EscapeString(w))
		}
		fmt.Fprintf(b, "<div class=\"warn\">記載が足りません（Claude が書き直す必要あり）: %s</div>\n", strings.Join(ws, "、"))
	}
	for _, h := range it.Holds {
		fmt.Fprintf(b, "<div class=\"hold\">%s</div>\n", inline(h))
	}
	b.WriteString("<div class=\"sec\">選択肢（1 つ選ぶ）</div>\n<div class=\"opts\">")
	for _, o := range it.Options {
		badge, cls := "", ""
		if o.Key == it.Recommended {
			badge, cls = "<span class=\"badge\">私の案</span>", " rec"
		}
		fmt.Fprintf(b, "<label class=\"opt%s\"><input type=\"radio\" name=\"c%d\" value=\"%s\"><span class=\"key\">%s</span><span class=\"lab\">%s%s</span>",
			cls, pos, o.Key, o.Key, inline(o.Label), badge)
		if o.Desc != "" {
			fmt.Fprintf(b, "<div class=\"desc\">%s</div>", inline(o.Desc))
		}
		b.WriteString("</label>")
	}
	if len(it.Options) == 0 && it.Fields[FieldRecommend] != "" {
		fmt.Fprintf(b, "<div class=\"why\"><b>私の案（選択肢の形になっていない）:</b><br>%s</div>", inline(it.Fields[FieldRecommend]))
	}
	fmt.Fprintf(b, "<label class=\"opt alt\"><input type=\"radio\" name=\"c%d\" value=\"other\"><span class=\"key\">＋</span><span class=\"lab\">その他 — 下の欄に書く</span></label>", pos)
	fmt.Fprintf(b, "<label class=\"opt alt\"><input type=\"radio\" name=\"c%d\" value=\"hold\"><span class=\"key\">…</span><span class=\"lab\">保留 — 今は決めない（質問・欲しい情報を下に）</span></label>", pos)
	b.WriteString("</div>\n")
	switch {
	case it.Recommended != "" && it.Reason != "":
		fmt.Fprintf(b, "<div class=\"why\"><b>私の案 %s の理由:</b> %s</div>\n", it.Recommended, inline(it.Reason))
	case it.Recommended == "" && len(it.Options) > 0 && it.Fields[FieldRecommend] != "":
		fmt.Fprintf(b, "<div class=\"why\"><b>私の案:</b> %s</div>\n", inline(it.Fields[FieldRecommend]))
	}
	b.WriteString("<textarea placeholder=\"補足・理由・質問（任意。「その他」を選んだときは必須）\"></textarea>\n</section>\n")
}

var (
	codeRe = regexp.MustCompile("`([^`]+)`")
	boldRe = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	urlRe  = regexp.MustCompile(`https?://[^\s<>"'）」』]+`)
)

// inline は欄の値を HTML にする。エスケープしたうえで、`code`・**強調**・裸の URL だけを整形し、改行は <br>。
// Markdown の全部は要らない(欄は 1〜2 行の文)。
func inline(s string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		out = append(out, inlineLine(line))
	}
	return strings.Join(out, "<br>")
}

func inlineLine(line string) string {
	// コード片を先に退避し、その外側だけ強調と URL を処理する(コード内の * や URL を触らない)
	var codes []string
	esc := codeRe.ReplaceAllStringFunc(html.EscapeString(line), func(m string) string {
		codes = append(codes, "<code>"+m[1:len(m)-1]+"</code>")
		return fmt.Sprintf("\x00%d\x00", len(codes)-1)
	})
	esc = boldRe.ReplaceAllString(esc, "<b>$1</b>")
	esc = urlRe.ReplaceAllStringFunc(esc, func(u string) string {
		u = strings.TrimRight(u, ".,;:!?、。")
		return "<a href=\"" + u + "\">" + u + "</a>"
	})
	for i, c := range codes {
		esc = strings.Replace(esc, fmt.Sprintf("\x00%d\x00", i), c, 1)
	}
	return esc
}

const formCSS = `
:root{--bg:#fbfbfa;--ink:#1f2328;--sub:#57606a;--mut:#8c959f;--panel:#fff;--line:#d8dee4;--accent:#0969da;--accent-soft:#ddf4ff}
@media (prefers-color-scheme:dark){:root{--bg:#0d1117;--ink:#e6edf3;--sub:#9da7b3;--mut:#6e7681;--panel:#161b22;--line:#30363d;--accent:#58a6ff;--accent-soft:#1c2a3a}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--ink);font:15px/1.7 system-ui,-apple-system,"Segoe UI",Roboto,"Hiragino Sans","Noto Sans JP",sans-serif}
.doc{max-width:860px;margin:0 auto;padding:28px 20px 120px}
h1{font-size:22px;margin:.2em 0 .4em}
code{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:.92em;background:var(--accent-soft);border-radius:4px;padding:0 .3em}
a{color:var(--accent)}
.lead{color:var(--sub);font-size:14px;margin:.2em 0 1.2em}
.meta{color:var(--mut);font-size:12.5px;word-break:break-all}
.ap{background:var(--panel);border:1px solid var(--line);border-radius:14px;padding:22px 24px;margin:18px 0}
.ap h2{margin:0 0 .6em;font-size:19px}
.ap .num{display:inline-block;background:var(--accent);color:#fff;border-radius:8px;padding:0 .5em;margin-right:.5em;font-size:14px}
.kv{display:grid;grid-template-columns:10em 1fr;gap:.45em 1em;margin:.4em 0 1em}
.kv .k{color:var(--sub);font-size:13px;font-weight:700;padding-top:.15em}
.kv .v{margin:0}.kv .v.miss{color:#c92a2a}
.sec{color:var(--sub);font-size:13px;font-weight:700;margin:.6em 0 .3em}
.opts{display:flex;flex-direction:column;gap:8px;margin:.2em 0 .8em}
.opt{display:grid;grid-template-columns:auto auto 1fr;gap:.15em .6em;align-items:baseline;border:1px solid var(--line);border-radius:10px;padding:10px 14px;cursor:pointer}
.opt:has(input:checked){border-color:var(--accent);background:var(--accent-soft)}
.opt input{margin:0;accent-color:var(--accent);align-self:center;width:16px;height:16px}
.opt .key{font-weight:800;color:var(--accent);min-width:1.2em}
.opt .lab{font-weight:700}
.opt .desc{grid-column:3/4;color:var(--sub);font-size:13.5px}
.opt.alt{border-style:dashed}.opt.alt .lab{font-weight:600;color:var(--sub)}
.badge{display:inline-block;font-size:11px;background:var(--accent);color:#fff;border-radius:6px;padding:0 .5em;margin-left:.6em;vertical-align:1px}
.why{background:var(--accent-soft);border-radius:8px;padding:8px 12px;font-size:13.5px;margin:.2em 0 .8em}
.warn{background:#fff4e5;color:#8a4b00;border:1px solid #ffd8a8;border-radius:8px;padding:8px 12px;font-size:13px;margin:.4em 0}
@media (prefers-color-scheme:dark){.warn{background:#3a2a12;color:#ffd8a8;border-color:#6b4a1a}}
.hold{font-size:13px;color:var(--sub);margin:.3em 0}
.ap textarea{width:100%;min-height:64px;border:1px solid var(--line);border-radius:8px;padding:8px 10px;font:inherit;font-size:14px;line-height:1.6;background:var(--bg);color:var(--ink);resize:vertical}
.bar{position:fixed;left:0;right:0;bottom:0;background:var(--panel);border-top:1px solid var(--line);padding:12px 20px;display:flex;gap:14px;align-items:center;flex-wrap:wrap;z-index:2}
.bar button{font:inherit;font-weight:700;background:var(--accent);color:#fff;border:none;border-radius:9px;padding:9px 18px;cursor:pointer}
.bar button:disabled{opacity:.6;cursor:default}
.st{font-size:13.5px;color:var(--sub)}.st.ok{color:#2b8a3e;font-weight:700}.st.err{color:#c92a2a}
#fb{width:100%;min-height:90px;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px}
body.sent .ap{opacity:.5;pointer-events:none}
.empty{color:var(--sub);padding:30px 0}
`

const formJS = `
(function(){
  var META=JSON.parse(document.getElementById('meta').textContent);
  var secs=Array.prototype.slice.call(document.querySelectorAll('section.ap'));
  function collect(){
    return secs.map(function(s){
      var r=s.querySelector('input[type=radio]:checked');
      var c=(s.querySelector('textarea')||{value:''}).value.trim();
      return {n:+s.dataset.n, title:s.dataset.title, choice:r?r.value:'', comment:c};
    }).filter(function(x){return x.choice;});
  }
  var btn=document.getElementById('send'), st=document.getElementById('st'), fb=document.getElementById('fb');
  if(!btn)return;
  btn.addEventListener('click',function(){
    var items=collect();
    var bad=items.filter(function(x){return x.choice==='other'&&!x.comment;});
    if(bad.length){st.textContent='「その他」を選んだ項目（'+bad.map(function(x){return x.n;}).join(', ')+'）は、どうするかを下の欄に書いてください';st.className='st err';return;}
    if(!items.length){st.textContent='どの項目も選ばれていません';st.className='st err';return;}
    var body={nonce:META.nonce,items:items};
    btn.disabled=true; st.textContent='送信中…'; st.className='st';
    fetch('/reply',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
      .then(function(r){return r.json().then(function(j){return {ok:r.ok,j:j};});})
      .then(function(x){
        if(!x.ok)throw new Error(x.j&&x.j.error||'error');
        st.textContent='送信しました（'+items.length+' 件）。このタブは閉じて構いません。'; st.className='st ok';
        document.body.classList.add('sent');
      })
      .catch(function(e){
        btn.disabled=false;
        st.textContent='送信できませんでした（'+e.message+'）。下の JSON をコピーしてチャットに貼ってください。'; st.className='st err';
        fb.hidden=false; fb.value=JSON.stringify({items:items},null,1);
      });
  });
})();
`
