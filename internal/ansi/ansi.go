// Package ansi quita los códigos de color de la terminal (para el log en
// archivo, el panel web y la salida de los bots del hub).
package ansi

import "regexp"

// re son los códigos de color de la terminal.
var re = regexp.MustCompile("\x1b\\[[0-9;]*m")

// Strip devuelve s sin los códigos de color.
func Strip(s string) string { return re.ReplaceAllString(s, "") }
