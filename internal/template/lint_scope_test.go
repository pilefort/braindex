package template

import (
	"strings"
	"testing"
)

// hub テンプレの record-lint / contradiction-scan は braindex の CLI 前提: 手順が braindex lint / braindex scope を呼び、
// 原型にあった個人環境のパス・Python・モデル名に依存しない(Phase 6・2026-09-03)。
func TestHub_LintAndScopeSkillsMatchCLI(t *testing.T) {
	files, err := Files(KindHub)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]string{}
	for _, f := range files {
		byPath[f.Path] = string(f.Content)
	}
	cases := []struct {
		path string
		want []string
	}{
		{".claude/skills/record-lint/SKILL.md", []string{"braindex lint", "-glossary", "warn", "candidate", "contradiction-scan", "docs/conventions.md"}},
		{".claude/skills/contradiction-scan/SKILL.md", []string{"braindex scope", "-topic", "chunk", "objective", "output format", "task boundaries", "record-lint"}},
		// Phase 7(2026-09-05): 調査スキルは braindex verify / braindex answer を呼ぶ。日本語の推敲スキルは同梱しない(決定 2026-09-03)
		{".claude/skills/research-distill/SKILL.md", []string{"braindex verify", "braindex answer", "github", "arxiv", "url", "quote", "objective", "output format", "task boundaries", "独立確認", "サブエージェント確認", "主張どまり", "推敲", "docs/notes/"}},
	}
	for _, c := range cases {
		skill := byPath[c.path]
		if skill == "" {
			t.Fatalf("%s が無い", c.path)
		}
		if !strings.HasPrefix(skill, "---\nname: ") {
			t.Errorf("%s の先頭が frontmatter でない", c.path)
		}
		for _, want := range c.want {
			if !strings.Contains(skill, want) {
				t.Errorf("%s に %q が無い", c.path, want)
			}
		}
		for _, bad := range []string{"python", "lint.py", "scope.py", "verify.py", "answer_html.py", "~/.claude", "~/projects", "BRAIN_DIR", "BRAIN_CATALOG", "braindex.exe", "go run", "model:", "rules/", "brain/"} {
			if strings.Contains(skill, bad) {
				t.Errorf("%s に %q が残っている(原型の個人環境への依存)", c.path, bad)
			}
		}
	}
	for _, p := range []string{"CLAUDE.md", "docs/conventions.md", "README.md"} {
		for _, name := range []string{"record-lint", "contradiction-scan", "research-distill"} {
			if !strings.Contains(byPath[p], name) {
				t.Errorf("%s が %s に触れていない", p, name)
			}
		}
	}
}
