package changehistory

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/pilefort/braindex/internal/scan"
)

// BOM と改行の違いは本文の変更に数えない。本文が違えばハッシュも違う。
func TestHash_正規化(t *testing.T) {
	base := Hash([]byte("# T\n\n結論: a\n"))
	if len(base) != 64 {
		t.Fatalf("sha256 の 16 進(64 桁)のはず: %q", base)
	}
	for name, in := range map[string]string{
		"CRLF":     "# T\r\n\r\n結論: a\r\n",
		"CR":       "# T\r\r結論: a\r",
		"BOM":      "\xEF\xBB\xBF# T\n\n結論: a\n",
		"BOM+CRLF": "\xEF\xBB\xBF# T\r\n\r\n結論: a\r\n",
	} {
		if got := Hash([]byte(in)); got != base {
			t.Errorf("[%s] 正規化で同じになるはず: %s != %s", name, got, base)
		}
	}
	if Hash([]byte("# T\n\n結論: a\n\n追記\n")) == base {
		t.Errorf("本文が違うのに同じハッシュ")
	}
	if Hash(nil) != Hash([]byte{}) {
		t.Errorf("nil と空は同じ")
	}
}

// 記録が無い初回は、全件を観測日不明(空)で記録する。「今日変わった」と捏造しない。
func TestUpdate_初回は観測日不明(t *testing.T) {
	seen := []Note{{Path: "b/docs/notes/b.md", Hash: "hb"}, {Path: "a/docs/notes/a.md", Hash: "ha"}}
	h, rep := Update(History{}, seen, nil, "2026-09-06")
	if !rep.Initial || rep.Total != 2 || rep.New != 0 || rep.Changed != 0 {
		t.Errorf("report=%+v", rep)
	}
	want := []Entry{{Path: "a/docs/notes/a.md", Hash: "ha"}, {Path: "b/docs/notes/b.md", Hash: "hb"}}
	if !reflect.DeepEqual(h.Notes, want) || h.Version != Version {
		t.Errorf("got=%+v", h)
	}
	// 記録はあるが 0 件(初回ではない)なら、新しいノートは今日観測した
	h2, rep2 := Update(History{Version: Version, Notes: []Entry{}}, seen[:1], nil, "2026-09-06")
	if rep2.Initial || rep2.New != 1 || h2.Notes[0].Observed != "2026-09-06" {
		t.Errorf("記録ありの新規: report=%+v notes=%+v", rep2, h2.Notes)
	}
}

// 要旨以外(本文の後半)だけの変更でもハッシュが変わり、観測日が今日になる。変わらなければ記録は同じバイト列。
func TestUpdate_本文だけの変更と変更なし(t *testing.T) {
	before := []byte("# 題\n\n結論: 同じ要旨\n\n本文の後半\n")
	after := []byte("# 題\n\n結論: 同じ要旨\n\n本文の後半を書き足した\n")
	prev, _ := Update(History{}, []Note{{Path: "r/docs/notes/n.md", Hash: Hash(before)}}, nil, "2026-09-01")

	same, rep := Update(prev, []Note{{Path: "r/docs/notes/n.md", Hash: Hash(before)}}, nil, "2026-09-06")
	if rep.Initial || rep.Changed != 0 || rep.New != 0 || rep.Missing != 0 || !bytes.Equal(same.Marshal(), prev.Marshal()) {
		t.Errorf("変更なしで記録が変わった: report=%+v\n%s", rep, same.Marshal())
	}

	changed, rep := Update(prev, []Note{{Path: "r/docs/notes/n.md", Hash: Hash(after)}}, nil, "2026-09-06")
	if rep.Changed != 1 || changed.Notes[0].Observed != "2026-09-06" || changed.Notes[0].Hash != Hash(after) {
		t.Errorf("本文だけの変更を観測できていない: report=%+v notes=%+v", rep, changed.Notes)
	}
	// 同じ材料からは同じ記録(決定性)
	again, _ := Update(prev, []Note{{Path: "r/docs/notes/n.md", Hash: Hash(after)}}, nil, "2026-09-06")
	if !bytes.Equal(again.Marshal(), changed.Marshal()) {
		t.Errorf("同じ材料で記録が違う")
	}
}

