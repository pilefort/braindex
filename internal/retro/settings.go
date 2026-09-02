package retro

import (
	"path/filepath"
	"strings"
)

// Settings は braindex.json の retro 節。
type Settings struct {
	SessionsDir     string  `json:"sessions_dir"`     // セッションログの置き場。空なら ~/.claude/projects(sessions.DefaultDir)
	WindowDays      int     `json:"window_days"`      // 直近何日を窓にするか(check の既定)。既定 14
	Threshold       float64 `json:"threshold"`        // 訂正率の閾値(0〜1)。既定 0.08
	PositionBins    string  `json:"position_bins"`    // セッション内位置の区間。既定 "1-3,4-10,11-30,31-"
	Dictionary      string  `json:"dictionary"`       // 訂正辞書のファイル。埋め込みの既定辞書の代わりに使う。空なら既定辞書
	DictionaryExtra string  `json:"dictionary_extra"` // 追加の辞書ファイル。既定辞書(か dictionary)に足す
}

// 既定値。
const (
	DefaultWindowDays   = 14
	DefaultThreshold    = 0.08 // 2026-09-03 の決定。完成後に試用して見直す(較正の実測は直近 14 日で 8.3%)
	DefaultPositionBins = "1-3,4-10,11-30,31-" // 2026-09-03 の決定。最初の 3 発話(全発話の 54%・率高め)を分ける
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
	return s
}

// ExpandHome は先頭の "~"("~" だけ・"~/"・"~\")を home に置き換える。それ以外はそのまま。
func ExpandHome(p, home string) string {
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
