package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pilefort/braindex/internal/sessions"
)

// Failed は対応する最後の実行が失敗した判定。
const Failed = "FAILED"

var sessionRules = []struct {
	kind     string
	claim    *regexp.Regexp
	commands []string
}{
	{"test", regexp.MustCompile(`(?i)テスト\s*(が|を|は)?\s*(通|パス|成功)|\btests?\s+pass\b|\btests?\s+passed\b`), []string{"go test", "npm test", "npm run test", "pnpm test", "yarn test", "pytest", "cargo test", "make test"}},
	{"build", regexp.MustCompile(`(?i)ビルド\s*(が|は)?\s*(通|成功)|\bvet\s*(が)?\s*通`), []string{"go build", "go vet", "npm run build", "cargo build"}},
	{"commit", regexp.MustCompile(`コミット\s*(した|しました|済み)`), []string{"git commit"}},
	{"push", regexp.MustCompile(`(?i)\bpush\s*(した|しました|済み)|プッシュ\s*(した|しました)`), []string{"git push", "gh pr create"}},
}

var sessionNegative = regexp.MustCompile(`(?i)ない|なかった|ません|未|ず|これから|予定|つもり|\b(no|not|never|will|plan|planned)\b|n't`)

// Session はログのパス、または既定の置き場の ID を照合する。外部への通信はしない。
func Session(target string) []Result {
	s, err := loadVerifySession(target)
	if err != nil {
		return []Result{{Kind: "session", Target: collapseSpace(target), Status: Error, Detail: collapseSpace(err.Error())}}
	}
	return CheckSession(s)
}

// loadVerifySession は既存の Dir を通す。単一ファイルの公開 reader がないため、
// パス指定でも親の置き場を読み、対象のパスだけを取り出す。
func loadVerifySession(target string) (sessions.Session, error) {
	root, path := "", ""
	info, statErr := os.Stat(target)
	if statErr == nil || strings.ContainsAny(target, `/\`) || strings.HasSuffix(target, ".jsonl") {
		if statErr != nil {
			return sessions.Session{}, statErr
		}
		if info.IsDir() || !strings.HasSuffix(target, ".jsonl") {
			return sessions.Session{}, fmt.Errorf(".jsonl ファイルを指定してください")
		}
		var err error
		path, err = filepath.Abs(target)
		if err != nil {
			return sessions.Session{}, err
		}
		root = filepath.Dir(filepath.Dir(path))
	} else {
		var err error
		root, err = sessions.DefaultDir()
		if err != nil {
			return sessions.Session{}, err
		}
	}
	all, warnings, err := (sessions.Dir{Path: root}).Sessions(sessions.Options{IncludeSubagents: true})
	if err != nil {
		return sessions.Session{}, err
	}
	var matches []sessions.Session
	for _, s := range all {
		if (path != "" && s.Path == path) || (path == "" && s.ID == target) {
			matches = append(matches, s)
		}
	}
	if len(matches) != 1 {
		return sessions.Session{}, fmt.Errorf("セッションを一意に読めない（該当 %d 件、人間の発話がないログも対象外）: %s", len(matches), target)
	}
	s := matches[0]
	for _, w := range warnings {
		if strings.Contains(w, s.Path) || strings.Contains(w, filepath.Join(filepath.Dir(s.Path), s.ID, "subagents")) {
			return sessions.Session{}, fmt.Errorf("%s", w)
		}
	}
	return s, nil
}

type sessionExecution struct {
	call     sessions.ToolCall
	subagent bool
}

// CheckSession は本体とサブエージェントのアシスタント発話を照合する。
func CheckSession(s sessions.Session) []Result {
	turns := append([]sessions.Turn(nil), s.Turns...)
	var calls []sessionExecution
	collect := func(ts []sessions.Turn, subagent bool) {
		for _, t := range ts {
			if t.Role != sessions.Assistant {
				continue
			}
			for _, c := range t.Calls {
				if c.Name == "Bash" && !c.Time.IsZero() {
					calls = append(calls, sessionExecution{c, subagent})
				}
			}
		}
	}
	collect(s.Turns, false)
	for _, a := range s.Subagents {
		collect(a.Turns, true)
		turns = append(turns, a.Turns...)
	}
	sort.SliceStable(turns, func(i, j int) bool { return turns[i].Time.Before(turns[j].Time) })
	sort.SliceStable(calls, func(i, j int) bool { return calls[i].call.Time.Before(calls[j].call.Time) })
	var out []Result
	for _, t := range turns {
		if t.Role != sessions.Assistant {
			continue
		}
		for _, sentence := range sessionSentences(t.Text) {
			if sessionNegative.MatchString(sentence) {
				continue
			}
			for _, rule := range sessionRules {
				if !rule.claim.MatchString(sentence) {
					continue
				}
				r := Result{Kind: "session:" + rule.kind, Target: collapseSpace(sentence), Status: NotFound, Detail: "発話より前に対応する実行がない"}
				if t.Time.IsZero() {
					r.Detail = "発話の時刻がないため照合できない"
				} else {
					for _, e := range calls {
						if !e.call.Time.Before(t.Time) {
							break
						}
						matched := false
						for _, command := range rule.commands {
							if strings.Contains(e.call.Command, command) {
								matched = true
								break
							}
						}
						if !matched {
							continue
						}
						r.Status = Found
						if e.call.IsError {
							r.Status = Failed
						}
						command := []rune(collapseSpace(e.call.Command))
						if len(command) > 60 {
							command = command[:60]
						}
						r.Detail = fmt.Sprintf("%s subagent=%t %s", e.call.Time.Format(time.RFC3339Nano), e.subagent, string(command))
						if !e.call.HasResult {
							r.Detail += "（結果の記録がないため成否未確認）"
						}
					}
				}
				out = append(out, r)
			}
		}
	}
	if len(out) == 0 {
		return []Result{{Kind: "session", Target: collapseSpace(s.ID), Status: Found, Detail: "照合対象の主張がない"}}
	}
	return out
}

// sessionSentences は句点・終止符・改行で区切り、フェンスと引用行を除く。
func sessionSentences(text string) []string {
	var out []string
	var fence byte
	fenceLen := 0
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ">") {
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			n := 0
			for n < len(trimmed) && trimmed[n] == trimmed[0] {
				n++
			}
			if fence == 0 {
				fence, fenceLen = trimmed[0], n
			} else if trimmed[0] == fence && n >= fenceLen && strings.TrimSpace(trimmed[n:]) == "" {
				fence = 0
			}
			continue
		}
		if fence != 0 || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			continue
		}
		start := 0
		for i, r := range line {
			if strings.ContainsRune("。！？.!?", r) {
				end := i + len(string(r))
				if s := strings.TrimSpace(line[start:end]); s != "" {
					out = append(out, s)
				}
				start = end
			}
		}
		if s := strings.TrimSpace(line[start:]); s != "" {
			out = append(out, s)
		}
	}
	return out
}
