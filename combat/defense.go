package combat

// Defenderse de lo que hace un jugador (ver PVP.md, "Defensa"):
//
//   - Lava encima: ScoopLava, para el reflejo de la lava (safety.Lava.Scoop):
//     la fuente de los pies, recogida con un cubo vacío (el bot se queda su
//     lava) o tapada con agua, mirando abajo, al instante.
//   - Un mazazo que viene: el rival con maza por encima del bot y a menos de 5
//     en horizontal (subiendo o cayendo). Con escudo, arriba y de cara a él (el
//     golpe de la maza lo para el escudo), apartándose de lado; sin escudo,
//     agua a los pies (counterMace) o fuera de donde va a caer.
//   - Encerrado (los cuatro lados tapados a la altura de los pies o de la
//     cabeza) por bloques que no son suyos y con un rival cerca: romper la
//     pared de atrás (la de su lado le deja pegarle con el pico en la mano) y
//     salir a pelear, antes de que llegue lo siguiente (lava dentro, flechas
//     por arriba).
//   - Saliendo del búnker y le pegan (o le espera pegado a la salida): deja de
//     picar y pelea, con el escudo.

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/protocol/types"
)

// ScoopLava: el reflejo de la lava pide quitar la fuente de p (la de los
// pies). Desde el Think del controlador: no espera nada (los paquetes van en
// orden: el cambio de mano llega antes que el uso). Dice si lo intenta.
func (f *Fighter) ScoopLava(p world.BlockPos) bool {
	inv, held := f.inventory()
	slot := -1
	for _, want := range []string{"minecraft:bucket", "minecraft:water_bucket"} {
		for i := slotHotbar; i <= slotHotbar+8; i++ {
			if nameOf(inv[i]) == want {
				slot = i
				break
			}
		}
		if slot < 0 {
			for i := slotMainStart; i < slotHotbar; i++ {
				if nameOf(inv[i]) == want {
					h := f.spareHotbar()
					if f.d.Screens.Click(0, i, h, types.ContainerInputSwap) == nil {
						slot = slotHotbar + h
					}
					break
				}
			}
		}
		if slot >= 0 {
			break
		}
	}
	if slot < 0 {
		return false
	}
	if slot != slotHotbar+held {
		if f.selectHotbar(slot-slotHotbar) != nil {
			return false
		}
	}
	f.lowerShield()
	f.handsBusy.Store(true)
	pos := f.d.Player.Position()
	f.d.Hands.UseItemLooking(pos.Yaw, 90, func(act.Use) { f.handsBusy.Store(false) })
	f.markOwnFluid(world.BlockPos{X: p.X, Y: p.Y, Z: p.Z})
	return true
}

// markOwnFluid: el agua o la lava de c es del bot (clearFluids no la quita
// hasta pasado lavaKeep).
func (f *Fighter) markOwnFluid(c world.BlockPos) {
	if f.ars.lavaAt == nil {
		f.ars.lavaAt = map[world.BlockPos]uint64{}
	}
	f.ars.lavaAt[c] = f.steps
}

// maceIncoming: t lleva maza y está por encima del bot, cerca en horizontal:
// un mazazo en camino (con una carga de viento, desde un saliente).
func (f *Fighter) maceIncoming(t *foe) bool {
	if nameOf(screen.Slot(t.e.Equipment[0])) != "minecraft:mace" {
		return false
	}
	p := f.d.Player.Position()
	return t.e.Y-p.Y > 1.2 && math.Hypot(t.e.X-p.X, t.e.Z-p.Z) < 5 && !t.e.OnGround
}

