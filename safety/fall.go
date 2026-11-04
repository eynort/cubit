package safety

import (
	"math"
	"strings"
	"sync"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/block"
	"github.com/mj41/go-mc26/protocol/types"
)

// Fall es el reflejo del cubo de agua (el "MLG" de los jugadores): cayendo de
// una altura que hace daño (vanilla: lo que pasa de 3 bloques, un corazón
// por bloque), con un cubo de agua en la barra rápida, mira abajo y, cuando
// el suelo entra en su alcance, echa el agua encima: cae en ella y la caída no
// cuenta (Entity.resetFallDistance al tocar agua). Luego recoge el agua con el
// cubo vacío (no deja agua por ahí y se queda el cubo). Un golpe que te tira
// de una torre, un puente roto, el borde de un acantilado en una pelea.
//
// Mientras lo hace manda él (como la lava): ni la pelea ni el walker tocan la
// mirada ni las teclas.
type Fall struct {
	World   *world.World
	Mover   *physics.Mover
	Screens *screen.Manager
	Hands   *act.Hands
	Logf    func(format string, args ...any)

	mu      sync.Mutex
	state   int            // fallIdle, fallPlacing, fallLanded
	water   world.BlockPos // la celda donde echó el agua
	ticks   int            // ticks en el estado
	saves   int            // caídas salvadas
	pending bool
	usedAt  int // el tick (de ticks) del último intento de recogerla           // el cubo ya se pidió a las manos (esperando a que salga)
}

const (
	fallIdle = iota
	fallPlacing
	fallLanded
)

// fallReach: hasta dónde llega el cubo, de los ojos (1.62 sobre los pies) a la
// cara de arriba del suelo, con el alcance de interacción (4.5): con los pies a
// menos de esto del suelo, se echa. Cayendo deprisa (hasta 3.9 bloques por
// tick) se está en alcance un tick o menos: el cubo va en la mano y mirando
// abajo desde antes, y el uso se pide un tick antes (sale con la posición
// del tick siguiente, que es la que mira el servidor).
const fallReach = 2.8

// Saves es cuántas caídas ha salvado con el cubo.
func (f *Fall) Saves() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saves
}

// Active: está echando o recogiendo el agua (manda él).
func (f *Fall) Active() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state != fallIdle
}

