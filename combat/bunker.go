package combat

// El búnker: contra un jugador, para curarse sin que le peguen. Correr no
// sirve (corre igual) y comer al descubierto es comerse dos golpes; con
// bloques, el bot se encierra en su celda: dos de alto a los cuatro lados (lo
// primero, el lado del rival) y, si hay bloques, techo (si no, sube y pega
// desde arriba). Un jugador no pega a través de un bloque (su clic da en el
// bloque): para llegar tiene que romperlos, y eso es tiempo (con pico, medio
// segundo cada uno; con la espada, mucho más). Dentro, come (las doradas
// primero), bebe, y repone el bloque que le rompan.
//
// Se sale curado (rompiendo la pared del lado del rival: a pelear), o cuando el
// rival se va; si lo que hay es un rival que no se le gana (stanceAvoid), se
// queda dentro hasta que se canse (3 minutos como mucho). Si entra (rompió la
// pared y está pegado), se pelea.

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// Builder pone bloques (pathing.HandsBuilder).
type Builder interface {
	Blocks() int
	Place(pos, against world.BlockPos) bool
	// PlaceNow pone sin esperar la respuesta del servidor (el bloque, predicho)
	PlaceNow(pos, against world.BlockPos) bool
	Busy() bool
}

const (
	bunkerMinBlocks = 2    // la pared del lado del rival
	bunkerMaxSteps  = 3600 // 3 minutos dentro como mucho
)

type bunkerState struct {
	on        bool
	feet      world.BlockPos
	since     uint64
	enemy     int32
	opening   []world.BlockPos // saliendo: lo que queda por romper
	logged    bool             // dijo ya que está cerrado
	placed    int              // bloques puestos (con los repuestos)
	lastPlace uint64           // el paso del último
	jump      bool             // saltar para poner (la cara de arriba de una pared)
	// la salida (exit.go no: aquí): por qué lado, intentos y qué hacer al abrir
	side      int    // el lado que se abre (0 el del rival … 3 el de atrás)
	tries     int    // golpes de pico seguidos al mismo bloque sin romperlo
	sides     int    // lados probados
	thenPearl bool   // abierto, perla lejos de él
	thenFight bool   // abierto, a por él (aunque no haya pegado: venía armado a por el bot)
	planned   string // el plan que dijo (para no repetirlo)
}

// cell es un bloque del búnker y contra qué se pone.
type cell struct{ pos, against world.BlockPos }

// bunkerCells: las celdas del búnker alrededor de feet, el lado del rival
// (hacia ex, ez) primero: pies y cabeza de cada lado, luego el techo (un
// bloque sobre la pared del lado del rival, y contra él el de encima de la cabeza).
func bunkerCells(feet world.BlockPos, ex, ez float64) []cell {
	type side struct {
		d   [2]int
		dot float64
	}
	cx, cz := float64(feet.X)+0.5, float64(feet.Z)+0.5
	dx, dz := ex-cx, ez-cz
	n := math.Max(math.Hypot(dx, dz), 1e-6)
	sides := []side{{d: [2]int{1, 0}}, {d: [2]int{-1, 0}}, {d: [2]int{0, 1}}, {d: [2]int{0, -1}}}
	for i := range sides {
		sides[i].dot = (float64(sides[i].d[0])*dx + float64(sides[i].d[1])*dz) / n
	}
	sort.SliceStable(sides, func(i, j int) bool { return sides[i].dot > sides[j].dot })
	var out []cell
	for _, s := range sides {
		lo := world.BlockPos{X: feet.X + s.d[0], Y: feet.Y, Z: feet.Z + s.d[1]}
		hi := world.BlockPos{X: lo.X, Y: lo.Y + 1, Z: lo.Z}
		out = append(out, cell{lo, world.BlockPos{X: lo.X, Y: lo.Y - 1, Z: lo.Z}}, cell{hi, lo})
	}
	// el techo: antes un bloque sobre la pared del lado del rival y contra
	// él, el de encima de la cabeza. Ese primero se pone saltando: la cara de
	// arriba de la pared está por encima de los ojos (de pie no se ve: el
	// anticheat no lo deja)
	wall := out[1].pos
	cap := world.BlockPos{X: wall.X, Y: wall.Y + 1, Z: wall.Z}
	roof := world.BlockPos{X: feet.X, Y: feet.Y + 2, Z: feet.Z}
	return append(out, cell{cap, wall}, cell{roof, cap})
}

