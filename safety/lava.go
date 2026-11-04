// Package safety son los reflejos que mandan sobre todo lo demás (la pelea,
// el pathfinding, las tareas): se miran en cada tick, antes que nada.
package safety

import (
	"math"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// Lava es el reflejo de la lava. En cada tick mira las celdas del cuerpo y
// las de alrededor:
//
//   - dentro de lava: sale ya (salto, que en lava es nadar hacia arriba, y
//     hacia la última celda segura por la que pasó: por ahí vino, se puede
//     volver);
//   - lava al lado (en una celda que toca el cuerpo, o encima de la cabeza):
//     se aparta antes de que la lava llegue, deshaciendo el camino hasta un
//     sitio sin lava a 2 bloques.
//
// Mientras escapa manda él: ni la pelea ni el walker tocan las teclas, y al
// empezar para el walker (su camino ya no vale).
type Lava struct {
	World *world.World
	Mover *physics.Mover
	// Stop para lo que mueve al bot (el walker): se llama en otra goroutine.
	Stop func()
	Logf func(format string, args ...any)
	// Extinguish apaga el fuego de p (pegarle: se rompe al instante, como hace un
	// jugador). nil: sin manos, solo se huye.
	Extinguish func(p world.BlockPos)

	mu       sync.Mutex
	trail    []world.BlockPos // las últimas celdas seguras por las que pasó (la última, la más reciente)
	escaping bool
	inLava   bool
	target   world.BlockPos
	ticks    int // ticks escapando (para no quedarse colgado)
	putting  int // ticks apagando el fuego en el que está
	escapes  atomic.Int64
}

const trailLen = 40

// Escaping dice si está escapando de la lava (las tareas esperan).
func (l *Lava) Escaping() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.escaping
}

// Escapes son las veces que tuvo que escapar (las tareas ven si cambió).
func (l *Lava) Escapes() int64 { return l.escapes.Load() }

func (l *Lava) name(p world.BlockPos) string {
	s, ok := l.World.BlockAt(p)
	if !ok {
		return ""
	}
	return world.StateString(s)
}

// IsLava: lava (fuente o corriendo) en p.
func (l *Lava) IsLava(p world.BlockPos) bool { return strings.HasPrefix(l.name(p), "minecraft:lava") }

func (l *Lava) burns(p world.BlockPos) bool {
	n := l.name(p)
	for _, c := range []string{"minecraft:lava", "minecraft:fire", "minecraft:soul_fire", "minecraft:magma_block", "minecraft:campfire", "minecraft:soul_campfire"} {
		if strings.HasPrefix(n, c) {
			return true
		}
	}
	return false
}

func (l *Lava) collides(p world.BlockPos) bool {
	s, ok := l.World.BlockAt(p)
	return !ok || len(block.CollisionShape(s)) > 0
}

// bodyCells son las celdas que toca la caja del jugador (0.6 x 1.8).
func bodyCells(pos basic.Position) []world.BlockPos {
	var out []world.BlockPos
	for x := int(math.Floor(pos.X - 0.3)); x <= int(math.Floor(pos.X+0.3)); x++ {
		for z := int(math.Floor(pos.Z - 0.3)); z <= int(math.Floor(pos.Z+0.3)); z++ {
			for y := int(math.Floor(pos.Y)); y <= int(math.Floor(pos.Y+1.79)); y++ {
				out = append(out, world.BlockPos{X: x, Y: y, Z: z})
			}
		}
	}
	return out
}

// lavaNear: hay lava que puede llegar al cuerpo: encima de la cabeza (cae),
// o al lado y con fuerza para extenderse a la celda del cuerpo (no el borde
// quieto de un charco: la lava baja de nivel al correr y la del borde ya no
// avanza; junto a ella se puede estar).
func (l *Lava) lavaNear(pos basic.Position) bool {
	cells := bodyCells(pos)
	top := cells[0].Y
	for _, c := range cells {
		top = max(top, c.Y)
	}
	for _, c := range cells {
		if c.Y == top && l.IsLava(world.BlockPos{X: c.X, Y: c.Y + 1, Z: c.Z}) {
			return true // encima de la cabeza
		}
		for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			if l.spreads(world.BlockPos{X: c.X + d[0], Y: c.Y, Z: c.Z + d[1]}) {
				return true
			}
		}
	}
	return false
}