// defendMace: contra un mazazo en camino, el escudo de cara a él y de lado
// (Think: plan.guard con dodge). Sin escudo, el agua (counterMace) o
// apartarse de donde va a caer. Dice si el paso se fue en eso.
func (f *Fighter) defendMace(ctx context.Context, t *foe, shield bool) bool {
	if !f.maceIncoming(t) {
		return false
	}
	if shield && !f.shieldOff(0) {
		if f.plan.Load().mode != fighting || !f.plan.Load().guard {
			f.logf("%s viene con la maza desde arriba: escudo arriba y me aparto", t.name)
		}
		f.stopMoving()
		f.setPlan(plan{mode: fighting, target: t.id, closeIn: true, shield: true, guard: true, dodge: true,
			gx: t.e.X, gy: t.e.Y + 0.9, gz: t.e.Z, weapon: f.plan.Load().weapon})
		return true
	}
	if f.counterMace(ctx, t) {
		return true
	}
	// sin escudo ni agua: fuera de debajo de él (a donde no llega al caer)
	p := f.d.Player.Position()
	dx, dz := p.X-t.e.X, p.Z-t.e.Z
	n := math.Max(math.Hypot(dx, dz), 0.1)
	f.stopMoving()
	f.setPlan(plan{mode: acting, target: t.id, steer: true, steerX: p.X + dx/n*4, steerZ: p.Z + dz/n*4})
	return true
}

// trapped: el bot encerrado (los cuatro lados sin paso) por bloques que no
// puso él, con t cerca. Empieza a salir por el lado de t (bunker.go: la
// salida del búnker) y a pelear. Dice si lo hizo.
func (f *Fighter) trapped(t *foe) bool {
	if f.bk.on || t.dist > 6 || f.steps < f.ex.trapNext {
		return false
	}
	p := f.d.Player.Position()
	feet := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
	cells := bunkerCells(feet, t.e.X, t.e.Z)
	if !f.enclosed(cells) {
		return false
	}
	mine := 0
	for _, c := range cells {
		if f.myBlocks[c.pos] && f.solid(c.pos) {
			mine++
		}
	}
	if mine >= 4 {
		return false // (su búnker: lo lleva bunkerStep)
	}
	f.ex.trapNext = f.steps + 40
	// por detrás: rompiendo la de su lado, él espera pegado y pega mientras el
	// bot aún tiene el pico en la mano (medido: de 20 a 6 saliendo así). Por
	// detrás tiene que dar la vuelta, y el bot le espera con la espada y el escudo
	f.logf("%s me ha encerrado: rompo la pared de atrás y salgo a pelear", t.name)
	f.bk = bunkerState{on: true, feet: feet, since: f.steps, enemy: t.id}
	f.openWall(cells, 3)
	f.bk.thenFight, f.bk.trap = true, true
	return f.bk.on
}

// exitAmbush: saliendo del búnker, el rival pega (o espera pegado a la
// salida): deja de picar y pelea. Dice si lo dejó.
func (f *Fighter) exitAmbush(foes []foe) bool {
	if !f.bk.on || len(f.bk.opening) == 0 || f.bk.trap {
		return false
	}
	hurt := f.lastHurt.Load() > 0 && nowMilli()-f.lastHurt.Load() < 1200
	// (pegado a la salida solo cuenta con hueco ya abierto: con la pared
	// entera no llega, y es mejor acabar de abrir)
	gap := false
	for _, c := range f.bk.opened {
		if !f.solid(c) {
			gap = true
		}
	}
	for _, fo := range foes {
		if fo.e.Type != "minecraft:player" {
			continue
		}
		if fo.dist < 4.5 && (hurt || gap && fo.dist < 2.8) {
			f.logf("saliendo del búnker y %s me espera: dejo de picar y peleo", fo.name)
			enemy := fo.id
			f.quitBunker()
			if e, ok := f.d.Entities.Get(enemy); ok {
				f.Engage(enemy, f.label(e))
			}
			return true
		}
	}
	return false
}

// lavaBucketHeld: lleva un cubo de lava (lo que recogió de él: se le devuelve).
func (f *Fighter) lavaBucketHeld() bool { return f.hasItem("minecraft:lava_bucket") }

var _ = strings.HasPrefix

func nowMilli() int64 { return time.Now().UnixMilli() }
