package main

import "testing"

// TestToBot: solo cuenta como respuesta lo que va para el bot: lo que
// hablan otros entre ellos ("usa /home bed") no.
func TestToBot(t *testing.T) {
	for _, c := range []struct {
		text string
		want bool
	}{
		{"tCreo que si, usa /home bed", false},
		{"sospecho que sí, porque por defecto aparecerías en tu cama", false},
		{"Steve_Bot ya te di permiso", true},
		{"steve usa el cofre de la izquierda", true},
		{"ya puedes, bot", true},
		{"robot", false},
	} {
		if got := toBot(c.text); got != c.want {
			t.Errorf("toBot(%q) = %v", c.text, got)
		}
	}
}
