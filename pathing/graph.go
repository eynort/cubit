package pathing

import (
	"container/heap"
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/path"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// Node es un sitio donde estar: la celda de los pies, la altura del suelo y
// cómo se llega desde el nodo anterior (path.Walk, Ascend, Descend, GapJump, Climb, Swim).
type Node = path.Node

const (
	halfWidth = 0.3
	height    = 1.8
	maxStep   = 0.6  // se sube andando (losas, escalones)
	maxJump   = 1.25 // altura de un salto
	safeFall  = 3    // caída sin daño
	waterFall = 12   // caída sobre agua
)

// Movimientos que ponen un bloque (además de los de path: Walk, Ascend…).
const (
	// Bridge: andar a una celda sin suelo poniendo antes un bloque bajo ella,
	// contra el lateral del bloque que se pisa (agachado al borde).
	Bridge = path.Swim + 1 + iota
	// Pillar: subir un bloque en el sitio: saltar y poner un bloque debajo.
	Pillar
	// Break: andar a una celda tapada por hojas (o enredaderas, azaleas),
	// rompiéndolas antes, como un jugador.
	Break
	// BreakUp: subir un escalón a una celda tapada por hojas (rompiendo
	// también las de encima de la cabeza, para el salto).
	BreakUp
)

// kindName es el nombre de un movimiento (path.Kind.String no conoce Bridge ni Pillar).
func kindName(k path.Kind) string {
	switch k {
	case Bridge:
		return "puente"
	case Pillar:
		return "torre"
	case Break:
		return "romper hojas"
	case BreakUp:
		return "romper hojas y subir"
	}
	return k.String()
}

// costes de construir: más que andar, para que solo se construya cuando haga falta
const (
	bridgeCost = 5
	pillarCost = 6
)

// ErrNoPath: no hay camino (todo lo alcanzable buscado).
var ErrNoPath = errors.New("no hay camino")

// graph es el mundo tal como lo ve la búsqueda. Para saber si una celda es
// suelo usa la lógica de la librería (losas, vallas, escaleras, puertas,
// agua, lava) vía path.Graph.Start; los movimientos son nuestros.
type graph struct {
	w    physics.Blocks
	kit  *path.Graph
	memo map[world.BlockPos]floorMemo // por búsqueda
	// extraMemo: lo que cuesta de más cada celda (por búsqueda: a una celda se
	// llega desde muchas vecinas, y cada vez eran ~30 consultas al mundo)
	extraMemo map[world.BlockPos]float64
	avoid     map[world.BlockPos]bool    // celdas que no pisar (el servidor las rechazó)
	penalty   map[world.BlockPos]float64 // coste de más: trabas de antes (memory.go)
	// climbPenalty se suma a subir y a saltar huecos (el servidor los rechaza)
	climbPenalty float64
	// breakSoft: se pueden romper hojas para pasar (hay manos)
	breakSoft bool
	// passable: además de lo blando, celdas que se pueden romper para pasar
	// (los andamios de una construcción); nil: ninguna
	passable func(world.BlockPos) bool
	// build: hay bloques para construir (puentes y torres); goalY: la altura
	// de la meta (no se hacen torres más arriba)
	build bool
	goalY int
	// noBridge: con build, solo torres (SetBridges)
	noBridge bool
	// digTicks: lo que tarda en romper un bloque (<0: no se rompe); nil: no
	// rompe. Para torres bajo un techo que se rompe enseguida (capCost)
	digTicks func(block.StateID) int
	digMemo  map[block.StateID]int
	// jump es la altura de un salto (1.25; más con el efecto de salto: Salto II ~2.2)
	jump float64
	// fallBudget es cuánto daño de caída se puede permitir (vida menos un margen):
	// una caída de k bloques quita k-3
	fallBudget float64
	// maxFall es la caída más alta sin daño que se acepta (0: la normal, 3)
	maxFall int
	// health es la vida del bot al buscar (0: no se sabe, como si estuviera llena)
	health float64
	// budget es cuánto puede durar una búsqueda (0: sin límite); acabado, se
	// devuelve el camino hacia lo más cercano encontrado
	budget time.Duration
}

type floorMemo struct {
	f  float64
	ok bool
}

func newGraph(w physics.Blocks, tags physics.Tags) *graph {
	return &graph{w: w, kit: &path.Graph{World: w, Tags: tags}, jump: maxJump}
}

// jumpHeight es lo alto que llega un salto con boost de impulso extra (el
// efecto de salto: 0.1 por nivel), como la física: v sube 0.42+boost y cada
// tick v = (v - 0.08) × 0.98.
func jumpHeight(boost float64) float64 {
	h, v := 0.0, 0.42+boost
	for v > 0 {
		h += v
		v = (v - 0.08) * 0.98
	}
	return h
}

// floor es la altura del suelo de la celda p si un jugador puede estar ahí.
func (g *graph) floor(p world.BlockPos) (float64, bool) {
	if g.memo != nil {
		if m, ok := g.memo[p]; ok {
			return m.f, m.ok
		}
	}
	n, ok := g.kit.Start(float64(p.X)+0.5, float64(p.Y), float64(p.Z)+0.5)
	ok = ok && n.Pos == p
	if g.memo != nil {
		g.memo[p] = floorMemo{n.Floor, ok}
	}
	return n.Floor, ok
}

func (g *graph) state(p world.BlockPos) (block.StateID, bool) { return g.w.BlockAt(p) }

func (g *graph) name(p world.BlockPos) string {
	s, ok := g.state(p)
	if !ok {
		return ""
	}
	return world.StateString(s)
}

func (g *graph) water(p world.BlockPos) bool { return g.is(p, flagWater) }

// afloat: la celda p está justo encima del agua sin nada sólido debajo: donde
// está un jugador nadando en la superficie, con la cabeza fuera. La búsqueda
// lo pone "de pie" sobre el agua, pero flotando no hay de dónde saltar: no se
// sube a una orilla un bloque más alta que el agua (en los logs, minutos
// nadando contra una orilla así, saltando, con el camino subiendo por ahí).
func (g *graph) afloat(p world.BlockPos) bool {
	if g.water(p) || g.climbable(p) {
		return false
	}
	below := world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z}
	if !g.water(below) {
		return false
	}
	s, ok := g.state(below)
	if !ok {
		return false
	}
	for _, b := range block.CollisionShape(s) {
		if b.MaxY >= 0.85 {
			return false // un bloque con agua dentro, casi entero: se pisa
		}
	}
	return true
}

