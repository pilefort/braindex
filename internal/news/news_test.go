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

func TestSettings_Defaults(t *testing.T) {
	s := Settings{}.WithDefaults()
	if s.Dir != "news" || s.Feeds != "news/feeds.json" || s.SeenDays != 90 {
		t.Errorf("既定値: %+v", s)
	}
	if s.Cap("daily") != 15 || s.Cap("weekly") != 25 || s.Cap("all") != 20 || s.Cap("") != 20 {
		t.Errorf("既定の上限: daily=%d weekly=%d all=%d", s.Cap("daily"), s.Cap("weekly"), s.Cap("all"))
	}
	// 指定した層だけ上書き。他の層は既定の表ではなく DefaultCap(指定した表が正)
	s = Settings{CapPerLayer: map[string]int{"daily": 5}}.WithDefaults()
	if s.Cap("daily") != 5 || s.Cap("weekly") != 20 {
		t.Errorf("上書き: daily=%d weekly=%d", s.Cap("daily"), s.Cap("weekly"))
	}
}

func TestSettings_KeepMonths(t *testing.T) {
	if DefaultKeepMonths != 3 {
		t.Fatalf("既定の月数: got=%d want=3", DefaultKeepMonths)
	}
	for _, months := range []int{0, 1, 6} {
		s := Settings{KeepMonths: months}
		if err := s.Validate(); err != nil {
			t.Fatalf("keep_months=%d: %v", months, err)
		}
		want := months
		if want == 0 {
			want = DefaultKeepMonths
		}
		if got := s.WithDefaults().KeepMonths; got != want {
			t.Errorf("keep_months=%d: got=%d want=%d", months, got, want)
		}
	}
	if err := (Settings{KeepMonths: -1}).Validate(); err == nil || !strings.Contains(err.Error(), "news.keep_months") {
		t.Errorf("keep_months=-1 はキー名を含むエラーにする: %v", err)
	}
}

// show_min_score は 0(全件を主要表示)を設定できる。他のキーのように 0 を未設定とみなすと、
// 「全部見たい」を恒久設定にできない(決定 2026-09-03 → manual/news.md「決めたこと」)。
func TestSettings_ShowMinScore(t *testing.T) {
	if got := (Settings{}).MinScore(); got != DefaultShowMinScore {
		t.Errorf("未設定は既定 %d: got=%d", DefaultShowMinScore, got)
	}
	if got := (Settings{}).WithDefaults().MinScore(); got != DefaultShowMinScore {
		t.Errorf("WithDefaults 後も既定: got=%d", got)
	}
	zero := 0
	if got := (Settings{ShowMinScore: &zero}).WithDefaults().MinScore(); got != 0 {
		t.Errorf("0 が既定に置き換わっている: got=%d", got)
	}
	three := 3
	if got := (Settings{ShowMinScore: &three}).WithDefaults().MinScore(); got != 3 {
		t.Errorf("3 が保たれていない: got=%d", got)
	}
}

// 範囲外は既定に丸めず設定の誤りにする(丸めると、書いた値と動きが食い違ったまま気づけない)。
func TestSettings_Validate(t *testing.T) {
	if err := (Settings{}).Validate(); err != nil {
		t.Errorf("既定は通る: %v", err)
	}
	for _, n := range []int{-1, 4} {
		v := n
		err := (Settings{ShowMinScore: &v}).Validate()
		if err == nil {
			t.Errorf("show_min_score=%d はエラーにする", n)
			continue
		}
		if !strings.Contains(err.Error(), "show_min_score") {
			t.Errorf("エラー文にキー名が無い: %v", err)
		}
	}
	if err := (Settings{SeenDays: -1}).Validate(); err == nil {
		t.Error("seen_days=-1 はエラーにする")
	}
	if err := (Settings{ProfileDays: -1}).Validate(); err == nil {
		t.Error("profile_days=-1 はエラーにする")
	}
	if err := (Settings{CapPerLayer: map[string]int{"daily": -1}}).Validate(); err == nil {
		t.Error("cap_per_layer の負値はエラーにする")
	}
}

