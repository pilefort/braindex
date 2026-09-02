package retro

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pilefort/braindex/internal/sessions"
)

// ダイジェストの切り詰め(文字数)。読む側(レトロスペクティブ本体の skill)がセッション単位で読めるように抑える。
const (
	UserRunes      = 2000 // ユーザー発話
	AssistantRunes = 300  // 直前のアシスタント本文(SPEC「300 字」)
)

// Input はダイジェスト生成の入力。
type Input struct {
	Sessions    []sessions.Session
	Window      Window         // 窓の外の発話は書かない
	WindowLabel string         // 見出しの window 行(表示用)。空なら "全期間"
	Corrections []*Dictionary  // 訂正の印(★)に使う辞書。無ければ印を付けない
	Sentiment   *Dictionary    // 感情の印(☆)に使う辞書。nil なら付けない
	Loc         *time.Location // 時刻の表示。nil なら UTC
	Home        string         // プロジェクト名の "~" 置換
}

// DigestFile は 1 セッションのダイジェスト。
type DigestFile struct {
	RelPath     string // 出力先からの相対パス(スラッシュ区切り)
	Content     []byte // Markdown(LF)
	Start       time.Time
	Project     string // 表示用(ホームは "~")
	Session     string
	UserTurns   int // 窓の中の人間の発話数
	Corrections int // うち訂正ヒットのある発話数
}

// Result はダイジェストの集合と索引。
type Result struct {
	Files           []DigestFile
	Index           []byte // index.tsv(start・project・session・user_turns・corrections・file)
	Sessions        int
	UserTurns       int
	CorrectionTurns int
}

// Extract はセッションごとのダイジェストと index.tsv を作る(ファイルには書かない。書くのは呼び出し側)。
// 同じ入力からは同じバイト列になる(生成日時などは入れない)。窓に人間の発話が無いセッションは含めない。
func Extract(in Input) Result {
	loc := in.Loc
	if loc == nil {
		loc = time.UTC
	}
	label := in.WindowLabel
	if label == "" {
		label = "全期間"
	}
	var res Result
	for _, s := range in.Sessions {
		f, ok := digest(s, in, loc, label)
		if !ok {
			continue
		}
		res.Files = append(res.Files, f)
		res.Sessions++
		res.UserTurns += f.UserTurns
		res.CorrectionTurns += f.Corrections
	}
	sort.SliceStable(res.Files, func(i, j int) bool {
		if !res.Files[i].Start.Equal(res.Files[j].Start) {
			return res.Files[i].Start.Before(res.Files[j].Start)
		}
		return res.Files[i].Session < res.Files[j].Session
	})
	var b bytes.Buffer
	b.WriteString("start\tproject\tsession\tuser_turns\tcorrections\tfile\n")
	for _, f := range res.Files {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%d\t%d\t%s\n", fmtTime(f.Start, loc, "2006-01-02 15:04"), f.Project, f.Session, f.UserTurns, f.Corrections, f.RelPath)
	}
	res.Index = b.Bytes()
	return res
}