// swimming: en p se nada (en el agua o flotando encima): sin suelo del que saltar.
func (g *graph) swimming(p world.BlockPos) bool { return g.water(p) || g.afloat(p) }

// swimReach es lo más alto que se pone el suelo al salir nadando desde p: la
// superficie del agua (8/9 de su bloque) más lo que empuja el impulso de salir
// del agua contra una pared (el vanilla jumpOutOfFluid). En los logs, nadando
// contra una orilla 1.1 por encima del agua los pies no pasaban de 1.0 encima:
// se sube a una losa o a un escalón casi a ras, no a un bloque entero.
// Fuera del agua, sin límite (+Inf).
func (g *graph) swimReach(p world.BlockPos) float64 {
	const lift = 0.75
	switch {
	case g.afloat(p):
		return float64(p.Y) - 1 + 8.0/9 + lift
	case g.water(p):
		top := p.Y
		for k := 0; k < 16 && g.water(world.BlockPos{X: p.X, Y: top + 1, Z: p.Z}); k++ {
			top++
		}
		return float64(top) + 8.0/9 + lift
	}
	return math.Inf(1)
}

// airAbove: subiendo en vertical desde p (por agua) se llega a aire en menos de n bloques.
func (g *graph) airAbove(p world.BlockPos, n int) bool {
	for dy := 1; dy <= n; dy++ {
		q := world.BlockPos{X: p.X, Y: p.Y + dy, Z: p.Z}
		if g.water(q) {
			continue
		}
		s, ok := g.state(q)
		return ok && block.IsAir(s)
	}
	return false
}

