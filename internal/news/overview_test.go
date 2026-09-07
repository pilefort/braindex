package news

import (
	"strings"
	"testing"
)

const overviewMD = `# ニュース概要 2026-09-07

選んだ 2 件の概要です。

<!--braindex-article id=abc123 question=q-1 link=https://example.com/1-->
## 記事ひとつめ

前提から: これは **試験用** の概要です。

<!--braindex-article id=def456 question=q-2 link=javascript:alert(1) title=題名を明示-->
## 見出しは無視される

こちらは別の記事。
`

// 区切りで記事に分け、属性を読む。見出しから題名を拾い、危ないリンクは落とす。
func TestParseOverview(t *testing.T) {
	intro, arts := ParseOverview(overviewMD)
	if !strings.Contains(intro, "選んだ 2 件の概要です。") || strings.Contains(intro, "記事ひとつめ") {
		t.Errorf("前書き:\n%s", intro)
	}
	if len(arts) != 2 {
		t.Fatalf("記事 %d 件 want 2", len(arts))
	}
	a := arts[0]
	if a.ID != "abc123" || a.Question != "q-1" || a.Link != "https://example.com/1" || a.Title != "記事ひとつめ" {
		t.Errorf("1 件目: %+v", a)
	}
	if !strings.Contains(a.Body, "前提から") || strings.Contains(a.Body, "見出しは無視される") {
		t.Errorf("1 件目の本文:\n%s", a.Body)
	}
	b := arts[1]
	if b.Title != "題名を明示" {
		t.Errorf("title を明示したのに見出しを使っている: %q", b.Title)
	}
	if b.Link != "" {
		t.Errorf("http(s) でないリンクを落としていない: %q", b.Link)
	}
	// 区切りが無ければ記事なし(黙って 1 件にまとめない)
	if intro, arts := ParseOverview("# ただの文書\n\n本文"); len(arts) != 0 || !strings.Contains(intro, "本文") {
		t.Errorf("区切り無し: %d 件 / %q", len(arts), intro)
	}
}

// 画面には記事ごとの選択肢と、詳しく知りたい分を頼むボタンが出る。
func TestRenderOverview(t *testing.T) {
	intro, arts := ParseOverview(overviewMD)
	h := string(RenderOverview("k1", "概要の試験", intro, arts))
	for _, want := range []string{
		`<title>概要の試験</title>`,
		`<li class="art" data-id="abc123" data-q="q-1" data-link="https://example.com/1" data-title="記事ひとつめ">`,
		`<input type="radio" name="s-abc123" value="deep"><span>詳しく知りたい</span>`,
		`value="done"><span>概要で足りた`,
		`value="none"><span>興味なし`,
		`id="ask"`, `id="progress"`, `const META={id:"k1"}`,
		"<strong>試験用</strong>", // 本文が Markdown から変換されている
	} {
		if !strings.Contains(h, want) {
			t.Errorf("%q が無い", want)
		}
	}
	// 危ないリンクは data 属性にも出さない
	if strings.Contains(h, "javascript:") {
		t.Error("javascript: のリンクが残っている")
	}
	// 自己完結(外部読み込み無し)
	if strings.Contains(h, "<link ") || strings.Contains(h, "src=\"http") {
		t.Error("外部を読み込んでいる")
	}
}
