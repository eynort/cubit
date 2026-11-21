package combat

// Lo de un experto, más allá del arsenal (ver PVP.md):
//
//   - Cristales del End y anclas de reaparición: explosiones de 6 y 5. Se
//     calcula el daño de verdad (ServerExplosion: distancia, lo que se ve de
//     cada cuerpo, armadura, protección y protección contra explosiones) a
//     él y al bot, y solo se hace si le quita mucho más que al bot y el bot
//     no muere. Un cristal suyo cerca: el escudo de cara a él (la explosión
//     de frente no entra), o romperlo si le hace más a él.
//   - Tótems: gastado uno, otro a la mano izquierda al momento; los que se
//     le saltan al rival, contados.
//   - Caña de pescar: a uno que se va o al que no se llega, se le engancha y
//     se le trae (un décimo de la distancia de golpe).
//   - Perla de ender ofensiva: a uno que huye lejos o al que no se llega, al
//     lado suyo (cuesta 5 de vida: con 12 o más).
//   - Élitros y maza: con cielo libre, despegar con cohetes, subir ~22
//     bloques sobre él, caer en picado (la caída cuenta con más de medio
//     bloque por tick), quitarse los élitros encima de él y la maza.
//   - Agua: a uno con maza que viene cayendo, agua a los pies del bot (en el
//     agua la caída se olvida: su golpe no suma); en una telaraña, romperla.
//   - Tridente con propulsión: en el agua, a por él volando.

import (
	"context"
	"math"
	"strings"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

const (
	crystalPower = 6.0
	anchorPower  = 5.0
	slotChest    = 6
	diveHeight   = 22  // sobre él antes de caer en picado
	diveEvery    = 600 // entre dos picados
	pearlEvery   = 40
	rodEvery     = 40
	pearlSpeed   = 1.5
)

// expertState es lo de este archivo que recuerda Run.
type expertState struct {
	anchor      world.BlockPos // el ancla que se puso
	anchorPhase int            // 0 nada, 1 puesta, 2 cargada
	anchorAt    uint64
	blastNext   uint64 // entre dos cristales o anclas
	rodNext     uint64
	rodAt       uint64 // el paso en que se lanzó la caña (0: no está fuera)
	rodReel     uint64 // y en el que se recogió
	pearlNext   uint64
	dive        int // el picado: 0 nada, 1 poniéndose los élitros, 2 subiendo, 3 cayendo, 4 sin élitros
	diveAt      uint64
	diveNext    uint64
	rocketAt    uint64
	waterAt     uint64 // el agua contra la maza
	pops        map[int32]int
	popAt       uint64         // el paso en que se gastó un tótem del bot (0: nada pendiente)
	blastSkip   world.BlockPos // la celda que la explosión quita antes de estallar (el ancla): no tapa
	trapNext    uint64         // hasta este paso no se mira otra vez si le encerraron (defense.go)
	bowKeep     uint64         // hasta aquí se sigue con el arco (bow.go: sin ir y venir)
	obby        world.BlockPos // la obsidiana recién puesta para un cristal
	obbyUntil   uint64         // hasta aquí se espera a verla y se pone el cristal encima
	obbyNext    uint64         // hasta aquí, ninguna obsidiana más
	bowRest     uint64         // y hasta aquí, sin él
}

// initExpert: los ganchos de los élitros en la física (si no los puso nadie)
// y lo que se escucha del servidor.
func (f *Fighter) initExpert() {
	m := f.d.Mover
	if m == nil {
		return
	}
	if m.CanGlide == nil {
		m.CanGlide = func() bool {
			inv, _ := f.inventory()
			return nameOf(inv[slotChest]) == "minecraft:elytra"
		}
	}
	if m.Rocket == nil {
		m.Rocket = f.boosting
	}
	if m.FallFlying == nil {
		m.FallFlying = func() (bool, uint64) { return f.glideOn.Load(), f.glideTold.Load() }
		f.d.Client.Events.AddListener(bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlaySetEntityData, F: f.onSelfData})
	}
}

// onSelfData: los flags compartidos del bot (el 0x80: planeando).
func (f *Fighter) onSelfData(p pk.Packet) error {
	var d play.SetEntityData
	if err := p.Scan(&d); err != nil || int32(d.ID) != int32(f.d.Player.Login.PlayerID) {
		return nil
	}
	for _, v := range d.PackedItems {
		if b, ok := v.Value.(*pk.Byte); ok && v.Index == entitydata.EntitySharedFlags {
			f.glideOn.Store(uint8(*b)&0x80 != 0)
			f.glideTold.Add(1)
		}
	}
	return nil
}

// boosting: un cohete suyo tira de él.
func (f *Fighter) boosting() bool {
	p := f.d.Player.Position()
	for _, e := range f.d.Entities.Nearby(p.X, p.Y, p.Z, 4) {
		if e.Type == "minecraft:firework_rocket" && e.SpawnData == int32(f.d.Player.Login.PlayerID) {
			return true
		}
	}
	return false
}

// onPop: el evento 35 (un tótem gastado). El del bot: otro a la mano al
// momento (Run); el de otro, contado.
func (f *Fighter) onPop(id int32) {
	if id == int32(f.d.Player.Login.PlayerID) {
		f.popped.Store(true)
		return
	}
	f.mu.Lock()
	_, foe := f.aggressors[id]
	f.mu.Unlock()
	if foe {
		f.enemyPop.Store(id)
	}
}

