//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// reexec sustituye este proceso por uno nuevo (mismo PID: en Docker sigue
// siendo el principal).
func reexec(exe string, args, env []string) error { return syscall.Exec(exe, args, env) }

// ownGroup: el proceso hijo en su propio grupo (las señales de la terminal no le llegan).
func ownGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
