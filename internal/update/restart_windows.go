//go:build windows

package update

import (
	"os"
	"os/exec"
)

// Restart arranca otro proceso igual (en Windows no hay exec) y deja que este
// salga: el llamador sale justo después, ya con el puerto libre.
func Restart(exe string, args, env []string) error {
	c := exec.Command(exe, args[1:]...)
	c.Env, c.Stdin, c.Stdout, c.Stderr = env, os.Stdin, os.Stdout, os.Stderr
	if err := c.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
