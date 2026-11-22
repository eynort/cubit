// Package automation son los trabajos del bot (talar, y los que vengan):
// un ejecutor que lleva una tarea a la vez y las acciones que todas usan —
// ir a un sitio, cavar, poner, recoger lo que cae, fabricar, guardar en cofres.
//
// Una tarea es una función que recibe un *Job: con él actúa y, en cada paso,
// cede ante una pelea (Job.Wait) y se entera de si la pararon (ctx).
package automation

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/world"

	"cubit/internal/i18n"
	"cubit/pathing"
)

// errText: el texto de un error en el idioma del bot (los de i18n vienen en los dos).
func errText(r *Runner, err error) string {
	var e *i18n.Error
	if errors.As(err, &e) {
		return r.tr(e.EN, e.ES)
	}
	return err.Error()
}

// Deps es lo que las tareas usan del bot.
type Deps struct {
	Client   *bot.Client
	Player   *basic.Player
	World    *world.World
	Entities *entities.Manager
	Screens  *screen.Manager
	Hands    *act.Hands
	Ctl      *control.Controller
	Walker   *pathing.Walker
	Builder  *pathing.HandsBuilder
	// Fighting dice si el bot está en una pelea (la tarea espera).
	Fighting func() bool
	// HasFood dice si lleva algo de comer (nil: como si no).
	HasFood func() bool
	// Escaping dice si el reflejo de la lava tiene el control (la tarea espera).
	Escaping func() bool
	// StopOthers para lo que mueve al bot fuera de las tareas (goto, follow…).
	StopOthers func()
	// StorageFile es dónde se guarda el almacén (los cofres).
	StorageFile string
	// Say escribe en el chat del juego (nil: no escribe).
	Say func(text string)
	// Lang: el idioma del bot para lo que dice en el chat ("es" o "en"; nil: inglés).
	Lang func() string
	// BuildFile es dónde se guarda la construcción apuntada (/build).
	BuildFile string
	// Trace escribe en el log de la sesión (solo el archivo): la traza de una tarea atascada.
	Trace func(format string, args ...any)
	// OnEnd se llama al acabar una tarea (bien, con error o parada): name y por qué.
	OnEnd func(name string, err error)
	// Problem se llama con cada problema anotado (su número, la tarea y qué
	// pasó): el índice de problemas (logs/problemas.log).
	Problem func(n int, task, msg string)
	// OnStall se llama cuando el vigilante la ve atascada: en qué estaba, el
	// intento (1-3) con el que la reempieza, o gaveUp si la para.
	OnStall func(name, status string, attempt int, gaveUp bool)
}

// Runner lleva las tareas: una a la vez.
type Runner struct {
	d     Deps
	ui    *act.Screens
	Store *Storage // las zonas protegidas (donde el servidor repone lo que rompe)

	beat    atomic.Int64        // la última señal de vida de la tarea (UnixNano): Wait, Status, Logf
	resting atomic.Bool         // la tarea espera quieta a propósito (Rest): no es un atasco
	job     atomic.Pointer[Job] // la tarea en curso (para su progreso en Status)
	chats   atomic.Int64        // los mensajes de jugadores recibidos: una respuesta (NoteChat)

	mu       sync.Mutex
	cancel   context.CancelFunc
	name     string
	status   string
	statusEN string // status en inglés (status, en español): StatusT; statusNow elige
	done     chan struct{}
}

// New crea el ejecutor (y carga el almacén guardado).
func New(d Deps) *Runner {
	r := &Runner{d: d, ui: &act.Screens{C: d.Client, Ctl: d.Ctl, Screens: d.Screens}}
	r.Store = loadStorage(d.StorageFile)
	return r
}

// Start empieza una tarea (parando la que hubiera). parent es el contexto del bot.
func (r *Runner) Start(parent context.Context, name string, task func(j *Job) error) {
	r.Stop()
	if r.d.StopOthers != nil {
		r.d.StopOthers()
	}
	r.start(parent, name, task, 0)
}

