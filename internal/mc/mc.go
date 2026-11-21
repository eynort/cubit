// Package mc: lo básico de Minecraft que comparten el bot, el hub y el
// servidor de pruebas: nombres de jugador y direcciones de servidor.
package mc

import (
	"net"
	"regexp"
	"strings"
)

// ValidName: un nombre de jugador válido (3-16 letras, números o _).
var ValidName = regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`)

// HostOf quita el puerto.
func HostOf(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return strings.Trim(addr, "[]")
}

// SplitAddr: host y puerto (25565 si no lo lleva).
func SplitAddr(a string) (string, string) {
	if h, p, err := net.SplitHostPort(a); err == nil {
		return h, p
	}
	return strings.Trim(a, "[]"), "25565"
}
