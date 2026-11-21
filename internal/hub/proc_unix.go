//go:build !windows

package hub

import (
	"os/exec"
	"syscall"
)

// ownGroup: el proceso hijo en su propio grupo (las señales de la terminal no le llegan).
func ownGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