// retotem: tras gastar un tótem, otro a la mano izquierda (un clic, sin
// esperar: con 1 de vida no hay tiempo). Dice si lo hizo.
func (f *Fighter) retotem() bool {
	if id := f.enemyPop.Swap(0); id != 0 {
		if f.ex.pops == nil {
			f.ex.pops = map[int32]int{}
		}
		f.ex.pops[id]++
		name := "rival"
		if e, ok := f.d.Entities.Get(id); ok {
			name = f.label(e)
		}
		f.logf("le salta un tótem a %s (%d)", name, f.ex.pops[id])
	}
	if f.popped.Swap(false) {
		f.ex.popAt = f.steps
	}
	if f.ex.popAt == 0 {
		return false
	}
	inv, _ := f.inventory()
	if nameOf(inv[slotOffhand]) == itemTotem { // (el inventario llega tras el aviso: a esperarlo un poco)
		if f.steps-f.ex.popAt > 10 {
			f.ex.popAt = 0
		}
		return false
	}
	f.ex.popAt = 0
	slot, ok := f.slotOf(itemTotem)
	if !ok {
		f.logf("tótem gastado y no me quedan")
		return false
	}
	if err := f.d.Screens.Click(0, slot, offhandButton, types.ContainerInputSwap); err != nil {
		f.logf("tótem: %v", err)
		return false
	}
	f.offhandWant, f.offhandAt = itemTotem, f.steps
	n := 0
	for i := slotMainStart; i <= slotOffhand; i++ {
		if nameOf(inv[i]) == itemTotem {
			n += int(inv[i].Count)
		}
	}
	f.logf("tótem gastado: otro a la mano izquierda (quedan %d)", n-1)
	return true
}

// busy: las manos en algo hasta que conteste el servidor (como la lava): Run
// no cambia de objeto mientras (handsBusy).
func (f *Fighter) busy(what string) func(act.Use) {
	f.handsBusy.Store(true)
	return func(u act.Use) {
		f.handsBusy.Store(false)
		if u.Err != nil && what != "" { // ("": el cristal es una entidad, no un bloque: el "no cambió" no cuenta)
			f.logf("%s: %v", what, u.Err)
		}
	}
}

// --- explosiones ---

// seen: la parte del cuerpo (pies en feet, medio ancho half, alto high) que
// se ve desde c (Explosion.getSeenPercent: rayos desde puntos del cuerpo).
func (f *Fighter) seen(c [3]float64, feet [3]float64, half, high float64) float64 {
	n, clear := 0, 0
	for _, fx := range []float64{0, 0.5, 1} {
		for _, fy := range []float64{0, 0.25, 0.5, 0.75, 1} {
			for _, fz := range []float64{0, 0.5, 1} {
				a := [3]float64{feet[0] - half + 2*half*fx, feet[1] + high*fy, feet[2] - half + 2*half*fz}
				n++
				if f.rayFree(a, c) {
					clear++
				}
			}
		}
	}
	return float64(clear) / float64(n)
}

// rayFree: de a a b no hay bloques con forma.
func (f *Fighter) rayFree(a, b [3]float64) bool {
	d := math.Sqrt(sq(b[0]-a[0]) + sq(b[1]-a[1]) + sq(b[2]-a[2]))
	steps := int(math.Ceil(d / 0.25))
	last := world.BlockPos{X: math.MinInt32}
	for i := 1; i < steps; i++ {
		k := float64(i) / float64(steps)
		c := cellOf(a[0]+(b[0]-a[0])*k, a[1]+(b[1]-a[1])*k, a[2]+(b[2]-a[2])*k)
		if c != last && c != f.ex.blastSkip && f.solid(c) {
			return false
		}
		last = c
	}
	return true
}

// blastRaw: el daño de una explosión de power en c a un cuerpo de jugador
// con los pies en feet, antes de armadura (dificultad normal).
func (f *Fighter) blastRaw(c [3]float64, power float64, feet [3]float64) float64 {
	diam := 2 * power
	dist := math.Sqrt(sq(feet[0]-c[0])+sq(feet[1]-c[1])+sq(feet[2]-c[2])) / diam
	if dist > 1 {
		return 0
	}
	i := (1 - dist) * f.seen(c, feet, 0.3, 1.8)
	if i <= 0 {
		return 0
	}
	return (i*i+i)/2*7*diam + 1
}

// blastAfter: tras la armadura y la protección (la normal cuenta 1 por nivel
// y pieza; la de explosiones, 2; hasta 20, un 4 % cada punto).
func (f *Fighter) blastAfter(raw float64, armor []screen.Slot) float64 {
	var a, t, epf float64
	for _, s := range armor {
		pa, pt := armorPiece(s)
		a, t = a+pa, t+pt
		epf += float64(f.enchant(s, "protection") + 2*f.enchant(s, "blast_protection"))
	}
	return afterArmor(raw, a, t) * (1 - math.Min(20, epf)/25)
}

func (f *Fighter) myArmorSlots() []screen.Slot {
	inv, _ := f.inventory()
	return []screen.Slot{inv[5], inv[6], inv[7], inv[8]}
}

func theirArmorSlots(e entities.Entity) []screen.Slot {
	var out []screen.Slot
	for i := 2; i <= 5; i++ {
		out = append(out, screen.Slot(e.Equipment[i]))
	}
	return out
}

// blastTrade: lo que una explosión de power en c le quita a t y al bot.
func (f *Fighter) blastTrade(c [3]float64, power float64, t *foe) (him, me float64) {
	p := f.d.Player.Position()
	f.ex.blastSkip = world.BlockPos{X: math.MinInt32}
	if power == anchorPower { // (el ancla desaparece al estallar: no tapa nada)
		f.ex.blastSkip = cellOf(c[0], c[1], c[2])
	}
	him = f.blastAfter(f.blastRaw(c, power, [3]float64{t.e.X, t.e.Y, t.e.Z}), theirArmorSlots(t.e))
	me = f.blastAfter(f.blastRaw(c, power, [3]float64{p.X, p.Y, p.Z}), f.myArmorSlots())
	return
}

