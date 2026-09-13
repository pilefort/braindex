package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pilefort/braindex/internal/config"
	"github.com/pilefort/braindex/internal/indexdata"
	"github.com/pilefort/braindex/internal/interest"
	"github.com/pilefort/braindex/internal/links"
	"github.com/pilefort/braindex/internal/retro"
	"github.com/pilefort/braindex/internal/scan"
	"github.com/pilefort/braindex/internal/sessions"
)

func init() {
	register(&command{name: "related", summary: "いまの作業（リポ・ISSUE・直近のセッション・引数の語）に関連するノートを索引と links.tsv から並べる（手元だけ・規則ベース）", run: runRelated})
}

type relatedOutput struct {
	Repo  string       `json:"repo"`
	Terms []links.Term `json:"terms"`
	links.Ranking
	Warnings []string `json:"warnings"`
}

func runRelated(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("braindex related", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var cfgPath, repo, issue, sessionID string
	var days, limit int
	var noMention, asJSON bool
	fs.StringVar(&cfgPath, "config", defaultConfig, "設定ファイル")
	fs.StringVar(&repo, "repo", "", "いまのリポ名（省略時はカレントから推定）")
	fs.StringVar(&issue, "issue", "", "材料にする work/ISSUE-*.md 1 枚")
	fs.StringVar(&sessionID, "session", "", "材料にするセッション ID 1 つ")
	fs.IntVar(&days, "days", 7, "直近のセッションを見る日数")
	fs.IntVar(&limit, "limit", 10, "各節の件数上限")
	fs.BoolVar(&noMention, "no-mention", false, "mention の辺を使わない")
	fs.BoolVar(&asJSON, "json", false, "JSON で出す")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	fail := func(err error) int { fmt.Fprintln(stderr, "braindex related:", err); return 1 }
	if days <= 0 || limit <= 0 {
		return fail(fmt.Errorf("-days と -limit は 1 以上"))
	}
	if sessionID != "" && (strings.ContainsAny(sessionID, `/\*?[]:`) || sessionID == "." || sessionID == "..") {
		return fail(fmt.Errorf("-session はパスではなく ID を指定する"))
	}
	fc, found, err := config.Load(cfgPath)
	if err != nil {
		return fail(err)
	}
	if !found {
		return fail(fmt.Errorf("設定ファイルが無い: %s", cfgPath))
	}
	cfg, catPath, _, err := resolve(options{config: cfgPath})
	if err != nil {
		return fail(err)
	}
	root, err := filepath.Abs(cfg.Root)
	if err != nil {
		return fail(err)
	}
	if repo == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fail(err)
		}
		rel, err := filepath.Rel(root, cwd)
		if err != nil || rel == ".." || strings.HasPrefix(filepath.ToSlash(rel), "../") {
			return fail(fmt.Errorf("カレントが root の外です。-repo を指定する"))
		}
		// SplitRepo expects a file below the repository, including when cwd is the repository root.
		var ok bool
		repo, _, ok = scan.SplitRepo(cfg, filepath.ToSlash(filepath.Join(rel, "_related_")))
		if !ok {
			return fail(fmt.Errorf("カレントからリポを決められません。-repo を指定する"))
		}
	}
	if strings.ContainsAny(repo, `\:*?[]`) || strings.HasPrefix(repo, "/") {
		return fail(fmt.Errorf("-repo は root 相対のリポ名を指定する"))
	}
	for _, s := range strings.Split(repo, "/") {
		if s == "" || s == "." || s == ".." {
			return fail(fmt.Errorf("-repo のリポ名が不正"))
		}
	}
	repoDir := filepath.Join(root, filepath.FromSlash(repo))
	if st, err := os.Stat(repoDir); err != nil || !st.IsDir() {
		return fail(fmt.Errorf("リポのディレクトリが無い: %s", repo))
	}
	b, err := os.ReadFile(catPath)
	if err != nil {
		return fail(fmt.Errorf("索引を読めない: %w", err))
	}
	entries, err := indexdata.ParseCatalog(b)
	if err != nil {
		return fail(err)
	}
	out := relatedOutput{Repo: repo, Terms: []links.Term{}, Warnings: []string{}}
	warn := func(s string) { out.Warnings = append(out.Warnings, s) }
	materials := map[string][]string{"ISSUE": {}, "セッション": {}, "引数": fs.Args()}
	var issues []string
	if issue != "" {
		if filepath.Base(filepath.Dir(issue)) != "work" || !strings.HasPrefix(filepath.Base(issue), "ISSUE-") || !strings.HasSuffix(issue, ".md") {
			return fail(fmt.Errorf("-issue は work/ISSUE-*.md を指定する"))
		}
		issues = []string{issue}
	} else {
		issues, err = filepath.Glob(filepath.Join(repoDir, "work", "ISSUE-*.md"))
		if err != nil {
			return fail(err)
		}
	}
	sort.Strings(issues)
	if len(issues) == 0 {
		warn("ISSUE が無いので ISSUE の語は無しで続ける")
	}
	for _, p := range issues {
		b, err := os.ReadFile(p)
		if err != nil {
			return fail(fmt.Errorf("ISSUE を読めない: %w", err))
		}
		materials["ISSUE"] = append(materials["ISSUE"], string(b))
	}
	home, _ := os.UserHomeDir()
	sessDir := retro.ResolvePath(fc.Retro.SessionsDir, filepath.Dir(cfgPath), home)
	if sessDir == "" {
		sessDir, err = sessions.DefaultDir()
		if err != nil {
			return fail(err)
		}
	}
	if _, err := os.Stat(sessDir); errors.Is(err, os.ErrNotExist) {
		warn("セッションの置き場が無いのでセッションの語は無しで続ける")
	} else if err != nil {
		return fail(err)
	} else {
		opts := sessions.Options{Since: time.Now().AddDate(0, 0, -days), UnderRoot: repoDir}
		if sessionID != "" {
			opts = sessions.Options{}
		}
		ss, ws, err := (sessions.Dir{Path: sessDir}).Sessions(opts)
		if err != nil {
			return fail(err)
		}
		out.Warnings = append(out.Warnings, ws...)
		matched := 0
		for _, s := range ss {
			if sessionID != "" && s.ID != sessionID {
				continue
			}
			matched++
			for _, turn := range s.HumanTurns() {
				if !turn.Boilerplate {
					materials["セッション"] = append(materials["セッション"], turn.Text)
				}
			}
		}
		if sessionID != "" && matched != 1 {
			return fail(fmt.Errorf("セッション ID は 1 件に特定できるものを指定する（一致 %d 件）", matched))
		}
	}
	out.Terms = relatedTerms(materials, repo)
	edgePath := filepath.Join(filepath.Dir(catPath), links.FileName)
	edges := []links.Edge{}
	connection := "つながり: 無し（braindex で生成する）"
	if b, err := os.ReadFile(edgePath); errors.Is(err, os.ErrNotExist) {
		warn("links.tsv が無いので語の点だけで並べる")
	} else if err != nil {
		return fail(err)
	} else {
		edges, err = links.Parse(b)
		if err != nil {
			return fail(err)
		}
		mode := "mention を含む"
		if noMention {
			mode = "mention を除く"
		}
		connection = fmt.Sprintf("つながり: %s（%d 本。%s）", filepath.ToSlash(edgePath), len(edges), mode)
	}
	if len(out.Terms) == 0 {
		warn("材料が無い")
	}
	out.Ranking = links.Rank(entries, edges, out.Terms, repo, links.RankOptions{Limit: limit, NoMention: noMention})
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return fail(err)
		}
	} else {
		if err := renderRelated(stdout, out, connection); err != nil {
			return fail(err)
		}
	}
	for _, w := range out.Warnings {
		fmt.Fprintln(stderr, "braindex related: 警告:", w)
	}
	if len(out.Warnings) > 0 {
		return 2
	}
	return 0
}

