package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/mj41/go-mc26/level/block"
	"log"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/automation"
)

// El trabajo largo (talar, escombros, granja, camas) sobrevive a una muerte:
// al empezar se pone una home del servidor donde está (/sethome botwork); si
// muere, al reaparecer vuelve con /home (o andando si no hay home), recoge lo
// que tiró si fue cerca y retoma la misma tarea. Al terminar (o con /stop)
// se borra la home. Si muere demasiadas veces seguidas, lo deja.

const (
	workHome      = "botwork"
	maxWorkDeaths = 4 // en workDeathWindow: más, y se para (algo lo mata una y otra vez)
)

var workDeathWindow = 30 * time.Minute

type workState struct {
	mu       sync.Mutex
	name     string
	task     func(*automation.Job) error
	home     bool           // se puso la home
	homeAt   time.Time      // cuándo se pidió (para atribuirle la respuesta del servidor)
	hasPos   bool           // homePos vale
	homePos  world.BlockPos // dónde empezó (para volver andando si no hay home)
	resuming bool           // parada por una muerte: se retoma (no borrar la home)
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
func (b *Bot) reportWork(text string) {
	notify(text)
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

// onTaskStall: el vigilante vio la tarea atascada (la reempieza, o la para).
func (b *Bot) onTaskStall(name, status string, attempt int, gaveUp bool) {
	if gaveUp {
		b.reportWork(fmt.Sprintf("[%s] me he atascado otra vez en \"%s\": lo dejo", name, status))
		return
	}
	b.reportWork(fmt.Sprintf("[%s] me he atascado en \"%s\": vuelvo a empezar (%d de 3)", name, status, attempt))
}

// untilState: las condiciones de parada ("until …") de la orden que se está
// ejecutando, para el trabajo que empiece (startWork las recoge).
var untilState struct {
	sync.Mutex
	conds []automation.Cond
	used  bool
}

// startWork empieza una tarea larga con su home de vuelta.
func (b *Bot) startWork(name string, task func(*automation.Job) error) {
	untilState.Lock()
	if untilState.conds != nil {
		task = automation.Until(task, untilState.conds)
		var cs []string
		for _, c := range untilState.conds {
			cs = append(cs, c.String())
		}
		log.Printf("%s: hasta %s", name, strings.Join(cs, " o "))
		untilState.used = true
	}
	untilState.Unlock()
	p := b.Player.Position()
	here := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y)), Z: int(math.Floor(p.Z))}
	b.work.mu.Lock()
	b.work.name, b.work.task, b.work.resuming = name, task, false
	orderFrom.Lock()
	b.work.by = orderFrom.who
	orderFrom.Unlock()
	setHome := !b.work.home && !b.Tasks.Store.IsProtected(here)
	if !b.work.hasPos || setHome {
		b.work.homePos, b.work.hasPos = here, true
	}
	if setHome {
		b.work.home, b.work.homeAt = true, time.Now()
	}
	b.work.mu.Unlock()
	if setHome {
		log.Printf("trabajo: pongo la home \"%s\" aquí (%v) para volver si muero", workHome, here)
		if err := b.Chat.SendCommand("sethome " + workHome); err != nil {
			log.Print("sethome: ", err)
		}
	}
	b.Tasks.Start(b.ctx, name, task)
}

// onTaskEnd: una tarea acabó. Si era el trabajo y no se va a retomar (una
// muerte), se borra su home.
func (b *Bot) onTaskEnd(name string, err error) {
	// por qué paró, en el chat: si no, quien lo mandó no se entera
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, automation.ErrStopCondition) {
		b.reportWork(fmt.Sprintf("[%s] paro: %v", name, err))
	} else if err == nil || errors.Is(err, automation.ErrStopCondition) {
		notify(fmt.Sprintf("[%s] terminado", name))
	}
	b.work.mu.Lock()
	mine := b.work.name == name && !b.work.resuming
	hadHome := b.work.home
	if mine {
		b.work.name, b.work.task, b.work.home, b.work.hasPos = "", nil, false, false
	}
	b.work.mu.Unlock()
	if mine && hadHome {
		log.Printf("trabajo terminado: borro la home \"%s\"", workHome)
		if err := b.Chat.SendCommand("delhome " + workHome); err != nil {
			log.Print("delhome: ", err)
		}
	}
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

