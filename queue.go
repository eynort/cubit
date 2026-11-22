package main

// La cola de tareas: "/queue <orden>" apunta una orden para cuando acabe la
// actual, y una línea con " ; " ("/mine iron ~ ~ ~ ; /chop oak 10 64 10")
// empieza la primera y deja el resto en la cola. Al acabar bien una tarea,
// empieza la siguiente sola; si una falla o se rinde, el bot lo cuenta en el
// chat y deja la cola en pausa (/queue resume sigue). /stop también la pausa
// (sin vaciarla).
//
// La cola vive en el proceso, no en la conexión: sobrevive a las
// reconexiones (la tarea interrumpida se retoma en resumeAfterReconnect y,
// cuando acaba, sigue la cola).

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"cubit/internal/i18n"
)

type queued struct {
	line string // la orden tal cual ("/chop oak ~ ~ ~")
	by   string // quién la mandó por /msg ("" = consola o panel): a quién avisar
}

type taskQueue struct {
	mu      sync.Mutex
	items   []queued
	paused  bool
	active  bool // la cola lanzó un goto y está en marcha (no tiene Runner que avise)
	started bool // hay (o hubo) una cadena: al vaciarse se avisa de que se acabó
}

// workQueue: la cola de este bot (un proceso, un bot).
var workQueue = &taskQueue{}

const maxQueue = 50

func (q *taskQueue) snapshot() (lines []string, paused bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	lines = make([]string, len(q.items))
	for i, it := range q.items {
		lines[i] = it.line
	}
	return lines, q.paused
}

func (q *taskQueue) add(line, by string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) >= maxQueue {
		return i18n.Errorf("the queue is full (%d)", "la cola está llena (%d)", maxQueue)
	}
	q.items = append(q.items, queued{line, by})
	q.started = true
	return nil
}

func (q *taskQueue) pop() (queued, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.paused || len(q.items) == 0 {
		return queued{}, false
	}
	it := q.items[0]
	q.items = q.items[1:]
	return it, true
}

func (q *taskQueue) setPaused(p bool) {
	q.mu.Lock()
	q.paused = p
	if p {
		q.active = false
	}
	q.mu.Unlock()
}

// reset: una cadena nueva reemplaza lo que hubiera.
func (q *taskQueue) reset() {
	q.mu.Lock()
	q.items, q.paused, q.active, q.started = nil, false, false, false
	q.mu.Unlock()
}

func (q *taskQueue) clear() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := len(q.items)
	q.items, q.active, q.started = nil, false, false
	return n
}

func (q *taskQueue) remove(n int) (queued, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if n < 1 || n > len(q.items) {
		return queued{}, false
	}
	it := q.items[n-1]
	q.items = append(q.items[:n-1:n-1], q.items[n:]...)
	return it, true
}

// ---- cadenas: "a ; b ; c" ----

// splitChain parte una línea por " ; " (con espacios: un ";" suelto es texto).
// Las que son texto (sin "/", /say, /cmd) no se parten.
func splitChain(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "/") || !strings.Contains(line, ";") {
		return []string{line}
	}
	switch strings.ToLower(strings.Fields(line)[0]) {
	case "/say", "/cmd":
		return []string{line}
	}
	var out []string
	for _, p := range strings.Split(" "+line+" ", " ; ") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{line}
	}
	return out
}

type cmdKind int

const (
	kindInstant cmdKind = iota // acaba al momento (status, say, inventory…)
	kindTask                   // una tarea que acaba sola: se puede encolar
	kindOpenEnd                // sigue hasta /stop (follow, guard, fight…) o va a un jugador: no se encola
)

// kindOf clasifica una línea ("/chop …").
func kindOf(line string) cmdKind {
	line = strings.TrimSpace(line)
	f := strings.Fields(strings.TrimPrefix(line, "/"))
	if len(f) == 0 || !strings.HasPrefix(line, "/") {
		return kindInstant
	}
	switch strings.ToLower(f[0]) {
	case "chop", "mine":
		return kindTask
	case "excavate":
		if len(f) > 1 && strings.EqualFold(f[1], "chests") {
			return kindInstant
		}
		return kindTask
	case "goto":
		if len(f) == 4 {
			return kindTask
		}
		return kindOpenEnd
	case "follow", "guard", "fight":
		return kindOpenEnd
	}
	return kindInstant
}

// runChain ejecuta "a ; b ; c": la primera tarea empieza ya, las demás tareas
// van a la cola (que se reemplaza) y lo instantáneo se hace en el momento.
func (b *Bot) runChain(segs []string) error {
	by := currentOrderFrom()
	workQueue.reset()
	started := false
	for _, s := range segs {
		switch k := kindOf(s); {
		case k == kindInstant:
			if err := b.run(s); err != nil {
				return err
			}
		case !started:
			goTo := k == kindTask && strings.EqualFold(strings.Fields(s)[0], "/goto")
			if goTo { // un goto no tiene Runner que avise: moveEnded sigue la cola solo si está activa
				workQueue.mu.Lock()
				workQueue.active = true
				workQueue.mu.Unlock()
			}
			if err := b.run(s); err != nil {
				if goTo {
					workQueue.mu.Lock()
					workQueue.active = false
					workQueue.mu.Unlock()
				}
				return err
			}
			started = true
		case k == kindTask:
			if err := workQueue.add(s, by); err != nil {
				return err
			}
		default:
			return errf("%s never ends by itself: it can't wait in the queue", "%s no acaba solo: no puede esperar en la cola", strings.Fields(s)[0])
		}
	}
	b.notifyQueue()
	return nil
}

func currentOrderFrom() string {
	orderFrom.Lock()
	defer orderFrom.Unlock()
	return orderFrom.who
}

// ---- avance ----

