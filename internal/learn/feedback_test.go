package learn

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// 回答 4 件: 既知・不要・期限前の後で・期限切れの後で。候補(candidates)と同じ語。
func answered() Feedbacks {
	var fb Feedbacks
	fb.Set(Feedback{Section: SectionReadNotWritten, Word: "webassembly", Answer: Later, Date: "2026-08-20", Until: "2026-09-01"})
	fb.Set(Feedback{Section: SectionUnsettled, Word: "kubernetes", Answer: Known, Date: "2026-09-01"})
	fb.Set(Feedback{Section: SectionStumbles, Word: "ingress", Answer: Unwanted, Date: "2026-09-02"})
	fb.Set(Feedback{Section: SectionUnsettled, Word: "istio", Answer: Later, Date: "2026-09-03", Until: "2026-09-10"})
	return fb
}

func ids(fb Feedbacks) string {
	var out []string
	for _, f := range fb.Answers {
		out = append(out, string(f.Section)+":"+f.Word)
	}
	return strings.Join(out, " ")
}

// 回答は節の順 → 語の昇順に並び、同じ内容からは同じバイト列になる。読み戻すと同じ回答になる。
func TestFeedbacks_JSONの往復と決定性(t *testing.T) {
	fb := answered()
	if got, want := ids(fb), "unsettled:istio unsettled:kubernetes stumbles:ingress read_not_written:webassembly"; got != want {
		t.Errorf("並び: %q", got)
	}
	a := fb.JSON()
	if !bytes.Equal(a, fb.JSON()) {
		t.Error("同じ内容で JSON が違う")
	}
	if !strings.HasPrefix(string(a), "{\n  \"version\": 1,\n  \"answers\": [\n") || !strings.HasSuffix(string(a), "}\n") || bytes.Contains(a, []byte("\r")) {
		t.Errorf("JSON の形:\n%s", a)
	}
	back, err := ParseFeedbacks(a)
	if err != nil {
		t.Fatal(err)
	}
	if back.Version != FeedbackVersion || !reflect.DeepEqual(back.Answers, fb.Answers) {
		t.Errorf("読み戻しが違う:\n%+v\n%+v", back, fb)
	}
	if !bytes.Equal(back.JSON(), a) {
		t.Error("読み戻した回答の JSON が元と違う")
	}
	// until は後で だけに出る
	if strings.Count(string(a), `"until"`) != 2 {
		t.Errorf("until の数が違う:\n%s", a)
	}
}

// 同じ節と語に回答し直すと置き換わり(前の until は残らない)、解除は有無を返す。
func TestFeedbacks_SetとClear(t *testing.T) {
	fb := answered()
	fb.Set(Feedback{Section: SectionUnsettled, Word: "istio", Answer: Known, Date: "2026-09-04"})
	if f, ok := fb.Find(SectionUnsettled, "istio"); !ok || f.Answer != Known || f.Until != "" || f.Date != "2026-09-04" {
		t.Errorf("置き換え: %+v ok=%v", f, ok)
	}
	if len(fb.Answers) != 4 {
		t.Errorf("件数が増えた: %d", len(fb.Answers))
	}
	if !fb.Clear(SectionUnsettled, "istio") || fb.Clear(SectionUnsettled, "istio") {
		t.Error("解除の戻り値")
	}
	if _, ok := fb.Find(SectionUnsettled, "istio"); ok {
		t.Error("解除したのに残っている")
	}
	// 別の節の同じ語は別の回答
	if fb.Clear(SectionUnsettled, "ingress") {
		t.Error("stumbles の回答を unsettled で消した")
	}
	if _, ok := fb.Find(SectionStumbles, "ingress"); !ok {
		t.Error("stumbles の回答が消えた")
	}
}

