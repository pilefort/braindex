package news

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pilefort/braindex/internal/feed"
	"github.com/pilefort/braindex/internal/interest"
)

func intp(n int) *int { return &n }

// 応答の解析: コードフェンスや前置きが混ざっても JSON 配列だけを拾う。範囲外・非数の関心度は未採点(nil)。
func TestParseAnnotationResponse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Annotations
	}{
		{"素の配列", `[{"id":"a","t":"訳","s":"概要","r":2}]`, Annotations{"a": {Title: "訳", Summary: "概要", Score: intp(2)}}},
		{"コードフェンス", "```json\n[{\"id\":\"a\",\"t\":\"\",\"s\":\"\",\"r\":0}]\n```", Annotations{"a": {Score: intp(0)}}},
		{"前置きつき", "結果です:\n[{\"id\":\"a\",\"r\":\"3\"}] 以上", Annotations{"a": {Score: intp(3)}}},
		{"範囲外は未採点", `[{"id":"a","r":7},{"id":"b","r":"x"},{"id":"c"}]`, Annotations{"a": {}, "b": {}, "c": {}}},
		{"id 無しは捨てる", `[{"t":"x","r":1}]`, Annotations{}},
		{"空", "", Annotations{}},
		{"壊れた JSON", `[{"id":`, Annotations{}},
		{"配列でない", `{"id":"a","r":1}`, Annotations{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseAnnotationResponse(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %+v want %+v", got, c.want)
			}
		})
	}
}

// プロンプトに載るのは 見出し・概要・言語・プロファイルの語・keep の見出し だけ。JSON 配列だけを返せと書く。
func TestBuildAnnotationPrompt(t *testing.T) {
	batch := []annotationItem{{ID: "x1", Lang: "en", Title: "Go 1.99 released", Summary: "Generics improved"}}
	p := BuildAnnotationPrompt(batch, []string{"go", "generics"}, []string{"残した見出し"})
	for _, want := range []string{`"id":"x1"`, "Go 1.99 released", "Generics improved", "go・generics", "- 残した見出し", "JSON 配列だけ", "0〜3"} {
		if !strings.Contains(p, want) {
			t.Errorf("プロンプトに %q が無い:\n%s", want, p)
		}
	}
	p2 := BuildAnnotationPrompt(batch, nil, nil)
	if !strings.Contains(p2, "(未設定)") || !strings.Contains(p2, "(まだ無し)") {
		t.Errorf("語も例も無いときの表記が違う:\n%s", p2)
	}
}

// 訳が落ちた英語の記事は、半分のまとまりでもう一度だけ聞く。2 度目も空なら印を付け、次回からは聞かない。
func TestAnnotateRetriesMissingTranslation(t *testing.T) {
	f := &fakeAnnotator{reply: func(p string) (string, error) {
		// 聞き直しは 1 件ずつ来る。a1 はそのとき訳を返し、a2 は最後まで返さない。
		if strings.Contains(p, `"id":"a1"`) && !strings.Contains(p, `"id":"a2"`) {
			return `[{"id":"a1","t":"見出し一","s":"要約","r":3}]`, nil
		}
		return `[{"id":"a1","t":"","s":"","r":3},{"id":"a2","t":"","s":"","r":1},{"id":"b1","t":"","s":"","r":1}]`, nil
	}}
	cache := Annotations{}
	rep := Annotate(context.Background(), f, llmResults(), cache, AnnotateOptions{Pool: 2, Batch: 2})
	if len(f.prompts) != 4 {
		t.Fatalf("呼び出し %d 回 want 4(最初の 2 まとまり＋聞き直し 2 件)", len(f.prompts))
	}
	if rep.Requested != 3 || rep.Retried != 2 || rep.Annotated != 3 {
		t.Errorf("report %+v want Requested=3 Retried=2 Annotated=3", rep)
	}
	want := Annotations{
		"a1": {Title: "見出し一", Summary: "要約", Score: intp(3)},
		"a2": {Score: intp(1), NoTitle: true},
		"b1": {Score: intp(1)},
	}
	if !reflect.DeepEqual(cache, want) {
		t.Errorf("cache %+v want %+v", cache, want)
	}
	// 印の付いた記事は次からは聞かない(毎回聞き直すと費用だけ増える)。
	f.prompts = nil
	Annotate(context.Background(), f, llmResults(), cache, AnnotateOptions{Pool: 2, Batch: 2})
	if len(f.prompts) != 0 {
		t.Errorf("2 回目に %d 回聞いた want 0", len(f.prompts))
	}
}

