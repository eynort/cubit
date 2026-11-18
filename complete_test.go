package main

import (
	"strings"
	"testing"
)

func values(s []suggestion) []string {
	var out []string
	for _, x := range s {
		if x.value != "" {
			out = append(out, x.value)
		}
	}
	return out
}

// TestSubcommandSuggestions: los subcomandos de las demás órdenes.
func TestSubcommandSuggestions(t *testing.T) {
	names := []string{"Alex"}
	onlineNames.Store(&names)
	for _, c := range []struct{ line, want string }{
		{"/excavate ", "/excavate chunk "},
		{"/excavate 1 2 3 4 5 6 ", "/excavate 1 2 3 4 5 6 exact"},
		{"/fight ", "/fight Alex"},
		{"/follow ", "/follow Alex"},
		{"/inventory ", "/inventory grid"},
		{"/guard Alex ", "[<minutes>]"},
		{"/lang ", "/lang es"},
	} {
		got := suggestions(c.line)
		ok := false
		for _, s := range got {
			if s.value == c.want || s.label == c.want || strings.HasPrefix(s.label, c.want) {
				ok = true
			}
		}
		if !ok {
			t.Errorf("%q: falta %q en %v / %v", c.line, c.want, labels(got), values(got))
		}
	}
}

// TestEveryCommandCompletes: ninguna orden se queda sin sugerencia al escribir
// su primer argumento (o dice que no lleva nada).
func TestEveryCommandCompletes(t *testing.T) {
	for name := range commands {
		if got := suggestions("/" + name + " "); len(got) == 0 {
			t.Errorf("/%s: sin sugerencias", name)
		}
	}
}