// airNear: desde p (bajo el agua) se sale a respirar cerca: subiendo por su
// columna o por una de las de alrededor (a dos bloques), con aire a menos de 8
// encima. Bucear bajo un muro o una barca es esto: el aire está al lado. Una
// cueva inundada o un túnel largo, no (el aire dura 15 s).
func (g *graph) airNear(p world.BlockPos) bool {
	if g.airAbove(p, 8) {
		return true
	}
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			if dx == 0 && dz == 0 {
				continue
			}
			q := world.BlockPos{X: p.X + dx, Y: p.Y, Z: p.Z + dz}
			if g.water(q) && g.airAbove(q, 8) {
				return true
			}
		}
	}
	return false
}

// holeDepth es cuántos bloques se cae por el hueco mid (los pies a mid.Y)
// hasta lo primero que pare la caída (hasta 32; al agua: sin daño, 0).
// Lo que haya abajo (lava, cactus) es cosa de deadly, no de la caída.
func (g *graph) holeDepth(mid world.BlockPos) int {
	for d := 0; d < 32; d++ {
		q := world.BlockPos{X: mid.X, Y: mid.Y - d - 1, Z: mid.Z}
		if g.water(q) {
			return 0
		}
		s, ok := g.state(q)
		if !ok {
			return 32 // sin cargar: como si no tuviera fondo
		}
		if len(block.CollisionShape(s)) > 0 || strings.Contains(g.name(q), "lava") {
			return d
		}
	}
	return 32
}

func (g *graph) climbable(p world.BlockPos) bool { return g.is(p, flagClimbable) }

func (g *graph) deadly(p world.BlockPos) bool { return g.is(p, flagDeadly) }

// clear dice si el cuerpo (columna central de la celda x,z) cabe entre y0 e y1.
func (g *graph) clear(x, z int, y0, y1 float64) bool {
	cx, cz := float64(x)+0.5, float64(z)+0.5
	for y := int(math.Floor(y0)); float64(y) < y1; y++ {
		s, ok := g.state(world.BlockPos{X: x, Y: y, Z: z})
		if !ok {
			return false
		}
		for _, b := range block.CollisionShape(s) {
			if b.MaxX <= cx-halfWidth-float64(x) || b.MinX >= cx+halfWidth-float64(x) ||
				b.MaxZ <= cz-halfWidth-float64(z) || b.MinZ >= cz+halfWidth-float64(z) {
				continue
			}
			if float64(y)+b.MaxY > y0+1e-6 && float64(y)+b.MinY < y1-1e-6 {
				return false
			}
		}
	}
	return true
}

type edge struct {
	to   Node
	cost float64
}

var dirs = [8][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {1, -1}, {-1, 1}, {-1, -1}}

