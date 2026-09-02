package template

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hub テンプレの週次レビューは braindex review 前提: スキルの手順が CLI を呼び、モデル名に依存せず、
// 設定の雛形に review 節がある(Phase 2・2026-09-02)。
func TestHub_ReviewSkillMatchesCLI(t *testing.T) {
	files, err := Files(KindHub)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]string{}
	for _, f := range files {
		byPath[f.Path] = string(f.Content)
	}
	skill := byPath[".claude/skills/braindex-review/SKILL.md"]
	for _, want := range []string{"`braindex review`", "work/review/<今日>.md", "-since", "`braindex` で索引を再生成"} {
		if !strings.Contains(skill, want) {
			t.Errorf("SKILL.md に %q が無い", want)
		}
	}
	for _, bad := range []string{"model:", "git log --since", "git diff --stat"} {
		if strings.Contains(skill, bad) {
			t.Errorf("SKILL.md に %q が残っている(手順は CLI に寄せる)", bad)
		}
	}
	if !strings.Contains(byPath["braindex.json"], `"review"`) {
		t.Errorf("braindex.json に review 節が無い")
	}
	for _, p := range []string{"CLAUDE.md", "docs/conventions.md"} {
		if !strings.Contains(byPath[p], "`braindex review`") {
			t.Errorf("%s が braindex review に触れていない", p)
		}
	}
}

// リポ直下の braindex.example.json は hub テンプレの braindex.json と同じ内容に保つ。README はこれを
// braindex.json としてコピーする手順を案内しているので、片方だけ直すと init した hub と手で置いた hub で
// 設定(review 節など)が食い違う。
func TestHub_ExampleJSONMatchesTemplate(t *testing.T) {
	files, err := Files(KindHub)
	if err != nil {
		t.Fatal(err)
	}
	var tmpl []byte
	for _, f := range files {
		if f.Path == "braindex.json" {
			tmpl = f.Content
		}
	}
	if tmpl == nil {
		t.Fatal("hub テンプレに braindex.json が無い")
	}
	example, err := os.ReadFile(filepath.Join("..", "..", "braindex.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(example) != string(tmpl) {
		t.Errorf("braindex.example.json と hub テンプレの braindex.json が違う(同じ内容に保つ):\n example: %s\ntemplate: %s", example, tmpl)
	}
}
