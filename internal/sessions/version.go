package sessions

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// MinKnownVersion と MaxKnownVersion は、この読み取り層の除外規則(ExcludeReason・isMeta・isSidechain・
// message.content のブロック種)を実ログで確かめた Claude Code の版の範囲。
// ログの形式は版ごとに変わりうるので、範囲の外の版を読んだら「規則が合わないかもしれない」と伝える。
// 規則そのものは変えない——合わない証拠が無いうちに挙動を変えると、確かめた版での結果まで動く。
//
// 上げ方は manual/retro.md「確認済みの版」に書いてある。
const (
	MinKnownVersion = "2.1.258" // パッケージのコメントで確認した版(2026-09-02)
	MaxKnownVersion = "2.1.263" // 2026-09-06 に実ログで確認(type・isSidechain・timestamp・cwd・version・content のブロック種)
)

// VersionInRange は v が確認済みの範囲に入るか。
// 空文字と "." 区切りの数値として読めない版は「分からない」として範囲内に倒す——
// 判定できないものを「未確認」と言うと、直しようのない警告が出続ける。
func VersionInRange(v string) bool {
	n, ok := parseVersion(v)
	if !ok {
		return true
	}
	lo, _ := parseVersion(MinKnownVersion)
	hi, _ := parseVersion(MaxKnownVersion)
	return compareVersion(n, lo) >= 0 && compareVersion(n, hi) <= 0
}

// parseVersion は "." 区切りの版を数値の列にする。空の段・数字でない段があれば ok=false。
func parseVersion(v string) ([]int, bool) {
	if v == "" {
		return nil, false
	}
	parts := strings.Split(v, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}

// compareVersion は段ごとに数値で比べる(短いほうは 0 埋め)。文字列比較だと 2.1.9 > 2.1.10 になる。
func compareVersion(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// unknownVersionWarnings は、確認済みの範囲の外の版があれば「古い側」「新しい側」で 1 行ずつ返す
// (最大 2 行)。versions は 版 → ファイル数。
//
// 版ごとに 1 行にしないのは、実ログで数えたら手元に範囲外の版が 21 種あり、21 行出たため
// (2026-09-06 実測)。行が増えるほど読まれなくなるので、種類と件数と端の版だけを言う。
// 古い側と新しい側を分けるのは、次にやることが違うから——新しい側は「確かめて MaxKnownVersion を上げる」、
// 古い側は「そういうログが混ざっている」という情報。
func unknownVersionWarnings(versions map[string]int) []string {
	var older, newer []string
	oldFiles, newFiles := 0, 0
	for v, n := range versions {
		if VersionInRange(v) {
			continue
		}
		if compareVersionString(v, MinKnownVersion) < 0 {
			older = append(older, v)
			oldFiles += n
		} else {
			newer = append(newer, v)
			newFiles += n
		}
	}
	sortVersions(older)
	sortVersions(newer)
	var out []string
	if len(older) > 0 {
		out = append(out, fmt.Sprintf("セッションログに確認済み(%s〜%s)より古い版が %d 種・%d ファイルある(最も古い %s)。除外規則が合わない可能性がある",
			MinKnownVersion, MaxKnownVersion, len(older), oldFiles, older[0]))
	}
	if len(newer) > 0 {
		out = append(out, fmt.Sprintf("セッションログに確認済み(%s〜%s)より新しい版が %d 種・%d ファイルある(最も新しい %s)。除外規則を確かめて MaxKnownVersion を上げる(manual/retro.md「確認済みの版」)",
			MinKnownVersion, MaxKnownVersion, len(newer), newFiles, newer[len(newer)-1]))
	}
	return out
}

// compareVersionString は版の文字列同士を数値で比べる。読めない版は 0(同じ)を返す。
func compareVersionString(a, b string) int {
	x, ok1 := parseVersion(a)
	y, ok2 := parseVersion(b)
	if !ok1 || !ok2 {
		return 0
	}
	return compareVersion(x, y)
}

// sortVersions は版を数値の昇順に並べる(文字列比較だと 2.1.9 が 2.1.10 の後ろに来る)。
func sortVersions(vs []string) {
	sort.Slice(vs, func(i, j int) bool { return compareVersionString(vs[i], vs[j]) < 0 })
}
