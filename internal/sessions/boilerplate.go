package sessions

import "strings"

// 定型(機械実行・貼り付け)の判定に使う値。
const (
	// BoilerplatePrefixRunes は判定に使う冒頭の長さ(文字)。空白は 1 つに畳んでから切る。
	BoilerplatePrefixRunes = 120
	// BoilerplateMinRunes より短い発話は判定にかけない。「違う」「そうじゃない」のような
	// 短い同文の訂正は、人が打っていても何セッションにも現れるため。
	BoilerplateMinRunes = 40
	// DefaultBoilerplateSessions は「同じ冒頭が何セッションに出たら定型とみなすか」の既定。
	DefaultBoilerplateSessions = 3
)

// MarkBoilerplate は、同じ冒頭の人間の発話が minSessions 以上のセッションに現れるとき、
// その発話に Turn.Boilerplate を立てる。立てた発話の数を返す。
//
// 定型の正体は機械が流し込んだ指示(定期実行・スクリプト・貼り付けたテンプレ)で、
// 人が打った発話として数えると訂正率も関心プロファイルも歪む。判定を retro・news・learn で
// 共有するために読み取り層に置く——別々に持つと、同じログから違う数が出る
// (設計レビュー 2026-09-06 M11)。
//
// ログに機械実行の印は無い(promptSource は対話と同じ typed)ので、冒頭の一致という規則で見分ける。
func MarkBoilerplate(ss []Session, minSessions int) int {
	if minSessions <= 0 {
		minSessions = DefaultBoilerplateSessions
	}
	// 冒頭 → その冒頭が現れたセッション ID の集合
	seen := map[string]map[string]bool{}
	for _, s := range ss {
		for _, t := range s.Turns {
			if t.Role != User {
				continue
			}
			k, ok := BoilerplateKey(t.Text)
			if !ok {
				continue
			}
			if seen[k] == nil {
				seen[k] = map[string]bool{}
			}
			seen[k][s.ID] = true
		}
	}
	n := 0
	for i := range ss {
		for j := range ss[i].Turns {
			t := &ss[i].Turns[j]
			if t.Role != User {
				continue
			}
			if k, ok := BoilerplateKey(t.Text); ok && len(seen[k]) >= minSessions {
				t.Boilerplate = true
				n++
			}
		}
	}
	return n
}

// BoilerplateKey は定型の判定に使う鍵。空白を 1 つに畳み、先頭 BoilerplatePrefixRunes 文字で切る。
// BoilerplateMinRunes より短い発話は判定にかけない(ok=false)。
func BoilerplateKey(text string) (string, bool) {
	t := strings.Join(strings.Fields(text), " ")
	rs := []rune(t)
	if len(rs) < BoilerplateMinRunes {
		return "", false
	}
	if len(rs) > BoilerplatePrefixRunes {
		rs = rs[:BoilerplatePrefixRunes]
	}
	return string(rs), true
}