// neighbours son los nodos alcanzables desde n en un movimiento.
func (g *graph) neighbours(n Node) []edge {
	var out []edge
	p, f0 := n.Pos, n.Floor
	inWater := g.water(p)
	// nadando no hay suelo del que saltar: no se sube más alto que esto
	reach := g.swimReach(p)
	// sobre un bloque que se pondrá (torre, puente): cuenta como bloque entero
	onTop := g.solidTop(p, f0) || n.Kind == Pillar || n.Kind == Bridge
	for _, d := range dirs {
		diag := d[0] != 0 && d[1] != 0
		base := 1.0
		if diag {
			base = math.Sqrt2
		}
		if inWater {
			base *= 2
		}
		// en diagonal las dos celdas de los lados tienen que dejar pasar el cuerpo
		sides := func(y0, y1 float64) bool {
			return !diag || (g.clear(p.X+d[0], p.Z, y0, y1) && g.clear(p.X, p.Z+d[1], y0, y1))
		}

		// andar al mismo nivel (losas y escalones incluidos)
		level := world.BlockPos{X: p.X + d[0], Y: p.Y, Z: p.Z + d[1]}
		if f, ok := g.floor(level); ok && f-f0 <= maxStep+1e-9 && sides(math.Max(f, f0), math.Max(f, f0)+height) {
			kind := path.Walk
			if g.swimming(level) {
				kind = path.Swim
			}
			out = append(out, edge{Node{Pos: level, Floor: f, Kind: kind}, base + g.extra(level)})
			continue
		}
		// tapado por hojas: romperlas y pasar (solo en recto, al mismo nivel)
		// (romper hojas es barato: mucho menos que una torre o un puente, que
		// gastan bloques y hay que cavarlos)
		if g.breakSoft && !diag {
			if cost, ok := g.breakable(level, f0); ok {
				out = append(out, edge{Node{Pos: level, Floor: float64(level.Y), Kind: Break}, base + cost + g.extra(level)})
				continue
			}
			// un escalón arriba tapado por hojas (y hojas encima de la cabeza al saltar)
			up := world.BlockPos{X: level.X, Y: level.Y + 1, Z: level.Z}
			over := world.BlockPos{X: p.X, Y: p.Y + 2, Z: p.Z}
			if cost, ok := g.breakable(up, float64(up.Y)); ok && f0 == float64(p.Y) && float64(up.Y) <= reach {
				if os, ok := g.state(over); ok && (len(block.CollisionShape(os)) == 0 || Soft(g.name(over))) {
					if Soft(g.name(over)) {
						cost += 1
					}
					out = append(out, edge{Node{Pos: up, Floor: float64(up.Y), Kind: BreakUp}, base + 0.6 + cost + g.extra(up)})
					continue
				}
			}
		}
		// subir saltando (también en diagonal): un bloque, o dos con un efecto de salto
		climbed := false
		for dy := 1; float64(dy) <= g.jump-0.2; dy++ {
			up := world.BlockPos{X: p.X + d[0], Y: p.Y + dy, Z: p.Z + d[1]}
			if f, ok := g.floor(up); ok && f-f0 > maxStep && f-f0 <= g.jump && f <= reach &&
				g.clear(p.X, p.Z, f0, f+height) && sides(f0, f+height) {
				out = append(out, edge{Node{Pos: up, Floor: f, Kind: path.Ascend}, base + 0.6*float64(dy) + g.extra(up)})
				climbed = true
				break
			}
		}
		if climbed {
			continue
		}
		// suelo en la misma celda pero más alto de lo que se anda: se salta
		if f, ok := g.floor(level); ok {
			if f-f0 <= g.jump && f <= reach && !diag && g.clear(p.X, p.Z, f0, f+height) {
				out = append(out, edge{Node{Pos: level, Floor: f, Kind: path.Ascend}, base + 0.6 + g.extra(level)})
			}
			continue
		}
		// el cuerpo tiene que poder pasar por encima del borde
		if !g.clear(level.X, level.Z, f0, f0+height) || !sides(f0, f0+height) {
			continue
		}
		// puente: poner un bloque bajo la celda de al lado (en recto, desde un bloque entero)
		if g.build && !g.noBridge && !diag && onTop {
			under := world.BlockPos{X: level.X, Y: p.Y - 1, Z: level.Z}
			if g.replaceable(under) {
				out = append(out, edge{Node{Pos: level, Floor: f0, Kind: Bridge}, bridgeCost + g.extra(level)})
			}
		}
		// bajar: caer hasta el primer suelo (3 bloques, o más sobre agua)
		for k := 1; k <= waterFall; k++ {
			down := world.BlockPos{X: level.X, Y: p.Y - k, Z: level.Z}
			if f, ok := g.floor(down); ok {
				safe := safeFall
				if g.maxFall > 0 {
					safe = g.maxFall
				}
				switch {
				case k <= safe || g.water(down):
					out = append(out, edge{Node{Pos: down, Floor: f, Kind: path.Descend}, base + 0.25*float64(k) + g.extra(down)})
				case float64(k-safeFall) <= g.fallBudget:
					// más alto que lo que no duele: la caída quita k-3 de vida;
					// si la vida lo aguanta con margen, se baja (caro: solo si no hay otra)
					out = append(out, edge{Node{Pos: down, Floor: f, Kind: path.Descend}, base + 0.25*float64(k) + 6*float64(k-safeFall) + g.extra(down)})
				}
				break
			}
			if !g.clear(down.X, down.Z, float64(down.Y), float64(down.Y)+1) {
				break // pared sin suelo encima
			}
		}
		// saltar huecos corriendo: 1-3 bloques al mismo nivel, 1-2 subiendo/bajando
		out = append(out, g.gapJumps(n, d, diag)...)
	}
	// torre: saltar y poner un bloque debajo (si la meta está más arriba)
	if g.build && p.Y < g.goalY && onTop && g.replaceable(p) {
		up := world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}
		if g.clear(p.X, p.Z, f0, f0+1+height) {
			out = append(out, edge{Node{Pos: up, Floor: float64(up.Y), Kind: Pillar}, pillarCost})
		} else if extra, ok := g.capCost(p, f0); ok {
			out = append(out, edge{Node{Pos: up, Floor: float64(up.Y), Kind: Pillar}, pillarCost + extra})
		}
	}
	// escaleras de mano y agua: arriba y abajo
	if g.climbable(p) || inWater {
		kind := path.Climb
		if inWater {
			kind = path.Swim
		}
		above := world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}
		if f, ok := g.floor(above); ok {
			out = append(out, edge{Node{Pos: above, Floor: f, Kind: kind}, 1.5})
		}
		below := world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z}
		if f, ok := g.floor(below); ok && (g.climbable(below) || g.water(below)) {
			out = append(out, edge{Node{Pos: below, Floor: f, Kind: kind}, 1.5})
		}
	}
	return g.noFire(n.Pos, out)
}

