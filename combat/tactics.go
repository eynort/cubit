package combat

// Tácticas contra un jugador que no son el golpe:
//
//   - Tirarle abajo: subido a algo (una torre, un saliente) y con el bloque
//     que pisa al alcance, se le rompe (con la herramienta que vaya): cae, y
//     la caída duele. Si no se llega, el arco (bow.go: el empuje de la flecha
//     le tira).
//   - Quitarle el agua y la lava: un cubo que ha echado él (una fuente suelta,
//     no un lago) cerca de la pelea: el agua le frena la caída, le apaga y le
//     separa; la lava, una trampa. Se recoge con un cubo vacío; sin cubo, se
//     tapa con un bloque.
//   - Encerrarle: quieto (comiendo, bebiendo, parado) y a tiro de bloques, se
//     le ponen bloques detrás y a los lados, a la altura de los pies y de la
//     cabeza: no se escapa ni le aleja el empujón (cada golpe le deja pegado
//     a la pared), y el lado del bot queda abierto para pegarle.

import (
	"context"
	"math"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/level/block"
	"github.com/mj41/go-mc26/level/component"
	pk "github.com/mj41/go-mc26/net/packet"
)

// tacticsState es lo que recuerdan las tácticas (solo Run).
type tacticsState struct {
	id         int32      // el rival que se mira
	lastX      [2]float64 // dónde estaba
	stillFrom  uint64     // desde qué paso está quieto
	knockAt    world.BlockPos
	knockTry   int
	caged      int            // bloques de jaula puestos a este rival
	cageFeet   world.BlockPos // la celda en que se le encierra
	cageDone   bool           // esa jaula, acabada
	cageFrom   [2]float64     // desde dónde se empezó (el lado abierto)
	approach   bool           // acercándose para encerrarle: sin pegarle aún
	walled     bool           // está encerrado (dicho en el log)
	bunker     world.BlockPos // la celda de su búnker (rompiéndolo)
	bunkerAt   uint64         // y cuándo se vio cerrado por última vez
	breaking   bool           // rompiéndole la pared (las manos en eso)
	breakAt    world.BlockPos // el bloque que se le rompe
	breakTry   int            // intentos con él
	cageAt     uint64         // el paso del último
	cageNext   uint64         // hasta este paso, ninguna jaula más
	fluidAt    uint64         // el de la última vez que quitó un fluido
	fluidPos   world.BlockPos // cuál
	fluidTries int            // y cuántas veces
	bowShots   int64          // flechas tiradas al empezar a tirarle de donde se subió (bow.go)
	bowY       float64        // a qué altura estaba entonces
	bowOn      bool           // tirándole de donde se subió
	bowOff     uint64         // hasta este paso, sin arco contra él (no le tira: a subir)
	coverAt    uint64         // el paso en que se cubrió de sus flechas (cover)
	coverPos   world.BlockPos // la celda de la cabeza de la cobertura
}

// fluidWait: los pasos entre dos intentos de quitar un fluido (la respuesta del servidor).
const fluidWait = 20

// eyeReach: lo que alcanza la mano (de los ojos al centro del bloque).
const eyeReach = 4.4

// safeDig: a qué distancia de uno armado se le rompe la pared: fuera de su
// golpe (3 de los ojos a la caja: unos 3,4 de centro a centro), con la pared
// (un bloque más cerca) al alcance de la mano.
const safeDig = 3.9

func (f *Fighter) eyeDist(c world.BlockPos) float64 {
	p := f.d.Player.Position()
	return math.Sqrt(sq(float64(c.X)+0.5-p.X) + sq(float64(c.Y)+0.5-(p.Y+1.62)) + sq(float64(c.Z)+0.5-p.Z))
}

func sq(x float64) float64 { return x * x }

// track apunta si el rival t se mueve (para "quieto").
func (f *Fighter) trackStill(t *foe) {
	if f.tac.id != t.id || math.Hypot(t.e.X-f.tac.lastX[0], t.e.Z-f.tac.lastX[1]) > 0.08 {
		f.tac.stillFrom = f.steps
	}
	if f.tac.id != t.id {
		f.tac = tacticsState{id: t.id, stillFrom: f.steps}
	}
	f.tac.lastX = [2]float64{t.e.X, t.e.Z}
}

