package automation

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/level/block"
	"github.com/mj41/go-mc26/protocol/types"

	"minecraft-bot/pathing"
)

// Minar: llegar a un bloque bajo tierra cavando un túnel en escalera (como
// un jugador: dos de alto, escalón a escalón), sin abrir nunca un hueco junto
// a lava o agua, y sacar los bloques de un tipo hasta juntar los que hacen falta.

// findBlock es el bloque más cercano cuyo nombre cumple ok: a menos de r en
// horizontal, entre down bloques por debajo de los pies y up por encima.
func (j *Job) findBlock(r, down, up int, ok func(string) bool, skip map[world.BlockPos]bool) (world.BlockPos, bool) {
	f := j.Feet()
	type cand struct {
		p world.BlockPos
		d float64
	}
	var cs []cand
	for dy := -down; dy <= up; dy++ {
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				p := world.BlockPos{X: f.X + dx, Y: f.Y + dy, Z: f.Z + dz}
				if skip[p] {
					continue
				}
				if n := j.Block(p); n != "" && n != "air" && ok(n) {
					cs = append(cs, cand{p, math.Sqrt(float64(dx*dx+dz*dz)) + 1.2*math.Abs(float64(dy))})
				}
			}
		}
	}
	if len(cs) == 0 {
		return world.BlockPos{}, false
	}
	sort.Slice(cs, func(a, b int) bool { return cs[a].d < cs[b].d })
	return cs[0].p, true
}

// liquid: agua o lava (o un bloque inundado).
func (j *Job) liquid(p world.BlockPos) bool {
	st, ok := j.r.d.World.BlockAt(p)
	if !ok {
		return true // sin cargar: como si lo fuera
	}
	return liquidState(world.StateString(st))
}

// liquidState: el estado ("minecraft:oak_leaves[distance=1,waterlogged=false]")
// es agua o lava, o lleva agua dentro. Por el nombre del bloque y
// waterlogged=true: buscar "water" en todo el texto daba por agua cualquier
// bloque que puede llevarla (hojas, losas, escaleras… con waterlogged=false),
// y junto a ellos no se cavaba (en un árbol, nada).
func liquidState(s string) bool {
	name := strings.TrimPrefix(s, "minecraft:")
	if i := strings.IndexByte(name, '['); i >= 0 {
		name = name[:i]
	}
	switch name {
	case "water", "lava", "bubble_column", "kelp", "kelp_plant", "seagrass", "tall_seagrass":
		return true
	}
	return strings.Contains(s, "waterlogged=true")
}

// safeToOpen: cavar p no deja entrar lava ni agua (ninguna al lado ni encima).
func (j *Job) safeToOpen(p world.BlockPos) bool {
	for _, d := range [][3]int{{0, 0, 0}, {1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, 1, 0}} {
		if j.liquid(world.BlockPos{X: p.X + d[0], Y: p.Y + d[1], Z: p.Z + d[2]}) {
			return false
		}
	}
	return !j.fallOpensLava(p)
}

// fallOpensLava: abrir p deja caer la grava (o arena) de encima, y los huecos
// que deja al caer tocan lava (o agua): la lava entra por arriba y baja por la
// columna hasta el bot. Así murió en el Nether: picó grava, la de encima cayó
// y la lava que tenía al lado bajó por el túnel.
func (j *Job) fallOpensLava(p world.BlockPos) bool {
	q := world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}
	for k := 0; k < 32 && j.fallsAbove(world.BlockPos{X: q.X, Y: q.Y - 1, Z: q.Z}); k++ {
		// q cae: queda vacío, y lo que lo toca entra
		for _, d := range [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, 1, 0}} {
			if j.liquid(add(q, d)) {
				return true
			}
		}
		q.Y++
	}
	return false
}

// errLavaNear: Wait tras apartarse de lava que no pudo tapar; la tarea busca otro sitio.
var errLavaNear = errors.New("lava cerca que no puedo tapar")

// lavaNear: hay lava suelta a menos de r bloques de los pies.
func (j *Job) lavaNear(r int) bool {
	f := j.Feet()
	for dx := -r; dx <= r; dx++ {
		for dy := -1; dy <= r; dy++ {
			for dz := -r; dz <= r; dz++ {
				if j.Block(world.BlockPos{X: f.X + dx, Y: f.Y + dy, Z: f.Z + dz}) == "lava" {
					return true
				}
			}
		}
	}
	return false
}

// plugExposed tapa la lava (y el agua) que hay cerca y puede correr hacia el
// bot: la que toca aire a menos de r bloques y está al alcance. Tras abrir un
// bloque, o tras apartarse de lava que apareció, en vez de seguir como si
// nada (la tarea volvía a la misma celda y acabó dentro). Dice cuántas tapó.
func (j *Job) plugExposed(r int) int {
	f := j.Feet()
	type cell struct {
		p world.BlockPos
		d int
	}
	var cs []cell
	for dx := -r; dx <= r; dx++ {
		for dy := -r; dy <= r+1; dy++ {
			for dz := -r; dz <= r; dz++ {
				c := world.BlockPos{X: f.X + dx, Y: f.Y + dy, Z: f.Z + dz}
				if j.Block(c) != "lava" || !j.Reaches(c) {
					continue
				}
				open := false
				// corre hacia los lados y hacia abajo: aire ahí es por donde viene
				// (un lago con aire encima no sube; ese se deja)
				for _, d := range [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, -1, 0}} {
					if n := add(c, d); !j.solid(n) && !j.liquid(n) {
						open = true
					}
				}
				if open {
					cs = append(cs, cell{c, dx*dx + dy*dy + dz*dz})
				}
			}
		}
	}
	sort.Slice(cs, func(a, b int) bool { return cs[a].d < cs[b].d })
	n := 0
	for _, c := range cs {
		if n >= 16 {
			break
		}
		fill := j.filler()
		if fill == "" {
			j.Logf("lava cerca y nada con qué taparla")
			break
		}
		if err := j.Place(fill, c.p); err != nil {
			continue
		}
		n++
	}
	if n > 0 {
		j.plugged += n
		j.Logf("tapo %d de lava que había cerca", n)
	}
	return n
}

