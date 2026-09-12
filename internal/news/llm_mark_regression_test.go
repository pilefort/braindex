package news

import (
	"reflect"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/feed"
)

func TestLLMScored_関心語LLMは未採点(t *testing.T) {
	rs := []Result{{Source: Source{Name: "A"}, New: []feed.Entry{{ID: "a", Title: "LLM model"}}}}
	rk := Ranking{"a": {Value: 3, Matched: []string{"LLM", "model"}}}
	rk = ApplyAnnotations(rk, rs, Annotations{"a": {Title: "訳のみ"}})
	if got := LLMScored(rs, rk); got != 0 {
		t.Errorf("LLMScored = %d, want 0", got)
	}
	o := DigestOptions{Ranking: rk, MinScore: 2}
	h := string(RenderHTML(rs, o))
	if strings.Contains(h, "LLM 採点") || !strings.Contains(h, "関心に合った語: LLM・model") {
		t.Error("関心語 LLM を LLM 採点と誤認している")
	}
	if md := string(Digest(rs, o)); !strings.Contains(md, "★3（LLM・model）") {
		t.Errorf("一致語の表示が変わった: %s", md)
	}
}

func TestApplyAnnotations_関心語LLMを保持してコピー(t *testing.T) {
	rs := []Result{{Source: Source{Name: "A"}, New: []feed.Entry{{ID: "a", Title: "LLM model"}}}}
	want := []string{"LLM", "model", "LLM"}
	rk := Ranking{"a": {Value: 2, Matched: append([]string(nil), want...)}}
	ann := Annotations{"a": {Score: intp(3)}}
	got := ApplyAnnotations(rk, rs, ann)
	if !reflect.DeepEqual(got["a"].Matched, want) {
		t.Errorf("Matched = %v, want %v", got["a"].Matched, want)
	}
	if LLMScored(rs, got) != 1 {
		t.Error("LLM 採点を数えていない")
	}
	if twice := ApplyAnnotations(got, rs, ann); !reflect.DeepEqual(twice, got) {
		t.Errorf("再適用で変わった: %v", twice)
	}
	o := DigestOptions{Ranking: got, MinScore: 2}
	if h := string(RenderHTML(rs, o)); !strings.Contains(h, "LLM 採点・関心に合った語: LLM・model・LLM") {
		t.Error("HTML で一致語 LLM が消えた")
	}
	if md := string(Digest(rs, o)); !strings.Contains(md, "★3（LLM・LLM・model・LLM）") {
		t.Errorf("Markdown で一致語 LLM が消えた: %s", md)
	}
	got["a"].Matched[0] = "changed"
	if !reflect.DeepEqual(rk["a"].Matched, want) {
		t.Error("入力の一致語とスライスを共有している")
	}
}