// noFire quita los movimientos que meten el cuerpo en fuego (a la celda, o
// rozando las de los lados en diagonal): cruzarlo quema y, en 20 ticks dentro,
// prende al jugador. El reflejo de la lava saca al bot de él; si el camino
// volviera a meterlo, se pelearían.
func (g *graph) noFire(from world.BlockPos, es []edge) []edge {
	burns := func(p world.BlockPos) bool {
		return g.fireAt(p) || g.fireAt(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z})
	}
	out := es[:0]
	for _, e := range es {
		p := e.to.Pos
		if burns(p) {
			continue
		}
		if dx, dz := p.X-from.X, p.Z-from.Z; dx != 0 && dz != 0 && (burns(world.BlockPos{X: from.X + dx, Y: p.Y, Z: from.Z}) || burns(world.BlockPos{X: from.X, Y: p.Y, Z: from.Z + dz})) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// gapJumps son los saltos corriendo sobre un hueco en la dirección d.
func (g *graph) gapJumps(n Node, d [2]int, diag bool) []edge {
	p, f0 := n.Pos, n.Floor
	if g.swimming(p) || g.climbable(p) {
		return nil // nadando (o flotando) no hay de dónde saltar
	}
	maxGap := 3
	if diag {
		maxGap = 1
	}
	// cabeza libre en el despegue para el salto
	if !g.clear(p.X, p.Z, f0, f0+height+g.jump) {
		return nil
	}
	var out []edge
	for gap := 1; gap <= maxGap; gap++ {
		mid := world.BlockPos{X: p.X + gap*d[0], Y: p.Y, Z: p.Z + gap*d[1]}
		if _, ok := g.floor(mid); ok {
			return out // no es un hueco: se anda
		}
		if !g.clear(mid.X, mid.Z, f0, f0+height+g.jump) {
			return out // algo en medio del salto
		}
		// un salto que sale mal es caer por el hueco: no si esa caída mata (o, con poca vida, si duele)
		if fall := g.holeDepth(mid) - safeFall; fall > 0 && (float64(fall) >= g.health-2 || g.health < 10) {
			return out
		}
		if diag && (!g.clear(p.X+d[0], p.Z, f0, f0+height+g.jump) || !g.clear(p.X, p.Z+d[1], f0, f0+height+g.jump)) {
			return out
		}
		for _, dy := range []int{1, 0, -1, -2} {
			if dy == 1 && gap > 1 || dy < 0 && gap > 3 {
				continue
			}
			// un salto de 3 necesita carrerilla: una celda llana detrás
			if gap == 3 && dy >= 0 {
				back := world.BlockPos{X: p.X - d[0], Y: p.Y, Z: p.Z - d[1]}
				if f, ok := g.floor(back); !ok || math.Abs(f-f0) > 1e-6 {
					continue
				}
			}
			land := world.BlockPos{X: p.X + (gap+1)*d[0], Y: p.Y + dy, Z: p.Z + (gap+1)*d[1]}
			f, ok := g.floor(land)
			if !ok || f-f0 > g.jump || f-f0 < float64(dy)-0.5 || g.water(land) && dy > 0 {
				continue
			}
			if !g.clear(land.X, land.Z, math.Max(f, f0), math.Max(f, f0)+height) {
				continue
			}
			cost := 2.5 + 1.5*float64(gap*gap)
			if diag {
				cost += 1
			}
			out = append(out, edge{Node{Pos: land, Floor: f, Kind: path.GapJump}, cost + g.extra(land)})
		}
	}
	return out
}

// solidTop: se está de pie en p sobre la cara de arriba de un bloque entero
// (contra su lateral se pone un puente; encima de él, una torre).
func (g *graph) solidTop(p world.BlockPos, f float64) bool {
	if f != float64(p.Y) || g.water(p) || g.climbable(p) {
		return false
	}
	s, ok := g.state(world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z})
	if !ok {
		return false
	}
	for _, b := range block.CollisionShape(s) {
		if b.MinX <= 1e-9 && b.MinZ <= 1e-9 && b.MaxX >= 1-1e-9 && b.MaxZ >= 1-1e-9 && b.MaxY >= 1-1e-9 && b.MinY <= 1e-9 {
			return true // un bloque entero
		}
	}
	return false
}

