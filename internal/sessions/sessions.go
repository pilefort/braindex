// Package sessions は Claude Code のセッションログ(~/.claude/projects/<slug>/*.jsonl)を読み、
// 人間の発話とアシスタント本文を時系列に並べる。レトロスペクティブ(retro)とニュースサジェスト(news)が
// 共有する入力層。
//
// 本文は返すだけで、どこにも書かず送らない。判定はすべて規則ベース(同じログ → 同じ結果)。
// 他のエージェントのログは v1 では対象外だが、読み取りは Source インターフェースの背後に置き、後から足せる形にする。
//
// ログの形式(2026-09-02 に version 2.1.258 の実ログで確認): 1 行 1 JSON。使う項目は type(user / assistant)・
// isSidechain・isMeta・timestamp・cwd・version・message.id・message.content(文字列か、
// {type: text | tool_use | tool_result | thinking} のブロック列)。それ以外の type の行(attachment・system・
// ai-title など)は読まない。
package sessions

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Role は発話の主体。
type Role string

const (
	User      Role = "user"
	Assistant Role = "assistant"
)

// ToolUse はアシスタントが呼んだツールの名前と回数。
type ToolUse struct {
	Name  string
	Count int
}

// Usage は 1 メッセージの使用量。分割行の値は加算せず最後の値を使う。
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// ToolCall はツール呼び出しと、後続行にある結果の対応。
type ToolCall struct {
	ID, Name, Command  string
	Time               time.Time
	IsError, HasResult bool
}

// Turn は 1 発話。
type Turn struct {
	UUID string // thinking だけの行も含む、そのメッセージの最初の行
	// ParentUUID は捨てた tool_result・attachment 行を指すことがあり、Turn だけでは親子を辿り切れない。
	ParentUUID string
	Usage      *Usage // アシスタントだけ。同じ message.id の最後の行の値
	Calls      []ToolCall
	Role       Role
	Index      int       // 人間の発話の通し番号(そのセッションで何番目か。1 始まり。除外規則を通った発話だけを数える)。アシスタントは 0
	Time       time.Time // ログの timestamp(UTC)。無ければゼロ値
	Text       string    // 人間: 打った本文(<system-reminder> ブロックは除く)。アシスタント: text ブロックを改行で連結
	Tools      []ToolUse // アシスタントが呼んだツール(出現順)。人間は nil
	// Boilerplate は「同じ冒頭の発話が複数セッションに現れる」= 機械が流し込んだ指示と判定された発話。
	// MarkBoilerplate が立てる。retro・news・learn はこの発話を数えない。
	Boilerplate bool
}

// Session は 1 つのセッションログ。
type Session struct {
	Subagents []Subagent
	ID        string    // ファイル名から .jsonl を除いたもの
	Path      string    // 読んだファイル
	Project   string    // 最初に現れた cwd。無ければ置き場のディレクトリ名(slug)
	Version   string    // 最初に現れた version
	Start     time.Time // 最初の発話の時刻(時刻のある発話が無ければゼロ値)
	End       time.Time // 最後の発話の時刻
	Turns     []Turn    // 時系列
	UserTurns int       // 人間の発話数(最後の Index と同じ)
}

// Subagent は本体とは別ファイルに記録された発話。定型判定や発話の除外規則は掛けない。
type Subagent struct {
	AgentID, AgentType, Description, ToolUseID string
	Path                                       string
	Turns                                      []Turn
	Start, End                                 time.Time
}

// TotalUsage は本体の各 Turn の使用量の和。サブエージェントは含めない。
func (s Session) TotalUsage() Usage {
	var total Usage
	for _, t := range s.Turns {
		if u := t.Usage; u != nil {
			total.InputTokens += u.InputTokens
			total.OutputTokens += u.OutputTokens
			total.CacheCreationInputTokens += u.CacheCreationInputTokens
			total.CacheReadInputTokens += u.CacheReadInputTokens
		}
	}
	return total
}

