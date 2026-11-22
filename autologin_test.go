package main

import (
	"encoding/json"
	"strings"
	"testing"

	"cubit/internal/config"
)

// TestLoginPrompt: los avisos de AuthMe y nLogin, en inglés y en español.
func TestLoginPrompt(t *testing.T) {
	for text, want := range map[string]string{
		`Please login with "/login <password>"`:                                                   "login",
		"Please, login with the following command: /login <password>":                             "login",
		"Use /login <password> to log in":                                                         "login",
		"§cPor favor, inicia sesión con '/login <contraseña>'":                                    "login",
		"Inicia sesión con /LOGIN <contraseña>":                                                   "login",
		"Please register with \"/register <password> <ConfirmPassword>\"":                         "register",
		"Please, register to the server with the command: /register <password> <ConfirmPassword>": "register",
		"Por favor, regístrate con '/register <contraseña> <repetir contraseña>'":                 "register",
		"Use /register <password> <password>":                                                     "register",
		"Login with /login or register with /register":                                            "login",
		"Successfully logged in!":                                                                 "",
		"Has iniciado sesión: /login ya no hace falta":                                            "",
		"Steve joined the game":                                                                   "",
		"<Steve> how do I /login here?":                                                           "login", // (solo se mira el chat del sistema: onSystemChat)
		"Registered successfully! (/login next time)":                                             "",
		"": "",
	} {
		if got := loginPrompt(text); got != want {
			t.Errorf("loginPrompt(%q) = %q, quería %q", text, got, want)
		}
	}
}

// TestLoginPasswordMasked: la contraseña entra por los ajustes, se guarda y
// nunca sale: ni en el JSON de los ajustes ni en el de lo que se ve.
func TestLoginPasswordMasked(t *testing.T) {
	cfgMu.Lock()
	oldCfg, oldPath, oldOwners := cfg, cfgPath, remoteOwners
	cfg, cfgPath = config.Bot{Server: "minecraft", Name: "Viejo_Bot"}, ""
	cfgMu.Unlock()
	defer func() { cfgMu.Lock(); cfg, cfgPath, remoteOwners = oldCfg, oldPath, oldOwners; cfgMu.Unlock() }()

	const pw = "s3cretoRaro"
	if _, err := applySettings(panelSettings{Name: "Viejo_Bot", LoginPassword: pw}); err != nil {
		t.Fatal(err)
	}
	if loginPassword() != pw {
		t.Fatal("no se guardó la contraseña")
	}
	b, _ := json.Marshal(currentSettings())
	if strings.Contains(string(b), pw) || !strings.Contains(string(b), `"hasLoginPassword":true`) {
		t.Fatalf("los ajustes enseñan la contraseña o no dicen que hay: %s", b)
	}
	// sin tocar la contraseña (vacía = no cambiar)
	if _, err := applySettings(panelSettings{Name: "Viejo_Bot"}); err != nil || loginPassword() != pw {
		t.Fatalf("otro ajuste cambió la contraseña: %v", err)
	}
	for _, bad := range []string{"con espacio", "tab\tx", strings.Repeat("a", 101)} {
		if _, err := applySettings(panelSettings{Name: "Viejo_Bot", LoginPassword: bad}); err == nil {
			t.Errorf("contraseña %q aceptada", bad)
		}
	}
	if _, err := applySettings(panelSettings{Name: "Viejo_Bot", ClearLoginPassword: true}); err != nil || loginPassword() != "" {
		t.Fatalf("no se quitó: %v", err)
	}
}