// spreads: en p hay lava que todavía corre a los lados (una fuente o una que
// no es la última de su corriente; la que cae en columna no se extiende).
func (l *Lava) spreads(p world.BlockPos) bool {
	n := l.name(p)
	if !strings.HasPrefix(n, "minecraft:lava") {
		return false
	}
	i := strings.Index(n, "level=")
	if i < 0 {
		return true
	}
	lv := 0
	for _, ch := range n[i+6:] {
		if ch < '0' || ch > '9' {
			break
		}
		lv = lv*10 + int(ch-'0')
	}
	// 0 fuente; 1-7 corriendo (más alto, más débil); 8+ cayendo. En el mundo
	// normal baja de 2 en 2: la de nivel 6 ya no avanza (en el Nether, la 7)
	return lv < 6
}

// lavaWithin: hay lava a r bloques (o menos) de la celda p, a la altura del cuerpo o encima.
func (l *Lava) lavaWithin(p world.BlockPos, r int) bool {
	for dx := -r; dx <= r; dx++ {
		for dz := -r; dz <= r; dz++ {
			for dy := 0; dy <= 2; dy++ {
				if l.IsLava(world.BlockPos{X: p.X + dx, Y: p.Y + dy, Z: p.Z + dz}) {
					return true
				}
			}
		}
	}
	return false
}

// standable: se puede estar de pie en p sin quemarse: suelo firme que no
// quema debajo, dos celdas libres (sin lava ni fuego).
func (l *Lava) standable(p world.BlockPos) bool {
	below := world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z}
	head := world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}
	return l.collides(below) && !l.burns(below) && !l.collides(p) && !l.collides(head) &&
		!l.burns(p) && !l.burns(head)
}

// Think es el reflejo de cada tick: true si tomó el control (escapando).
func (l *Lava) Think(s *control.State, bp *basic.Player) bool {
	pos := bp.Position()
	feet := world.BlockPos{X: int(math.Floor(pos.X)), Y: int(math.Floor(pos.Y + 1e-6)), Z: int(math.Floor(pos.Z))}
	inLava := false
	if l.Mover != nil {
		if p := l.Mover.Player(); p != nil { // (nil antes del primer paso de la física)
			inLava = p.InLava
		}
	}
	for _, c := range bodyCells(pos) {
		if l.IsLava(c) {
			inLava = true
		}
	}
	// en fuego (el de una explosión, el del suelo): fuera antes de 20 ticks, que es cuando prende al jugador
	inFire := false
	for _, c := range bodyCells(pos) {
		if n := l.name(c); strings.HasPrefix(n, "minecraft:fire") || strings.HasPrefix(n, "minecraft:soul_fire") {
			inFire = true
		}
	}
	near := inLava || inFire || l.lavaNear(pos)

	l.mu.Lock()
	defer l.mu.Unlock()
	// en fuego (y no en lava): apagarlo a golpes es lo más rápido (un tick por
	// llama); si en 12 ticks sigue dentro, a huir
	if !inFire {
		l.putting = 0
	}
	if inFire && !inLava && l.Extinguish != nil && l.putting < 12 {
		if l.putting == 0 {
			l.logf("¡fuego! lo apago")
			l.escapes.Add(1)
		}
		if l.putting%3 == 0 {
			for _, c := range bodyCells(pos) {
				if n := l.name(c); strings.HasPrefix(n, "minecraft:fire") || strings.HasPrefix(n, "minecraft:soul_fire") {
					l.Extinguish(c)
					break // una a la vez: las manos hacen una cosa
				}
			}
		}
		l.putting++
		l.escaping = true
		s.Input = control.Input{}
		return true
	}
	if !near {
		if s.OnGround && l.standable(feet) && !l.lavaWithin(feet, 1) {
			if n := len(l.trail); n == 0 || l.trail[n-1] != feet {
				l.trail = append(l.trail, feet)
				if len(l.trail) > trailLen {
					l.trail = l.trail[1:]
				}
			}
		}
		if l.escaping && s.OnGround {
			l.escaping = false
			s.Input = control.Input{}
			l.logf("fuera de peligro (en %v)", feet)
		}
		if !l.escaping {
			return false
		}
	}
	// peligro: a dónde ir
	reached := math.Hypot(float64(l.target.X)+0.5-pos.X, float64(l.target.Z)+0.5-pos.Z) < 0.12 && l.target.Y == feet.Y
	if !l.escaping || reached || l.inLava != inLava || l.ticks%10 == 0 || !l.standable(l.target) || l.lavaWithin(l.target, 0) {
		t, ok := l.pick(feet, inLava)
		if !ok {
			// sin sitio conocido: hacia arriba (nadar) y hacia donde no hay lava
			t = feet
		}
		if !l.escaping {
			l.escapes.Add(1)
			if inFire && !inLava {
				l.logf("¡estoy en fuego! salgo hacia %v", t)
			} else if inLava {
				l.logf("¡estoy en lava! salgo hacia %v", t)
			} else {
				l.logf("¡lava al lado! me aparto hacia %v", t)
			}
			if l.Stop != nil {
				go l.Stop()
			}
		}
		l.target = t
	}
	l.escaping, l.inLava = true, inLava
	l.ticks++
	if l.ticks > 20*30 { // medio minuto sin salir: que lo intente el resto (no dejar al bot quieto)
		l.ticks = 0
		l.escaping = false
		return false
	}
	// a la celda objetivo: mirar a ella, adelante; saltar si está en lava (nada
	// hacia arriba), si choca con algo (escalón) o si la celda está más alta
	tx, tz := float64(l.target.X)+0.5, float64(l.target.Z)+0.5
	dist := math.Hypot(tx-pos.X, tz-pos.Z)
	act.LookAt(bp, false, tx, float64(l.target.Y)+1.6, tz)
	in := control.Input{Jump: inLava || s.HorizontalCollision || l.target.Y > feet.Y}
	if dist > 0.1 {
		in.Forward = true
		in.Sprint = !inLava && dist > 1.5
		in.Shift = !inLava && dist < 0.6 // despacio al final: centrado en la celda (el cuerpo no roza el fuego de al lado)
	}
	s.Input = in
	return true
}

