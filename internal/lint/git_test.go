package lint

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestHeadContentJapaneseCRLF(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無い環境")
	}
	dir := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	run := func(args ...string) {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "core.autocrlf=false", "-c", "user.name=test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	path := filepath.Join(dir, "日本語のノート.md")
	if err := os.WriteFile(path, []byte("見出し\r\n本文\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "--", "日本語のノート.md")
	run("commit", "-qm", "fixture")
	got, ok := HeadContent(path)
	if !ok || string(got) != "見出し\n本文\n" {
		t.Fatalf("HeadContent = %q, %v", got, ok)
	}
}