// WithDefaults が埋める層別上限は複製。呼び出し側が書き換えても既定の表は汚れない。
func TestSettings_DefaultsAreCopied(t *testing.T) {
	s := Settings{}.WithDefaults()
	s.CapPerLayer["daily"] = 1
	if DefaultCapPerLayer["daily"] != 15 {
		t.Errorf("既定の表が書き換わった: %v", DefaultCapPerLayer)
	}
	if got := (Settings{}).WithDefaults().Cap("daily"); got != 15 {
		t.Errorf("次の WithDefaults に漏れた: %d", got)
	}
}

func TestParseFeeds(t *testing.T) {
	good := `[{"name": "A", "url": "https://example.com/a.xml", "layer": "daily", "lang": "en", "category": "tech", "note": "x"},
	         {"name": "B", "url": "http://example.com/b.xml", "layer": "weekly"},
	         {"name": "C", "url": "https://example.com/c.xml"}]`
	srcs, err := ParseFeeds([]byte(good), "feeds.json")
	if err != nil || len(srcs) != 3 || srcs[0].Category != "tech" || srcs[1].Layer != "weekly" {
		t.Fatalf("err=%v srcs=%+v", err, srcs)
	}
	if got := FilterLayer(srcs, "daily"); len(got) != 1 || got[0].Name != "A" {
		t.Errorf("daily: %+v", got)
	}
	if got := FilterLayer(srcs, LayerAll); len(got) != 3 {
		t.Errorf("all: %+v", got)
	}
	if got := FilterLayer(srcs, "none"); got != nil {
		t.Errorf("none: %+v", got)
	}
	if got := Layers(srcs); !reflect.DeepEqual(got, []string{"daily", "weekly"}) {
		t.Errorf("Layers: %v", got)
	}

	bad := map[string]string{ // 入力 → エラーに含まれる語
		`[{"url": "https://example.com/a.xml"}]`:                                     "name が無い",
		`[{"name": "A", "url": "ftp://example.com/a"}]`:                              "http(s) でない",
		`[{"name": "A", "url": "https://x/a"}, {"name": "A", "url": "https://x/b"}]`: "重複",
		`[{"name": "A", "url": "https://x/a", "layers": "daily"}]`:                   "layers",
		`{"feeds": []}`: "feeds.json",
		`[{"name": "A", "url": "https://x/a"}] []`: "余分な内容",
	}
	for in, want := range bad {
		_, err := ParseFeeds([]byte(in), "feeds.json")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err=%v(%q を期待)", in, err, want)
		}
	}
}

func TestLoadFeeds_Missing(t *testing.T) {
	_, err := LoadFeeds(filepath.Join(t.TempDir(), "feeds.json"))
	if err == nil || !strings.Contains(err.Error(), "フィード一覧が無い") {
		t.Errorf("err=%v", err)
	}
}

func entry(i string) feed.Entry { return feed.Entry{ID: i, Title: "t" + i, Link: "https://x/" + i} }