// resumeWork: tras reaparecer, de vuelta al trabajo (con /home o andando),
// recoge lo que tiró si murió cerca y retoma la tarea.
func (b *Bot) resumeWork(death *deathSpot) {
	b.work.mu.Lock()
	name, task, home, homePos, hasPos := b.work.name, b.work.task, b.work.home, b.work.homePos, b.work.hasPos
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
	back := false
	if home {
		start := b.Player.Position()
		log.Printf("trabajo: vuelvo con /home %s", workHome)
		if err := b.Chat.SendCommand("home " + workHome); err == nil {
			for i := 0; i < 60 && !back; i++ { // hasta 15 s (algunos servidores hacen esperar)
				if b.Ctl.WaitTicks(ctx, 5) != nil {
					b.abandonWork(name)
					return
				}
				p := b.Player.Position()
				back = math.Hypot(p.X-start.X, p.Z-start.Z) > 8
			}
		}
	}
	if !back && hasPos {
		log.Printf("trabajo: vuelvo andando a donde trabajaba (%v)", homePos)
		back = b.gotoCoords(ctx, float64(homePos.X)+0.5, float64(homePos.Z)+0.5, nil)
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
// da por terminado (y se borra su home).
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

// homeReply: un mensaje del servidor justo tras el /sethome que dice que no se
// pudo (sitio inseguro, sin permiso, límite de homes): no hay home; si muere,
// volverá andando.
var homeFailMsg = regexp.MustCompile(`(?i)(error|unsafe|insegur|no puedes|cannot|can't|not allowed|límite|limit|permiso)`)

func (b *Bot) homeReply(text string) {
	b.work.mu.Lock()
	defer b.work.mu.Unlock()
	if !b.work.home || time.Since(b.work.homeAt) > 3*time.Second || !homeFailMsg.MatchString(text) {
		return
	}
	b.work.home = false
	log.Printf("trabajo: el servidor no me deja poner la home (%s): si muero, vuelvo andando", text)
}

// retryHome: hay un trabajo sin home (el servidor dijo que el sitio era
// inseguro: dentro de una mina, en el aire): cada 2 minutos, si está de pie
// con el cielo encima, lo intenta otra vez.
func (b *Bot) retryHome() {
	b.work.mu.Lock()
	try := b.work.name != "" && !b.work.home && !b.work.resuming && time.Since(b.work.homeAt) > 2*time.Minute
	b.work.mu.Unlock()
	if !try || !b.Ctl.State().OnGround || b.Combat.Fighting() {
		return
	}
	p := b.Player.Position()
	here := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y)), Z: int(math.Floor(p.Z))}
	if b.Tasks.Store.IsProtected(here) || !b.skyAbove(here) {
		return
	}
	b.work.mu.Lock()
	b.work.home, b.work.homeAt, b.work.homePos, b.work.hasPos = true, time.Now(), here, true
	b.work.mu.Unlock()
	log.Printf("trabajo: pruebo otra vez a poner la home \"%s\" (%v)", workHome, here)
	if err := b.Chat.SendCommand("sethome " + workHome); err != nil {
		log.Print("sethome: ", err)
	}
}

// skyAbove: nada macizo encima de p (hasta 40 bloques; las hojas no cuentan).
func (b *Bot) skyAbove(p world.BlockPos) bool {
	for y := p.Y + 2; y <= p.Y+40; y++ {
		q := world.BlockPos{X: p.X, Y: y, Z: p.Z}
		st, ok := b.World.BlockAt(q)
		if !ok {
			return false
		}
		if n := world.StateString(st); !block.IsAir(st) && !strings.Contains(n, "_leaves") && !strings.Contains(n, "_log") && len(block.CollisionShape(st)) > 0 {
			return false
		}
	}
	return true
}
