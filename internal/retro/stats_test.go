package retro

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pilefort/braindex/internal/sessions"
)

func at(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("時刻の書き間違い %q: %v", s, err)
	}
	return tm
}

func user(t *testing.T, idx int, when, text string) sessions.Turn {
	return sessions.Turn{Role: sessions.User, Index: idx, Time: at(t, when), Text: text}
}

// session は reader と同じ形のセッションを組む(UserTurns と、時刻のある最初・最後の発話の Start/End を埋める)。
func session(id, project string, turns ...sessions.Turn) sessions.Session {
	s := sessions.Session{ID: id, Project: project, Turns: turns}
	for _, tn := range turns {
		if tn.Role == sessions.User {
			s.UserTurns++
		}
		if !tn.Time.IsZero() {
			if s.Start.IsZero() {
				s.Start = tn.Time
			}
			s.End = tn.Time
		}
	}
	return s
}

// 架空の 4 セッション。A: 3 発話(訂正 1)・B: 2 発話(訂正 1)・C: 12 発話(訂正 3。Index 2・11・12)・D: 時刻なし 1 発話(訂正 1)
func fixture(t *testing.T) []sessions.Session {
	a := session("a1", "/home/someone/repo-a",
		user(t, 1, "2026-08-03T10:00:00Z", "索引を作って"),
		sessions.Turn{Role: sessions.Assistant, Time: at(t, "2026-08-03T10:01:00Z"), Text: "違う話ですが作ります"}, // アシスタントは数えない
		user(t, 2, "2026-08-03T10:05:00Z", "違う、docs だけ"),
		user(t, 3, "2026-08-12T09:00:00Z", "ありがとう"),
	)
	b := session("b1", "/home/someone/repo-b",
		user(t, 1, "2026-08-12T12:00:00Z", "勝手に消さないで"),
		user(t, 2, "2026-08-20T12:00:00Z", "OK"),
	)
	var cturns []sessions.Turn
	for i := 1; i <= 12; i++ {
		text := "次をやって"
		if i == 2 || i >= 11 {
			text = "そうじゃなくて"
		}
		cturns = append(cturns, user(t, i, fmt.Sprintf("2026-08-25T%02d:00:00Z", i), text))
	}
	c := session("c1", "/work/other", cturns...)
	d := session("d1", "/work/other", sessions.Turn{Role: sessions.User, Index: 1, Text: "時刻なし。違う"})
	return []sessions.Session{a, b, c, d}
}

func tally(items []Item) (n, hits int) {
	for _, it := range items {
		n++
		if it.Hit {
			hits++
		}
	}
	return n, hits
}

func TestJudge(t *testing.T) {
	ss := fixture(t)
	dict := Corrections()

	items := Judge(ss, Window{}, dict)
	if n, h := tally(items); n != 18 || h != 6 {
		t.Errorf("無制限: want=18/6 got=%d/%d", n, h)
	}
	first := Item{Time: at(t, "2026-08-03T10:00:00Z"), Project: "/home/someone/repo-a", Session: "a1", Index: 1, Hit: false}
	if len(items) == 0 {
		t.Fatal("Item が 1 つも無い")
	}
	if items[0] != first {
		t.Errorf("Item の中身: want=%+v got=%+v", first, items[0])
	}
	// 時刻なしの発話は、窓が無制限のときだけ入る
	if n, h := tally(Judge(ss, Window{Since: at(t, "2026-08-10T00:00:00Z")}, dict)); n != 15 || h != 4 {
		t.Errorf("Since: want=15/4 got=%d/%d", n, h)
	}
	if n, h := tally(Judge(ss, Window{Since: at(t, "2026-08-10T00:00:00Z"), Until: at(t, "2026-08-15T00:00:00Z")}, dict)); n != 2 || h != 1 {
		t.Errorf("Since+Until: want=2/1 got=%d/%d", n, h)
	}
	// Until はその時刻を含まない
	if n, _ := tally(Judge(ss, Window{Until: at(t, "2026-08-03T10:05:00Z")}, dict)); n != 1 {
		t.Errorf("Until は含まない: want=1 got=%d", n)
	}
	if got := Judge(nil, Window{}, dict); got != nil {
		t.Errorf("空: got=%+v", got)
	}
}

