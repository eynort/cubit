package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mj41/go-mc26-kit/bot/world"

	"cubit/automation"
	"cubit/internal/i18n"
)

// El trabajo largo (talar, escombros, granja, camas) sobrevive a una muerte:
// al empezar se apunta dónde está; si muere, al reaparecer vuelve andando,
// recoge lo que tiró si fue cerca y retoma la misma tarea. Si muere
// demasiadas veces seguidas, lo deja. (Sin comandos del servidor: vale en
// cualquiera.)

const maxWorkDeaths = 4 // en workDeathWindow: más, y se para (algo lo mata una y otra vez)

var workDeathWindow = 30 * time.Minute

type workState struct {
	mu       sync.Mutex
	name     string
	task     func(*automation.Job) error
	hasPos   bool           // pos vale
	pos      world.BlockPos // dónde empezó (para volver andando si muere)
	resuming bool           // parada por una muerte: se retoma
	deaths   []time.Time
	by       string // quién lo mandó por /msg ("" desde la consola o la cola): a quién avisar
}

// orderFrom es quién manda la orden que se está ejecutando (/msg), para el
// trabajo que empiece: a él se le avisa por privado si se atasca.
var orderFrom struct {
	sync.Mutex
	who string
}

// asOrderFrom ejecuta f como orden de who (startWork lo recoge).
func asOrderFrom(who string, f func() error) error {
	orderFrom.Lock()
	orderFrom.who = who
	orderFrom.Unlock()
	defer func() {
		orderFrom.Lock()
		orderFrom.who = ""
		orderFrom.Unlock()
	}()
	return f()
}

// reportWork cuenta en el chat qué le pasa al trabajo: por privado a quien lo
// mandó por /msg, o en el chat si vino de la consola (lo ve quien lo vigila).
func (b *Bot) reportWork(format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	log.Printf("chat: %s", text) // (el puente de Discord del hub lo lee del log)
	b.work.mu.Lock()
	by := b.work.by
	b.work.mu.Unlock()
	if by != "" {
		b.whisper(by, text)
		return
	}
	if b.Chat == nil {
		return
	}
	if err := b.Chat.SendMessage(shortChat(text, 240)); err != nil {
		log.Print("chat: ", err)
	}
}

// shortChat corta un mensaje a n bytes (sin partir una letra) con "…".
func shortChat(text string, n int) string {
	if len(text) <= n {
		return text
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}

// taskNames: cómo se nombra cada tarea en el chat ("con lo de …").
var taskNames = map[string]text{
	"excavar": {"the digging", "excavar"},
	"talar":   {"the chopping", "talar"},
	"minar":   {"the mining", "minar"},
	"recoger": {"picking stuff up", "recoger cosas"},
}

func taskName(name string) string {
	if t, ok := taskNames[name]; ok {
		return t.String()
	}
	return name
}

// errText: el texto de un error en el idioma del bot (los de i18n vienen en los dos).
func errText(err error) string {
	var e *i18n.Error
	if errors.As(err, &e) {
		return tr(e.EN, e.ES)
	}
	return err.Error()
}

// onTaskStall: el vigilante vio la tarea atascada (la reempieza, o la para).
func (b *Bot) onTaskStall(name, status string, attempt int, gaveUp bool) {
	if gaveUp {
		b.reportWork(tr("still stuck with %s, I'm leaving it", "nada, sigo trabado con %s, lo dejo"), taskName(name))
		return
	}
	b.reportWork(tr("I got stuck with %s, trying again (%d/3)", "me trabé con %s, lo intento otra vez (%d/3)"), taskName(name), attempt)
}

// startWork empieza una tarea larga (apuntando dónde, para volver si muere).
func (b *Bot) startWork(name string, task func(*automation.Job) error) {
	p := b.Player.Position()
	here := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y)), Z: int(math.Floor(p.Z))}
	b.work.mu.Lock()
	b.work.name, b.work.task, b.work.resuming = name, task, false
	orderFrom.Lock()
	b.work.by = orderFrom.who
	orderFrom.Unlock()
	if !b.work.hasPos {
		b.work.pos, b.work.hasPos = here, true
	}
	b.work.mu.Unlock()
	b.Tasks.Start(b.ctx, name, task)
}

