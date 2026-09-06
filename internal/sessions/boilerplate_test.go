package sessions

import (
	"strings"
	"testing"
)

func userTurn(text string) Turn { return Turn{Role: User, Text: text} }

// 定型(機械が流し込んだ指示)の判定。判定を retro・news・learn で共有するために読み取り層に置いた
// (設計レビュー 2026-09-06 M11)。
func TestBoilerplateKey(t *testing.T) {
	long := strings.Repeat("あ", 40)
	cases := []struct {
		desc, text string
		ok         bool
		want       string
	}{
		{"39 文字は判定にかけない", strings.Repeat("あ", 39), false, ""},
		{"40 文字ちょうどは判定にかける", long, true, long},
		{"120 文字で切る", strings.Repeat("い", 200), true, strings.Repeat("い", 120)},
		{"空白は 1 つに畳む", "定期実行の  指示 です" + strings.Repeat("あ", 40), true, "定期実行の 指示 です" + strings.Repeat("あ", 40)},
		{"短い訂正は定型にしない", "違う", false, ""},
	}
	for _, c := range cases {
		got, ok := BoilerplateKey(c.text)
		if ok != c.ok || got != c.want {
			t.Errorf("BoilerplateKey[%s]: want=(%q,%v) got=(%q,%v)", c.desc, c.want, c.ok, got, ok)
		}
	}
}

func TestMarkBoilerplate(t *testing.T) {
	tmpl := "定期実行のプロンプト。今日のニュースを選別して関心度を付けてください。" + strings.Repeat("あ", 20)
	other := "こちらは別の長い発話で、1 セッションにしか出てこないものです。" + strings.Repeat("い", 20)
	ss := []Session{
		{ID: "s1", Turns: []Turn{userTurn(tmpl), userTurn(other)}},
		{ID: "s2", Turns: []Turn{userTurn(tmpl)}},
		{ID: "s3", Turns: []Turn{userTurn(tmpl), {Role: Assistant, Text: tmpl}}},
	}
	if n := MarkBoilerplate(ss, 3); n != 3 {
		t.Errorf("印を付けた数: %d", n)
	}
	if !ss[0].Turns[0].Boilerplate || !ss[1].Turns[0].Boilerplate || !ss[2].Turns[0].Boilerplate {
		t.Error("3 セッションに出る発話に印が付いていない")
	}
	if ss[0].Turns[1].Boilerplate {
		t.Error("1 セッションにしか出ない発話に印が付いた")
	}
	if ss[2].Turns[1].Boilerplate {
		t.Error("アシスタントの発話に印が付いた")
	}

	// 2 セッションでは足りない(既定 3)
	ss2 := []Session{{ID: "a", Turns: []Turn{userTurn(tmpl)}}, {ID: "b", Turns: []Turn{userTurn(tmpl)}}}
	if n := MarkBoilerplate(ss2, 0); n != 0 {
		t.Errorf("2 セッションで定型にした: %d", n)
	}
	// 冪等: 2 回掛けても同じ
	MarkBoilerplate(ss, 3)
	if !ss[0].Turns[0].Boilerplate || ss[0].Turns[1].Boilerplate {
		t.Error("2 回掛けると結果が変わる")
	}
}

// braindex 自身が claude -p で流し込んだプロンプトは人の発話として数えない。
func TestExcludeReason_braindexTool(t *testing.T) {
	if got := ExcludeReason("[braindex-news]\nあなたは選別係。"); got != "braindex-tool" {
		t.Errorf("got=%q", got)
	}
	if got := ExcludeReason("braindex-news の話をしたい"); got != "" {
		t.Errorf("本文中の語まで除いた: %q", got)
	}
}
