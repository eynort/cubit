package automation

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/mj41/go-mc26-kit/bot/world"
)

// Los problemas de una tarea (un atasco, un bloque que no se deja, un camino
// que no existe) se anotan con todo lo que hace falta para arreglarlos
// después sin haber estado allí: una ficha "PROBLEMA #n" en la traza de la
// sesión (dónde estaba, qué tenía, qué bloques había alrededor, qué mobs, por
// dónde venía cavando) y una línea en el índice de problemas (Deps.Problem:
// logs/problemas.log), que dice en qué sesión y con qué número buscarla.

// problemsPerKind: fichas por clase de problema en una tarea (el mismo
// atasco repetido llenaría la traza; la línea del log sale siempre).
const problemsPerKind = 5

var problemSeq struct {
	sync.Mutex
	n int
}

// problemKind es la clase de un problema: el mensaje sin números (las
// coordenadas cambian, el problema es el mismo).
var digits = regexp.MustCompile(`-?\d+(\.\d+)?`)

func problemKind(msg string) string { return digits.ReplaceAllString(msg, "#") }

// Problem escribe en el log, como Logf, algo que salió mal, y deja su ficha.
func (j *Job) Problem(format string, args ...any) {
	j.Logf(format, args...)
	j.snapshot(fmt.Sprintf(format, args...))
}

// snapshot deja la ficha del problema msg (con el límite por clase).
func (j *Job) snapshot(msg string) {
	j.pmu.Lock() // (el vigilante la llama desde otra goroutine)
	defer j.pmu.Unlock()
	if j.problems == nil {
		j.problems = map[string]int{}
	}
	kind := problemKind(msg)
	j.problems[kind]++
	if j.problems[kind] > problemsPerKind {
		if j.problems[kind] == problemsPerKind+1 {
			j.Logf("(este problema se repite: no dejo más fichas suyas en esta tarea)")
		}
		return
	}
	problemSeq.Lock()
	problemSeq.n++
	n := problemSeq.n
	problemSeq.Unlock()
	j.problemIDs = append(j.problemIDs, n)
	j.r.mu.Lock()
	name, status := j.r.name, j.r.status
	j.r.mu.Unlock()
	if j.r.d.Trace != nil {
		j.r.d.Trace("PROBLEMA #%d [%s] %s\n%s", n, name, msg, j.describe(status))
	}
	if j.r.d.Problem != nil {
		j.r.d.Problem(n, name, msg)
	}
}

// ProblemSummary: " — 3 problemas (PROBLEMA #4-#6 en el log de la sesión)", o "".
func (j *Job) ProblemSummary() string {
	j.pmu.Lock()
	ids := append([]int(nil), j.problemIDs...)
	j.pmu.Unlock()
	if len(ids) == 0 {
		return ""
	}
	which := fmt.Sprintf("#%d", ids[0])
	if len(ids) > 1 {
		which = fmt.Sprintf("#%d-#%d", ids[0], ids[len(ids)-1])
	}
	return fmt.Sprintf(" — %d problemas (PROBLEMA %s en el log de la sesión)", len(ids), which)
}

