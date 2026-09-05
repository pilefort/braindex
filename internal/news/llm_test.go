package news

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
// Ranking が nil(プロファイル空)なら LLM 採点だけの Ranking を作り、未採点は minScore(主要表示・フォールバック)。
func TestApplyAnnotations(t *testing.T) {
	rs := llmResults()
	ann := Annotations{"a1": {Score: intp(3)}, "a2": {Title: "訳のみ"}, "b1": {Score: intp(0)}}
	rk := Ranking{"a1": {Value: 1, Matched: []string{"x"}}, "a2": {Value: 2, Matched: []string{"y"}}, "a3": {Value: 0}, "b1": {Value: 2}}
	got := ApplyAnnotations(rk, rs, ann, 2)
	if got["a1"].Value != 3 || !reflect.DeepEqual(got["a1"].Matched, []string{LLMMark}) {
		t.Errorf("a1 = %+v", got["a1"])
	}
	if got["a2"].Value != 2 || got["a2"].Matched[0] != "y" {
		t.Errorf("a2(LLM 未採点)は語の点を保つ: %+v", got["a2"])
	}
	if got["b1"].Value != 0 {
		t.Errorf("b1 = %+v", got["b1"])
	}
	got2 := ApplyAnnotations(nil, rs, ann, 2)
	if got2 == nil || got2["a1"].Value != 3 || got2["a2"].Value != 2 || got2["a3"].Value != 2 || got2["b1"].Value != 0 {
		t.Errorf("nil Ranking からの生成: %+v", got2)
	}
	if ApplyAnnotations(nil, rs, Annotations{}, 2) != nil {
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
	rk := ApplyAnnotations(nil, rs, ann, 2)
	o := DigestOptions{Layer: "daily", Today: "2026-08-15", Cap: 10, Ranking: rk, MinScore: 2, Annotations: ann}
	md := string(Digest(rs, o))
	if !strings.Contains(md, "[Title one](https://example.com/1) ★3（LLM）／訳: 見出し一") {
		t.Errorf("md:\n%s", md)
	}
	h := string(RenderHTML(rs, o))
	if !strings.Contains(h, "訳: 見出し一") || !strings.Contains(h, "LLM") {
		t.Errorf("html に訳か LLM の印が無い")
	}
}