// replaceable: se puede poner un bloque en p (aire, agua, hierba alta…).
func (g *graph) replaceable(p world.BlockPos) bool {
	s, ok := g.state(p)
	return ok && block.Replaceable(s) && !g.climbable(p)
}

// extra es lo que cuesta de más estar en p: agua, cerca de peligros.
func (g *graph) extra(p world.BlockPos) float64 {
	if c, ok := g.extraMemo[p]; ok {
		return c
	}
	c := g.extraAt(p)
	if g.extraMemo != nil {
		g.extraMemo[p] = c
	}
	return c
}

func (g *graph) extraAt(p world.BlockPos) float64 {
	c := 0.0
	if g.water(p) {
		c += 1.5
		if g.water(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}) { // la cabeza bajo el agua: no respira
			c += 3
			if !g.airNear(p) { // y sin aire cerca (una cueva inundada, un túnel largo): se ahoga
				c += 25
			}
		}
	}
	for _, d := range dirs {
		q := world.BlockPos{X: p.X + d[0], Y: p.Y, Z: p.Z + d[1]}
		if g.deadly(q) || g.deadly(world.BlockPos{X: q.X, Y: q.Y - 1, Z: q.Z}) {
			c += 2
			break
		}
	}
	// fuego al lado (el cuerpo roza la celda si no va centrado)
	for _, d := range dirs[:4] {
		if g.fireAt(world.BlockPos{X: p.X + d[0], Y: p.Y, Z: p.Z + d[1]}) || g.fireAt(world.BlockPos{X: p.X + d[0], Y: p.Y + 1, Z: p.Z + d[1]}) {
			c += 8
			break
		}
	}
	// lava a la altura del cuerpo al lado (o encima de la cabeza): la que corre
	// llega en un momento; se pasa por ahí solo si no hay otro camino
	if g.lavaAt(world.BlockPos{X: p.X, Y: p.Y + 2, Z: p.Z}) {
		c += 30
	} else {
		for _, d := range dirs[:4] {
			if g.lavaAt(world.BlockPos{X: p.X + d[0], Y: p.Y, Z: p.Z + d[1]}) || g.lavaAt(world.BlockPos{X: p.X + d[0], Y: p.Y + 1, Z: p.Z + d[1]}) {
				c += 30
				break
			}
		}
	}
	return c
}

// Soft: lo que se rompe para pasar sin pensarlo (al momento): hojas
// naturales, enredaderas, azaleas.
func Soft(name string) bool {
	if strings.Contains(name, "persistent=true") {
		return false // hojas que puso un jugador (un seto, una casa): no se tocan
	}
	n := strings.TrimPrefix(name, "minecraft:")
	if i := strings.IndexByte(n, '['); i >= 0 {
		n = n[:i]
	}
	return strings.HasSuffix(n, "_leaves") || strings.HasSuffix(n, "vine") || strings.HasSuffix(n, "vines") ||
		n == "azalea" || n == "flowering_azalea" // (no telarañas: a mano tardan 20 s)
}