// worthBlast: la explosión compensa: el bot no muere (con margen) y a él le
// quita bastante más (o le mata).
func (f *Fighter) worthBlast(him, me float64, t *foe, st basic.Status) bool {
	mine := float64(st.Health) + absorption(st)
	if him >= health(t.e) { // le mata: a cambio de quedarse con 6 o más
		return me <= mine-6
	}
	return me <= 0.4*mine && him >= 6 && him >= 1.6*me
}

// crystals son los cristales del End que hay cerca de (x, y, z).
func (f *Fighter) crystals(x, y, z, r float64) []entities.Entity {
	var out []entities.Entity
	for _, e := range f.d.Entities.Nearby(x, y, z, r) {
		if e.Type == "minecraft:end_crystal" {
			out = append(out, e)
		}
	}
	return out
}

// crystalStep: romper un cristal que compense, o poner uno (y la obsidiana
// debajo si hace falta) donde compense. Dice si el paso se fue en eso.
func (f *Fighter) crystalStep(ctx context.Context, t *foe, st basic.Status) bool {
	// la obsidiana recién puesta: el cristal encima en cuanto se vea, sin
	// volver a la espada entre medias (contra una persona: obsidiana, espada,
	// obsidiana… 25 veces en 12 s, y cada cambio reinicia el golpe)
	if f.steps < f.ex.obbyUntil {
		b := f.ex.obby
		if f.d.Hands.Busy() || !strings.HasPrefix(f.blockName(b), "minecraft:obsidian") {
			f.setPlan(plan{mode: acting, target: t.id})
			return true
		}
		f.ex.obbyUntil = 0
		if slot, ok := f.slotOf("minecraft:end_crystal"); ok && f.airAt(world.BlockPos{X: b.X, Y: b.Y + 1, Z: b.Z}) && !f.crystalBlocked(b, t.e) && f.holdChecked(ctx, slot, "minecraft:end_crystal") {
			f.setPlan(plan{mode: acting, target: t.id})
			f.d.Hands.PlaceOn(world.BlockPos{X: b.X, Y: b.Y + 1, Z: b.Z}, b, f.busy(""))
			f.ex.blastNext = f.steps + 4
			f.logf("cristal sobre la obsidiana, junto a %s", t.name)
			return true
		}
	}
	if f.d.Hands.Busy() {
		return false
	}
	p := f.d.Player.Position()
	// los que ya hay (suyos o del bot), al alcance del golpe
	for _, c := range f.crystals(t.e.X, t.e.Y, t.e.Z, 8) {
		if boxDistance(p, c) > reach+0.3 {
			continue
		}
		him, me := f.blastTrade([3]float64{c.X, c.Y, c.Z}, crystalPower, t)
		if !f.worthBlast(him, me, t, st) {
			continue
		}
		id := c.ID
		f.d.Hands.Attack(id, func() (entities.Entity, bool) { return f.d.Entities.Get(id) }, nil)
		f.logf("rompo el cristal: a %s le quita ~%.0f, a mí ~%.0f", t.name, him, me)
		f.ex.blastNext = f.steps + 10
		return true
	}
	if f.steps < f.ex.blastNext || !f.hasItem("minecraft:end_crystal") {
		return false
	}
	_, obby := f.slotOf("minecraft:obsidian")
	feet := cellOf(t.e.X, t.e.Y, t.e.Z)
	type cand struct {
		base  world.BlockPos
		place bool // poner obsidiana en base antes
		score float64
		him   float64
	}
	best := cand{score: math.Inf(-1)}
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			for dy := -1; dy <= 0; dy++ {
				b := world.BlockPos{X: feet.X + dx, Y: feet.Y + dy, Z: feet.Z + dz}
				n := f.blockName(b)
				onBase := strings.HasPrefix(n, "minecraft:obsidian") || strings.HasPrefix(n, "minecraft:bedrock")
				place := false
				if !onBase {
					// obsidiana en una celda libre con suelo debajo, que no pise nadie
					if !obby || f.steps < f.ex.obbyNext || dy != 0 || !f.replaceable(b) || !f.solid(world.BlockPos{X: b.X, Y: b.Y - 1, Z: b.Z}) || f.cellHasBody(b, t.e) {
						continue
					}
					place = true
				}
				up1, up2 := world.BlockPos{X: b.X, Y: b.Y + 1, Z: b.Z}, world.BlockPos{X: b.X, Y: b.Y + 2, Z: b.Z}
				if !f.airAt(up1) || !f.airAt(up2) || f.crystalBlocked(b, t.e) || f.eyeDist(b) > eyeReach {
					continue
				}
				c := [3]float64{float64(b.X) + 0.5, float64(b.Y + 1), float64(b.Z) + 0.5}
				// (romperlo desde aquí: si no, ponerlo no sirve; y el daño, el de aquí)
				if math.Sqrt(sq(c[0]-p.X)+sq(c[1]+1-(p.Y+1.62))+sq(c[2]-p.Z)) > reach+1 {
					continue
				}
				him, me := f.blastTrade(c, crystalPower, t)
				if !f.worthBlast(him, me, t, st) {
					continue
				}
				if s := him - 1.6*me; s > best.score {
					best = cand{base: b, place: place, score: s, him: him}
				}
			}
		}
	}
	if math.IsInf(best.score, -1) {
		return false
	}
	f.stopMoving()
	f.setPlan(plan{mode: acting, target: t.id})
	b := best.base
	if best.place {
		slot, _ := f.slotOf("minecraft:obsidian")
		if !f.holdChecked(ctx, slot, "minecraft:obsidian") {
			return true
		}
		below := world.BlockPos{X: b.X, Y: b.Y - 1, Z: b.Z}
		f.d.Hands.PlaceOn(b, below, f.busy("la obsidiana no se puso"))
		f.logf("obsidiana junto a %s para el cristal", t.name)
		f.ex.obby, f.ex.obbyUntil, f.ex.obbyNext = b, f.steps+12, f.steps+20
		return true                  // (el cristal, el paso siguiente: con la obsidiana ya puesta)
	}
	slot, _ := f.slotOf("minecraft:end_crystal")
	if !f.holdChecked(ctx, slot, "minecraft:end_crystal") {
		return true
	}
	f.d.Hands.PlaceOn(world.BlockPos{X: b.X, Y: b.Y + 1, Z: b.Z}, b, f.busy(""))
	f.ex.blastNext = f.steps + 4
	f.logf("cristal junto a %s (le quitaría ~%.0f)", t.name, best.him)
	return true
}

