package news

import (
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strings"

	"github.com/pilefort/braindex/internal/feed"
	"github.com/pilefort/braindex/internal/weblink"
)

// SelectionType は選別 JSON の type。HTML の「選別を書き出す」が出し、news apply が読む。
const SelectionType = "braindex-news-selection"

// SelectionPrefix は選別 JSON のファイル名の接頭辞(Downloads から拾う目印)。
const SelectionPrefix = "braindex-news-selection_"

// RenderHTML は選別 UI 付きの自己完結 HTML(外部 JS・CSS 無し・LF)を組む。
// フィードは分類(category)ごとにまとめ、各フィードで 主要 → 関心外と判定(<details> で折りたたみ) の順。
// 「残す／不要」はブラウザの localStorage に覚え、「選別を書き出す」で JSON をダウンロードする(サーバ不要)。
// 同じ入力からは同じバイト列になる(生成日時を入れない。日付は o.Today)。
func RenderHTML(results []Result, o DigestOptions) []byte {
	byCat := map[string][]Result{}
	var failures, empty []string
	totalNew, totalMain := 0, 0
	for _, r := range results {
		if r.Err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", r.Source.Name, r.Err))
			continue
		}
		if len(r.New) == 0 {
			empty = append(empty, r.Source.Name)
			continue
		}
		byCat[r.Source.Category] = append(byCat[r.Source.Category], r)
		totalNew += len(r.New)
	}
	cats := make([]string, 0, len(byCat))
	for c := range byCat {
		cats = append(cats, c)
	}
	sort.Strings(cats)

	var parts strings.Builder
	for _, cat := range cats {
		if cat != "" {
			fmt.Fprintf(&parts, "<h2>%s</h2>\n", esc(cat))
		}
		for _, r := range byCat[cat] {
			main, low := Split(r.New, o.Ranking, o.MinScore)
			shown := capped(main, o.Cap)
			totalMain += len(shown)
			fmt.Fprintf(&parts, "<h3>%s（新着 %d 件", esc(r.Source.Name), len(r.New))
			if o.Ranking != nil {
				fmt.Fprintf(&parts, "・主要 %d 件", len(main))
			}
			parts.WriteString("）</h3>\n<ul>\n")
			for _, e := range shown {
				parts.WriteString(itemHTML(e, r, o, false))
			}
			parts.WriteString("</ul>\n")
			if len(main) > len(shown) {
				fmt.Fprintf(&parts, "<small>…他 %d 件は省略（上限 %d 件/フィード）</small>\n", len(main)-len(shown), o.Cap)
			}
			if len(low) > 0 {
				fmt.Fprintf(&parts, "<details class=\"lowbox\"><summary>関心外と判定 %d 件（展開して確認。ここから「残す」＝採点の見逃しとして記録）</summary>\n<ul>\n", len(low))
				lowShown := capped(low, o.Cap)
				for _, e := range lowShown {
					parts.WriteString(itemHTML(e, r, o, true))
				}
				parts.WriteString("</ul>\n")
				if len(low) > len(lowShown) {
					fmt.Fprintf(&parts, "<small>…他 %d 件は省略</small>\n", len(low)-len(lowShown))
				}
				parts.WriteString("</details>\n")
			}
		}
	}
	items := parts.String()
	if items == "" {
		items = "<p>新着はありません。</p>\n"
	}

	var foot []string
	if len(empty) > 0 {
		foot = append(foot, "新着なし: "+esc(strings.Join(empty, ", ")))
	}
	if len(failures) > 0 {
		foot = append(foot, "<b class=\"prune\">取得失敗:</b> "+esc(strings.Join(failures, " / ")))
	}
	if len(o.Totals) > 0 {
		cands := map[string]bool{}
		for _, f := range PruneCandidates(o.Totals, PruneMinShown) {
			cands[f] = true
		}
		names := make([]string, 0, len(o.Totals))
		for f := range o.Totals {
			names = append(names, f)
		}
		sort.Strings(names)
		var lines []string
		for _, f := range names {
			d := o.Totals[f]
			line := fmt.Sprintf("%s: 残す %d / 見た %d", esc(f), d.Kept, d.Shown)
			if d.Hidden > 0 {
				line += fmt.Sprintf("・関心外 %d 件中 救済 %d", d.Hidden, d.Rescued)
			}
			if cands[f] {
				line += " <b class=\"prune\">← 間引き候補（一度も残していない）</b>"
			}
			lines = append(lines, line)
		}
		foot = append(foot, "<b>選別の反映状況（累積）:</b><br>"+strings.Join(lines, "<br>")+
			"<br><small>救済 = 関心外と判定されたのに残した件数（採点の見逃し）。増えるフィードは news/interests.md に関心語を足す。"+
			"間引きは news/feeds.json から該当行を消す。</small>")
	}
	scoring := "採点なし（全件を主要表示）"
	if o.Ranking != nil {
		scoring = fmt.Sprintf("関心度は語の一致（braindex news profile）。%d 以上を主要表示", o.MinScore)
		if o.Annotations != nil {
			scoring = fmt.Sprintf("関心度は語の一致（braindex news profile）に LLM 補助（news.llm・バッジの説明に LLM と出る）を重ねたもの。%d 以上を主要表示", o.MinScore)
		}
	}
	foot = append(foot, fmt.Sprintf("生成: %s / braindex news fetch（取得・既読・採点は決定論。%s）", esc(o.Today), scoring))

	title := fmt.Sprintf("ニュースダイジェスト %s（%s 層・新着 %d 件・主要 %d 件）", o.Today, o.Layer, totalNew, totalMain)
	out := strings.NewReplacer(
		"__TITLE__", esc(title),
		"__DATE_JS__", jsString(o.Today),
		"__LAYER_JS__", jsString(o.Layer),
		"__ITEMS__", items,
		"__FOOTER__", strings.Join(foot, "<br><br>"),
	).Replace(htmlTemplate)
	return []byte(out)
}

