package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pilefort/braindex/internal/extract"
	"github.com/pilefort/braindex/internal/fsutil"
	"github.com/pilefort/braindex/internal/notetype"
	"github.com/pilefort/braindex/internal/scan"
	"github.com/pilefort/braindex/internal/textutil"
)

func init() {
	register(&command{name: "type", summary: "内容の種別の候補を出し、チェックした分だけ本文に書く", run: runType})
}

func runType(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "suggest":
			return runTypeSuggest(args[1:], stdout, stderr)
		case "apply":
			return runTypeApply(args[1:], stdout, stderr)
		case "-h", "--help", "help":
			fmt.Fprintln(stderr, "使い方: braindex type suggest [フラグ] / braindex type apply <チェックリスト> [フラグ]")
			return 0
		}
	}
	fmt.Fprintln(stderr, "使い方: braindex type suggest [フラグ] / braindex type apply <チェックリスト> [フラグ]")
	return 1
}

func typeFlags(name string, o *options, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("braindex type "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.config, "config", "", "設定ファイル（既定: カレントの braindex.json）")
	fs.StringVar(&o.root, "root", "", "ノートの root（設定より優先）")
	return fs
}

func runTypeSuggest(args []string, stdout, stderr io.Writer) int {
	var o options
	fs := typeFlags("suggest", &o, stderr)
	var repo, out string
	var asJSON bool
	fs.StringVar(&repo, "repo", "", "このリポだけを対象にする")
	fs.StringVar(&out, "out", "", "候補を保存するファイル（既存ファイルは上書きしない）")
	fs.BoolVar(&asJSON, "json", false, "候補を JSON で出す")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	fail := func(err error) int { fmt.Fprintln(stderr, "braindex type suggest:", err); return 1 }
	if fs.NArg() != 0 {
		return fail(fmt.Errorf("位置引数は受け付けない: %q", fs.Args()))
	}
	cfg, _, date, err := resolve(o)
	if err != nil {
		return fail(err)
	}
	sc, err := scan.Scan(cfg)
	if err != nil {
		return fail(err)
	}
	warnings := append([]string{}, sc.Warnings...)
	suggestions := []notetype.Suggestion{}
	missing := 0
	for _, f := range sc.Files {
		if f.Kind == "decisions" || (repo != "" && f.Repo != repo) {
			continue
		}
		content, err := os.ReadFile(f.Abs)
		if err != nil {
			warnings = append(warnings, f.Rel+": "+scan.DescribeErr(err))
			continue
		}
		value, _, err := notetype.Parse(content)
		if err != nil {
			warnings = append(warnings, f.Rel+": "+err.Error())
			continue
		}
		if value != "" {
			continue
		}
		m := extract.Extract(filepath.Base(f.Abs), content, f.Kind)
		s := notetype.Suggest(f.Rel, m.Title, filepath.Base(f.Abs))
		if len(s.Axes) == 0 {
			missing++
			continue
		}
		suggestions = append(suggestions, s)
	}
	sort.Slice(suggestions, func(i, j int) bool { return suggestions[i].Path < suggestions[j].Path })
	var data []byte
	if asJSON {
		data, err = json.MarshalIndent(suggestions, "", "  ")
		if err != nil {
			return fail(err)
		}
		data = append(data, '\n')
	} else {
		var b strings.Builder
		fmt.Fprintf(&b, "# 内容の種別の候補（braindex type suggest %s・候補 %d 件・候補なし %d 件）\n", date, len(suggestions), missing)
		fmt.Fprintln(&b, "[x] にした行だけを `braindex type apply <このファイル>` が本文に書く。値を直したいときは種別の列を書き換える。")
		for _, s := range suggestions {
			value := s.Candidate
			if value == "" {
				value = "未定（" + strings.Join(s.Axes, "・") + "）"
			}
			fmt.Fprintf(&b, "- [ ] %s | %s | %s | 語: %s\n", value, typeCell(s.Path), typeCell(s.Title), typeCell(strings.Join(s.Terms, ", ")))
		}
		fmt.Fprintf(&b, "\n候補なし %d 件\n", missing)
		data = []byte(b.String())
	}
	if out != "" {
		if _, err := os.Lstat(out); err == nil {
			return fail(fmt.Errorf("既にある: %s（別名を指定する）", out))
		} else if !errors.Is(err, os.ErrNotExist) {
			return fail(err)
		}
		err = fsutil.WriteAtomic(out, data, 0o644)
	} else {
		_, err = stdout.Write(data)
	}
	if err != nil {
		return fail(err)
	}
	for _, w := range warnings {
		fmt.Fprintln(stderr, "braindex type suggest: 警告:", w)
	}
	if len(warnings) > 0 {
		return 2
	}
	return 0
}