func TestTotalAndRate(t *testing.T) {
	items := Judge(fixture(t), Window{}, Corrections())
	total := Total(items)
	if total.Key != "合計" || total.Utterances != 18 || total.Corrections != 6 {
		t.Errorf("Total: got=%+v", total)
	}
	if r := total.Rate(); r < 0.333 || r > 0.334 {
		t.Errorf("Rate: got=%v", r)
	}
	if r := (Count{}).Rate(); r != 0 {
		t.Errorf("発話 0 の率は 0: got=%v", r)
	}
}

func TestByProject(t *testing.T) {
	items := Judge(fixture(t), Window{}, Corrections())
	got := ByProject(items, "/home/someone")
	want := []Count{
		{Key: "/work/other", Utterances: 13, Corrections: 4},
		{Key: "~/repo-a", Utterances: 3, Corrections: 1},
		{Key: "~/repo-b", Utterances: 2, Corrections: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ByProject:\n want=%+v\n  got=%+v", want, got)
	}
}

func TestByWeek(t *testing.T) {
	items := Judge(fixture(t), Window{}, Corrections())
	got := ByWeek(items, time.UTC)
	want := []Count{
		{Key: "2026-W32", Utterances: 2, Corrections: 1},
		{Key: "2026-W33", Utterances: 2, Corrections: 1},
		{Key: "2026-W34", Utterances: 1, Corrections: 0},
		{Key: "2026-W35", Utterances: 12, Corrections: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ByWeek(時刻なしは入れない):\n want=%+v\n  got=%+v", want, got)
	}
	// 週の境界はタイムゾーンで決まる。UTC の日曜 23:00 は +09:00 では月曜
	jst := time.FixedZone("JST", 9*3600)
	one := []Item{{Time: at(t, "2026-08-09T23:00:00Z"), Project: "/x", Session: "s", Index: 1}}
	if got := ByWeek(one, time.UTC); len(got) != 1 || got[0].Key != "2026-W32" {
		t.Errorf("UTC: got=%+v", got)
	}
	if got := ByWeek(one, jst); len(got) != 1 || got[0].Key != "2026-W33" {
		t.Errorf("JST: got=%+v", got)
	}
}

func TestParseBins(t *testing.T) {
	got, err := ParseBins(" 1-10 , 11-30,31- ")
	if err != nil {
		t.Fatal(err)
	}
	want := []Bin{{Lo: 1, Hi: 10, Label: "1-10"}, {Lo: 11, Hi: 30, Label: "11-30"}, {Lo: 31, Hi: 0, Label: "31-"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseBins: want=%+v got=%+v", want, got)
	}
	for _, bad := range []string{"", "0-5", "10-5", "a-b", "5", "1-10,", "-10"} {
		if _, err := ParseBins(bad); err == nil {
			t.Errorf("ParseBins(%q) はエラーにする", bad)
		}
	}
}

func TestByPosition(t *testing.T) {
	items := Judge(fixture(t), Window{}, Corrections())
	bins, _ := ParseBins("1-10,11-30,31-")
	got := ByPosition(items, bins)
	want := []Count{
		{Key: "1-10", Utterances: 16, Corrections: 4},
		{Key: "11-30", Utterances: 2, Corrections: 2},
		{Key: "31-", Utterances: 0, Corrections: 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ByPosition:\n want=%+v\n  got=%+v", want, got)
	}
}

func TestRender(t *testing.T) {
	rows := []Count{{Key: "~/repo-a", Utterances: 3, Corrections: 1}, {Key: "~/repo-b", Utterances: 2, Corrections: 1}, {Key: "31-", Utterances: 0, Corrections: 0}}
	got := Render("プロジェクト", rows, Count{Key: "合計", Utterances: 5, Corrections: 2})
	want := strings.Join([]string{
		"| プロジェクト | 発話 | 訂正 | 率 |",
		"|---|---:|---:|---:|",
		"| ~/repo-a | 3 | 1 | 33.3% |",
		"| ~/repo-b | 2 | 1 | 50.0% |",
		"| 31- | 0 | 0 | - |",
		"| 合計 | 5 | 2 | 40.0% |",
		"",
	}, "\n")
	if got != want {
		t.Errorf("Render:\n want=%q\n  got=%q", want, got)
	}
	// 区分なし(合計だけ)
	if got := Render("区分", nil, Count{Key: "合計", Utterances: 1, Corrections: 1}); !strings.HasSuffix(got, "| 合計 | 1 | 1 | 100.0% |\n") {
		t.Errorf("合計だけ: got=%q", got)
	}
}

func TestRecent(t *testing.T) {
	now := at(t, "2026-09-03T15:30:00Z")
	if w := Recent(now, 14, time.UTC); !w.Since.Equal(at(t, "2026-08-20T00:00:00Z")) || !w.Until.IsZero() {
		t.Errorf("UTC: got=%+v", w)
	}
	// +09:00 では 2026-09-03T01:00Z は 10:00 なので、その日の 0 時(前日 15:00Z)から 14 日前
	jst := time.FixedZone("JST", 9*3600)
	if w := Recent(at(t, "2026-09-03T01:00:00Z"), 14, jst); !w.Since.Equal(at(t, "2026-08-19T15:00:00Z")) {
		t.Errorf("JST: got=%v", w.Since.UTC())
	}
	if w := Recent(now, 0, time.UTC); !w.Since.IsZero() {
		t.Errorf("0 日は無制限: got=%+v", w)
	}
}

// 閾値は床として残しつつ、基準期間から 2SE を超えて上振れたときだけ鳴らす(決定 2026-09-06)。
// 閾値だけだと、その人の平常運転が閾値の上にある間は毎回鳴り続けて合図の意味が消える。
func TestCompare(t *testing.T) {
	c := func(n, hit int) Count { return Count{Utterances: n, Corrections: hit} }
	cases := []struct {
		desc             string
		recent, baseline Count
		threshold        float64
		want             Verdict
	}{
		{"閾値以下なら基準を見るまでもない", c(100, 5), c(500, 40), 0.08, Below},
		{"閾値ちょうどは超えない", c(100, 8), c(500, 40), 0.08, Below},
		{"閾値超えだが基準と同水準(2SE≈5.4pt の中)", c(100, 9), c(500, 40), 0.08, SameAsBaseline},
		{"閾値も基準も超えた", c(100, 15), c(500, 40), 0.08, Exceed},
		{"基準の発話が少なければ閾値だけで判定", c(100, 9), c(20, 2), 0.08, Exceed},
		{"基準の発話がちょうど 50 なら使う", c(100, 9), c(50, 4), 0.08, SameAsBaseline},
		{"基準を使わない(発話 0)なら閾値だけ", c(100, 9), Count{}, 0.08, Exceed},
		{"基準の率が 0 なら差が正で鳴る", c(100, 9), c(500, 0), 0.08, Exceed},
		{"直近に発話が無ければ鳴らさない", Count{}, c(500, 40), 0.08, Below},
	}
	for _, x := range cases {
		if got := Compare(x.recent, x.baseline, x.threshold); got != x.want {
			t.Errorf("Compare[%s]: want=%v got=%v", x.desc, x.want, got)
		}
	}
}

func TestBaseline(t *testing.T) {
	loc := time.UTC
	recent := Recent(time.Date(2026, 9, 1, 10, 0, 0, 0, loc), 14, loc)
	b := Baseline(recent, 8)
	if want := time.Date(2026, 6, 23, 0, 0, 0, 0, loc); !b.Since.Equal(want) { // 2026-08-18(窓の起点)の 56 日前
		t.Errorf("基準の起点: want=%v got=%v", want, b.Since)
	}
	if !b.Until.Equal(recent.Since) {
		t.Errorf("基準の終端は窓の起点: want=%v got=%v", recent.Since, b.Until)
	}
	// 窓と基準は重ならない: 窓の起点ちょうどの発話は窓の側
	if b.Contains(recent.Since) || !recent.Contains(recent.Since) {
		t.Error("窓の起点が両方に入るか、どちらにも入らない")
	}
	if got := Baseline(recent, 0); got != (Window{}) {
		t.Errorf("0 週は基準なし: %+v", got)
	}
	if got := Baseline(Window{}, 8); got != (Window{}) {
		t.Errorf("全期間の窓には基準を作れない: %+v", got)
	}
}