// 壊れた記録は読み飛ばさずエラーにする(黙って読むと次の保存で回答を失う)。
func TestParseFeedbacks_壊れた記録(t *testing.T) {
	one := func(fields string) string { return `{"version":1,"answers":[{` + fields + `}]}` }
	cases := []struct{ in, want string }{
		{"", "空"},
		{"   \n", "空"},
		{`{"version":2,"answers":[]}`, "版 2 は読めない"},
		{`{"version":1,"answers":[],"extra":1}`, "unknown field"},
		{`{"version":1,"answers":[]} x`, "余分な内容"},
		{one(`"section":"nope","word":"x","answer":"known","date":"2026-09-01"`), `節 "nope" は無い`},
		{one(`"section":"unsettled","word":"","answer":"known","date":"2026-09-01"`), "1 語でない"},
		{one(`"section":"unsettled","word":"a b","answer":"known","date":"2026-09-01"`), "1 語でない"},
		{one(`"section":"unsettled","word":"x","answer":"maybe","date":"2026-09-01"`), `answer "maybe" は無い`},
		{one(`"section":"unsettled","word":"x","answer":"known","date":"2026/09/01"`), "date は YYYY-MM-DD"},
		{one(`"section":"unsettled","word":"x","answer":"later","date":"2026-09-01"`), "later には until"},
		{one(`"section":"unsettled","word":"x","answer":"known","date":"2026-09-01","until":"2026-09-02"`), "until は later だけ"},
		{one(`"section":"unsettled","word":"x","answer":"later","date":"2026-09-01","until":"明日"`), "until は YYYY-MM-DD"},
		{`{"version":1,"answers":[{"section":"unsettled","word":"x","answer":"known","date":"2026-09-01"},{"section":"unsettled","word":"x","answer":"unwanted","date":"2026-09-02"}]}`, `"x" が 2 回ある`},
	}
	for _, c := range cases {
		_, err := ParseFeedbacks([]byte(c.in))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err=%v want %q", c.in, err, c.want)
		}
	}
	// answers が無い・null でも読めて、空のスライスになる
	for _, in := range []string{`{"version":1}`, `{"version":1,"answers":null}`} {
		fb, err := ParseFeedbacks([]byte(in))
		if err != nil || fb.Answers == nil || len(fb.Answers) != 0 {
			t.Errorf("%s: err=%v %+v", in, err, fb)
		}
	}
}

