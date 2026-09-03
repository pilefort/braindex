package schedule

import (
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
	b.WriteString(" # braindex:")
	b.WriteString(j.Name)
	return b.String(), nil
}

// shellQuote は sh 向けに単引用符で囲む。中の単引用符は '"'"' で退避する。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// Merge は existing の crontab から hub のブロックを差し替えた全文を返す。
// ブロックが無ければ末尾に足す。ブロックの外の行には触らない。
// lines が空ならブロックごと消す(Remove と同じ)。
func Merge(existing, hub string, lines []string) string {
	before, _, after, found := splitBlock(existing, hub)
	if len(lines) == 0 {
		if !found {
			return normalize(existing)
		}
		return joinLines(append(before, after...))
	}
	block := make([]string, 0, len(lines)+2)
	block = append(block, beginMarker(hub))
	block = append(block, lines...)
	block = append(block, endMarker(hub))
	if !found {
		return joinLines(append(before, block...))
	}
	out := make([]string, 0, len(before)+len(block)+len(after))
	out = append(out, before...)
	out = append(out, block...)
	out = append(out, after...)
	return joinLines(out)
}

// Remove は hub のブロックを消した全文を返す。
func Remove(existing, hub string) string {
	return Merge(existing, hub, nil)
}

// BlockLines は hub のブロックの中身(マーカーを除く)を返す。ブロックが無ければ nil。
func BlockLines(existing, hub string) []string {
	_, inside, _, found := splitBlock(existing, hub)
	if !found {
		return nil
	}
	return inside
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

// splitBlock は crontab を「ブロックの前・中・後」に分ける。
// found=false のとき before は全行(末尾に足す前提)、inside と after は空。
func splitBlock(existing, hub string) (before, inside, after []string, found bool) {
	lines := splitLines(existing)
	begin, end := -1, -1
	for i, l := range lines {
		switch strings.TrimSpace(l) {
		case beginMarker(hub):
			if begin < 0 {
				begin = i
			}
		case endMarker(hub):
			if begin >= 0 && end < 0 {
				end = i
			}
		}
	}
	// 開始だけあって終了が無いファイルは、壊れた記録として末尾までをブロックとみなす
	// (次の install で正しい形に戻る。ブロック外の行を巻き込まないよう、開始が無ければ何もしない)。
	if begin < 0 {
		return lines, nil, nil, false
	}
	if end < 0 {
		end = len(lines) - 1
	}
	return lines[:begin], lines[begin+1 : end], lines[end+1:], true
}

// splitLines は改行(CRLF も)で分け、末尾の空行は落とす。
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// joinLines は行を crontab の全文にする。空でなければ必ず末尾を改行で終える
// (最終行に改行が無い crontab を受け付けない実装があるため)。
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// normalize は改行を LF に揃え、末尾の改行を 1 つにする。
func normalize(s string) string {
	return joinLines(splitLines(s))
}