// HumanTurns は人間の発話だけを時系列で返す。
func (s Session) HumanTurns() []Turn {
	out := make([]Turn, 0, s.UserTurns)
	for _, t := range s.Turns {
		if t.Role == User {
			out = append(out, t)
		}
	}
	return out
}

// Options は読み取りの条件。
type Options struct {
	// IncludeSubagents が false ならサブエージェントのディレクトリを開かない。
	IncludeSubagents bool
	// Since を指定すると、それより前に最終更新されたファイル(mtime)は開かない(全件だと GB 単位になるため)。
	// 開いたセッションの発話は Since より前のものも含めて全部返す。発話単位の絞り込みは呼び出し側が Turn.Time で行う。
	// mtime は「そのファイルに最後に書いた時刻」なので、中の timestamp がそれより後になることはない。
	Since time.Time

	// UnderRoot を指定すると、Session.Project(最初の cwd)がそのディレクトリの配下にあるセッションだけ返す。
	// 空なら全部返す。cwd の無いセッション(置き場のディレクトリ名しか分からないもの)は、
	// 配下かどうか判定できないので除いて件数を warning にまとめる。
	// hub の root の外(索引に載らないリポ・OS のシステムディレクトリなど)で交わした会話が
	// 訂正率や関心プロファイルに混ざるのを防ぐ(設計レビュー 2026-09-06 M2)。
	UnderRoot string

	// BoilerplateSessions は「同じ冒頭の発話が何セッションに出たら定型とみなすか」。
	// 0 なら DefaultBoilerplateSessions。読み取りの最後に MarkBoilerplate を掛けるので、
	// retro・news・learn のどこから読んでも同じ発話が定型になる。
	BoilerplateSessions int
}

// Source はセッションログの供給元。
type Source interface {
	// Sessions は opts に合うセッションを開始時刻の昇順(同時刻は ID 順)で返す。
	// 人間の発話が 1 つも無いセッションは含めない。読めないファイル・JSON でない行は警告にして飛ばし、
	// エラーにするのは置き場そのものが読めないときだけ。
	Sessions(opts Options) (sessions []Session, warnings []string, err error)
}

// Dir は <Path>/<slug>/*.jsonl を読む Source(Claude Code の置き方)。Path 直下のファイルは読まない。
type Dir struct {
	Path string
}

// DefaultDir は Claude Code の既定の置き場 ~/.claude/projects。
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("ホームディレクトリが分からない: %w", err)
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

