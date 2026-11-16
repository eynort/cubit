package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

func labels(s []suggestion) []string {
	var out []string
	for _, x := range s {
		out = append(out, x.label)
	}
	return out
}

func TestSuggestions(t *testing.T) {
	names := []string{"Alex", "Ben_y", "bob", "Steve"}
	onlineNames.Store(&names)

	if got := suggestions("/"); len(got) != len(commands) {
		t.Errorf(`"/" debería listar las %d órdenes, da %v`, len(commands), labels(got))
	}
	got := suggestions("/fol")
	if len(got) != 1 || got[0].value != "/follow " || !strings.Contains(got[0].label, "<player>") {
		t.Errorf(`"/fol": %+v`, got)
	}
	if got := suggestions("/follow b"); strings.Join(labels(got), ",") != "Ben_y,bob" {
		t.Errorf(`"/follow b": %v`, labels(got))
	}
	if got := suggestions("/fight "); strings.Join(labels(got), ",") != "in,Alex,Ben_y,bob,Steve" {
		t.Errorf(`"/fight ": "in" y todos los jugadores, da %v`, labels(got))
	}
	if got := suggestions("/fight zz"); len(got) != 1 || got[0].value != "" {
		t.Errorf(`"/fight zz": una pista sin valor, da %+v`, got)
	}
	if got := suggestions("/sprint o"); strings.Join(labels(got), ",") != "on,off" {
		t.Errorf(`"/sprint o": %v`, labels(got))
	}
	if got := suggestions("/spawn"); len(got) != 1 || got[0].value != "" {
		t.Errorf(`"/spawn" (del servidor): una pista, da %+v`, got)
	}
	if got := suggestions("hola"); got != nil {
		t.Errorf("sin / no hay sugerencias: %v", labels(got))
	}
}

