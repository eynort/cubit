package automation

// Condiciones de parada: cualquier tarea puede llevar, opcionalmente, una o
// varias condiciones que la terminan en cuanto una se cumple ("/chop abeto
// until got logs 64 or time 20m"). Es un patrón común a todos los módulos:
//
//   - Until(task, conds) envuelve cualquier tarea. El progreso (lo recogido,
//     los contadores, el reloj) vive en el envoltorio: sigue igual si la tarea
//     se vuelve a empezar (atascada) o se retoma tras una muerte.
//   - Sin hacer nada en el módulo, la tarea se para en el siguiente Wait tras
//     cumplirse (como un /stop, pero acaba "bien": la cola sigue).
//   - Un módulo que quiera acabar con orden (terminar el árbol, guardar lo
//     que lleva) mira j.StopReached() en su bucle: con eso, Wait ya no la
//     corta, y es la tarea la que sale cuando le viene bien.
//   - Lo que un módulo cuenta y no se ve en el inventario (árboles talados,
//     ovejas esquiladas) lo publica con j.Tally("nombre", n): "count nombre N".
//
// Condiciones (ParseUntil):
//
//	got <objeto|grupo> <n>   lo recogido del suelo desde que empezó
//	have <objeto|grupo> <n>  lo que lleva en el inventario ahora
//	count <contador> <n>     un contador de la tarea (Tally)
//	time <duración>          el tiempo desde que empezó (10m, 1h30m)

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/protocol/types"
)

// ErrStopCondition: la tarea se paró porque se cumplió su condición (no es
// un fallo: el Runner la da por terminada bien).
var ErrStopCondition = errors.New("condición de parada cumplida")

// Cond es una condición de parada.
type Cond interface {
	// Met dice si se cumple.
	Met(p *Progress, j *Job) bool
	// Progress es cómo va ("logs 12/64"), para el estado.
	Progress(p *Progress, j *Job) string
	// String es la condición tal como se escribe ("got logs 64").
	String() string
}

// Progress es lo que lleva hecho una tarea con condiciones de parada.
type Progress struct {
	mu     sync.Mutex
	start  time.Time
	got    map[string]int // recogido del suelo, por objeto
	counts map[string]int // contadores de la tarea (Tally)
}

func newProgress() *Progress {
	return &Progress{start: time.Now(), got: map[string]int{}, counts: map[string]int{}}
}

// Got es cuánto se recogió de lo que cumple match.
func (p *Progress) Got(match func(string) bool) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for it, c := range p.got {
		if match(it) {
			n += c
		}
	}
	return n
}

// Count es un contador de la tarea.
func (p *Progress) Count(name string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.counts[name]
}

// Elapsed es el tiempo desde que empezó.
func (p *Progress) Elapsed() time.Duration { return time.Since(p.start) }

func (p *Progress) add(m map[string]int, k string, n int) {
	p.mu.Lock()
	m[k] += n
	p.mu.Unlock()
}

// stopper son las condiciones de una tarea y su progreso.
type stopper struct {
	conds    []Cond
	progress *Progress
	met      Cond // la que se cumplió (nil: ninguna aún)
	polled   bool // la tarea mira StopReached: acaba ella, Wait no la corta
	ticks    int  // ticks esperados en Wait desde que se miraron
}

// stopCheckEvery: en Wait, las condiciones se miran cada tantos ticks (leer
// el inventario en cada tick sobra).
const stopCheckEvery = 10

// check mira las condiciones.
func (s *stopper) check(j *Job) Cond {
	if s.met != nil {
		return s.met
	}
	for _, c := range s.conds {
		if c.Met(s.progress, j) {
			s.met = c
			j.Logf("condición de parada cumplida: %s (%s)", c, c.Progress(s.progress, j))
			break
		}
	}
	return s.met
}

// Until envuelve una tarea con condiciones de parada (sin condiciones: la
// misma tarea). Llamar a la tarea otra vez (reinicio, retomarla) sigue con el
// mismo progreso.
func Until(task func(j *Job) error, conds []Cond) func(j *Job) error {
	if len(conds) == 0 {
		return task
	}
	p := newProgress()
	return func(j *Job) error {
		s := &stopper{conds: conds, progress: p}
		j.stop = s
		j.r.progress.Store(p)
		defer j.r.progress.CompareAndSwap(p, nil)
		if s.check(j) != nil { // ya cumplida antes de empezar
			return nil
		}
		err := task(j)
		if errors.Is(err, ErrStopCondition) || (err == nil && s.met != nil) {
			return nil
		}
		return err
	}
}

