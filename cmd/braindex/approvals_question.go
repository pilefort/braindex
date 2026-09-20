package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/pilefort/braindex/internal/approvals"
)

const approvalsHookQuestionReason = "判断待ちに積まずに問いかけで発話を終えています。" +
	"判断を求めるなら、判断待ちのファイル %s に 5 欄（決めたいこと／なぜ今決めるか／選択肢と得失／私の案／決めないとどうなるか）で積んでください。" +
	"自分で決められること・調べれば分かることなら、問いを消して進めてください。"

func checkUnqueuedQuestion(in hookInput, ff approvalsFileFlags, path string, disabled bool, stdout io.Writer) int {
	if disabled || in.StopHookActive || !hasUnqueuedQuestion(lastHookAssistantText(in.TranscriptPath)) {
		return 0
	}
	// 設定の読み込みにも失敗した場合は、明示されたパスか既定の相対パスを案内する。
	if path == "" {
		path = ff.file
		if path == "" {
			path = approvals.DefaultFile
		}
	}
	_ = json.NewEncoder(stdout).Encode(hookOutput{
		Decision:      "block",
		SystemMessage: "判断待ちに積んでいない問いかけを差し戻します。",
		Reason:        fmt.Sprintf(approvalsHookQuestionReason, path),
	})
	return 0
}

// lastHookAssistantText は本線の最後の空でない本文を読む。
// ReadBytes を使い、JSONL の一行の長さに固定上限を設けない。
func lastHookAssistantText(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	r := bufio.NewReader(f)
	last := ""
	for {
		line, err := r.ReadBytes('\n')
		if err != nil && err != io.EOF {
			return ""
		}
		var entry struct {
			Type        string `json:"type"`
			IsSidechain bool   `json:"isSidechain"`
			Message     struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &entry) == nil && entry.Type == "assistant" && !entry.IsSidechain {
			var parts []string
			for _, part := range entry.Message.Content {
				if part.Type == "text" {
					parts = append(parts, part.Text)
				}
			}
			if body := strings.Join(parts, "\n"); strings.TrimSpace(body) != "" {
				last = body
			}
		}
		if err == io.EOF {
			return last
		}
	}
}

var hookInlineCode = regexp.MustCompile("`+[^`]*`+")

func hasUnqueuedQuestion(body string) bool {
	// 引用行を先に除き、引用内のコードフェンスが本文に影響しないようにする。
	var prose []string
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), ">") {
			prose = append(prose, line)
		}
	}
	var text strings.Builder
	for i, part := range strings.Split(strings.Join(prose, "\n"), "```") {
		if i%2 == 0 {
			text.WriteString(part)
		} else {
			text.WriteByte('\n')
		}
	}
	body = hookInlineCode.ReplaceAllString(text.String(), "")
	if strings.Contains(body, "判断待ち") {
		return true
	}
	lines := strings.Split(body, "\n")
	seen := 0
	for i := len(lines) - 1; i >= 0 && seen < 3; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		seen++
		line := strings.TrimRightFunc(lines[i], func(r rune) bool {
			return unicode.IsSpace(r) || strings.ContainsRune("*_）)」。", r)
		})
		for _, suffix := range []string{"？", "?", "ますか", "ですか", "でしょうか", "ませんか"} {
			if strings.HasSuffix(line, suffix) {
				return true
			}
		}
	}
	return false
}
