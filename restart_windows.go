//go:build windows

package main

import (
	"os"
	"os/exec"
)

// reexec arranca otro proceso igual (en Windows no hay exec) y deja que este salga.
func reexec(exe string, args, env []string) error {
	c := exec.Command(exe, args[1:]...)
	c.Env, c.Stdin, c.Stdout, c.Stderr = env, os.Stdin, os.Stdout, os.Stderr
	return c.Start()
}

// ownGroup: en Windows no hace falta (Ctrl-C ya va solo al proceso de la consola).
func ownGroup(cmd *exec.Cmd) {}
