package main

import (
	"testing"

	"github.com/google/uuid"
)

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
	if !allowedRemote("Alex") || allowedRemote("alex") || allowedRemote("Malo") { // (con las mayúsculas exactas: remote_sec_test.go)
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
