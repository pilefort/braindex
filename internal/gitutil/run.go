// Package gitutil は git 実行時の文字列処理を統一する。
package gitutil

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Run は dir で git を実行する。非 ASCII パスをエスケープせず、出力の CRLF を LF にする。
func Run(path, dir string, args ...string) (string, error) {
	cmd := exec.Command(path, append([]string{"-c", "core.quotePath=false", "-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(bytes.ReplaceAll(out, []byte("\r\n"), []byte("\n"))), nil
}
