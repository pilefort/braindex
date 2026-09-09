//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// detachProcess は子を親から切り離す。
// DETACHED_PROCESS でコンソールを継がせず、CREATE_NEW_PROCESS_GROUP で
// 親に送られた Ctrl-C を巻き込まないようにする。
func detachProcess(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200}
}