// StopReached dice si se cumplió una condición de parada de la tarea (false
// si no tiene). La tarea que lo mira sale ella cuando le venga bien (y Wait
// ya no la corta): se llama en el bucle principal, entre una vuelta y otra.
func (j *Job) StopReached() bool {
	if j.stop == nil {
		return false
	}
	j.stop.polled = true
	return j.stop.check(j) != nil
}

// Tally suma n al contador name de la tarea (para "count name N"). Sin
// condiciones de parada no hace nada.
func (j *Job) Tally(name string, n int) {
	if j.stop != nil && n != 0 {
		j.stop.progress.add(j.stop.progress.counts, name, n)
	}
}

// StopProgress es cómo van las condiciones de parada ("" sin ellas).
func (j *Job) StopProgress() string {
	if j.stop == nil {
		return ""
	}
	var out []string
	for _, c := range j.stop.conds {
		out = append(out, c.Progress(j.stop.progress, j))
	}
	return strings.Join(out, " o ")
}

// stopCheck: en Wait (tras n ticks), la condición cumplida para una tarea
// que no la mira ella.
func (j *Job) stopCheck(n int) error {
	if j.stop == nil || j.stop.polled {
		return nil
	}
	if j.stop.ticks += n; j.stop.ticks < stopCheckEvery {
		return nil
	}
	j.stop.ticks = 0
	if j.stop.check(j) == nil {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrStopCondition, j.stop.met)
}

// watchPickups cuenta lo que el bot recoge del suelo para la tarea en curso
// (got): se llama una vez, al crear el Runner.
func (r *Runner) watchPickups() {
	if r.d.Entities == nil || r.d.Player == nil {
		return
	}
	r.d.Entities.Watch(entities.Events{PickedUp: func(item, collector int32, amount int) {
		p := r.progress.Load()
		if p == nil || collector != int32(r.d.Player.Login.PlayerID) {
			return
		}
		e, ok := r.d.Entities.Get(item)
		if !ok {
			return
		}
		v, ok := e.Data[entitydata.ItemEntityItem]
		if !ok {
			return
		}
		st, ok := v.Value.(*types.ItemStack)
		if !ok {
			return
		}
		if amount <= 0 {
			amount = int(st.Count)
		}
		p.add(p.got, strings.TrimPrefix(itemName(int32(st.Item)), "minecraft:"), amount)
	}})
}

// ---- las condiciones ----

// itemGroups son nombres para grupos de objetos ("got logs 64"). Para añadir
// uno, aquí.
var itemGroups = map[string]func(string) bool{
	"logs":   isLog,
	"planks": isPlanks,
	"wood":   func(s string) bool { return isLog(s) || isPlanks(s) },
	"wool":   func(s string) bool { return strings.HasSuffix(s, "_wool") },
	"beds":   func(s string) bool { return strings.HasSuffix(s, "_bed") },
	"food":   func(s string) bool { _, ok := goodFood(s); return ok },
}

