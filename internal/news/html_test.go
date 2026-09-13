package news

import (
	"context"
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
	o := DigestOptions{Layer: "daily", Today: "2026-08-15", Cap: 1, Ranking: Rank(res, p, nil), MinScore: 2}
	h := string(RenderHTML(res, o))
	for _, s := range []string{
		"<!doctype html>", "<title>ニュースダイジェスト 2026-08-15（daily 層・新着 4 件・主要 1 件）</title>", // 主要は表示した件数(A の 1 件。D は関心度 0)
		"<h2>tech</h2>", "<h3>A（新着 3 件・主要 1 件）</h3>",
		`<li class="item" data-id="1" data-title="ゴルーチン &lt;入門&gt;" data-link="https://x/1?a=1&amp;b=2" data-feed="A" data-cat="tech" data-low="0" data-r="2" data-summary="概要 &amp; 説明">`,
		`おすすめ · 関心に合った語: ゴルーチン`, `<h3 class="article-title">ゴルーチン &lt;入門&gt;</h3><p class="sum">概要 &amp; 説明</p>`,
		"<summary>ほかの記事 2 件", `<li class="item low" data-id="2"`, "…他 1 件は省略</small>",
		"<h3>D（新着 1 件・主要 0 件）</h3>", `data-id="d"`,
		"新着なし: C", `<b class="prune">取得失敗:</b> B: HTTP 404`, "生成: 2026-08-15 / braindex news fetch", "2 以上を主要表示",
		`const META={date:"2026-08-15",layer:"daily"};`, `type:"braindex-news-selection"`,
		// 保存ダイアログ(File System Access API)と、非対応ブラウザ向けのダウンロードの両方が入っている
		`window.showSaveFilePicker`, `id:"braindex-news-inbox"`, `a.download=name`,
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

// フィードの見出し「主要 N 件」は、タイトルの合計と同じく上限(Cap)で切った後の件数(実際に表示した件数)に揃える
// (決定 2026-09-12「主要 N 件は上限で切った後の数に揃える」→ manual/news.md「決めたこと」)。
// タイトルの合計は totalMain(shown の件数の和)から出すのに、見出しは切る前の len(main) を出すと数が食い違う。
func TestRenderHTML_主要件数は上限後の件数に揃う(t *testing.T) {
	p := interest.Profile{Terms: []interest.Term{{Word: "ゴルーチン", Weight: 2}}}
	res := []Result{{Source: Source{Name: "A", Category: "tech"}, New: []feed.Entry{
		{ID: "1", Title: "ゴルーチン 1", Link: "https://x/1"},
		{ID: "2", Title: "ゴルーチン 2", Link: "https://x/2"},
		{ID: "3", Title: "ゴルーチン 3", Link: "https://x/3"},
		{ID: "4", Title: "ゴルーチン 4", Link: "https://x/4"},
	}}}
	o := DigestOptions{Layer: "daily", Today: "2026-08-15", Cap: 2, Ranking: Rank(res, p, nil), MinScore: 2}
	h := string(RenderHTML(res, o))
	if !strings.Contains(h, "<title>ニュースダイジェスト 2026-08-15（daily 層・新着 4 件・主要 2 件）</title>") {
		t.Errorf("タイトルの主要件数が上限後(2 件)になっていない:\n%s", h)
	}
	if !strings.Contains(h, "<h3>A（新着 4 件・主要 2 件）</h3>") {
		t.Errorf("見出しの主要件数がタイトルと揃っていない(上限後の 2 件になっていない):\n%s", h)
	}
	if strings.Contains(h, "主要 4 件") {
		t.Errorf("見出しが上限前の件数(4 件)のまま:\n%s", h)
	}
}

// フィードのリンクは http(s) だけを載せる。javascript: などは href にも data-link にも出さず、題名だけ出す
// (決定 2026-09-03 → manual/design.md「決めたこと」)。data-link にも出さないのは、選別 JSON 経由で keep に入るのを止めるため。
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
	if !strings.Contains(h, `<h3 class="article-title">危ない</h3>`) {
		t.Error("題名がそのまま(リンクなしで)出ていない")
	}
	if !strings.Contains(h, `<a href="https://x/2" target="_blank" rel="noopener">原文を開く ↗</a>`) {
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

// 拾い上げた記事は独立した枠に出てラベルが付き、「ほかの記事」には出ない(同じ記事が 2 か所に出ない)。
func TestRenderHTML_Serendipity(t *testing.T) {
	results := []Result{{Source: Source{Name: "A", Category: "tech"}, New: []feed.Entry{
		{ID: "hi", Title: "主要の記事", Link: "https://example.com/1"},
		{ID: "lo", Title: "拾い上げた記事", Link: "https://example.com/2"},
		{ID: "lo2", Title: "ほかの記事", Link: "https://example.com/3"},
	}}}
	rk := Ranking{"hi": interest.Score{Value: 3}, "lo": interest.Score{Value: 1}, "lo2": interest.Score{Value: 0}}
	h := string(RenderHTML(results, DigestOptions{Today: "2026-09-07", Layer: "daily", Cap: 10, Ranking: rk, MinScore: 2,
		Serendipity: map[string]bool{"lo": true}}))
	for _, want := range []string{`class="category serendipity"`, `<h2>` + SerendipityLabel + `</h2>`,
		`<span class="tag lucky">` + SerendipityLabel + `</span>`, `関心の外と判定した記事から`} {
		if !strings.Contains(h, want) {
			t.Errorf("%q が無い", want)
		}
	}
	if n := strings.Count(h, `data-id="lo"`); n != 1 {
		t.Errorf(`data-id="lo" が %d 個(1 個であるべき: 2 か所に出ると選別の状態が壊れる)`, n)
	}
	// 拾い上げた記事は関心外の側から数えるので、keep したら rescued として集計される
	if !strings.Contains(h, `data-id="lo" data-title="拾い上げた記事" data-link="https://example.com/2" data-feed="A" data-cat="tech" data-low="1"`) {
		t.Error("拾い上げた記事の data-low が 1 でない")
	}
	// ほかの記事の折りたたみは残る(lo2 だけ)
	if !strings.Contains(h, "ほかの記事 1 件") {
		t.Errorf("折りたたみの件数が違う:\n%s", h)
	}
}

func TestMetadataSummaryDisplayAndPrompt(t *testing.T) {
	for _, raw := range []string{"", "Article URL: https://example.com/a\nPoints: 12", "Actual explanation."} {
		results := []Result{{Source: Source{Name: "Example", Lang: "en"}, New: []feed.Entry{{ID: "a", Title: "Headline", Summary: feed.CleanSummary(raw, feed.SummaryLimit)}}}}
		f := &fakeAnnotator{reply: func(p string) (string, error) {
			if strings.HasPrefix(raw, "Article") && strings.Contains(p, "Article URL:") {
				t.Fatal("metadata in prompt")
			}
			if raw == "Actual explanation." && !strings.Contains(p, raw) {
				t.Fatal("missing prose")
			}
			return `[{"id":"a","t":"訳","s":"","r":2}]`, nil
		}}
		Annotate(context.Background(), f, results, Annotations{}, AnnotateOptions{})
		opts := DigestOptions{Annotations: Annotations{"a": {Title: "訳", Summary: "古いメタデータの訳"}}}
		rendered := string(RenderHTML(results, opts))
		if raw == "" || strings.HasPrefix(raw, "Article") {
			if !strings.Contains(rendered, `<p class="sum">概要がありません。原文を開くか、解説を相談できます。</p>`) {
				t.Errorf("missing empty-summary guidance for %q", raw)
			}
			for _, output := range []string{rendered, string(Digest(results, opts))} {
				if strings.Contains(output, "Article URL:") || strings.Contains(output, "古いメタデータの訳") {
					t.Fatal("metadata rendered")
				}
			}
		} else if !strings.Contains(rendered, raw) {
			t.Fatal("missing prose")
		}
	}
}
func TestRenderHTMLSuggestions(t *testing.T) {
	o := DigestOptions{Suggestions: []Suggestion{{CatalogEntry: CatalogEntry{Name: `A"<&`, URL: `https://example.com/rss?a="<&`, Genre: `G<&`}, Matched: []string{`word<&`}}}, QueryCandidates: []string{`語"<&`}}
	h := string(RenderHTML(nil, o))
	for _, want := range []string{`<section class="category suggest"><details class="suggest">`, "取材先の候補 1 件・検索語の候補 1 件", `data-url="` + esc(o.Suggestions[0].URL) + `"`, `data-name="` + esc(o.Suggestions[0].Name) + `"`, `data-query="` + esc(o.QueryCandidates[0]) + `"`, "この語だけを Google News に送ります"} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(h, `<details class="suggest" open`) {
		t.Fatal("suggestions opened")
	}
	if h != string(RenderHTML(nil, o)) {
		t.Fatal("HTML not deterministic")
	}
	o.Library = true
	for _, opts := range []DigestOptions{o, {}} {
		if strings.Contains(string(RenderHTML(nil, opts)), `<section class="category suggest">`) {
			t.Fatal("unexpected suggestions")
		}
	}
}