// still: el rival lleva un rato sin moverse (medio segundo).
func (f *Fighter) still(t *foe) bool { return f.tac.id == t.id && f.steps-f.tac.stillFrom >= 10 }

// usingItem: está usando lo de la mano (comer, beber, el escudo, el arco).
func usingItem(e entities.Entity) (item string, ok bool) {
	v, has := e.Data[entitydata.LivingEntityLivingEntityFlags]
	if !has {
		return "", false
	}
	b, isByte := v.Value.(*pk.Byte)
	if !isByte || *b&1 == 0 {
		return "", false
	}
	hand := 0
	if *b&2 != 0 {
		hand = 1
	}
	it := e.Equipment[hand]
	return itemName(int32(it.Item)), it.Count > 0
}

// eatingNow: está comiendo o bebiendo (no el escudo ni el arco).
func eatingNow(e entities.Entity) bool {
	n, ok := usingItem(e)
	if !ok {
		return false
	}
	if strings.Contains(n, "potion") || n == "minecraft:golden_apple" || n == "minecraft:enchanted_golden_apple" {
		return true
	}
	it := e.Equipment[0]
	if itemName(int32(it.Item)) != n {
		it = e.Equipment[1]
	}
	return act.Component[*component.Food](screen.Slot(it)) != nil
}