// SetProblemIndex cambia a quién se le da cada problema anotado (el índice).
func (r *Runner) SetProblemIndex(f func(n int, task, msg string)) { r.d.Problem = f }

// SetOnStall cambia a quién se avisa de una tarea atascada.
func (r *Runner) SetOnStall(f func(name, status string, attempt int, gaveUp bool)) { r.d.OnStall = f }

// SetTrace cambia dónde se escribe la traza de una tarea atascada.
func (r *Runner) SetTrace(f func(format string, args ...any)) { r.d.Trace = f }

// stallAfter: una tarea sin ninguna señal de vida tanto tiempo está atascada
// (watchEvery: cada cuánto se mira). Variables para los tests.
var (
	stallAfter = 2 * time.Minute
	watchEvery = 5 * time.Second
	// stuckAfter: dando señales de vida pero sin alejarse más de stuckMove
	// bloques ni cambiar el inventario tanto tiempo, también está atascada
	// (ir y venir entre dos bloques: cada paso es una señal de vida)
	stuckAfter = 3 * time.Minute
)

// stuckMove: lo que hay que moverse para que cuente como avanzar.
const stuckMove = 3

func (r *Runner) start(parent context.Context, name string, task func(j *Job) error, restarts int) {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	r.mu.Lock()
	r.cancel, r.name, r.status, r.statusEN, r.done = cancel, name, "empezando", "starting", done
	r.mu.Unlock()
	r.beat.Store(time.Now().UnixNano())
	go r.watch(ctx, parent, name, task, restarts, done)
	go func() {
		defer close(done)
		defer cancel()
		j := &Job{r: r, ctx: ctx}
		r.job.Store(j)
		err := task(j)
		r.job.CompareAndSwap(j, nil)
		if r.d.Walker != nil {
			r.d.Walker.Stop()
		}
		if r.d.Ctl != nil {
			r.d.Ctl.SetInput(control.Input{})
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			j.snapshot("la tarea terminó con error: " + err.Error())
		}
		sum, label := j.ProblemSummary(), r.label(name)
		switch {
		case errors.Is(err, context.Canceled):
			log.Printf(r.tr("[%s] stopped%s", "[%s] parado%s"), label, sum)
		case err != nil:
			log.Printf(r.tr("[%s] finished: %s%s", "[%s] terminado: %s%s"), label, errText(r, err), sum)
		default:
			log.Printf(r.tr("[%s] finished%s", "[%s] terminado%s"), label, sum)
		}
		r.mu.Lock()
		current := r.done == done
		if current {
			r.cancel, r.name, r.status, r.statusEN = nil, "", "", ""
		}
		r.mu.Unlock()
		if current && r.d.OnEnd != nil {
			r.d.OnEnd(name, err)
		}
	}()
}

