package news

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pilefort/braindex/internal/feed"
)

// Seen は既読。記事 ID → 初めて見た日 YYYY-MM-DD。
type Seen map[string]string

// LoadSeen は既読ファイルを読む。無ければ空(エラーにしない)。
func LoadSeen(path string) (Seen, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Seen{}, nil
		}
		return nil, fmt.Errorf("既読ファイルを読めない: %w", err)
	}
	var s Seen
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("既読ファイル %s: %w(壊れていれば削除すると全件が新着になる)", path, err)
	}
	if s == nil {
		s = Seen{}
	}
	return s, nil
}

// Save は既読ファイルを書く。キーを昇順に 1 行 1 件で書くので、同じ内容なら同じバイト列になる。
// 書き込みは原子的(途中で止まっても前回の既読が残る。半端な JSON は LoadSeen が読めず、全件が新着に戻ってしまう)。
func (s Seen) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeAtomic(path, s.Marshal(), 0o644)
}

// Marshal は Save が書くバイト列。
func (s Seen) Marshal() []byte {
	ids := make([]string, 0, len(s))
	for id := range s {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var sb strings.Builder
	sb.WriteString("{")
	for i, id := range ids {
		if i > 0 {
			sb.WriteString(",")
		}
		k, _ := json.Marshal(id)
		v, _ := json.Marshal(s[id])
		sb.WriteString("\n")
		sb.Write(k)
		sb.WriteString(": ")
		sb.Write(v)
	}
	if len(ids) > 0 {
		sb.WriteString("\n")
	}
	sb.WriteString("}\n")
	return []byte(sb.String())
}

// FilterNew は既読に無い記事だけを返す(順序は保つ)。
func (s Seen) FilterNew(entries []feed.Entry) []feed.Entry {
	var out []feed.Entry
	for _, e := range entries {
		if _, ok := s[e.ID]; !ok {
			out = append(out, e)
		}
	}
	return out
}

// Mark は記事を既読にする。既に入っている記事の日付は変えない(初めて見た日を保つ)。
func (s Seen) Mark(entries []feed.Entry, today string) {
	for _, e := range entries {
		if _, ok := s[e.ID]; !ok {
			s[e.ID] = today
		}
	}
}

// Prune は today から keepDays より前に見た記事を落とした複製を返す。
// フィードから消えて久しい記事を覚え続けない(再配信は keepDays 内なら既読のまま)。
func (s Seen) Prune(today string, keepDays int) (Seen, error) {
	t, err := time.Parse("2006-01-02", today)
	if err != nil {
		return nil, fmt.Errorf("today は YYYY-MM-DD: %q", today)
	}
	cutoff := t.AddDate(0, 0, -keepDays).Format("2006-01-02")
	out := Seen{}
	for id, d := range s {
		if d >= cutoff {
			out[id] = d
		}
	}
	return out, nil
}
