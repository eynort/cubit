//go:build !windows

package main

import "syscall"

// reexec sustituye este proceso por uno nuevo (mismo PID: en Docker sigue
// siendo el principal).
func reexec(exe string, args, env []string) error { return syscall.Exec(exe, args, env) }
