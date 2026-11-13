package main

import (
	"strings"
	"testing"
)

// TestAuthPlugins: los avisos de varios plugins de login, en inglés y en
// español, se contestan con el comando que toca.
func TestAuthPlugins(t *testing.T) {
	cfgPath = "" // (no se guarda nada)
	for _, c := range []struct {
		pass  string
		lines []string
		want  string // los comandos mandados, separados por |
		done  bool
	}{
		{"Abc123", []string{"Please, register to the server with the command: /register <password> <ConfirmPassword>", "Successfully registered!"}, "register Abc123 Abc123", true},
		{"Abc123", []string{"Please, login with the command: /login <password>", "Successful login!"}, "login Abc123", true},
		{"Abc123", []string{"Please, login with the command: /login <password>", "You are now logged in"}, "login Abc123", true},
		{"Abc123", []string{"Usa /register <contraseña> <contraseña> para registrarte", "Te has registrado correctamente"}, "register Abc123 Abc123", true},
		{"Abc123", []string{"Usa /login <contraseña>", "Sesión iniciada"}, "login Abc123", true},
		{"Abc123", []string{"Register with /register <password>"}, "register Abc123", false},
		{"Abc123", []string{"/register <password> <password>", "You are already registered!"}, "register Abc123 Abc123|login Abc123", false},
		{"Abc123", []string{"Type /login <password> or /register <password> <password> if you are new"}, "login Abc123", false},
		{"Abc123", []string{"Usa /login <contraseña>", "Usa /login <contraseña>", "Usa /login <contraseña>"}, "login Abc123", false}, // (repetido enseguida: una vez)
		{"", []string{"Please login: /login <password>"}, "", false},                                                             // sin contraseña: no inventa
		{"Abc123", []string{"Welcome to the server! Visit our store"}, "", false},
		{"Abc123", []string{"Register: /register <email> <email>"}, "", false},
	} {
		var b Bot
		var sent []string
		quit := false
		b.auth.cmd = func(s string) error { sent = append(sent, s); return nil }
		b.auth.quit = func() { quit = true }
		botPassword = c.pass
		for _, l := range c.lines {
			b.onAuthText(l)
		}
		if got := strings.Join(sent, "|"); got != c.want {
			t.Errorf("%q: mandó %q, quería %q", c.lines, got, c.want)
		}
		if b.auth.done != c.done || quit {
			t.Errorf("%q: dentro=%v (quería %v), se fue=%v", c.lines, b.auth.done, c.done, quit)
		}
	}
}

// TestAuthNewPassword: sin contraseña, al pedirle registrarse se inventa una
// (y la recuerda); con la contraseña rechazada se desconecta.
func TestAuthNewPassword(t *testing.T) {
	cfgPath = ""
	botPassword = ""
	var b Bot
	var sent []string
	quit := false
	b.auth.cmd = func(s string) error { sent = append(sent, s); return nil }
	b.auth.quit = func() { quit = true }
	b.onAuthText("Usa /register <contraseña> <contraseña>")
	if len(sent) != 1 || botPassword == "" || sent[0] != "register "+botPassword+" "+botPassword {
		t.Fatalf("mandó %q con la contraseña %q", sent, botPassword)
	}
	if p := newPassword(); len(p) < 10 || strings.ToLower(p) == p || strings.IndexAny(p, "0123456789") < 0 {
		t.Errorf("contraseña floja: %q", p)
	}
	b.onAuthText("Tu contraseña debe tener al menos 2 números")
	if !quit {
		t.Error("con la contraseña rechazada no se fue")
	}
}
