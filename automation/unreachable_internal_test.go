package automation

import (
	"errors"
	"testing"
)

// TestUnreachable: los fallos de "no hay forma de llegar" se reconocen (como
// el del servidor: "no puedo cavar hacia … (agua, lava o nada donde pisar)").
func TestUnreachable(t *testing.T) {
	for msg, want := range map[string]bool{
		"no puedo cavar hacia 232 116 -231 (agua, lava o nada donde pisar)": true,
		"no llego a 84 100 4: failed: no hay camino (desde 84 64 4)":         true,
		"tardo demasiado en llegar a 231 200 -232":                           true,
		"no avanzo hacia 1 2 3 (sin moverme)":                                true,
		"sin picos":                                                          false,
		"el servidor no deja romper":                                         false,
	} {
		if got := unreachable(errors.New(msg)); got != want {
			t.Errorf("%q: %v, quería %v", msg, got, want)
		}
	}
}
