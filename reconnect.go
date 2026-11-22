package main

// Volver a entrar solo: si el servidor se reinicia, se corta la red o le
// echan por inactividad, el bot espera un poco (cada vez más, hasta 5 min) y
// entra otra vez, con el trabajo y la cola que tenía. No vuelve si se le
// mandó salir, si falló el login o si le banearon (finalKick).
//
// La consola, la IA y el panel web viven todo el rato; lo de cada conexión
// (el mundo, los módulos) se crea de nuevo, y current apunta al bot de ahora.

import (
	"context"
	"flag"
	"log"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/microsoft"
	mcversion "github.com/mj41/go-mc26/data/version"

	"cubit/automation"
)

var (
	reconnectFlag = flag.Bool("reconnect", true, "volver a entrar solo si se corta la conexión o le echan (no si le banean o falla el login)")
	microsoftFlag = flag.Bool("microsoft", false, "entrar con una cuenta de Minecraft de pago (servidores premium): la primera vez da un código para abrir en el navegador; luego se recuerda")
)

// current es el bot de la conexión de ahora (para la consola, la IA y el panel).
var current atomic.Pointer[Bot]

// finalKick: expulsiones que no se arreglan volviendo a entrar.
var finalKick = regexp.MustCompile(`(?i)(banned|baneado|ban(ned)? (permanente|temporal)|you are banned|whitelist|lista blanca|not whitelisted|` +
	`logged in from another location|otra ubicaci[oó]n|already (connected|online|playing)|ya est[aá]s conectado|` +
	`outdated (client|server)|incompatible|unsupported|failed to (verify|authenticate)|invalid session|sesi[oó]n inv[aá]lida|premium)`)

// runForever: conexión tras conexión mientras haga falta.
func runForever(ctx context.Context, quit func()) error {
	log.Printf("Minecraft %s · servidor %s · %s", versionName(), serverAddr, botName)
	ui := startUI()
	defer ui.Stop()
	startMonitor(ctx)
	if ui == nil {
		go console(ctx) // sin terminal (entrada redirigida): consola simple
	}

	var prev *Bot
	wait := 5 * time.Second
	for {
		b, again, err := run(ctx, quit, prev)
		prev = b
		if ctx.Err() != nil {
			return nil
		}
		if !again || !*reconnectFlag {
			return err
		}
		if err != nil {
			log.Print(err)
		}
		if !b.connectedAt.IsZero() && time.Since(b.connectedAt) > 3*time.Minute {
			wait = 5 * time.Second // (estuvo un rato dentro: no es un fallo seguido)
		}
		log.Printf("vuelvo a entrar en %s (Ctrl+C para salir)", wait.Round(time.Second))
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		case <-reconnectNow: // (el servidor de pruebas acaba de encenderse)
			wait = 5 * time.Second
			continue
		}
		wait = min(wait*2, 5*time.Minute)
	}
}

// setAuth pone la cuenta: offline, el nombre (y el uuid que le da el
// servidor); premium (--microsoft), la de Microsoft, que se recuerda en la
// caché del sistema (solo la primera vez pide abrir el navegador).
func (b *Bot) setAuth(ctx context.Context) error {
	if !*microsoftFlag {
		b.Client.Auth = bot.Auth{Name: botName} // modo offline: solo hace falta el nombre
		return nil
	}
	acc, err := microsoft.AuthenticateDeviceCode(ctx, microsoft.Options{
		CacheKey: strings.ToLower(botName),
		CacheDir: filepath.Join(filepath.Dir(cfgPath), ".microsoft"), // (junto a bot.json: sobrevive al contenedor)
		OnDeviceCode: func(c microsoft.DeviceCode) {
			log.Printf("\n\n  ===> CUENTA DE MICROSOFT: abre %s y escribe el código  %s  <===\n", c.VerificationURI, c.UserCode)
		},
		OnStatus: func(m string) { log.Printf("microsoft: %s", m) },
	})
	if err != nil {
		return err
	}
	if !strings.EqualFold(acc.Profile.Name, botName) {
		log.Printf("microsoft: la cuenta se llama %s (no %s): uso ese nombre", acc.Profile.Name, botName)
		botName = acc.Profile.Name
	}
	id := acc.Profile.ID
	if u, err := uuid.Parse(id); err == nil {
		id = u.String()
	}
	b.Client.Auth = bot.Auth{Name: acc.Profile.Name, UUID: id, AsTk: acc.AccessToken()}
	return nil
}

// carryOver es lo que pasa de una conexión a la siguiente.
type carryOver struct {
	name    string
	task    func(*automation.Job) error
	pos     [3]int
	hasPos  bool
	running bool // la tarea estaba en marcha (o volviendo a ella)
}

// inherit recoge la cola y el trabajo de la conexión anterior.
func (b *Bot) inherit(prev *Bot) {
	if prev == nil || prev.Tasks == nil {
		return
	}
	c := &carryOver{}
	prev.work.mu.Lock()
	c.name, c.task, c.hasPos = prev.work.name, prev.work.task, prev.work.hasPos
	c.pos = [3]int{prev.work.pos.X, prev.work.pos.Y, prev.work.pos.Z}
	c.running = c.task != nil && prev.work.resuming // (markInterrupted)
	prev.work.mu.Unlock()

	b.work.mu.Lock()
	b.work.name, b.work.task, b.work.hasPos = c.name, c.task, c.hasPos
	b.work.pos.X, b.work.pos.Y, b.work.pos.Z = c.pos[0], c.pos[1], c.pos[2]
	b.work.resuming = c.running
	b.work.mu.Unlock()
	b.resume = c
}

// queueWork: las órdenes de la cola que son un trabajo (startWork).
var queueWork = map[string]bool{"chop": true, "mine": true, "beds": true, "farm": true}

// resumeAfterReconnect: ya dentro otra vez, de vuelta al trabajo que se cortó.
func (b *Bot) resumeAfterReconnect() {
	c := b.resume
	if c == nil {
		return
	}
	if c.running {
		time.Sleep(2 * time.Second) // (que cargue el mundo)
		if b.ctx.Err() != nil {
			return
		}
		log.Printf("reconexión: retomo /%s", c.name)
		b.resumeWork(nil)
	}
}

// versionName: la versión de Minecraft que habla el bot.
func versionName() string { return mcversion.Name }

// markInterrupted: se corta la conexión con un trabajo en marcha: se marca
// para retomarlo (como al morir), antes de que acabe con la conexión.
func (b *Bot) markInterrupted() {
	if b.Tasks != nil {
		b.workAtDeath()
	}
}