// fakeAnnotator は CLI の代わり。受けたプロンプトを記録し、決めた応答を返す。
type fakeAnnotator struct {
	prompts []string
	reply   func(prompt string) (string, error)
}

func (f *fakeAnnotator) Annotate(_ context.Context, prompt string) (string, error) {
	f.prompts = append(f.prompts, prompt)
	return f.reply(prompt)
}

func llmResults() []Result {
	return []Result{
		{Source: Source{Name: "A", Lang: "en"}, New: []feed.Entry{
			{ID: "a1", Title: "Title one", Summary: "sum"},
			{ID: "a2", Title: "Title two"},
			{ID: "a3", Title: "Title three"},
		}},
		{Source: Source{Name: "B", Lang: "ja"}, New: []feed.Entry{{ID: "b1", Title: "見出し"}}},
		{Source: Source{Name: "C"}, Err: errors.New("失敗")},
	}
}

// 採点済みのキャッシュは呼ばない。プール(1 フィード pool 件)の外は採点しない。応答は cache に合流し、旧訳は残す。
func TestAnnotate(t *testing.T) {
	f := &fakeAnnotator{reply: func(p string) (string, error) {
		return `[{"id":"a1","t":"見出し一","s":"要約","r":3},{"id":"b1","t":"","s":"","r":1}]`, nil
	}}
	cache := Annotations{"a2": {Title: "旧訳", Score: intp(2)}}
	rep := Annotate(context.Background(), f, llmResults(), cache, AnnotateOptions{Pool: 2, Batch: 10})
	if len(f.prompts) != 1 {
		t.Fatalf("呼び出し %d 回 want 1", len(f.prompts))
	}
	if strings.Contains(f.prompts[0], "a2") || strings.Contains(f.prompts[0], "a3") {
		t.Errorf("採点済み(a2)・プール外(a3)がプロンプトに載っている:\n%s", f.prompts[0])
	}
	if !strings.Contains(f.prompts[0], `"lang":"ja"`) || !strings.Contains(f.prompts[0], `"lang":"en"`) {
		t.Errorf("言語が載っていない:\n%s", f.prompts[0])
	}
	if rep.Requested != 2 || rep.Annotated != 2 || rep.Failed != 0 {
		t.Errorf("report %+v", rep)
	}
	want := Annotations{
		"a1": {Title: "見出し一", Summary: "要約", Score: intp(3)},
		"a2": {Title: "旧訳", Score: intp(2)},
		"b1": {Score: intp(1)},
	}
	if !reflect.DeepEqual(cache, want) {
		t.Errorf("cache %+v\nwant %+v", cache, want)
	}
}

// 応答に無い id は未採点のまま。バッチの失敗は数えて続ける(フォールバック)。翻訳だけの旧キャッシュは採点し直し、訳は捨てない。
func TestAnnotate_失敗と旧訳(t *testing.T) {
	calls := 0
	f := &fakeAnnotator{reply: func(p string) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("timeout")
		}
		return `[{"id":"a3","t":"","s":"","r":2}]`, nil
	}}
	cache := Annotations{"a3": {Title: "旧訳だけ"}} // Score 無し → 採点し直す
	rep := Annotate(context.Background(), f, llmResults(), cache, AnnotateOptions{Pool: 10, Batch: 2})
	if calls != 2 {
		t.Fatalf("バッチ %d 回 want 2(4 件を 2 件ずつ)", calls)
	}
	if rep.Requested != 4 || rep.Failed != 1 || rep.Annotated != 1 {
		t.Errorf("report %+v", rep)
	}
	if got := cache["a3"]; got.Title != "旧訳だけ" || got.Score == nil || *got.Score != 2 {
		t.Errorf("a3 = %+v(旧訳を残して採点だけ更新したい)", got)
	}
	if _, ok := cache["a1"]; ok {
		t.Errorf("失敗したバッチの a1 が cache に入っている")
	}
}