// 区切り文字と改行をエスケープし、チェックリストを読み戻せるようにする。
func typeCell(s string) string {
	s = html.EscapeString(s)
	s = strings.ReplaceAll(s, "|", "&#124;")
	s = strings.ReplaceAll(s, "\r", "&#13;")
	return strings.ReplaceAll(s, "\n", "&#10;")
}

func runTypeApply(args []string, stdout, stderr io.Writer) int {
	var o options
	fs := typeFlags("apply", &o, stderr)
	var dry bool
	fs.BoolVar(&dry, "dry-run", false, "本文を書かずに結果を出す")
	// SPEC の「チェックリストの後にフラグ」を受け、フラグを先に書く形も許す。
	list := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		list, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	fail := func(err error) int { fmt.Fprintln(stderr, "braindex type apply:", err); return 1 }
	if list == "" && fs.NArg() == 1 {
		list = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return fail(fmt.Errorf("チェックリストは 1 ファイルを指定する"))
	}
	if list == "" {
		return fail(fmt.Errorf("チェックリストを指定する"))
	}
	cfg, _, _, err := resolve(o)
	if err != nil {
		return fail(err)
	}
	content, err := os.ReadFile(list)
	if err != nil {
		return fail(err)
	}
	written, same, skipped := 0, 0, 0
	// dry-run でも同じパスの重複指定を実行時と同じ順で扱う。
	pending := map[string][]byte{}
	for i, line := range textutil.SplitLines(content) {
		if len(line) < 5 || !strings.EqualFold(line[:5], "- [x]") {
			continue
		}
		parts := strings.Split(strings.TrimSpace(line[5:]), "|")
		path := fmt.Sprintf("%d 行目", i+1)
		skip := func(err error) {
			skipped++
			fmt.Fprintf(stdout, "飛ばした（%v）: %s\n", err, path)
			fmt.Fprintf(stderr, "braindex type apply: 警告: %s: %v\n", path, err)
		}
		if len(parts) < 2 {
			skip(fmt.Errorf("種別とパスの列が無い"))
			continue
		}
		path = html.UnescapeString(strings.TrimSpace(parts[1]))
		value, err := notetype.Normalize(strings.TrimSpace(parts[0]))
		if err != nil || !notetype.Valid(value) {
			skip(fmt.Errorf("種別 %q を失敗・手順・観測に直す", strings.TrimSpace(parts[0])))
			continue
		}
		p, err := typeNotePath(cfg.Root, path)
		if err != nil {
			skip(err)
			continue
		}
		before, ok := pending[p]
		if !ok {
			before, err = os.ReadFile(p)
		}
		if err != nil {
			skip(err)
			continue
		}
		old, _, err := notetype.Parse(before)
		if err != nil {
			skip(err)
			continue
		}
		if old == value {
			same++
			fmt.Fprintf(stdout, "そのまま: %s\n", path)
			continue
		}
		if old != "" {
			skip(fmt.Errorf("既存の種別は %s（手で直す）", old))
			continue
		}
		// 11 行目以降の既存値も黙って置き換えない。
		if len(notetype.Fields(before)) > 0 {
			skip(fmt.Errorf("種別行が先頭 10 行の外にある（手で直す）"))
			continue
		}
		after, err := notetype.Insert(before, value)
		if err != nil {
			skip(err)
			continue
		}
		if !dry {
			st, err := os.Stat(p)
			if err != nil {
				skip(err)
				continue
			}
			if err := fsutil.WriteAtomic(p, after, st.Mode().Perm()); err != nil {
				skip(err)
				continue
			}
		}
		pending[p] = after
		written++
		fmt.Fprintf(stdout, "書いた: %s\n", path)
	}
	fmt.Fprintf(stdout, "書いた %d 件・そのまま %d 件・飛ばした %d 件\n", written, same, skipped)
	if skipped > 0 {
		return 2
	}
	return 0
}

// root 相対の既存ノートだけを許す。リンクの解決後も root の内側であることを確かめる。
func typeNotePath(root, rel string) (string, error) {
	rel = strings.ReplaceAll(rel, "\\", "/")
	if !filepath.IsLocal(filepath.FromSlash(rel)) || strings.Contains(rel, ":") || strings.ToLower(filepath.Ext(rel)) != ".md" {
		return "", fmt.Errorf("root 相対のノートのパスではない: %q", rel)
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	p, err := filepath.EvalSymlinks(filepath.Join(base, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	r, err := filepath.Rel(base, p)
	if err != nil || !filepath.IsLocal(r) {
		return "", fmt.Errorf("root の外を指すパス: %q", rel)
	}
	return p, nil
}