// breakable: a la celda p (al nivel del suelo f0) se llega rompiendo lo blando
// que tapa los pies o la cabeza: suelo entero debajo, y en las dos celdas
// nada más que aire o cosas blandas (al menos una). Devuelve lo que cuesta.
func (g *graph) breakable(p world.BlockPos, f0 float64) (float64, bool) {
	if math.Abs(float64(p.Y)-f0) > 1e-6 || g.deadly(p) || g.avoid[p] {
		return 0, false
	}
	below := world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z}
	bs, ok := g.state(below)
	if !ok || g.deadly(below) {
		return 0, false
	}
	full := false
	for _, b := range block.CollisionShape(bs) {
		if b.MaxY >= 1-1e-9 {
			full = true
		}
	}
	if !full {
		return 0, false
	}
	soft := 0
	for dy := 0; dy <= 1; dy++ {
		c := world.BlockPos{X: p.X, Y: p.Y + dy, Z: p.Z}
		st, ok := g.state(c)
		if !ok {
			return 0, false
		}
		if len(block.CollisionShape(st)) == 0 && !strings.HasPrefix(g.name(c), "minecraft:lava") && !strings.HasPrefix(g.name(c), "minecraft:water") {
			continue
		}
		if !Soft(g.name(c)) && !(g.passable != nil && g.passable(c)) {
			return 0, false
		}
		soft++
	}
	if soft == 0 {
		return 0, false
	}
	return 1.0 * float64(soft), true
}

func (g *graph) fireAt(p world.BlockPos) bool { return g.is(p, flagFire) }

func (g *graph) lavaAt(p world.BlockPos) bool { return g.is(p, flagLava) }

// start es el nodo donde está un jugador en (x, y, z).
func (g *graph) start(x, y, z float64) (Node, bool) { return g.kit.Start(x, y, z) }

// heurística: distancia octil en horizontal + lo que cuesta subir.
func heuristic(a, b world.BlockPos) float64 {
	dx, dz := math.Abs(float64(a.X-b.X)), math.Abs(float64(a.Z-b.Z))
	oct := math.Max(dx, dz) + (math.Sqrt2-1)*math.Min(dx, dz)
	if dy := float64(b.Y - a.Y); dy > 0 {
		return oct + 0.6*dy // cada bloque hacia arriba es un salto
	}
	return oct // bajar casi no cuesta
}

type item struct {
	pos world.BlockPos
	f   float64
	idx int
}

type pq []*item

func (q pq) Len() int                    { return len(q) }
func (q pq) Less(i, j int) bool          { return q[i].f < q[j].f }
func (q pq) Swap(i, j int)               { q[i], q[j] = q[j], q[i]; q[i].idx, q[j].idx = i, j }
func (q *pq) Push(x any)                 { it := x.(*item); it.idx = len(*q); *q = append(*q, it) }
func (q *pq) Pop() any                   { old := *q; it := old[len(old)-1]; *q = old[:len(old)-1]; return it }
func (q pq) top() *item                  { return q[0] }
func (q *pq) update(it *item, f float64) { it.f = f; heap.Fix(q, it.idx) }

type rec struct {
	node   Node
	g      float64
	parent world.BlockPos
	root   bool
	it     *item
	closed bool
}

// result de una búsqueda: el camino (sin el nodo de salida) y si llega a la meta.
type result struct {
	path     []Node
	complete bool
	searched int  // nodos mirados
	timedOut bool // se acabó el tiempo (budget) antes que los nodos
}