// onTaskEnd: una tarea acabó. Si era el trabajo y no se va a retomar (una
// muerte), se olvida.
func (b *Bot) onTaskEnd(name string, err error) {
	// por qué paró, en el chat: si no, quien lo mandó no se entera
	if err != nil && !errors.Is(err, context.Canceled) {
		b.reportWork(tr("I stopped %s: %s", "dejo %s: %s"), taskName(name), errText(err))
	}
	b.work.mu.Lock()
	if b.work.name == name && !b.work.resuming {
		b.work.name, b.work.task, b.work.hasPos = "", nil, false
	}
	b.work.mu.Unlock()
}

// workAtDeath: al morir, si había un trabajo en marcha, se marca para
// retomarlo (devuelve si lo había).
func (b *Bot) workAtDeath() bool {
	b.work.mu.Lock()
	defer b.work.mu.Unlock()
	if b.work.name == "" || (b.Tasks.Status() == "" && !b.work.resuming) {
		return false // (muerto volviendo al trabajo: también se retoma)
	}
	b.work.resuming = true
	return true
}

// resumeWork: tras reaparecer, de vuelta al trabajo (andando),
// recoge lo que tiró si murió cerca y retoma la tarea.
func (b *Bot) resumeWork(death *deathSpot) {
	b.work.mu.Lock()
	name, task, pos, hasPos := b.work.name, b.work.task, b.work.pos, b.work.hasPos
	now := time.Now()
	var recent []time.Time
	for _, t := range b.work.deaths {
		if now.Sub(t) < workDeathWindow {
			recent = append(recent, t)
		}
	}
	b.work.deaths = append(recent, now)
	tooMany := len(b.work.deaths) > maxWorkDeaths
	b.work.mu.Unlock()
	if task == nil {
		return
	}
	if tooMany {
		log.Printf("trabajo: he muerto %d veces en %s: dejo /%s (algo me mata una y otra vez)", maxWorkDeaths+1, workDeathWindow, name)
		b.work.mu.Lock()
		b.work.resuming = false
		b.work.mu.Unlock()
		b.onTaskEnd(name, nil)
		return
	}
	b.waitRespawn()
	ctx, cancel := b.startMove(b.ctx)
	gen := setMoveKind("goto")
	defer cancel()
	defer clearMove(gen)
	if hasPos {
		log.Printf("trabajo: vuelvo andando a donde trabajaba (%v)", pos)
		b.gotoCoords(ctx, float64(pos.X)+0.5, float64(pos.Z)+0.5, nil)
	}
	if ctx.Err() != nil {
		b.abandonWork(name) // una orden del operador tomó el control
		return
	}
	// lo que tiré al morir, si está cerca y aún no ha desaparecido
	if death != nil && time.Since(death.at) < 4*time.Minute {
		p := b.Player.Position()
		if d := math.Hypot(death.x-p.X, death.z-p.Z); d < 96 && b.gotoCoords(ctx, death.x, death.z, nil) {
			b.lastDeath.Store(nil)
			log.Print("trabajo: recojo lo que tiré al morir")
			b.runAndWait("recoger", automation.PickUp(12))
		}
	}
	if ctx.Err() != nil {
		b.abandonWork(name)
		return
	}
	clearMove(gen)
	b.work.mu.Lock()
	b.work.resuming = false
	b.work.mu.Unlock()
	log.Printf("trabajo: retomo /%s", name)
	b.Tasks.Start(b.ctx, name, task)
}

// waitRespawn espera a estar vivo otra vez y con el mundo cargado.
func (b *Bot) waitRespawn() {
	for i := 0; i < 100; i++ {
		time.Sleep(200 * time.Millisecond)
		if b.Player.Status().Health > 0 && b.Ctl.State().Loaded {
			break
		}
	}
	time.Sleep(2 * time.Second)
}

// runAndWait hace una tarea corta y espera a que acabe (hasta 2 minutos).
func (b *Bot) runAndWait(name string, task func(*automation.Job) error) {
	b.Tasks.Start(b.ctx, name, task)
	ctx, cancel := context.WithTimeout(b.ctx, 2*time.Minute)
	defer cancel()
	for b.Tasks.Status() != "" && ctx.Err() == nil {
		time.Sleep(500 * time.Millisecond)
	}
}

// abandonWork: el operador mandó otra cosa mientras se volvía al trabajo: se
// da por terminado.
func (b *Bot) abandonWork(name string) {
	b.work.mu.Lock()
	b.work.resuming = false
	b.work.mu.Unlock()
	b.onTaskEnd(name, nil)
}

// workResuming: se está volviendo al trabajo tras una muerte (cuenta como en marcha).
func (b *Bot) workResuming() bool {
	b.work.mu.Lock()
	defer b.work.mu.Unlock()
	return b.work.resuming
}
