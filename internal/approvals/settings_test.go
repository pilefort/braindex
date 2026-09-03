package approvals

import "testing"

// WithDefaults は空の項目だけを既定で埋め、書いてある値には触らない。
func TestSettings_WithDefaults(t *testing.T) {
	cases := []struct {
		name string
		in   Settings
		want Settings
	}{
		{
			"空なら既定で埋まる",
			Settings{},
			Settings{File: DefaultFile, Decisions: DefaultDecisions, TimeoutSec: 0},
		},
		{
			"書いてある値は上書きしない",
			Settings{File: "work/判断待ち.md", Decisions: "notes/decisions.md", TimeoutSec: 300},
			Settings{File: "work/判断待ち.md", Decisions: "notes/decisions.md", TimeoutSec: 300},
		},
		{
			"片方だけ書いてあれば残りだけ埋まる",
			Settings{Decisions: "docs/決定.md"},
			Settings{File: DefaultFile, Decisions: "docs/決定.md", TimeoutSec: 0},
		},
		{
			// 0 は「無期限」で既定と同じ意味なので、ポインタで区別する必要がない
			"timeout_sec の 0 は無期限のまま",
			Settings{File: "a.md", Decisions: "b.md", TimeoutSec: 0},
			Settings{File: "a.md", Decisions: "b.md", TimeoutSec: 0},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.in.WithDefaults(); got != c.want {
				t.Errorf("WithDefaults() = %+v, want %+v", got, c.want)
			}
		})
	}
}

// WithDefaults は受け取った値を書き換えない(複製を返す)。
func TestSettings_WithDefaults_元を書き換えない(t *testing.T) {
	s := Settings{}
	_ = s.WithDefaults()
	if s.File != "" || s.Decisions != "" {
		t.Errorf("元の Settings が書き換わった: %+v", s)
	}
}

// 負の timeout_sec は設定の誤りとして弾く(0 は無期限なので通す)。
func TestSettings_Validate(t *testing.T) {
	if err := (Settings{TimeoutSec: 0}).Validate(); err != nil {
		t.Errorf("0(無期限)を弾いた: %v", err)
	}
	if err := (Settings{TimeoutSec: 60}).Validate(); err != nil {
		t.Errorf("正の値を弾いた: %v", err)
	}
	err := (Settings{TimeoutSec: -1}).Validate()
	if err == nil {
		t.Fatal("負の timeout_sec を通した")
	}
	if !contains(err.Error(), "timeout_sec") {
		t.Errorf("どのキーの誤りか分からないメッセージ: %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