// faces son las seis caras de un bloque.
var faces = [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, 1, 0}, {0, -1, 0}}

func add(p world.BlockPos, d [3]int) world.BlockPos {
	return world.BlockPos{X: p.X + d[0], Y: p.Y + d[1], Z: p.Z + d[2]}
}

// fluidBlock: agua o lava de verdad (fuente o corriente), donde se puede
// poner un bloque; no un bloque inundado (ese no se tapa).
func (j *Job) fluidBlock(p world.BlockPos) bool {
	n := j.Block(p)
	return n == "lava" || n == "water" || n == "bubble_column"
}

// fillers: lo que sirve para tapar lava (macizo, que no cae ni arde).
var fillers = []string{"netherrack", "cobblestone", "blackstone", "cobbled_deepslate", "basalt", "stone",
	"andesite", "diorite", "granite", "tuff", "dirt", "end_stone"}

// filler es el bloque con el que tapar ("" ninguno).
func (j *Job) filler() string {
	items := j.Items()
	for _, f := range fillers {
		if items[f] > 0 {
			return f
		}
	}
	return ""
}

// lavaHit es una celda de agua o lava que entraría al abrir un bloque, y el
// bloque contra el que ponerle otro encima (uno entero que la toca).
type lavaHit struct{ c, against world.BlockPos }

// lavaCells son las celdas con agua o lava que hay que tapar antes de abrir p:
// p misma, las de los lados y la de encima (la de debajo no sube); y si
// encima de p hay grava, también las que tocan la columna que caería (al
// caer deja huecos por donde entra: así murió en el Nether).
func (j *Job) lavaCells(p world.BlockPos) []lavaHit {
	var out []lavaHit
	for _, d := range [][3]int{{0, 0, 0}, {1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, 1, 0}} {
		if c := add(p, d); j.liquid(c) {
			out = append(out, lavaHit{c, p})
		}
	}
	q := world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}
	for k := 0; k < 32 && j.fallsAbove(world.BlockPos{X: q.X, Y: q.Y - 1, Z: q.Z}); k++ {
		for _, d := range [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, 1, 0}} {
			if c := add(q, d); j.liquid(c) {
				out = append(out, lavaHit{c, q})
			}
		}
		q.Y++
	}
	return out
}

// pluggable: lo que entraría al abrir p se puede tapar desde aquí (lleva con
// qué, todo está al alcance y es agua o lava suelta, no un bloque inundado).
func (j *Job) pluggable(p world.BlockPos) bool { return j.pluggableReach(p, true) }

func (j *Job) pluggableReach(p world.BlockPos, reach bool) bool {
	if j.filler() == "" {
		return false
	}
	for _, h := range j.lavaCells(p) {
		if !j.fluidBlock(h.c) || reach && !j.Reaches(h.c) {
			return false
		}
	}
	return true
}

// plug tapa la lava (y el agua) que entraría al abrir p, ANTES de picarlo: un
// bloque en cada celda, puesto contra el bloque entero que la toca (p, o la
// grava que caería), o contra lo que haya al lado si p es la lava. Así la
// lava no llega a correr nunca: picar primero y tapar después es una carrera
// contra ella. Dice si quedó todo tapado.
func (j *Job) plug(p world.BlockPos) bool {
	cells := j.lavaCells(p)
	if len(cells) == 0 {
		return true
	}
	// p primero (si es lava): luego sirve de apoyo para las demás
	for k, h := range cells {
		if h.c == p && k > 0 {
			cells[0], cells[k] = cells[k], cells[0]
		}
	}
	for _, h := range cells {
		if !j.liquid(h.c) {
			continue // (tapada ya por otra)
		}
		f := j.filler()
		if f == "" {
			j.Problem("lava junto a %v y nada con qué taparla", p)
			return false
		}
		var err error
		if h.c != h.against && j.solid(h.against) {
			err = j.PlaceOn(f, h.c, h.against)
		} else {
			err = j.Place(f, h.c)
		}
		if err != nil {
			j.Problem("tapar %s en %v: %v", j.Block(h.c), h.c, err)
			return false
		}
	}
	if left := j.lavaCells(p); len(left) > 0 {
		return false
	}
	j.plugged += len(cells)
	j.Status("tapé %d de lava junto a %v", len(cells), p)
	return true
}

// makeSafe deja p listo para abrir: si entraría lava, la tapa antes
// (acercándose si la lava queda fuera del alcance).
func (j *Job) makeSafe(p world.BlockPos) bool {
	if j.safeToOpen(p) {
		return true
	}
	if !j.pluggable(p) && j.pluggableReach(p, false) {
		if err := j.closeTo(p, 2.6); err != nil {
			j.Problem("acercarme a la lava de %v: %v", p, err)
		}
	}
	return j.pluggable(p) && j.plug(p) && j.safeToOpen(p)
}

// fallsAbove: justo encima de p hay grava (o arena…): abrir p la suelta.
func (j *Job) fallsAbove(p world.BlockPos) bool {
	st, ok := j.r.d.World.BlockAt(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z})
	return ok && pathing.Falling(world.StateString(st))
}

// dropsOnMe: abrir p (en la columna del bot, por encima de los pies) suelta
// grava que cae sobre él y lo entierra.
func (j *Job) dropsOnMe(p world.BlockPos) bool {
	f := j.Feet()
	return p.X == f.X && p.Z == f.Z && p.Y > f.Y && j.fallsAbove(p)
}