// Think mira la caída; dice si este tick lo maneja él.
func (f *Fall) Think(s *control.State, bp *basic.Player) bool {
	if f.Mover == nil || f.Hands == nil || f.Screens == nil {
		return false
	}
	p := f.Mover.Player()
	if p == nil {
		return false
	}
	pos := bp.Position()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ticks++
	switch f.state {
	case fallIdle:
		if p.OnGround || p.InWater || p.InLava || p.Vel.Y > -0.3 {
			return false
		}
		ground, h, soft, ok := f.landing(pos, p.Vel)
		if !ok || soft || h < 0.2 {
			return false // agua (o nada) donde va a caer
		}
		// el daño de la caída entera: lo caído más lo que queda, menos 3
		// (un corazón ya es daño: cualquier caída que quite vida)
		if dmg := math.Ceil(p.FallDistance + h - 3); dmg < 1 {
			return false
		}
		if !f.bucketToHand() {
			return false
		}
		f.state, f.ticks, f.pending = fallPlacing, 0, false
		f.water = world.BlockPos{X: ground.X, Y: ground.Y + 1, Z: ground.Z}
		f.logf("cayendo %.0f bloques: cubo de agua", p.FallDistance+h)
		s.Input = control.Input{}
		return true
	case fallPlacing:
		s.Input = control.Input{}
		if p.InWater || p.OnGround || f.ticks > 200 { // (una caída larga: 64 bloques son más de 50 ticks)
			if f.pending { // (echó el agua: lo de "salvada" se ve en la vida, no aquí)
				f.saves++
			}
			f.state, f.ticks, f.pending = fallLanded, 0, false
			return true
		}
		if f.pending {
			return true
		}
		if !f.bucketToHand() { // (el cubo en la mano: si estaba en el inventario, llega ahora)
			if _, ok := hotbarWith(f.Screens, "minecraft:water_bucket"); !ok {
				if _, ok := inventoryWith(f.Screens, "minecraft:water_bucket"); !ok {
					f.state = fallIdle // (ya no hay cubo)
					return false
				}
			}
		}
		// dónde estará tras este tick (el uso sale el tick siguiente, desde
		// ahí: es lo que mira el servidor), y dónde va a caer desde allí (con
		// lo que se mueve de lado: un empujón, salir corriendo de un borde)
		nv := physics.Vec3{X: p.Vel.X * 0.91, Y: (p.Vel.Y - 0.08) * 0.98, Z: p.Vel.Z * 0.91}
		next := pos
		next.X, next.Y, next.Z = pos.X+p.Vel.X, pos.Y+p.Vel.Y, pos.Z+p.Vel.Z
		ground, h, soft, ok := f.landing(next, nv)
		if !ok {
			bp.UpdatePosition(func(q basic.Position) basic.Position { q.Pitch = 90; return q })
			return true
		}
		if soft {
			return true // (cae en agua, heno, slime…: nada que hacer)
		}
		f.water = world.BlockPos{X: ground.X, Y: ground.Y + 1, Z: ground.Z}
		// mirando a donde va a caer desde antes (el giro no cuesta un tick)
		yaw, pitch, dist := lookAt(next, float64(ground.X)+0.5, float64(ground.Y)+1-0.01, float64(ground.Z)+0.5)
		bp.UpdatePosition(func(q basic.Position) basic.Position { q.Yaw, q.Pitch = yaw, pitch; return q })
		held, slot := f.heldBucket()
		if !held {
			if slot >= 0 {
				_ = f.Screens.SelectHotbar(slot)
			}
			return true
		}
		// h es la altura tras este tick (desde donde sale el uso). En alcance:
		// ahora. Cayendo muy deprisa (casi 4 bloques por tick) puede no haber
		// un tick dentro del alcance: si el siguiente ya estaría en el suelo,
		// esta es la última ocasión
		h2 := h + (nv.Y-0.08)*0.98
		if h > 0.05 && dist <= 4.5 && (h <= fallReach || h2 <= 0.05) {
			f.Hands.UseItemLooking(yaw, pitch, nil)
			f.pending = true
		}
		return true
	case fallLanded:
		// recoger el agua: un cubo vacío y mirarla (si no queda o no llega, se deja)
		// (el agua se ve cuando la cuenta el servidor: con su retraso, unos ticks)
		if !isSourceAt(f.World, f.water) && f.ticks > 12 || f.ticks > 100 {
			f.state = fallIdle
			return false
		}
		if !isSourceAt(f.World, f.water) {
			s.Input = control.Input{}
			return true
		}
		if f.ticks < 3 || p.Vel.Y < -0.1 {
			s.Input = control.Input{}
			return true // (que se asiente: el agua aún se está poniendo)
		}
		slot, ok := f.hotbarWith("minecraft:bucket")
		if !ok {
			if _, full := f.hotbarWith("minecraft:water_bucket"); full && f.ticks <= 12 {
				s.Input = control.Input{}
				return true // (el cubo se vacía cuando lo cuenta el servidor: con su retraso)
			}
			f.state = fallIdle
			return false
		}
		f.Screens.Lock()
		held := f.Screens.HeldSlot
		f.Screens.Unlock()
		if held != slot {
			_ = f.Screens.SelectHotbar(slot)
			return true
		}
		// otra vez si no salió, pasada la ida y vuelta al servidor (antes el
		// cubo aún se ve vacío aunque ya esté lleno: sería echarla otra vez)
		if !f.pending || f.ticks-f.usedAt > 25 {
			f.Hands.UseItemToward(float64(f.water.X)+0.5, float64(f.water.Y)+0.85, float64(f.water.Z)+0.5, nil) // (la superficie: desde un lado, el centro lo tapa el bloque de al lado)
			if !f.pending {
				f.logf("recojo el agua")
			}
			f.pending, f.usedAt = true, f.ticks
		}
		s.Input = control.Input{}
		return true
	}
	return false
}

// bucketToHand: el cubo de agua en la mano (o pedido): de la barra, se
// selecciona; del inventario, se pasa a la barra (un clic, el tick
// siguiente está). Dice si hay cubo.
func (f *Fall) bucketToHand() bool {
	if held, slot := f.heldBucket(); held {
		return true
	} else if slot >= 0 {
		_ = f.Screens.SelectHotbar(slot)
		return true
	}
	inv, ok := inventoryWith(f.Screens, "minecraft:water_bucket")
	if !ok {
		return false
	}
	_ = f.Screens.Click(0, inv, spareHotbar(f.Screens), types.ContainerInputSwap)
	return true
}