// buildOrder: en qué orden se ponen: la pared del rival, el techo (sin él,
// saltando junto a la pared le ve por encima y le pega en la cabeza), la de
// atrás (la de su empujón: sin ella, un golpe por un lado le saca del
// búnker), y los dos lados.
func buildOrder(c []cell) []cell {
	return []cell{c[0], c[1], c[8], c[9], c[6], c[7], c[2], c[3], c[4], c[5]}
}

func (f *Fighter) solid(p world.BlockPos) bool {
	s, ok := f.d.World.BlockAt(p)
	return ok && len(block.CollisionShape(s)) > 0
}

func (f *Fighter) replaceable(p world.BlockPos) bool {
	s, ok := f.d.World.BlockAt(p)
	return ok && block.Replaceable(s)
}

// occupied: alguien (un enemigo) está en la celda (ahí no se pone el bloque).
func (f *Fighter) occupied(c world.BlockPos, foes []foe) bool {
	for _, fo := range foes {
		e := fo.e
		const in = 0.02 // (tocando el borde no es estar dentro)
		if e.X+0.3-in > float64(c.X) && e.X-0.3+in < float64(c.X+1) && e.Z+0.3-in > float64(c.Z) && e.Z-0.3+in < float64(c.Z+1) &&
			e.Y < float64(c.Y+1) && e.Y+1.8 > float64(c.Y) {
			return true
		}
	}
	return false
}

// walls cuenta cuántas celdas del búnker (las 8 de pared) están puestas, y si
// las dos del lado del rival lo están.
func (f *Fighter) walls(cells []cell) (n int, front bool) {
	for i, c := range cells[:8] {
		if f.solid(c.pos) {
			n++
		}
		if i == 1 {
			front = n == 2
		}
	}
	return
}

// wantBunker: encerrarse aquí vale la pena (un jugador, bloques, de pie, sin
// tenerlo encima).
func (f *Fighter) wantBunker(t *foe) bool {
	if f.d.Builder == nil || t == nil || t.e.Type != "minecraft:player" || f.d.Builder.Blocks() < bunkerMinBlocks {
		return false
	}
	// (con él a menos de 5 y de cara, la pared de su lado no da tiempo: le
	// pega y le saca antes de cerrarlo; en los logs, todos los búnkeres
	// empezados con él a 3-4 acabaron en "me movieron", a pelear con medio
	// búnker y sin haber comido)
	near := 3.0
	if facing(t.e, f.d.Player.Position()) && !eatingNow(t.e) {
		near = 5
	}
	if t.dist < near || t.dist > 14 || f.steps < f.bunkerRetry || !f.d.Ctl.State().OnGround {
		return false
	}
	// bloques para el búnker entero (paredes y techo): a medias no para nada
	if need := f.bunkerNeed(t); f.d.Builder.Blocks() < need {
		return false
	}
	if p := f.d.Mover.Player(); p != nil && (p.InWater || p.InLava) {
		return false
	}
	// ardiendo: primero apagarse (el reflejo con el cubo, o el agua de al lado)
	if f.burningFixable() {
		return false
	}
	return true
}

// burningFixable: arde y tiene cómo apagarse: un cubo de agua (el reflejo)
// o agua cerca (toWater). Encerrarse así es quemarse dentro.
func (f *Fighter) burningFixable() bool {
	if f.Burning == nil || !f.Burning() {
		return false
	}
	if _, res := f.d.Player.Status().Effects["minecraft:fire_resistance"]; res {
		return false
	}
	if f.hasItem("minecraft:water_bucket") {
		return true
	}
	_, ok := f.nearWater()
	return ok
}

// quitBunker deja el búnker a medias (y no se vuelve a intentar en 5 s).
func (f *Fighter) quitBunker() {
	f.bk = bunkerState{}
	f.bunkerRetry = f.steps + 100
	f.setPlan(plan{})
}

