package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openInBrowser は path を既定のブラウザ(OS の既定アプリ)で開く。テストでは差し替える。
// 起動するだけで終了を待たない。失敗は呼び出し側が警告にする(ダイジェストは書けているので致命ではない)。
var openInBrowser = func(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path) // start の第 1 引数はウィンドウ題名なので空を渡す
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ブラウザで開けない: %w", err)
	}
	return nil
}