// support: los bloques sobre los que está de pie e (bajo su caja).
func (f *Fighter) support(e entities.Entity) []world.BlockPos {
	y := int(math.Floor(e.Y - 0.01))
	var out []world.BlockPos
	seen := map[world.BlockPos]bool{}
	for _, dx := range []float64{-0.29, 0.29} {
		for _, dz := range []float64{-0.29, 0.29} {
			c := world.BlockPos{X: int(math.Floor(e.X + dx)), Y: y, Z: int(math.Floor(e.Z + dz))}
			if !seen[c] && f.solid(c) {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// knockDown: subido a algo (2 o más por encima) sobre un solo bloque al
// alcance, y no metido en un pozo: romperlo (cae; con hueco debajo, de 3 o
// más se hace daño; en una torre, baja un bloque, y otro). Dice si está en ello.
func (f *Fighter) knockDown(ctx context.Context, t *foe) bool {
	p := f.d.Player.Position()
	if f.d.Hands.Busy() {
		_, digging := f.d.Hands.DigProgress()
		return digging && f.tac.knockTry > 0 // (rompiéndolo)
	}
	if t.e.Type != "minecraft:player" || t.e.Y-p.Y < 2 {
		f.tac.knockTry = 0
		return false
	}
	under := f.support(t.e)
	if len(under) != 1 {
		return false
	}
	c := under[0]
	if f.eyeDist(c) > eyeReach {
		return false
	}
	// en un pozo (rodeado de bloques a la altura de sus pies) solo bajaría
	// dentro, más lejos del bot; en una torre de 1×1 o un saliente cae (o
	// baja bloque a bloque hasta donde se le pega)
	walls := 0
	for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		if f.solid(world.BlockPos{X: c.X + d[0], Y: c.Y + 1, Z: c.Z + d[1]}) {
			walls++
		}
	}
	if walls >= 3 {
		return false
	}
	if s, ok := f.d.World.BlockAt(c); !ok || strings.Contains(stateName(s), "bedrock") || strings.Contains(stateName(s), "obsidian") {
		return false
	}
	if c != f.tac.knockAt {
		f.tac.knockAt, f.tac.knockTry = c, 0
		f.logf("%s está subido a %v: le rompo el bloque que pisa", t.name, c)
	}
	if f.tac.knockTry >= 4 {
		return false // (no se rompe: zona protegida, o sin herramienta tarda una eternidad)
	}
	if slot, ok := f.toolFor(c); ok {
		if _, held := f.inventory(); slot != slotHotbar+held {
			_ = f.hold(ctx, slot)
			return true
		}
	}
	f.tac.knockTry++
	f.stopMoving()
	f.setPlan(plan{mode: acting, target: t.id})
	f.d.Hands.Dig(c, func(d act.Dig) {
		if d.Err != nil {
			f.logf("romperle el suelo: %v", d.Err)
		}
	})
	return true
}

// clearFluids: una fuente de agua o lava suelta (de un cubo) cerca de la
// pelea, al alcance: recogerla con un cubo vacío o taparla con un bloque.
func (f *Fighter) clearFluids(ctx context.Context, t *foe) bool {
	// tras recoger una, a que el servidor conteste (unos 8 ticks de ida y
	// vuelta): antes el cubo aún se ve vacío y el agua en su sitio, y "recoger"
	// con el cubo ya lleno es echarla otra vez (en los logs: recoger, echar,
	// recoger… 6 s en el mismo sitio, el agua esparcida)
	if f.d.Hands.Busy() || f.tac.fluidAt > 0 && f.steps-f.tac.fluidAt < fluidWait {
		return false
	}
	p := f.d.Player.Position()
	feet := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
	best, bestD, lava := world.BlockPos{}, math.Inf(1), false
	for dx := -4; dx <= 4; dx++ {
		for dy := -2; dy <= 3; dy++ {
			for dz := -4; dz <= 4; dz++ {
				c := world.BlockPos{X: feet.X + dx, Y: feet.Y + dy, Z: feet.Z + dz}
				isLava, ok := f.looseSource(c)
				if !ok || f.eyeDist(c) > eyeReach || isLava && f.ownLava(c) { // (la suya, recién echada: aún no)
					continue
				}
				// cerca de uno de los dos: la del bot (agua: frena su golpe; lava: le quema) o la del rival
				dMe := math.Hypot(float64(c.X)+0.5-p.X, float64(c.Z)+0.5-p.Z)
				dHim := math.Hypot(float64(c.X)+0.5-t.e.X, float64(c.Z)+0.5-t.e.Z)
				if math.Min(dMe, dHim) > 3 {
					continue
				}
				d := dMe
				if isLava {
					d -= 10 // (la lava, antes)
				}
				if d < bestD {
					best, bestD, lava = c, d, isLava
				}
			}
		}
	}
	if math.IsInf(bestD, 1) {
		return false
	}
	what := map[bool]string{false: "agua", true: "lava"}[lava]
	// la misma, otra vez y ya intentada: no sale (se rellena, o no es suelta); a otra cosa
	if best == f.tac.fluidPos && f.tac.fluidTries >= 3 {
		return false
	}
	if best != f.tac.fluidPos {
		f.tac.fluidPos, f.tac.fluidTries = best, 0
	}
	// con un cubo vacío: se recoge (y el cubo se queda)
	if slot, ok := f.slotOf("minecraft:bucket"); ok {
		if !f.holdChecked(ctx, slot, "minecraft:bucket") {
			return true
		}
		if inv, h := f.inventory(); nameOf(inv[slotHotbar+h]) != "minecraft:bucket" {
			return true // (ya no está vacío: el agua de antes llegó)
		}
		f.tac.fluidTries++
		f.logf("le quito el %s que puso (cubo) en %v", what, best)
		f.stopMoving()
		f.setPlan(plan{mode: acting, target: t.id})
		f.tac.fluidAt = f.steps
		f.handsBusy.Store(true)
		f.d.Hands.UseItemToward(float64(best.X)+0.5, float64(best.Y)+0.85, float64(best.Z)+0.5, func(act.Use) { f.handsBusy.Store(false) }) // (la superficie)
		return true
	}
	// sin cubo: un bloque encima (lo reemplaza)
	if f.d.Builder != nil && f.d.Builder.Blocks() > 0 && !f.d.Builder.Busy() {
		if against, ok := f.againstFor(best); ok {
			f.stopMoving()
			f.setPlan(plan{mode: acting, target: t.id})
			if f.d.Builder.PlaceNow(best, against) { // (false: pasando el bloque a la mano; el paso siguiente, otra vez)
				f.tac.fluidAt = f.steps
				f.tac.fluidTries++
				f.logf("le tapo el %s que puso en %v", what, best)
			}
			return true
		}
	}
	return false
}

// looseSource: c es una fuente de agua o lava suelta (de un cubo: con una
// fuente al lado como mucho; un lago o un río tiene muchas).
func (f *Fighter) looseSource(c world.BlockPos) (lava, ok bool) {
	s, has := f.d.World.BlockAt(c)
	if !has {
		return false, false
	}
	n := world.StateString(s)
	isWater := strings.HasPrefix(n, "minecraft:water")
	isLava := strings.HasPrefix(n, "minecraft:lava")
	if !isWater && !isLava || strings.Contains(n, "level=") && !strings.Contains(n, "level=0") {
		return false, false
	}
	near := 0
	for _, d := range [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, -1, 0}} {
		s2, ok := f.d.World.BlockAt(world.BlockPos{X: c.X + d[0], Y: c.Y + d[1], Z: c.Z + d[2]})
		if !ok {
			continue
		}
		n2 := world.StateString(s2)
		if (strings.HasPrefix(n2, "minecraft:water") || strings.HasPrefix(n2, "minecraft:lava")) && (!strings.Contains(n2, "level=") || strings.Contains(n2, "level=0")) {
			near++
		}
	}
	return isLava, near <= 1
}

// againstFor: un bloque sólido al lado de c contra el que poner (el de debajo primero).
func (f *Fighter) againstFor(c world.BlockPos) (world.BlockPos, bool) {
	for _, d := range [][3]int{{0, -1, 0}, {1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, 1, 0}} {
		a := world.BlockPos{X: c.X + d[0], Y: c.Y + d[1], Z: c.Z + d[2]}
		if f.solid(a) {
			return a, true
		}
	}
	return world.BlockPos{}, false
}

// slotOf: dónde lleva el objeto (la barra primero).
func (f *Fighter) slotOf(name string) (int, bool) {
	inv, _ := f.inventory()
	for i := slotHotbar + 8; i >= slotMainStart; i-- {
		if nameOf(inv[i]) == name {
			return i, true
		}
	}
	return 0, false
}

// cage: quieto (comiendo, parado) y a tiro de bloques: detrás y a los lados,
// pies y cabeza. El lado del bot, abierto (para pegarle; el empujón le deja
// contra la pared). Dice si está en ello.
func (f *Fighter) cage(t *foe) bool {
	if f.d.Builder == nil || t.e.Type != "minecraft:player" || f.d.Builder.Blocks() < 2 {
		return false
	}
	f.tac.approach = false
	// contra uno armado, solo comiendo o bebiendo (bot contra bot: "quieto"
	// era estar tras el escudo medio segundo, y las paredes de una jaula le
	// dejaban "arrinconado" para la siguiente: la arena llena de piedra y los
	// dos separados por muros). Y un rato entre jaulas
	armed := armedEntity(t.e)
	if !(eatingNow(t.e) || !armed && (f.still(t) || f.cornered(t))) || t.dist < 1.6 || t.dist > 5.5 || f.steps < f.tac.cageNext {
		return false
	}
	if f.matchID == t.id && f.match.myHits <= 2 {
		return false // (a punto de morir: a rematarle, no a poner bloques)
	}
	if feet := (world.BlockPos{X: int(math.Floor(t.e.X)), Y: int(math.Floor(t.e.Y + 1e-6)), Z: int(math.Floor(t.e.Z))}); feet == f.tac.cageFeet && f.tac.cageDone {
		return false
	}
	if f.d.Builder.Busy() || f.d.Hands.Busy() {
		return true
	}
	if f.steps-f.tac.cageAt < 2 {
		return true
	}
	p := f.d.Player.Position()
	feet := world.BlockPos{X: int(math.Floor(t.e.X)), Y: int(math.Floor(t.e.Y + 1e-6)), Z: int(math.Floor(t.e.Z))}
	// los lados de su celda, el de atrás (lejos del bot) primero; sin el del bot
	dx, dz := t.e.X-p.X, t.e.Z-p.Z
	if feet == f.tac.cageFeet {
		dx, dz = t.e.X-f.tac.cageFrom[0], t.e.Z-f.tac.cageFrom[1]
	}
	type side struct {
		d   [2]int
		dot float64
	}
	sides := []side{{d: [2]int{1, 0}}, {d: [2]int{-1, 0}}, {d: [2]int{0, 1}}, {d: [2]int{0, -1}}}
	for i := range sides {
		sides[i].dot = float64(sides[i].d[0])*dx + float64(sides[i].d[1])*dz
	}
	for i := 1; i < len(sides); i++ {
		for j := i; j > 0 && sides[j].dot > sides[j-1].dot; j-- {
			sides[j], sides[j-1] = sides[j-1], sides[j]
		}
	}
	if feet != f.tac.cageFeet { // (se movió a otra celda: otra jaula)
		f.tac.cageFeet, f.tac.caged, f.tac.cageDone = feet, 0, false
		f.tac.cageFrom = [2]float64{p.X, p.Z} // (el lado abierto, el de ahora: no cambia aunque el bot se mueva)
	}
	others := []foe{*t}
	// la de detrás es la que importa (sin ella, el primer golpe le saca): al
	// alcance, o primero acercarse (fight)
	back := world.BlockPos{X: feet.X + sides[0].d[0], Y: feet.Y, Z: feet.Z + sides[0].d[1]}
	if !f.solid(back) && f.eyeDist(back) > eyeReach && f.tac.caged == 0 {
		f.tac.approach = true // (fight: acercarse sin pegarle: el golpe le sacaría de la celda antes de cerrarla)
		return false
	}
	for _, s := range sides[:3] {
		lo := world.BlockPos{X: feet.X + s.d[0], Y: feet.Y, Z: feet.Z + s.d[1]}
		hi := world.BlockPos{X: lo.X, Y: lo.Y + 1, Z: lo.Z}
		for _, c := range []cell{{lo, world.BlockPos{X: lo.X, Y: lo.Y - 1, Z: lo.Z}}, {hi, lo}} {
			if f.solid(c.pos) || !f.replaceable(c.pos) || !f.solid(c.against) || f.occupied(c.pos, others) || f.eyeDist(c.pos) > eyeReach {
				continue
			}
			if f.tac.caged == 0 {
				why := "está quieto"
				if f.cornered(t) {
					why = "está arrinconado"
				}
				f.logf("%s %s: le encierro con bloques (detrás y a los lados)", t.name, why)
			}
			f.setPlan(plan{mode: acting, target: t.id})
			if f.d.Builder.PlaceNow(c.pos, c.against) {
				f.tac.caged++
				f.tac.cageAt = f.steps
				if f.tac.caged >= 8 {
					f.tac.cageDone, f.tac.cageNext = true, f.steps+600
				}
			}
			return true
		}
	}
	f.tac.cageDone = true // (nada más que poner: hecho)
	f.tac.cageNext = f.steps + 600
	return false
}

func stateName(s block.StateID) string { return block.StateList[s].ID() }

// clearShot: de los ojos a su pecho, en línea recta, ningún bloque (la
// flecha va algo por encima: si la recta está libre, casi siempre llega).
func (f *Fighter) clearShot(e entities.Entity) bool {
	p := f.d.Player.Position()
	ax, ay, az := p.X, p.Y+1.62, p.Z
	bx, by, bz := e.X, e.Y+1.0, e.Z
	n := int(math.Ceil(math.Sqrt(sq(bx-ax)+sq(by-ay)+sq(bz-az)) / 0.25))
	for i := 1; i < n; i++ {
		k := float64(i) / float64(n)
		c := world.BlockPos{X: int(math.Floor(ax + (bx-ax)*k)), Y: int(math.Floor(ay + (by-ay)*k)), Z: int(math.Floor(az + (bz-az)*k))}
		if f.solid(c) {
			return false
		}
	}
	return true
}

// inCage: t está en un hueco cerrado por tres lados (la jaula que le puso el
// bot, o su búnker con la pared ya rota): el pathfinding no ve línea recta
// hasta su celda, pero por la abertura se le pega.
func (f *Fighter) inCage(t *foe) bool {
	feet := world.BlockPos{X: int(math.Floor(t.e.X)), Y: int(math.Floor(t.e.Y + 1e-6)), Z: int(math.Floor(t.e.Z))}
	if f.tac.id == t.id && f.tac.caged > 0 && feet == f.tac.cageFeet {
		return true
	}
	// en su búnker roto o en la puerta (un empujón le saca a medias)
	if b := f.tac.bunker; f.tac.id == t.id && f.tac.bunkerAt > 0 && f.steps-f.tac.bunkerAt < 600 &&
		feet.Y == b.Y && abs(feet.X-b.X)+abs(feet.Z-b.Z) <= 1 {
		return true
	}
	walls := 0
	for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		if f.solid(world.BlockPos{X: feet.X + d[0], Y: feet.Y, Z: feet.Z + d[1]}) || f.solid(world.BlockPos{X: feet.X + d[0], Y: feet.Y + 1, Z: feet.Z + d[1]}) {
			walls++
		}
	}
	return walls >= 3
}

// breakIn: encerrado en su búnker (pies y cabeza tapados a los cuatro lados):
// romper la pared del lado del bot, la cabeza primero (por ahí ya se le pega)
// y luego los pies (para que salga, o entrar). A uno armado, desde fuera de
// su alcance (safeDig): que salga a campo abierto, donde el escudo manda; en
// la puerta, con la pared detrás, no hay empujón y el cambio de golpes es a la
// par. Si la vuelve a poner, otra vez.
// Mientras no llegue a la pared, se acerca (fight). Dice si está en ello.
func (f *Fighter) breakIn(ctx context.Context, t *foe) bool {
	if f.d.Hands.Busy() {
		_, digging := f.d.Hands.DigProgress()
		return digging && f.tac.breaking
	}
	// a su altura (un hueco en lo alto de algo es cosa de subir, el arco o
	// romperle el suelo: knockDown)
	if t.e.Type != "minecraft:player" || t.dist > 7 || math.Abs(t.e.Y-f.d.Player.Position().Y) > 1.2 {
		return false
	}
	f.tac.breaking = false
	p := f.d.Player.Position()
	feet := world.BlockPos{X: int(math.Floor(t.e.X)), Y: int(math.Floor(t.e.Y + 1e-6)), Z: int(math.Floor(t.e.Z))}
	cells := bunkerCells(feet, p.X, p.Z) // (la pared "del rival" de su búnker es la del lado del bot)
	if !f.enclosed(cells) {
		f.tac.walled = false
		return false
	}
	if !f.tac.walled {
		f.tac.walled = true
		f.logf("%s se ha encerrado: le rompo la pared", t.name)
	}
	f.tac.bunker, f.tac.bunkerAt = feet, f.steps
	armed := armedEntity(t.e) && !eatingNow(t.e)
	for _, c := range []world.BlockPos{cells[1].pos, cells[0].pos} { // cabeza, pies
		if !f.solid(c) {
			continue
		}
		// armado: desde fuera de su alcance (su golpe llega a 3; romper, a
		// 4,5): pegando por la ventana mientras se rompe la otra, con el pico
		// en la mano y sin escudo, el cambio de golpes es suyo
		if armed && (t.dist < safeDig-0.25 || f.eyeDist(c) > eyeReach) {
			f.setPlan(plan{mode: acting, target: t.id, keepAt: safeDig})
			return true
		}
		if f.eyeDist(c) > eyeReach {
			return false // (más cerca: fight)
		}
		if s, ok := f.d.World.BlockAt(c); ok && (strings.Contains(stateName(s), "bedrock") || strings.Contains(stateName(s), "obsidian")) {
			return false
		}
		if c != f.tac.breakAt {
			f.tac.breakAt, f.tac.breakTry = c, 0
		}
		if f.tac.breakTry >= 5 {
			return false // (no se rompe: zona protegida)
		}
		if slot, ok := f.toolFor(c); ok {
			if _, held := f.inventory(); slot != slotHotbar+held {
				_ = f.hold(ctx, slot)
				return true
			}
		}
		f.tac.breakTry++
		f.tac.breaking = true
		f.stopMoving()
		if armed {
			f.setPlan(plan{mode: acting, target: t.id, keepAt: safeDig})
		} else {
			f.setPlan(plan{mode: acting, target: t.id})
		}
		f.d.Hands.Dig(c, func(d act.Dig) {
			if d.Err != nil {
				f.logf("romperle la pared: %v", d.Err)
			}
		})
		return true
	}
	return false
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// cornered: el rival tiene ya dos o más lados tapados a la altura de los
// pies (un rincón, un pasillo, una jaula a medias): con uno o dos bloques
// más no se mueve, aunque no esté quieto. Con bloques para ello.
func (f *Fighter) cornered(t *foe) bool {
	if f.d.Builder == nil || f.d.Builder.Blocks() < 3 {
		return false
	}
	feet := world.BlockPos{X: int(math.Floor(t.e.X)), Y: int(math.Floor(t.e.Y + 1e-6)), Z: int(math.Floor(t.e.Z))}
	if !f.solid(world.BlockPos{X: feet.X, Y: feet.Y - 1, Z: feet.Z}) {
		return false // (en el aire: saltando, cayendo)
	}
	walls := 0
	for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		if f.solid(world.BlockPos{X: feet.X + d[0], Y: feet.Y, Z: feet.Z + d[1]}) {
			walls++
		}
	}
	return walls >= 2 && walls < 4
}

// coverWait: los pasos que se queda tras la cobertura mientras él tensa.
const coverWait = 60

// cover: un jugador le apunta con el arco desde lejos y el bot no tiene
// escudo (o se lo desactivaron): con bloques, una pared de dos (pies y
// cabeza) hacia él, y quieto detrás mientras tensa (la flecha da en el
// bloque). Luego sigue (el pathfinding la rodea). Dice si está en ello.
func (f *Fighter) cover(t *foe, shield bool) bool {
	if t.e.Type != "minecraft:player" || f.d.Builder == nil || t.dist < 6 {
		return false
	}
	if shield && !f.shieldOff(0) {
		return false // (el escudo para la flecha: Think lo sube)
	}
	p := f.d.Player.Position()
	aimed := aiming(t.e, p)
	feet := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
	// ya cubierto: quieto tras ella mientras siga tensando (un rato como mucho)
	if f.tac.coverAt > 0 && f.steps-f.tac.coverAt < coverWait && f.solid(f.tac.coverPos) &&
		abs(f.tac.coverPos.X-feet.X)+abs(f.tac.coverPos.Z-feet.Z) == 1 && f.tac.coverPos.Y == feet.Y+1 {
		if !aimed {
			return false
		}
		f.stopMoving()
		f.setPlan(plan{mode: acting, target: t.id})
		return true
	}
	if !aimed || f.d.Builder.Blocks() < 2 || f.d.Builder.Busy() || f.d.Walker.Building() || !f.d.Ctl.State().OnGround {
		return false
	}
	if f.steps-f.tac.coverAt < coverWait*2 && f.tac.coverAt > 0 {
		return false // (otra vez tan pronto no: se queda aquí parado para siempre)
	}
	// hacia él, por el eje que más pesa
	dx, dz := t.e.X-p.X, t.e.Z-p.Z
	d := [2]int{0, 0}
	if math.Abs(dx) > math.Abs(dz) {
		d[0] = int(math.Copysign(1, dx))
	} else {
		d[1] = int(math.Copysign(1, dz))
	}
	lo := world.BlockPos{X: feet.X + d[0], Y: feet.Y, Z: feet.Z + d[1]}
	hi := world.BlockPos{X: lo.X, Y: lo.Y + 1, Z: lo.Z}
	// desde arriba la flecha pasa por encima de una pared baja: solo si él no está muy por encima
	if t.e.Y-p.Y > 3 {
		return false
	}
	for _, c := range []cell{{lo, world.BlockPos{X: lo.X, Y: lo.Y - 1, Z: lo.Z}}, {hi, lo}} {
		if f.solid(c.pos) {
			continue
		}
		if !f.replaceable(c.pos) || !f.solid(c.against) && c.pos == lo {
			return false
		}
		if c.pos == hi && !f.solid(lo) {
			return true // (el de abajo, puesto: en la respuesta del servidor)
		}
		f.stopMoving()
		f.setPlan(plan{mode: acting, target: t.id})
		if f.d.Builder.PlaceNow(c.pos, c.against) && c.pos == hi {
			f.tac.coverAt, f.tac.coverPos = f.steps, hi
			f.logf("%s me apunta con el arco: me cubro con bloques", t.name)
		}
		return true
	}
	f.tac.coverAt, f.tac.coverPos = f.steps, hi // (ya había pared)
	return true
}