func (f *Fighter) airAt(c world.BlockPos) bool {
	s, ok := f.d.World.BlockAt(c)
	return ok && len(block.CollisionShape(s)) == 0 && !strings.Contains(world.StateString(s), "water") && !strings.Contains(world.StateString(s), "lava")
}

// cellHasBody: el cuerpo de e o del bot ocupa algo de la celda c.
func (f *Fighter) cellHasBody(c world.BlockPos, e entities.Entity) bool {
	p := f.d.Player.Position()
	for _, b := range [][3]float64{{e.X, e.Y, e.Z}, {p.X, p.Y, p.Z}} {
		if b[0]+0.3 > float64(c.X) && b[0]-0.3 < float64(c.X+1) && b[2]+0.3 > float64(c.Z) && b[2]-0.3 < float64(c.Z+1) &&
			b[1]+1.8 > float64(c.Y) && b[1] < float64(c.Y+1) {
			return true
		}
	}
	return false
}

// crystalBlocked: el hueco del cristal sobre b (de b+1 a b+3) lo ocupa alguien.
func (f *Fighter) crystalBlocked(b world.BlockPos, e entities.Entity) bool {
	p := f.d.Player.Position()
	for _, q := range [][3]float64{{e.X, e.Y, e.Z}, {p.X, p.Y, p.Z}} {
		if q[0]+0.3 > float64(b.X) && q[0]-0.3 < float64(b.X+1) && q[2]+0.3 > float64(b.Z) && q[2]-0.3 < float64(b.Z+1) &&
			q[1]+1.8 > float64(b.Y+1) && q[1] < float64(b.Y+3) {
			return true
		}
	}
	return false
}

// crystalThreat: un cristal cerca del bot que le quitaría bastante y que el
// bot no va a romper: dónde está (para el escudo).
func (f *Fighter) crystalThreat(t *foe, st basic.Status) (entities.Entity, bool) {
	p := f.d.Player.Position()
	for _, c := range f.crystals(p.X, p.Y, p.Z, 8) {
		me := f.blastAfter(f.blastRaw([3]float64{c.X, c.Y, c.Z}, crystalPower, [3]float64{p.X, p.Y, p.Z}), f.myArmorSlots())
		if me >= 4 {
			return c, true
		}
	}
	return entities.Entity{}, false
}

// anchorStep: el ancla de reaparición (fuera del Nether explota): ponerla
// junto a él, cargarla con piedra luminosa y usarla. Dice si el paso se fue en eso.
func (f *Fighter) anchorStep(ctx context.Context, t *foe, st basic.Status) bool {
	if f.d.Hands.Busy() {
		return false
	}
	if f.d.Nether != nil && f.d.Nether() || string(f.d.Player.Spawn.Dimension) == "minecraft:the_nether" {
		return false // (en el Nether el ancla no explota: guarda la reaparición)
	}
	c := f.ex.anchor
	center := [3]float64{float64(c.X) + 0.5, float64(c.Y) + 0.5, float64(c.Z) + 0.5}
	placed := strings.HasPrefix(f.blockName(c), "minecraft:respawn_anchor")
	if f.ex.anchorPhase == 1 && !placed && f.steps-f.ex.anchorAt < 12 {
		return true // (a que el servidor la ponga)
	}
	if f.ex.anchorPhase > 0 && (!placed || f.steps-f.ex.anchorAt > 100 || f.eyeDist(c) > eyeReach) {
		f.ex.anchorPhase = 0
	}
	switch f.ex.anchorPhase {
	case 1: // puesta: cargarla
		slot, ok := f.slotOf("minecraft:glowstone")
		if !ok || !f.holdChecked(ctx, slot, "minecraft:glowstone") {
			f.ex.anchorPhase = 0
			return false
		}
		f.stopMoving()
		f.setPlan(plan{mode: acting, target: t.id})
		f.d.Hands.UseBlock(c, f.busy("el ancla no se cargó"))
		f.ex.anchorPhase, f.ex.anchorAt = 2, f.steps
		return true
	case 2: // cargada (se ve en su estado): usarla con otra cosa, si compensa
		if !strings.Contains(f.blockName(c), "charges=") || strings.Contains(f.blockName(c), "charges=0") {
			return f.steps-f.ex.anchorAt < 20 // (aún sin la respuesta)
		}
		him, me := f.blastTrade(center, anchorPower, t)
		if !f.worthBlast(him, me, t, st) {
			return false // (se queda cargada: si él se acerca, compensará)
		}
		slot := f.bestArm()
		inv, _ := f.inventory()
		if nameOf(inv[slot]) == "minecraft:glowstone" || !f.holdChecked(ctx, slot, nameOf(inv[slot])) {
			return false
		}
		f.stopMoving()
		f.setPlan(plan{mode: acting, target: t.id})
		f.d.Hands.UseBlock(c, f.busy("el ancla no explotó"))
		f.ex.anchorPhase = 0
		f.ex.blastNext = f.steps + 20
		f.logf("¡ancla! a %s le quita ~%.0f, a mí ~%.0f", t.name, him, me)
		return true
	}
	if f.steps < f.ex.blastNext || !f.hasItem("minecraft:respawn_anchor") || !f.hasItem("minecraft:glowstone") {
		return false
	}
	feet := cellOf(t.e.X, t.e.Y, t.e.Z)
	best, bestScore := world.BlockPos{}, math.Inf(-1)
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			for dy := 0; dy <= 1; dy++ {
				b := world.BlockPos{X: feet.X + dx, Y: feet.Y + dy, Z: feet.Z + dz}
				if !f.replaceable(b) || f.cellHasBody(b, t.e) || f.eyeDist(b) > eyeReach {
					continue
				}
				against, ok := f.againstFor(b)
				if !ok {
					continue
				}
				_ = against
				him, me := f.blastTrade([3]float64{float64(b.X) + 0.5, float64(b.Y) + 0.5, float64(b.Z) + 0.5}, anchorPower, t)
				if !f.worthBlast(him, me, t, st) {
					continue
				}
				if s := him - 1.6*me; s > bestScore {
					best, bestScore = b, s
				}
			}
		}
	}
	if math.IsInf(bestScore, -1) {
		return false
	}
	slot, _ := f.slotOf("minecraft:respawn_anchor")
	if !f.holdChecked(ctx, slot, "minecraft:respawn_anchor") {
		return false
	}
	against, _ := f.againstFor(best)
	f.stopMoving()
	f.setPlan(plan{mode: acting, target: t.id})
	f.d.Hands.PlaceOn(best, against, f.busy("el ancla no se puso"))
	f.ex.anchor, f.ex.anchorPhase, f.ex.anchorAt = best, 1, f.steps
	f.logf("ancla junto a %s", t.name)
	return true
}