// watch vigila que la tarea no se quede atascada: sin señal de vida en
// stallAfter (ni en una pelea, ni con una pantalla abierta: un horno tarda),
// deja en el log dónde estaba (las goroutines de las tareas) y la vuelve a
// empezar, hasta 3 veces.
func (r *Runner) watch(ctx, parent context.Context, name string, task func(j *Job) error, restarts int, done chan struct{}) {
	t := time.NewTicker(watchEvery)
	defer t.Stop()
	var prog progressWatch
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if r.d.Fighting != nil && r.d.Fighting() || r.d.Escaping != nil && r.d.Escaping() {
			r.beat.Store(time.Now().UnixNano())
			prog.pause(time.Now())
			continue
		}
		if r.d.Screens != nil {
			if _, open := r.d.Screens.Open(); open {
				r.beat.Store(time.Now().UnixNano())
				prog.pause(time.Now())
				continue
			}
		}
		// el avance: moverse o que cambie el inventario (quieto a propósito, también)
		pos, items, ok := r.progressMark()
		still := prog.update(time.Now(), pos, items, ok && !r.resting.Load())
		idle := time.Since(time.Unix(0, r.beat.Load()))
		why := ""
		switch {
		case idle >= stallAfter:
			why = fmt.Sprintf(r.tr("%s doing nothing", "%s sin hacer nada"), idle.Round(time.Second))
		case still >= stuckAfter:
			why = fmt.Sprintf(r.tr("%s without progress (within %d blocks of %v, inventory unchanged)", "%s sin avanzar (a menos de %d bloques de %v, sin cambiar el inventario)"), still.Round(time.Second), stuckMove, prog.anchor)
		default:
			continue
		}
		r.mu.Lock()
		status := r.statusNow()
		r.mu.Unlock()
		label := r.label(name)
		log.Printf(r.tr("[%s] stuck: %s (at \"%s\"): trace saved to the log", "[%s] atascada %s (en \"%s\"): dejo la traza en el log"), label, why, status)
		if j := r.job.Load(); j != nil {
			j.snapshot(fmt.Sprintf("atascada %s", why))
		}
		if r.d.Trace != nil {
			r.d.Trace("TAREA ATASCADA %s (%s):\n%s", name, status, taskStacks())
		}
		if restarts >= 3 {
			log.Printf(r.tr("[%s] stuck again: stopping it", "[%s] atascada otra vez: la paro"), label)
			if r.d.OnStall != nil {
				go r.d.OnStall(name, status, restarts, true)
			}
			r.Stop()
			return
		}
		log.Printf(r.tr("[%s] restarting it (%d of 3)", "[%s] la vuelvo a empezar (%d de 3)"), label, restarts+1)
		if r.d.OnStall != nil {
			go r.d.OnStall(name, status, restarts+1, false)
		}
		// cancelarla sin esperarla (está atascada: puede no volver nunca) y otra vez
		r.mu.Lock()
		if r.cancel != nil {
			r.cancel()
		}
		r.mu.Unlock()
		if r.d.Walker != nil {
			r.d.Walker.Stop()
		}
		r.start(parent, name, task, restarts+1)
		return
	}
}

// progressWatch mide cuánto lleva el bot sin avanzar: sin alejarse más de
// stuckMove bloques de donde empezó a contar ni cambiar lo que lleva.
type progressWatch struct {
	anchor world.BlockPos
	items  int
	since  time.Time
	set    bool
}

// pause: no cuenta (peleando, con una pantalla abierta): se empieza de nuevo.
func (w *progressWatch) pause(now time.Time) { w.set = false; w.since = now }

// update anota dónde está y lo que lleva (ok false: no se sabe, o quieto a
// propósito) y dice cuánto lleva sin avanzar.
func (w *progressWatch) update(now time.Time, pos world.BlockPos, items int, ok bool) time.Duration {
	switch {
	case !ok:
		w.pause(now)
	case !w.set || abs(pos.X-w.anchor.X) > stuckMove || abs(pos.Y-w.anchor.Y) > stuckMove ||
		abs(pos.Z-w.anchor.Z) > stuckMove || items != w.items:
		w.anchor, w.items, w.since, w.set = pos, items, now, true
	}
	return now.Sub(w.since)
}

// progressMark es dónde está el bot y cuántas cosas lleva (ok false: no se sabe).
func (r *Runner) progressMark() (world.BlockPos, int, bool) {
	if r.d.Player == nil || r.d.Screens == nil {
		return world.BlockPos{}, 0, false
	}
	p := r.d.Player.Position()
	pos := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y)), Z: int(math.Floor(p.Z))}
	s := r.d.Screens
	s.Lock()
	n := 0
	for i := invStart; i <= invEnd; i++ {
		n += int(s.Inventory.Slots[i].Count)
	}
	s.Unlock()
	return pos, n, true
}