func capped(es []feed.Entry, cap int) []feed.Entry {
	if cap > 0 && len(es) > cap {
		return es[:cap]
	}
	return es
}

func esc(s string) string { return html.EscapeString(s) }

// jsString は <script> の中に置ける JS の文字列リテラル(引用符を含む)にする。
// HTML の実体参照は <script> の中では復号されないので、esc では値が壊れる(層に & や " が入ると
// &amp; のまま JS の値になり、選別 JSON の layer が実際の層と食い違う)。
// encoding/json は既定で < > & をユニコードエスケープに逃がすので、閉じタグで script の外に出ることもない。
func jsString(s string) string {
	b, err := json.Marshal(s)
	if err != nil { // string の Marshal は失敗しないが、握りつぶさずに安全側へ倒す
		return `""`
	}
	return string(b)
}

// itemHTML は 1 項目の <li>。data-* に選別 JSON へ書く値を持たせる。low は折りたたみ側。
func itemHTML(e feed.Entry, r Result, o DigestOptions, low bool) string {
	cls, lowFlag := "item", "0"
	if low {
		cls, lowFlag = "item low", "1"
	}
	score, badge := "", ""
	if o.Ranking != nil {
		s := o.Ranking[e.ID]
		score = fmt.Sprint(s.Value)
		tip := "関心度"
		if len(s.Matched) > 0 {
			tip += "（" + strings.Join(s.Matched, "・") + "）"
		}
		badge = fmt.Sprintf("<span class=\"r r%d\" title=\"%s\">%d</span>", s.Value, esc(tip), s.Value)
	}
	date := ""
	if e.Published != "" {
		date = "<small> " + esc(e.Published) + "</small>"
	}
	sum := ""
	if e.Summary != "" {
		sum = "<div class=\"sum\">" + esc(e.Summary) + "</div>"
	}
	// LLM 補助の訳(見出し・概要)。原文の下に添える(原文は残す。訳の誤りを見比べられるように)
	if a, ok := o.Annotations[e.ID]; ok && a.Title != "" {
		tr := "訳: " + esc(a.Title)
		if a.Summary != "" {
			tr += " — " + esc(a.Summary)
		}
		sum = "<div class=\"sum\">" + tr + "</div>" + sum
	}
	// フィード由来のリンクは信用しない。http(s) でなければ表示にも選別 JSON(data-link)にも載せず、
	// 題名だけを出す(決定 2026-09-03「生成物のリンクは http(s) 以外を落とす」)。
	link := e.Link
	if !weblink.Safe(link) {
		link = ""
	}
	title := esc(e.Title)
	if link != "" {
		title = "<a href=\"" + esc(link) + "\" target=\"_blank\" rel=\"noopener\">" + title + "</a>"
	}
	return fmt.Sprintf("<li class=\"%s\" data-id=\"%s\" data-title=\"%s\" data-link=\"%s\" data-feed=\"%s\" data-cat=\"%s\" data-low=\"%s\" data-r=\"%s\">"+
		"<span class=\"btns\"><button class=\"bk\">残す</button><button class=\"bd\">不要</button></span>%s"+
		"<span>%s%s%s</span></li>\n",
		cls, esc(e.ID), esc(e.Title), esc(link), esc(r.Source.Name), esc(r.Source.Category), lowFlag, score,
		badge, title, date, sum)
}