// --- caña, perla, agua, telaraña, propulsión ---

// myBobber: el corcho de la caña del bot, si está fuera.
func (f *Fighter) myBobber() (entities.Entity, bool) {
	p := f.d.Player.Position()
	for _, e := range f.d.Entities.Nearby(p.X, p.Y, p.Z, 40) {
		if e.Type == "minecraft:fishing_bobber" && e.SpawnData == int32(f.d.Player.Login.PlayerID) {
			return e, true
		}
	}
	return entities.Entity{}, false
}

// hookedOn: el corcho tiene enganchado a id.
func hookedOn(b entities.Entity, id int32) bool {
	v, ok := b.Data[entitydata.FishingHookHookedEntity]
	if !ok {
		return false
	}
	switch n := v.Value.(type) {
	case *pk.VarInt:
		return int32(*n) == id+1
	case *pk.Int:
		return int32(*n) == id+1
	}
	return false
}

// ballisticG: el ángulo para llegar a h en horizontal y dy en vertical con
// esa velocidad, gravedad y rozamiento por tick (el tiro bajo), y los ticks.
func ballisticG(h, dy, speed, g, drag float64) (pitch float64, ticks int, ok bool) {
	best := math.Inf(1)
	for a := -40.0; a <= 70; a += 0.5 {
		r := a * math.Pi / 180
		vh, vy := speed*math.Cos(r), speed*math.Sin(r)
		x, y := 0.0, 0.0
		for n := 1; n <= 200; n++ {
			x, y = x+vh, y+vy
			vh, vy = vh*drag, vy*drag-g
			if x >= h {
				if e := math.Abs(y - dy); e < best && e < 0.8 {
					best, pitch, ticks, ok = e, -a, n, true
				}
				break
			}
			if vy < 0 && y < dy-3 {
				break
			}
		}
		if ok && best < 0.3 {
			return
		}
	}
	return
}

// rodStep: la caña. A uno que se va (o al que no se llega) a 5-18: se lanza;
// enganchado, se recoge (le trae); si no engancha en 2 s, se recoge igual.
func (f *Fighter) rodStep(ctx context.Context, t *foe) bool {
	if f.steps-f.ex.rodReel < 15 && f.ex.rodReel > 0 {
		return false // (recogida: el corcho tarda en irse)
	}
	if b, out := f.myBobber(); out || f.ex.rodAt > 0 {
		age := f.steps - f.ex.rodAt
		if !out && age > 10 {
			f.ex.rodAt = 0 // (se perdió)
			return false
		}
		if out && (hookedOn(b, t.id) || age > 40) {
			if hookedOn(b, t.id) {
				f.logf("enganchado: tiro de %s", t.name)
			}
			inv, h := f.inventory()
			if nameOf(inv[slotHotbar+h]) != "minecraft:fishing_rod" {
				f.ex.rodAt = 0 // (ya no la tiene en la mano: se recoge sola)
				return false
			}
			p := f.d.Player.Position()
			f.d.Hands.UseItemLooking(p.Yaw, p.Pitch, f.busy(""))
			f.ex.rodAt, f.ex.rodReel = 0, f.steps
			return true
		}
		return age < 45 // (con la caña fuera, a esperar)
	}
	if f.steps < f.ex.rodNext || t.dist < 5 || t.dist > 18 || !f.clearShot(t.e) {
		return false
	}
	p := f.d.Player.Position()
	flat := math.Max(math.Hypot(t.e.X-p.X, t.e.Z-p.Z), 0.1)
	away := (f.t.seenVel[0]*(t.e.X-p.X) + f.t.seenVel[1]*(t.e.Z-p.Z)) / flat
	// (se va, no se avanza hacia él, o se le ve sin camino recto: un hueco, agua)
	stalled := f.chaseID == t.id && f.steps-f.chaseAt > 60 || !f.d.Walker.Direct(p, t.e.X, t.e.Z) && math.Abs(t.e.Y-p.Y) < 3
	if away < 0.1 && !stalled {
		return false
	}
	slot, ok := f.slotOf("minecraft:fishing_rod")
	if !ok || !f.holdChecked(ctx, slot, "minecraft:fishing_rod") {
		return false
	}
	ex, ey, ez := p.X, p.Y+1.62, p.Z
	pitch, ticks, ok := ballisticG(flat, t.e.Y+1-ey, 1.1, 0.03, 0.92)
	if !ok {
		return false
	}
	lx, lz := t.e.X+f.t.seenVel[0]*float64(ticks), t.e.Z+f.t.seenVel[1]*float64(ticks)
	pitch, _, _ = ballisticG(math.Hypot(lx-ex, lz-ez), t.e.Y+1-ey, 1.1, 0.03, 0.92)
	f.stopMoving()
	f.setPlan(plan{mode: acting, target: t.id})
	f.d.Hands.UseItemLooking(float32(-math.Atan2(lx-ex, lz-ez)*180/math.Pi), float32(pitch), f.busy(""))
	f.ex.rodAt, f.ex.rodNext = f.steps, f.steps+rodEvery
	f.logf("caña a %s (a %.0f, se va)", t.name, t.dist)
	return true
}