// 読めなかった範囲(ファイル・ディレクトリ)にある記録は前回のまま据え置く。見当たらないとも変わったとも言わない。
func TestUpdate_読めなかった範囲は据え置く(t *testing.T) {
	prev := History{Version: Version, Notes: []Entry{
		{Path: "r/docs/notes/locked/deep.md", Hash: "h1", Observed: "2026-08-01"},
		{Path: "r/docs/notes/ok.md", Hash: "h2", Observed: "2026-08-02"},
		{Path: "r/docs/notes/unreadable.md", Hash: "h3", Observed: "2026-08-03"},
		{Path: "r/docs/notes/gone.md", Hash: "h4", Observed: "2026-08-04"},
	}}
	gaps := []scan.Gap{
		{Rel: "r/docs/notes/locked", Dir: true, Reason: "permission denied"},
		{Rel: "r/docs/notes/unreadable.md", Reason: "permission denied"},
	}
	h, rep := Update(prev, []Note{{Path: "r/docs/notes/ok.md", Hash: "h2"}}, gaps, "2026-09-06")
	if rep.Held != 2 || rep.Missing != 1 || rep.Changed != 0 {
		t.Errorf("report=%+v", rep)
	}
	byPath := map[string]Entry{}
	for _, e := range h.Notes {
		byPath[e.Path] = e
	}
	for _, p := range []string{"r/docs/notes/locked/deep.md", "r/docs/notes/unreadable.md"} {
		if e := byPath[p]; e.Missing != "" || e.Observed != prev.Notes[0].Observed && p == "r/docs/notes/locked/deep.md" {
			t.Errorf("%s は据え置くべき: %+v", p, e)
		}
	}
	if e := byPath["r/docs/notes/gone.md"]; e.Missing != "2026-09-06" {
		t.Errorf("範囲の外で見当たらないなら missing: %+v", e)
	}
}

// 消えたノートは見当たらない日を持つ。同じ内容で戻れば観測日は元のまま、違う内容で戻れば今日。
func TestUpdate_削除と再出現(t *testing.T) {
	p := "r/docs/notes/n.md"
	h0, _ := Update(History{}, []Note{{Path: p, Hash: "h1"}}, nil, "2026-08-01")
	h1, rep := Update(h0, nil, nil, "2026-09-01")
	if rep.Missing != 1 || len(h1.Notes) != 1 || h1.Notes[0].Missing != "2026-09-01" {
		t.Fatalf("削除: report=%+v notes=%+v", rep, h1.Notes)
	}
	if len(h1.Present()) != 0 {
		t.Errorf("Present に見当たらないものが入っている")
	}
	// 見当たらないままなら日付は据え置き
	h1b, rep := Update(h1, nil, nil, "2026-09-02")
	if rep.Missing != 0 || h1b.Notes[0].Missing != "2026-09-01" {
		t.Errorf("見当たらない日が動いた: report=%+v notes=%+v", rep, h1b.Notes)
	}
	// 同じ内容で戻る
	h2, rep := Update(h1, []Note{{Path: p, Hash: "h1"}}, nil, "2026-09-06")
	if rep.Reappeared != 1 || rep.Changed != 0 || h2.Notes[0].Missing != "" || h2.Notes[0].Observed != "" {
		t.Errorf("同じ内容の再出現: report=%+v notes=%+v", rep, h2.Notes)
	}
	// 違う内容で戻る
	h3, rep := Update(h1, []Note{{Path: p, Hash: "h2"}}, nil, "2026-09-06")
	if rep.Changed != 1 || rep.Reappeared != 0 || h3.Notes[0].Missing != "" || h3.Notes[0].Observed != "2026-09-06" {
		t.Errorf("違う内容の再出現: report=%+v notes=%+v", rep, h3.Notes)
	}
}