// unbury: si le cayó grava (o arena) en la cabeza o los pies, la cava ya:
// dentro de un bloque se asfixia (1 de vida cada medio segundo).
func (j *Job) unbury() {
	if j.unburying {
		return // (Dig espera con Wait, que vuelve a llamar aquí)
	}
	j.unburying = true
	defer func() { j.unburying = false }()
	for tries := 0; tries < 12; tries++ {
		f := j.Feet()
		var in *world.BlockPos
		for _, c := range []world.BlockPos{{X: f.X, Y: f.Y + 1, Z: f.Z}, f} {
			if st, ok := j.r.d.World.BlockAt(c); ok && pathing.Falling(world.StateString(st)) {
				in = &c
				break
			}
		}
		if in == nil {
			return
		}
		if tries == 0 {
			j.Logf("me cayó %s encima (en %v): lo cavo para no asfixiarme", j.Block(*in), *in)
		}
		if err := j.Dig(*in); err != nil {
			j.Problem("desenterrarme: %v", err)
			return
		}
		_ = j.r.d.Ctl.WaitTicks(j.ctx, 3) // (la siguiente de la columna tarda en caer)
	}
}

// solid: hay algo que estorba al cuerpo en p.
func (j *Job) solid(p world.BlockPos) bool {
	st, ok := j.r.d.World.BlockAt(p)
	return ok && len(block.CollisionShape(st)) > 0
}

// clearCell cava p si estorba (y otra vez si cae grava o arena encima).
func (j *Job) clearCell(p world.BlockPos) error {
	if j.pits[p] {
		return fmt.Errorf("en %v está el hoyo de la basura: por ahí no", p)
	}
	if j.dropsOnMe(p) {
		return fmt.Errorf("encima de %v hay %s: me caería en la cabeza", p, j.Block(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}))
	}
	for tries := 0; (j.solid(p) || j.fluidBlock(p)) && tries < 6; tries++ {
		if !j.makeSafe(p) {
			return fmt.Errorf("agua o lava junto a %v", p)
		}
		if err := j.Dig(p); err != nil {
			return err
		}
		if err := j.Wait(4); err != nil { // la grava tarda en caer
			return err
		}
		j.plugExposed(2) // si al abrir apareció lava (una bolsa escondida), tapada ya
	}
	if j.solid(p) {
		return fmt.Errorf("%v no se deja cavar", p)
	}
	return nil
}

// TunnelTo llega hasta tener el bloque target al alcance: andando si hay
// camino, y si no cavando un túnel en escalera hacia él.
func (j *Job) TunnelTo(target world.BlockPos) error {
	return j.tunnelTo(target, func() bool { return j.canDig(target) })
}

// closeTo se acerca (andando o cavando) hasta tener target a menos de d de
// los ojos: para tapar la lava que hay detrás de un bloque hay que llegarle.
func (j *Job) closeTo(target world.BlockPos, d float64) error {
	return j.tunnelTo(target, func() bool { return j.canDig(target) && j.eyeDist(target) <= d })
}