func TestSeen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".seen.json")
	s, err := LoadSeen(path)
	if err != nil || len(s) != 0 {
		t.Fatalf("無いとき: err=%v s=%v", err, s)
	}
	s["id1"] = "2026-08-01"
	entries := []feed.Entry{entry("id1"), entry("id2")}
	if got := s.FilterNew(entries); len(got) != 1 || got[0].ID != "id2" {
		t.Errorf("FilterNew: %+v", got)
	}
	s.Mark(entries, "2026-08-15")
	if s["id1"] != "2026-08-01" || s["id2"] != "2026-08-15" {
		t.Errorf("Mark: %v", s)
	}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	want := "{\n\"id1\": \"2026-08-01\",\n\"id2\": \"2026-08-15\"\n}\n"
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != want {
		t.Errorf("Save:\n%s", b)
	}
	s2, err := LoadSeen(path)
	if err != nil || !reflect.DeepEqual(s, s2) {
		t.Errorf("往復: err=%v %v", err, s2)
	}
	if string(Seen{}.Marshal()) != "{}\n" {
		t.Errorf("空: %q", Seen{}.Marshal())
	}

	// Forget はその日に初めて見た印だけを外す(中断した回の書き直し用)
	f := Seen{"today1": "2026-08-15", "today2": "2026-08-15", "before": "2026-08-14"}
	if n := f.Forget("2026-08-15"); n != 2 || len(f) != 1 || f["before"] != "2026-08-14" {
		t.Errorf("Forget: n=%d %v", n, f)
	}
	if n := (Seen{"a": "2026-08-14"}).Forget("2026-08-15"); n != 0 {
		t.Errorf("別の日の印を外した: %d", n)
	}

	pruned, err := (Seen{"old": "2026-01-01", "new": "2026-08-10", "edge": "2026-05-17"}).Prune("2026-08-15", 90)
	if err != nil || len(pruned) != 2 || pruned["old"] != "" || pruned["new"] == "" || pruned["edge"] == "" {
		t.Errorf("Prune: err=%v %v", err, pruned)
	}
	if _, err := (Seen{}).Prune("2026/08/15", 90); err == nil {
		t.Error("日付の誤りがエラーにならない")
	}

	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSeen(path); err == nil {
		t.Error("壊れたファイルがエラーにならない")
	}
}

// stubFetcher は URL ごとに決めた結果を返す。
type stubFetcher map[string]any // feed.Document か error

func (f stubFetcher) Fetch(_ context.Context, url string) (feed.Document, error) {
	switch v := f[url].(type) {
	case feed.Document:
		return v, nil
	case error:
		return feed.Document{}, v
	}
	return feed.Document{}, errors.New("stub に無い URL " + url)
}

func TestCollect(t *testing.T) {
	srcs := []Source{{Name: "A", URL: "a"}, {Name: "B", URL: "b"}, {Name: "C", URL: "c"}}
	f := stubFetcher{
		"a": feed.Document{Entries: []feed.Entry{entry("a1"), entry("a2")}},
		"b": errors.New("HTTP 500"),
		"c": feed.Document{Entries: []feed.Entry{entry("c1")}},
	}
	seen := Seen{"a1": "2026-08-01"}
	res := Collect(context.Background(), f, srcs, seen, "2026-08-15", false)
	if len(res) != 3 || res[0].Source.Name != "A" || res[2].Source.Name != "C" {
		t.Fatalf("順序: %+v", res)
	}
	if ids(res[0].New) != "a2" || ids(res[2].New) != "c1" || res[1].Err == nil || len(res[1].Entries) != 0 {
		t.Errorf("新着: %+v", res)
	}
	if seen["a2"] != "2026-08-15" || seen["c1"] != "2026-08-15" || seen["a1"] != "2026-08-01" {
		t.Errorf("既読: %v", seen)
	}
	if AllFailed(res) || len(Failed(res)) != 1 {
		t.Errorf("失敗の数: %d", len(Failed(res)))
	}

	// replay: 既読を見ず全件、既読も増えない
	before := len(seen)
	res = Collect(context.Background(), f, srcs[:1], seen, "2026-08-16", true)
	if ids(res[0].New) != "a1,a2" || len(seen) != before {
		t.Errorf("replay: new=%s seen=%v", ids(res[0].New), seen)
	}

	if !AllFailed(Collect(context.Background(), f, srcs[1:2], seen, "2026-08-16", false)) || !AllFailed(nil) {
		t.Error("AllFailed")
	}
}

func ids(es []feed.Entry) string {
	var s []string
	for _, e := range es {
		s = append(s, e.ID)
	}
	return strings.Join(s, ",")
}

