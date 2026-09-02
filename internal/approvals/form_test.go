package approvals

import (
	"bytes"
	"strings"
	"testing"
)

func sampleMeta() Meta {
	return Meta{Project: "example-hub", Path: "work/APPROVALS.md", Nonce: "fixed-nonce", GeneratedAt: "2026-01-02 03:04"}
}

func TestRenderForm_Deterministic(t *testing.T) {
	d := Parse(load(t, "two-items.md"))
	a := RenderForm(d, sampleMeta())
	b := RenderForm(d, sampleMeta())
	if !bytes.Equal(a, b) {
		t.Fatal("同一入力で出力が違う")
	}
}

func TestRenderForm_Elements(t *testing.T) {
	d := Parse(load(t, "two-items.md"))
	h := string(RenderForm(d, sampleMeta()))
	for _, want := range []string{
		"<!doctype html>", `<html lang="ja">`, "<title>承認待ち — example-hub</title>",
		`<section class="ap" data-n="1"`, `<section class="ap" data-n="2"`,
		`name="c1" value="A"`, `name="c1" value="B"`, `name="c1" value="other"`, `name="c1" value="hold"`,
		`name="c2" value="C"`,
		`<span class="badge">私の案</span>`, "依存を増やさない方針",
		"標準ライブラリだけで読める／コメントが書けない",
		`<code>app.json</code>`,
		`data-title="設定ファイルの形式を JSON にするか TOML にするか"`,
		`"nonce":"fixed-nonce"`, `fetch('/reply'`,
		`<button id="send">決定を送信</button>`,
		"CI の仕様が決まるまで",
		"work/APPROVALS.md ／ 生成 2026-01-02 03:04",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("HTML に %q が無い", want)
		}
	}
	if strings.Contains(h, "<script src=") || strings.Contains(h, "<link ") || strings.Contains(h, "http://") || strings.Contains(h, "https://") {
		t.Error("外部読み込みがある")
	}
	if n := strings.Count(h, `class="badge"`); n != 1 {
		t.Errorf("私の案バッジは項目 1 の A にだけ付く: %d 個", n)
	}
}

func TestRenderForm_WarningsAndEscape(t *testing.T) {
	md := "# 承認待ち\n\n## <script>alert(1)</script> & 題\n\n**決めたいこと:** a < b\n**選択肢:**\n- A. x\n**私の案:** 案なし\n"
	h := string(RenderForm(Parse([]byte(md)), sampleMeta()))
	if strings.Contains(h, "<script>alert") {
		t.Error("見出しがエスケープされていない")
	}
	for _, want := range []string{"&lt;script&gt;alert(1)&lt;/script&gt; &amp; 題", "a &lt; b",
		"記載が足りません", "なぜ今決めるか", "（未記載）"} {
		if !strings.Contains(h, want) {
			t.Errorf("HTML に %q が無い", want)
		}
	}
}

func TestRenderForm_Empty(t *testing.T) {
	h := string(RenderForm(Parse(load(t, "empty.md")), sampleMeta()))
	if !strings.Contains(h, "承認待ちはありません") || strings.Contains(h, `id="send"`) {
		t.Errorf("空のとき: %s", h)
	}
}

func TestInline(t *testing.T) {
	for in, want := range map[string]string{
		"a `x<y` b":                 "a <code>x&lt;y</code> b",
		"see https://example.com/p": `see <a href="https://example.com/p">https://example.com/p</a>`,
		"1 行\n2 行":                  "1 行<br>2 行",
		"**強**":                     "<b>強</b>",
	} {
		if got := inline(in); got != want {
			t.Errorf("inline(%q) = %q, want %q", in, got, want)
		}
	}
}