// TestTabCyclesPlayers: escribir /follow, Tab recorre los jugadores, Enter elige.
func TestTabCyclesPlayers(t *testing.T) {
	names := []string{"Alex", "Ben_y", "Steve"}
	onlineNames.Store(&names)
	in := textinput.New()
	in.Focus()
	m := &tui{in: in, vp: viewport.New(80, 20), sel: -1, w: 80, h: 24}
	typ := func(s string) {
		for _, r := range s {
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
	}
	key := func(k tea.KeyType) { m.Update(tea.KeyMsg{Type: k}) }

	typ("/fol")
	key(tea.KeyTab)
	key(tea.KeyEnter) // elige /follow
	if v := m.in.Value(); v != "/follow " {
		t.Fatalf("tras elegir la orden: %q", v)
	}
	if len(m.sugg) != 3 {
		t.Fatalf("tras /follow se ofrecen los jugadores: %v", labels(m.sugg))
	}
	key(tea.KeyTab)
	key(tea.KeyTab)
	if v := m.in.Value(); v != "/follow Ben_y" {
		t.Fatalf("dos Tab: %q", v)
	}
	key(tea.KeyTab)
	key(tea.KeyTab) // vuelve al principio
	if v := m.in.Value(); v != "/follow Alex" {
		t.Fatalf("cuatro Tab: %q", v)
	}
	key(tea.KeyShiftTab)
	key(tea.KeyEnter) // elige Steve
	if v := m.in.Value(); v != "/follow Steve" || m.sel != -1 {
		t.Fatalf("Shift+Tab y Enter: %q sel=%d", v, m.sel)
	}
}

func TestFightInSuggestions(t *testing.T) {
	names := []string{"Alex", "Ines"}
	onlineNames.Store(&names)
	got := labels(suggestions("/fight i"))
	if strings.Join(got, ",") != "in,Ines" {
		t.Errorf(`"/fight i": %v`, got)
	}
	if got := suggestions("/fight in "); len(got) != 2 || got[1].value != "/fight in ~ ~" {
		t.Errorf(`"/fight in ": %+v`, got)
	}
	if got := suggestions("/fight in 10 "); len(got) != 1 || got[0].value != "" {
		t.Errorf(`"/fight in 10 ": una pista, da %+v`, got)
	}
}

func TestCoord(t *testing.T) {
	for _, c := range []struct {
		in   string
		base float64
		want float64
		ok   bool
	}{{"12", 5, 12, true}, {"-3.5", 5, -3.5, true}, {"~", 5, 5, true}, {"~-2", 5, 3, true}, {"x", 5, 0, false}} {
		got, err := coord(c.in, c.base)
		if (err == nil) != c.ok || (c.ok && got != c.want) {
			t.Errorf("coord(%q)=%v,%v", c.in, got, err)
		}
	}
}

func TestCmdSuggestions(t *testing.T) {
	if got := suggestions("/cm"); len(got) != 1 || got[0].value != "/cmd " {
		t.Errorf(`"/cm": %+v`, got)
	}
	if got := suggestions("/tp"); len(got) != 1 || got[0].value != "" {
		t.Errorf(`"/tp": ya no hay /tpa, da %+v`, got)
	}
}

func TestGuardSuggestions(t *testing.T) {
	names := []string{"Alex", "Ben_y"}
	onlineNames.Store(&names)
	if got := suggestions("/gu"); len(got) != 1 || got[0].value != "/guard " {
		t.Errorf(`"/gu": %+v`, got)
	}
	if got := labels(suggestions("/guard a")); strings.Join(got, ",") != "Alex" {
		t.Errorf(`"/guard a": %v`, got)
	}
}

func TestGotoSuggestions(t *testing.T) {
	names := []string{"Alex"}
	onlineNames.Store(&names)
	got := suggestions("/goto ")
	if strings.Join(labels(got), ",") != "Alex,<x> <z>,~ ~" || got[2].value != "/goto ~ ~" {
		t.Errorf(`"/goto ": %+v`, got)
	}
	if got := suggestions("/goto 10 "); len(got) != 1 || got[0].value != "" {
		t.Errorf(`"/goto 10 ": una pista, da %+v`, got)
	}
}

func TestChopSuggestions(t *testing.T) {
	if got := suggestions("/cho"); len(got) != 1 || got[0].value != "/chop " {
		t.Errorf(`"/cho": %+v`, got)
	}
	if got := labels(suggestions("/ch")); len(got) != 2 {
		t.Errorf(`"/ch": /chests y /chop, da %v`, got)
	}
	if got := labels(suggestions("/chop ro")); strings.Join(got, ",") != "roble,roble_oscuro,roble_palido" {
		t.Errorf(`"/chop ro": %v`, got)
	}
	if got := labels(suggestions("/base ")); strings.Join(got, ",") != "here,info" {
		t.Errorf(`"/base ": %v`, got)
	}
}

// TestHistory: flecha arriba recorre las órdenes ya mandadas (aunque tengan
// sugerencias), abajo vuelve hasta lo que se estaba escribiendo; no se
// repite la última.
func TestHistory(t *testing.T) {
	t.Chdir(t.TempDir()) // el archivo del historial, aparte
	in := textinput.New()
	in.Focus()
	m := &tui{in: in, vp: viewport.New(80, 20), sel: -1, w: 80, h: 24, exec: func(string) {}}
	typ := func(s string) {
		for _, r := range s {
			m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
	}
	key := func(k tea.KeyType) { m.Update(tea.KeyMsg{Type: k}) }
	send := func(s string) { typ(s); key(tea.KeyEsc); key(tea.KeyEnter) }

	send("/chop oak")
	send("/status")
	send("/status")
	send("hola")
	if strings.Join(m.hist, "|") != "/chop oak|/status|hola" {
		t.Fatalf("historial: %q", m.hist)
	}
	key(tea.KeyUp)
	if v := m.in.Value(); v != "hola" {
		t.Fatalf("arriba: %q", v)
	}
	key(tea.KeyUp)
	key(tea.KeyUp)
	if v := m.in.Value(); v != "/chop oak" {
		t.Fatalf("tres arriba: %q", v)
	}
	key(tea.KeyUp) // ya en la primera: se queda
	if v := m.in.Value(); v != "/chop oak" {
		t.Fatalf("más arriba: %q", v)
	}
	key(tea.KeyDown)
	if v := m.in.Value(); v != "/status" {
		t.Fatalf("abajo: %q", v)
	}
	key(tea.KeyDown)
	key(tea.KeyDown) // abajo del todo: la línea vacía
	if v := m.in.Value(); v != "" {
		t.Fatalf("abajo del todo: %q", v)
	}
	// lo que se estaba escribiendo se conserva al bajar de nuevo
	typ("/go")
	key(tea.KeyUp) // con sugerencias de /go…: las recorre (no el historial)
	key(tea.KeyEsc)
	m.setValue("")
	m.suggest()
	// un mensaje a medias (sin sugerencias): arriba al historial, abajo lo devuelve
	typ("a medias")
	key(tea.KeyUp)
	if v := m.in.Value(); v != "hola" {
		t.Fatalf("arriba con texto: %q", v)
	}
	key(tea.KeyDown)
	if v := m.in.Value(); v != "a medias" {
		t.Fatalf("lo escrito no volvió: %q", v)
	}
	// y se guarda entre sesiones
	if got := loadHistory(); strings.Join(got, "|") != "/chop oak|/status|hola" {
		t.Fatalf("guardado: %q", got)
	}
}

// TestLang: las órdenes y parámetros siempre en inglés; las explicaciones en
// inglés por defecto y en español con /lang es (y se recuerda).
func TestLang(t *testing.T) {
	t.Chdir(t.TempDir())
	curLang.Store(nil)
	defer func() { en := "en"; curLang.Store(&en) }()
	got := suggestions("/fol")
	if len(got) != 1 || got[0].desc != "follow a player non-stop (until /stop)" {
		t.Fatalf("en inglés por defecto: %+v", got)
	}
	if err := cmdLang(nil, []string{"es"}); err != nil {
		t.Fatal(err)
	}
	got = suggestions("/fol")
	if got[0].desc != "seguir a un jugador sin parar (hasta /stop)" || !strings.Contains(got[0].label, "<player>") {
		t.Fatalf("en español: %+v", got)
	}
	curLang.Store(nil) // otra sesión: se lee del archivo
	if lang() != "es" {
		t.Fatalf("no se recordó el idioma: %s", lang())
	}
	if err := cmdLang(nil, []string{"fr"}); err == nil {
		t.Errorf("fr no existe")
	}
	for name, c := range commands { // todo en inglés: nombres de órdenes, parámetros y opciones
		for _, s := range append([]string{name}, argWords(c)...) {
			for _, bad := range []string{"jugador", "cantidad", "cola", "camas", "granja", "aqui", "accion", "minutos", "madera", "texto", "modo", "comando"} {
				if s == bad {
					t.Errorf("/%s: %q no está en inglés", name, s)
				}
			}
		}
	}
}

func argWords(c command) []string {
	var out []string
	for _, a := range c.args {
		out = append(out, a.name)
		out = append(out, a.choices...)
	}
	return out
}

// TestDropSuggestions: /drop sugiere lo que lleva (y los jugadores), y la
// cantidad de lo elegido.
func TestDropSuggestions(t *testing.T) {
	names := []string{"Alex", "dave"}
	onlineNames.Store(&names)
	inv := func() map[string]int { return map[string]int{"dirt": 100, "diamond": 3, "oak_log": 12} }
	carriedSource.Store(&inv)
	defer carriedSource.Store(nil)

	if got := labels(suggestions("/drop ")); strings.Join(got, ",") != "diamond,dirt,oak_log,Alex,dave" {
		t.Errorf("/drop : %v", got)
	}
	got := suggestions("/drop d")
	if strings.Join(labels(got), ",") != "diamond,dirt,dave" || got[1].value != "/drop dirt " {
		t.Errorf("/drop d: %v", got)
	}
	got = suggestions("/drop dirt ")
	if strings.Join(labels(got), ",") != "<amount>,100,Alex,dave" || got[1].value != "/drop dirt 100 " || got[2].value != "/drop dirt Alex" {
		t.Errorf("/drop dirt : %+v", got)
	}
	if got := labels(suggestions("/drop dirt 10 ")); strings.Join(got, ",") != "Alex,dave" {
		t.Errorf("/drop dirt 10 : %v", got)
	}
	if got := suggestions("/drop dirt Alex "); got != nil {
		t.Errorf("/drop dirt Alex : %+v", got)
	}
	if got := suggestions("/drop zz"); len(got) != 1 || got[0].value != "" {
		t.Errorf("/drop zz: %+v", got)
	}
}
