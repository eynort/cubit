package main

// Servidores con AuthMe / nLogin: al entrar piden /login <contraseña> (o
// /register <contraseña> <contraseña>). Si en los ajustes hay una contraseña,
// el bot contesta solo. La contraseña no sale nunca en el log ni por la API.

import (
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
)

type loginState struct {
	mu    sync.Mutex
	sent  map[string]int       // cuántas veces ha contestado a cada petición en esta conexión
	last  map[string]time.Time // cuándo fue la última
	asked bool                 // ya avisó de que no hay contraseña
}

const (
	maxLoginAnswers = 3                       // un servidor que sigue pidiéndolo es que la contraseña no vale
	loginGap        = 4 * time.Second         // entre dos respuestas a lo mismo (los plugins repiten el aviso)
	loginDelay      = 1200 * time.Millisecond // (el plugin tiene que haber visto al jugador entrar)
)

// loginCmd: "/login" o "/register" en el texto del aviso (también pegado a
// una barra o entre comillas).
var loginCmd = regexp.MustCompile(`/(login|register|reg|l)\b`)

// loginPrompt dice qué pide un mensaje del servidor: "login", "register" o "" (nada).
// Vale para los avisos de AuthMe y nLogin, en inglés y en español:
//
//	Please login with "/login <password>"
//	Please, register to the server with the command: /register <password> <ConfirmPassword>
//	Por favor, inicia sesión con '/login <contraseña>'
//	Regístrate con /register <contraseña> <contraseña>
//	Use /login <password> to log in
func loginPrompt(text string) string {
	t := strings.ToLower(text)
	if t == "" || len(t) > 400 {
		return ""
	}
	// "ya estás registrado / sesión iniciada": no pide nada
	for _, done := range []string{"successfully logged", "logged in successfully", "login successful", "sesión iniciada", "has iniciado sesión", "has sido registrado", "registered successfully", "successfully registered", "registro completado"} {
		if strings.Contains(t, done) {
			return ""
		}
	}
	login, register := false, false
	for _, m := range loginCmd.FindAllStringSubmatch(t, -1) {
		switch m[1] {
		case "login", "l":
			login = true
		case "register", "reg":
			register = true
		}
	}
	switch {
	case login && register:
		// "login with /login or register with /register": primero iniciar sesión
		return "login"
	case login:
		return "login"
	case register:
		return "register"
	}
	return ""
}

// loginPassword es la contraseña de los ajustes ("" si no hay).
func loginPassword() string {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	return cfg.LoginPassword
}

// checkLoginPrompt mira un mensaje del servidor: si pide iniciar sesión o
// registrarse y hay contraseña, contesta.
func (b *Bot) checkLoginPrompt(text string) {
	kind := loginPrompt(text)
	if kind == "" {
		return
	}
	pw := loginPassword()
	b.login.mu.Lock()
	if pw == "" {
		first := !b.login.asked
		b.login.asked = true
		b.login.mu.Unlock()
		if first {
			log.Print("[login] el servidor pide /" + kind + ", pero no hay contraseña en los ajustes (panel → ajustes)")
		}
		return
	}
	if b.login.sent == nil {
		b.login.sent, b.login.last = map[string]int{}, map[string]time.Time{}
	}
	if b.login.sent[kind] >= maxLoginAnswers || time.Since(b.login.last[kind]) < loginGap {
		b.login.mu.Unlock()
		return
	}
	b.login.sent[kind]++
	b.login.last[kind] = time.Now()
	n := b.login.sent[kind]
	b.login.mu.Unlock()
	if n == maxLoginAnswers {
		log.Print(tr("[login] several attempts already: if it keeps asking, the password in the settings is wrong", "[login] ya van varios intentos: si sigue pidiéndolo, la contraseña de los ajustes no vale"))
	}
	log.Printf(tr("[login] the server asks for /%s: answering with the password from the settings", "[login] el servidor pide /%s: contesto con la contraseña de los ajustes"), kind)
	go func() {
		select {
		case <-time.After(loginDelay):
		case <-b.ctx.Done():
			return
		}
		line := "login " + pw
		if kind == "register" {
			line = "register " + pw + " " + pw
		}
		if err := b.Chat.SendCommand(line); err != nil {
			log.Print("[login] ", kind, ": no se pudo enviar") // (sin la línea: lleva la contraseña)
		}
	}()
}
