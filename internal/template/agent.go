package template

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// HomeDir は個人スキルのホームを返す。テストでは一時ディレクトリに差し替える。
var HomeDir = os.UserHomeDir

// CodexHome は CODEX_HOME、未設定ならホームの .codex を返す。
var CodexHome = func() (string, error) {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return home, nil
	}
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

// ParseAgents は対応先を検証し、空要素と重複を除いて昇順で返す。
func ParseAgents(s string) ([]string, error) {
	out := []string{}
	for _, raw := range strings.Split(s, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if name != "claude" && name != "codex" {
			return nil, fmt.Errorf("未知の対応先 %q(候補: claude, codex)", name)
		}
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out, nil
}

// HasAgent は対応先が選ばれているかを返す。
func HasAgent(agents []string, name string) bool { return slices.Contains(agents, name) }

// HomeDisplayPath はホーム内だけを ~ 表記にする。ホーム外の CODEX_HOME は絶対パスで示す。
func HomeDisplayPath(p string) string {
	home, err := HomeDir()
	if err == nil {
		rel, err := filepath.Rel(home, p)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(p)
}