// startBunker empieza a encerrarse donde está.
// bunkerNeed: los bloques que faltan para cerrar el búnker aquí contra t.
func (f *Fighter) bunkerNeed(t *foe) int {
	p := f.d.Player.Position()
	feet := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
	n := 0
	for _, c := range bunkerCells(feet, t.e.X, t.e.Z) {
		if !f.solid(c.pos) {
			n++
		}
	}
	return n
}

// shieldHolds: con escudo en la mano izquierda (o en el inventario) y el rival
// sin hacha: a la defensiva no le entra nada (TestOutclassedShield), así que
// esperar dentro a que se vaya (hasta 3 min) no hace falta.
func (f *Fighter) shieldHolds(t *foe) bool {
	if t == nil || holdsAxe(t.e) || f.matchID == t.id && f.match.them.axe {
		return false
	}
	return f.hasItem(itemShield)
}

func (f *Fighter) startBunker(t *foe, why string) {
	p := f.d.Player.Position()
	f.stopMoving()
	f.bk = bunkerState{on: true, since: f.steps, enemy: t.id,
		feet: world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}}
	f.logf("me encierro con bloques (%s; tengo %d)", why, f.d.Builder.Blocks())
	f.setPlan(plan{mode: sheltering, target: t.id, shelter: f.bk.feet, jump: f.bk.jump})
}

