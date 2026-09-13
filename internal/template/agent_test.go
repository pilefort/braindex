package template

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseAgents(t *testing.T) {
	for _, c := range []struct {
		input string
		want  []string
	}{
		{"codex,claude,codex,,", []string{"claude", "codex"}},
		{" codex ", []string{"codex"}},
		{",", []string{}},
	} {
		got, err := ParseAgents(c.input)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%q: %v %v", c.input, got, err)
		}
	}
	if _, err := ParseAgents("foo"); err == nil {
		t.Fatal("unknown accepted")
	}
}

func codexTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	oldHome, oldCodex := HomeDir, CodexHome
	t.Cleanup(func() { HomeDir, CodexHome = oldHome, oldCodex })
	HomeDir = func() (string, error) { return home, nil }
	t.Setenv("CODEX_HOME", "")
	return home
}

func TestCodexHome(t *testing.T) {
	home := codexTestHome(t)
	if got, err := CodexHome(); err != nil || got != filepath.Join(home, ".codex") {
		t.Fatalf("%s %v", got, err)
	}
	custom := filepath.Join(t.TempDir(), "profile")
	t.Setenv("CODEX_HOME", custom)
	if got, err := CodexHome(); err != nil || got != custom {
		t.Fatalf("%s %v", got, err)
	}
}

func TestClaudeDoesNotResolveHome(t *testing.T) {
	oldHome, oldCodex := HomeDir, CodexHome
	t.Cleanup(func() { HomeDir, CodexHome = oldHome, oldCodex })
	HomeDir = func() (string, error) { t.Fatal("HomeDir called"); return "", nil }
	CodexHome = func() (string, error) { t.Fatal("CodexHome called"); return "", nil }
	hub := t.TempDir()
	if _, err := InstallFeatures(hub, []Feature{FeatureAll}); err != nil {
		t.Fatal(err)
	}
	if _, err := Update(hub, KindHub, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	led, _, err := LoadLedger(hub)
	if err != nil || !reflect.DeepEqual(led.Agents, []string{"claude"}) {
		t.Fatalf("%+v %v", led, err)
	}
	// キーの無い旧版は Claude と推定し、dry-run では台帳を書かない。
	// SaveLedger は hub の空配列を明記するため、旧版を直接作る。
	raw := []byte(`{"version":1,"kind":"hub","features":[],"files":{}}`)
	if err := os.WriteFile(filepath.Join(hub, LedgerPath), raw, 0600); err != nil {
		t.Fatal(err)
	}
	res, err := Update(hub, KindHub, UpdateOptions{DryRun: true})
	if err != nil || !res.AgentsInferred {
		t.Fatalf("%+v %v", res, err)
	}
	got, _ := os.ReadFile(filepath.Join(hub, LedgerPath))
	if string(got) != string(raw) {
		t.Fatal("dry-run wrote ledger")
	}
	if _, err := Update(hub, KindHub, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	led, _, _ = LoadLedger(hub)
	if !reflect.DeepEqual(led.Agents, []string{"claude"}) {
		t.Fatal(led.Agents)
	}
}
