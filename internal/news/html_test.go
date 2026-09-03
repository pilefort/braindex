package news

import (
	"errors"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/feed"
	"github.com/pilefort/braindex/internal/interest"
)

func TestRenderHTML(t *testing.T) {
	p := interest.Profile{Terms: []interest.Term{{Word: "ゴルーチン", Weight: 2}}}
	res := []Result{
		{Source: Source{Name: "A", Category: "tech"}, New: []feed.Entry{
			{ID: "1", Title: "ゴルーチン <入門>", Link: "https://x/1?a=1&b=2", Published: "2026-08-14", Summary: "概要 & 説明"},
			{ID: "2", Title: "関係ない", Link: "https://x/2"},
			{ID: "3", Title: "無関係 2", Link: "https://x/3"},
		}},
		{Source: Source{Name: "B", Category: "tech"}, Err: errors.New("HTTP 404")},
		{Source: Source{Name: "C"}, New: nil},
		{Source: Source{Name: "D"}, New: []feed.Entry{{ID: "d", Title: "分類なし", Link: "https://x/d"}}},
	}
	o := DigestOptions{Layer: "daily", Today: "2026-08-15", Cap: 1, Ranking: Rank(res, p), MinScore: 2}
	h := string(RenderHTML(res, o))
	for _, s := range []string{
		"<!doctype html>", "<title>ニュースダイジェスト 2026-08-15（daily 層・新着 4 件・主要 1 件）</title>", // 主要は表示した件数(A の 1 件。D は関心度 0)
		"<h2>tech</h2>", "<h3>A（新着 3 件・主要 1 件）</h3>",
		`<li class="item" data-id="1" data-title="ゴルーチン &lt;入門&gt;" data-link="https://x/1?a=1&amp;b=2" data-feed="A" data-cat="tech" data-low="0" data-r="2">`,
		`<span class="r r2" title="関心度（ゴルーチン）">2</span>`, `<a href="https://x/1?a=1&amp;b=2" target="_blank" rel="noopener">ゴルーチン &lt;入門&gt;</a><small> 2026-08-14</small><div class="sum">概要 &amp; 説明</div>`,
		"<summary>関心外と判定 2 件", `<li class="item low" data-id="2"`, "…他 1 件は省略</small>",
		"<h3>D（新着 1 件・主要 0 件）</h3>", `data-id="d"`,
		"新着なし: C", `<b class="prune">取得失敗:</b> B: HTTP 404`, "生成: 2026-08-15 / braindex news fetch", "2 以上を主要表示",
		`const META={date:"2026-08-15",layer:"daily"};`, `type:"braindex-news-selection"`, `a.download="braindex-news-selection_"`,
	} {
		if !strings.Contains(h, s) {
			t.Errorf("HTML に %q が無い", s)
		}
	}
	if strings.Contains(h, "<h2></h2>") || strings.Contains(h, "data-id=\"3\"") {
		t.Error("空の分類見出し、または上限を超えた項目が出た")
	}
	if strings.Contains(h, "<script src") || strings.Contains(h, "<link ") {
		t.Error("外部の JS / CSS を参照している")
	}
	if string(RenderHTML(res, o)) != h {
		t.Error("2 回の生成が一致しない")
	}

	// 累積の脚注: 名前順・救済の内訳・間引き候補
	o.Totals = map[string]FeedStats{"B": {Shown: 25, Dropped: 20}, "A": {Shown: 10, Kept: 2, Hidden: 5, Rescued: 1}}
	h3 := string(RenderHTML(res, o))
	mustHave := "<b>選別の反映状況（累積）:</b><br>A: 残す 2 / 見た 10・関心外 5 件中 救済 1<br>B: 残す 0 / 見た 25 <b class=\"prune\">← 間引き候補（一度も残していない）</b>"
	if !strings.Contains(h3, mustHave) {
		t.Errorf("脚注に %q が無い", mustHave)
	}
	if strings.Contains(h, "選別の反映状況") {
		t.Error("Totals 無しで脚注が出た")
	}
	// 決定性: 脚注は map(Totals)から組むので、走査順が出力に漏れていないかを繰り返して見る
	for i := 0; i < 5; i++ {
		if again := string(RenderHTML(res, o)); again != h3 {
			t.Fatalf("%d 回目の生成が一致しない: got=%q want=%q", i+2, again, h3)
		}
	}

	// 採点なし・新着なし
	h2 := string(RenderHTML([]Result{{Source: Source{Name: "A"}}}, DigestOptions{Layer: "all", Today: "2026-08-15", Cap: 20}))
	if !strings.Contains(h2, "<p>新着はありません。</p>") || !strings.Contains(h2, "採点なし（全件を主要表示）") || strings.Contains(h2, "class=\"r ") {
		t.Errorf("採点なし:\n%s", h2)
	}
}

