package interest

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pilefort/braindex/internal/render"
	"github.com/pilefort/braindex/internal/sessions"
)

func TestWords(t *testing.T) {
	cases := map[string][]string{
		"Go の encoding/xml で RSS を読む":                  {"go", "encoding", "rss"},        // go は 2 文字の許可リスト・xml はストップワード・小文字に畳む
		"ベクトルDBは入れない。埋め込みも同じ":                          {"db", "ベクトル"},                   // db は 2 文字の許可リスト。「埋め込み」は漢字が連続しないので語にならない
		"設計判断記録日本語版 という長い複合語":                          {"複合語"},                          // 漢字 9 連続は捨てる。「長い」は 1 文字
		"ラテン文字とカタカナ・混在-abc_def-":                       {"abc_def", "ラテン", "カタカナ", "混在"}, // 中黒で切る・前後の記号を落とす。「文字」はストップワード
		"123 456 a1 ab1 abc1":                          {"ab1", "abc1"},                  // 数字だけ・先頭数字・2 文字は除く
		"the file is in the docs and the docs are new": nil,                              // 全部ストップワード
		"設定 ファイル confirm 設定":                           {"confirm"},                      // 重複は 1 回・ストップワード
		"":                                             nil,
		"ーーー ・・・":                                      nil, // 長音・中黒だけ
	}
	for in, want := range cases {
		if got := Words(in); !reflect.DeepEqual(got, want) {
			t.Errorf("Words(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestStripURLs(t *testing.T) {
	got := Words(StripURLs("記事 https://example.com/path?utm=1 と http://x.test/a を読む"))
	if !reflect.DeepEqual(got, []string{"記事"}) {
		t.Errorf("%v", got)
	}
}

func day(s string) time.Time {
	d, _ := time.Parse("2006-01-02", s)
	return d.Add(12 * time.Hour)
}

// window は today の days 日前 0 時から today の翌日 0 時まで(本番では呼び出し側がローカルの 0 時で作る)。
func window(today string, days int) (time.Time, time.Time) {
	t, err := time.Parse("2006-01-02", today)
	if err != nil {
		panic(err)
	}
	return t.AddDate(0, 0, -days), t.AddDate(0, 0, 1)
}

func TestBuild(t *testing.T) {
	since, until := window("2026-09-03", 14)
	in := Input{
		Today: "2026-09-03", Days: 14, Since: since, Until: until,
		Catalog: []render.Entry{
			{Repo: "repo-a", Date: "2026-09-03", Kind: "note", Title: "フィード の パース"}, // 今日: 係数 2
			{Repo: "repo-a", Date: "2026-08-20", Kind: "note", Title: "フィード の 既読"},  // 窓の端: 係数 1
			{Repo: "repo-b", Date: "2026-08-19", Kind: "note", Title: "窓の外 スケジューラ"}, // 窓の外
			{Repo: "repo-b", Date: "2026-09-04", Kind: "note", Title: "未来 スケジューラ"},  // 今日より後
		},
		Sessions: []sessions.Session{
			{ID: "s1", Turns: []sessions.Turn{{Role: sessions.User, Time: day("2026-09-01"), Text: "フィード を パース したい https://example.com/x"}}},
			{ID: "s2", Turns: []sessions.Turn{
				{Role: sessions.User, Time: day("2026-09-02"), Text: "パース"},
				{Role: sessions.Assistant, Time: day("2026-09-02"), Text: "パース と ゴルーチン"},
			}},
			{ID: "old", Turns: []sessions.Turn{{Role: sessions.User, Time: day("2026-08-01"), Text: "スケジューラ"}}}, // 窓の外
			{ID: "notime", Turns: []sessions.Turn{{Role: sessions.User, Text: "スケジューラ"}}},                       // 時刻なし
		},
		Keeps: []Keep{
			{Month: "2026-08", Title: "ゴルーチン の 本"},
			{Month: "2026-05", Title: "スケジューラ"}, // 3 か月より前
		},
		Extra: []string{"# コメント", "", "  Rust  ", "ゴルーチン"},
	}
	p, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Sources, map[string]int{SourceIndex: 2, SourceSessions: 2, SourceKeep: 1, SourceExtra: 2}) {
		t.Errorf("Sources: %v", p.Sources)
	}
	if p.Weight("スケジューラ") != 0 || p.Weight("example") != 0 {
		t.Error("窓の外・URL の語が入った")
	}
	// パース: index 2(今日)/max 3(フィード)=0.667・sessions 2/2=1 → 1.667。フィード: index 3/3=1・sessions 1/2=0.5 → 1.5
	// ゴルーチン: sessions 1/2=0.5・keep 1/1=1・extra 1 → 2.5(最大)。repo-a: index 3/3=1(リポ名も語)。既読: index 1/3
	want := []string{"ゴルーチン", "パース", "フィード", "repo-a", "rust", "既読"}
	var got []string
	for _, tm := range p.Terms {
		got = append(got, tm.Word)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("順序: %v (want %v)\n%s", got, want, p.Marshal(0))
	}
	if p.Terms[0].Weight != 2.5 || p.Terms[1].Weight != 1.667 || p.Terms[2].Weight != 1.5 || p.Terms[3].Weight != 1 || p.Terms[5].Weight != 0.333 {
		t.Errorf("重み: %+v", p.Terms)
	}
	if c := p.Terms[2].Counts; c[SourceIndex] != 3 || c[SourceSessions] != 1 || c[SourceKeep] != 0 {
		t.Errorf("フィードの出典: %v", c)
	}

	// 決定性
	p2, _ := Build(in)
	if !reflect.DeepEqual(p, p2) || string(p.Marshal(10)) != string(p2.Marshal(10)) {
		t.Error("2 回の生成が一致しない")
	}
	// 表
	md := string(p.Marshal(2))
	for _, s := range []string{"# 関心プロファイル 2026-09-03（直近 14 日）", "材料: ノート 2・セッション 2・keep 1・補助 2 ／ 語 6",
		"| ゴルーチン | 2.500 | | 1 | 1 | 1 |", "| パース | 1.667 | 2 | 2 | | |", "（上位 2 語。残り 4 語）"} {
		if !strings.Contains(md, s) {
			t.Errorf("表に %q が無い:\n%s", s, md)
		}
	}
	if strings.Contains(md, "フィード") {
		t.Error("top を超えて書いた")
	}
	j, err := p.JSON()
	if err != nil || !strings.Contains(string(j), `"word": "ゴルーチン"`) || !strings.HasSuffix(string(j), "}\n") {
		t.Errorf("JSON: err=%v\n%s", err, j)
	}
}

func TestBuild_EmptyAndErrors(t *testing.T) {
	since, until := window("2026-09-03", 14)
	p, err := Build(Input{Today: "2026-09-03", Since: since, Until: until})
	if err != nil || len(p.Terms) != 0 || p.Days != 14 {
		t.Errorf("空: err=%v %+v", err, p)
	}
	if !strings.Contains(string(p.Marshal(0)), "語 0") {
		t.Error("空の表")
	}
	if _, err := Build(Input{Today: "2026/09/03", Since: since, Until: until}); err == nil {
		t.Error("日付の誤りがエラーにならない")
	}
}

func TestParseKeep(t *testing.T) {
	text := "# 2026-08\n\n- [見出し A](https://x/a)\n- 見出しではない\n- [見出し B](https://x/b) 補足\r\n"
	got := ParseKeep("2026-08", text)
	want := []Keep{{"2026-08", "見出し A"}, {"2026-08", "見出し B"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%v", got)
	}
}

// 補助ファイル(extra)の 1 行は、語の規則に通してから語にする(記事の側と同じ規則でないと照合できない)。
// 1 行から複数語が出ても材料の数は 1 行。規則で語にならない行は行そのものを小文字で語にする。
func TestBuild_ExtraLines(t *testing.T) {
	since, until := window("2026-09-03", 14)
	p, err := Build(Input{Today: "2026-09-03", Since: since, Until: until, Extra: []string{"Rust の パース", "# コメント", "", "  ai  "}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Sources[SourceExtra] != 2 {
		t.Errorf("材料は行数(コメントと空行は読まない): %v", p.Sources)
	}
	var got []string
	for _, tm := range p.Terms {
		got = append(got, tm.Word)
	}
	// extra だけなので重みは全部 1。同点は語の昇順
	want := []string{"ai", "rust", "パース"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("語: %v (want %v)", got, want)
	}
}

// BOM 付きのファイル(Windows の編集で付く)でも 1 行目を落とさない。入力は BOM 除去してから解析する(決定 2026-08-07)。
func TestBOM(t *testing.T) {
	got := ParseKeep("2026-08", "\uFEFF- [見出し](https://x/a)\n")
	if want := []Keep{{"2026-08", "見出し"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("keep の 1 行目: %v", got)
	}
	since, until := window("2026-09-03", 14)
	p, err := Build(Input{Today: "2026-09-03", Since: since, Until: until, Extra: []string{"\uFEFFRust"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Terms) != 1 || p.Terms[0].Word != "rust" {
		t.Errorf("補助の 1 行目: %+v", p.Terms)
	}
}

// 窓は呼び出し側が渡した時刻で切る。Build の中で日付から作ると UTC の 0 時になり、
// retro(ローカルの 0 時)と別の日を指した(設計レビュー 2026-09-06 M3b)。
func TestBuild_窓は渡された時刻で切る(t *testing.T) {
	jst := time.FixedZone("JST", 9*60*60)
	// JST の 2026-09-03 0 時 = UTC の 2026-09-02 15 時。
	// この発話は UTC の日付では 09-02 だが、JST では 09-03 なので窓に入る
	turn := time.Date(2026, 9, 2, 16, 0, 0, 0, time.UTC)
	ss := []sessions.Session{{ID: "s", Turns: []sessions.Turn{{Role: sessions.User, Time: turn, Text: "ゴルーチン"}}}}

	since := time.Date(2026, 9, 3, 0, 0, 0, 0, jst)
	in := Input{Today: "2026-09-03", Days: 1, Since: since, Until: since.AddDate(0, 0, 1), Sessions: ss}
	p, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	if p.Weight("ゴルーチン") == 0 {
		t.Error("JST の窓に入るはずの発話が入っていない")
	}

	// UTC の 0 時を起点にすると同じ発話が窓の外になる(これが直した食い違い)
	inUTC := in
	inUTC.Since = time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	inUTC.Until = inUTC.Since.AddDate(0, 0, 1)
	p, err = Build(inUTC)
	if err != nil {
		t.Fatal(err)
	}
	if p.Weight("ゴルーチン") != 0 {
		t.Error("UTC の窓では外れるはず(テストの前提が崩れている)")
	}

	// Until はその時刻を含まない
	inEdge := in
	inEdge.Until = turn
	p, _ = Build(inEdge)
	if p.Weight("ゴルーチン") != 0 {
		t.Error("Until ちょうどの発話を含めている")
	}
}

func TestBuild_窓が無ければエラー(t *testing.T) {
	if _, err := Build(Input{Today: "2026-09-03"}); err == nil {
		t.Error("Since と Until が無いのにエラーにならない")
	}
}
