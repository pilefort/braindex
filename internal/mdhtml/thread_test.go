package mdhtml

import (
	"strings"
	"testing"
)

const threadSample = "# 索引の設計\n" +
	"\n" +
	`<!--braindex:entry at="2026-09-06T03:20:00+09:00" q="走査の順番は決まってる？"-->` + "\n" +
	"\n" +
	"名前順です。\n" +
	"\n" +
	"## 内訳\n" +
	"\n" +
	"- 置き場ごとに名前順\n" +
	"\n" +
	`<!--braindex:entry at="2026-09-05T20:00:00+09:00" q="索引って何を見てる？"-->` + "\n" +
	"\n" +
	"ノートの見出しです。\n"

func TestParseThread(t *testing.T) {
	title, es := ParseThread(threadSample)
	if title != "索引の設計" {
		t.Fatalf("タイトル: got %q", title)
	}
	if len(es) != 2 {
		t.Fatalf("エントリ数: got %d want 2", len(es))
	}
	if es[0].At != "2026-09-06T03:20:00+09:00" || es[0].Q != "走査の順番は決まってる？" {
		t.Fatalf("1 件目の見出し: got %+v", es[0])
	}
	// 回答本文の `## 見出し` はエントリの境界にならない(マーカーだけが境界)。
	if !strings.Contains(es[0].Body, "## 内訳") || !strings.HasPrefix(es[0].Body, "名前順です。") {
		t.Fatalf("1 件目の本文: got %q", es[0].Body)
	}
	if es[1].Body != "ノートの見出しです。" {
		t.Fatalf("2 件目の本文: got %q", es[1].Body)
	}
}

// マーカーの無い 1 枚ものに追記できるよう、本文全体を日時不明の 1 エントリとして読む。
func TestParseThread_マーカーなし(t *testing.T) {
	title, es := ParseThread("# 題名\n\n本文です。\n")
	if title != "題名" {
		t.Fatalf("タイトル: got %q", title)
	}
	if len(es) != 1 || es[0].At != "" || es[0].Body != "本文です。" {
		t.Fatalf("エントリ: got %+v", es)
	}
}

func TestParseThread_空(t *testing.T) {
	title, es := ParseThread("")
	if title != "" || len(es) != 0 {
		t.Fatalf("got %q %+v", title, es)
	}
}

// Render → Parse で元に戻る(質問の引用符・山括弧・改行を含む)。
func TestRenderThread_往復(t *testing.T) {
	in := []Entry{
		{At: "2026-09-06T03:20:00+09:00", Q: `"引用" <と> &`, Body: "回答 1\n\n## 節\n\nつづき"},
		{At: "", Q: "", Body: "回答 2"},
	}
	md := RenderThread("題名", in)
	title, out := ParseThread(md)
	if title != "題名" {
		t.Fatalf("タイトル: got %q", title)
	}
	if len(out) != len(in) {
		t.Fatalf("エントリ数: got %d want %d\n%s", len(out), len(in), md)
	}
	for i := range in {
		if out[i] != in[i] {
			t.Fatalf("%d 件目: got %+v want %+v", i, out[i], in[i])
		}
	}
}

// 質問の改行は 1 行に潰す(マーカーは 1 行なので)。
func TestRenderThread_質問の改行(t *testing.T) {
	md := RenderThread("題名", []Entry{{At: "x", Q: "1 行目\n2 行目", Body: "本文"}})
	if strings.Count(md, "<!--braindex:entry") != 1 {
		t.Fatalf("マーカーが 1 つでない:\n%s", md)
	}
	_, es := ParseThread(md)
	if len(es) != 1 || es[0].Q != "1 行目 2 行目" {
		t.Fatalf("got %+v", es)
	}
}

func TestThreadPage_Golden(t *testing.T) {
	checkGolden(t, "thread.page.html", ThreadPage(threadSample, "索引の設計"))
}

func TestThreadPage_Deterministic(t *testing.T) {
	a := ThreadPage(threadSample, "索引の設計")
	b := ThreadPage(threadSample, "索引の設計")
	if a != b {
		t.Fatal("同じ入力で出力が違う")
	}
}

// 見出しの畳み込みと既読の記録に使う id は、同じ日時が並んでも重複しない。
func TestThreadPage_id重複(t *testing.T) {
	md := RenderThread("題名", []Entry{
		{At: "2026-09-06T03:20:00+09:00", Body: "A"},
		{At: "2026-09-06T03:20:00+09:00", Body: "B"},
	})
	h := ThreadPage(md, "題名")
	if strings.Count(h, `<details class="ent"`) != 2 {
		t.Fatalf("エントリが 2 つでない:\n%s", h)
	}
	if strings.Count(h, `id="e-20260906T032000"`) != 1 || strings.Count(h, `id="e-20260906T032000-2"`) != 1 {
		t.Fatalf("同じ日時のエントリで id が分かれていない:\n%s", h)
	}
}

// 日時が読めないときは素のまま出す(落ちない)。
func TestThreadPage_日時不明(t *testing.T) {
	h := ThreadPage(RenderThread("題名", []Entry{{Body: "A"}}), "題名")
	if !strings.Contains(h, "日時不明") {
		t.Fatalf("日時不明の表示が無い:\n%s", h)
	}
}

func TestIsThread(t *testing.T) {
	if !IsThread(threadSample) {
		t.Error("スレッドを 1 枚ものと判定した")
	}
	if IsThread("# 題名\n\n本文です。\n") {
		t.Error("1 枚ものをスレッドと判定した")
	}
	// マーカーに見えるだけの行(コードブロックの中の説明など)は属性が無いので数えない。
	if IsThread("```\n<!--braindex:entry-->\n```\n") {
		t.Error("属性の無い行をマーカーと判定した")
	}
}

func TestThreadPage_外部読み込みなし(t *testing.T) {
	h := ThreadPage(threadSample, "索引の設計")
	for _, bad := range []string{"<link", "src=", "@import", "http://", "https://cdn"} {
		if strings.Contains(h, bad) {
			t.Fatalf("外部読み込みらしき記述がある: %s", bad)
		}
	}
}
