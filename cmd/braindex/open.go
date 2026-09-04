package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openInBrowser は path を既定のブラウザ(OS の既定アプリ)で開く。テストでは差し替える。
// 起動するだけで終了を待たない。失敗は呼び出し側が警告にする(ダイジェストは書けているので致命ではない)。
var openInBrowser = func(path string) error {
	argv := browserCommand(runtime.GOOS, path)
	cmd := exec.Command(argv[0], argv[1:]...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ブラウザで開けない: %w", err)
	}
	return nil
}

// browserCommand は path を既定アプリで開くコマンドを組み立てる。
func browserCommand(goos, path string) []string {
	switch goos {
	case "windows":
		// cmd /c start は渡した文字列をシェルが再解釈するので、& を含むパスが途中で切れて開けない
		// (決定 2026-09-04)。rundll32 は argv がそのまま届き、成功で終了コード 0 を返すので
		// 失敗も見分けられる(explorer は成功でも 1 を返す)。file パスと http URL の両方を扱える。
		return []string{"rundll32", "url.dll,FileProtocolHandler", path}
	case "darwin":
		return []string{"open", path}
	default:
		return []string{"xdg-open", path}
	}
}
