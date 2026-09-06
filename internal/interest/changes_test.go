package interest

import (
	"strings"
	"testing"
	"time"

	"github.com/pilefort/braindex/internal/changehistory"
	"github.com/pilefort/braindex/internal/render"
)

func changesInput(changes []changehistory.Entry) Input {
	since := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	return Input{
		Today: "2026-09-06", Days: 14, Since: since, Until: since.AddDate(0, 0, 15),
		Catalog: []render.Entry{
			{Repo: "alpha", Date: "2026-01-10", Kind: "notes", Title: "Kubernetes の運用", Path: "alpha/docs/notes/k8s.md"},
			{Repo: "alpha", Date: "", Kind: "notes", Title: "Terraform の書き方", Path: "alpha/docs/notes/tf.md"},
			{Repo: "beta", Date: "2026-09-01", Kind: "notes", Title: "Rust の所有権", Path: "beta/docs/notes/rust.md"},
		},
		Changes: changes,
	}
}

// 記録日が窓の外(古い・日付なし)でも、本文の変更の観測日が窓の中なら index の材料に数える。
// 観測日が空(記録を始めた時点で既にあった)・見当たらない記録・記録に無いノートは今までどおり記録日で決める。
func TestBuild_本文の変更で古いノートを数える(t *testing.T) {
	p, err := Build(changesInput([]changehistory.Entry{
		{Path: "alpha/docs/notes/k8s.md", Hash: "h1", Observed: "2026-09-05"},
		{Path: "alpha/docs/notes/tf.md", Hash: "h2", Observed: "2026-09-04"},
		{Path: "beta/docs/notes/rust.md", Hash: "h3", Observed: "2026-09-02"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if p.Sources[SourceIndex] != 3 || p.Changed != 2 {
		t.Errorf("index=%d changed=%d (3 と 2 を期待)", p.Sources[SourceIndex], p.Changed)
	}
	for _, w := range []string{"kubernetes", "terraform", "rust"} {
		if p.Weight(w) == 0 {
			t.Errorf("%s が語に無い: %+v", w, p.Terms)
		}
	}
	// 観測日が新しい方(2026-09-05)が、記録日が窓内でも古い観測(rust: 2026-09-02)より係数が高い
	if p.Weight("kubernetes") <= p.Weight("rust") {
		t.Errorf("観測日で係数を決めていない: kubernetes=%v rust=%v", p.Weight("kubernetes"), p.Weight("rust"))
	}
	if md := string(p.Marshal(0)); !strings.Contains(md, "材料: ノート 3（うち 2 は本文の変更で数えた）・セッション 0") {
		t.Errorf("材料の行: %s", md)
	}

	// 観測日が空・見当たらない・窓の外の観測は数えない。記録が nil なら今までどおり
	for name, changes := range map[string][]changehistory.Entry{
		"nil":    nil,
		"観測日が空":  {{Path: "alpha/docs/notes/k8s.md", Hash: "h1"}},
		"見当たらない": {{Path: "alpha/docs/notes/k8s.md", Hash: "h1", Observed: "2026-09-05", Missing: "2026-09-06"}},
		"窓の外の観測": {{Path: "alpha/docs/notes/k8s.md", Hash: "h1", Observed: "2026-08-01"}},
	} {
		p, err := Build(changesInput(changes))
		if err != nil {
			t.Fatal(err)
		}
		if p.Sources[SourceIndex] != 1 || p.Changed != 0 || p.Weight("kubernetes") != 0 {
			t.Errorf("[%s] index=%d changed=%d kubernetes=%v (1・0・0 を期待)", name, p.Sources[SourceIndex], p.Changed, p.Weight("kubernetes"))
		}
		if md := string(p.Marshal(0)); !strings.Contains(md, "材料: ノート 1・セッション 0") {
			t.Errorf("[%s] 変更が無いときは材料の行を変えない: %s", name, md)
		}
	}
}

// 記録日が窓の中で観測日がそれより古ければ記録日のまま(観測日で古くしない)。記録日より新しければ観測日。
func TestBuild_観測日は記録日より新しいときだけ使う(t *testing.T) {
	older, err := Build(changesInput([]changehistory.Entry{{Path: "beta/docs/notes/rust.md", Hash: "h", Observed: "2026-08-30"}}))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Build(changesInput(nil))
	if err != nil {
		t.Fatal(err)
	}
	if older.Weight("rust") != plain.Weight("rust") || older.Changed != 0 {
		t.Errorf("古い観測日で係数が変わった: %v != %v changed=%d", older.Weight("rust"), plain.Weight("rust"), older.Changed)
	}
	newer, err := Build(changesInput([]changehistory.Entry{{Path: "beta/docs/notes/rust.md", Hash: "h", Observed: "2026-09-06"}}))
	if err != nil {
		t.Fatal(err)
	}
	if newer.Changed != 0 || newer.Sources[SourceIndex] != 1 {
		t.Errorf("記録日が窓内なら Changed に数えない: changed=%d index=%d", newer.Changed, newer.Sources[SourceIndex])
	}
	// 1 件だけなので正規化で重みは 1 のまま。生の数(Counts)で係数の差を見る
	if newer.Terms[0].Counts[SourceIndex] <= plain.Terms[0].Counts[SourceIndex] {
		t.Errorf("新しい観測日で係数が上がっていない: %v <= %v", newer.Terms[0].Counts[SourceIndex], plain.Terms[0].Counts[SourceIndex])
	}
}
