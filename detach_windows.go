//go:build windows

package main

import "os/exec"

// detach does nothing on Windows: the socket has no named-pipe transport yet, so the
// plugin does not run there at all (see the README). This exists to keep the rest of
// the package compiling for it.
func detach(*exec.Cmd) {}
