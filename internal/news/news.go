// Package news はニュースサジェスト(braindex news)の状態と流れを担う。
//
// フィード一覧(feeds.json)を読み、各フィードを取得(internal/feed)し、既読(.seen.json)との差分を新着として
// ダイジェスト(Markdown)にする。取得は internal/feed に任せ、ここは既読・上限・失敗の扱いと出力の組み立て。
// 外へ出る通信はフィードの GET だけで、セッション内容やノートは送らない。
// 関心の採点は internal/interest(語の一致・規則ベース)。LLM 補助(llm.go)は設定 news.llm で明示したときだけ動く opt-in で、
// 既定では LLM を呼ばない。
package news

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/pilefort/braindex/internal/fsutil"
	"github.com/pilefort/braindex/internal/interest"
)

// writeAtomic は保存物(既読・統計・LLM キャッシュ・keep)の書き込み。一時ファイルに書き切ってから置き換えるので、
// 途中で止まっても前回の内容が残る(fsutil.WriteAtomic)。テストで差し替える(保存の失敗を再現するため)。
var writeAtomic = fsutil.WriteAtomic

// Settings は braindex.json の news 節。省略・0 は既定値。
type Settings struct {
	Dir          string         `json:"dir"`            // ニュースの置き場(hub 相対・スラッシュ区切り)。既定 news
	Feeds        string         `json:"feeds"`          // フィード一覧の JSON(hub 相対)。既定 news/feeds.json
	SeenDays     int            `json:"seen_days"`      // 既読を覚えておく日数。既定 90
	CapPerLayer  map[string]int `json:"cap_per_layer"`  // 層ごとの 1 フィードあたり表示上限。無い層は DefaultCap
	KeepMonths   int            `json:"keep_months"`    // keep 履歴を遡る月数。既定 3
	ProfileDays  int            `json:"profile_days"`   // 関心プロファイルが見る直近の日数(索引・セッション)。既定 14
	SessionsDir  string         `json:"sessions_dir"`   // セッションログの置き場。空なら retro.sessions_dir → ~/.claude/projects
	ShowMinScore *int           `json:"show_min_score"` // この関心度(0〜interest.MaxScore)以上を主要表示。未満は「関心外と判定」に折りたたむ。
	// ポインタなのは 0(全件を主要表示)と未設定(既定 2)を区別するため。他のキーのように 0 を未設定とみなすと、0 を設定できない
	Serendipity   *int   `json:"serendipity"`     // 関心外から拾い上げて「もしかして興味あるかも」に出す件数。既定 2・0 で出さない。ポインタなのは 0(出さない)と未設定(既定 2)を区別するため
	LLM           string `json:"llm"`             // LLM 補助(翻訳＋採点)。"off"(既定)か "claude-cli"(claude CLI のヘッドレス呼び出し・opt-in)
	LLMModel      string `json:"llm_model"`       // claude CLI に渡すモデル名(--model)。空なら CLI の既定
	LLMTimeoutSec int    `json:"llm_timeout_sec"` // 1 バッチの待ち時間(秒)。既定 120
	LLMBudgetSec  int    `json:"llm_budget_sec"`  // LLM 補助全体の待ち時間(秒)。既定 600
}

// LLM 補助の値。
const (
	LLMOff               = "off"
	LLMClaudeCLI         = "claude-cli"
	DefaultLLMTimeoutSec = 120
	DefaultLLMBudgetSec  = 600
)

// 既定値。
const (
	DefaultDir          = "news"
	DefaultFeeds        = "news/feeds.json"
	DefaultSeenDays     = 90
	DefaultProfileDays  = 14
	DefaultKeepMonths   = 3
	DefaultShowMinScore = 2              // 原型と同じ(2026-08-15〜の運用値)
	DefaultSerendipity  = 2              // 関心外から日替わりで拾い上げる件数
	MaxSerendipity      = 10             // これ以上は「たまに」でなくなる
	KeepDir             = "keep"         // Dir の下。選別で残した見出し(YYYY-MM.md)。git 管理
	InterestsFile       = "interests.md" // Dir の下。補助の関心ファイル(任意・1 行 1 語)
	DefaultCap          = 20
	SeenFile            = ".seen.json" // Dir の下。git 管理外(braindex init が配る hub の .gitignore が news/.*.json を除外する)
)

