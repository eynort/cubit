package main

import (
	"os"
	"testing"
)

// Los tests, en inglés (sin leer el .lang del operador).
func TestMain(m *testing.M) {
	en := "en"
	curLang.Store(&en)
	// (lo que en el bot sale de bot.json: un nombre y un dueño de prueba)
	botName, serverHost, remoteOwners = "Steve_Bot", "localhost", []string{"Alex"}
	os.Exit(m.Run())
}

// /log?after=<el último>: nada nuevo es [] en el JSON, no null (la API lo promete).
func TestLogRingSinceEmpty(t *testing.T) {
	r := &logRing{}
	_, _ = r.Write([]byte("una\n"))
	lines, next := r.since(99)
	if lines == nil || len(lines) != 0 || next != 1 {
		t.Fatalf("lines=%#v next=%d", lines, next)
	}
}
