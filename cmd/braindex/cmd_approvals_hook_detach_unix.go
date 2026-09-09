//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detachProcess は子を新しいセッションに置き、親の端末や終了に巻き込まれないようにする。
func detachProcess(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