func TestDigest(t *testing.T) {
	res := []Result{
		{Source: Source{Name: "A", Category: "tech"}, New: []feed.Entry{
			{Title: "記事 [1]", Link: "https://x/1", Published: "2026-08-14"},
			{Title: "記事 2", Link: "https://x/2"},
			{Title: "記事 3", Link: "https://x/3"},
		}},
		{Source: Source{Name: "B"}, Err: errors.New("HTTP 404")},
		{Source: Source{Name: "C"}, New: nil}, // 新着なし: 書かない
		{Source: Source{Name: "D"}, New: []feed.Entry{{Title: "d", Link: "https://x/d"}}},
	}
	want := `# ニュースダイジェスト 2026-08-15（daily 層）

新着 4 件（フィード 3 本）・採点なし

## A（tech・新着 3 件）
- 2026-08-14 [記事 ［1］](https://x/1)
- [記事 2](https://x/2)
- （上限 2 件を超えた 1 件は省略）

## D（新着 1 件）
- [d](https://x/d)

## 取得失敗
- B: HTTP 404
`
	o := DigestOptions{Layer: "daily", Today: "2026-08-15", Cap: 2}
	got := string(Digest(res, o))
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if string(Digest(res, o)) != got {
		t.Error("2 回の生成が一致しない")
	}
	empty := string(Digest(nil, DigestOptions{Layer: "all", Today: "2026-08-15", Cap: 20}))
	if !strings.Contains(empty, "新着 0 件（フィード 0 本）・採点なし") || strings.Contains(empty, "取得失敗") {
		t.Errorf("空:\n%s", empty)
	}
}

// md のダイジェストも HTML(itemHTML)や keep(appendKeeps)と同じく weblink.Safe を通す
// (決定 2026-09-03「生成物のリンクは http(s) 以外を落とす」→ manual/design.md「決めたこと」)。
// 安全でないリンクは HTML と同じ扱いにする: リンクを付けず題名だけを出す。
func TestDigest_安全でないリンクは題名だけ(t *testing.T) {
	res := []Result{{Source: Source{Name: "A"}, New: []feed.Entry{
		{Title: "危ない", Link: "javascript:alert(1)"},
		{Title: "普通", Link: "https://x/ok"},
	}}}
	want := "# ニュースダイジェスト 2026-08-15（daily 層）\n\n" +
		"新着 2 件（フィード 1 本）・採点なし\n\n" +
		"## A（新着 2 件）\n" +
		"- 危ない\n" +
		"- [普通](https://x/ok)\n\n"
	got := string(Digest(res, DigestOptions{Layer: "daily", Today: "2026-08-15", Cap: 20}))
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, "javascript:") {
		t.Errorf("安全でないリンクをそのまま埋めている:\n%s", got)
	}
}

// 採点あり: 主要(関心度 降順)と関心外の二段。当たった語を添える。各段に上限。
func TestDigest_Ranked(t *testing.T) {
	p := interest.Profile{Terms: []interest.Term{{Word: "ゴルーチン", Weight: 2}, {Word: "パース", Weight: 1}, {Word: "rust", Weight: 0.4}}}
	res := []Result{{Source: Source{Name: "A"}, New: []feed.Entry{
		{ID: "1", Title: "Rust 入門", Link: "https://x/1"},
		{ID: "2", Title: "関係ない", Link: "https://x/2"},
		{ID: "3", Title: "ゴルーチン", Link: "https://x/3", Summary: "Rust から パース"}, // 3.4/2 = 1.7 → 3
		{ID: "4", Title: "ゴルーチン の話", Link: "https://x/4"},
		{ID: "5", Title: "無関係 2", Link: "https://x/5"},
		{ID: "6", Title: "無関係 3", Link: "https://x/6"},
	}}}
	rk := Rank(res, p, nil)
	if rk["3"].Value != 3 || rk["4"].Value != 2 || rk["1"].Value != 1 || rk["2"].Value != 0 {
		t.Fatalf("Rank: %v", rk)
	}
	main, low := Split(res[0].New, rk, 2)
	if ids(main) != "3,4" || ids(low) != "1,2,5,6" {
		t.Errorf("Split: main=%s low=%s", ids(main), ids(low))
	}
	want := `# ニュースダイジェスト 2026-08-15（daily 層）

新着 6 件（フィード 1 本）・関心度 2 以上を主要表示

## A（新着 6 件・主要 2 件）
- [ゴルーチン](https://x/3) ★3（ゴルーチン・パース・rust）
- [ゴルーチン の話](https://x/4) ★2（ゴルーチン）
- 関心外と判定 4 件:
  - [Rust 入門](https://x/1) ★1（rust）
  - [関係ない](https://x/2) ★0
  - （上限 2 件を超えた 2 件は省略）

`
	got := string(Digest(res, DigestOptions{Layer: "daily", Today: "2026-08-15", Cap: 2, Ranking: rk, MinScore: 2}))
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	// 決定性: 採点からやり直しても同じバイト列。Ranking も Score.Matched も map を経由するので、
	// 走査順が出力に漏れていれば実行のたびに揺れる(1 回だけでは捕まらないので繰り返す)
	for i := 0; i < 5; i++ {
		again := string(Digest(res, DigestOptions{Layer: "daily", Today: "2026-08-15", Cap: 2, Ranking: Rank(res, p, nil), MinScore: 2}))
		if again != got {
			t.Fatalf("%d 回目の生成が一致しない:\n%s\nwant:\n%s", i+2, again, got)
		}
	}
	// 空のプロファイルは採点無し
	if Rank(res, interest.Profile{}, nil) != nil {
		t.Error("空のプロファイルで採点した")
	}
	if m, l := Split(res[0].New, nil, 2); len(m) != 6 || l != nil {
		t.Error("採点無しで分けた")
	}
}