// htmlTemplate は原型(news_collect.py)の選別 UI を、固有の文言を外して移したもの。
// 選別 JSON: {type, date, layer, exported_at, keeps: [{id, title, link, feed, category, score, rescued}], feed_stats: {feed: {shown, kept, dropped, hidden, rescued}}}
const htmlTemplate = `<!doctype html>
<html lang="ja"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>__TITLE__</title>
<style>
:root{color-scheme:light dark;--fg:#1a1a1a;--bg:#fff;--dim:#777;--line:#e4e4e4;--keep:#0a7a33;--keepbg:#e9f7ee;--drop:#a33;--accent:#0b62c4}
@media(prefers-color-scheme:dark){:root{--fg:#ddd;--bg:#181a1b;--dim:#888;--line:#333;--keepbg:#12301c;--accent:#5aa2e8}}
body{font-family:"Segoe UI","Hiragino Sans","Noto Sans JP",sans-serif;margin:0;background:var(--bg);color:var(--fg)}
header{position:sticky;top:0;background:var(--bg);border-bottom:1px solid var(--line);padding:10px 16px;z-index:9}
h1{font-size:1.05rem;margin:0 0 6px}
.bar{display:flex;gap:12px;align-items:center;flex-wrap:wrap;font-size:.85rem}
.bar .cnt b{font-weight:600}
button.export{background:var(--accent);color:#fff;border:0;border-radius:6px;padding:6px 14px;cursor:pointer;font-size:.85rem}
.help{color:var(--dim);font-size:.75rem}
main{max-width:960px;margin:0 auto;padding:12px 16px 60px}
h2{font-size:1rem;border-bottom:1px solid var(--line);padding-bottom:4px;margin:26px 0 8px}
h3{font-size:.85rem;color:var(--dim);margin:14px 0 4px;font-weight:600}
ul{list-style:none;margin:0;padding:0}
li.item{display:flex;gap:8px;align-items:baseline;padding:5px 6px;border-radius:6px;border-left:3px solid transparent}
li.item.cur{outline:1px solid var(--accent)}
li.item.keep{background:var(--keepbg);border-left-color:var(--keep)}
li.item.drop{opacity:.42}
li.item.drop a{text-decoration:line-through}
.btns{display:flex;gap:4px;flex:none}
.btns button{border:1px solid var(--line);background:transparent;color:var(--fg);border-radius:5px;cursor:pointer;font-size:.72rem;padding:1px 7px}
li.keep .bk,li.drop .bd{background:var(--accent);color:#fff;border-color:var(--accent)}
a{color:var(--accent);text-decoration:none}a:hover{text-decoration:underline}
small{color:var(--dim)}
.sum{color:var(--dim);font-size:.8rem;margin:1px 0 0;line-height:1.45}
.r{flex:none;font-size:.68rem;color:var(--dim);border:1px solid var(--line);border-radius:4px;padding:0 5px}
.r3{color:var(--keep);border-color:var(--keep)}
li.item.low{opacity:.75}
details.lowbox{margin:4px 0 0 6px}
details.lowbox>summary{cursor:pointer;color:var(--dim);font-size:.8rem;padding:3px 0}
footer{max-width:960px;margin:0 auto;padding:10px 16px 40px;color:var(--dim);font-size:.8rem;border-top:1px solid var(--line)}
.prune{color:var(--drop)}
</style></head><body>
<header>
<h1>__TITLE__</h1>
<div class="bar">
<span class="cnt">残す <b id="nK">0</b> ／ 不要 <b id="nD">0</b> ／ 未 <b id="nU">0</b></span>
<button class="export" id="exp">選別を書き出す</button>
<span class="help">クリック or キー: j/k 移動・f 残す・x 不要・u 取消 ／ 書き出した JSON は braindex news apply（と次回の fetch）が取り込む（同じ日の再書き出しは上書き） ／ 数字バッジ=関心度。「関心外と判定」は折りたたみ、そこから残す＝採点の見逃しとして記録</span>
</div>
</header>
<main>
__ITEMS__</main>
<footer>__FOOTER__</footer>
<script>
const META={date:__DATE_JS__,layer:__LAYER_JS__};
const LSKEY="braindex-news-"+META.date+"-"+META.layer;
let state={};try{state=JSON.parse(localStorage.getItem(LSKEY)||"{}")}catch(e){}
const items=[...document.querySelectorAll("li.item")];let cur=-1;
function paint(){let k=0,d=0;items.forEach(li=>{const s=state[li.dataset.id];li.classList.toggle("keep",s==="keep");li.classList.toggle("drop",s==="drop");if(s==="keep")k++;else if(s==="drop")d++});
document.getElementById("nK").textContent=k;document.getElementById("nD").textContent=d;document.getElementById("nU").textContent=items.length-k-d;
localStorage.setItem(LSKEY,JSON.stringify(state));}
function setS(li,v){const id=li.dataset.id;if(v===null)delete state[id];else state[id]=v;paint();}
items.forEach(li=>{li.querySelector(".bk").onclick=()=>setS(li,state[li.dataset.id]==="keep"?null:"keep");
li.querySelector(".bd").onclick=()=>setS(li,state[li.dataset.id]==="drop"?null:"drop");});
function move(d){if(items.length===0)return;if(cur>=0)items[cur].classList.remove("cur");cur=Math.min(items.length-1,Math.max(0,cur+d));const li=items[cur];li.classList.add("cur");const dt=li.closest("details");if(dt)dt.open=true;li.scrollIntoView({block:"center"});}
document.addEventListener("keydown",e=>{if(e.target.tagName==="INPUT")return;
if(e.key==="j")move(1);else if(e.key==="k")move(-1);
else if(cur>=0&&e.key==="f")setS(items[cur],"keep");
else if(cur>=0&&e.key==="x")setS(items[cur],"drop");
else if(cur>=0&&e.key==="u")setS(items[cur],null);});
document.getElementById("exp").onclick=()=>{
const keeps=[],stats={};
items.forEach(li=>{const f=li.dataset.feed,s=state[li.dataset.id],low=li.dataset.low==="1";
const st=stats[f]=stats[f]||{shown:0,kept:0,dropped:0,hidden:0,rescued:0};
if(low)st.hidden++;else st.shown++;
if(s==="keep"){if(low)st.rescued++;else st.kept++;keeps.push({id:li.dataset.id,title:li.dataset.title,link:li.dataset.link,feed:f,category:li.dataset.cat,score:li.dataset.r||"",rescued:low});}
else if(s==="drop"&&!low)st.dropped++;});
const payload={type:"braindex-news-selection",date:META.date,layer:META.layer,exported_at:new Date().toISOString(),keeps,feed_stats:stats};
const ts=new Date().toISOString().replace(/[-:T]/g,"").slice(0,14);
const a=document.createElement("a");
a.href=URL.createObjectURL(new Blob([JSON.stringify(payload,null,1)],{type:"application/json"}));
a.download="braindex-news-selection_"+META.date+"_"+META.layer+"_"+ts+".json";a.click();
document.getElementById("exp").textContent="書き出し済み ✓（braindex news apply で反映）";};
paint();
</script></body></html>
`