// Sessions は Source を実装する。
func (d Dir) Sessions(opts Options) ([]Session, []string, error) {
	slugs, err := os.ReadDir(d.Path)
	if err != nil {
		// 置き場が無いのは「まだ Claude Code を使っていない」か置き場の指定違い。初めての利用者が最初に見る文言なので
		// OS の生エラー(open …: The system cannot find the path specified.)でなく、何が無くていつ作られるかを言う
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, fmt.Errorf("セッションログの置き場が無い: %s(Claude Code を使うと ~/.claude/projects に作られる)", d.Path)
		}
		return nil, nil, fmt.Errorf("セッションログの置き場を読めない: %w", err)
	}
	var out []Session
	var warns []string
	versions := map[string]int{} // 読んだファイルの版 → 件数
	outside, unknownCwd := 0, 0  // UnderRoot の外・cwd が分からず除いたセッション数
	for _, slug := range slugs {
		if !slug.IsDir() {
			continue
		}
		slugDir := filepath.Join(d.Path, slug.Name())
		files, err := os.ReadDir(slugDir)
		if err != nil {
			warns = append(warns, fmt.Sprintf("セッションログの置き場 %s: 読めない: %v", slugDir, err))
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			if !opts.Since.IsZero() {
				if info, err := f.Info(); err == nil && info.ModTime().Before(opts.Since) {
					continue
				}
			}
			s, w := readSession(filepath.Join(slugDir, f.Name()), slug.Name())
			warns = append(warns, w...)
			// 版は開いたファイル全部から数える。人間の発話が 0 のセッションも含めるのは、
			// 形式が変わって発話を拾えなくなった回こそ「読んだのに使えなかった」側に落ちるため
			versions[s.Version]++
			if s.UserTurns == 0 {
				continue
			}
			if opts.UnderRoot != "" {
				switch under, known := underRoot(s.Project, opts.UnderRoot); {
				case !known:
					unknownCwd++
					continue
				case !under:
					outside++
					continue
				}
			}
			if opts.IncludeSubagents {
				subagents, w := readSubagents(filepath.Join(slugDir, s.ID, "subagents"))
				s.Subagents = subagents
				warns = append(warns, w...)
			}
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Start.Equal(out[j].Start) {
			return out[i].Start.Before(out[j].Start)
		}
		return out[i].ID < out[j].ID
	})
	if outside > 0 {
		warns = append(warns, fmt.Sprintf("%s の外のセッション %d 件を除いた(全部見るなら設定 retro.all_projects を true にする)", opts.UnderRoot, outside))
	}
	if unknownCwd > 0 {
		warns = append(warns, fmt.Sprintf("作業ディレクトリが分からないセッション %d 件を除いた", unknownCwd))
	}
	// 定型(機械が流し込んだ指示)の印を付ける。数えるかどうかは呼び出し側が Turn.Boilerplate で決める
	MarkBoilerplate(out, opts.BoilerplateSessions)
	// 読んだログの版が確認済みの範囲の外なら伝える。除外規則は変えない。
	warns = append(warns, unknownVersionWarnings(versions)...)
	return out, warns, nil
}

