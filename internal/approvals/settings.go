package approvals

import "fmt"

// Settings は braindex.json の approvals 節。省略可。
//
// 置き場を毎回フラグで渡さずに済むようにする。優先順位は フラグ > 設定 > 既定 で、
// フラグを明示したときはフラグが勝つ(既存のコマンドの振る舞いを変えないため)。
type Settings struct {
	File       string `json:"file"`        // 判断待ちのファイル。hub 相対か絶対。既定 work/APPROVALS.md
	Decisions  string `json:"decisions"`   // 決定の追記先。hub 相対か絶対。既定 docs/decisions.md
	TimeoutSec int    `json:"timeout_sec"` // serve が回答を待つ秒数。0 は無期限
}

// 既定値。スラッシュ区切りで持ち、使う側が filepath へ直す(設定ファイルは OS をまたぐ)。
const (
	DefaultFile      = "work/APPROVALS.md"
	DefaultDecisions = "docs/decisions.md"
)

// WithDefaults は空の項目を既定値で埋めた複製を返す。
//
// TimeoutSec をポインタにしないのは、0 が「無期限」で既定と同じ意味だから
// (news.show_min_score は 0 が「全件表示」で既定 2 と違う意味なのでポインタにした・決定 2026-09-03 → manual/news.md「決めたこと」)。
func (s Settings) WithDefaults() Settings {
	if s.File == "" {
		s.File = DefaultFile
	}
	if s.Decisions == "" {
		s.Decisions = DefaultDecisions
	}
	return s
}

// Validate は設定の誤りを返す。
func (s Settings) Validate() error {
	if s.TimeoutSec < 0 {
		return fmt.Errorf("approvals.timeout_sec は 0 以上で書く(0 は無期限): %d", s.TimeoutSec)
	}
	return nil
}
