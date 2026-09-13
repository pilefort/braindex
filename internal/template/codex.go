package template

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/pilefort/braindex/internal/textblock"
)

// CodexInstructions は CLAUDE.md と同じ元に指定の置換と前置きだけを加える。
func CodexInstructions(hubAbs string, feats []Feature) []byte {
	body, _ := templates.ReadFile("templates/hub/CLAUDE.md")
	s := strings.ReplaceAll(string(body), ".claude/skills/", "~/.agents/skills/")
	s = strings.ReplaceAll(s, "# この hub で作業するときの決まり", "# braindex の hub `"+hubAbs+"` で作業するときの決まり")
	return []byte(fmt.Sprintf("この範囲は braindex が hub `%s` のために管理している（手で直さない。`braindex update` で更新される）。以下の相対パス（`index/catalog.md`・`docs/`・`work/`・`braindex.json`）は hub `%s` からの相対。\n\n%s", hubAbs, hubAbs, s))
}

// CodexSkill は同梱スキルを、定めた置換だけで Codex 向けにする。
func CodexSkill(name string, body []byte) []byte {
	return []byte(strings.NewReplacer(
		".claude/skills/", "~/.agents/skills/",
		"使い方: `/record-lint ", "使い方（record-lint スキルを呼ぶ）: `",
		"使い方: `/contradiction-scan ", "使い方（contradiction-scan スキルを呼ぶ）: `",
		"WebFetch", "Web ページの取得ツール",
	).Replace(string(body)))
}

func codexFiles(feats []Feature) ([]File, error) {
	files, err := FeatureFiles(feats)
	if err != nil {
		return nil, err
	}
	var out []File
	for _, f := range files {
		if !strings.HasPrefix(f.Path, ".claude/skills/") {
			continue
		}
		name := path.Base(path.Dir(f.Path))
		if name == "retro" {
			continue
		}
		out = append(out, File{Path: "agents/skills/" + name + "/SKILL.md", Content: CodexSkill(name, f.Content)})
	}
	return out, nil
}

// 台帳の home キーは仕様上 agents/ で始まり、実際の配置は .agents/ になる。
func homeTarget(home, key string) string { return filepath.Join(home, filepath.FromSlash("."+key)) }

func mergeCodexInstructions(dst string, feats []Feature, dry bool) (target string, created, changed bool, err error) {
	hub, err := filepath.Abs(dst)
	if err != nil {
		return "", false, false, err
	}
	hub = filepath.ToSlash(hub)
	if strings.ContainsAny(hub, "\r\n") {
		return "", false, false, fmt.Errorf("hub のパスに改行がある")
	}
	home, err := CodexHome()
	if err != nil {
		return "", false, false, err
	}
	target = filepath.Join(home, "AGENTS.md")
	cur, err := os.ReadFile(target)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return target, false, false, err
	}
	created = errors.Is(err, fs.ErrNotExist)
	begin, end := "# BEGIN braindex "+hub, "# END braindex "+hub
	lines := strings.Split(strings.TrimRight(string(CodexInstructions(hub, feats)), "\n"), "\n")
	out := []byte(textblock.MergePreservingOutside(string(cur), begin, end, lines))
	changed = !bytes.Equal(cur, out)
	if changed && !dry {
		err = writeFile(target, out)
	} else {
		err = nil
	}
	return target, created, changed, err
}

func installCodex(dst string, feats []Feature, led *Ledger, res *Result) error {
	home, err := HomeDir()
	if err != nil {
		return err
	}
	target, created, changed, err := mergeCodexInstructions(dst, feats, false)
	if err != nil {
		return err
	}
	if created {
		res.HomeCreated = append(res.HomeCreated, target)
	} else if changed {
		res.HomeMerged = append(res.HomeMerged, target)
	} else {
		res.HomeSkipped = append(res.HomeSkipped, target)
	}
	files, err := codexFiles(feats)
	if err != nil {
		return err
	}
	if led.Home == nil {
		led.Home = map[string]string{}
	}
	for _, f := range files {
		target := homeTarget(home, f.Path)
		if _, err := os.ReadFile(target); err == nil {
			res.HomeSkipped = append(res.HomeSkipped, target)
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := writeFile(target, f.Content); err != nil {
			return err
		}
		res.HomeCreated = append(res.HomeCreated, target)
		led.Home[f.Path] = Hash(f.Content)
		if err := SaveLedger(dst, *led); err != nil {
			return err
		}
	}
	return nil
}

func updateCodex(dst string, feats []Feature, led *Ledger, opt UpdateOptions, res *UpdateResult) error {
	home, err := HomeDir()
	if err != nil {
		return err
	}
	target, created, changed, err := mergeCodexInstructions(dst, feats, opt.DryRun)
	if err != nil {
		return err
	}
	if created {
		res.HomeCreated = append(res.HomeCreated, target)
	} else if changed {
		res.HomeMerged = append(res.HomeMerged, target)
	} else {
		res.HomeSkipped = append(res.HomeSkipped, target)
	}
	files, err := codexFiles(feats)
	if err != nil {
		return err
	}
	if led.Home == nil {
		led.Home = map[string]string{}
	}
	for _, f := range files {
		target := homeTarget(home, f.Path)
		cur, err := os.ReadFile(target)
		write := true
		switch {
		case errors.Is(err, fs.ErrNotExist):
			res.HomeCreated = append(res.HomeCreated, target)
		case err != nil:
			return err
		case bytes.Equal(cur, f.Content):
			res.HomeSkipped = append(res.HomeSkipped, target)
			write = false
		case opt.Force || led.Home[f.Path] == Hash(cur):
			res.HomeUpdated = append(res.HomeUpdated, target)
		default:
			res.HomeConflicts = append(res.HomeConflicts, Conflict{Path: target, New: target + NewSuffix})
			if !opt.DryRun {
				if err := writeFile(target+NewSuffix, f.Content); err != nil {
					return err
				}
			}
			continue
		}
		if !opt.DryRun {
			if write {
				if err := writeFile(target, f.Content); err != nil {
					return err
				}
			}
			led.Home[f.Path] = Hash(f.Content)
			if err := SaveLedger(dst, *led); err != nil {
				return err
			}
		}
	}
	return nil
}