// フィードのリンクは http(s) だけを載せる。javascript: などは href にも data-link にも出さず、題名だけ出す
// (決定 2026-09-03)。data-link にも出さないのは、選別 JSON 経由で keep に入るのを止めるため。
func TestRenderHTML_リンクのスキームを絞る(t *testing.T) {
	res := []Result{{Source: Source{Name: "A"}, New: []feed.Entry{
		{ID: "1", Title: "危ない", Link: "javascript:alert(1)"},
		{ID: "2", Title: "普通", Link: "https://x/2"},
	}}}
	h := string(RenderHTML(res, DigestOptions{Layer: "daily", Today: "2026-08-15", Cap: 20}))
	if strings.Contains(h, "javascript:") {
		t.Errorf("javascript: が HTML に残っている:\n%s", h)
	}
	if !strings.Contains(h, `data-id="1" data-title="危ない" data-link=""`) {
		t.Error("落としたリンクの data-link が空になっていない")
	}
	if !strings.Contains(h, `data-low="0" data-r=""><span class="btns"><button class="bk">残す</button><button class="bd">不要</button></span><span>危ない</span>`) {
		t.Error("題名がそのまま(リンクなしで)出ていない")
	}
	if !strings.Contains(h, `<a href="https://x/2" target="_blank" rel="noopener">普通</a>`) {
		t.Error("http(s) のリンクまで落としている")
	}
}

// <script> の中は HTML エスケープが効かない(実体参照が復号されない)ので、JS の文字列として埋める。
// layer は feeds.json と -layer が決める自由なラベルなので、記号が入りうる。
func TestRenderHTML_ScriptContext(t *testing.T) {
	res := []Result{{Source: Source{Name: "A"}, New: []feed.Entry{{ID: "1", Title: "t", Link: "https://x/1"}}}}
	h := string(RenderHTML(res, DigestOptions{Layer: `a"b&c`, Today: "2026-08-15", Cap: 20}))
	if !strings.Contains(h, `const META={date:"2026-08-15",layer:"a\"b\u0026c"};`) {
		t.Errorf("META が JS の文字列になっていない:\n%s", metaLine(h))
	}
	// <script> を閉じる文字列でも外に出ない
	h = string(RenderHTML(res, DigestOptions{Layer: "</script><b>x", Today: "2026-08-15", Cap: 20}))
	if strings.Contains(metaLine(h), "</script>") {
		t.Errorf("script を閉じられた:\n%s", metaLine(h))
	}
	// 記号を含まない層は今までどおりの見た目
	h = string(RenderHTML(res, DigestOptions{Layer: "daily", Today: "2026-08-15", Cap: 20}))
	if !strings.Contains(h, `const META={date:"2026-08-15",layer:"daily"};`) {
		t.Errorf("META:\n%s", metaLine(h))
	}
}

func metaLine(h string) string {
	for _, line := range strings.Split(h, "\n") {
		if strings.HasPrefix(line, "const META=") {
			return line
		}
	}
	return "(META 行が無い)"
}
