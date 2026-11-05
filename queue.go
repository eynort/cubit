package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"minecraft-bot/automation"
)

// La cola de órdenes de primer nivel (goto, guard, fight, chop, mine, y las
// tareas beds/farm): el operador las pone en fila y el bot las hace una
// tras otra. Se puede ver, añadir, quitar, mover, saltar la actual y pausar.
//
// Una orden de primer nivel escrita directamente (sin /queue) se hace al
// momento, como siempre: la de la cola que estaba en marcha se interrumpe y
// vuelve a ponerse la primera; la cola sigue cuando la directa termina.
// /stop para lo que haya y pausa la cola (/queue resume la reanuda).

// queueKinds son las órdenes que pueden ir en la cola.
var queueKinds = map[string]bool{
	"goto": true, "guard": true, "fight": true, "chop": true, "mine": true, "beds": true, "farm": true,
}

type queueItem struct {
	id   int
	line string // la orden sin "/": "chop oak 20"
}

func (it queueItem) kind() string { return strings.ToLower(strings.Fields(it.line)[0]) }

// Queue es la cola. start arranca una orden; busy dice si la orden de ese
// tipo sigue en marcha; idle dice si el bot está libre (sin nada directo).
type Queue struct {
	mu     sync.Mutex
	items  []queueItem
	cur    *queueItem
	since  time.Time
	paused bool
	nextID int

	start func(line string) error
	busy  func(kind string) bool
	idle  func() bool
	now   func() time.Time
}

// grace: tras arrancar una orden, lo que se tarda en darla por terminada si
// no se ve en marcha (una pelea tarda un momento en empezar).
const queueGrace = 3 * time.Second

// Add pone una orden al final (o en la posición pos, 1 = la primera; 0 = al final).
func (q *Queue) Add(line string, pos int) (queueItem, error) {
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "/"))
	f := strings.Fields(line)
	if len(f) == 0 {
		return queueItem{}, errf("missing the command (goto, guard, fight, chop, mine, beds, farm)", "falta la orden (goto, guard, fight, chop, mine, beds, farm)")
	}
	if !queueKinds[strings.ToLower(f[0])] {
		return queueItem{}, errf("/%s cannot be queued (these can: goto, guard, fight, chop, mine, beds, farm)", "/%s no va en la cola (sí: goto, guard, fight, chop, mine, beds, farm)", f[0])
	}
	if _, until := automation.SplitUntil(f[1:]); until != nil { // las condiciones, comprobadas ya (no al llegarle el turno)
		if _, err := automation.ParseUntil(until); err != nil {
			return queueItem{}, fmt.Errorf("until: %w", err)
		}
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.nextID++
	it := queueItem{id: q.nextID, line: line}
	if pos <= 0 || pos > len(q.items) {
		q.items = append(q.items, it)
	} else {
		q.items = append(q.items[:pos-1], append([]queueItem{it}, q.items[pos-1:]...)...)
	}
	return it, nil
}

// Remove quita la orden de la posición pos (1 = la primera pendiente).
func (q *Queue) Remove(pos int) (queueItem, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if pos < 1 || pos > len(q.items) {
		return queueItem{}, errf("there is no item %d (pending: %d)", "no hay orden %d (pendientes: %d)", pos, len(q.items))
	}
	it := q.items[pos-1]
	q.items = append(q.items[:pos-1], q.items[pos:]...)
	return it, nil
}

// Move lleva la orden de la posición from a la posición to.
func (q *Queue) Move(from, to int) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := len(q.items)
	if from < 1 || from > n || to < 1 || to > n {
		return errf("positions go from 1 to %d", "posiciones entre 1 y %d", n)
	}
	it := q.items[from-1]
	rest := append(append([]queueItem{}, q.items[:from-1]...), q.items[from:]...)
	q.items = append(rest[:to-1], append([]queueItem{it}, rest[to-1:]...)...)
	return nil
}

// Clear vacía las pendientes (la actual sigue).
func (q *Queue) Clear() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := len(q.items)
	q.items = nil
	return n
}

// SetPaused pausa o reanuda la cola.
func (q *Queue) SetPaused(p bool) {
	q.mu.Lock()
	q.paused = p
	q.mu.Unlock()
}

// Current es la orden en marcha ("" ninguna).
func (q *Queue) Current() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.cur == nil {
		return ""
	}
	return q.cur.line
}

// Pending son las órdenes que esperan, en orden.
func (q *Queue) Pending() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]string, len(q.items))
	for i, it := range q.items {
		out[i] = it.line
	}
	return out
}

// Describe es la cola para mostrarla.
func (q *Queue) Describe() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	var sb strings.Builder
	switch {
	case q.cur != nil:
		fmt.Fprintf(&sb, tr("running: /%s", "en marcha: /%s"), q.cur.line)
	case q.paused:
		sb.WriteString(tr("paused (/queue resume)", "en pausa (/queue resume)"))
	default:
		sb.WriteString(tr("nothing running", "nada en marcha"))
	}
	if q.paused && q.cur != nil {
		sb.WriteString(tr(" — queue paused", " — cola en pausa"))
	}
	if len(q.items) == 0 {
		sb.WriteString(tr("; queue empty", "; cola vacía"))
	}
	for i, it := range q.items {
		fmt.Fprintf(&sb, "\n  %d. /%s", i+1, it.line)
	}
	return sb.String()
}

