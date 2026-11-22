//go:build !windows

package update

import "syscall"

// Restart sustituye este proceso por exe con los mismos argumentos y entorno
// (mismo PID). Solo vuelve si falla.
func Restart(exe string, args, env []string) error { return syscall.Exec(exe, args, env) }
