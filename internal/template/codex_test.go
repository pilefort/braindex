package template

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/textblock"
)

func readCodexTest(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCodexInstallAndIsolation(t *testing.T) {
	home := codexTestHome(t)
	custom := filepath.Join(t.TempDir(), "codex")
	t.Setenv("CODEX_HOME", custom)
	hub := t.TempDir()
	abs, _ := filepath.Abs(hub)
	abs = filepath.ToSlash(abs)
	begin, end := "# BEGIN braindex "+abs, "# END braindex "+abs
	before, after := "一\r\n二\r\n三\r\n\r\n", "四\r\n五\r\n六\r\n\r\n"
	agents := filepath.Join(custom, "AGENTS.md")
	if err := writeFile(agents, []byte(before+begin+"\nold\n"+end+"\n"+after)); err != nil {
		t.Fatal(err)
	}
	res, err := InstallAgents(hub, []Feature{FeatureConventions}, []string{"codex"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.HomeMerged) != 1 || len(res.HomeCreated) != 3 {
		t.Fatalf("%+v", res)
	}
	first := readCodexTest(t, agents)
	if !bytes.HasPrefix(first, []byte(before)) || !bytes.HasSuffix(first, []byte(after)) {
		t.Fatal("outside changed")
	}
	for _, want := range []string{begin, end, "hub `" + abs + "`", "~/.agents/skills/"} {
		if !strings.Contains(string(first), want) {
			t.Fatalf("missing %s", want)
		}
	}
	if bytes.Contains(first, []byte(".claude/skills/")) {
		t.Fatal("Claude path remains")
	}
	led, _, err := LoadLedger(hub)
	if err != nil || len(led.Home) != 3 || !reflect.DeepEqual(led.Agents, []string{"codex"}) {
		t.Fatalf("%+v %v", led, err)
	}
	for _, name := range []string{"record-lint", "contradiction-scan", "research-distill"} {
		key := "agents/skills/" + name + "/SKILL.md"
		b := readCodexTest(t, homeTarget(home, key))
		if led.Home[key] != Hash(b) {
			t.Fatal("wrong hash")
		}
		for _, bad := range []string{".claude/skills/", "使い方: `/record-lint ", "使い方: `/contradiction-scan ", "WebFetch"} {
			if bytes.Contains(b, []byte(bad)) {
				t.Fatal(bad)
			}
		}
	}
	for _, p := range []string{filepath.Join(hub, "CLAUDE.md"), filepath.Join(hub, ".claude"), filepath.Join(hub, ".agents"), filepath.Join(home, ".agents/skills/retro"), filepath.Join(home, ".codex")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("unexpected %s: %v", p, err)
		}
	}
	for _, p := range []string{"README.md", "braindex.json", "docs/conventions.md", "work/TODO.md"} {
		readCodexTest(t, filepath.Join(hub, p))
	}
	ledgerBefore := readCodexTest(t, filepath.Join(hub, LedgerPath))
	if _, err := InstallAgents(hub, []Feature{FeatureConventions}, []string{"codex"}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, readCodexTest(t, agents)) || !bytes.Equal(ledgerBefore, readCodexTest(t, filepath.Join(hub, LedgerPath))) {
		t.Fatal("not idempotent")
	}
	other := t.TempDir()
	if _, err := InstallAgents(other, nil, []string{"codex"}); err != nil {
		t.Fatal(err)
	}
	combined := readCodexTest(t, agents)
	otherAbs, _ := filepath.Abs(other)
	otherAbs = filepath.ToSlash(otherAbs)
	otherLines := textblock.Lines(string(combined), "# BEGIN braindex "+otherAbs, "# END braindex "+otherAbs)
	if len(otherLines) == 0 || bytes.Count(combined, []byte("# BEGIN braindex ")) != 2 {
		t.Fatal("missing second hub")
	}
	// 自分の範囲は編集済みでも更新し、別 hub の範囲と前後は残す。
	edited := strings.Replace(string(combined), "この範囲は braindex", "edited", 1)
	if err := os.WriteFile(agents, []byte(edited), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Update(hub, KindHub, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(combined, readCodexTest(t, agents)) {
		t.Fatal("update did not restore only own block")
	}
}

func TestCodexSkillUpdate(t *testing.T) {
	for _, mode := range []string{"existing", "edited", "old", "same", "force", "missing"} {
		t.Run(mode, func(t *testing.T) {
			home := codexTestHome(t)
			hub := t.TempDir()
			key := "agents/skills/record-lint/SKILL.md"
			target := homeTarget(home, key)
			if mode == "existing" {
				if err := writeFile(target, []byte("user")); err != nil {
					t.Fatal(err)
				}
			}
			res, err := InstallAgents(hub, []Feature{FeatureConventions}, []string{"codex"})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "existing" && (len(res.HomeSkipped) != 1 || string(readCodexTest(t, target)) != "user") {
				t.Fatalf("%+v", res)
			}
			led, _, _ := LoadLedger(hub)
			switch mode {
			case "edited", "force":
				if err := os.WriteFile(target, []byte("user"), 0600); err != nil {
					t.Fatal(err)
				}
			case "old":
				if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
					t.Fatal(err)
				}
				led.Home[key] = Hash([]byte("old"))
				if err := SaveLedger(hub, led); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
			}
			ledgerBefore := readCodexTest(t, filepath.Join(hub, LedgerPath))
			agents := filepath.Join(home, ".codex/AGENTS.md")
			if err := os.WriteFile(agents, []byte("user rules\r\n"), 0600); err != nil {
				t.Fatal(err)
			}
			dry, err := Update(hub, KindHub, UpdateOptions{DryRun: true, Force: mode == "force"})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(ledgerBefore, readCodexTest(t, filepath.Join(hub, LedgerPath))) || string(readCodexTest(t, agents)) != "user rules\r\n" {
				t.Fatal("dry-run wrote")
			}
			if _, err := os.Stat(target + NewSuffix); !os.IsNotExist(err) {
				t.Fatal("dry-run created .new")
			}
			out, err := Update(hub, KindHub, UpdateOptions{Force: mode == "force"})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(dry, out) {
				t.Fatalf("dry=%+v actual=%+v", dry, out)
			}
			switch mode {
			case "existing", "edited":
				if len(out.HomeConflicts) != 1 || string(readCodexTest(t, target)) != "user" {
					t.Fatalf("%+v", out)
				}
				readCodexTest(t, target+NewSuffix)
				now, _, _ := LoadLedger(hub)
				if now.Home[key] != led.Home[key] {
					t.Fatal("conflict advanced hash")
				}
			case "old", "force":
				if len(out.HomeUpdated) != 1 {
					t.Fatalf("%+v", out)
				}
			case "same":
				if len(out.HomeSkipped) != 3 {
					t.Fatalf("%+v", out)
				}
			case "missing":
				if len(out.HomeCreated) != 1 {
					t.Fatalf("%+v", out)
				}
			}
		})
	}
}

func TestCodexAddAgentAndDeterminism(t *testing.T) {
	codexTestHome(t)
	hub := t.TempDir()
	if _, err := InstallFeatures(hub, []Feature{FeatureReview, FeatureRetro}); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallAgents(hub, nil, []string{"codex"}); err != nil {
		t.Fatal(err)
	}
	led, _, _ := LoadLedger(hub)
	if !reflect.DeepEqual(led.Agents, []string{"claude", "codex"}) || len(led.Home) != 4 {
		t.Fatalf("%+v", led)
	}
	readCodexTest(t, filepath.Join(hub, "CLAUDE.md"))
	abs, _ := filepath.Abs(hub)
	abs = filepath.ToSlash(abs)
	if !bytes.Equal(CodexInstructions(abs, nil), CodexInstructions(abs, nil)) {
		t.Fatal("instructions not deterministic")
	}
	a, err := codexFiles([]Feature{FeatureAll})
	if err != nil {
		t.Fatal(err)
	}
	b, err := codexFiles([]Feature{FeatureAll})
	if err != nil || !reflect.DeepEqual(a, b) {
		t.Fatal("skills not deterministic")
	}
}

func TestCodexPartialFailureKeepsLedger(t *testing.T) {
	home := codexTestHome(t)
	hub := t.TempDir()
	oldWrite := writeFile
	t.Cleanup(func() { writeFile = oldWrite })
	failed := filepath.Join(home, ".agents", "skills", "research-distill", "SKILL.md")
	writeFile = func(p string, b []byte) error {
		if p == failed {
			return os.ErrPermission
		}
		return oldWrite(p, b)
	}
	res, err := InstallAgents(hub, []Feature{FeatureConventions}, []string{"codex"})
	if err == nil || len(res.HomeCreated) != 3 {
		t.Fatalf("%+v %v", res, err)
	}
	led, _, err := LoadLedger(hub)
	if err != nil || len(led.Home) != 2 || len(led.Files) == 0 {
		t.Fatalf("%+v %v", led, err)
	}
	writeFile = oldWrite
	out, err := Update(hub, KindHub, UpdateOptions{})
	if err != nil || len(out.HomeConflicts) != 0 || len(out.HomeCreated) != 1 {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestCodexOnlyAddingClaude(t *testing.T) {
	codexTestHome(t)
	hub := t.TempDir()
	if _, err := InstallAgents(hub, []Feature{FeatureConventions}, []string{"codex"}); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallFeatures(hub, nil); err != nil {
		t.Fatal(err)
	}
	readCodexTest(t, filepath.Join(hub, ".claude/skills/record-lint/SKILL.md"))
	led, _, _ := LoadLedger(hub)
	if !reflect.DeepEqual(led.Agents, []string{"claude", "codex"}) {
		t.Fatal(led.Agents)
	}
}
