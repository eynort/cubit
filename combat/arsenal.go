package combat

// El arsenal: lo que no es la espada, el hacha ni el arco (ver PVP.md).
//
//   - Lanza (26.2): la estocada llega de 2 a 4,5 bloques (la espada, 3) y
//     solo sale con el golpe cargado del todo. Contra uno que pega de cerca
//     se le pincha desde donde no llega y se retrocede mientras recarga. La
//     embestida (mantener el uso y correr hacia él) hace daño según lo rápido
//     que se le choca: de entrada, a uno que viene o que está quieto.
//   - Maza: un golpe cayendo más de 1,5 bloques suma 4 por bloque los 3
//     primeros, 2 los 5 siguientes y 1 el resto, y anula la caída. Con una
//     carga de viento a los pies, el bot sube unos 7 bloques y cae encima con
//     la maza: 20 o más de golpe. Sin carga, se usa en cualquier caída.
//   - Carga de viento: además del salto de la maza, tirada al rival con un
//     precipicio, lava o el vacío detrás, le echa.
//   - Cubo de lava: a sus pies (4 por golpe y ardiendo 15 s), y se recoge a
//     los 3 s (no se queda la lava suelta, y el cubo vuelve a servir).
//   - Telaraña: a sus pies; atrapado no se mueve ni salta (ni críticos ni
//     escapar), y la lanza le pincha desde fuera de su alcance.
//   - Mechero: fuego a sus pies, si no hay lava.
//   - Tridente: cuerpo a cuerpo es un arma más (9). Lanzado (a 2,5 bloques
//     por tick, 8 de daño) va con el arco: si tiene lealtad vuelve solo; sin
//     ella solo se lanza a uno al que no se llega, y se recoge después.
//   - Pociones arrojadizas de daño, veneno, lentitud y debilidad: a él.