// フィードの見出し「主要 N 件」は上限(Cap)で切った後、実際に書いた件数に揃える
// (決定 2026-09-12「主要 N 件は上限で切った後の数に揃える」→ manual/news.md「決めたこと」)。
// 主要と判定した件数が上限を超えるとき、見出しの N が上限前の件数のままだと、
// 実際に書いた項目数(上限後)と食い違う。
func TestDigest_主要件数は上限後の件数に揃う(t *testing.T) {
	p := interest.Profile{Terms: []interest.Term{{Word: "ゴルーチン", Weight: 2}}}
	res := []Result{{Source: Source{Name: "A"}, New: []feed.Entry{
		{ID: "1", Title: "ゴルーチン 1", Link: "https://x/1"},
		{ID: "2", Title: "ゴルーチン 2", Link: "https://x/2"},
		{ID: "3", Title: "ゴルーチン 3", Link: "https://x/3"},
		{ID: "4", Title: "ゴルーチン 4", Link: "https://x/4"},
	}}}
	rk := Rank(res, p, nil)
	main, _ := Split(res[0].New, rk, 2)
	if len(main) != 4 {
		t.Fatalf("前提: 主要の判定が 4 件でない: %v", main)
	}
	got := string(Digest(res, DigestOptions{Layer: "daily", Today: "2026-08-15", Cap: 2, Ranking: rk, MinScore: 2}))
	if !strings.Contains(got, "## A（新着 4 件・主要 2 件）") {
		t.Errorf("見出しの主要件数が上限後(2 件)になっていない:\n%s", got)
	}
	if strings.Contains(got, "主要 4 件") {
		t.Errorf("見出しが上限前の件数(4 件)のまま:\n%s", got)
	}
}

