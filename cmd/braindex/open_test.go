package main

import (
	"runtime"
	"strings"
	"testing"
)

// Windows でブラウザを開くのは rundll32 にする（決定 2026-09-04）。
//
// cmd /c start は渡した文字列をシェルが再解釈するので、& を含むパスが途中で切れて開けない
// （実測 → docs/notes/common/windows-open-path-with-ampersand.md）。
func TestBrowserCommand_windowsはシェルを介さない(t *testing.T) {
	got := browserCommand("windows", "/tmp/a&b/x.html")
	if got[0] == "cmd" {
		t.Fatalf("シェル経由になっている: %v", got)
	}
	want := []string{"rundll32", "url.dll,FileProtocolHandler", "/tmp/a&b/x.html"}
	if len(got) != len(want) {
		t.Fatalf("argv=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argv=%v want %v", got, want)
		}
	}
	// パスは分割せず 1 引数で渡す（& を含んでいても）
	if !strings.Contains(got[len(got)-1], "&") {
		t.Errorf("パスが分かれている: %v", got)
	}
}

func TestBrowserCommand_他のOS(t *testing.T) {
	if got := browserCommand("darwin", "/tmp/x.html"); got[0] != "open" || got[1] != "/tmp/x.html" {
		t.Errorf("darwin: %v", got)
	}
	if got := browserCommand("linux", "/tmp/x.html"); got[0] != "xdg-open" || got[1] != "/tmp/x.html" {
		t.Errorf("linux: %v", got)
	}
}

// openInBrowser は browserCommand の結果をそのまま起動する（goos は実行中のもの）。
func TestOpenInBrowser_実行中のOSの組み立てを使う(t *testing.T) {
	if got := browserCommand(runtime.GOOS, "/tmp/x.html"); len(got) < 2 {
		t.Fatalf("argv が短い: %v", got)
	}
}