// Rest espera ticks quieta a propósito (la granja esperando a que crezca el
// trigo): el vigilante no lo toma por un atasco.
func (j *Job) Rest(ticks int) error {
	j.r.resting.Store(true)
	defer j.r.resting.Store(false)
	return j.Wait(ticks)
}

// taskStacks son las pilas de las goroutines que están en el código de las tareas.
func taskStacks() string {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	var out []string
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(g, "cubit/automation") {
			out = append(out, g)
		}
	}
	return strings.Join(out, "\n\n")
}

// Stop para la tarea en curso (y espera a que suelte el bot).
func (r *Runner) Stop() {
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
}

// NoteProtected: el servidor dijo que en p (donde está el bot) no se puede
// construir ni romper. Se apunta la zona (y se guarda): las tareas la evitan.
func (r *Runner) NoteProtected(p world.BlockPos) {
	r.Store.NoteDenied(p) // (siempre, aunque esté cerca de una zona ya guardada)
	if r.Store.NoteProtected(p) {
		log.Printf("zona protegida apuntada alrededor de %v: ahí no toco nada", p)
	}
}

// KeepGold dice si la armadura de oro tiene que quedarse puesta: en el Nether
// (los piglins atacan a quien no lleva oro).
func (r *Runner) KeepGold() bool {
	return string(r.d.Player.Spawn.Dimension) == theNether
}

// Status es la tarea en curso y por dónde va ("" si ninguna).
func (r *Runner) Status() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.name == "" {
		return ""
	}
	label := r.label(r.name)
	cur := r.statusNow()
	if strings.HasPrefix(cur, label+":") {
		return cur // (el estado ya lleva el nombre: "excavar: …")
	}
	return label + ": " + cur
}

// statusNow es el estado en el idioma del bot (con r.mu).
func (r *Runner) statusNow() string { return r.tr(r.statusEN, r.status) }

// StatusEN es Status en inglés (el estado que se le da a la IA).
func (r *Runner) StatusEN() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.name == "" {
		return ""
	}
	label := r.name
	if en, ok := taskLabels[r.name]; ok {
		label = en
	}
	return label + ": " + r.statusEN
}

// Job es una tarea en marcha: lo que puede hacer, paso a paso.
type Job struct {
	r       *Runner
	ctx     context.Context
	trail   []world.BlockPos       // las celdas de los túneles que cavó (para volver a salir)
	fatal   error                  // por qué la tarea tiene que parar (sin comida y herido): cada Wait lo devuelve
	ignore  func(item string) bool // lo que Collect no recoge (la basura que tiró)
	feeding bool                   // buscando comida (no se vuelve a empezar desde Wait)
	// pickFloor: los picos con estos usos o menos no se usan (0: cualquiera).
	// Lo pone /mine con los picos del cofre, para no romper ninguno.
	pickFloor int
	// pickOK: qué picos cuentan para pickFloor (nil: los de diamante y netherita; /mine: los del nivel del mineral)
	pickOK func(string) bool
	// protectSince, protectR: con protectSince puesto, las zonas protegidas que
	// cuentan son solo los "no" del servidor desde protectSince, y a protectR
	// bloques (/excavate: la caja la eligió el jugador; lo apuntado antes, a
	// 16 de donde estaba el bot, no la bloquea entera)
	protectSince time.Time
	protectR     int
	// toolFloor: las palas, hachas y azadas con estos usos o menos no se usan (0: cualquiera). /excavate: no se rompe ninguna
	toolFloor int
	// goLimit: las vueltas de 2 ticks que GoNear y GoTo esperan antes de rendirse (0: 60 s)
	goLimit   int
	plugged   int                     // celdas de lava tapadas (para el resumen)
	junkPit   bool                    // la basura, a un hoyo tapado (makeRoom), como en el Nether
	pits      map[world.BlockPos]bool // las celdas de los hoyos de basura: no se cavan
	pitFloors []world.BlockPos        // el fondo de cada hoyo (donde está lo tirado)
	unburying bool                    // cavando la grava que le cayó encima (unbury)
	// sin dónde guardar: storageAsked ya se preguntó por el chat; noStore se
	// sigue sin guardar ni recoger (noStoreSig: dónde se podía guardar entonces)
	// collectOnly: si no es nil, Collect solo coge lo que está donde dice (la caja de /excavate)
	collectOnly func(world.BlockPos) bool
	// collectOpen: si no es nil, es lo que se prueba para llegar a un objeto que no se alcanza (abrirse paso entre las hojas)
	collectOpen func(world.BlockPos) bool
	// collectUntil: si no es cero, CollectAround deja de ir a por objetos pasada esa hora
	collectUntil time.Time
	storageAsked bool
	pickAsked    bool            // ya se pidió un pico por el chat en esta tarea
	farTold      map[string]bool // los cofres lejanos que ya se dijo que no se usan (tooFar)
	// el cofre de minar que se hizo en esta tarea (makeMineChest), si se intentó,
	// si falta dónde guardar y si ya se avisó de ello (lootCheck)
	localChest    *world.BlockPos
	localChestDim string
	chestTried    bool
	storeMissing  bool
	lootWarned    bool
	noStore       bool
	noStoreSig    string
	storeTold     bool // ya se dijo por el chat que sigue sin guardar
	// los problemas anotados (Problem): cuántos de cada clase y sus números
	pmu        sync.Mutex
	problems   map[string]int
	problemIDs []int
}

