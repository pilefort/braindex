package lint

import (
	"regexp"
	"strings"
)

var (
	// 日付は別に検査し、「同日」なども具体的な日付の指摘にする。
	reSupersede        = regexp.MustCompile(`^(失効|一部失効): (\S+) → (.+)$`)
	rePartialSupersede = regexp.MustCompile(`^(.+)（([^（）]+)）$`)
)

// checkSupersedeLines は ## 直下の失効・一部失効行だけを見る。旧表記・現行か失効かの判定は対象外。
func checkSupersedeLines(lines []string, skip []bool, add func(int, string, string, string, ...any)) {
	var headings []string
	for i, l := range lines {
		if !skip[i] && strings.HasPrefix(l, "## ") {
			headings = append(headings, strings.TrimSpace(l[3:]))
		}
	}
	for i, l := range lines {
		if skip[i] || !strings.HasPrefix(l, "## ") || i+1 >= len(lines) || skip[i+1] {
			continue
		}
		line := lines[i+1]
		if !strings.HasPrefix(line, "失効") && !strings.HasPrefix(line, "一部失効") {
			continue
		}
		m := reSupersede.FindStringSubmatch(line)
		var target string
		if m != nil {
			target = strings.TrimSpace(m[3])
			if m[1] == "一部失効" {
				if p := rePartialSupersede.FindStringSubmatch(target); p != nil && strings.TrimSpace(p[2]) != "" {
					target = strings.TrimSpace(p[1])
				} else {
					target = ""
				}
			}
		}
		if target == "" {
			add(i+2, KindSupersedeFormat, SeverityWarn, "書式は「失効: YYYY-MM-DD → 後継の見出し」または「一部失効: YYYY-MM-DD → 後継の見出し（どの部分か）」: %s", line)
			continue
		}
		if len(m[2]) != 10 || !isISODate(m[2]) {
			add(i+2, KindSupersedeDate, SeverityWarn, "日付は YYYY-MM-DD の絶対日付で書く: %q", m[2])
		}
		// 見出しの冒頭だけを書いた後継名も受け付ける。途中の部分一致はしない。
		found := false
		for _, h := range headings {
			if strings.HasPrefix(h, target) {
				found = true
				break
			}
		}
		if !found {
			add(i+2, KindSupersedeTarget, SeverityWarn, "後継 %q で始まる ## 見出しが同じファイルに無い", target)
		}
	}
}
