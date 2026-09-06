package catalog

import (
	"reflect"
	"sort"
	"testing"

	"github.com/pilefort/braindex/internal/indexdata"
	"github.com/pilefort/braindex/internal/render"
)

// 直接渡すレコードでも、保存した表を読む場合と同じ比較結果になる。
// fixture のパイプ文字の全角化も比較対象に含める。
func TestBuildRecordsMatchSavedCatalog(t *testing.T) {
	result, err := Build(e2eConfig(), "2026-08-07")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := indexdata.ParseCatalog(result.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	got := append([]indexdata.Entry(nil), result.Records...)
	sort.Slice(got, func(i, j int) bool { return got[i].Path < got[j].Path })
	sort.Slice(parsed, func(i, j int) bool { return parsed[i].Path < parsed[j].Path })
	if result.Entries != len(got) || !reflect.DeepEqual(got, parsed) {
		t.Fatalf("records=%+v parsed=%+v count=%d", got, parsed, result.Entries)
	}
	// 索引 = レコードの描画 + 走査の記録。記録を足しても表は変わらない
	if string(withCoverage(render.Render(result.Records, "2026-08-07"), result.Coverage)) != string(result.Catalog) {
		t.Fatal("レコードの再描画で索引が変わった")
	}
}