// eyeDist es la distancia de los ojos al centro del bloque p.
func (j *Job) eyeDist(p world.BlockPos) float64 {
	pos := j.Pos()
	dx, dy, dz := float64(p.X)+0.5-pos.X, float64(p.Y)+0.5-(pos.Y+1.62), float64(p.Z)+0.5-pos.Z
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

func (j *Job) tunnelTo(target world.BlockPos, there func() bool) error {
	if there() {
		return nil
	}
	if err := j.GoNear(target); err == nil && there() {
		return nil
	}
	// lo pisado en este túnel no se vuelve a pisar (un paso a nivel que
	// vuelve atrás era ir y venir entre dos bloques durante minutos), y sin
	// acercarse en noProgress pasos se deja (el que llama prueba por otro lado)
	visited := map[world.BlockPos]bool{j.Feet(): true}
	dist := func(p world.BlockPos) int { return abs(target.X-p.X) + abs(target.Z-p.Z) + 2*abs(target.Y-p.Y) }
	best, since := dist(j.Feet()), 0
	for steps := 0; steps < 400; steps++ {
		if err := j.Wait(1); err != nil {
			return err
		}
		if there() {
			return nil
		}
		j.Status("cavando un túnel hacia %v", target)
		cur := j.Feet()
		visited[cur] = true
		if d := dist(cur); d < best {
			best, since = d, 0
		} else if since++; since > noProgress {
			return fmt.Errorf("no avanzo hacia %v (agua, lava o cuevas en medio; estoy en %v)", target, cur)
		}
		next, ok := j.nextTunnelCell(cur, target, visited)
		if !ok || next == target || next == (world.BlockPos{X: target.X, Y: target.Y - 1, Z: target.Z}) {
			if there() {
				return nil
			}
			return fmt.Errorf("no puedo cavar hacia %v (agua, lava o nada donde pisar)", target)
		}
		// abrir el paso: los pies y la cabeza en la siguiente celda; subiendo,
		// también encima de la cabeza aquí (el salto); bajando, encima allí
		cells := []world.BlockPos{{X: next.X, Y: next.Y + 1, Z: next.Z}, next}
		if next.Y > cur.Y {
			cells = append([]world.BlockPos{{X: cur.X, Y: cur.Y + 2, Z: cur.Z}}, cells...)
		}
		if next.Y < cur.Y {
			cells = append([]world.BlockPos{{X: next.X, Y: next.Y + 2, Z: next.Z}}, cells...)
		}
		for _, c := range cells {
			if err := j.clearCell(c); err != nil {
				return err
			}
		}
		if len(j.trail) == 0 || j.trail[len(j.trail)-1] != cur {
			j.trail = append(j.trail, cur)
		}
		if err := j.GoTo(next); err != nil {
			return fmt.Errorf("túnel: %w", err)
		}
	}
	return fmt.Errorf("demasiado lejos para un túnel: %v", target)
}

// noProgress: los pasos de túnel sin acercarse al destino tras los que se deja.
const noProgress = 24

// canDig: el bloque p se puede picar desde donde está: al alcance, con una
// cara al aire (no a través de otros bloques) y sin ser la columna bajo los
// pies (cavar hacia abajo en vertical es caerse a lo que haya).
func (j *Job) canDig(p world.BlockPos) bool {
	f := j.Feet()
	if p.X == f.X && p.Z == f.Z && p.Y < f.Y || !j.Reaches(p) {
		return false
	}
	for _, d := range [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
		if !j.solid(world.BlockPos{X: p.X + d[0], Y: p.Y + d[1], Z: p.Z + d[2]}) {
			return true
		}
	}
	return false
}

// nextTunnelCell es el siguiente escalón hacia target: un paso en horizontal
// (por el eje más largo; si no se puede, por el otro) y como mucho uno en
// vertical, con suelo firme debajo y sin agua ni lava al abrirlo.
func (j *Job) nextTunnelCell(cur, target world.BlockPos, visited map[world.BlockPos]bool) (world.BlockPos, bool) {
	dx, dz := target.X-cur.X, target.Z-cur.Z
	dy := 0
	switch {
	case target.Y > cur.Y+1:
		dy = 1
	case target.Y < cur.Y-1:
		dy = -1
	}
	sx, sz := sign(dx), sign(dz)
	var steps [][2]int
	if abs(dx) >= abs(dz) {
		steps = [][2]int{{sx, 0}, {0, sz}}
	} else {
		steps = [][2]int{{0, sz}, {sx, 0}}
	}
	if dx == 0 && dz == 0 { // justo encima o debajo: una escalera de caracol hacia un lado
		steps = [][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}}
	}
	// y si hacia el destino no se puede de ninguna forma, los demás lados
	// (los de los lados antes que hacia atrás; los ya pisados no: visited)
	var extra [][2]int
	back := [2]int{-sx, -sz}
	for _, d := range [][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}} {
		if !slices.Contains(steps, d) && d != back {
			extra = append(extra, d)
		}
	}
	if back != [2]int{0, 0} && !slices.Contains(steps, back) {
		extra = append(extra, back)
	}
	// primero por donde no hay lava; si no, por donde se puede tapar (clearCell
	// la tapa). Subiendo, si abrir encima de la cabeza soltaría grava, antes un
	// paso a nivel: desde la columna de al lado se sube sin nada encima
	dys := []int{dy}
	if dy != 0 {
		dys = append(dys, 0)
	}
	for _, group := range [][][2]int{steps, extra} {
		for _, dy := range dys {
			for pass := 0; pass < 2; pass++ {
				for _, st := range group {
					if st == [2]int{0, 0} {
						continue
					}
					n := world.BlockPos{X: cur.X + st[0], Y: cur.Y + dy, Z: cur.Z + st[1]}
					if visited[n] {
						continue // ya se pisó: volver es dar vueltas
					}
					floor := world.BlockPos{X: n.X, Y: n.Y - 1, Z: n.Z}
					if !j.solid(floor) || j.liquid(floor) {
						continue // nada donde pisar (una cueva): por otro lado
					}
					open := []world.BlockPos{n, {X: n.X, Y: n.Y + 1, Z: n.Z}}
					if dy > 0 {
						open = append(open, world.BlockPos{X: cur.X, Y: cur.Y + 2, Z: cur.Z})
					}
					if dy < 0 {
						open = append(open, world.BlockPos{X: n.X, Y: n.Y + 2, Z: n.Z})
					}
					safe := !j.nearPit(n)
					for _, c := range open {
						// el hoyo de la basura (abrirlo es recogerla otra vez), o grava
						// encima de la cabeza (abrirlo es enterrarse)
						if j.pits[c] || j.dropsOnMe(c) {
							safe = false
						}
						if pass == 0 && (j.solid(c) && !j.safeToOpen(c) || j.liquid(c)) ||
							pass == 1 && !j.safeToOpen(c) && !j.pluggable(c) {
							safe = false
						}
					}
					if safe {
						return n, true
					}
				}
			}
		}
	}
	return world.BlockPos{}, false
}

// Surface vuelve por los túneles que cavó, celda a celda hacia atrás, hasta
// donde empezó a cavar (subir por el pathfinding desde el fondo de una mina
// puede no encontrar camino). En cada escalón hacia arriba deja tres de alto
// para que el salto no se dé con el techo.
func (j *Job) Surface() error {
	defer func() { j.trail = nil }()
	if err := j.retrace(); err != nil {
		return err
	}
	// desandado el túnel, ¿aún con techo? (el túnel empezó bajo tierra: otra
	// tarea, un reinicio): una escalera hacia arriba hasta ver el cielo
	if j.Underground() {
		return j.ToSky()
	}
	return nil
}

// Underground: hay techo sobre la cabeza (algo macizo que no son hojas en
// las 40 celdas de encima).
func (j *Job) Underground() bool {
	f := j.Feet()
	for y := f.Y + 2; y <= f.Y+40; y++ {
		p := world.BlockPos{X: f.X, Y: y, Z: f.Z}
		n := j.Block(p)
		if n == "" {
			return false // sin cargar: no se sabe
		}
		if j.solid(p) && !strings.HasSuffix(n, "_leaves") && !strings.HasSuffix(n, "_log") {
			return true
		}
	}
	return false
}