// 語の一致の Ranking に LLM の関心度を重ねる。LLM 未採点は語の点を保つ。
// Ranking が nil(プロファイル空)なら LLM 採点だけの Ranking を作り、未採点は載せない(Ranking に無い＝未採点＝主要表示・バッジ無し)。
func TestApplyAnnotations(t *testing.T) {
	rs := llmResults()
	ann := Annotations{"a1": {Score: intp(3)}, "a2": {Title: "訳のみ"}, "b1": {Score: intp(0)}}
	rk := Ranking{"a1": {Value: 1, Matched: []string{"x"}}, "a2": {Value: 2, Matched: []string{"y"}}, "a3": {Value: 0}, "b1": {Value: 2}}
	got := ApplyAnnotations(rk, rs, ann)
	if LLMScored(rs, got) != 2 {
		t.Errorf("scored=%d", LLMScored(rs, got))
	}
	if !reflect.DeepEqual(rk["a1"].Matched, []string{"x"}) {
		t.Error("input ranking changed")
	}
	if twice := ApplyAnnotations(got, rs, ann); !reflect.DeepEqual(twice, got) {
		t.Errorf("repeat changed ranking: %v", twice)
	}
	o := DigestOptions{Cap: 10, Ranking: got}
	if md := string(Digest(rs, o)); !strings.Contains(md, "LLM・x") {
		t.Errorf("missing evidence: %s", md)
	}
	if h := string(RenderHTML(rs, o)); !strings.Contains(h, "LLM") || !strings.Contains(h, "関心に合った語: x") {
		t.Error("HTML missing score provenance or words")
	}
	if got["a1"].Value != 3 || !got["a1"].LLM || !reflect.DeepEqual(got["a1"].Matched, []string{"x"}) {
		t.Errorf("a1 = %+v", got["a1"])
	}
	if got["a2"].Value != 2 || got["a2"].Matched[0] != "y" {
		t.Errorf("a2(LLM 未採点)は語の点を保つ: %+v", got["a2"])
	}
	if got["b1"].Value != 0 {
		t.Errorf("b1 = %+v", got["b1"])
	}
	got2 := ApplyAnnotations(nil, rs, ann)
	_, hasA2 := got2["a2"]
	_, hasA3 := got2["a3"]
	if got2 == nil || got2["a1"].Value != 3 || hasA2 || hasA3 || got2["b1"].Value != 0 {
		t.Errorf("nil Ranking からの生成: %+v", got2)
	}
	if ApplyAnnotations(nil, rs, Annotations{}) != nil {
		t.Errorf("注釈が空なら nil のまま")
	}
	_ = interest.MaxScore
}

// キャッシュの読み書き。無ければ空。壊れていればエラー。
func TestAnnotations_LoadSave(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, LLMCacheFile)
	a, err := LoadAnnotations(p)
	if err != nil || len(a) != 0 {
		t.Fatalf("無いとき: %v %v", a, err)
	}
	a["x"] = Annotation{Title: "t", Score: intp(1)}
	if err := a.Save(p); err != nil {
		t.Fatal(err)
	}
	b, err := LoadAnnotations(p)
	if err != nil || !reflect.DeepEqual(a, b) {
		t.Fatalf("読み戻し %v %v", b, err)
	}
	os.WriteFile(p, []byte("{壊れた"), 0o644)
	if _, err := LoadAnnotations(p); err == nil {
		t.Error("壊れたファイルでエラーにならない")
	}
}

// 表示: md は訳を添え、LLM の点は（LLM）と出る。
func TestDigest_翻訳とLLMの点(t *testing.T) {
	rs := []Result{{Source: Source{Name: "A", Lang: "en"}, New: []feed.Entry{{ID: "a1", Title: "Title one", Link: "https://example.com/1"}}}}
	ann := Annotations{"a1": {Title: "見出し一", Score: intp(3)}}
	rk := ApplyAnnotations(nil, rs, ann)
	o := DigestOptions{Layer: "daily", Today: "2026-08-15", Cap: 10, Ranking: rk, MinScore: 2, Annotations: ann}
	md := string(Digest(rs, o))
	if !strings.Contains(md, "[Title one](https://example.com/1) ★3（LLM）／訳: 見出し一") {
		t.Errorf("md:\n%s", md)
	}
	h := string(RenderHTML(rs, o))
	if !strings.Contains(h, `<h3 class="article-title">見出し一</h3>`) || !strings.Contains(h, "LLM") {
		t.Errorf("html に訳か LLM の印が無い")
	}
}