// Marshal と Parse は往復する。並びは Path 昇順・LF・末尾に改行 1 つ・空でも notes は []。
func TestMarshalParse_往復(t *testing.T) {
	h := History{Version: Version, Notes: []Entry{
		{Path: "z/docs/notes/z.md", Hash: "hz", Observed: "2026-09-06", Missing: "2026-09-07"},
		{Path: "a/docs/notes/a.md", Hash: "ha"},
	}}
	b := h.Marshal()
	want := "{\n  \"version\": 1,\n  \"notes\": [\n    {\n      \"path\": \"a/docs/notes/a.md\",\n      \"hash\": \"ha\"\n    },\n" +
		"    {\n      \"path\": \"z/docs/notes/z.md\",\n      \"hash\": \"hz\",\n      \"observed\": \"2026-09-06\",\n      \"missing\": \"2026-09-07\"\n    }\n  ]\n}\n"
	if string(b) != want {
		t.Errorf("形が違う:\n%s", b)
	}
	got, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Notes, []Entry{h.Notes[1], h.Notes[0]}) {
		t.Errorf("往復で値が変わった: %+v", got.Notes)
	}
	if empty := (History{}).Marshal(); string(empty) != "{\n  \"version\": 1,\n  \"notes\": []\n}\n" {
		t.Errorf("空の記録: %s", empty)
	}
	if got, err := Parse((History{}).Marshal()); err != nil || !got.Known() || len(got.Notes) != 0 {
		t.Errorf("空の記録を読み戻すと記録あり・0 件のはず: %+v %v", got, err)
	}
}

// 壊れた記録は無言で読み飛ばさずエラーにする(次の Save で前回の観測を失わないため)。
func TestParse_壊れた記録(t *testing.T) {
	cases := map[string]string{
		"空":        "",
		"JSON でない": "{",
		"末尾に余分":    "{\"version\":1,\"notes\":[]} x",
		"未知のキー":    "{\"version\":1,\"notes\":[],\"extra\":1}",
		"版が違う":     "{\"version\":2,\"notes\":[]}",
		"版が無い":     "{\"notes\":[]}",
		"path が空":  "{\"version\":1,\"notes\":[{\"path\":\"\",\"hash\":\"h\"}]}",
		"hash が空":  "{\"version\":1,\"notes\":[{\"path\":\"a.md\",\"hash\":\"\"}]}",
		"path の重複": "{\"version\":1,\"notes\":[{\"path\":\"a.md\",\"hash\":\"h\"},{\"path\":\"a.md\",\"hash\":\"h\"}]}",
		"observed": "{\"version\":1,\"notes\":[{\"path\":\"a.md\",\"hash\":\"h\",\"observed\":\"2026/09/06\"}]}",
		"missing":  "{\"version\":1,\"notes\":[{\"path\":\"a.md\",\"hash\":\"h\",\"missing\":\"昨日\"}]}",
	}
	for desc, in := range cases {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("[%s] エラーになっていない", desc)
		}
	}
}

// 無ければ記録なし(エラーにしない)。書いて読めば同じ値。壊れていればパス付きのエラー。
func TestLoadSave(t *testing.T) {
	p := filepath.Join(t.TempDir(), "index", FileName)
	h, err := Load(p)
	if err != nil || h.Known() {
		t.Fatalf("無い記録: %+v %v", h, err)
	}
	want, _ := Update(History{}, []Note{{Path: "r/docs/notes/a.md", Hash: "ha"}}, nil, "2026-09-06")
	if err := Save(p, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("読み戻し: %+v %v", got, err)
	}
	if err := os.WriteFile(p, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), FileName) {
		t.Errorf("壊れた記録はパス付きのエラー: %v", err)
	}
	// 一時ファイルが残らない
	des, _ := os.ReadDir(filepath.Dir(p))
	if len(des) != 1 {
		t.Errorf("一時ファイルが残っている: %v", des)
	}
}

// 同じ hub で生成が重なっても、記録は常にどちらかの完全な内容で、読み手が半端なものを読むことはない。
// 排他は入れていない(書き切ってから置き換えるだけ)。同じ材料からは同じ記録になるので、どちらが勝っても内容は同じ。
func TestSave_同時実行(t *testing.T) {
	p := filepath.Join(t.TempDir(), FileName)
	seen := []Note{{Path: "r/docs/notes/a.md", Hash: "ha"}, {Path: "r/docs/notes/b.md", Hash: "hb"}}
	first, _ := Update(History{}, seen, nil, "2026-09-01")
	if err := Save(p, first); err != nil {
		t.Fatal(err)
	}
	want, _ := Update(first, seen, nil, "2026-09-06")
	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n*2)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			prev, err := Load(p)
			if err != nil {
				errs <- err
				return
			}
			next, _ := Update(prev, seen, nil, "2026-09-06")
			if err := Save(p, next); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		// 置き換えの瞬間に読み書きが重なると OS が拒むことはあるが、記録が壊れることはない
		t.Logf("同時実行中のエラー(壊れていなければよい): %v", err)
	}
	got, err := Load(p)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("同時実行後の記録: %+v %v", got, err)
	}
}
