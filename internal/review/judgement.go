package review

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// JudgementSections は人が埋める節の見出し(下書きの節 5〜7)。機械節と違い、braindex は中身を作らない。
var JudgementSections = []string{
	"今週の差分ダイジェスト（リポ別）",
	"アーカイブ（実施・見送りと理由）",
	"次アクション",
}

// emptyJudgementSections は前回の下書き path のうち、見出しだけで中身の無い判断の節の名前を返す。
// path が空・ファイルが無いときは何も返さない(初回・別名で出したときは確かめようがない)。
//
// 見出しごと無い節は数えない。braindex が作った下書きでないファイル(手で書いた覚え書きなど)を
// 「全部空」と言っても直しようがない。見出しがあって中身が無いものだけを返す。
//
// 「中身がある」の判定: その節の中に、見出しでなく、括弧書きのひな型でもない行が 1 行でもあること。
// ひな型は braindex が書いた「（1〜3 件）」の類で、消さずに追記する人がいるので中身と数えない。
func emptyJudgementSections(path string) (empty []string, warnings []string) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			warnings = append(warnings, fmt.Sprintf("前回の下書き %s: %s", path, describeErr(err)))
		}
		return nil, warnings
	}
	seen, filled := map[string]bool{}, map[string]bool{}
	cur := ""
	inFence := false
	for _, line := range splitLines(b) {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if !inFence && strings.HasPrefix(t, "## ") {
			cur = strings.TrimSpace(t[3:])
			seen[cur] = true
			continue
		}
		if cur == "" || t == "" || isTemplateLine(t) {
			continue
		}
		filled[cur] = true
	}
	for _, name := range JudgementSections {
		if seen[name] && !filled[name] {
			empty = append(empty, name)
		}
	}
	return empty, warnings
}

// isTemplateLine は braindex が節に置いた案内の行(全角括弧で囲んだ 1 行)。
func isTemplateLine(t string) bool {
	return strings.HasPrefix(t, "（") && strings.HasSuffix(t, "）")
}
