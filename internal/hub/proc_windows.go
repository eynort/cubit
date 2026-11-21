//go:build windows

package hub

import "os/exec"

// ownGroup: en Windows no hace falta (Ctrl-C ya va solo al proceso de la consola).
func ownGroup(cmd *exec.Cmd) {}
