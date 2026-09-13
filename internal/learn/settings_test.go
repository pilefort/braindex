package learn

import (
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/sessions"
)

func TestSettings_WithDefaults(t *testing.T) {
	want := Settings{MinSessions: 3, MaxSessionRatio: 0.1, MinCorrections: 2, BoilerplateSessions: 3}
	if got := (Settings{}).WithDefaults(); got != want {
		t.Fatalf("既定: got=%+v want=%+v", got, want)
	}
	explicit := Settings{MinSessions: 5, MaxSessionRatio: 0.25, MinCorrections: 4, BoilerplateSessions: 6}
	if got := explicit.WithDefaults(); got != explicit {
		t.Fatalf("指定値: got=%+v", got)
	}
	want.MinSessions = 5
	if got := (Settings{MinSessions: 5}).WithDefaults(); got != want {
		t.Fatalf("一部指定: got=%+v", got)
	}
	o := (Options{}).withDefaults()
	if o.MinSessions != DefaultMinSessions || o.MaxSessionRatio != DefaultMaxSessionRatio || o.MinCorrections != DefaultMinCorrections || o.BoilerplateSessions != DefaultBoilerplateSessions {
		t.Fatalf("Options の既定が異なる: %+v", o)
	}
}

func TestSettings_Validate(t *testing.T) {
	for _, s := range []Settings{{}, {MinSessions: 1, MaxSessionRatio: 1, MinCorrections: 1, BoilerplateSessions: 1}, {MaxSessionRatio: 0.25}} {
		if err := s.Validate(); err != nil {
			t.Errorf("有効な設定 %+v: %v", s, err)
		}
	}
	for _, tt := range []struct {
		key string
		s   Settings
	}{
		{"min_sessions", Settings{MinSessions: -1}},
		{"max_session_ratio", Settings{MaxSessionRatio: -0.1}},
		{"max_session_ratio", Settings{MaxSessionRatio: 1.01}},
		{"min_corrections", Settings{MinCorrections: -1}},
		{"boilerplate_sessions", Settings{BoilerplateSessions: -1}},
	} {
		t.Run(tt.key, func(t *testing.T) {
			for _, s := range []Settings{tt.s, tt.s.WithDefaults()} {
				if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "設定 learn."+tt.key+":") {
					t.Errorf("設定の誤りにキー名が必要: %v", err)
				}
			}
		})
	}
}

func TestBuild_BoilerplateThresholdOverridesReadMarks(t *testing.T) {
	in := fixture()
	text := "違う。kubernetes ingress gateway の設定について、もう一度手順を詳しく説明してほしい"
	for i := range in.Sessions {
		in.Sessions[i].Turns = []sessions.Turn{human(1, at(i+1, 9), text)}
	}
	sessions.MarkBoilerplate(in.Sessions, 3)
	if got := Build(in).Sources["corrections"]; got != 0 {
		t.Fatalf("既定で定型を除く: %d", got)
	}
	in.Options.BoilerplateSessions = 4
	if got := Build(in).Sources["corrections"]; got != 3 {
		t.Fatalf("閾値を上げたら訂正を数える: %d", got)
	}
}