// pick elige a dónde escapar: deshaciendo el camino (la celda segura más
// reciente que no tenga lava a menos de 2), o la celda de pie más cercana sin
// lava cerca en 4 bloques a la redonda.
func (l *Lava) pick(feet world.BlockPos, inLava bool) (world.BlockPos, bool) {
	clear := 2
	for r := clear; r >= 1; r-- {
		for i := len(l.trail) - 1; i >= 0; i-- {
			p := l.trail[i]
			if abs(p.X-feet.X)+abs(p.Z-feet.Z) > 12 || abs(p.Y-feet.Y) > 3 {
				continue
			}
			if l.standable(p) && !l.lavaWithin(p, r) {
				// por el camino: la siguiente celda del rastro hacia ella (no en línea recta por la lava)
				return l.step(i, feet), true
			}
		}
	}
	best, bestD := world.BlockPos{}, math.Inf(1)
	for dx := -4; dx <= 4; dx++ {
		for dz := -4; dz <= 4; dz++ {
			for dy := -1; dy <= 2; dy++ {
				p := world.BlockPos{X: feet.X + dx, Y: feet.Y + dy, Z: feet.Z + dz}
				if !l.standable(p) || l.lavaWithin(p, 1) {
					continue
				}
				d := math.Hypot(float64(dx), float64(dz)) + 0.5*math.Abs(float64(dy))
				if d < bestD {
					best, bestD = p, d
				}
			}
		}
	}
	return best, !math.IsInf(bestD, 1)
}

// step: de las celdas del rastro entre la actual y la i-ésima, la primera que
// no sea la de los pies (se deshace el camino celda a celda).
func (l *Lava) step(i int, feet world.BlockPos) world.BlockPos {
	for k := len(l.trail) - 1; k > i; k-- {
		p := l.trail[k]
		if p != feet && abs(p.X-feet.X) <= 1 && abs(p.Z-feet.Z) <= 1 && l.standable(p) {
			return p
		}
	}
	return l.trail[i]
}

func (l *Lava) logf(format string, args ...any) {
	if l.Logf != nil {
		l.Logf(format, args...)
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