// bunkerStep lleva el búnker; dice si este paso se fue en eso.
func (f *Fighter) bunkerStep(ctx context.Context, st basic.Status, foes []foe, stance stance) bool {
	if !f.bk.on {
		return false
	}
	p := f.d.Player.Position()
	feet := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
	// (un golpe por una abertura le levanta: en el aire sigue dentro)
	if feet.X != f.bk.feet.X || feet.Z != f.bk.feet.Z || feet.Y < f.bk.feet.Y || feet.Y > f.bk.feet.Y+1 {
		f.logf("fuera del búnker (me movieron a %.2f %.2f %.2f, era %v): a pelear", p.X, p.Y, p.Z, f.bk.feet)
		f.quitBunker()
		return false
	}
	// el rival: el del búnker, o el jugador más cerca
	var t *foe
	for i := range foes {
		if foes[i].id == f.bk.enemy {
			t = &foes[i]
		}
	}
	if t == nil { // (aún no es pelea: uno que venía, en guardia)
		if e, ok := f.d.Entities.Get(f.bk.enemy); ok && !dead(e) {
			t = &foe{aggressor: aggressor{id: e.ID, name: f.label(e)}, e: e, dist: math.Sqrt(e.DistanceSqr(p.X, p.Y, p.Z))}
		}
	}
	if t == nil {
		for i := range foes {
			if foes[i].e.Type == "minecraft:player" && (t == nil || foes[i].dist < t.dist) {
				t = &foes[i]
			}
		}
	}
	// saliendo: rompiendo la pared
	if len(f.bk.opening) > 0 {
		return f.digOut(ctx)
	}
	ex, ez := p.X+1, p.Z
	if t != nil {
		ex, ez = t.e.X, t.e.Z
	}
	cells := bunkerCells(f.bk.feet, ex, ez)
	n, front := f.walls(cells)
	// ¿curarse más? con comida (o el hambre llena, que cura sola) sí; si no,
	// esperar dentro no da vida
	canHeal := len(f.foods()) > 0 || st.Food >= 18
	healed := st.Health >= 18 || !canHeal || st.Health >= 14 && len(f.foods()) == 0
	pearl := f.canEscape() && st.Health >= 7
	f.sayPlan(t, stance, healed, pearl)
	switch {
	case t == nil || t.dist > 24:
		f.logf("se fue: salgo del búnker por detrás (vida %.1f)", st.Health)
		f.openWall(cells, 3)
		return f.digOut(ctx)
	case healed && stance == stanceAvoid && pearl:
		// no se le gana, pero hay perla: se abre por detrás y se va lejos de él
		f.logf("curado (vida %.1f): abro por detrás y me voy con la perla", st.Health)
		f.openWall(cells, 3)
		f.bk.thenPearl = true
		return f.digOut(ctx)
	case !front && (f.occupied(cells[0].pos, []foe{*t}) || f.occupied(cells[1].pos, []foe{*t})):
		f.logf("está donde va la pared (a %.1f, en %.2f %.2f %.2f; pared %v %v): a pelear", t.dist, t.e.X, t.e.Y, t.e.Z, cells[0].pos, cells[1].pos)
		f.quitBunker()
		return false
	case !front && f.d.Builder.Blocks() == 0 && !f.d.Builder.Busy():
		f.logf("sin bloques y la pared abierta: a pelear")
		f.quitBunker()
		return false
	case healed && (stance != stanceAvoid || f.shieldHolds(t) || f.steps-f.bk.since > bunkerMaxSteps):
		if stance == stanceAvoid && f.shieldHolds(t) {
			f.logf("curado (vida %.1f): salgo con el escudo (a la defensiva no me entra)", st.Health)
		} else if stance == stanceAvoid {
			f.logf("%s no se va en %d s y no tengo perla: salgo a pelear (vida %.1f)", t.name, bunkerMaxSteps/20, st.Health)
		} else {
			f.logf("curado en el búnker (vida %.1f): salgo a por %s", st.Health, t.name)
		}
		// por detrás: abriendo la pared de su lado, él espera pegado y pega
		// el primero (y el pico en la mano: el golpe sin cargar); por detrás
		// tiene que dar la vuelta, y el bot le espera con la espada cargada
		f.openWall(cells, 3)
		f.bk.thenFight = true
		return f.digOut(ctx)
	}
	// el plan, al acabar el paso (con el salto si hace falta para poner)
	f.bk.jump = false
	defer func() {
		if f.bk.on && len(f.bk.opening) == 0 {
			f.setPlan(plan{mode: sheltering, target: t.id, shelter: f.bk.feet, jump: f.bk.jump})
		}
	}()
	// centrado en la celda (si no, el bloque de al lado no cabe)
	centered := math.Abs(p.X-(float64(feet.X)+0.5)) < 0.2 && math.Abs(p.Z-(float64(feet.Z)+0.5)) < 0.2
	if !centered {
		return true
	}
	// la pared del lado del rival, lo primero; luego curarse (comer es 1,6 s
	// sin poner bloques: si le rompen esa pared comiendo, se deja de comer y se
	// repone); luego el resto
	if _, _, eating := f.d.Hands.Using(); eating && f.missing(cells[:2], foes) {
		f.d.Hands.CancelEat()
	}
	if f.placeNext(cells[:2], foes) {
		return true
	}
	// con muy poca vida, a comer ya; si no, antes las cuatro paredes y el techo
	// (con un lado abierto, su golpe por la abertura le saca del búnker de un empujón)
	if (front || t.dist > 6) && st.Health < 6 && f.heal(ctx, st, ex, ez, t) {
		return true
	}
	// las cuatro paredes y el techo (sin él, saltando junto a la pared ve por
	// encima y le pega en la cabeza)
	if _, _, eating := f.d.Hands.Using(); !eating && f.placeNext(buildOrder(cells), foes) {
		return true
	}
	if (front || t.dist > 6) && f.heal(ctx, st, ex, ez, t) {
		return true
	}
	if f.placeNext(buildOrder(cells), foes) {
		return true
	}
	if n == 8 && !f.bk.logged {
		f.bk.logged = true
		roof := ""
		if f.solid(cells[9].pos) {
			roof = " y techo"
		}
		f.logf("cerrado (8 paredes%s): vida %.1f", roof, st.Health)
	}
	return true
}

// missing: falta alguna de cells y se puede poner.
func (f *Fighter) missing(cells []cell, foes []foe) bool {
	for _, c := range cells {
		if !f.solid(c.pos) && f.replaceable(c.pos) && f.solid(c.against) && !f.occupied(c.pos, foes) {
			return true
		}
	}
	return false
}

