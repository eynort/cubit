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

// TestMineSuggestions: /mine ofrece los subcomandos y los minerales; chest
// lleva sus coordenadas (u off, list); la lista con comas no repite; tras el
// mineral, la cantidad o all, y until.
func TestMineSuggestions(t *testing.T) {
	has := func(line, want string) {
		t.Helper()
		got := suggestions(line)
		for _, s := range got {
			if s.value == want || s.label == want {
				return
			}
		}
		t.Errorf("%q: falta %q en %v / %v", line, want, labels(got), values(got))
	}
	hasnt := func(line, bad string) {
		t.Helper()
		for _, s := range suggestions(line) {
			if s.value == bad || s.label == bad {
				t.Errorf("%q: no debería ofrecer %q", line, bad)
			}
		}
	}
	has("/mine ", "/mine chest ")
	has("/mine ", "/mine list")
	has("/mine ", "/mine diamond ")
	has("/mine ", "/mine debris ")
	has("/mine d", "/mine diamond ")
	hasnt("/mine d", "/mine chest ")
	has("/mine chest ", "/mine chest ~ ~ ~")
	has("/mine chest ", "/mine chest off")
	has("/mine chest ", "/mine chest list")
	has("/mine diamond,i", "/mine diamond,iron ")
	hasnt("/mine diamond,", "/mine diamond,diamond ")
	has("/mine diamond ", "/mine diamond all")
	has("/mine diamond ", "/mine diamond until ")
	has("/mine diamond 10 ", "/mine diamond 10 until ")
	has("/mine diamond until ", "/mine diamond until got ")
	has("/mine diamond until got ", "<item|group>")
	has("/mine diamond until got diamond 10 ", "/mine diamond until got diamond 10 or ")
	has("/mine diamond until time ", "/mine diamond until time 30m ")
	if got := suggestions("/mine diamond chest"); len(values(got)) != 0 {
		t.Errorf(`"/mine diamond chest": "chest" no va ahí, no se ofrece: %v`, values(got))
	}
}

// TestSubcommandSuggestions: los subcomandos de las demás órdenes.
func TestSubcommandSuggestions(t *testing.T) {
	names := []string{"Alex"}
	onlineNames.Store(&names)
	for _, c := range []struct{ line, want string }{
		{"/queue ", "/queue add "},
		{"/queue add mi", "/queue add mine "},
		{"/queue add mine dia", "/queue add mine diamond "},
		{"/queue insert ", "<position>"},
		{"/queue insert 1 ch", "/queue insert 1 chop "},
		{"/foodchest ", "/foodchest add "},
		{"/foodchest add ", "/foodchest add ~ ~ ~"},
		{"/ai ", "/ai reset "},
		{"/ai reset ", "/ai reset Alex"},
		{"/build ", "/build load "},
		{"/build rotate ", "/build rotate 90"},
		{"/build chest ", "/build chest add "},
		{"/build start ", "/build start noclear"},
		{"/chop ", "/chop abeto "},
		{"/chop abeto ", "[<amount>]"},
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