// row はログ 1 行のうち使う項目。
type row struct {
	UUID        string `json:"uuid"`
	ParentUUID  string `json:"parentUuid"`
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	IsMeta      bool   `json:"isMeta"`
	Timestamp   string `json:"timestamp"`
	Cwd         string `json:"cwd"`
	Version     string `json:"version"`
	Message     struct {
		Usage   *Usage          `json:"usage"`
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// block は message.content のブロック。結果本文は保持しない。
type block struct {
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Input     json.RawMessage `json:"input"`
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Name      string          `json:"name"`
}

var (
	userMark      = []byte(`"user"`)
	assistantMark = []byte(`"assistant"`)
)

// reader は 1 ファイルを読む間の状態。
type reader struct {
	subagent  bool
	messages  map[string]*messageInfo
	calls     map[string]callLocation
	s         Session
	lastMsgID string // 直前に足したアシスタント発話の message.id。同じ id の行は 1 発話に束ねる
	bad       int    // JSON として読めなかった行数
}

type messageInfo struct {
	uuid, parentUUID string
	turn             int // 最初に保持した Turn。thinking だけの間は -1
}

type callLocation struct{ turn, call int }

func readSession(path, slug string) (Session, []string) {
	return readLog(path, slug, false)
}

func readLog(path, slug string, subagent bool) (Session, []string) {
	r := reader{s: Session{ID: strings.TrimSuffix(filepath.Base(path), ".jsonl"), Path: path}, subagent: subagent}
	f, err := os.Open(path)
	if err != nil {
		return r.s, []string{fmt.Sprintf("セッションログ %s: 読めない: %v", path, err)}
	}
	defer f.Close()
	var warns []string
	// 1 行が数 MB(tool_result の中身)になるので Scanner の上限に掛からないよう ReadBytes で読む
	br := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		if t := bytes.TrimSpace(line); len(t) > 0 {
			r.line(t)
		}
		if err != nil {
			if err != io.EOF {
				warns = append(warns, fmt.Sprintf("セッションログ %s: 途中までしか読めなかった: %v", path, err))
			}
			break
		}
	}
	if r.bad > 0 {
		warns = append(warns, fmt.Sprintf("セッションログ %s: JSON でない %d 行を飛ばした", path, r.bad))
	}
	if r.s.Project == "" {
		r.s.Project = slug
	}
	return r.s, warns
}

// line は 1 行を処理する。line は前後の空白を除いてあり、空ではない。
func (r *reader) line(line []byte) {
	if line[0] != '{' || line[len(line)-1] != '}' {
		r.bad++ // 途中で切れた行やゴミ
		return
	}
	// user / assistant の行は必ず "user" か "assistant" の文字列を含む。含まない行(attachment・system など)は
	// JSON を解く前に落とす(全件読むと GB 単位なので、解かずに済む行は解かない)
	if !bytes.Contains(line, userMark) && !bytes.Contains(line, assistantMark) {
		return
	}
	var o row
	if err := json.Unmarshal(line, &o); err != nil {
		r.bad++
		return
	}
	if (o.Type != "user" && o.Type != "assistant") || (o.IsSidechain && !r.subagent) {
		return // サブエージェント(isSidechain)の行は人間の発話でもアシスタント本文でもない
	}
	if r.s.Project == "" {
		r.s.Project = o.Cwd
	}
	if r.s.Version == "" {
		r.s.Version = o.Version
	}
	ts := parseTime(o.Timestamp)
	text, tools, hasText := contentParts(o.Message.Content)
	switch o.Type {
	case "user":
		r.linkResults(o.Message.Content)
		if !hasText {
			return // tool_result だけの行
		}
		// isMeta は Skill 起動時の文脈・画像の貼り付け・別セッションからのメッセージなど、人が打っていない行
		// (2026-09-03 に実ログで確認。除外は同日の決定 → manual/retro.md「決めたこと」)
		if !r.subagent && (o.IsMeta || ExcludeReason(text) != "") {
			return
		}
		r.s.UserTurns++
		r.s.Turns = append(r.s.Turns, Turn{Role: User, Index: r.s.UserTurns, Time: ts, Text: stripReminders(text), UUID: o.UUID, ParentUUID: o.ParentUUID})
		r.lastMsgID = ""
	case "assistant":
		info := &messageInfo{uuid: o.UUID, parentUUID: o.ParentUUID, turn: -1}
		if o.Message.ID != "" {
			if r.messages == nil {
				r.messages = make(map[string]*messageInfo)
			}
			if previous := r.messages[o.Message.ID]; previous != nil {
				info = previous
			} else {
				r.messages[o.Message.ID] = info
			}
		}
		if info.turn >= 0 {
			r.s.Turns[info.turn].Usage = o.Message.Usage
		}
		if text == "" && len(tools) == 0 {
			return // thinking だけの行など
		}
		if o.Message.ID != "" && o.Message.ID == r.lastMsgID {
			// 同じ API メッセージの続きの行(Claude Code はブロックごとに 1 行書く)。1 発話に束ねる
			last := &r.s.Turns[len(r.s.Turns)-1]
			if text != "" {
				if last.Text != "" {
					last.Text += "\n"
				}
				last.Text += text
			}
			for _, t := range tools {
				last.Tools = addTool(last.Tools, t.Name, t.Count)
			}
		} else {
			r.s.Turns = append(r.s.Turns, Turn{Role: Assistant, Time: ts, Text: text, Tools: tools, UUID: info.uuid, ParentUUID: info.parentUUID})
			r.lastMsgID = o.Message.ID
		}
		if info.turn < 0 {
			info.turn = len(r.s.Turns) - 1
			r.s.Turns[info.turn].Usage = o.Message.Usage
		}
		r.addCalls(o.Message.Content, ts)
	}
	if !ts.IsZero() {
		if r.s.Start.IsZero() {
			r.s.Start = ts
		}
		r.s.End = ts
	}
}

// addCalls は保持した発話に呼び出しを追加し、結果との対応用に位置を記録する。
func (r *reader) addCalls(raw json.RawMessage, ts time.Time) {
	var blocks []block
	if json.Unmarshal(raw, &blocks) != nil {
		return
	}
	i := len(r.s.Turns) - 1
	for _, b := range blocks {
		if b.Type != "tool_use" {
			continue
		}
		call := ToolCall{ID: b.ID, Name: b.Name, Time: ts}
		if b.Name == "Bash" {
			var input struct {
				Command string `json:"command"`
			}
			if json.Unmarshal(b.Input, &input) == nil {
				call.Command = input.Command
			}
		}
		t := &r.s.Turns[i]
		if b.ID != "" {
			if r.calls == nil {
				r.calls = make(map[string]callLocation)
			}
			r.calls[b.ID] = callLocation{i, len(t.Calls)}
		}
		t.Calls = append(t.Calls, call)
	}
}

func (r *reader) linkResults(raw json.RawMessage) {
	var blocks []block
	if json.Unmarshal(raw, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		if b.Type != "tool_result" {
			continue
		}
		if loc, ok := r.calls[b.ToolUseID]; ok {
			call := &r.s.Turns[loc.turn].Calls[loc.call]
			call.HasResult, call.IsError = true, b.IsError
		}
	}
}

func readSubagents(root string) ([]Subagent, []string) {
	var out []Subagent
	var warns []string
	// WalkDir のファイル名順で返す。journal.jsonl や meta.json はログとして読まない。
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path != root || !errors.Is(err, fs.ErrNotExist) {
				warns = append(warns, fmt.Sprintf("サブエージェントログ %s: 読めない: %v", path, err))
			}
			return nil
		}
		if d.IsDir() || !strings.HasPrefix(d.Name(), "agent-") || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		s, w := readLog(path, "", true)
		warns = append(warns, w...)
		a := Subagent{AgentID: strings.TrimSuffix(strings.TrimPrefix(d.Name(), "agent-"), ".jsonl"), Path: path, Turns: s.Turns, Start: s.Start, End: s.End}
		if err := readSubagentMeta(strings.TrimSuffix(path, ".jsonl")+".meta.json", &a); err != nil {
			warns = append(warns, fmt.Sprintf("サブエージェントのメタ情報 %s: 読めない: %v", path, err))
		}
		out = append(out, a)
		return nil
	})
	return out, warns
}