// pearlChase: la perla ofensiva. A uno que se va lejos (más de 14 y
// alejándose) o al que no se llega en 3 s, al lado suyo.
func (f *Fighter) pearlChase(ctx context.Context, t *foe, st basic.Status) bool {
	if f.steps < f.ex.pearlNext || st.Health < 12 || t.dist < 10 || t.dist > 60 || !f.clearShot(t.e) {
		return false
	}
	p := f.d.Player.Position()
	flat := math.Max(math.Hypot(t.e.X-p.X, t.e.Z-p.Z), 0.1)
	away := (f.t.seenVel[0]*(t.e.X-p.X) + f.t.seenVel[1]*(t.e.Z-p.Z)) / flat
	stalled := f.chaseID == t.id && f.steps-f.chaseAt > 60
	if !(away > 0.12 && t.dist > 14 || stalled) {
		return false
	}
	slot, ok := f.slotOf("minecraft:ender_pearl")
	if !ok {
		return false
	}
	ex, ey, ez := p.X, p.Y+1.62-0.1, p.Z
	// a su lado (2 bloques antes, del lado del bot), donde estará al caer
	_, ticks, ok := ballisticG(flat, t.e.Y-ey, pearlSpeed, pearlGravity, 0.99)
	if !ok {
		return false
	}
	lx, lz := t.e.X+f.t.seenVel[0]*float64(ticks), t.e.Z+f.t.seenVel[1]*float64(ticks)
	h := math.Hypot(lx-ex, lz-ez)
	pitch, _, ok := ballisticG(math.Max(h-2, 1), t.e.Y-ey, pearlSpeed, pearlGravity, 0.99)
	if !ok || !f.holdChecked(ctx, slot, "minecraft:ender_pearl") {
		return false
	}
	f.ex.pearlNext = f.steps + pearlEvery
	f.d.Hands.UseItemLooking(float32(-math.Atan2(lx-ex, lz-ez)*180/math.Pi), float32(pitch), nil)
	f.logf("perla para alcanzar a %s (a %.0f)", t.name, t.dist)
	_ = f.d.Ctl.WaitTicks(ctx, 2)
	return true
}

// counterMace: uno con maza cayendo sobre el bot (3 o más por encima, a 4
// en horizontal): agua a los pies del bot; cae en ella y la caída no cuenta.
func (f *Fighter) counterMace(ctx context.Context, t *foe) bool {
	if f.steps-f.ex.waterAt < 40 || nameOf(screen.Slot(t.e.Equipment[0])) != "minecraft:mace" {
		return false
	}
	p := f.d.Player.Position()
	if t.e.Y-p.Y < 3 || math.Hypot(t.e.X-p.X, t.e.Z-p.Z) > 4 {
		return false
	}
	slot, ok := f.slotOf("minecraft:water_bucket")
	if !ok || !f.holdChecked(ctx, slot, "minecraft:water_bucket") {
		return false
	}
	f.ex.waterAt = f.steps
	f.markOwnFluid(cellOf(p.X, p.Y, p.Z)) // (su agua: clearFluids no se la quita)
	f.d.Hands.UseItemLooking(p.Yaw, 90, nil)
	f.logf("%s viene cayendo con la maza: agua a mis pies", t.name)
	return true
}

// escapeWeb: el bot en una telaraña: romperla (la espada la corta al momento).
func (f *Fighter) escapeWeb(ctx context.Context) bool {
	p := f.d.Player.Position()
	me := entities.Entity{X: p.X, Y: p.Y, Z: p.Z}
	if f.d.Hands.Busy() || !f.webbed(me) {
		return false
	}
	for y := int(math.Floor(p.Y)); y <= int(math.Floor(p.Y+1.79)); y++ {
		for x := int(math.Floor(p.X - 0.3)); x <= int(math.Floor(p.X+0.3)); x++ {
			for z := int(math.Floor(p.Z - 0.3)); z <= int(math.Floor(p.Z+0.3)); z++ {
				c := world.BlockPos{X: x, Y: y, Z: z}
				if !strings.HasPrefix(f.blockName(c), "minecraft:cobweb") {
					continue
				}
				if s, ok := f.findItem(func(n string, _ int) bool { return strings.HasSuffix(n, "_sword") || n == "minecraft:shears" }); ok {
					inv, _ := f.inventory()
					_ = f.holdChecked(ctx, s, nameOf(inv[s]))
				}
				f.d.Hands.Dig(c, nil)
				f.logf("estoy en una telaraña: la rompo")
				return true
			}
		}
	}
	return false
}

