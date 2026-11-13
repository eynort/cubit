package main

// Entrar en un servidor con plugin de login (AuthMe, nLogin, LibreLogin,
// VeloAuth, JPremium, OpeNLogin…), sea cual sea: se lee lo que dice (chat,
// encima de la hotbar o en un título) y se contesta con /register o /login.
//
//   - "/register <contraseña> <contraseña>" (o con una sola): se registra, con
//     la contraseña guardada o con una nueva que se guarda en bot.json.
//   - "/login <contraseña>": entra con la guardada (si no hay, lo dice: --password).
//   - "ya estás registrado" tras /register: /login; "no estás registrado" tras /login: /register.
//   - Contraseña incorrecta o rechazada: lo dice y se desconecta (repetir no sirve).
//   - Si el aviso se repite sin respuesta, lo reintenta (como mucho 3 veces).
//
// Sin plugin de login no hace nada. La skin (botSkin) se pide al entrar de
// verdad: tras el login, o a los pocos segundos si nadie pidió login.

import (
	"bytes"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

type authState struct {
	mu       sync.Mutex
	prompted bool      // el servidor pidió login o registro
	done     bool      // ya dentro (o no hay plugin de login)
	last     time.Time // el último /login o /register mandado
	sent     string    // "login" o "register"
	tries    int
	pending  string // contraseña inventada al registrarse (se guarda al mandarla)
	warned   bool   // ya avisó de que no sabe la contraseña

	cmd  func(string) error // (tests: en vez de mandarlo al servidor)
	quit func()
}

func (b *Bot) authCmd(c string) error {
	if b.auth.cmd != nil {
		return b.auth.cmd(c)
	}
	return b.Chat.SendCommand(c)
}

var (
	// "/register <password> <password>", "/reg <clave> <clave>"…
	authRegister = regexp.MustCompile(`(?i)/(?:register|registrar(?:se)?|reg)\b([^/\n]*)`)
	authLogin    = regexp.MustCompile(`(?i)/(?:login|log|l|entrar|iniciar)\b(?:\s|$|<|\[)`)
	authArg      = regexp.MustCompile(`<[^>]*>|\[[^\]]*\]|\{[^}]*\}`)
	authEmail    = regexp.MustCompile(`(?i)e-?mail|correo`)
	authCaptcha  = regexp.MustCompile(`(?i)captcha|c[oó]digo|code`)

	authOK = regexp.MustCompile(`(?i)(logged[ -]in|(login|log-in) successful|successful(ly)? log-?in|successfully (logged|registered|authenticated|log)|` +
		`registered successfully|you are now (logged|registered)|welcome back|successfully authenticated|` +
		`sesi[oó]n iniciada|has iniciado sesi[oó]n|iniciaste sesi[oó]n|inicio de sesi[oó]n (correcto|exitoso)|` +
		`(te has|te registraste|has sido|fuiste) registrad|registr(o|ado) (completado|correctamente|con [eé]xito|exitoso)|` +
		`autenticad[oa] (correctamente|con [eé]xito)|te has autenticado|bienvenid[oa] de nuevo|conectado correctamente)`)
	authBadPass = regexp.MustCompile(`(?i)((wrong|incorrect|invalid|bad)\s+(password|pass)|` +
		`(contraseña|clave)\s+(incorrecta|inv[aá]lida|err[oó]nea|equivocada|no v[aá]lida)|` +
		`(password|contraseña|clave)\b.{0,60}\b(too short|too long|too weak|demasiado (corta|larga|d[eé]bil)|must|debe|needs?|necesita|requires?|requiere|not allowed|no (est[aá] )?permitid|unsafe|insegura))`)
	authAlready    = regexp.MustCompile(`(?i)(already registered|ya (est[aá]s |est[aá] )?registrad|ya (tienes|existe) (una )?cuenta|account already exists)`)
	authNotRegistr = regexp.MustCompile(`(?i)(not registered|no est[aá]s registrad|no (tienes|existe) (una |la )?cuenta|isn'?t registered|no registrad)`)
)

// onAuthText mira un texto del servidor (chat, hotbar o título) por si es
// del plugin de login.
func (b *Bot) onAuthText(text string) {
	a := &b.auth
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.done || strings.TrimSpace(text) == "" {
		return
	}
	switch {
	case a.sent != "" && authOK.MatchString(text):
		a.done = true
		if a.pending != "" {
			rememberPassword(a.pending)
			a.pending = ""
		}
		log.Printf("login: dentro (%s)", a.sent)
		b.afterLogin()
		return
	case a.sent != "" && authBadPass.MatchString(text) && !authRegister.MatchString(text) && !authLogin.MatchString(text):
		if a.sent == "register" {
			log.Printf("login: el servidor no acepta la contraseña para registrarse: %q. Pon otra con --password.", text)
		} else {
			log.Printf("login: contraseña incorrecta para %s en %s: %q. Pon la buena con --password.", botName, serverHost, text)
			notify("login: contraseña incorrecta; no vuelvo a entrar")
		}
		a.done = true
		if a.quit != nil {
			a.quit()
		} else {
			b.noRetry.Store(true) // (ni volver a entrar: fallaría igual)
			b.Client.Close()      // (repetir con la misma no sirve, y algunos banean)
		}
		return
	case a.sent == "register" && authAlready.MatchString(text):
		a.sent, a.tries = "", 0 // ya registrado: login
		a.pending = ""
		b.sendLogin(a)
		return
	case a.sent == "login" && authNotRegistr.MatchString(text):
		a.sent, a.tries = "", 0
		b.sendRegister(a, "")
		return
	}
	reg := authRegister.FindStringSubmatch(text)
	login := authLogin.MatchString(text)
	if reg == nil && !login {
		return
	}
	a.prompted = true
	// los dos (" /login o /register si eres nuevo"): con contraseña, login
	if reg != nil && (!login || botPassword == "") {
		b.sendRegister(a, reg[1])
	} else {
		b.sendLogin(a)
	}
}

// again: ¿se puede mandar (otra vez)? Los avisos se repiten cada pocos
// segundos: no se contesta a cada uno, solo si el anterior no sirvió.
func (a *authState) again(kind string) bool {
	if a.sent == kind && (time.Since(a.last) < 5*time.Second || a.tries >= 3) {
		return false
	}
	if a.sent != kind {
		a.tries = 0
	}
	a.sent, a.last = kind, time.Now()
	a.tries++
	return true
}

func (b *Bot) sendLogin(a *authState) {
	if botPassword == "" {
		if !a.warned {
			a.warned = true
			log.Printf("login: %s pide /login y no sé la contraseña de %s. Escríbela en la consola (/login …) o arranca con --password.", serverHost, botName)
		}
		return
	}
	if !a.again("login") {
		return
	}
	log.Printf("login: /login (intento %d)", a.tries)
	if err := b.authCmd("login " + botPassword); err != nil {
		log.Printf("login: %v", err)
	}
}

// sendRegister: usage es lo que sigue a "/register" en el aviso, para ver
// cuántas veces va la contraseña.
func (b *Bot) sendRegister(a *authState, usage string) {
	args := authArg.FindAllString(usage, -1)
	for _, x := range args {
		if authEmail.MatchString(x) || authCaptcha.MatchString(x) {
			if !a.warned {
				a.warned = true
				log.Printf("login: %s pide registrarse con %s: eso no lo sé hacer solo; regístralo a mano desde la consola", serverHost, x)
			}
			return
		}
	}
	if !a.again("register") {
		return
	}
	pass := botPassword
	if pass == "" {
		if a.pending == "" {
			a.pending = newPassword()
			log.Printf("login: %s pide registrarse; uso una contraseña nueva (se guarda en %s)", serverHost, cfgPath)
		}
		pass = a.pending
		rememberPassword(pass) // (ya: si se cae justo después, no se pierde)
	}
	cmd := "register " + pass + " " + pass
	if len(args) == 1 {
		cmd = "register " + pass
	}
	log.Printf("login: /register (intento %d)", a.tries)
	if err := b.authCmd(cmd); err != nil {
		log.Printf("login: %v", err)
	}
}

// noteManualAuth: el usuario escribió /login o /register en la consola: esa
// es la contraseña de esta cuenta.
func (b *Bot) noteManualAuth(line string) {
	f := strings.Fields(line)
	if len(f) < 2 {
		return
	}
	switch strings.ToLower(strings.TrimPrefix(f[0], "/")) {
	case "changepassword", "changepass", "cp":
		if len(f) >= 3 {
			rememberPassword(f[2]) // /changepassword <vieja> <nueva>
		}
	case "login", "l", "log", "register", "reg":
		rememberPassword(f[1])
		kind := "register"
		if strings.HasPrefix(strings.ToLower(strings.TrimPrefix(f[0], "/")), "l") {
			kind = "login"
		}
		b.auth.mu.Lock()
		if !b.auth.done {
			b.auth.sent, b.auth.last, b.auth.pending = kind, time.Now(), ""
		}
		b.auth.mu.Unlock()
	}
}

// joinedGame: el servidor mandó el inicio de partida (también al pasar del
// limbo del login al servidor de juego). Si en unos segundos nadie pidió
// login, no hay plugin: ya está dentro.
func (b *Bot) joinedGame() {
	a := &b.auth
	a.mu.Lock()
	if a.sent != "" && !a.done {
		// un proxy (Velocity/Bungee) que tras el login manda a otro
		// servidor: eso ya es estar dentro
		a.done = true
		if a.pending != "" {
			rememberPassword(a.pending)
			a.pending = ""
		}
		a.mu.Unlock()
		log.Print("login: dentro (cambio de servidor)")
		b.afterLogin()
		return
	}
	a.mu.Unlock()
	time.AfterFunc(8*time.Second, func() {
		a.mu.Lock()
		free := !a.prompted && !a.done
		if free {
			a.done = true
		}
		a.mu.Unlock()
		if free {
			b.afterLogin()
		}
	})
}

// afterLogin: ya en el servidor de juego. Pide la skin (una vez por arranque).
func (b *Bot) afterLogin() {
	if botSkin == "" || !b.skinSent.CompareAndSwap(false, true) {
		return
	}
	go func() {
		time.Sleep(3 * time.Second)
		log.Printf("skin: /skin %s", botSkin)
		if err := b.Chat.SendCommand("skin " + botSkin); err != nil {
			log.Printf("skin: %v", err)
		}
	}()
}

// watchAuthTitles: los avisos de login que salen como título en pantalla.
func (b *Bot) watchAuthTitles() {
	b.Client.Events.AddListener(bot.PacketHandler{
		Priority: -20, ID: packetid.ClientboundPlaySetTitleText,
		F: func(p pk.Packet) error {
			var t play.SetTitleText
			if _, err := t.ReadFrom(bytes.NewReader(p.Data)); err == nil {
				b.onAuthText(t.Text.ClearString())
			}
			return nil
		},
	}, bot.PacketHandler{
		Priority: -20, ID: packetid.ClientboundPlaySetSubtitleText,
		F: func(p pk.Packet) error {
			var t play.SetSubtitleText
			if _, err := t.ReadFrom(bytes.NewReader(p.Data)); err == nil {
				b.onAuthText(t.Text.ClearString())
			}
			return nil
		},
	})
}