func relatedTerms(materials map[string][]string, repo string) []links.Term {
	excluded := map[string]bool{}
	for _, seg := range strings.Split(repo, "/") {
		excluded[strings.ToLower(seg)] = true
		for _, w := range interest.Words(seg) {
			excluded[w] = true
		}
	}
	byWord := map[string]*links.Term{}
	for _, src := range []string{"ISSUE", "セッション", "引数"} {
		weight := 2
		if src == "セッション" {
			weight = 1
		}
		seen := map[string]bool{}
		for _, text := range materials[src] {
			for _, w := range interest.Words(interest.StripURLs(text)) {
				if excluded[w] || seen[w] {
					continue
				}
				seen[w] = true
				if byWord[w] == nil {
					byWord[w] = &links.Term{Word: w, Sources: []string{}}
				}
				t := byWord[w]
				t.Weight = max(t.Weight, weight)
				t.Sources = append(t.Sources, src)
			}
		}
	}
	out := make([]links.Term, 0, len(byWord))
	for _, t := range byWord {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Weight != out[j].Weight {
			return out[i].Weight > out[j].Weight
		}
		return out[i].Word < out[j].Word
	})
	return out
}

func renderRelated(w io.Writer, out relatedOutput, connection string) error {
	var b strings.Builder
	words := []string{}
	counts := map[string]int{}
	for i, t := range out.Terms {
		if i < 20 {
			words = append(words, t.Word)
		}
		for _, s := range t.Sources {
			counts[s]++
		}
	}
	suffix := ""
	if len(out.Terms) > 20 {
		suffix = " …"
	}
	fmt.Fprintf(&b, "入力の語: %s%s（出典の内訳: ISSUE %d・セッション %d・引数 %d）\n%s\n", strings.Join(words, ", "), suffix, counts["ISSUE"], counts["セッション"], counts["引数"], connection)
	section := func(title string, rr []links.Ranked, local bool) {
		fmt.Fprintf(&b, "\n## %s\n", title)
		for i, r := range rr {
			p := r.Path
			if local {
				p = strings.TrimPrefix(p, out.Repo+"/")
			}
			matches := []string{}
			for _, m := range r.Words {
				matches = append(matches, fmt.Sprintf("%s(%d; %s)", m.Word, m.Weight, strings.Join(m.Where, ",")))
			}
			fmt.Fprintf(&b, "%d. %s  %.1f  語: %s  つながり: %g\n", i+1, p, r.Score, strings.Join(matches, ", "), r.Links.Score())
		}
	}
	section("このリポ（"+out.Repo+"）", out.ThisRepo, true)
	section("他のリポ", out.OtherRepos, false)
	_, err := io.WriteString(w, b.String())
	return err
}
