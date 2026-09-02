package review

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TodoItem は work/TODO.md の未チェック項目 1 つ。
type TodoItem struct {
	Text   string // "- [ ] " の後ろ
	Line   int    // 1 始まりの行番号
	Date   string // 行が最後に変わった日 YYYY-MM-DD
	Approx bool   // 日付が git でなくファイルの mtime による(git 管理外・未追跡)。表示に ~ を付ける
}

// RepoTodos は 1 リポ分の放置 TODO。Items は日付昇順 → 行番号。
type RepoTodos struct {
	Repo  string
	Items []TodoItem
}

var todoLine = regexp.MustCompile(`^\s*[-*+]\s+\[ \]\s+(.*\S)\s*$`)

// TodoFile は各リポの TODO の置き場(リポ相対)。
const TodoFile = "work/TODO.md"

// StaleTodos は root 直下の各ディレクトリの work/TODO.md を読み、未チェック項目のうち、その行が最後に変わった日が
// cutoff(YYYY-MM-DD)以前のものを集める。行の日付は g が nil でなく、ファイルが git 管理下なら git blame の author-time、
// それ以外はファイルの mtime(Approx)。work/TODO.md が無いディレクトリは飛ばす(正常)。読めないものは warnings に積む。
func StaleTodos(root string, g *Git, cutoff string) (repos []RepoTodos, warnings []string, err error) {
	des, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, fmt.Errorf("root を読めない: %w", err)
	}
	for _, de := range des {
		if !de.IsDir() || strings.HasPrefix(de.Name(), ".") {
			continue
		}
		repoDir := filepath.Join(root, de.Name())
		todoPath := filepath.Join(repoDir, filepath.FromSlash(TodoFile))
		content, err := os.ReadFile(todoPath)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				warnings = append(warnings, fmt.Sprintf("%s/%s: %s", de.Name(), TodoFile, describeErr(err)))
			}
			continue
		}
		lines := splitLines(content)
		dates, approx := lineDates(g, repoDir, todoPath, len(lines))
		var items []TodoItem
		for i, l := range lines {
			m := todoLine.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			d := ""
			if i < len(dates) {
				d = dates[i]
			}
			if d == "" || d > cutoff {
				continue
			}
			items = append(items, TodoItem{Text: m[1], Line: i + 1, Date: d, Approx: approx})
		}
		if len(items) == 0 {
			continue
		}
		sort.SliceStable(items, func(a, b int) bool {
			if items[a].Date != items[b].Date {
				return items[a].Date < items[b].Date
			}
			return items[a].Line < items[b].Line
		})
		repos = append(repos, RepoTodos{Repo: de.Name(), Items: items})
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].Repo < repos[j].Repo })
	return repos, warnings, nil
}

// lineDates は各行の日付を返す。git で取れなければ mtime を全行に当てて approx=true。
func lineDates(g *Git, repoDir, path string, n int) (dates []string, approx bool) {
	if g != nil {
		if ds, ok := g.LineDates(repoDir, TodoFile); ok {
			return ds, false
		}
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, true
	}
	d := fi.ModTime().Format("2006-01-02")
	dates = make([]string, n)
	for i := range dates {
		dates[i] = d
	}
	return dates, true
}

// LineDates は dir 相対 rel の各行が最後に変わった日(YYYY-MM-DD・著者のタイムゾーン)を git blame で返す。
// 未追跡・git 管理外・git の失敗なら ok=false(呼び出し側が mtime に倒す)。
// まだコミットされていない行は blame が「今」を返すので、放置扱いにはならない。
func (g Git) LineDates(dir, rel string) (dates []string, ok bool) {
	out, err := g.run(dir, "blame", "--line-porcelain", "--", rel)
	if err != nil {
		return nil, false
	}
	var t int64
	tz := "+0000"
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "author-time "):
			t, _ = strconv.ParseInt(strings.TrimSpace(line[len("author-time "):]), 10, 64)
		case strings.HasPrefix(line, "author-tz "):
			tz = strings.TrimSpace(line[len("author-tz "):])
		case strings.HasPrefix(line, "\t"): // 行本体。ここまでのヘッダで 1 行分が確定する
			dates = append(dates, formatEpoch(t, tz))
		}
	}
	return dates, true
}

// formatEpoch は UNIX 秒を tz("+0900" 形式)の日付にする。tz が読めなければ UTC。
func formatEpoch(t int64, tz string) string {
	loc := time.UTC
	if len(tz) == 5 {
		sign := 1
		if tz[0] == '-' {
			sign = -1
		}
		h, err1 := strconv.Atoi(tz[1:3])
		m, err2 := strconv.Atoi(tz[3:5])
		if err1 == nil && err2 == nil {
			loc = time.FixedZone(tz, sign*(h*3600+m*60))
		}
	}
	return time.Unix(t, 0).In(loc).Format("2006-01-02")
}

// WriteTodoSection は「## 放置 TODO」の節を書く。weeks は閾値の週数、cutoff はその日付。
func WriteTodoSection(b *strings.Builder, repos []RepoTodos, weeks int, cutoff string) {
	b.WriteString("## 放置 TODO\n\n")
	fmt.Fprintf(b, "%d 週間以上（%s 以前から）動いていない未完了の項目。日付は行が最後に変わった日（`git blame`）。`~` は git で追えず mtime。\n", weeks, cutoff)
	if len(repos) == 0 {
		b.WriteString("\n- なし\n")
		return
	}
	for _, r := range repos {
		b.WriteString("\n### " + r.Repo + "\n")
		for _, it := range r.Items {
			mark := ""
			if it.Approx {
				mark = "~"
			}
			fmt.Fprintf(b, "- [ ] %s（%s%s）\n", it.Text, mark, it.Date)
		}
	}
}

// describeErr は警告向けにエラーを短く言い直す(scan.DescribeErr と同じ規則。import の循環を避けて持つ)。
func describeErr(err error) string {
	if errors.Is(err, fs.ErrNotExist) {
		return "存在しない"
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}