// ToSky sube cavando una escalera (dos de alto, escalón a escalón) hasta
// tener el cielo encima.
func (j *Job) ToSky() error {
	j.Logf("estoy bajo tierra: subo cavando hasta la superficie")
	for k := 0; k < 16 && j.Underground(); k++ {
		if err := j.Wait(1); err != nil {
			return err
		}
		j.Status("subiendo a la superficie")
		f := j.Feet()
		d := [][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}}[k%4]
		to := world.BlockPos{X: f.X + 6*d[0], Y: f.Y + 6, Z: f.Z + 6*d[1]}
		if err := j.TunnelTo(to); err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			j.Problem("subir: %v (pruebo por otro lado)", err)
		}
	}
	j.trail = nil // ese túnel ya no es para volver
	if j.Underground() {
		return fmt.Errorf("no consigo salir a la superficie (estoy en %v)", j.Feet())
	}
	return nil
}

// retrace desanda el túnel cavado (el rastro), de la última celda a la primera.
func (j *Job) retrace() error {
	cells := make([]world.BlockPos, len(j.trail))
	for i, p := range j.trail {
		cells[len(cells)-1-i] = p
	}
	return j.walkCells(cells)
}

// walkCells anda por un túnel cavado celda a celda (en el orden dado),
// abriendo lo que se haya cerrado (grava que cayó, lava tapada).
func (j *Job) walkCells(path []world.BlockPos) error {
	for i := range path {
		p, cur := path[i], j.Feet()
		if p == cur {
			continue
		}
		j.Status("por el túnel (%d/%d)", i+1, len(path))
		cells := []world.BlockPos{{X: p.X, Y: p.Y + 1, Z: p.Z}, p}
		if p.Y > cur.Y {
			cells = append([]world.BlockPos{{X: cur.X, Y: cur.Y + 2, Z: cur.Z}}, cells...)
		}
		for _, c := range cells {
			if j.Reaches(c) {
				if err := j.clearCell(c); err != nil {
					return err
				}
			}
		}
		if err := j.GoTo(p); err != nil {
			if p.Y > cur.Y { // otra vez, con más sitio para saltar
				for _, c := range []world.BlockPos{{X: cur.X, Y: cur.Y + 3, Z: cur.Z}, {X: p.X, Y: p.Y + 2, Z: p.Z}} {
					if j.Reaches(c) {
						_ = j.clearCell(c)
					}
				}
				err = j.GoTo(p)
			}
			if err != nil {
				return fmt.Errorf("salir del túnel: %w", err)
			}
		}
	}
	return nil
}

func sign(x int) int {
	switch {
	case x > 0:
		return 1
	case x < 0:
		return -1
	}
	return 0
}

// Mine saca bloques que cumplen ok hasta tener want (lo que cuenta have):
// busca el más cercano, llega (cavando si hace falta) y saca también los que
// alcance desde ahí. Nunca el bloque bajo sus pies. Si no ve ninguno, cava
// túneles en escalera hacia abajo para buscar.
func (j *Job) Mine(what string, ok func(string) bool, have func() int, want int) error {
	return j.mine(mineOpts{what: what, ok: ok, have: have, want: want, runs: 5})
}

// MineAt es Mine a una altura: baja (cavando) hasta level y, si no ve
// ninguno, hace galerías rectas a esa altura (minería en ramas: cada una
// destapa lo de los lados, también lo que el servidor esconde) hasta runs.
func (j *Job) MineAt(what string, ok func(string) bool, have func() int, want, level, runs int) error {
	return j.mine(mineOpts{what: what, ok: ok, have: have, want: want, level: &level, runs: runs})
}

type mineOpts struct {
	what string
	ok   func(string) bool
	have func() int
	want int
	// level: la altura a la que buscar (nil: cualquiera, bajando)
	level *int
	runs  int // túneles o galerías de exploración como mucho
	// stop: por qué dejar de minar ya (nil: seguir), mirado en cada vuelta
	stop func() error
}

// MineUntil es MineAt con una razón para parar antes (los picos gastados).
func (j *Job) MineUntil(what string, ok func(string) bool, have func() int, want, level, runs int, stop func() error) error {
	return j.mine(mineOpts{what: what, ok: ok, have: have, want: want, level: &level, runs: runs, stop: stop})
}

