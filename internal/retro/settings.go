package retro

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Settings は braindex.json の retro 節。
type Settings struct {
	SessionsDir     string  `json:"sessions_dir"`     // セッションログの置き場。空なら ~/.claude/projects(sessions.DefaultDir)
	WindowDays      int     `json:"window_days"`      // 直近何日を窓にするか(check の既定)。既定 14
	Threshold       float64 `json:"threshold"`        // 訂正率の閾値(0〜1)。既定 0.08
	BaselineWeeks   *int    `json:"baseline_weeks"`   // 窓の直前の何週を基準期間にするか。既定 8。0 で基準を使わない(閾値だけ)
	AllProjects     bool    `json:"all_projects"`     // true なら root の外のセッションも数える。既定 false(root 配下だけ)
	PositionBins    string  `json:"position_bins"`    // セッション内位置の区間。既定 "1-3,4-10,11-30,31-"
	Dictionary      string  `json:"dictionary"`       // 訂正辞書のファイル。埋め込みの既定辞書の代わりに使う。空なら既定辞書
	DictionaryExtra string  `json:"dictionary_extra"` // 追加の辞書ファイル。既定辞書(か dictionary)に足す
}

// 既定値。
const (
	DefaultWindowDays    = 14
	DefaultThreshold     = 0.08                 // 2026-09-03 の決定。完成後に試用して見直す(較正の実測は直近 14 日で 8.3%)
	DefaultPositionBins  = "1-3,4-10,11-30,31-" // 2026-09-03 の決定。最初の 3 発話(全発話の 54%・率高め)を分ける
	DefaultBaselineWeeks = 8                    // 2026-09-06 の決定。窓 14 日の 4 倍で、季節の波を拾わず揺れを均せる長さ
)

// WithDefaults は空・0 の項目を既定値で埋めた複製を返す。SessionsDir の既定はホームに依存するので、ここでは埋めない。
func (s Settings) WithDefaults() Settings {
	if s.WindowDays <= 0 {
		s.WindowDays = DefaultWindowDays
	}
	if s.Threshold <= 0 {
		s.Threshold = DefaultThreshold
	}
	if s.PositionBins == "" {
		s.PositionBins = DefaultPositionBins
	}
	// 0 は「基準を使わない」という指定なので既定で埋めない。書かれていない(nil)ときだけ既定を入れる
	if s.BaselineWeeks == nil {
		n := DefaultBaselineWeeks
		s.BaselineWeeks = &n
	}
	return s
}

// Baseline は基準期間の週数(WithDefaults の後に呼ぶ)。書かれていなければ既定。
func (s Settings) Baseline() int {
	if s.BaselineWeeks == nil {
		return DefaultBaselineWeeks
	}
	return *s.BaselineWeeks
}

// Validate は設定ファイルに書かれた値が範囲内かを確かめる(WithDefaults の前に呼ぶ。0 は「未指定」なので通す)。
// 範囲外は既定値に丸めず、設定の誤りとしてエラーにする(未知のキーを通さないのと同じ考え)。
func (s Settings) Validate() error {
	if s.WindowDays < 0 {
		return fmt.Errorf("設定 retro.window_days: 0 以上の整数(0 は既定の %d): %d", DefaultWindowDays, s.WindowDays)
	}
	if s.Threshold < 0 || s.Threshold > 1 {
		return fmt.Errorf("設定 retro.threshold: 0〜1 の割合(0.10 = 10%%。0 は既定の %.2f): %g", DefaultThreshold, s.Threshold)
	}
	if s.BaselineWeeks != nil && *s.BaselineWeeks < 0 {
		return fmt.Errorf("設定 retro.baseline_weeks: 0 以上の整数(0 は基準を使わない・書かなければ既定の %d): %d", DefaultBaselineWeeks, *s.BaselineWeeks)
	}
	return nil
}

// ExpandHome は先頭の "~"("~" だけ・"~/"・"~\")を home に置き換える。それ以外はそのまま。
// home が空(ホームディレクトリが分からない)なら展開せず p をそのまま返す("~/logs" がカレント相対の "logs" に化けないように)。
func ExpandHome(p, home string) string {
	if home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		return filepath.Join(home, p[2:])
	}
	return p
}

// ResolvePath は設定ファイルに書かれたパスを解決する: "~" を展開し、相対なら baseDir(設定ファイルのディレクトリ)基準にする
// (設計判断 2026-09-02「設定ファイル内の相対パスは設定ファイルのディレクトリ」)。空は空のまま。
func ResolvePath(p, baseDir, home string) string {
	if p == "" {
		return ""
	}
	if q := ExpandHome(p, home); q != p {
		return q
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(baseDir, filepath.FromSlash(p))
}