// DefaultCapPerLayer は cap_per_layer を省略したときの層別上限。
var DefaultCapPerLayer = map[string]int{"daily": 15, "weekly": 25, LayerGeneralNews: 5}

// LayerGeneralNews は候補から登録した一般ニュースの層。
const LayerGeneralNews = "general"

// WithDefaults は空・0 の項目を既定値で埋めた複製を返す。
func (s Settings) WithDefaults() Settings {
	if s.Dir == "" {
		s.Dir = DefaultDir
	}
	if s.Feeds == "" {
		s.Feeds = DefaultFeeds
	}
	if s.SeenDays <= 0 {
		s.SeenDays = DefaultSeenDays
	}
	if s.CapPerLayer == nil {
		// 複製を持たせる(そのまま指すと、返した Settings への書き込みが既定の表を汚す)
		m := make(map[string]int, len(DefaultCapPerLayer))
		for k, v := range DefaultCapPerLayer {
			m[k] = v
		}
		s.CapPerLayer = m
	}
	if s.KeepMonths <= 0 {
		s.KeepMonths = DefaultKeepMonths
	}
	if s.ProfileDays <= 0 {
		s.ProfileDays = DefaultProfileDays
	}
	if s.ShowMinScore == nil {
		n := DefaultShowMinScore
		s.ShowMinScore = &n
	}
	if s.Serendipity == nil {
		n := DefaultSerendipity
		s.Serendipity = &n
	}
	if s.LLM == "" {
		s.LLM = LLMOff
	}
	if s.LLMTimeoutSec <= 0 {
		s.LLMTimeoutSec = DefaultLLMTimeoutSec
	}
	if s.LLMBudgetSec <= 0 {
		s.LLMBudgetSec = DefaultLLMBudgetSec
	}
	return s
}

// SerendipityCount は関心外から拾い上げる件数。WithDefaults を通していない Settings でも既定を返す。
func (s Settings) SerendipityCount() int {
	if s.Serendipity == nil {
		return DefaultSerendipity
	}
	return *s.Serendipity
}

// MinScore は主要表示の下限。WithDefaults を通していない Settings でも既定を返す。
func (s Settings) MinScore() int {
	if s.ShowMinScore == nil {
		return DefaultShowMinScore
	}
	return *s.ShowMinScore
}

// Validate は設定ファイルに書かれた値を確かめる。範囲外は既定に丸めず、設定の誤りとしてエラーにする
// (未知のキーを通さないのと同じ考え。丸めると、書いた値と動きが食い違ったまま気づけない)。
func (s Settings) Validate() error {
	if s.LLMBudgetSec < 0 {
		return fmt.Errorf("設定 news.llm_budget_sec: 0 以上(0 は既定 %d): %d", DefaultLLMBudgetSec, s.LLMBudgetSec)
	}
	if s.ShowMinScore != nil && (*s.ShowMinScore < 0 || *s.ShowMinScore > interest.MaxScore) {
		return fmt.Errorf("設定 news.show_min_score: 0〜%d のどれか(0 は全件を主要表示): %d", interest.MaxScore, *s.ShowMinScore)
	}
	if s.Serendipity != nil && (*s.Serendipity < 0 || *s.Serendipity > MaxSerendipity) {
		return fmt.Errorf("設定 news.serendipity: 0〜%d のどれか(0 は出さない): %d", MaxSerendipity, *s.Serendipity)
	}
	if s.SeenDays < 0 {
		return fmt.Errorf("設定 news.seen_days: 0 以上(0 は既定 %d): %d", DefaultSeenDays, s.SeenDays)
	}
	if s.KeepMonths < 0 {
		return fmt.Errorf("設定 news.keep_months: 0 以上(0 は既定 %d): %d", DefaultKeepMonths, s.KeepMonths)
	}
	if s.ProfileDays < 0 {
		return fmt.Errorf("設定 news.profile_days: 0 以上(0 は既定 %d): %d", DefaultProfileDays, s.ProfileDays)
	}
	for layer, n := range s.CapPerLayer {
		if n < 0 {
			return fmt.Errorf("設定 news.cap_per_layer[%q]: 0 以上(0 は既定 %d): %d", layer, DefaultCap, n)
		}
	}
	switch s.LLM {
	case "", LLMOff, LLMClaudeCLI:
	default:
		return fmt.Errorf("設定 news.llm: %q か %q(既定 %q・LLM を呼ばない): %q", LLMOff, LLMClaudeCLI, LLMOff, s.LLM)
	}
	if s.LLMTimeoutSec < 0 {
		return fmt.Errorf("設定 news.llm_timeout_sec: 0 以上(0 は既定 %d): %d", DefaultLLMTimeoutSec, s.LLMTimeoutSec)
	}
	return nil
}