// describe es la ficha: el estado del bot y lo que tiene alrededor.
func (j *Job) describe(status string) string {
	d := j.r.d
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, "  "+format+"\n", args...) }
	line("estado: %q", status)
	if d.Player == nil {
		return b.String()
	}
	p := j.Pos()
	f := j.Feet()
	st := d.Player.Status()
	line("dónde: %.2f %.2f %.2f (bloque %v) en %s, mirando yaw %.0f pitch %.0f", p.X, p.Y, p.Z, f, dimName(j.Dimension()), p.Yaw, p.Pitch)
	line("vida %.1f, comida %d; en la mano: %s; picos sin gastar %d (%d usos); huecos libres %d; pickFloor %d",
		st.Health, st.Food, orHand(j.Holding()), j.UsablePicks(), j.pickUses(), j.FreeSlots(), j.pickFloor)
	if d.Ctl != nil {
		cs := d.Ctl.State()
		in := cs.Input
		var keys []string
		for _, k := range []struct {
			on   bool
			name string
		}{{in.Forward, "W"}, {in.Backward, "S"}, {in.Left, "A"}, {in.Right, "D"}, {in.Jump, "saltar"}, {in.Shift, "agachado"}, {in.Sprint, "correr"}} {
			if k.on {
				keys = append(keys, k.name)
			}
		}
		if len(keys) == 0 {
			keys = []string{"ninguna"}
		}
		line("movimiento: en el suelo %v, corriendo %v, teclas %s", cs.OnGround, cs.Sprinting, strings.Join(keys, "+"))
	}
	if d.Walker != nil {
		line("walker: %s", d.Walker.Status())
	}
	if d.Fighting != nil && d.Fighting() {
		line("en una pelea")
	}
	if len(j.trail) > 0 {
		from := max(0, len(j.trail)-6)
		line("túnel cavado: %d celdas, las últimas %v", len(j.trail), j.trail[from:])
	}
	// los bloques de alrededor: 5x5 de 1 abajo a 2 arriba (x a la derecha, z hacia abajo)
	if d.World != nil {
		short := map[string]string{}
		var legend []string
		next := 'a'
		var fluids []string
		for y := f.Y + 2; y >= f.Y-1; y-- {
			line("y=%d%s", y, map[int]string{f.Y: " (los pies)", f.Y + 1: " (la cabeza)", f.Y - 1: " (el suelo)"}[y])
			for z := f.Z - 2; z <= f.Z+2; z++ {
				var row strings.Builder
				for x := f.X - 2; x <= f.X+2; x++ {
					q := world.BlockPos{X: x, Y: y, Z: z}
					n := j.Block(q)
					c := "."
					switch {
					case n == "":
						c = "?"
					case x == f.X && z == f.Z && (y == f.Y || y == f.Y+1):
						c = "@"
					case n == "air" || n == "cave_air" || n == "void_air":
					default:
						if _, ok := short[n]; !ok {
							short[n] = string(next)
							legend = append(legend, fmt.Sprintf("%c=%s", next, n))
							next++
						}
						c = short[n]
						if n == "water" || n == "lava" {
							fluids = append(fluids, fmt.Sprintf("%s en %v", n, q))
						}
					}
					row.WriteString(c)
				}
				line("  %s", row.String())
			}
		}
		if len(legend) > 0 {
			line("leyenda: %s (. aire, @ el bot, ? sin cargar)", strings.Join(legend, " "))
		}
		if len(fluids) > 0 {
			line("agua/lava al lado: %s", strings.Join(fluids, ", "))
		}
	}
	// lo que hay cerca (mobs, jugadores, objetos)
	if d.Entities != nil {
		type near struct {
			what string
			dist float64
		}
		var ns []near
		for _, e := range d.Entities.Nearby(p.X, p.Y, p.Z, 12) {
			if e.Type == "minecraft:experience_orb" {
				continue
			}
			ns = append(ns, near{strings.TrimPrefix(e.Type, "minecraft:"), math.Sqrt((e.X-p.X)*(e.X-p.X) + (e.Y-p.Y)*(e.Y-p.Y) + (e.Z-p.Z)*(e.Z-p.Z))})
		}
		sort.Slice(ns, func(a, b int) bool { return ns[a].dist < ns[b].dist })
		var out []string
		for i, n := range ns {
			if i == 10 {
				out = append(out, fmt.Sprintf("y %d más", len(ns)-10))
				break
			}
			out = append(out, fmt.Sprintf("%s a %.1f", n.what, n.dist))
		}
		if len(out) > 0 {
			line("cerca (12 bloques): %s", strings.Join(out, ", "))
		}
	}
	return b.String()
}