// riptide: en el agua (o la lluvia, que no se ve aquí) y con un tridente de
// propulsión, a por él volando: tensar 10 ticks mirándole y soltar.
func (f *Fighter) riptide(ctx context.Context, t *foe) bool {
	phys := f.d.Mover.Player()
	if phys == nil || !phys.InWater || t.dist < 6 || t.dist > 30 {
		return false
	}
	inv, _ := f.inventory()
	slot, ok := f.findItem(func(n string, i int) bool { return n == "minecraft:trident" && f.enchant(inv[i], "riptide") > 0 })
	if !ok || !f.holdChecked(ctx, slot, "minecraft:trident") {
		return false
	}
	p := f.d.Player.Position()
	yaw := float32(-math.Atan2(t.e.X-p.X, t.e.Z-p.Z) * 180 / math.Pi)
	pitch := float32(-math.Atan2(t.e.Y+1-(p.Y+1.62), math.Hypot(t.e.X-p.X, t.e.Z-p.Z)) * 180 / math.Pi)
	f.d.Hands.UseItemLooking(yaw, pitch, nil)
	_ = f.d.Ctl.WaitTicks(ctx, tridentDraw)
	a := play.PlayerAction{Action: types.PlayerActionActionReleaseUseItem}
	_ = f.d.Client.Conn.WritePacket(pk.Marshal(a.PacketID(), a))
	f.logf("propulsión con el tridente hacia %s", t.name)
	return true
}

// --- élitros y maza ---

func (f *Fighter) skyClear(x, y, z float64, up int) bool {
	c := cellOf(x, y, z)
	for dy := 2; dy <= up; dy++ {
		if f.solid(world.BlockPos{X: c.X, Y: c.Y + dy, Z: c.Z}) {
			return false
		}
	}
	return true
}

// diveStep: el picado con élitros y maza. Dice si manda en este paso.
func (f *Fighter) diveStep(ctx context.Context, t *foe, st basic.Status) bool {
	phys := f.d.Mover.Player()
	if phys == nil {
		return false
	}
	p := f.d.Player.Position()
	inv, _ := f.inventory()
	abort := func(why string) bool {
		f.logf("picado: %s", why)
		f.ex.dive = 0
		f.ex.diveNext = f.steps + diveEvery
		if nameOf(inv[slotChest]) == "minecraft:elytra" && phys.OnGround {
			f.unwearElytra(ctx)
		}
		return false
	}
	switch f.ex.dive {
	case 0:
		_, mace := f.maceSlot()
		_, rockets := f.slotOf("minecraft:firework_rocket")
		_, elytra := f.findItem(func(n string, _ int) bool { return n == "minecraft:elytra" })
		worn := nameOf(inv[slotChest]) == "minecraft:elytra"
		safe := f.hasItem("minecraft:water_bucket") || nameOf(inv[slotOffhand]) == itemTotem
		if !mace || !rockets || !(elytra || worn) || !safe || f.steps < f.ex.diveNext || st.Health < 14 || !phys.OnGround ||
			t.dist < 8 || t.dist > 60 || !f.skyClear(p.X, p.Y, p.Z, 30) || !f.skyClear(t.e.X, t.e.Y, t.e.Z, 30) {
			return false
		}
		f.logf("picado con élitros y maza contra %s", t.name)
		f.ex.dive, f.ex.diveAt = 1, f.steps
		if !worn {
			s, _ := f.findItem(func(n string, _ int) bool { return n == "minecraft:elytra" })
			if !f.holdChecked(ctx, s, "minecraft:elytra") {
				return abort("no consigo los élitros en la mano")
			}
			f.d.Hands.UseItemLooking(p.Yaw, p.Pitch, nil) // (usarlos los pone: lo del pecho a la mano)
		}
		return true
	case 1: // puestos: a los cohetes y a despegar
		if nameOf(inv[slotChest]) != "minecraft:elytra" {
			if f.steps-f.ex.diveAt > 20 {
				return abort("no me pongo los élitros")
			}
			return true
		}
		s, ok := f.slotOf("minecraft:firework_rocket")
		if !ok || !f.holdChecked(ctx, s, "minecraft:firework_rocket") {
			return abort("sin cohetes")
		}
		f.ex.dive, f.ex.diveAt = 2, f.steps
		f.stopMoving()
		f.setPlan(plan{mode: acting, target: t.id, fly: 1})
		return true
	case 2: // subiendo
		f.setPlan(plan{mode: acting, target: t.id, fly: 1})
		if f.steps-f.ex.diveAt > 200 || phys.OnGround && f.steps-f.ex.diveAt > 60 {
			return abort("no despego")
		}
		if f.glideOn.Load() && !f.boosting() && f.steps-f.ex.rocketAt > 8 && p.Y < t.e.Y+diveHeight {
			f.d.Hands.UseItemLooking(p.Yaw, -80, nil) // (casi en vertical: el picado luego va en diagonal hacia él)
			f.ex.rocketAt = f.steps
		}
		if p.Y >= t.e.Y+diveHeight && !f.boosting() {
			if m, ok := f.maceSlot(); ok {
				_ = f.hold(ctx, m)
			}
			f.ex.dive, f.ex.diveAt = 3, f.steps
		}
		return true
	case 3: // en picado hacia él
		f.setPlan(plan{mode: acting, target: t.id, fly: 2})
		if phys.OnGround || f.steps-f.ex.diveAt > 200 {
			return abort("aterricé sin golpe")
		}
		// fuera los élitros justo encima de él (sin ellos se cae en línea:
		// lo que se avanza en horizontal ya va en el impulso)
		vel := phys.Vel
		above := p.Y - (t.e.Y + 1.8)
		ticks := math.Max(1, above/math.Max(-vel.Y, 0.5))
		ahead := math.Hypot(t.e.X-(p.X+vel.X*ticks), t.e.Z-(p.Z+vel.Z*ticks))
		if above < 6 && ahead < 1.5 || above < 2.5 {
			f.unwearElytra(ctx)
			f.ex.dive, f.ex.diveAt = 4, f.steps
		}
		return true
	case 4: // sin élitros, cayendo: la maza (smashStep la usa)
		if phys.OnGround || f.steps-f.ex.diveAt > 60 {
			f.ex.dive, f.ex.diveNext = 0, f.steps+diveEvery
			return false
		}
		// sin golpe y el suelo cerca: agua debajo (si el reflejo de las caídas no lo hizo ya)
		if m, ok := f.maceSlot(); ok {
			if _, h := f.inventory(); m != slotHotbar+h {
				_ = f.hold(ctx, m)
			}
		}
		// (ya por debajo de su pecho: el golpe no llegó)
		if g, ok := f.d.Walker.Ground(int(math.Floor(p.X)), int(math.Floor(p.Z)), int(p.Y)); ok && p.Y < t.e.Y+0.8 && p.Y-float64(g.Y) < 4 && phys.Vel.Y < -0.6 && f.enemyHurt.Load() != t.id {
			if s, ok := f.slotOf("minecraft:water_bucket"); ok && f.hold(ctx, s) == nil {
				f.d.Hands.UseItemLooking(p.Yaw, 90, nil)
				f.logf("picado: fallé el golpe, agua para la caída")
				f.ex.dive, f.ex.diveNext = 0, f.steps+diveEvery
				return true
			}
		}
		inv, _ := f.inventory()
		if m, ok := f.maceSlot(); ok {
			mw := weaponOf(inv[m])
			f.setPlan(plan{mode: fighting, target: t.id, closeIn: true, weapon: mw, smash: true})
		}
		return true
	}
	return false
}