// eatHere come fd sin parar el paso (a diferencia de eat): se puede dejar a
// medias para reponer un bloque. Dice si está en ello.
func (f *Fighter) eatHere(ctx context.Context, fd food) bool {
	if _, _, eating := f.d.Hands.Using(); eating {
		return true
	}
	if f.d.Hands.Busy() || f.d.Builder.Busy() {
		return true
	}
	inv, held := f.inventory()
	name := nameOf(inv[fd.slot])
	if nameOf(inv[slotHotbar+held]) != name {
		if err := f.hold(ctx, fd.slot); err != nil {
			return false
		}
		return true // (en la mano cuando lo confirme el servidor)
	}
	f.d.Hands.Eat(func(u act.Use) {
		if u.Err == nil {
			f.logf("comí %s en el búnker (vida %.1f)", strings.TrimPrefix(name, "minecraft:"), f.d.Player.Status().Health)
		}
	})
	return true
}

// placeNext pone el primero de cells que falte; dice si está en ello.
func (f *Fighter) placeNext(cells []cell, foes []foe) bool {
	if f.d.Builder.Busy() || f.d.Hands.Busy() {
		return true
	}
	if f.d.Builder.Blocks() == 0 {
		return false
	}
	for _, c := range cells {
		if f.solid(c.pos) || !f.replaceable(c.pos) || !f.solid(c.against) || f.occupied(c.pos, foes) {
			continue
		}
		// la cara contra la que se pone tiene que verse desde los ojos: una
		// cara de arriba más alta que ellos, saltando (en lo alto del salto)
		p := f.d.Player.Position()
		if c.pos.Y >= f.bk.feet.Y+2 && c.pos.X == f.bk.feet.X && c.pos.Z == f.bk.feet.Z && (!f.d.Ctl.State().OnGround || p.Y+1.8 > float64(c.pos.Y)) {
			return true // el techo, de pie (saltando, la cabeza está en su sitio: no cabe)
		}
		if c.against.Y < c.pos.Y {
			if p.Y+1.62 < float64(c.pos.Y)+0.15 {
				f.bk.jump = true
				return true
			}
		}
		if f.steps-f.bk.lastPlace < 2 { // (un clic cada 2 ticks: lo de un jugador rápido)
			return true
		}
		if f.d.Builder.PlaceNow(c.pos, c.against) {
			f.bk.placed++
			f.bk.lastPlace = f.steps
			if f.myBlocks == nil || len(f.myBlocks) > 256 {
				f.myBlocks = map[world.BlockPos]bool{}
			}
			f.myBlocks[c.pos] = true
			f.shelterBlocks.Add(1)
		}
		return true // (o pasando el bloque a la mano: se reintenta el paso siguiente)
	}
	return false
}

// heal: dentro, curarse (dorada, poción de curación, comida). Dice si lo hizo.
func (f *Fighter) heal(ctx context.Context, st basic.Status, ex, ez float64, t *foe) bool {
	foods := f.foods()
	again := func() { f.setPlan(plan{mode: sheltering, target: t.id, shelter: f.bk.feet, jump: f.bk.jump}) }
	if st.Health < 20 {
		// la dorada (absorción y regeneración) con poca vida; con algo, la comida
		// normal (con el hambre llena se cura solo)
		if gap, ok := pickFood(foods, 20-st.Food, true); ok && st.Health <= 14 {
			if _, regen := st.Effects["minecraft:regeneration"]; !regen || st.Health < 8 {
				return f.eatHere(ctx, gap)
			}
		}
		if ps := f.potions(); st.Health <= 12 {
			if pt, ok := pickPotion(ps, "healing", true); ok && f.throwDown(ctx, pt, "en el búnker") {
				return true
			}
			if pt, ok := pickPotion(ps, "healing", false); ok {
				f.drink(ctx, pt, "en el búnker")
				again()
				return true
			}
		}
	}
	if st.Food < 20 && (st.Health < 20 || st.Food < 18) {
		if fd, ok := pickFood(foods, 20-st.Food, false); ok {
			return f.eatHere(ctx, fd)
		}
	}
	return false
}