import (
	"context"
	"math"
	"strings"
	"sync/atomic"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/level/component"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

const (
	lavaKeep     = 60  // pasos que se deja la lava antes de recogerla
	lavaEvery    = 100 // entre dos cubos de lava
	webEvery     = 30  // entre dos telarañas (de una se sale en ~1 s: otra en cuanto sale)
	windEvery    = 12  // entre dos cargas de viento (10 de espera del juego)
	splashEvery  = 60  // entre dos pociones tiradas a él
	lungeEvery   = 60  // entre dos embestidas
	smashEvery   = 60  // entre dos saltos con la carga de viento
	tridentSpeed = 2.5 // bloques por tick, lanzado
	tridentDraw  = 11  // ticks tensando (10, y uno de margen)
	splashSpeed  = 0.5 // la poción arrojadiza: bloques por tick
	theirReach   = 3.4 // lo que llega un golpe normal, de centro a centro
)

// arsenalState es lo que recuerda el arsenal (solo Run).
type arsenalState struct {
	lavaAt      map[world.BlockPos]uint64 // la lava que echó el bot, y cuándo
	lavaNext    uint64
	webNext     uint64
	windNext    uint64
	splashNext  uint64
	lungeNext   uint64
	lungeAt     uint64 // el paso en que se decidió la última
	smashNext   uint64
	smash       int    // el salto de la maza: 0 nada, 1 carga tirada, 2 en el aire
	smashAt     uint64 // el paso en que empezó la fase
	smashTarget int32
	peakY       float64     // lo más alto desde que dejó el suelo (la caída)
	spearUntil  uint64      // hasta este paso se sigue con la lanza (cambiar de arma reinicia el golpe)
	tridentOut  atomic.Bool // lanzó un tridente sin lealtad: a recogerlo (lo pone Think)
	lostOK      atomic.Bool // se puede lanzar un tridente sin lealtad (no se le llega)
}

// hitReach es lo que alcanza el arma de los ojos a su caja (con margen por el retraso).
func (w weapon) hitReach() float64 {
	if w.reach > 0 {
		return w.reach - 0.2
	}
	return reach
}

// findItem: dónde lleva algo que cumple ok (la barra primero).
func (f *Fighter) findItem(ok func(name string, s int) bool) (int, bool) {
	inv, _ := f.inventory()
	for i := slotHotbar + 8; i >= slotMainStart; i-- {
		if n := nameOf(inv[i]); n != "" && ok(n, i) {
			return i, true
		}
	}
	return 0, false
}

func (f *Fighter) spearSlot() (int, bool) {
	inv, _ := f.inventory()
	return f.findItem(func(_ string, i int) bool { return weaponOf(inv[i]).stab })
}

func (f *Fighter) maceSlot() (int, bool) {
	return f.findItem(func(n string, _ int) bool { return n == "minecraft:mace" })
}

// cellOf es la celda de los pies de e.
func cellOf(x, y, z float64) world.BlockPos {
	return world.BlockPos{X: int(math.Floor(x)), Y: int(math.Floor(y + 1e-6)), Z: int(math.Floor(z))}
}

func (f *Fighter) blockName(c world.BlockPos) string {
	s, ok := f.d.World.BlockAt(c)
	if !ok {
		return ""
	}
	return world.StateString(s)
}

// webbed: e está en una telaraña (alguna celda que toca su cuerpo).
func (f *Fighter) webbed(e entities.Entity) bool {
	return f.touches(e, func(n string) bool { return strings.HasPrefix(n, "minecraft:cobweb") })
}

// touches: alguna celda que ocupa el cuerpo de e (0,6 de ancho, 1,8 de alto) cumple ok.
func (f *Fighter) touches(e entities.Entity, ok func(string) bool) bool {
	const half = 0.3
	for x := int(math.Floor(e.X - half)); x <= int(math.Floor(e.X+half)); x++ {
		for z := int(math.Floor(e.Z - half)); z <= int(math.Floor(e.Z+half)); z++ {
			for y := int(math.Floor(e.Y)); y <= int(math.Floor(e.Y+1.79)); y++ {
				if ok(f.blockName(world.BlockPos{X: x, Y: y, Z: z})) {
					return true
				}
			}
		}
	}
	return false
}

// burningIn: e está en lava o fuego.
func (f *Fighter) burningIn(e entities.Entity) bool {
	return f.touches(e, func(n string) bool {
		return strings.HasPrefix(n, "minecraft:lava") || strings.HasPrefix(n, "minecraft:fire")
	})
}

// ownLava: la lava de c la echó el bot hace poco (no se recoge aún).
func (f *Fighter) ownLava(c world.BlockPos) bool {
	at, ok := f.ars.lavaAt[c]
	return ok && f.steps-at < lavaKeep
}

// lineTo: de los ojos a (x, y, z) no hay bloques (salvo skip).
func (f *Fighter) lineTo(x, y, z float64, skip world.BlockPos) bool {
	p := f.d.Player.Position()
	ax, ay, az := p.X, p.Y+1.62, p.Z
	n := int(math.Ceil(math.Sqrt(sq(x-ax)+sq(y-ay)+sq(z-az)) / 0.2))
	for i := 1; i < n; i++ {
		k := float64(i) / float64(n)
		c := world.BlockPos{X: int(math.Floor(ax + (x-ax)*k)), Y: int(math.Floor(ay + (y-ay)*k)), Z: int(math.Floor(az + (z-az)*k))}
		if c != skip && f.solid(c) {
			return false
		}
	}
	return true
}

// groundUnder: e está de pie (bloque macizo bajo los pies, celda libre) y dónde.
func (f *Fighter) groundUnder(e entities.Entity) (feet world.BlockPos, ok bool) {
	feet = cellOf(e.X, e.Y, e.Z)
	below := world.BlockPos{X: feet.X, Y: feet.Y - 1, Z: feet.Z}
	n := f.blockName(feet)
	free := n == "" || strings.HasPrefix(n, "minecraft:air") || strings.HasPrefix(n, "minecraft:cave_air") || f.replaceable(feet) && !strings.Contains(n, "water") && !strings.Contains(n, "lava")
	return feet, free && f.solid(below) && e.Y-float64(feet.Y) < 0.2
}

// arsenal: lava, telaraña, fuego, carga de viento y pociones contra un
// jugador. Dice si el paso se fue en eso.
func (f *Fighter) arsenal(ctx context.Context, t *foe, st basic.Status) bool {
	if f.d.Hands.Busy() || f.d.Walker.Building() || f.ars.smash > 0 || f.lungeOn() {
		return false
	}
	return f.pourLava(ctx, t) || f.castWeb(ctx, t, st) || f.setFire(ctx, t) || f.windPush(ctx, t) || f.throwHarm(ctx, t, st)
}

// pourLava: un cubo de lava a sus pies. Él a tiro (4,4 de los ojos), de pie,
// no más alto que el bot (la lava corre hacia abajo) y a 2,2 o más (que no
// llegue al bot); ni en agua ni ya ardiendo en ella.
func (f *Fighter) pourLava(ctx context.Context, t *foe) bool {
	if f.steps < f.ars.lavaNext || f.burningIn(t.e) {
		return false
	}
	slot, ok := f.slotOf("minecraft:lava_bucket")
	if !ok {
		return false
	}
	feet, ok := f.groundUnder(t.e)
	p := f.d.Player.Position()
	if !ok || t.e.Y > p.Y+0.6 || math.Hypot(float64(feet.X)+0.5-p.X, float64(feet.Z)+0.5-p.Z) < 2.2 || f.eyeDist(feet) > eyeReach {
		return false
	}
	below := world.BlockPos{X: feet.X, Y: feet.Y - 1, Z: feet.Z}
	tx, ty, tz := float64(feet.X)+0.5, float64(feet.Y)-0.02, float64(feet.Z)+0.5
	if !f.lineTo(tx, ty, tz, below) {
		return false
	}
	if !f.holdChecked(ctx, slot, "minecraft:lava_bucket") {
		return false
	}
	if f.ars.lavaAt == nil {
		f.ars.lavaAt = map[world.BlockPos]uint64{}
	}
	f.ars.lavaAt[feet] = f.steps
	f.ars.lavaNext = f.steps + lavaEvery
	f.stopMoving()
	f.setPlan(plan{mode: acting, target: t.id})
	f.handsBusy.Store(true)
	f.d.Hands.UseItemToward(tx, ty, tz, func(act.Use) { f.handsBusy.Store(false) })
	f.logf("lava a los pies de %s (%d %d %d)", t.name, feet.X, feet.Y, feet.Z)
	return true
}

// castWeb: una telaraña a sus pies: atrapado ni se acerca ni se va ni salta
// (ni críticos). Cambiar de objeto cuesta la carga del golpe (medido: con
// espada y telarañas, peor que con la espada sola), así que solo cuando
// paga: con la lanza (se le pincha desde donde no llega) o con poca vida y
// algo que comer (atrapado, a comer sin que llegue).
func (f *Fighter) castWeb(ctx context.Context, t *foe, st basic.Status) bool {
	if f.steps < f.ars.webNext || f.webbed(t.e) || t.dist < 2 {
		return false
	}
	_, spear := f.spearSlot()
	_, gap := pickFood(f.foods(), 20-st.Food, true)
	if !spear && !(st.Health <= 10 && gap) {
		return false
	}
	slot, ok := f.slotOf("minecraft:cobweb")
	if !ok {
		return false
	}
	// donde va a estar cuando llegue (la ida y vuelta, y el cambio de mano):
	// a uno que viene corriendo, delante de él
	ahead := t.e
	ahead.X, ahead.Z = t.e.X+f.t.seenVel[0]*8, t.e.Z+f.t.seenVel[1]*8
	feet, ok := f.groundUnder(ahead)
	p := f.d.Player.Position()
	below := world.BlockPos{X: feet.X, Y: feet.Y - 1, Z: feet.Z}
	// (la mano mide hasta el bloque contra el que se pone: el de debajo)
	if !ok || f.eyeDist(below) > eyeReach-0.1 || math.Hypot(float64(feet.X)+0.5-p.X, float64(feet.Z)+0.5-p.Z) < 1.5 {
		return false
	}
	if !f.lineTo(float64(feet.X)+0.5, float64(feet.Y)-0.02, float64(feet.Z)+0.5, below) {
		return false
	}
	if !f.holdChecked(ctx, slot, "minecraft:cobweb") {
		return false
	}
	f.ars.webNext = f.steps + webEvery
	f.stopMoving()
	f.setPlan(plan{mode: acting, target: t.id})
	f.handsBusy.Store(true)
	f.d.Hands.PlaceOn(feet, below, func(u act.Use) {
		f.handsBusy.Store(false)
		if u.Err != nil {
			f.logf("la telaraña no se puso: %v", u.Err)
			f.ars.webNext = 0 // (otra vez, ya)
		}
	})
	f.logf("telaraña a los pies de %s", t.name)
	return true
}

// setFire: fuego a sus pies con el mechero (sin lava, o gastada).
func (f *Fighter) setFire(ctx context.Context, t *foe) bool {
	if f.steps < f.ars.lavaNext || f.burningIn(t.e) || f.hasItem("minecraft:lava_bucket") {
		return false
	}
	slot, ok := f.slotOf("minecraft:flint_and_steel")
	if !ok {
		return false
	}
	feet, ok := f.groundUnder(t.e)
	p := f.d.Player.Position()
	below := world.BlockPos{X: feet.X, Y: feet.Y - 1, Z: feet.Z}
	if !ok || f.webbed(t.e) || math.Hypot(float64(feet.X)+0.5-p.X, float64(feet.Z)+0.5-p.Z) < 1.8 || f.eyeDist(below) > eyeReach-0.2 {
		return false
	}
	if !f.lineTo(float64(feet.X)+0.5, float64(feet.Y)-0.02, float64(feet.Z)+0.5, below) {
		return false
	}
	if !f.holdChecked(ctx, slot, "minecraft:flint_and_steel") {
		return false
	}
	f.ars.lavaNext = f.steps + lavaEvery
	f.stopMoving()
	f.setPlan(plan{mode: acting, target: t.id})
	f.handsBusy.Store(true)
	f.d.Hands.PlaceOn(feet, below, func(act.Use) { f.handsBusy.Store(false) })
	f.logf("fuego a los pies de %s", t.name)
	return true
}

// windPush: con un precipicio, lava o el vacío detrás de él (lo que hay a
// 2-3 bloques en la dirección contraria al bot), una carga de viento: le echa.
func (f *Fighter) windPush(ctx context.Context, t *foe) bool {
	if f.steps < f.ars.windNext || t.dist < 2.5 || t.dist > 10 {
		return false
	}
	slot, ok := f.slotOf("minecraft:wind_charge")
	if !ok {
		return false
	}
	p := f.d.Player.Position()
	flat := math.Hypot(t.e.X-p.X, t.e.Z-p.Z)
	if flat < 0.5 {
		return false
	}
	ux, uz := (t.e.X-p.X)/flat, (t.e.Z-p.Z)/flat
	if f.d.Walker.Safe(basic.Position{X: t.e.X, Y: t.e.Y, Z: t.e.Z}, ux, uz) || !f.clearShot(t.e) {
		return false
	}
	if !f.holdChecked(ctx, slot, "minecraft:wind_charge") {
		return false
	}
	f.ars.windNext = f.steps + windEvery
	f.stopMoving()
	f.setPlan(plan{mode: acting, target: t.id})
	f.handsBusy.Store(true)
	// al suelo, un bloque delante de él (sin gravedad, a 1,5 por tick:
	// adelantado lo que tarda): la ráfaga le empuja hacia atrás y arriba; en
	// el cuerpo, casi solo arriba
	lead := t.dist / 1.5
	gx, gz := t.e.X+f.t.seenVel[0]*lead-ux*0.9, t.e.Z+f.t.seenVel[1]*lead-uz*0.9
	f.d.Hands.UseItemToward(gx, math.Floor(t.e.Y+1e-6)+0.05, gz, func(act.Use) { f.handsBusy.Store(false) })
	f.logf("carga de viento a %s: tiene el vacío detrás", t.name)
	return true
}

// throwHarm: una arrojadiza mala a él (daño, veneno, lentitud, debilidad), de 4 a 6 bloques.
func (f *Fighter) throwHarm(ctx context.Context, t *foe, st basic.Status) bool {
	if f.steps < f.ars.splashNext || t.dist < 4 || t.dist > 6.5 {
		return false
	}
	ps := f.potions()
	for _, eff := range []string{"harming", "poison", "slowness", "weakness"} {
		pt, ok := pickPotion(ps, eff, true)
		if !ok {
			continue
		}
		p := f.d.Player.Position()
		ex, ey, ez := p.X, p.Y+1.62-0.1, p.Z
		// la poción sale 20° por encima de la mirada (ThrowablePotionItem)
		pitch, ticks, ok := ballistic(math.Hypot(t.e.X-ex, t.e.Z-ez), t.e.Y+0.5-ey, splashSpeed)
		if !ok {
			continue
		}
		lx, lz := t.e.X+f.t.seenVel[0]*float64(ticks), t.e.Z+f.t.seenVel[1]*float64(ticks)
		pitch, _, ok = ballistic(math.Hypot(lx-ex, lz-ez), t.e.Y+0.5-ey, splashSpeed)
		if !ok || pitch+20 > 90 || !f.clearShot(t.e) {
			continue
		}
		if !f.holdChecked(ctx, pt.slot, "minecraft:splash_potion") {
			return false
		}
		f.ars.splashNext = f.steps + splashEvery
		yaw := float32(-math.Atan2(lx-ex, lz-ez) * 180 / math.Pi)
		f.d.Hands.UseItemLooking(yaw, float32(pitch+20), nil)
		f.logf("poción de %s a %s", pt.kind, t.name)
		_ = f.d.Ctl.WaitTicks(ctx, 2)
		return true
	}
	return false
}

// smashStep: la maza. En el aire cayendo encima de él, la maza a la mano; de
// pie, a 1-3 bloques y con cielo libre, el salto con la carga de viento.
// Mientras dura, manda (dice si el paso se fue en eso).
func (f *Fighter) smashStep(ctx context.Context, t *foe, st basic.Status) bool {
	mace, hasMace := f.maceSlot()
	phys := f.d.Mover.Player()
	if !hasMace || phys == nil {
		f.ars.smash = 0
		return false
	}
	p := f.d.Player.Position()
	if phys.OnGround {
		f.ars.peakY = p.Y
	} else {
		f.ars.peakY = math.Max(f.ars.peakY, p.Y)
	}
	flat := math.Hypot(t.e.X-p.X, t.e.Z-p.Z)
	inv, held := f.inventory()
	holdMace := func() {
		if nameOf(inv[slotHotbar+held]) != "minecraft:mace" {
			_ = f.hold(ctx, mace)
		}
	}
	airPlan := func() {
		mw := weaponOf(inv[mace])
		mw.mace = true
		f.stopMoving()
		f.setPlan(plan{mode: fighting, target: t.id, closeIn: true, weapon: mw, smash: true})
	}
	switch f.ars.smash {
	case 1: // tirada la carga: ¿subió?
		if phys.Vel.Y > 0.3 {
			f.ars.smash, f.ars.smashAt = 2, f.steps
			holdMace()
			airPlan()
			return true
		}
		if f.steps-f.ars.smashAt > 8 {
			f.logf("la carga de viento no me subió")
			f.ars.smash = 0
			return false
		}
		return true
	case 2: // en el aire: la maza, y a caer encima
		if phys.OnGround && f.steps-f.ars.smashAt > 4 || f.steps-f.ars.smashAt > 80 {
			f.ars.smash = 0
			return false
		}
		holdMace()
		airPlan()
		return true
	}
	// cayendo (un empujón, un saliente, un salto desde arriba) y él debajo, a tiro
	if !phys.OnGround && f.ars.peakY-p.Y > 1.6 && phys.Vel.Y < 0 && flat < 4 && t.e.Y < p.Y+0.5 {
		holdMace()
		airPlan()
		return true
	}
	// el salto: de pie, él cerca y a la misma altura, cielo libre encima
	wind, hasWind := f.slotOf("minecraft:wind_charge")
	if !hasWind || !phys.OnGround || phys.InWater || f.steps < f.ars.smashNext || f.steps < f.ars.windNext ||
		flat > 3.2 || flat < 0.8 || math.Abs(t.e.Y-p.Y) > 1.2 || st.Health < 6 || f.webbed(t.e) && flat < 1.5 {
		return false
	}
	head := cellOf(p.X, p.Y, p.Z)
	for dy := 2; dy <= 9; dy++ {
		if f.solid(world.BlockPos{X: head.X, Y: head.Y + dy, Z: head.Z}) {
			return false
		}
	}
	if !f.holdChecked(ctx, wind, "minecraft:wind_charge") {
		return false
	}
	f.ars.smash, f.ars.smashAt, f.ars.smashTarget = 1, f.steps, t.id
	f.ars.smashNext, f.ars.windNext = f.steps+smashEvery, f.steps+windEvery
	f.stopMoving()
	f.setPlan(plan{mode: acting, target: t.id})
	f.d.Hands.UseItemLooking(p.Yaw, 90, nil)
	f.logf("carga de viento a los pies y maza contra %s", t.name)
	return true
}

// preferSpear: la lanza en vez de lo que diga bestWeapon cuando pegar desde
// lejos vale más que el daño: él atrapado o ardiendo (se le pincha sin que
// llegue), o perdiendo el intercambio contra uno que pega de cerca.
func (f *Fighter) preferSpear(t *foe, slot int, w weapon) (int, weapon) {
	if w.stab {
		return slot, w
	}
	s, ok := f.spearSlot()
	if !ok || t.e.Type == "minecraft:creeper" || rangedMobs[kind(t.e)] {
		return slot, w
	}
	if weaponOf(screen.Slot(t.e.Equipment[0])).stab {
		return slot, w // (otra lanza: la misma distancia; mejor el daño)
	}
	inv, _ := f.inventory()
	sw := weaponOf(inv[s])
	trapped := f.webbed(t.e) || f.burningIn(t.e)
	// pegado a él la lanza no da (menos de 2) y corre más de lo que se
	// retrocede: ahí la espada (salvo atrapado, que no se acerca)
	if !trapped && t.dist < 3 {
		f.ars.spearUntil = 0
		return slot, w
	}
	// (y si pega lo mismo o más por golpe que lo otro: además llega más lejos)
	if trapped || f.stanceNow == stanceCautious || f.stanceNow == stanceAvoid || sw.damage >= w.damage || f.steps < f.ars.spearUntil {
		if f.steps >= f.ars.spearUntil {
			f.ars.spearUntil = f.steps + 60
		}
		return s, sw
	}
	return slot, w
}

// wantLunge: la embestida con la lanza. Él a 5-14 bloques, a la misma
// altura y en línea recta, sin irse (lo que se le choca cuenta: hace falta
// 4,6 bloques por segundo, y corriendo son 5,6).
func (f *Fighter) wantLunge(t *foe) bool {
	if f.steps < f.ars.lungeNext || t.e.Type == "minecraft:creeper" {
		return false
	}
	s, ok := f.spearSlot()
	if !ok {
		return false
	}
	inv, _ := f.inventory()
	k := act.Component[*component.KineticWeapon](inv[s])
	if k == nil {
		return false
	}
	p := f.d.Player.Position()
	start := float64(k.DelayTicks)*0.28 + 2.5
	if t.dist < start || t.dist > 14 || math.Abs(t.e.Y-p.Y) > 1 || !f.d.Walker.Direct(p, t.e.X, t.e.Z) || f.d.Mover.Player() == nil || !f.d.Mover.Player().OnGround {
		return false
	}
	flat := math.Max(math.Hypot(t.e.X-p.X, t.e.Z-p.Z), 0.1)
	away := (f.t.seenVel[0]*(t.e.X-p.X) + f.t.seenVel[1]*(t.e.Z-p.Z)) / flat // lo que se aleja, bloques por tick
	// ni que se vaya (no se le alcanza a la velocidad que hace falta) ni que
	// venga corriendo: la lanza tarda en estar lista (12 ticks la de hierro,
	// y el retraso) y el choque llega antes
	return away < 0.04 && away > -0.08
}

// lungeOn: en plena embestida (Run no la corta): Think embistiendo, o
// recién decidida (Think empieza el tick siguiente).
func (f *Fighter) lungeOn() bool {
	return f.lunging.Load() || f.ars.lungeAt > 0 && f.steps-f.ars.lungeAt < 5
}

// thinkLunge: la embestida: el uso mantenido y a por él corriendo, mirándole.
// Se suelta al pasarle, al chocarle o pasado el tiempo.
func (f *Fighter) thinkLunge(s *control.State, bp *basic.Player, pl *plan, e entities.Entity, flat float64) {
	tick := s.Ticks
	// él viene corriendo antes de que la lanza esté lista: el choque llega
	// antes (sin daño) y deja al bot pegado a él. Se deja
	pos := bp.Position()
	if fl := math.Max(flat, 0.1); f.t.charging && tick-f.t.chargeFrom < 14 &&
		-(f.t.seenVel[0]*(e.X-pos.X)+f.t.seenVel[1]*(e.Z-pos.Z))/fl > 0.1 {
		f.endLunge(tick)
		f.lunging.Store(false)
		return
	}
	f.lowerShield()
	if !f.t.charging {
		if f.d.Hands.Busy() {
			return
		}
		cur := bp.Position()
		u := play.UseItem{Hand: types.InteractionHandMainHand, YRot: pk.Float(cur.Yaw), XRot: pk.Float(cur.Pitch)}
		if f.d.Client.Conn.WritePacket(pk.Marshal(u.PacketID(), u)) != nil {
			return
		}
		f.t.charging, f.t.chargeFrom = true, tick
		f.lunging.Store(true)
	}
	in := control.Input{Forward: true, Sprint: true}
	f.terrain(s, bp, &in)
	if !f.d.Walker.SprintAllowed() {
		in.Sprint = false
	}
	s.Input = in
	hit := f.enemyHurt.Swap(0) == pl.target && tick-f.t.chargeFrom > 3
	if hit || flat < 0.9 || tick-f.t.chargeFrom > 60 || !in.Forward {
		f.endLunge(tick)
	}
}

// endLunge suelta la lanza (fin de la embestida).
func (f *Fighter) endLunge(tick uint64) {
	if !f.t.charging {
		return
	}
	a := play.PlayerAction{Action: types.PlayerActionActionReleaseUseItem}
	_ = f.d.Client.Conn.WritePacket(pk.Marshal(a.PacketID(), a))
	f.t.charging = false
	f.lunging.Store(false)
	f.lastAttack.Store(tick)
}

// stab: la estocada de la lanza (lo que hace el clic izquierdo con ella:
// la acción STAB y el brazo).
func (f *Fighter) stab() {
	a := play.PlayerAction{Action: types.PlayerActionActionStab}
	if f.d.Client.Conn.WritePacket(pk.Marshal(a.PacketID(), a)) == nil {
		sw := play.Swing{Hand: types.InteractionHandMainHand}
		_ = f.d.Client.Conn.WritePacket(pk.Marshal(sw.PacketID(), sw))
	}
}

// tridentSlot: un tridente que se puede lanzar (sin propulsión, que solo va
// en el agua); con lealtad si loyalOnly.
func (f *Fighter) tridentSlot(loyalOnly bool) (int, bool) {
	inv, _ := f.inventory()
	return f.findItem(func(n string, i int) bool {
		if n != "minecraft:trident" || f.enchant(inv[i], "riptide") > 0 {
			return false
		}
		return !loyalOnly || f.enchant(inv[i], "loyalty") > 0
	})
}

// fetchTrident: sin pelea, el tridente que se lanzó (sin lealtad) está en el
// suelo: a por él (se recoge al pasar). Dice si va.
func (f *Fighter) fetchTrident() bool {
	if !f.ars.tridentOut.Load() {
		return false
	}
	if f.hasItem("minecraft:trident") {
		f.ars.tridentOut.Store(false)
		return false
	}
	p := f.d.Player.Position()
	best, bestD := entities.Entity{}, 48.0
	for _, e := range f.d.Entities.Nearby(p.X, p.Y, p.Z, 48) {
		if e.Type != "minecraft:trident" && !(e.Type == "minecraft:item" && itemEntityIs(e, "minecraft:trident")) {
			continue
		}
		if d := math.Sqrt(e.DistanceSqr(p.X, p.Y, p.Z)); d < bestD {
			best, bestD = e, d
		}
	}
	if bestD >= 48 {
		f.ars.tridentOut.Store(false)
		return false
	}
	if f.d.Walker.Status() != "walking" {
		f.logf("voy a por el tridente (a %.0f bloques)", bestD)
		f.d.Walker.Goto(cellOf(best.X, best.Y, best.Z))
	}
	return true
}

// itemEntityIs: e es un objeto en el suelo de ese tipo.
func itemEntityIs(e entities.Entity, name string) bool {
	v, ok := e.Data[entitydata.ItemEntityItem]
	if !ok {
		return false
	}
	switch it := v.Value.(type) {
	case *types.ItemStack:
		return it.Count > 0 && itemName(int32(it.Item)) == name
	}
	return false
}

// reclaimLava: sin pelea, la lava que echó el bot (ya pasado lavaKeep) y al
// alcance: se recoge con el cubo vacío (no se deja lava suelta). Dice si lo hizo.
func (f *Fighter) reclaimLava(ctx context.Context) bool {
	if len(f.ars.lavaAt) == 0 || f.d.Hands.Busy() || f.handsBusy.Load() || f.tac.fluidAt > 0 && f.steps-f.tac.fluidAt < fluidWait {
		return false
	}
	slot, ok := f.slotOf("minecraft:bucket")
	for c, at := range f.ars.lavaAt {
		if !strings.HasPrefix(f.blockName(c), "minecraft:lava") || f.steps-at > 2400 {
			delete(f.ars.lavaAt, c)
			continue
		}
		if !ok || f.steps-at < lavaKeep || f.eyeDist(c) > eyeReach {
			continue
		}
		if !f.holdChecked(ctx, slot, "minecraft:bucket") {
			return true
		}
		f.tac.fluidAt = f.steps
		f.handsBusy.Store(true)
		f.d.Hands.UseItemToward(float64(c.X)+0.5, float64(c.Y)+0.85, float64(c.Z)+0.5, func(act.Use) { f.handsBusy.Store(false) })
		f.logf("recojo mi lava (%d %d %d)", c.X, c.Y, c.Z)
		return true
	}
	return false
}