// Short es la cola en pocas letras (la barra de estado): "" si no hay nada.
func (q *Queue) Short() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.cur == nil && len(q.items) == 0 {
		return ""
	}
	s := fmt.Sprintf(tr("queue %d", "cola %d"), len(q.items))
	if q.paused {
		s += tr(" (paused)", " (pausa)")
	}
	return s
}

// Interrupt: una orden directa toma el control. La de la cola que estaba en
// marcha vuelve a ponerse la primera (se retoma después).
func (q *Queue) Interrupt() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.cur == nil {
		return
	}
	log.Printf(tr("queue: /%s interrupted by a direct command; it goes back to the front", "cola: /%s interrumpida por una orden directa; la vuelvo a poner la primera"), q.cur.line)
	q.items = append([]queueItem{*q.cur}, q.items...)
	q.cur = nil
}

// Stop: /stop. La actual se descarta y la cola se pausa.
func (q *Queue) Stop() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.cur != nil {
		log.Printf(tr("queue: /%s stopped", "cola: /%s parada"), q.cur.line)
		q.cur = nil
	}
	if len(q.items) > 0 && !q.paused {
		log.Printf(tr("queue: paused (%d pending; /queue resume to continue)", "cola: en pausa (%d pendientes; /queue resume para reanudar)"), len(q.items))
	}
	q.paused = q.paused || len(q.items) > 0
}

// Skip termina la actual (la siguiente empieza si la cola no está en pausa).
// Devuelve la que se saltó ("" ninguna).
func (q *Queue) Skip() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.cur == nil {
		return ""
	}
	s := q.cur.line
	q.cur = nil
	return s
}

// step es una vuelta del ejecutor: da por terminada la actual si ya no está
// en marcha y, si el bot está libre, arranca la siguiente.
func (q *Queue) step() {
	q.mu.Lock()
	if q.cur != nil {
		cur, since := *q.cur, q.since
		q.mu.Unlock()
		if q.now().Sub(since) < queueGrace || q.busy(cur.kind()) {
			return
		}
		q.mu.Lock()
		if q.cur != nil && q.cur.id == cur.id {
			log.Printf(tr("queue: finished /%s", "cola: terminada /%s"), cur.line)
			q.cur = nil
		}
	}
	if q.cur != nil || q.paused || len(q.items) == 0 {
		q.mu.Unlock()
		return
	}
	q.mu.Unlock()
	if !q.idle() {
		return // algo directo en marcha: la cola espera
	}
	q.mu.Lock()
	if q.cur != nil || q.paused || len(q.items) == 0 {
		q.mu.Unlock()
		return
	}
	it := q.items[0]
	q.items = q.items[1:]
	q.cur, q.since = &it, q.now()
	left := len(q.items)
	q.mu.Unlock()
	log.Printf(tr("queue: starting /%s (%d left)", "cola: empiezo /%s (quedan %d)"), it.line, left)
	if err := q.start(it.line); err != nil {
		log.Printf(tr("queue: /%s: %v (removed, going on)", "cola: /%s: %v (la quito y sigo)"), it.line, err)
		q.mu.Lock()
		if q.cur != nil && q.cur.id == it.id {
			q.cur = nil
		}
		q.mu.Unlock()
	}
}

// Run es el ejecutor: una vuelta cada medio segundo hasta que ctx se cierra.
func (q *Queue) Run(ctx context.Context) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			q.step()
		}
	}
}

// ---- con el bot ----

// newQueue crea la cola del bot: arranca las órdenes con sus funciones de
// siempre y mira si siguen en marcha.
func (b *Bot) newQueue() *Queue {
	return &Queue{
		now: time.Now,
		start: func(line string) error {
			f := strings.Fields(line)
			c := commands[strings.ToLower(f[0])]
			return b.runCommand(c, f[0], f[1:])
		},
		busy: func(kind string) bool { return b.busyWith(kind) },
		idle: func() bool {
			return b.Tasks.Status() == "" && currentMove() == "" && !b.Combat.Fighting() && !b.workResuming() &&
				b.Combat.Guarding() == "" && b.Combat.CurrentZone() == nil
		},
	}
}

// busyWith dice si la orden de tipo kind sigue en marcha.
func (b *Bot) busyWith(kind string) bool {
	switch kind {
	case "chop", "mine", "beds", "farm":
		return b.Tasks.Status() != "" || b.workResuming()
	case "goto":
		return currentMove() == "goto"
	case "guard":
		return currentMove() == "guard" || b.Combat.Guarding() != ""
	case "fight":
		return b.Combat.Fighting() || b.Combat.CurrentZone() != nil
	}
	return false
}