// openWall: lo que hay que romper para salir: cabeza y pies del lado side
// (0 el del rival, 3 el de atrás). Si detrás no hay suelo (un borde), el
// siguiente lado que lo tenga.
func (f *Fighter) openWall(cells []cell, side int) {
	f.bk.side, f.bk.tries = side, 0
	for k := 0; k < 4; k++ {
		i := (side + 4 - k) % 4 // (del pedido hacia el rival, que es el último)
		if side == 0 {
			i = k
		}
		lo := cells[2*i].pos
		if f.solid(world.BlockPos{X: lo.X, Y: lo.Y - 1, Z: lo.Z}) || k == 3 {
			f.bk.side = i
			break
		}
	}
	var out []world.BlockPos
	for _, c := range []world.BlockPos{cells[2*f.bk.side+1].pos, cells[2*f.bk.side].pos} {
		if f.solid(c) {
			out = append(out, c)
		}
	}
	f.bk.opening = out
	if len(out) == 0 {
		f.endBunker()
	}
}

// endBunker: fuera (abierto): lo que tocara al salir (la perla) y el plan libre.
func (f *Fighter) endBunker() {
	pearl, fight := f.bk.thenPearl, f.bk.thenFight
	enemy := f.bk.enemy
	if fight {
		if e, ok := f.d.Entities.Get(enemy); ok && !dead(e) {
			f.Engage(enemy, f.label(e))
		}
	}
	f.bk = bunkerState{}
	f.bunkerRetry = f.steps + 100
	f.setPlan(plan{})
	if pearl {
		if e, ok := f.d.Entities.Get(enemy); ok {
			st := f.d.Player.Status()
			if f.pearlAway(context.Background(), st, e.X, e.Z) {
				f.retreatSince = time.Now()
				f.setPlan(plan{mode: retreating, fleeX: e.X, fleeZ: e.Z})
			}
		}
	}
}

// digOut rompe lo que queda de la salida, con la herramienta que vaya mejor,
// sin parar los pasos (romper a mano son segundos: mientras, se sigue
// mirando). Un bloque que no se rompe en 5 intentos (zona protegida, el
// servidor no deja): otro lado.
func (f *Fighter) digOut(ctx context.Context) bool {
	for len(f.bk.opening) > 0 && !f.solid(f.bk.opening[0]) {
		f.bk.opening, f.bk.tries = f.bk.opening[1:], 0
	}
	if len(f.bk.opening) == 0 {
		f.logf("abierto: fuera del búnker")
		f.endBunker()
		return false
	}
	f.setPlan(plan{mode: sheltering, target: f.bk.enemy, shelter: f.bk.feet})
	if f.d.Hands.Busy() {
		return true
	}
	if f.bk.tries >= 5 {
		f.bk.sides++
		if f.bk.sides >= 4 {
			f.logf("no puedo romper ninguna pared del búnker: me quedo")
			f.bk.opening = nil
			f.bk.sides = 0
			return true
		}
		f.logf("no rompo %v: pruebo otro lado", f.bk.opening[0])
		p := f.d.Player.Position()
		f.openWall(bunkerCells(f.bk.feet, p.X+1, p.Z), (f.bk.side+1)%4)
		return true
	}
	c := f.bk.opening[0]
	if slot, ok := f.toolFor(c); ok {
		if _, held := f.inventory(); slot != slotHotbar+held {
			_ = f.hold(ctx, slot)
			return true
		}
	}
	f.bk.tries++
	f.d.Hands.Dig(c, func(d act.Dig) {
		if d.Err != nil {
			f.logf("salir del búnker: %v", d.Err)
		}
	})
	return true
}

// sayPlan dice qué va a hacer dentro (una vez por plan).
func (f *Fighter) sayPlan(t *foe, s stance, healed, pearl bool) {
	var plan string
	switch {
	case t == nil:
		return
	case !healed:
		plan = "me curo"
	case s != stanceAvoid:
		plan = "curado: salgo a pelear"
	case pearl:
		plan = "no le gano: me voy con la perla por detrás"
	case f.shieldHolds(t):
		plan = "no le gano, pero con el escudo no me entra: salgo a la defensiva"
	default:
		plan = fmt.Sprintf("no le gano y no tengo perla: espero a que se vaya (si sigue en %d s, salgo a pelear)", bunkerMaxSteps/20)
	}
	if plan != f.bk.planned {
		f.bk.planned = plan
		f.logf("búnker, plan: %s", plan)
	}
}

