package schedule

import (
	"github.com/pilefort/braindex/internal/textblock"
	"path"
	"strings"
)

// crontab の中で braindex が管理する範囲を囲む目印。hub ごとに 1 ブロックなので、
// 同じ利用者が複数の hub を登録しても互いを消さない。
func beginMarker(hub string) string { return "# BEGIN braindex " + hub }
func endMarker(hub string) string   { return "# END braindex " + hub }

// CronLine は 1 ジョブの crontab 行を作る。
//
// 行末の "# braindex:<名>" は list がジョブを見分けるための目印。
func CronLine(hub, exe string, j Job) (string, error) {
	w, err := ParseWhen(j.When)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(w.CronFields())
	b.WriteString(" cd ")
	b.WriteString(shellQuote(hub))
	b.WriteString(" && ")
	b.WriteString(shellQuote(exe))
	for _, a := range j.Args {
		b.WriteString(" ")
		b.WriteString(shellQuote(a))
	}
	b.WriteString(" >> ")
	b.WriteString(shellQuote(path.Join(hub, ".braindex", "schedule.log")))
	b.WriteString(" 2>&1 # braindex:")
	b.WriteString(j.Name)
	// cron はシェルの引用符内でも % を改行として扱う。
	return strings.ReplaceAll(b.String(), "%", `\%`), nil
}

// shellQuote は sh 向けに単引用符で囲む。中の単引用符は '"'"' で退避する。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// Merge は existing の crontab から hub のブロックを差し替えた全文を返す。
// ブロックが無ければ末尾に足す。ブロックの外の行には触らない。
// lines が空ならブロックごと消す(Remove と同じ)。
func Merge(existing, hub string, lines []string) string {
	return textblock.Merge(existing, beginMarker(hub), endMarker(hub), lines)
}

// Remove は hub のブロックを消した全文を返す。
func Remove(existing, hub string) string {
	return Merge(existing, hub, nil)
}

// BlockLines は hub のブロックの中身(マーカーを除く)を返す。ブロックが無ければ nil。
func BlockLines(existing, hub string) []string {
	return textblock.Lines(existing, beginMarker(hub), endMarker(hub))
}

func splitBlock(existing, hub string) (before, inside, after []string, found bool) {
	return textblock.Split(existing, beginMarker(hub), endMarker(hub))
}

// JobOfLine は crontab 行の末尾の目印からジョブ名を取る。目印が無ければ空。
func JobOfLine(line string) string {
	const mark = "# braindex:"
	i := strings.LastIndex(line, mark)
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(line[i+len(mark):])
}