// ItemGroups son los nombres de los grupos, ordenados (para la ayuda).
func ItemGroups() []string {
	var out []string
	for g := range itemGroups {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

// itemMatcher: un grupo, un patrón ("*_log") o un objeto ("oak_log").
func itemMatcher(s string) (func(string) bool, error) {
	s = strings.TrimPrefix(strings.ToLower(s), "minecraft:")
	if g, ok := itemGroups[s]; ok {
		return g, nil
	}
	if strings.ContainsAny(s, "*?[") {
		if _, err := path.Match(s, ""); err != nil {
			return nil, fmt.Errorf("patrón %q: %v", s, err)
		}
		return func(it string) bool { ok, _ := path.Match(s, it); return ok }, nil
	}
	if s == "" {
		return nil, errors.New("falta el objeto")
	}
	return func(it string) bool { return it == s }, nil
}

type gotCond struct {
	what  string
	match func(string) bool
	n     int
}

func (c gotCond) Met(p *Progress, _ *Job) bool { return p.Got(c.match) >= c.n }
func (c gotCond) Progress(p *Progress, _ *Job) string {
	return fmt.Sprintf("recogido %s %d/%d", c.what, p.Got(c.match), c.n)
}
func (c gotCond) String() string { return fmt.Sprintf("got %s %d", c.what, c.n) }

type haveCond struct {
	what  string
	match func(string) bool
	n     int
}

func (c haveCond) Met(_ *Progress, j *Job) bool { return j.CountWhere(c.match) >= c.n }
func (c haveCond) Progress(_ *Progress, j *Job) string {
	return fmt.Sprintf("lleva %s %d/%d", c.what, j.CountWhere(c.match), c.n)
}
func (c haveCond) String() string { return fmt.Sprintf("have %s %d", c.what, c.n) }

type countCond struct {
	name string
	n    int
}

func (c countCond) Met(p *Progress, _ *Job) bool { return p.Count(c.name) >= c.n }
func (c countCond) Progress(p *Progress, _ *Job) string {
	return fmt.Sprintf("%s %d/%d", c.name, p.Count(c.name), c.n)
}
func (c countCond) String() string { return fmt.Sprintf("count %s %d", c.name, c.n) }

type timeCond struct{ d time.Duration }

func (c timeCond) Met(p *Progress, _ *Job) bool { return p.Elapsed() >= c.d }
func (c timeCond) Progress(p *Progress, _ *Job) string {
	return fmt.Sprintf("tiempo %s/%s", p.Elapsed().Round(time.Second), c.d)
}
func (c timeCond) String() string { return "time " + c.d.String() }

// UntilUsage es la forma de las condiciones (para la ayuda y la IA).
const UntilUsage = "until <cond> [or <cond>…] — cond: got <item|group> <n> | have <item|group> <n> | count <counter> <n> | time <10m>"

// SplitUntil separa "chop abeto until got logs 64" en los argumentos de la
// orden y lo de después de "until" (nil si no hay).
func SplitUntil(args []string) (cmd, until []string) {
	for i, a := range args {
		if strings.EqualFold(a, "until") {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

// ParseUntil lee las condiciones de después de "until" ("got logs 64 or time
// 20m"): se para con la primera que se cumpla.
func ParseUntil(f []string) ([]Cond, error) {
	if len(f) == 0 {
		return nil, errors.New("until sin condición (" + UntilUsage + ")")
	}
	var out []Cond
	for len(f) > 0 {
		var c Cond
		var used int
		switch kind := strings.ToLower(f[0]); kind {
		case "got", "have":
			if len(f) < 3 {
				return nil, fmt.Errorf("%s <objeto|grupo> <n>", kind)
			}
			m, err := itemMatcher(f[1])
			if err != nil {
				return nil, err
			}
			n, err := positive(f[2])
			if err != nil {
				return nil, err
			}
			what := strings.ToLower(f[1])
			if kind == "got" {
				c = gotCond{what, m, n}
			} else {
				c = haveCond{what, m, n}
			}
			used = 3
		case "count":
			if len(f) < 3 {
				return nil, errors.New("count <contador> <n>")
			}
			n, err := positive(f[2])
			if err != nil {
				return nil, err
			}
			c, used = countCond{strings.ToLower(f[1]), n}, 3
		case "time":
			if len(f) < 2 {
				return nil, errors.New("time <duración> (p. ej. 10m, 1h30m)")
			}
			d, err := time.ParseDuration(strings.ToLower(f[1]))
			if err != nil || d <= 0 {
				return nil, fmt.Errorf("duración %q: usa p. ej. 90s, 10m, 1h30m", f[1])
			}
			c, used = timeCond{d}, 2
		default:
			return nil, fmt.Errorf("condición %q desconocida (%s)", f[0], UntilUsage)
		}
		out = append(out, c)
		f = f[used:]
		if len(f) > 0 {
			if !strings.EqualFold(f[0], "or") {
				return nil, fmt.Errorf("entre condiciones va \"or\", no %q", f[0])
			}
			f = f[1:]
			if len(f) == 0 {
				return nil, errors.New("\"or\" sin condición detrás")
			}
		}
	}
	return out, nil
}

func positive(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%q no es un número mayor que 0", s)
	}
	return n, nil
}