// heldBucket: si lleva el cubo de agua en la mano; si no, su casilla de la
// barra (-1: no está en la barra).
func (f *Fall) heldBucket() (bool, int) {
	slot, ok := hotbarWith(f.Screens, "minecraft:water_bucket")
	if !ok {
		return false, -1
	}
	f.Screens.Lock()
	held := f.Screens.HeldSlot
	f.Screens.Unlock()
	return held == slot, slot
}

// landing: dónde va a caer desde pos con la velocidad v (la gravedad, 0.08, y
// el rozamiento del aire: 0.98 en vertical, 0.91 en horizontal), y cuánto
// le queda (h: de los pies a la cara de arriba de ese suelo). soft: cae en
// algo que anula o frena la caída. ok false: nada debajo en 64 bloques.
func (f *Fall) landing(pos basic.Position, v physics.Vec3) (ground world.BlockPos, h float64, soft, ok bool) {
	x, y, z := pos.X, pos.Y, pos.Z
	vx, vy, vz := v.X, v.Y, v.Z
	for n := 0; n < 200; n++ {
		q := pos
		q.X, q.Y, q.Z = x, y, z
		g, gh, gs := f.groundBelow(q)
		if math.IsInf(gh, 1) {
			return g, gh, false, false
		}
		top := float64(g.Y + 1)
		if y+vy <= top { // (este tick toca el suelo de esta columna)
			return g, pos.Y - top, gs, true
		}
		x, y, z = x+vx, y+vy, z+vz
		vx, vy, vz = vx*0.91, (vy-0.08)*0.98, vz*0.91
	}
	return world.BlockPos{}, math.Inf(1), false, false
}

// lookAt: la mirada de los ojos (en pos) a un punto, y la distancia.
func lookAt(pos basic.Position, x, y, z float64) (yaw, pitch float32, dist float64) {
	dx, dy, dz := x-pos.X, y-(pos.Y+1.62), z-pos.Z
	yaw = float32(-math.Atan2(dx, dz) * 180 / math.Pi)
	pitch = float32(-math.Atan2(dy, math.Hypot(dx, dz)) * 180 / math.Pi)
	return yaw, pitch, math.Sqrt(dx*dx + dy*dy + dz*dz)
}

// groundBelow: el primer bloque sólido bajo los pies (h: cuánto falta), o si
// lo primero que hay es agua (soft: no hace falta nada).
func (f *Fall) groundBelow(pos basic.Position) (ground world.BlockPos, h float64, soft bool) {
	x, z := int(math.Floor(pos.X)), int(math.Floor(pos.Z))
	for y := int(math.Floor(pos.Y)); y > int(math.Floor(pos.Y))-64; y-- {
		c := world.BlockPos{X: x, Y: y, Z: z}
		st, ok := f.World.BlockAt(c)
		if !ok {
			return c, math.Inf(1), false
		}
		n := block.StateList[st].ID()
		if strings.Contains(n, "water") || strings.HasSuffix(n, "slime_block") || strings.HasSuffix(n, "hay_block") ||
			strings.HasSuffix(n, "cobweb") || strings.HasSuffix(n, "powder_snow") || strings.HasSuffix(n, "scaffolding") {
			return c, pos.Y - float64(y+1), true // (lo que frena o anula la caída)
		}
		if len(block.CollisionShape(st)) > 0 {
			return c, pos.Y - float64(y+1), false
		}
	}
	return world.BlockPos{}, math.Inf(1), false
}

func (f *Fall) isWater(c world.BlockPos) bool { return isWaterAt(f.World, c) }

func isWaterAt(w *world.World, c world.BlockPos) bool {
	st, ok := w.BlockAt(c)
	return ok && strings.Contains(block.StateList[st].ID(), "water")
}

// hotbarWith: la casilla de la barra rápida (0-8) con el objeto.
func (f *Fall) hotbarWith(item string) (int, bool) { return hotbarWith(f.Screens, item) }

func hotbarWith(sc *screen.Manager, item string) (int, bool) {
	sc.Lock()
	defer sc.Unlock()
	for h := 0; h < 9; h++ {
		sl := sc.Inventory.Slots[36+h]
		if sl.Count > 0 && int(sl.Item) >= 0 && int(sl.Item) < len(registryid.Item) && registryid.Item[sl.Item] == item {
			return h, true
		}
	}
	return 0, false
}

func (f *Fall) logf(format string, args ...any) {
	if f.Logf != nil {
		f.Logf(format, args...)
	}
}
