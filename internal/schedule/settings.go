// Package schedule は定期実行の登録(braindex schedule)を扱う。
//
// OS のスケジューラ(Windows は schtasks、macOS・Linux は crontab)へ渡すコマンドの組み立ては
// すべて純関数(plan.go・cron.go)にあり、このパッケージは外部プロセスを起動しない。
// 起動するのは cmd/braindex/cmd_schedule.go の runner で、テストはそこをモックに差し替える。
package schedule

import (
	"fmt"
	"regexp"
	"strings"
)

// Job は登録する定期実行 1 本。
//
// Args は braindex 自身のサブコマンドと引数(例 ["retro","check"])。文字列 1 本にしないのは、
// シェルの分割規則を設定ファイルに持ち込まないため。
type Job struct {
	Name string   `json:"name"` // タスク名・cron の目印に使う識別子
	Args []string `json:"args"` // braindex に渡す引数
	When string   `json:"when"` // "daily:HH:MM" か "weekly:<曜日>:HH:MM"
}

// Settings は braindex.json の schedule 節。
type Settings struct {
	Jobs []Job `json:"jobs"` // 登録するジョブ。空なら DefaultJobs
}

// DefaultJobs は jobs を書かなかったときに登録するジョブ。README の定期実行の例と揃える。
// retro を review の 5 分後にするのは、同じ時刻に 2 本走らせて索引の読みが競合しないようにするため。
func DefaultJobs() []Job {
	return []Job{
		{Name: "review", Args: []string{"review"}, When: "weekly:mon:09:00"},
		{Name: "retro", Args: []string{"retro", "check"}, When: "weekly:mon:09:05"},
	}
}

// WithDefaults は空の項目を既定値で埋めた複製を返す。
func (s Settings) WithDefaults() Settings {
	if len(s.Jobs) == 0 {
		s.Jobs = DefaultJobs()
	}
	return s
}

// nameRe はジョブ名に許す形。タスク名と cron の目印にそのまま入るので、空白や記号を通さない。
var nameRe = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// Validate は設定ファイルに書かれた値を確かめる。
//
// known は登録済みのサブコマンド名(呼び出し側が渡す)。nil なら先頭要素の照合をしない。
// 範囲外の値は既定に丸めず、設定の誤りとしてエラーにする(未知のキーを通さないのと同じ考え)。
func (s Settings) Validate(known []string) error {
	seen := map[string]bool{}
	for i, j := range s.Jobs {
		if !nameRe.MatchString(j.Name) {
			return fmt.Errorf("設定 schedule.jobs[%d].name: 英小文字・数字・ハイフンの 1〜32 文字: %q", i, j.Name)
		}
		if seen[j.Name] {
			return fmt.Errorf("設定 schedule.jobs[%d].name: 同じ名前が 2 回ある: %q", i, j.Name)
		}
		seen[j.Name] = true
		if len(j.Args) == 0 {
			return fmt.Errorf("設定 schedule.jobs[%d].args: braindex に渡す引数を 1 つ以上書く(例 [\"retro\",\"check\"])", i)
		}
		if known != nil && !contains(known, j.Args[0]) {
			return fmt.Errorf("設定 schedule.jobs[%d].args[0]: braindex のサブコマンドでない: %q(使えるのは %s)",
				i, j.Args[0], strings.Join(known, "・"))
		}
		if _, err := ParseWhen(j.When); err != nil {
			return fmt.Errorf("設定 schedule.jobs[%d].when: %w", i, err)
		}
	}
	return nil
}

// Find は名前でジョブを探す。
func (s Settings) Find(name string) (Job, bool) {
	for _, j := range s.Jobs {
		if j.Name == name {
			return j, true
		}
	}
	return Job{}, false
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