func (j *Job) mine(o mineOpts) error {
	skip := map[world.BlockPos]bool{}
	explored, branch := 0, 0
	down, up := 40, 8
	if o.level != nil {
		down, up = 12, 12
	}
	defer func() { // lo que se vio y no se pudo sacar: que se sepa dónde
		var left []string
		for q := range skip {
			if o.ok(j.Block(q)) {
				left = append(left, fmt.Sprint(q))
			}
		}
		if len(left) > 0 {
			sort.Strings(left)
			j.Logf("%s que dejo sin sacar (%d): %s", o.what, len(left), strings.Join(left, ", "))
		}
	}()
	for tries := 0; o.have() < o.want && tries < o.want*4+o.runs*2+12; tries++ {
		if err := j.Wait(1); err != nil {
			return err
		}
		if o.stop != nil {
			if err := o.stop(); err != nil {
				return err
			}
		}
		j.makeRoom()
		j.Status("minando %s (%d/%d)", o.what, o.have(), o.want)
		f := j.Feet()
		var p world.BlockPos
		found := false
		if o.level != nil && f.Y > *o.level+4 {
			// lo que se vea cerca, ya (como un jugador); si no, a su altura
			if p, found = j.findBlock(16, 16, 8, o.ok, skip); !found {
				if err := j.DescendTo(*o.level); err != nil {
					return fmt.Errorf("bajar a %d: %w", *o.level, err)
				}
				continue
			}
		} else {
			p, found = j.findBlock(32, down, up, o.ok, skip)
		}
		if !found {
			if explored >= o.runs {
				return fmt.Errorf("no encuentro %s (exploré %d túneles)", o.what, explored)
			}
			explored++
			if o.level != nil {
				// galerías: largas en x, una al lado de otra cada 3 (lo de en medio queda a la vista)
				d := 1
				if branch%2 == 1 {
					d = -1
				}
				if err := j.stripRun(d, 0, 24, o.ok, skip); err != nil {
					j.Problem("galería: %v", err)
				}
				if err := j.stripRun(0, 1, 3, o.ok, skip); err != nil { // a la siguiente
					j.Problem("galería: %v", err)
					if err := j.stripRun(0, -1, 3, o.ok, skip); err != nil {
						branch++ // por el otro lado tampoco: girar
					}
				}
				branch++
				continue
			}
			// sin altura: un túnel en escalera hacia abajo y hacia un lado
			d := [][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}}[explored%4]
			to := world.BlockPos{X: f.X + 14*d[0], Y: max(f.Y-12, -50), Z: f.Z + 14*d[1]}
			j.Logf("no veo %s: cavo hacia %v para buscar", o.what, to)
			if err := j.TunnelTo(to); err != nil {
				j.Problem("túnel: %v", err)
			}
			continue
		}
		if j.liquid(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}) && !j.fluidBlock(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}) {
			skip[p] = true // bajo un bloque inundado: eso no se tapa
			continue
		}
		if err := j.TunnelTo(p); err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			j.Problem("%s en %v: %v", o.what, p, err)
			skip[p] = true
			continue
		}
		j.digAround(o.ok, skip, func() bool { return o.have() >= o.want })
		if o.ok(j.Block(p)) { // no se pudo sacar desde aquí: a por otro
			skip[p] = true
		}
		if err := j.Collect(6); err != nil {
			return err
		}
		if err := j.fetchDrops(o.ok, 10); err != nil {
			return err
		}
	}
	if o.have() < o.want {
		return fmt.Errorf("solo junté %d de %d (%s)", o.have(), o.want, o.what)
	}
	return nil
}

// digAround pica los bloques que cumplen ok al alcance (menos la columna bajo los pies).
func (j *Job) digAround(ok func(string) bool, skip map[world.BlockPos]bool, enough func() bool) {
	f := j.Feet()
	for dx := -4; dx <= 4 && !enough(); dx++ {
		for dy := -2; dy <= 4; dy++ {
			for dz := -4; dz <= 4; dz++ {
				q := world.BlockPos{X: f.X + dx, Y: f.Y + dy, Z: f.Z + dz}
				if skip[q] || !ok(j.Block(q)) || !j.canDig(q) {
					continue
				}
				if j.dropsOnMe(q) { // sobre la cabeza y con grava encima: desde otro sitio
					continue
				}
				if !j.makeSafe(q) { // lava al lado: tapada antes de picar, o se deja
					j.Problem("%s en %v: lava al lado que no puedo tapar, lo dejo", j.Block(q), q)
					skip[q] = true
					continue
				}
				if err := j.Dig(q); err != nil {
					skip[q] = true
					if errors.Is(err, ErrNoPicks) {
						return
					}
				}
			}
		}
	}
}

// DescendTo baja cavando una escalera hasta la altura y.
func (j *Job) DescendTo(y int) error {
	f := j.Feet()
	if f.Y <= y+1 {
		return nil
	}
	j.Logf("bajo cavando hasta y=%d", y)
	// en vertical: una escalera en zigzag de dos columnas (no se aleja, y se
	// sube por ella); si se corta (lava, una cueva), desde un poco más allá
	for tries := 0; j.Feet().Y > y+1 && tries < 4; tries++ {
		f = j.Feet()
		dirs := [][2]int{{0, 0}, {6, 0}, {0, 6}, {-6, 0}}
		d := dirs[tries%4]
		to := world.BlockPos{X: f.X + d[0], Y: y, Z: f.Z + d[1]}
		if err := j.TunnelTo(to); err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			j.Problem("bajar: %v (pruebo por otro lado)", err)
		}
	}
	if j.Feet().Y > y+4 {
		return fmt.Errorf("no consigo bajar (estoy en y=%d)", j.Feet().Y)
	}
	return nil
}

// stripRun cava una galería recta de 1x2 de n bloques hacia (dx, dz) a la
// altura de los pies. Para antes si ve un bloque que cumple ok (para ir a
// por él), si delante hay agua o lava, o nada donde pisar.
func (j *Job) stripRun(dx, dz, n int, ok func(string) bool, skip map[world.BlockPos]bool) error {
	for i := 0; i < n; i++ {
		if err := j.Wait(1); err != nil {
			return err
		}
		cur := j.Feet()
		next := world.BlockPos{X: cur.X + dx, Y: cur.Y, Z: cur.Z + dz}
		head := world.BlockPos{X: next.X, Y: next.Y + 1, Z: next.Z}
		if !j.solid(world.BlockPos{X: next.X, Y: next.Y - 1, Z: next.Z}) {
			return fmt.Errorf("sin suelo delante (%v)", next)
		}
		for _, c := range []world.BlockPos{head, next} {
			if !j.safeToOpen(c) && !j.pluggable(c) {
				return fmt.Errorf("agua o lava delante que no puedo tapar (%v)", c)
			}
		}
		j.Status("galería (%d/%d)", i+1, n)
		for _, c := range []world.BlockPos{head, next} {
			if err := j.clearCell(c); err != nil {
				return err
			}
		}
		if len(j.trail) == 0 || j.trail[len(j.trail)-1] != cur {
			j.trail = append(j.trail, cur)
		}
		if err := j.GoTo(next); err != nil {
			return err
		}
		// ¿algo a la vista ahora? (lo destapado al cavar)
		if _, found := j.findBlock(5, 3, 3, ok, skip); found {
			return nil
		}
	}
	return nil
}

