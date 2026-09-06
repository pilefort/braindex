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

// 「新着」の印を消すのは summary のクリックだけで、details の toggle では消さない。
// Chromium は初期表示の `<details open>` にも toggle を投げるので、toggle で記録すると
// 読み込んだ瞬間に全エントリが「操作済み」になり、印が一度も出なかった(2026-09-06 実測)。
// ブラウザを回せないので、埋め込む JS の形で歯止めをかける。
func TestThreadPage_新着は初期表示で消えない(t *testing.T) {
	h := ThreadPage(threadSample, "索引の設計")
	if !strings.Contains(h, `sm.addEventListener('click'`) {
		t.Error("開閉の記録が summary のクリックで行われていない")
	}
	if strings.Contains(h, `addEventListener('toggle'`) {
		t.Error("details の toggle で開閉を記録している(初期表示で「新着」が消える)")
	}
	if !strings.Contains(h, `<span class="ent-n">新着</span>`) {
		t.Error("「新着」の印が HTML に無い")
	}
}

// 回答の中のコードブロックにマーカーの例を書いても、エントリが割れない(codex 指摘 2026-09-06)。
func TestParseThread_コードブロックの中のマーカーは境界にしない(t *testing.T) {
	md := "# 題名\n" +
		"\n<!--braindex:entry at=\"2026-09-06T10:00:00+09:00\" q=\"書き方は？\"-->\n\n" +
		"境界はこう書きます。\n\n```\n<!--braindex:entry at=\"2026-01-01T00:00:00+09:00\" q=\"例\"-->\n```\n\n後書き。\n"
	title, entries := ParseThread(md)
	if title != "題名" {
		t.Fatalf("題名が違う: %q", title)
	}
	if len(entries) != 1 {
		t.Fatalf("エントリ数が %d(期待 1)。コードブロックの中で割れている", len(entries))
	}
	if !strings.Contains(entries[0].Body, "後書き。") {
		t.Error("コードブロックより後の本文が落ちている")
	}
	// マーカーがコードブロックの中にしか無い .md は 1 枚もの。
	if IsThread("# 題名\n\n説明。\n\n```\n<!--braindex:entry at=\"2026-01-01T00:00:00+09:00\" q=\"例\"-->\n```\n") {
		t.Error("コードブロックの中のマーカーでスレッドと判定した")
	}
}

// 同じ秒に追記しても、既に描いてあるエントリの id が動かない(codex 指摘 2026-09-06)。
// id が動くと、開閉の記憶が別のエントリに移り、新しい回答が畳まれて出る。
func TestThreadPage_同じ秒に追記しても既存のidが動かない(t *testing.T) {
	at := "2026-09-06T10:00:00+09:00"
	before := RenderThread("題名", []Entry{{At: at, Q: "2 つ目", Body: "B"}, {At: at, Q: "1 つ目", Body: "A"}})
	after := Prepend(before, "題名", Entry{At: at, Q: "3 つ目", Body: "C"})
	ids := func(md string) []string {
		var out []string
		for _, ln := range strings.Split(ThreadPage(md, "題名"), "\n") {
			if i := strings.Index(ln, `<details class="ent" id="`); i >= 0 {
				s := ln[i+len(`<details class="ent" id="`):]
				out = append(out, s[:strings.IndexByte(s, '"')])
			}
		}
		return out
	}
	b, a := ids(before), ids(after)
	if len(b) != 2 || len(a) != 3 {
		t.Fatalf("エントリ数が違う: 前 %v / 後 %v", b, a)
	}
	if a[1] != b[0] || a[2] != b[1] {
		t.Errorf("追記で既存の id が動いた: 前 %v → 後 %v", b, a)
	}
	if a[0] == b[0] || a[0] == b[1] {
		t.Errorf("新しいエントリが既存の id を取った: %v", a)
	}
}

// チェックの消し込みは、エントリごとに別の鍵で覚える(codex 指摘 2026-09-06)。
// 同じ文言の項目を含む回答を足すと、古いチェックが新しい項目に移るため。
func TestThreadPage_チェックの鍵はエントリごと(t *testing.T) {
	h := ThreadPage(threadSample, "索引の設計")
	if !strings.Contains(h, `closest('details.ent')`) {
		t.Error("チェックの鍵にエントリの id が入っていない")
	}
}
