package learn

import "fmt"

// 学習候補の閾値の既定値。
const (
	DefaultMinSessions         = 3
	DefaultMaxSessionRatio     = 0.1
	DefaultMinCorrections      = 2
	DefaultBoilerplateSessions = 3
)

// Settings は braindex.json の learn 節。省略・0 は既定値。
type Settings struct {
	MinSessions         int     `json:"min_sessions"`
	MaxSessionRatio     float64 `json:"max_session_ratio"`
	MinCorrections      int     `json:"min_corrections"`
	BoilerplateSessions int     `json:"boilerplate_sessions"`
}

// WithDefaults は 0 の項目を既定値で埋めた複製を返す。
func (s Settings) WithDefaults() Settings {
	if s.MinSessions == 0 {
		s.MinSessions = DefaultMinSessions
	}
	if s.MaxSessionRatio == 0 {
		s.MaxSessionRatio = DefaultMaxSessionRatio
	}
	if s.MinCorrections == 0 {
		s.MinCorrections = DefaultMinCorrections
	}
	if s.BoilerplateSessions == 0 {
		s.BoilerplateSessions = DefaultBoilerplateSessions
	}
	return s
}

// Validate は範囲外の値を設定の誤りとして返す。
func (s Settings) Validate() error {
	if s.MinSessions < 0 {
		return fmt.Errorf("設定 learn.min_sessions: 0 以上(0 は既定 %d): %d", DefaultMinSessions, s.MinSessions)
	}
	if !(s.MaxSessionRatio >= 0 && s.MaxSessionRatio <= 1) {
		return fmt.Errorf("設定 learn.max_session_ratio: 0〜1 の範囲(0 は既定 %g): %g", DefaultMaxSessionRatio, s.MaxSessionRatio)
	}
	if s.MinCorrections < 0 {
		return fmt.Errorf("設定 learn.min_corrections: 0 以上(0 は既定 %d): %d", DefaultMinCorrections, s.MinCorrections)
	}
	if s.BoilerplateSessions < 0 {
		return fmt.Errorf("設定 learn.boilerplate_sessions: 0 以上(0 は既定 %d): %d", DefaultBoilerplateSessions, s.BoilerplateSessions)
	}
	return nil
}