// junk: lo que sale cavando y no sirve (más allá de unos cuantos para construir).
func junk(n string) bool {
	switch n {
	case "netherrack", "cobblestone", "cobbled_deepslate", "dirt", "gravel", "andesite", "diorite", "granite",
		"tuff", "basalt", "blackstone", "soul_sand", "soul_soil", "calcite", "smooth_basalt", "magma_block":
		return true
	}
	return false
}

// IsJunk: n es basura de cavar (para el bot: lo que se tira para hacer sitio).
func IsJunk(n string) bool { return junk(n) }

// fillerReserve: en el Nether se queda con esto de netherrack (3 montones)
// para tapar lava; lo demás de cavar es basura.
const fillerReserve = 3 * 64

// junkSlot es un montón de basura del inventario.
type junkSlot struct {
	i int
	n string
	c int
}

// junkSlots son los montones de basura que tira: todos menos los que se
// quedan (keep bloques de los que sirven para construir o tapar, y en el
// Nether el netherrack primero).
func (j *Job) junkSlots(keep int, nether bool) []junkSlot {
	s := j.r.d.Screens
	var all []junkSlot
	s.Lock()
	for i := invStart; i <= invEnd; i++ {
		sl := s.Inventory.Slots[i]
		if n := strings.TrimPrefix(itemName(int32(sl.Item)), "minecraft:"); sl.Count > 0 && junk(n) {
			all = append(all, junkSlot{i, n, int(sl.Count)})
		}
	}
	s.Unlock()
	// lo que se queda: primero el netherrack (en el Nether), luego cualquier
	// bloque macizo; nunca grava, arena de almas ni magma (caen, frenan, queman)
	useful := func(n string) bool {
		return n != "gravel" && n != "soul_sand" && n != "soul_soil" && n != "magma_block"
	}
	kept := map[int]bool{}
	for pass := 0; pass < 2 && keep > 0; pass++ {
		for _, sl := range all {
			if keep <= 0 || kept[sl.i] || !useful(sl.n) || pass == 0 && nether && sl.n != "netherrack" {
				continue
			}
			kept[sl.i] = true
			keep -= sl.c
		}
	}
	var out []junkSlot
	for _, sl := range all {
		if !kept[sl.i] {
			out = append(out, sl)
		}
	}
	return out
}

// makeRoom tira la basura de cavar cuando el inventario se llena y ya no la
// recoge. En el Nether (/debris) se queda con fillerReserve de netherrack para
// la lava y tira lo demás a un hoyo que cava junto al túnel y luego tapa: ni
// estorba el camino de vuelta ni lo recoge al pasar (lo tirado en el túnel se
// coge solo al andar por encima). Fuera, un montón para construir, a los pies.
func (j *Job) makeRoom() {
	if j.FreeSlots() > 3 {
		return
	}
	nether := j.inNether() || j.junkPit
	keep := 64 // bloques para construir: los de un montón
	if nether {
		keep = fillerReserve
	}
	slots := j.junkSlots(keep, nether)
	j.ignore = func(n string) bool {
		// el netherrack se recoge mientras falte para la reserva
		return junk(n) && !(nether && n == "netherrack" && j.Count("netherrack") < fillerReserve)
	}
	if len(slots) == 0 {
		return
	}
	j.CloseScreen()
	thrown, where := 0, "a mis pies"
	if nether {
		if pit, ok := j.digDumpPit(); ok {
			thrown = j.throwSlots(slots, &pit)
			where = fmt.Sprintf("a un hoyo en %v", pit)
			j.coverPit(pit)
		}
	}
	if thrown == 0 {
		thrown = j.throwSlots(slots, nil)
	}
	if thrown > 0 {
		j.Logf("tiro %d bloques de basura %s para hacer sitio (me quedo %d de netherrack)", thrown, where, j.Count("netherrack"))
		_ = j.r.d.Ctl.WaitTicks(j.ctx, 5)
	}
}

// SetJunkPit: la basura se tira a un hoyo tapado, guardando netherrack para
// la lava, también fuera del Nether (/debris lo hace siempre en el Nether).
func (j *Job) SetJunkPit(on bool) { j.junkPit = on }

// throwSlots tira los montones; con pit, mirando al fondo del hoyo.
func (j *Job) throwSlots(slots []junkSlot, pit *world.BlockPos) int {
	// hacia el hoyo y 45° abajo: lo tirado sale de los ojos a 0.3 por tick
	// (Player.drop) y cae dentro, a un bloque; mirando al fondo, en vertical,
	// caería en la celda del propio bot
	look := func() {
		if pit != nil {
			p := j.Pos()
			cx, cz := float64(pit.X)+0.5, float64(pit.Z)+0.5
			h := math.Hypot(cx-p.X, cz-p.Z)
			act.LookAt(j.r.d.Player, false, cx, p.Y+1.62-h, cz)
		}
	}
	look()
	_ = j.Wait(2) // que el servidor sepa hacia dónde mira
	thrown := 0
	for _, sl := range slots {
		look()
		if err := j.r.d.Screens.Click(0, sl.i, 1, types.ContainerInputThrow); err == nil {
			thrown += sl.c
		}
		_ = j.Wait(1)
	}
	if pit != nil {
		_ = j.Wait(10) // que caigan al fondo antes de taparlo
	}
	return thrown
}