// レビュー #91-2: プロファイルが空で LLM 未採点の記事は Ranking に載せない(関心度を捏造しない)。Split は主要に入れ、描画はバッジ無し。
func TestApplyAnnotations_未採点は捏造しない(t *testing.T) {
	rs := llmResults()
	ann := Annotations{"a1": {Score: intp(3)}}
	rk := ApplyAnnotations(nil, rs, ann)
	if _, ok := rk["a2"]; ok {
		t.Errorf("未採点の a2 が Ranking に入っている: %+v", rk["a2"])
	}
	main, low := Split(rs[0].New, rk, 2)
	if len(main) != 3 || len(low) != 0 {
		t.Errorf("未採点は主要に入る: main=%d low=%d", len(main), len(low))
	}
	if main[0].ID != "a1" {
		t.Errorf("採点済み 3 が先頭: %v", main[0].ID)
	}
	o := DigestOptions{Layer: "d", Today: "2026-08-15", Cap: 10, Ranking: rk, MinScore: 2}
	md := string(Digest(rs[:1], o))
	if !strings.Contains(md, "[Title two]()\n") {
		t.Errorf("未採点に ★ が付いている:\n%s", md)
	}
	h := string(RenderHTML(rs[:1], o))
	if strings.Contains(h, `data-r="2"`) {
		t.Errorf("未採点の HTML に関心度 2 が出ている")
	}
}

// レビュー #91-4: 応答にバッチに無い id があっても cache に入れない・数えない。
func TestAnnotate_捏造idは捨てる(t *testing.T) {
	f := &fakeAnnotator{reply: func(string) (string, error) {
		return `[{"id":"a1","t":"訳","r":1},{"id":"zzz","t":"捏造","r":3}]`, nil
	}}
	cache := Annotations{}
	rep := Annotate(context.Background(), f, llmResults(), cache, AnnotateOptions{Pool: 10, Batch: 10})
	if _, ok := cache["zzz"]; ok {
		t.Errorf("バッチに無い id が cache に入った")
	}
	if rep.Annotated != 1 {
		t.Errorf("Annotated=%d want 1", rep.Annotated)
	}
}

// レビュー #91-5: 前置き・後置きに角括弧があっても配列を拾う。#91-7: 訳の改行・連続空白は 1 個の空白にする。
func TestParseAnnotationResponse_角括弧と空白(t *testing.T) {
	got := ParseAnnotationResponse("[注] 前置き\n[{\"id\":\"a\",\"t\":\"一行目\\n二行目  三\",\"r\":2}]\n後置き[終]")
	a, ok := got["a"]
	if !ok || a.Score == nil || *a.Score != 2 {
		t.Fatalf("配列を拾えない: %+v", got)
	}
	if a.Title != "一行目 二行目 三" {
		t.Errorf("Title=%q(空白を 1 個に正規化したい)", a.Title)
	}
}

// レビュー #91-6: 脚注の LLM の記述は、実際に LLM の点が適用された記事があるときだけ。無ければ「規則ベース」のまま。
func TestRenderHTML_脚注のLLM表記(t *testing.T) {
	rs := []Result{{Source: Source{Name: "A"}, New: []feed.Entry{{ID: "a1", Title: "T"}}}}
	rk := Ranking{"a1": {Value: 2, Matched: []string{"x"}}}
	o := DigestOptions{Layer: "d", Today: "2026-08-15", Cap: 10, Ranking: rk, MinScore: 2, Annotations: Annotations{"other": {Score: intp(3)}}}
	h := string(RenderHTML(rs, o))
	if strings.Contains(h, "LLM 補助") || !strings.Contains(h, "採点は規則ベース") {
		t.Errorf("適用 0 件なのに LLM の記述がある/規則ベースが消えた")
	}
	ann := Annotations{"a1": {Score: intp(3)}}
	o.Annotations = ann
	o.Ranking = ApplyAnnotations(rk, rs, ann)
	h = string(RenderHTML(rs, o))
	if !strings.Contains(h, "LLM 補助") || strings.Contains(h, "採点は規則ベース") {
		t.Errorf("適用ありなのに LLM の記述が無い/規則ベースが残っている")
	}
}

type budgetAnnotator struct{ calls int }

func (a *budgetAnnotator) Annotate(ctx context.Context, _ string) (string, error) {
	a.calls++
	<-ctx.Done()
	return "", ctx.Err()
}

func TestAnnotateBudgetStopsRemaining(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	a := &budgetAnnotator{}
	cache := Annotations{}
	results := []Result{{New: []feed.Entry{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}}}}
	rep := Annotate(ctx, a, results, cache, AnnotateOptions{Batch: 1})
	if a.calls != 1 || rep.Failed == 0 || !strings.Contains(strings.Join(rep.Errors, " "), "時間上限") {
		t.Fatalf("calls=%d report=%+v", a.calls, rep)
	}
	rk := Ranking{"a": interest.Score{Value: 2}, "b": interest.Score{Value: 1}}
	got := ApplyAnnotations(rk, results, cache)
	if got["a"].Value != 2 || got["b"].Value != 1 || got["b"].LLM {
		t.Fatal(got)
	}
}