// cmdQueue: /queue [list | add <command…> | insert <pos> <command…> |
// remove <n> | move <from> <to> | up <n> | down <n> | skip | clear | pause | resume].
func cmdQueue(b *Bot, args []string) error {
	q := b.Queue
	sub := "list"
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}
	num := func(i int) (int, error) {
		if len(args) <= i {
			return 0, errf("missing a position number (see /queue)", "falta un número de posición (mira /queue)")
		}
		n, err := strconv.Atoi(args[i])
		if err != nil {
			return 0, errf("%q is not a position", "%q no es una posición", args[i])
		}
		return n, nil
	}
	warn := func(line string) {
		if w := queueWarning(line); w != "" {
			log.Print(tr("queue: warning: ", "cola: aviso: ") + w)
		}
	}
	switch sub {
	case "list":
	case "add":
		it, err := q.Add(strings.Join(args[1:], " "), 0)
		if err != nil {
			return err
		}
		warn(it.line)
		log.Printf(tr("queue: added /%s (position %d)", "cola: añadida /%s (posición %d)"), it.line, len(q.Pending()))
	case "insert":
		pos, err := num(1)
		if err != nil {
			return err
		}
		it, err := q.Add(strings.Join(args[2:], " "), pos)
		if err != nil {
			return err
		}
		warn(it.line)
		log.Printf(tr("queue: inserted /%s at position %d", "cola: puesta /%s en la posición %d"), it.line, pos)
	case "remove":
		pos, err := num(1)
		if err != nil {
			return err
		}
		it, err := q.Remove(pos)
		if err != nil {
			return err
		}
		log.Printf(tr("queue: removed /%s", "cola: quitada /%s"), it.line)
	case "move":
		from, err := num(1)
		if err != nil {
			return err
		}
		to, err := num(2)
		if err != nil {
			return err
		}
		if err := q.Move(from, to); err != nil {
			return err
		}
	case "up", "down":
		pos, err := num(1)
		if err != nil {
			return err
		}
		to := pos - 1
		if sub == "down" {
			to = pos + 1
		}
		if err := q.Move(pos, to); err != nil {
			return err
		}
	case "skip":
		s := q.Skip()
		if s == "" {
			return errf("no queued command is running", "no hay ninguna orden de la cola en marcha")
		}
		b.stopAll()
		log.Printf(tr("queue: skipped /%s", "cola: salto /%s"), s)
	case "clear":
		log.Printf(tr("queue: removed %d pending", "cola: quitadas %d pendientes"), q.Clear())
	case "pause":
		q.SetPaused(true)
		log.Print(tr("queue: paused (the running command goes on; /queue resume to continue)", "cola: en pausa (la orden en marcha sigue; /queue resume para reanudar)"))
	case "resume":
		q.SetPaused(false)
		log.Print(tr("queue: resumed", "cola: reanudada"))
	default:
		return errf("usage: /queue [list|add <command>|insert <pos> <command>|remove <n>|move <from> <to>|up <n>|down <n>|skip|clear|pause|resume]",
			"uso: /queue [list|add <command>|insert <pos> <command>|remove <n>|move <from> <to>|up <n>|down <n>|skip|clear|pause|resume]")
	}
	log.Print(tr("queue: ", "cola: ") + q.Describe())
	return nil
}

// queueWarning avisa de órdenes que no terminan solas (la cola se quedaría en ellas).
func queueWarning(line string) string {
	f := strings.Fields(strings.ToLower(line))
	switch {
	case f[0] == "chop" && !hasNumber(f[1:]):
		return tr("/chop without an amount chops forever: the queue will not get past it (give an amount, or /queue skip)",
			"/chop sin cantidad tala sin parar: la cola no pasará de ahí (pon una cantidad, o /queue skip)")
	case f[0] == "guard" && len(f) < 3:
		return tr("/guard without minutes guards forever: the queue will not get past it (/guard <player> <minutes>, or /queue skip)",
			"/guard sin minutos protege sin parar: la cola no pasará de ahí (/guard <player> <minutes>, o /queue skip)")
	case f[0] == "fight" && len(f) > 1 && f[1] == "in":
		return tr("/fight in leaves a fight area set: the queue will not get past it (/queue skip)",
			"/fight in deja una zona de pelea puesta: la cola no pasará de ahí (/queue skip)")
	case f[0] == "farm":
		return tr("/farm never ends by itself: the queue will not get past it (/queue skip)",
			"/farm no termina sola: la cola no pasará de ahí (/queue skip)")
	}
	return ""
}

func hasNumber(f []string) bool {
	for _, s := range f {
		if _, err := strconv.Atoi(s); err == nil {
			return true
		}
	}
	return false
}

// stopAll para lo que esté haciendo el bot (moverse, la tarea, las peleas).
func (b *Bot) stopAll() {
	b.cancelMove()
	b.Tasks.Stop()
	b.Combat.Reset()
	b.Walker.Stop()
}