// toolFor: la herramienta del inventario para romper el bloque de c (pico
// para piedra, pala para tierra, hacha para madera).
func (f *Fighter) toolFor(c world.BlockPos) (int, bool) {
	s, ok := f.d.World.BlockAt(c)
	if !ok {
		return 0, false
	}
	name := block.StateList[s].ID()
	want := "_pickaxe"
	switch {
	case strings.Contains(name, "dirt") || strings.Contains(name, "grass") || strings.Contains(name, "sand") || strings.Contains(name, "gravel"):
		want = "_shovel"
	case strings.Contains(name, "planks") || strings.Contains(name, "log") || strings.Contains(name, "wood") ||
		strings.Contains(name, "bamboo") || strings.Contains(name, "stem") || strings.Contains(name, "hyphae"):
		want = "_axe"
	}
	inv, _ := f.inventory()
	best, bestRank := -1, -1
	for i := slotMainStart; i <= slotHotbar+8; i++ {
		n := nameOf(inv[i])
		if !strings.HasSuffix(n, want) {
			continue
		}
		r := tierRank(n)
		if r > bestRank {
			best, bestRank = i, r
		}
	}
	return best, best >= 0
}

func tierRank(n string) int {
	for i, t := range []string{"wooden", "golden", "stone", "iron", "diamond", "netherite"} {
		if strings.Contains(n, t) {
			return i
		}
	}
	return 0
}

// thinkShelter: quieto en el centro de la celda (si no, los bloques de al lado no caben).
func (f *Fighter) thinkShelter(s *control.State, bp *basic.Player, pl *plan) {
	f.lowerShield()
	pos := bp.Position()
	cx, cz := float64(pl.shelter.X)+0.5, float64(pl.shelter.Z)+0.5
	in := control.Input{}
	d := math.Hypot(cx-pos.X, cz-pos.Z)
	if pl.jump && s.OnGround && d < 0.2 {
		in.Jump = true // (para poner sobre la pared: su cara de arriba, vista desde lo alto del salto)
	}
	if d > 0.12 && !f.d.Hands.Busy() {
		// hacia el centro, agachado (despacio y sin caerse por un borde)
		yaw := float32(-math.Atan2(cx-pos.X, cz-pos.Z) * 180 / math.Pi)
		bp.UpdatePosition(func(q basic.Position) basic.Position { q.Yaw = yaw; return q })
		in.Forward, in.Shift = true, true
	}
	s.Input = in
}

// ShelterBlocks son los bloques que ha puesto encerrándose (con los repuestos).
func (f *Fighter) ShelterBlocks() int64 { return f.shelterBlocks.Load() }

// unbox: sin pelea y encerrado entre bloques que puso el bot (un búnker que
// acabó sin abrir: le sacaron de él, se quedó sin bloques, una orden "stop",
// reconectó): abre una salida. El pathfinding no rompe piedra: sin esto se
// queda dentro. Dice si empezó a salir.
func (f *Fighter) unbox() bool {
	if f.bk.on || len(f.myBlocks) == 0 || f.steps%20 != 0 {
		return false
	}
	p := f.d.Player.Position()
	feet := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
	cells := bunkerCells(feet, p.X+1, p.Z)
	mine := false
	for _, c := range cells {
		if f.myBlocks[c.pos] && f.solid(c.pos) {
			mine = true
		}
	}
	if !mine || !f.enclosed(cells) {
		return false
	}
	f.logf("encerrado entre mis bloques: abro una salida")
	f.bk = bunkerState{on: true, feet: feet, since: f.steps}
	f.openWall(cells, 3)
	return f.bk.on
}

// enclosed: ningún lado tiene paso (pies y cabeza libres).
func (f *Fighter) enclosed(cells []cell) bool {
	for i := 0; i < 4; i++ {
		if !f.solid(cells[2*i].pos) && !f.solid(cells[2*i+1].pos) {
			return false
		}
	}
	return true
}
