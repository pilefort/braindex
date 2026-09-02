package approvals

import (
	"bytes"
	"regexp"
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

// 見出しの番号は重なりうる(番号を振り忘れた見出しは出現順で補うので、「## 3.」「##（番号なし）」が並ぶと 3 が 2 つになる)。
// ラジオの name を項目ごとに分けないと 2 つの項目が同じグループになり、後の項目を選んだ瞬間に前の選択が外れて回答が落ちる。
func TestRenderForm_DuplicateHeadingNumbers(t *testing.T) {
	md := "# 承認待ち\n\n## 3. あ\n\n**選択肢:**\n- A. x\n- B. y\n\n## い\n\n**選択肢:**\n- A. p\n- B. q\n\n## う\n\n**選択肢:**\n- A. m\n- B. n\n"
	d := Parse([]byte(md))
	if len(d.Items) != 3 || d.Items[0].N != d.Items[2].N {
		t.Fatalf("前提が崩れた: 番号 %d / %d / %d（1 番目と 3 番目が重なる入力のはず）",
			d.Items[0].N, d.Items[1].N, d.Items[2].N)
	}
	h := string(RenderForm(d, sampleMeta()))
	names := map[string]bool{}
	for _, m := range regexp.MustCompile(`name="(c\d+)"`).FindAllStringSubmatch(h, -1) {
		names[m[1]] = true
	}
	if len(names) != 3 {
		t.Errorf("ラジオのグループ = %v, want 3 個（項目ごとに別）", names)
	}
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`\sid="([^"]+)"`).FindAllStringSubmatch(h, -1) {
		if seen[m[1]] {
			t.Errorf("id %q が重複している", m[1])
		}
		seen[m[1]] = true
	}
	// 項目の番号自体は表示と POST(data-n)に残す。apply は題名で照合し、題名が空のときだけ番号を見る
	for _, want := range []string{`data-n="3"`, `data-n="2"`, `<span class="num">3</span>`} {
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