// readSubagentMeta にキー名の対応を集約する。agentType・description・toolUseId は 2.1.269 の実ログで確認（2026-09-13）。他の版は未確認。
func readSubagentMeta(path string, a *Subagent) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var meta struct {
		AgentType   string `json:"agentType"`
		Description string `json:"description"`
		ToolUseID   string `json:"toolUseId"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return err
	}
	a.AgentType, a.Description, a.ToolUseID = meta.AgentType, meta.Description, meta.ToolUseID
	return nil
}

// contentParts は message.content(文字列か、ブロックの列)を本文と tool_use に分ける。
// 本文は text ブロック(空白だけのものは除く)を改行で連結したもの。hasText は text ブロック(か文字列本文)が
// 1 つでもあったか(tool_result だけの行を見分ける。本文が空でも true になりうる)。
func contentParts(raw json.RawMessage) (text string, tools []ToolUse, hasText bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", nil, false
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return "", nil, false
		}
		return s, nil, true
	}
	var blocks []block
	if json.Unmarshal(raw, &blocks) != nil {
		return "", nil, false
	}
	var texts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			hasText = true
			if strings.TrimSpace(b.Text) != "" {
				texts = append(texts, b.Text)
			}
		case "tool_use":
			tools = addTool(tools, b.Name, 1)
		}
	}
	return strings.Join(texts, "\n"), tools, hasText
}

// addTool は name の回数を n 増やす(初出なら末尾に足す。出現順を保つ)。
func addTool(tools []ToolUse, name string, n int) []ToolUse {
	for i := range tools {
		if tools[i].Name == name {
			tools[i].Count += n
			return tools
		}
	}
	return append(tools, ToolUse{Name: name, Count: n})
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

var reminderRe = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)

// stripReminders は Claude Code が差し込む <system-reminder> ブロックを落として前後の空白を除く。
// 人間が打った本文だけを残すため(辞書照合の偽ヒットの元にもなる)。
func stripReminders(text string) string {
	return strings.TrimSpace(reminderRe.ReplaceAllString(text, ""))
}

// ExcludeReason は user 行の本文を人間の発話に数えない理由を返す。数えるなら ""。
// 判定は <system-reminder> を落とした後の本文に掛ける(reminder の後ろにコマンドが続く行も command にする)。
//
//	empty             — reminder を除くと空
//	command           — スラッシュコマンド(<command-name>)とその出力(<local-command-...>)
//	continuation      — 文脈が尽きたときの継続要約
//	interrupt         — ユーザーによる中断
//	task-notification — サブエージェントの完了通知(2026-09-03 追加 → manual/retro.md「決めたこと」)
//
// 行の属性による除外(isSidechain・isMeta・tool_result だけの行)は Sessions が行う。
func ExcludeReason(text string) string {
	t := stripReminders(text)
	switch {
	case t == "":
		return "empty"
	case strings.HasPrefix(t, "<command-name>"), strings.HasPrefix(t, "<local-command"):
		return "command"
	case strings.HasPrefix(t, "This session is being continued"):
		return "continuation"
	case strings.Contains(t, "[Request interrupted by user"):
		return "interrupt"
	case strings.HasPrefix(t, "<task-notification>"):
		return "task-notification"
	case strings.HasPrefix(t, "[braindex-"):
		// braindex 自身が claude -p で流し込んだプロンプト(news の LLM 補助など)。
		// 人が打った発話ではないので数えない(設計レビュー 2026-09-06 M11)
		return "braindex-tool"
	}
	return ""
}

// DisplayPath は path の先頭が home なら "~" に置き換える(表示用。ログの cwd は絶対パスなので、
// そのまま出すと個人識別子が混じる)。home が空か、境界が一致しなければそのまま返す。
func DisplayPath(path, home string) string {
	home = strings.TrimRight(home, `/\`)
	if path == "" || home == "" || !strings.HasPrefix(path, home) {
		return path
	}
	rest := path[len(home):]
	if rest == "" {
		return "~"
	}
	if rest[0] == '/' || rest[0] == '\\' {
		return "~" + rest
	}
	return path
}

// underRoot は project(セッションの最初の cwd)が root の配下かを返す。
// known=false は「cwd がログに無く、置き場のディレクトリ名(slug)しか分からない」場合。
// slug は区切りをハイフンに潰した文字列で元のパスに戻せないので、配下かどうかを判定しない。
//
// 大文字小文字は Windows でだけ無視する(同じディレクトリがドライブ文字の大小違いで書かれる)。
func underRoot(project, root string) (under, known bool) {
	// cwd がログに無いと Session.Project は置き場のディレクトリ名(slug)になる。slug は区切りを
	// ハイフンに潰した文字列で元のパスに戻せないので、配下かどうかを判定しない。
	// 区切りを含むかどうかで見分ける(POSIX のログを Windows で読むこともあるので filepath.IsAbs は使わない)。
	if !strings.ContainsAny(project, `/\`) {
		return false, false
	}
	rel, err := filepath.Rel(caseFold(root), caseFold(project))
	if err != nil {
		return false, true // 別ドライブ・別の形式のパス。root の配下ではない
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return false, true
	}
	return true, true
}

// caseFold は Windows でだけ小文字に畳む。filepath.Rel はどの OS でもバイトで比べるので、
// Windows では大文字小文字違いの同じパスが「配下でない」と判定されてしまう。
func caseFold(p string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}