// 効いている回答の候補は節から外れて要約に積まれ、「後で」の期限が来た候補は残って回答日が付く。
func TestApply_回答済みは伏せ期限切れは戻す(t *testing.T) {
	r := candidates()
	Apply(&r, answered(), "2026-09-05")
	if len(r.Unsettled) != 0 || len(r.Stumbles) != 0 {
		t.Errorf("伏せていない: unsettled=%+v stumbles=%+v", r.Unsettled, r.Stumbles)
	}
	if len(r.ReadNotWritten) != 1 || r.ReadNotWritten[0].Word != "webassembly" || r.ReadNotWritten[0].Deferred != "2026-08-20" || r.ReadNotWritten[0].Keeps != 2 {
		t.Errorf("期限切れの候補: %+v", r.ReadNotWritten)
	}
	s := r.Feedback
	if s == nil {
		t.Fatal("要約が無い")
	}
	var got []string
	for _, h := range s.Hidden {
		got = append(got, string(h.Section)+":"+h.Item.Word+"="+string(h.Answer))
	}
	if want := "unsettled:kubernetes=known unsettled:istio=later stumbles:ingress=unwanted"; strings.Join(got, " ") != want {
		t.Errorf("伏せた候補: %q", strings.Join(got, " "))
	}
	if s.Known != 1 || s.Unwanted != 1 || s.Later != 1 || s.Expired != 1 {
		t.Errorf("数: %+v", s)
	}
	if h := s.Hidden[0]; h.Item.Sessions != 4 || h.Date != "2026-09-01" || h.Until != "" {
		t.Errorf("伏せた候補の中身: %+v", h)
	}
	if h := s.Hidden[1]; h.Until != "2026-09-10" {
		t.Errorf("後で の until: %+v", h)
	}

	md := string(r.Marshal())
	for _, want := range []string{
		"回答済み: 伏せた 3 件（既知 1・不要 1・後で 1）・「後で」の期限が来て再提示 1 件\n",
		"## 触れているが索引に無い（0）",
		"- webassembly — keep 2 件／後で（2026-08-20 に回答）の期限が来たので再提示\n",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("出力に %q が無い:\n%s", want, md)
		}
	}
	for _, no := range []string{"- kubernetes", "- istio", "- ingress"} {
		if strings.Contains(md, no) {
			t.Errorf("伏せた語 %q が出力にある:\n%s", no, md)
		}
	}
	if !bytes.Equal(r.Marshal(), r.Marshal()) {
		t.Error("同じ材料で出力が違う")
	}
	js, err := r.JSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"feedback": {`, `"hidden": [`, `"answer": "known"`, `"deferred": "2026-08-20"`, `"expired": 1`} {
		if !strings.Contains(string(js), want) {
			t.Errorf("JSON に %q が無い:\n%s", want, js)
		}
	}
}

// 「後で」は再提示日の前日まで伏せ、当日から出す。
func TestApply_後では再提示日の前日まで伏せる(t *testing.T) {
	var fb Feedbacks
	fb.Set(Feedback{Section: SectionUnsettled, Word: "istio", Answer: Later, Date: "2026-09-03", Until: "2026-09-10"})
	r := candidates()
	Apply(&r, fb, "2026-09-09")
	if len(r.Unsettled) != 1 || r.Unsettled[0].Word != "kubernetes" || r.Feedback.Later != 1 {
		t.Errorf("前日: %+v %+v", r.Unsettled, r.Feedback)
	}
	r = candidates()
	Apply(&r, fb, "2026-09-10")
	if len(r.Unsettled) != 2 || r.Unsettled[1].Word != "istio" || r.Unsettled[1].Deferred != "2026-09-03" || r.Feedback.Expired != 1 || len(r.Feedback.Hidden) != 0 {
		t.Errorf("当日: %+v %+v", r.Unsettled, r.Feedback)
	}
}

// 回答は節と語の組。同じ語でも別の節の候補は伏せない。伏せるものが無ければ「回答済み:」の行は出ない。
func TestApply_同じ語でも節が違えば伏せない(t *testing.T) {
	var fb Feedbacks
	fb.Set(Feedback{Section: SectionUnsettled, Word: "ingress", Answer: Unwanted, Date: "2026-09-01"})
	r := candidates()
	Apply(&r, fb, "2026-09-05")
	if len(r.Stumbles) != 1 || r.Stumbles[0].Word != "ingress" || len(r.Feedback.Hidden) != 0 {
		t.Errorf("stumbles の ingress を伏せた: %+v %+v", r.Stumbles, r.Feedback)
	}
	if md := string(r.Marshal()); strings.Contains(md, "回答済み") {
		t.Errorf("伏せていないのに回答済みの行がある:\n%s", md)
	}
}

// 件数は伏せた後に切る。先に切ると伏せた分だけ欠ける。
func TestApply_件数は伏せた後に切る(t *testing.T) {
	var fb Feedbacks
	fb.Set(Feedback{Section: SectionUnsettled, Word: "kubernetes", Answer: Known, Date: "2026-09-01"})
	r := candidates()
	Apply(&r, fb, "2026-09-05")
	r.Truncate(1)
	if len(r.Unsettled) != 1 || r.Unsettled[0].Word != "istio" {
		t.Errorf("伏せた後の 1 件: %+v", r.Unsettled)
	}
	r2 := candidates()
	r2.Truncate(0)
	if len(r2.Unsettled) != 2 {
		t.Errorf("0 は全件: %+v", r2.Unsettled)
	}
}

// 無ければ空(エラーなし)。保存は書き切ってから置き換え、読み戻すと同じ。一時ファイルは残らない。
func TestFeedbacks_保存と読み込み(t *testing.T) {
	path := filepath.Join(t.TempDir(), "work", "learn", "answers.json")
	fb, err := LoadFeedbacks(path)
	if err != nil || fb.Version != 0 || len(fb.Answers) != 0 {
		t.Fatalf("無いとき: err=%v %+v", err, fb)
	}
	fb = answered()
	if err := fb.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := LoadFeedbacks(path)
	if err != nil || !reflect.DeepEqual(back.Answers, fb.Answers) {
		t.Errorf("読み戻し: err=%v\n%+v", err, back)
	}
	b, _ := os.ReadFile(path)
	if !bytes.Equal(b, fb.JSON()) {
		t.Errorf("ファイルの中身が JSON() と違う:\n%s", b)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("一時ファイルが残っている: %d 件", len(entries))
	}
	// 壊れたファイルはパスつきのエラー
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFeedbacks(path); err == nil || !strings.Contains(err.Error(), "answers.json") {
		t.Errorf("壊れたファイル: err=%v", err)
	}
}

// 一覧は節の見出しと回答の説明。期限切れの「後で」はそう言う。
func TestFeedbacks_Markdown(t *testing.T) {
	md := string(answered().Markdown("2026-09-05"))
	for _, want := range []string{
		"# 学習候補への回答（4 件）\n\n",
		"- 触れているが索引に無い: istio — 後で（2026-09-03 に回答・2026-09-10 から再提示）\n",
		"- 触れているが索引に無い: kubernetes — 既知（2026-09-01）\n",
		"- 訂正の文脈に繰り返し出る: ingress — 不要（2026-09-02）\n",
		"- 残した記事にあるが索引に無い: webassembly — 後で（2026-08-20 に回答・2026-09-01 から再提示・期限切れ）\n",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("一覧に %q が無い:\n%s", want, md)
		}
	}
	if empty := string(Feedbacks{}.Markdown("2026-09-05")); !strings.Contains(empty, "（0 件）") || !strings.Contains(empty, "（なし）") {
		t.Errorf("空の一覧:\n%s", empty)
	}
}

func TestParseAnswerとParseSection(t *testing.T) {
	for in, want := range map[string]Answer{"known": Known, "既知": Known, "unwanted": Unwanted, "不要": Unwanted, "later": Later, "後で": Later} {
		if got, err := ParseAnswer(in); err != nil || got != want {
			t.Errorf("%s: %v %v", in, got, err)
		}
	}
	if _, err := ParseAnswer("maybe"); err == nil || !strings.Contains(err.Error(), "known=既知") {
		t.Errorf("無い回答: %v", err)
	}
	for _, s := range Sections() {
		if got, err := ParseSection(string(s)); err != nil || got != s || s.Title() == string(s) {
			t.Errorf("%s: %v %v", s, got, err)
		}
	}
	if _, err := ParseSection("触れているが索引に無い"); err == nil {
		t.Error("見出しでは節を引けない(識別子だけ)")
	}
}