// queueDone: la tarea acabó bien: sigue la cola.
func (b *Bot) queueDone() {
	it, ok := workQueue.pop()
	if !ok {
		workQueue.mu.Lock()
		was := workQueue.started && !workQueue.paused
		workQueue.started, workQueue.active = false, false
		workQueue.mu.Unlock()
		if was {
			b.reportWork("%s", tr("that was the last thing on my list, all done!", "esa era lo último de mi lista, ¡todo listo!"))
		}
		b.notifyQueue()
		return
	}
	b.reportWork(tr("done with that one! next up: %s", "¡listo eso! ahora toca: %s"), it.line)
	b.launch(it)
}

// launch empieza una orden de la cola.
func (b *Bot) launch(it queued) {
	workQueue.mu.Lock()
	workQueue.active = true
	workQueue.mu.Unlock()
	log.Printf("cola: empiezo %s", it.line)
	err := asOrderFrom(it.by, func() error { return b.run(it.line) })
	if err != nil {
		b.queueFail(tr("I couldn't start %s: %s", "no he podido empezar %s: %s"), it.line, errText(err))
		return
	}
	b.notifyQueue()
}

// queuePause deja la cola en pausa (sin vaciarla).
func (b *Bot) queuePause() {
	workQueue.setPaused(true)
	b.notifyQueue()
}

// queueFail: algo falló: se cuenta en el chat y la cola queda en pausa.
func (b *Bot) queueFail(format string, args ...any) {
	b.reportWork(format, args...)
	b.pauseWithNotice()
}

// pauseWithNotice pausa la cola y, si había algo esperando, lo dice.
func (b *Bot) pauseWithNotice() {
	b.queuePause()
	if lines, _ := workQueue.snapshot(); len(lines) > 0 {
		b.reportWork(tr("I'm holding the rest of the list (%d left), say /queue resume when you want me to go on",
			"dejo en pausa el resto de la lista (%d más), dime /queue resume cuando quieras que siga"), len(lines))
	}
}

// moveEnded: un goto de la cola acabó (ok: llegó).
func (b *Bot) moveEnded(ok bool) {
	workQueue.mu.Lock()
	act := workQueue.active
	workQueue.active = false
	workQueue.mu.Unlock()
	if !act {
		return
	}
	if ok {
		b.queueDone()
		return
	}
	b.queueFail("%s", tr("I couldn't get there", "no he podido llegar"))
}

// notifyQueue: la cola cambió (el panel y los eventos de la API).
func (b *Bot) notifyQueue() {
	lines, paused := workQueue.snapshot()
	emitEvent(apiEvent{Type: "queue", Text: fmt.Sprintf("%d", len(lines)), Data: map[string]any{"queue": lines, "paused": paused}})
}

// ---- /queue ----

func cmdQueue(b *Bot, args []string) error {
	usage := errf("usage: /queue <command> | list | clear | remove <n> | resume | pause", "uso: /queue <orden> | list | clear | remove <n> | resume | pause")
	if len(args) == 0 {
		return usage
	}
	switch strings.ToLower(args[0]) {
	case "list":
		b.logQueue()
	case "clear":
		n := workQueue.clear()
		log.Printf("cola vaciada (%d)", n)
		b.notifyQueue()
	case "remove":
		if len(args) != 2 {
			return usage
		}
		n, err := strconv.Atoi(args[1])
		if err != nil {
			return usage
		}
		it, ok := workQueue.remove(n)
		if !ok {
			return errf("there is no #%d in the queue", "no hay ningún nº %d en la cola", n)
		}
		log.Printf("cola: quito %s", it.line)
		b.notifyQueue()
	case "pause":
		b.queuePause()
		log.Print("cola en pausa")
	case "resume":
		workQueue.setPaused(false)
		log.Print("cola: sigo")
		b.kickQueue()
		b.notifyQueue()
	default:
		line := strings.Join(args, " ")
		if !strings.HasPrefix(line, "/") {
			line = "/" + line
		}
		segs := splitChain(line)
		for _, s := range segs { // todo o nada
			if kindOf(s) != kindTask {
				return errf("only tasks that finish by themselves can be queued (chop, mine, excavate, goto x y z): %s", "solo se encolan tareas que acaban solas (chop, mine, excavate, goto x y z): %s", s)
			}
		}
		for _, s := range segs {
			if err := workQueue.add(s, currentOrderFrom()); err != nil {
				return err
			}
		}
		b.kickQueue()
		b.notifyQueue()
		lines, _ := workQueue.snapshot()
		log.Printf("cola: apuntada (%d en espera)", len(lines))
	}
	return nil
}

// kickQueue: si no hay nada en marcha y la cola no está en pausa, empieza la siguiente.
func (b *Bot) kickQueue() {
	if b.Tasks == nil || b.Tasks.Status() != "" || b.workResuming() || currentMove() != "" {
		return
	}
	if it, ok := workQueue.pop(); ok {
		b.launch(it)
	}
}

func (b *Bot) logQueue() {
	lines, paused := workQueue.snapshot()
	if len(lines) == 0 {
		log.Print(tr("the queue is empty", "la cola está vacía"))
		return
	}
	out := tr("queue:", "cola:")
	if paused {
		out = tr("queue (paused, /queue resume):", "cola (en pausa, /queue resume):")
	}
	for i, l := range lines {
		out += fmt.Sprintf("\n  %d. %s", i+1, l)
	}
	log.Print(out)
}

// queueSummary: una línea para /status ("" sin cola).
func queueSummary() string {
	lines, paused := workQueue.snapshot()
	if len(lines) == 0 {
		return ""
	}
	s := fmt.Sprintf("%d: %s", len(lines), strings.Join(lines, " ; "))
	if paused {
		s += tr(" (paused)", " (en pausa)")
	}
	return s
}