// 下げた取材先の記事は関心度の上限が DemotedMaxScore になる。ほかの取材先は変わらない。
func TestRank_下げた取材先は上限が下がる(t *testing.T) {
	p := interest.Profile{Today: "2026-09-06", Terms: []interest.Term{
		{Word: "docker", Weight: 1.0}, {Word: "kubernetes", Weight: 1.0},
	}}
	res := []Result{
		{Source: Source{Name: "よく読む"}, New: []feed.Entry{{ID: "a", Title: "docker と kubernetes の話"}}},
		{Source: Source{Name: "不要ばかり"}, New: []feed.Entry{{ID: "b", Title: "docker と kubernetes の話"}}},
	}
	base := Rank(res, p, nil)
	if base["a"].Value != base["b"].Value {
		t.Fatalf("テストの前提: 同じ見出しは同じ点 (%d vs %d)", base["a"].Value, base["b"].Value)
	}
	if base["a"].Value <= DemotedMaxScore {
		t.Fatalf("テストの前提: 下げる前の点が上限より大きい (%d)", base["a"].Value)
	}

	rk := Rank(res, p, map[string]bool{"不要ばかり": true})
	if rk["a"].Value != base["a"].Value {
		t.Errorf("下げていない取材先の点が変わった: %d", rk["a"].Value)
	}
	if rk["b"].Value != DemotedMaxScore {
		t.Errorf("下げた取材先の点: want=%d got=%d", DemotedMaxScore, rk["b"].Value)
	}
	// 当たった語は残す(なぜ点が付いたかは見えるようにする)
	if len(rk["b"].Matched) == 0 {
		t.Error("当たった語まで消した")
	}
	// 点を上書きした後(LLM の採点)でも、もう一度かければ上限まで戻る
	rk["b"] = interest.Score{Value: interest.MaxScore, LLM: true, Matched: []string{"LLM"}}
	rk = CapDemoted(rk, res, map[string]bool{"不要ばかり": true})
	if rk["b"].Value != DemotedMaxScore || !rk["b"].LLM || len(rk["b"].Matched) != 1 || rk["b"].Matched[0] != "LLM" {
		t.Errorf("上書きの後の CapDemoted: %+v", rk["b"])
	}
	if rk["a"].Value != base["a"].Value {
		t.Errorf("下げていない取材先を CapDemoted が変えた: %d", rk["a"].Value)
	}
}

// 関心外から拾い上げる選び方: 主要表示は選ばない・関心度が高い方(1)を先に・1 フィード 1 件・同じ日なら同じ結果。
func TestPickSerendipity(t *testing.T) {
	results := []Result{
		{Source: Source{Name: "A"}, New: []feed.Entry{{ID: "a0"}, {ID: "a1"}, {ID: "a2"}}},
		{Source: Source{Name: "B"}, New: []feed.Entry{{ID: "b0"}, {ID: "b1"}}},
		{Source: Source{Name: "C"}, Err: errors.New("失敗")},
	}
	rk := Ranking{
		"a0": {Value: 0}, "a1": {Value: 1}, "a2": {Value: 3},
		"b0": {Value: 0}, "b1": {Value: 1},
	}
	got := PickSerendipity(results, rk, 2, 2, "2026-09-07")
	if len(got) != 2 {
		t.Fatalf("選んだ数 %d want 2: %v", len(got), got)
	}
	if got["a2"] {
		t.Errorf("主要表示の記事を選んでいる: %v", got)
	}
	if !got["a1"] || !got["b1"] {
		t.Errorf("関心度 1 でなく 0 を選んでいる(1 フィード 1 件のはず): %v", got)
	}
	if again := PickSerendipity(results, rk, 2, 2, "2026-09-07"); !reflect.DeepEqual(got, again) {
		t.Errorf("同じ日で結果が変わる: %v → %v", got, again)
	}
	if n := PickSerendipity(results, rk, 2, 0, "2026-09-07"); n != nil {
		t.Errorf("0 件の指定で選んでいる: %v", n)
	}
	if n := PickSerendipity(results, nil, 2, 2, "2026-09-07"); n != nil {
		t.Errorf("採点が無いのに選んでいる: %v", n)
	}
	// 1 フィードしか無ければ、指定より少なくても 1 件だけ
	one := PickSerendipity(results[:1], rk, 2, 2, "2026-09-07")
	if len(one) != 1 {
		t.Errorf("1 フィードから %d 件選んだ want 1: %v", len(one), one)
	}
}

// 拾い上げた記事は「ほかの記事」から外す(同じ記事が 2 か所に出ると選別の状態が壊れる)。
func TestTakeSerendipity(t *testing.T) {
	low := []feed.Entry{{ID: "x"}, {ID: "y"}, {ID: "z"}}
	rest, picked := TakeSerendipity(low, map[string]bool{"y": true})
	if len(rest) != 2 || rest[0].ID != "x" || rest[1].ID != "z" {
		t.Errorf("残り %+v", rest)
	}
	if len(picked) != 1 || picked[0].ID != "y" {
		t.Errorf("抜いた分 %+v", picked)
	}
	if r, p := TakeSerendipity(low, nil); len(r) != 3 || p != nil {
		t.Errorf("指定が無いときは触らない: %+v %+v", r, p)
	}
}