// digest は 1 セッションの md を組む。窓の中の人間の発話ごとに「見出し(時刻・位置・印)→ 直前のアシスタント本文 → 発話」。
func digest(s sessions.Session, in Input, loc *time.Location, label string) (DigestFile, bool) {
	proj := sessions.DisplayPath(s.Project, in.Home)
	var body bytes.Buffer
	var asstTexts []string // 前の人間発話以降のアシスタント本文
	var asstTools []sessions.ToolUse
	var asstLast time.Time // そのうち最後の発話の時刻
	users, corrections := 0, 0
	for _, t := range s.Turns {
		if t.Role == sessions.Assistant {
			if t.Text != "" {
				asstTexts = append(asstTexts, strings.ReplaceAll(t.Text, "\n", " "))
			}
			for _, u := range t.Tools {
				asstTools = addTool(asstTools, u.Name, u.Count)
			}
			asstLast = t.Time
			continue
		}
		asst, tools, last := asstTexts, asstTools, asstLast
		asstTexts, asstTools, asstLast = nil, nil, time.Time{}
		if !in.Window.Contains(t.Time) {
			continue
		}
		users++
		head := fmt.Sprintf("\n### [%s] #%d USER", fmtTime(t.Time, loc, "01-02 15:04"), t.Index)
		if len(in.Corrections) > 0 {
			if ms := Classify(t.Text, in.Corrections...); len(ms) > 0 {
				corrections++
				head += " ★ 訂正候補: " + joinPatterns(ms)
			}
		}
		if in.Sentiment != nil {
			if ms := Classify(t.Text, in.Sentiment); len(ms) > 0 {
				head += " ☆ 感情: " + joinPatterns(ms)
			}
		}
		body.WriteString(head + "\n")
		if len(asst) > 0 || len(tools) > 0 {
			line := "← ASSISTANT [" + fmtTime(last, loc, "01-02 15:04") + "]: " + truncate(strings.Join(asst, " / "), AssistantRunes)
			if len(tools) > 0 {
				line += " [tools: " + joinTools(tools) + "]"
			}
			body.WriteString(line + "\n")
		}
		body.WriteString(truncate(t.Text, UserRunes) + "\n")
	}
	if users == 0 {
		return DigestFile{}, false
	}
	var head bytes.Buffer
	fmt.Fprintf(&head, "# session %s\nproject: %s\n", s.ID, proj)
	if s.Version != "" {
		fmt.Fprintf(&head, "version: %s\n", s.Version)
	}
	fmt.Fprintf(&head, "start: %s\nend: %s\nwindow: %s\nuser_turns: %d\ncorrections: %d\n",
		fmtTime(s.Start, loc, "2006-01-02 15:04"), fmtTime(s.End, loc, "2006-01-02 15:04"), label, users, corrections)
	sid := s.ID
	if utf8.RuneCountInString(sid) > 8 {
		sid = string([]rune(sid)[:8])
	}
	rel := "sessions/" + safeName(proj) + "/" + fmtTime(s.Start, loc, "20060102_1504") + "_" + sid + ".md"
	return DigestFile{
		RelPath:     rel,
		Content:     append(head.Bytes(), body.Bytes()...),
		Start:       s.Start,
		Project:     proj,
		Session:     s.ID,
		UserTurns:   users,
		Corrections: corrections,
	}, true
}

// fmtTime は loc で整形する。ゼロ値は "-"(ファイル名用の layout では "0" 埋め)。
func fmtTime(t time.Time, loc *time.Location, layout string) string {
	if t.IsZero() {
		if layout == "20060102_1504" {
			return "00000000_0000"
		}
		return "-"
	}
	return t.In(loc).Format(layout)
}

// truncate は n 文字を超える本文を切り、残りの文字数を添える。
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + fmt.Sprintf("…(+%d 字)", len(r)-n)
}

func joinPatterns(ms []Match) string {
	ps := make([]string, len(ms))
	for i, m := range ms {
		ps[i] = m.Pattern
	}
	return strings.Join(ps, ", ")
}

func joinTools(tools []sessions.ToolUse) string {
	ps := make([]string, len(tools))
	for i, t := range tools {
		ps[i] = t.Name
		if t.Count > 1 {
			ps[i] += fmt.Sprintf("×%d", t.Count)
		}
	}
	return strings.Join(ps, ", ")
}

func addTool(tools []sessions.ToolUse, name string, n int) []sessions.ToolUse {
	for i := range tools {
		if tools[i].Name == name {
			tools[i].Count += n
			return tools
		}
	}
	return append(tools, sessions.ToolUse{Name: name, Count: n})
}

var unsafeRe = regexp.MustCompile(`[^\w.-]+`)

// safeName はプロジェクト名をディレクトリ名にする(英数字・_・.・- 以外の並びを _ に潰す)。空なら "_"。
func safeName(s string) string {
	s = unsafeRe.ReplaceAllString(s, "_")
	if s == "" {
		return "_"
	}
	return s
}
