package news

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Command はシェルを介さず外部採点プログラムに JSON を渡す。
type Command struct {
	Argv    []string
	Timeout time.Duration
}

func (c Command) Annotate(ctx context.Context, request string) (string, error) {
	if len(c.Argv) == 0 {
		return "", fmt.Errorf("外部採点: argv が空")
	}
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, c.Argv[0], c.Argv[1:]...)
	cmd.WaitDelay = 5 * time.Second
	cmd.Stdin = strings.NewReader(request)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	// JSON の破損でも stderr の先頭行を警告に残す。
	if err == nil && !json.Valid(out) {
		err = fmt.Errorf("標準出力が JSON ではない")
	}
	if err != nil {
		msg := firstLine(strings.TrimSpace(stderr.String()))
		if msg != "" {
			return "", fmt.Errorf("外部採点: %w: %s", err, msg)
		}
		return "", fmt.Errorf("外部採点: %w", err)
	}
	return string(out), nil
}

func parseCommandResponse(out string, batch []annotationItem) (Annotations, []string) {
	var response struct {
		Items []json.RawMessage `json:"items"`
	}
	got := Annotations{}
	if err := json.Unmarshal([]byte(out), &response); err != nil || response.Items == nil {
		return got, []string{"外部採点: 応答に items 配列がないか JSON が壊れている"}
	}
	asked := map[string]bool{}
	for _, it := range batch {
		asked[it.ID] = true
	}
	var warnings []string
	for _, raw := range response.Items {
		var row struct {
			ID    string `json:"id"`
			Score *int   `json:"r"`
			Tag   string `json:"tag"`
		}
		if err := json.Unmarshal(raw, &row); err != nil || !asked[row.ID] || row.Score == nil || *row.Score < 0 || *row.Score > 3 {
			warnings = append(warnings, "外部採点: 依頼にない id または不正な r・tag を捨てた")
			continue
		}
		got[row.ID] = Annotation{Score: row.Score, Tag: truncateRunes(oneLine(row.Tag), 24), External: true}
	}
	for _, it := range batch {
		id := it.ID
		if _, ok := got[id]; !ok {
			warnings = append(warnings, "外部採点: 依頼した記事の採点がない: "+id)
		}
	}
	return got, warnings
}