// Cap は layer の 1 フィードあたり表示上限。設定に無い層(all を含む)は DefaultCap。
func (s Settings) Cap(layer string) int {
	if n, ok := s.CapPerLayer[layer]; ok && n > 0 {
		return n
	}
	return DefaultCap
}

// Source はフィード一覧(feeds.json)の 1 件。
type Source struct {
	Name     string `json:"name"`               // 表示名。必須・一覧の中で一意
	URL      string `json:"url"`                // http(s) の URL。必須
	Layer    string `json:"layer,omitempty"`    // 自由なラベル(daily / weekly など)。-layer で絞る。空は all でだけ取る
	Lang     string `json:"lang,omitempty"`     // 言語(ja / en など)。表示と後続の翻訳判定に使う
	Category string `json:"category,omitempty"` // 分類(表示用)
	Note     string `json:"note,omitempty"`     // 備考(人向け。読まない)
}

// LoadFeeds は feeds.json(Source の配列)を読む。未知のキー・name/url の欠落・http(s) でない URL・name の重複はエラー。
func LoadFeeds(path string) ([]Source, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("フィード一覧が無い: %s(name と url を持つオブジェクトの配列を書く)", path)
		}
		return nil, fmt.Errorf("フィード一覧を読めない: %w", err)
	}
	return ParseFeeds(b, path)
}

// ParseFeeds は LoadFeeds のバイト列版。name はエラーメッセージ用。
func ParseFeeds(b []byte, name string) ([]Source, error) {
	// UTF-8 BOM (EF BB BF) を除去。feeds.json は braindex init が配らず利用者が手で書くので、
	// BOM を付けるエディタ(Windows PowerShell 5.1 の Set-Content -Encoding utf8 など)で
	// 書かれると encoding/json が先頭バイトで落ちる。config.Load と同じ規則。
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		b = b[3:]
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var srcs []Source
	if err := dec.Decode(&srcs); err != nil {
		return nil, fmt.Errorf("フィード一覧 %s: %w", name, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("フィード一覧 %s: 末尾に余分な内容がある(JSON の配列 1 つだけを書く)", name)
	}
	seen := map[string]bool{}
	for i, s := range srcs {
		if strings.TrimSpace(s.Name) == "" {
			return nil, fmt.Errorf("フィード一覧 %s: %d 番目に name が無い", name, i+1)
		}
		if !strings.HasPrefix(s.URL, "http://") && !strings.HasPrefix(s.URL, "https://") {
			return nil, fmt.Errorf("フィード一覧 %s: %s の url が http(s) でない: %q", name, s.Name, s.URL)
		}
		if seen[s.Name] {
			return nil, fmt.Errorf("フィード一覧 %s: name %q が重複している", name, s.Name)
		}
		seen[s.Name] = true
	}
	return srcs, nil
}

// LayerAll は全フィードを対象にする層の名前。
const LayerAll = "all"

// FilterLayer は layer のフィードだけを返す。LayerAll なら全件。
func FilterLayer(srcs []Source, layer string) []Source {
	if layer == LayerAll {
		return srcs
	}
	var out []Source
	for _, s := range srcs {
		if s.Layer == layer {
			out = append(out, s)
		}
	}
	return out
}

// Layers は一覧に現れる層の名前を昇順で返す(空は含めない)。
func Layers(srcs []Source) []string {
	set := map[string]bool{}
	for _, s := range srcs {
		if s.Layer != "" {
			set[s.Layer] = true
		}
	}
	out := make([]string, 0, len(set))
	for l := range set {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}
