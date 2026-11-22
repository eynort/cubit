//go:build !windows

package mcserver

import (
	"os/exec"
	"syscall"
)

// setProcAttr: su propio grupo de procesos, para que el Ctrl-C del hub no le
// llegue al servidor (se le para con "stop", y se le mata si no sale).
func setProcAttr(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
