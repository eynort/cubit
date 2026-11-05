package main

import (
	"testing"

	"github.com/google/uuid"
)

func TestSystemWhisper(t *testing.T) {
	for _, c := range []struct {
		in, from, msg string
		ok            bool
	}{
		{"[Alex -> me] follow yo", "Alex", "follow yo", true},
		{"[Alex -> Steve_Bot] /fight in ~ ~", "Alex", "/fight in ~ ~", true},
		{"Alex whispers to you: stop", "Alex", "stop", true},
		{"Alex te susurra: status", "Alex", "status", true},
		{"[Alex] [mi] Hola", "Alex", "Hola", true},
		{"[mi] [Alex] Hola", "", "", false}, // el eco de lo que manda el bot
		{"[ + ] Alex se ha unido", "", "", false},
		{"¡Bienvenido al servidor!", "", "", false},
	} {
		from, msg, ok := systemWhisper(c.in)
		if ok != c.ok || from != c.from || msg != c.msg {
			t.Errorf("%q → %q %q %v", c.in, from, msg, ok)
		}
	}
}

func TestRemoteLine(t *testing.T) {
	for in, want := range map[string]string{
		"follow yo":         "/follow Alex",
		"/guard me":         "/guard Alex",
		"/fight in ~ ~":     "/fight in ~ ~",
		"cmd tpa yo":        "/cmd tpa yo",        // /cmd va tal cual al servidor
		"say yo soy un bot": "/say yo soy un bot", // en /say no: no es un jugador
	} {
		if got := remoteLine(in, "Alex"); got != want {
			t.Errorf("%q → %q, se esperaba %q", in, got, want)
		}
	}
}

func TestRemoteAllowed(t *testing.T) {
	old := remoteOwners
	defer func() { remoteOwners = old }()
	remoteOwners = []string{"Alex"}
	if !allowedRemote("alex") || allowedRemote("Malo") {
		t.Error("lista de autorizados")
	}
	remoteOwners = []string{"*"}
	if !allowedRemote("Malo") {
		t.Error(`"*" acepta a cualquiera`)
	}
}

func TestCoordsReply(t *testing.T) {
	travel.Lock()
	travel.asked = map[string]uuid.UUID{"alex": uuid.New()}
	travel.Unlock()
	if !isCoordsReply("Otro", "120 -450") {
		t.Error("solo números: coordenadas")
	}
	if !isCoordsReply("Alex", "estoy en 30 64 -10") {
		t.Error("respuesta a la pregunta de /goto")
	}
	if isCoordsReply("Otro", "goto 30 -10") {
		t.Error("una orden con números no es una respuesta")
	}
}

func TestNoBuildMsg(t *testing.T) {
	for msg, want := range map[string]bool{
		"No tienes permiso de un administrador para construir aquí.": true,
		"Hey! Sorry, but you can't build here.":                      true,
		"¡Bienvenido al servidor!":                                   false,
		"<Alex> vamos a construir una casa":                         false,
	} {
		if got := noBuildMsg.MatchString(msg); got != want {
			t.Errorf("%q: %v, se esperaba %v", msg, got, want)
		}
	}
}

// TestRemoteDropToMe: "/drop dirt 10 yo" y "/drop dirt yo" por privado se dan a quien lo pide.
func TestRemoteDropToMe(t *testing.T) {
	for in, want := range map[string]string{
		"drop dirt 10 yo": "/drop dirt 10 Alex",
		"drop dirt me":    "/drop dirt Alex",
		"drop yo":         "/drop Alex",
	} {
		if got := remoteLine(in, "Alex"); got != want {
			t.Errorf("%q → %q, se esperaba %q", in, got, want)
		}
	}
}