// Fatal es el error por el que la tarea tiene que parar (nil si sigue);
// también se para si se canceló.
func (j *Job) Fatal() error {
	if j.fatal != nil {
		return j.fatal
	}
	return j.ctx.Err()
}

// Ctx es el contexto de la tarea (cancelado con /stop).
func (j *Job) Ctx() context.Context { return j.ctx }

// D son las piezas del bot.
func (j *Job) D() Deps { return j.r.d }

// Status dice por dónde va la tarea (sale en /status y en la barra).
func (j *Job) Status(format string, args ...any) {
	j.r.beat.Store(time.Now().UnixNano())
	j.r.mu.Lock()
	j.r.status = fmt.Sprintf(format, args...)
	j.r.statusEN = j.r.status
	j.r.mu.Unlock()
}

// StatusT es Status en el idioma del bot (en, es: los dos con los mismos %v).
func (j *Job) StatusT(en, es string, args ...any) {
	j.r.beat.Store(time.Now().UnixNano())
	j.r.mu.Lock()
	j.r.status = fmt.Sprintf(es, args...)
	j.r.statusEN = fmt.Sprintf(en, args...)
	j.r.mu.Unlock()
}

// LogT es Logf en el idioma del bot (en, es: los dos con los mismos %v).
func (j *Job) LogT(en, es string, args ...any) { j.Logf(j.tr(en, es), args...) }

// tr elige el texto en el idioma del bot (Deps.Lang; sin él, el de i18n).
func (j *Job) tr(en, es string) string { return j.r.tr(en, es) }

func (r *Runner) tr(en, es string) string {
	l := i18n.Lang()
	if r.d.Lang != nil {
		l = r.d.Lang()
	}
	if l == "es" {
		return es
	}
	return en
}

// taskLabels: el nombre de cada tarea en inglés (el interno, el de la orden
// en español, es el que sale con /lang es).
var taskLabels = map[string]string{
	"excavar": "excavate",
	"talar":   "chop",
	"minar":   "mine",
	"recoger": "pick up",
	"afk":     "afk",
}

// Label es cómo se llama la tarea name en /status, el panel y los logs, en
// el idioma del bot.
func Label(name string) string {
	if en, ok := taskLabels[name]; ok {
		return i18n.T(en, name)
	}
	return name
}

func (r *Runner) label(name string) string {
	if en, ok := taskLabels[name]; ok {
		return r.tr(en, name)
	}
	return name
}