// unwearElytra: fuera los élitros, al inventario (un clic: la mano sigue
// con la maza; la coraza se la vuelve a poner gear luego).
func (f *Fighter) unwearElytra(ctx context.Context) {
	if err := f.d.Screens.Click(0, slotChest, 0, types.ContainerInputQuickMove); err != nil {
		f.logf("élitros: %v", err)
	}
}

// thinkFly: con élitros. Subiendo: saltar, abrir los élitros en el aire y
// mirar arriba (los cohetes los usa Run); en picado: mirando a él.
func (f *Fighter) thinkFly(s *control.State, bp *basic.Player, pl *plan) {
	f.lowerShield()
	e, ok := f.d.Entities.Get(pl.target)
	pos := bp.Position()
	in := control.Input{}
	switch {
	case pl.fly == 1:
		yaw := pos.Yaw
		if ok {
			yaw = float32(-math.Atan2(e.X-pos.X, e.Z-pos.Z) * 180 / math.Pi)
		}
		f.turnTo(bp, yaw, -80, 30, 30)
		if s.OnGround {
			in.Jump = true
		} else if !s.FallFlying {
			in.Jump = s.Ticks%2 == 0 // (soltar y volver a pulsar: así se abren)
		}
	case ok:
		vx, vz := f.track(e, s.Ticks)
		x, z := e.X+vx*8, e.Z+vz*8
		h := math.Hypot(x-pos.X, z-pos.Z)
		pitch := float32(math.Atan2(pos.Y-(e.Y+1), h) * 180 / math.Pi)
		f.turnTo(bp, float32(-math.Atan2(x-pos.X, z-pos.Z)*180/math.Pi), max(pitch, 35), 40, 40)
	}
	s.Input = in
}

// bestArm es el slot de la mejor arma que lleva (para soltar lo que haya en la mano).
func (f *Fighter) bestArm() int {
	slot, _, _ := f.bestWeapon(20, 0, 0, false, "")
	return slot
}

// enchantBonus: lo que suman los encantamientos de s contra un kind (filo,
// golpe a no muertos, perdición de artrópodos) por golpe.
func (f *Fighter) enchantBonus(s screen.Slot, kind string) float64 {
	b := 0.0
	if l := f.enchant(s, "sharpness"); l > 0 {
		b += 0.5*float64(l) + 0.5
	}
	if l := f.enchant(s, "smite"); l > 0 && undead[kind] {
		b += 2.5 * float64(l)
	}
	if l := f.enchant(s, "bane_of_arthropods"); l > 0 && arthropod[kind] {
		b += 2.5 * float64(l)
	}
	return b
}

var undead = map[string]bool{"zombie": true, "husk": true, "drowned": true, "zombie_villager": true, "skeleton": true, "stray": true,
	"bogged": true, "wither_skeleton": true, "phantom": true, "zoglin": true, "zombified_piglin": true, "wither": true, "skeleton_horse": true}

var arthropod = map[string]bool{"spider": true, "cave_spider": true, "silverfish": true, "endermite": true, "bee": true}

// breachFactor: la armadura que cuenta contra s (brecha: un 15 % menos por nivel).
func (f *Fighter) breachFactor(s screen.Slot) float64 {
	return math.Max(0, 1-0.15*float64(f.enchant(s, "breach")))
}
