package schedule

import (
	"strings"
	"testing"
)

func TestSettings_WithDefaults(t *testing.T) {
	s := Settings{}.WithDefaults()
	if len(s.Jobs) != 2 || s.Jobs[0].Name != "review" || s.Jobs[1].Name != "retro" {
		t.Errorf("jobs を書かなければ既定の 2 本: got=%+v", s.Jobs)
	}
	// 既定は自分の検査を通る(壊れた既定を配らない)
	if err := s.Validate([]string{"review", "retro"}); err != nil {
		t.Errorf("既定のジョブが Validate を通らない: %v", err)
	}
	mine := Settings{Jobs: []Job{{Name: "x", Args: []string{"review"}, When: "daily:01:00"}}}
	if got := mine.WithDefaults(); len(got.Jobs) != 1 || got.Jobs[0].Name != "x" {
		t.Errorf("書いてあれば既定で埋めない: got=%+v", got.Jobs)
	}
}

func TestSettings_Validate(t *testing.T) {
	known := []string{"review", "retro", "news"}
	ok := []Settings{
		{},
		{Jobs: []Job{{Name: "a", Args: []string{"review"}, When: "daily:09:00"}}},
		{Jobs: []Job{
			{Name: "a-1", Args: []string{"retro", "check"}, When: "weekly:fri:23:59"},
			{Name: "b2", Args: []string{"news", "fetch", "-layer", "daily"}, When: "daily:07:00"},
		}},
	}
	for _, s := range ok {
		if err := s.Validate(known); err != nil {
			t.Errorf("Validate(%+v): 通るはず: %v", s.Jobs, err)
		}
	}
	bad := []struct {
		jobs []Job
		want string // エラー文に含まれる語
	}{
		{[]Job{{Name: "", Args: []string{"review"}, When: "daily:09:00"}}, "name"},
		{[]Job{{Name: "A", Args: []string{"review"}, When: "daily:09:00"}}, "name"},
		{[]Job{{Name: "週次", Args: []string{"review"}, When: "daily:09:00"}}, "name"},
		{[]Job{{Name: "a b", Args: []string{"review"}, When: "daily:09:00"}}, "name"},
		{[]Job{{Name: strings.Repeat("a", 33), Args: []string{"review"}, When: "daily:09:00"}}, "name"},
		{[]Job{
			{Name: "a", Args: []string{"review"}, When: "daily:09:00"},
			{Name: "a", Args: []string{"retro", "check"}, When: "daily:09:00"},
		}, "同じ名前"},
		{[]Job{{Name: "a", Args: nil, When: "daily:09:00"}}, "args"},
		{[]Job{{Name: "a", Args: []string{}, When: "daily:09:00"}}, "args"},
		// 任意のコマンドは登録しない(設定を任意コード実行の口にしない)
		{[]Job{{Name: "a", Args: []string{"rm", "-rf", "/"}, When: "daily:09:00"}}, "サブコマンドでない"},
		{[]Job{{Name: "a", Args: []string{"reviw"}, When: "daily:09:00"}}, "サブコマンドでない"},
		{[]Job{{Name: "a", Args: []string{"review"}, When: ""}}, "when"},
		{[]Job{{Name: "a", Args: []string{"review"}, When: "0 9 * * 1"}}, "when"},
	}
	for _, c := range bad {
		err := Settings{Jobs: c.jobs}.Validate(known)
		if err == nil {
			t.Errorf("Validate(%+v): エラーにする", c.jobs)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("Validate(%+v): エラー文に %q を含める: %v", c.jobs, c.want, err)
		}
	}
	// known が nil なら先頭要素を照合しない(呼び出し側がサブコマンド名を持たない場面)
	if err := (Settings{Jobs: []Job{{Name: "a", Args: []string{"なんでも"}, When: "daily:09:00"}}}).Validate(nil); err != nil {
		t.Errorf("known=nil では照合しない: %v", err)
	}
}

func TestSettings_Find(t *testing.T) {
	s := Settings{}.WithDefaults()
	if j, ok := s.Find("retro"); !ok || j.Name != "retro" {
		t.Errorf("Find(retro): got=%+v ok=%v", j, ok)
	}
	if _, ok := s.Find("なし"); ok {
		t.Error("無い名前は false")
	}
}
