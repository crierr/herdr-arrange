//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detach puts a child in its own session, so that herdr closing the popup — and
// signalling the process group it ran in — does not take the replacement popup's
// opener down with it before it has opened anything.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
