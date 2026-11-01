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
