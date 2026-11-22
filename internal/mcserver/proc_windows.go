//go:build windows

package mcserver

import (
	"os/exec"
	"syscall"
)

// setProcAttr: su propio grupo de procesos y sin ventana, para que el Ctrl-C
// del hub no le llegue (se le para con "stop").
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200 | 0x08000000, HideWindow: true}
}