// digDumpPit cava un hoyo de 2 de hondo bajo una celda abierta junto al bot
// (el suelo del túnel de al lado) y dice su boca. Lo tirado queda dos bloques
// por debajo de los pies: fuera del alcance de recoger.
func (j *Job) digDumpPit() (world.BlockPos, bool) {
	f := j.Feet()
	for _, d := range [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}} {
		c := add(f, d)
		top := world.BlockPos{X: c.X, Y: c.Y - 1, Z: c.Z}
		bottom := world.BlockPos{X: c.X, Y: c.Y - 2, Z: c.Z}
		if j.solid(c) || j.solid(world.BlockPos{X: c.X, Y: c.Y + 1, Z: c.Z}) || j.liquid(c) ||
			!j.solid(top) || j.r.Store.IsProtected(top) {
			continue // la celda de al lado no es pasillo
		}
		// ni a la lava ni al vacío: lo de abajo tiene que aguantar
		under := world.BlockPos{X: c.X, Y: c.Y - 3, Z: c.Z}
		if !j.solid(under) || j.liquid(under) {
			continue
		}
		ok := true
		for _, q := range []world.BlockPos{top, bottom} {
			if j.solid(q) && (!j.makeSafe(q) || j.Dig(q) != nil) {
				ok = false
				break
			}
		}
		if ok && !j.solid(top) && !j.solid(bottom) {
			if j.pits == nil {
				j.pits = map[world.BlockPos]bool{}
			}
			j.pits[top], j.pits[bottom] = true, true // no se vuelve a cavar por ahí
			j.pitFloors = append(j.pitFloors, bottom)
			return top, true
		}
	}
	return world.BlockPos{}, false
}

// nearPit: con los pies en c, el bot recogería lo de un hoyo de basura (el
// juego coge lo que toca la caja del jugador agrandada un bloque a los lados
// y medio abajo, a través de las paredes): el fondo a un bloque en
// horizontal y a la altura de los pies o hasta dos más arriba.
func (j *Job) nearPit(c world.BlockPos) bool {
	for _, q := range j.pitFloors {
		if abs(q.X-c.X) <= 1 && abs(q.Z-c.Z) <= 1 && q.Y >= c.Y && q.Y <= c.Y+2 {
			return true
		}
	}
	return false
}

// coverPit tapa la boca del hoyo: el suelo del pasillo vuelve a estar entero.
func (j *Job) coverPit(top world.BlockPos) {
	f := j.filler()
	if f == "" {
		j.Logf("hoyo de basura en %v: sin bloque con que taparlo", top)
		return
	}
	if err := j.Place(f, top); err != nil {
		j.Logf("tapar el hoyo de basura en %v: %v", top, err)
	}
}

// fetchDrops va a por lo que soltó lo picado y Collect no alcanzó (ok: los
// bloques picados; cuenta también lo que sueltan: "diamond" de una mena de
// diamante): lo sacado de lejos se queda en su hueco, cerrado por la lava
// tapada o sin camino andando; se cava hasta él. Si uno no se puede, a por
// el siguiente: nada se queda atrás.
func (j *Job) fetchDrops(ok func(string) bool, radius float64) error {
	if j.FreeSlots() == 0 {
		return nil // (lleno: lo del suelo se queda)
	}
	want := dropOf(ok)
	tried := map[int32]bool{}
	for round := 0; round < 8; round++ {
		p := j.Pos()
		var target *entities.Entity
		for _, e := range j.r.d.Entities.Nearby(p.X, p.Y, p.Z, radius) {
			if e.Type != "minecraft:item" || tried[e.ID] {
				continue
			}
			v, has := e.Data[entitydata.ItemEntityItem]
			if !has {
				continue
			}
			st, isStack := v.Value.(*types.ItemStack)
			if !isStack || !want(strings.TrimPrefix(itemName(int32(st.Item)), "minecraft:")) || j.burning(itemCell(e)) {
				continue
			}
			e := e
			target = &e
			break
		}
		if target == nil {
			return nil
		}
		tried[target.ID] = true
		id, cell := target.ID, itemCell(*target)
		what := strings.TrimPrefix(itemName(int32(target.Data[entitydata.ItemEntityItem].Value.(*types.ItemStack).Item)), "minecraft:")
		j.Status("cavando hasta %s que quedó en %v", what, cell)
		gone := func() bool {
			_, still := j.r.d.Entities.Get(id)
			return !still
		}
		near := func() bool {
			f := j.Feet()
			return gone() || abs(f.X-cell.X) <= 1 && abs(f.Z-cell.Z) <= 1 && (f.Y == cell.Y || f.Y == cell.Y+1)
		}
		if err := j.tunnelTo(cell, near); err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			j.Problem("no llego a %s en %v: %v", what, cell, err)
			continue
		}
		for t := 0; t < 10 && !gone(); t++ {
			if err := j.Wait(1); err != nil {
				return err
			}
		}
		if !gone() {
			// al lado pero sin cogerlo (un bloque más arriba no llega): meterse
			// en su celda, abriendo encima para que quepa el cuerpo (y una más:
			// bajando a ella, la cabeza pasa por ahí)
			var err error
			for _, dy := range []int{2, 1, 0} {
				c := world.BlockPos{X: cell.X, Y: cell.Y + dy, Z: cell.Z}
				if dy == 2 && j.Feet().Y == cell.Y {
					continue // a su altura no hace falta
				}
				if err = j.clearCell(c); err != nil {
					break
				}
			}
			if err == nil {
				err = j.GoTo(cell)
			}
			for t := 0; t < 20 && !gone() && err == nil; t++ {
				err = j.Wait(1)
			}
			if j.Fatal() != nil {
				return j.Fatal()
			}
			if !gone() {
				j.Problem("no consigo coger %s de %v: %v", what, cell, err)
			}
		}
	}
	return nil
}

// dropOf: lo que suelta un bloque que cumple ok (el bloque mismo, o lo que
// sueltan los minerales: "diamond" de "deepslate_diamond_ore").
func dropOf(ok func(string) bool) func(string) bool {
	return func(item string) bool {
		if ok(item) {
			return true
		}
		return oreDrops[item]
	}
}