// find busca con A* (ponderado 1.3: algo menos óptimo, mucho más rápido) de
// start a goal. near es a qué distancia de la meta vale (0: la celda exacta).
// Si no llega, devuelve el camino al sitio más cercano a la meta que encontró.
func (g *graph) find(ctx context.Context, start Node, goal world.BlockPos, near float64, maxNodes int) (result, error) {
	g.memo = map[world.BlockPos]floorMemo{}
	g.extraMemo = map[world.BlockPos]float64{}
	g.goalY = goal.Y
	defer func() { g.memo, g.extraMemo = nil, nil }()
	const weight = 1.3
	reached := func(p world.BlockPos) bool {
		if p == goal {
			return true
		}
		dx, dy, dz := float64(p.X-goal.X), float64(p.Y-goal.Y), float64(p.Z-goal.Z)
		return near > 0 && dx*dx+dz*dz <= near*near && math.Abs(dy) <= 1
	}
	seen := map[world.BlockPos]*rec{}
	open := &pq{}
	r0 := &rec{node: start, root: true}
	r0.it = &item{pos: start.Pos, f: weight * heuristic(start.Pos, goal)}
	seen[start.Pos] = r0
	heap.Push(open, r0.it)
	best, bestH := r0, heuristic(start.Pos, goal)
	n := 0
	t0, timedOut := time.Now(), false
	for open.Len() > 0 {
		if n++; n&255 == 0 {
			if ctx.Err() != nil {
				return result{}, ctx.Err()
			}
			// sin tiempo: lo mejor encontrado hasta ahora (más vale acercarse
			// que quedarse quieto esperando una búsqueda que no acaba)
			if g.budget > 0 && time.Since(t0) > g.budget {
				timedOut = true
				break
			}
		}
		it := heap.Pop(open).(*item)
		r := seen[it.pos]
		if r.closed {
			continue
		}
		r.closed = true
		if h := heuristic(r.node.Pos, goal); h < bestH {
			best, bestH = r, h
		}
		if reached(r.node.Pos) {
			return result{path: trace(seen, r), complete: true, searched: n}, nil
		}
		if n > maxNodes {
			break
		}
		for _, e := range g.neighbours(r.node) {
			if g.avoid[e.to.Pos] {
				continue
			}
			cost := r.g + e.cost + g.penalty[e.to.Pos]
			if e.to.Kind == path.Ascend || e.to.Kind == path.GapJump {
				cost += g.climbPenalty
			}
			q, ok := seen[e.to.Pos]
			if ok && (q.closed || cost >= q.g) {
				continue
			}
			f := cost + weight*heuristic(e.to.Pos, goal)
			if !ok {
				q = &rec{node: e.to, g: cost, parent: r.node.Pos}
				q.it = &item{pos: e.to.Pos, f: f}
				seen[e.to.Pos] = q
				heap.Push(open, q.it)
				continue
			}
			q.node, q.g, q.parent = e.to, cost, r.node.Pos
			open.update(q.it, f)
		}
	}
	if best.root {
		if timedOut {
			return result{searched: n, timedOut: true}, nil // sin tiempo: no se sabe aún
		}
		return result{}, ErrNoPath
	}
	return result{path: trace(seen, best), searched: n, timedOut: timedOut}, nil
}

func trace(seen map[world.BlockPos]*rec, r *rec) []Node {
	var out []Node
	for !r.root {
		out = append(out, r.node)
		r = seen[r.parent]
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// MaxCapTicks: un techo sobre la torre que tarda más que esto en romperse
// (2 s) no se rompe: se sube por otra columna.
const MaxCapTicks = 40

// capCost: subir en torre desde p con un bloque justo encima de la cabeza
// (un techo: alguien lo puso para que no suba) que se rompe enseguida con lo
// que lleva. Lo que cuesta romperlo (en "bloques andados": unos 4 ticks cada
// uno). No los que caen (arena, grava: rompiendo se le caen encima) ni los
// peligrosos.
func (g *graph) capCost(p world.BlockPos, f0 float64) (float64, bool) {
	if g.digTicks == nil || !g.clear(p.X, p.Z, f0, f0+height) {
		return 0, false
	}
	c := world.BlockPos{X: p.X, Y: p.Y + 2, Z: p.Z}
	st, ok := g.state(c)
	if !ok || g.deadly(c) || g.avoid[c] {
		return 0, false
	}
	n := g.name(c)
	if strings.Contains(n, "sand") || strings.Contains(n, "gravel") || strings.Contains(n, "powder") || strings.Contains(n, "anvil") {
		return 0, false
	}
	if len(block.CollisionShape(st)) == 0 {
		return 0, false // (lo que tapa no es este bloque)
	}
	t, ok := g.digMemo[st]
	if !ok {
		t = g.digTicks(st)
		if g.digMemo == nil {
			g.digMemo = map[block.StateID]int{}
		}
		g.digMemo[st] = t
	}
	if t < 0 || t > MaxCapTicks {
		return 0, false
	}
	return 2 + float64(t)/4, true
}