// Markdown に「もしかして興味あるかも」の節が出て、その記事は関心外の段から消える。
func TestDigest_Serendipity(t *testing.T) {
	results := []Result{{Source: Source{Name: "A"}, New: []feed.Entry{
		{ID: "hi", Title: "主要の記事", Link: "https://example.com/1"},
		{ID: "lo", Title: "拾い上げた記事", Link: "https://example.com/2"},
		{ID: "lo2", Title: "ほかの記事", Link: "https://example.com/3"},
	}}}
	rk := Ranking{"hi": {Value: 3}, "lo": {Value: 1}, "lo2": {Value: 0}}
	md := string(Digest(results, DigestOptions{Layer: "daily", Today: "2026-09-07", Cap: 10, Ranking: rk, MinScore: 2,
		Serendipity: map[string]bool{"lo": true}}))
	if !strings.Contains(md, "## "+SerendipityLabel+"（1 件）") {
		t.Errorf("拾い上げの節が無い:\n%s", md)
	}
	if !strings.Contains(md, "- 関心外と判定 1 件:") {
		t.Errorf("関心外の段から拾い上げた分を外していない:\n%s", md)
	}
	if strings.Count(md, "拾い上げた記事") != 1 {
		t.Errorf("同じ記事が 2 か所に出ている:\n%s", md)
	}
}

func TestSettingsLLMBudget(t *testing.T) {
	if (Settings{}).WithDefaults().LLMBudgetSec != 600 {
		t.Fatal("default")
	}
	if (Settings{LLMBudgetSec: 5}).WithDefaults().LLMBudgetSec != 5 {
		t.Fatal("explicit")
	}
	if (Settings{LLMBudgetSec: -1}).Validate() == nil {
		t.Fatal("negative accepted")
	}
}
func TestGeneralNewsCap(t *testing.T) {
	s := Settings{}.WithDefaults()
	if s.Cap(LayerGeneralNews) != 5 {
		t.Fatalf("cap=%d", s.Cap(LayerGeneralNews))
	}
	s.CapPerLayer[LayerGeneralNews] = 1
	if (Settings{}).WithDefaults().Cap(LayerGeneralNews) != 5 {
		t.Fatal("default map mutated")
	}
	s = Settings{CapPerLayer: map[string]int{"daily": 2}}.WithDefaults()
	if s.Cap(LayerGeneralNews) != DefaultCap {
		t.Fatal("explicit table not authoritative")
	}
	s = Settings{CapPerLayer: map[string]int{LayerGeneralNews: 7}}.WithDefaults()
	if s.Cap(LayerGeneralNews) != 7 {
		t.Fatal("explicit general cap ignored")
	}
}

func TestDigestSuggestions(t *testing.T) {
	o := DigestOptions{Suggestions: []Suggestion{{CatalogEntry: CatalogEntry{Name: "Example", Genre: "開発", URL: "https://example.com/rss"}, Matched: []string{"compiler"}}}, QueryCandidates: []string{"science"}}
	results := []Result{{Source: Source{Name: "Failed"}, Err: errors.New("failed")}}
	md := string(Digest(results, o))
	for _, want := range []string{"## 関心に合う取材先の候補", "1. **Example** — 開発 — 当たった語: compiler — https://example.com/rss", "- 検索語の候補: 「science」 — " + SearchFeedURL("science")} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Index(md, "## 関心に合う取材先の候補") > strings.Index(md, "## 取得失敗") {
		t.Fatal("suggestions after failures")
	}
	if md != string(Digest(results, o)) {
		t.Fatal("md not deterministic")
	}
	if strings.Contains(string(Digest(nil, DigestOptions{})), "## 関心に合う取材先の候補") {
		t.Fatal("empty section")
	}
}