// Logf escribe en el log con el nombre de la tarea.
func (j *Job) Logf(format string, args ...any) {
	j.r.beat.Store(time.Now().UnixNano())
	j.r.mu.Lock()
	name := j.r.name
	j.r.mu.Unlock()
	log.Printf("["+j.r.label(name)+"] "+format, args...)
}

// Wait es un punto seguro: espera n ticks y, si hay una pelea, hasta que
// acabe (la pelea manda: luego la tarea sigue). Devuelve el error si la tarea se paró.
func (j *Job) Wait(n int) error {
	if j.fatal != nil {
		return j.fatal
	}
	if err := j.r.d.Ctl.WaitTicks(j.ctx, n); err != nil {
		return err
	}
	j.r.beat.Store(time.Now().UnixNano())
	j.unbury()
	if j.r.d.Escaping != nil && j.r.d.Escaping() {
		j.Logf("lava: espero a estar a salvo")
		for j.r.d.Escaping() {
			if err := j.r.d.Ctl.WaitTicks(j.ctx, 5); err != nil {
				return err
			}
		}
		j.Logf("a salvo de la lava: la tapo antes de seguir")
		if j.plugExposed(4) == 0 && j.lavaNear(3) {
			// no se pudo tapar: no volver allí (la tarea busca otro camino)
			j.Logf("hay lava cerca que no puedo tapar: me alejo")
			return errLavaNear
		}
	}
	if j.r.d.Fighting != nil && j.r.d.Fighting() {
		j.Logf("pelea: espero a que termine")
		for j.r.d.Fighting() {
			if err := j.r.d.Ctl.WaitTicks(j.ctx, 10); err != nil {
				return err
			}
		}
		j.Logf("pelea terminada: sigo")
	}
	if err := j.heal(); err != nil {
		j.fatal = err
		return err
	}
	return nil
}

// heal: con poca vida la tarea no sigue (una caída, un mob más y se muere):
// quieto hasta curarse (come lo que lleve: lo hace la pelea fuera de combate;
// con hambre 18 o más se cura solo). Sin comida y con hambre no se cura nunca:
// error, la tarea se para.
func (j *Job) heal() error {
	st := j.r.d.Player.Status()
	if st.Health <= 0 || st.Health >= 8 || j.feeding {
		return nil // (buscando comida: esa es la cura; no se le corta el paso)
	}
	j.r.d.Walker.Stop()
	j.Logf("vida %.1f (comida %d): paro a curarme", st.Health, st.Food)
	start := time.Now()
	for {
		st = j.r.d.Player.Status()
		if st.Health >= 14 || st.Health <= 0 {
			j.Logf("curado (vida %.1f): sigo", st.Health)
			return nil
		}
		hasFood := j.r.d.HasFood != nil && j.r.d.HasFood()
		if !hasFood && st.Food < 18 {
			return fmt.Errorf("vida %.1f y sin comida: así no me curo, paro (dame comida)", st.Health)
		}
		if time.Since(start) > 4*time.Minute {
			return fmt.Errorf("no me curo (vida %.1f): paro", st.Health)
		}
		j.r.beat.Store(time.Now().UnixNano()) // curarse lleva su tiempo: no es un atasco
		if err := j.r.d.Ctl.WaitTicks(j.ctx, 20); err != nil {
			return err
		}
	}
}

// Pos es la posición del bot.
func (j *Job) Pos() basic.Position { return j.r.d.Player.Position() }

// Feet es la celda de los pies.
func (j *Job) Feet() world.BlockPos {
	p := j.Pos()
	return world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
}

// Block es el bloque en p ("" si no está cargado), sin "minecraft:" ni propiedades.
func (j *Job) Block(p world.BlockPos) string {
	s, ok := j.r.d.World.BlockAt(p)
	if !ok {
		return ""
	}
	return blockName(s)
}
