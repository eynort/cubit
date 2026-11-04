package safety

import (
	"math"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Fire es el reflejo de apagarse: ardiendo (un golpe con aspecto ígneo, una
// flecha en llamas, la lava o el fuego que pisó; el servidor lo dice en el
// bit 0x01 de los datos de la entidad), un punto de daño por segundo hasta 8 s.
// Con un cubo de agua en la barra rápida y sin resistencia al fuego, lo echa a
// sus pies, se apaga (el agua quita el fuego), y lo recoge. En tierra: en el
// aire, la caída es del reflejo del cubo (Fall), que también apaga.
type Fire struct {
	Client  *bot.Client
	Player  *basic.Player
	World   *world.World
	Mover   *physics.Mover
	Screens *screen.Manager
	Hands   *act.Hands
	Logf    func(format string, args ...any)
	// BeforeUse: antes de echar el agua, soltar lo que use la mano (el escudo
	// arriba, el arco tensado: la pelea). nil: nada.
	BeforeUse func()
	// Calm: no hay pelea a la que quitarle las manos (para rellenar el cubo
	// vacío con el agua de al lado). nil: nunca se rellena.
	Calm func() bool

	burning   atomic.Bool
	air       atomic.Int32 // el aire que le queda (300 lleno; bajo el agua baja 1 por tick)
	surfacing bool         // nadando hacia arriba (Think)
	active    atomic.Bool  // las manos son suyas (Active)
	mu        sync.Mutex
	state     int            // 0 nada, 1 echando el agua, 2 recogiéndola
	water     world.BlockPos // dónde la echó
	ticks     int
	pending   bool
	usedAt    int // el tick (de ticks) del último intento de recogerla
	rest      int // ticks sin volver a echar agua (el servidor aún no dijo que se apagó)
	outs      int
}

// Register escucha los datos de la entidad del bot (el fuego).
func (f *Fire) Register() {
	f.air.Store(300) // (lleno hasta que el servidor diga otra cosa)
	f.Client.Events.AddListener(bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlaySetEntityData, F: func(p pk.Packet) error {
		var d play.SetEntityData
		if err := p.Scan(&d); err != nil {
			return nil
		}
		if int32(d.ID) != int32(f.Player.Login.PlayerID) {
			return nil
		}
		for _, it := range d.PackedItems {
			if it.Index == entitydata.EntityAirSupply {
				switch v := it.Value.(type) {
				case *pk.VarInt:
					f.air.Store(int32(*v))
				case *pk.Int:
					f.air.Store(int32(*v))
				}
			}
			if it.Index == entitydata.EntitySharedFlags {
				if b, ok := it.Value.(*pk.Byte); ok {
					f.burning.Store(*b&1 != 0)
				}
			}
		}
		return nil
	}})
}

// Burning: está ardiendo (lo que dijo el servidor).
func (f *Fire) Burning() bool { return f.burning.Load() }

// airLow: con menos aire que esto (de 300; 15 s bajo el agua), a la superficie.
// (en los logs: peleando en un lago, ahogado con la pelea a medias)
const airLow = 150

// Air es el aire que le queda (300 lleno).
func (f *Fire) Air() int { return int(f.air.Load()) }

// drowning: bajo el agua con poco aire, a la superficie (nadar hacia arriba,
// mirando arriba) hasta tener la cabeza fuera y el aire lleno otra vez.
// Mientras, la pelea no toca las teclas (Active). Dice si lo maneja él.
func (f *Fire) drowning(s *control.State, bp *basic.Player, p *physics.Player) bool {
	air := f.air.Load()
	if f.surfacing && (air >= 300 || !p.InWater) {
		f.surfacing = false
		return false
	}
	if !f.surfacing {
		if air >= airLow || !p.InWater {
			return false
		}
		f.surfacing = true
		f.logf("me ahogo (aire %d de 300): subo a respirar", air)
	}
	bp.UpdatePosition(func(q basic.Position) basic.Position { q.Pitch = -60; return q })
	s.Input = control.Input{Jump: true, Forward: s.HorizontalCollision}
	return true
}

// Active: se está apagando (las manos y las teclas son suyas: la pelea no
// cambia lo de la mano mientras, o el cubo no sale nunca).
func (f *Fire) Active() bool { return f.active.Load() }

// Outs es cuántas veces se apagó con el cubo.
func (f *Fire) Outs() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.outs
}

// Think: dice si este tick lo maneja él.
func (f *Fire) Think(s *control.State, bp *basic.Player) bool {
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
	if f.state == 0 && f.drowning(s, bp, p) {
		f.active.Store(true)
		return true
	}
	busy := f.think(s, bp, p, pos)
	f.active.Store(busy)
	return busy
}

func (f *Fire) think(s *control.State, bp *basic.Player, p *physics.Player, pos basic.Position) bool {
	switch f.state {
	case 0:
		if f.rest > 0 {
			f.rest--
			return false
		}
		if !f.burning.Load() && p.OnGround && f.ticks%20 == 0 && f.refill(pos) {
			return true
		}
		if !f.burning.Load() || !p.OnGround || p.InWater || p.InLava {
			return false
		}
		if _, res := bp.Status().Effects["minecraft:fire_resistance"]; res {
			return false // no quema: no hace falta
		}
		slot, ok := hotbarWith(f.Screens, "minecraft:water_bucket")
		if !ok {
			// en el inventario: a la barra (un clic; el tick siguiente ya está)
			inv, has := inventoryWith(f.Screens, "minecraft:water_bucket")
			if !has || f.ticks < 4 {
				return has && f.ticks < 4
			}
			f.ticks = 0
			s.Input = control.Input{}
			_ = f.Screens.Click(0, inv, spareHotbar(f.Screens), types.ContainerInputSwap)
			return true
		}
		f.Screens.Lock()
		held := f.Screens.HeldSlot
		f.Screens.Unlock()
		s.Input = control.Input{}
		if f.BeforeUse != nil {
			f.BeforeUse() // (con el escudo arriba la mano no echa nada)
		}
		if held != slot {
			_ = f.Screens.SelectHotbar(slot)
			return true
		}
		f.water = world.BlockPos{X: int(math.Floor(pos.X)), Y: int(math.Floor(pos.Y + 1e-6)), Z: int(math.Floor(pos.Z))}
		f.Hands.UseItemLooking(pos.Yaw, 90, nil) // (a la cara de arriba del suelo: el agua, en sus pies)
		f.state, f.ticks, f.pending = 1, 0, false
		f.logf("ardiendo: me apago con el cubo de agua")
		return true
	case 1:
		// quieto en el agua hasta que el servidor diga que se apagó (con el
		// retraso, el agua se ve antes de que él la vea; un empujón saca del
		// charco: de vuelta a él)
		s.Input = control.Input{}
		if !f.burning.Load() {
			f.outs++
			f.state, f.ticks = 2, 0
			return true
		}
		if !p.InWater && isWaterAt(f.World, f.water) {
			f.walkTo(s, bp, pos, f.water, 0.25)
		}
		if f.ticks > 40 || f.ticks > 20 && !isWaterAt(f.World, f.water) { // no salió (sin sitio, o el servidor no lo puso)
			f.state = 0
			if isWaterAt(f.World, f.water) {
				f.state, f.ticks = 2, 0 // (recogerla igual)
				return true
			}
			return false
		}
		return true
	case 2:
		// recogerla (como en Fall): con el cubo vacío, mirándola
		// (hasta que deje de ser una fuente: el agua que se esparce alrededor
		// no se recoge, el cubo la atraviesa; un buen rato si hace falta: el
		// cubo lleno es lo que le salva la próxima vez)
		if !isSourceAt(f.World, f.water) && f.ticks > 12 || f.ticks > 200 {
			f.state, f.rest = 0, 10
			if f.ticks > 200 {
				f.logf("no pude recoger el agua de %v", f.water)
			}
			return false
		}
		s.Input = control.Input{}
		if !isSourceAt(f.World, f.water) {
			return true
		}
		slot, ok := hotbarWith(f.Screens, "minecraft:bucket")
		if !ok {
			if f.ticks > 12 {
				f.state = 0
				return false
			}
			return true
		}
		f.Screens.Lock()
		held := f.Screens.HeldSlot
		f.Screens.Unlock()
		if held != slot {
			_ = f.Screens.SelectHotbar(slot)
			return true
		}
		if dx, dz := float64(f.water.X)+0.5-pos.X, float64(f.water.Z)+0.5-pos.Z; math.Hypot(dx, dz) > 3.5 {
			f.walkTo(s, bp, pos, f.water, 3) // (un empujón le alejó: hasta tenerla al alcance)
			return true
		}
		// otra vez si no salió, pero pasada la ida y vuelta al servidor (unos 8
		// ticks): antes, el cubo aún se ve vacío aunque ya esté lleno, y "recoger"
		// con él lleno es echar el agua otra vez
		if !f.pending || f.ticks-f.usedAt > 25 {
			f.Hands.UseItemToward(float64(f.water.X)+0.5, float64(f.water.Y)+0.85, float64(f.water.Z)+0.5, nil) // (la superficie: desde un lado, el centro lo tapa el bloque de al lado)
			f.pending, f.usedAt = true, f.ticks
		}
		return true
	}
	return false
}

func (f *Fire) logf(format string, args ...any) {
	if f.Logf != nil {
		f.Logf(format, args...)
	}
}

// inventoryWith: la casilla del inventario (fuera de la barra) con el objeto.
func inventoryWith(sc *screen.Manager, item string) (int, bool) {
	sc.Lock()
	defer sc.Unlock()
	for i := 9; i < 36; i++ {
		sl := sc.Inventory.Slots[i]
		if sl.Count > 0 && int(sl.Item) >= 0 && int(sl.Item) < len(registryid.Item) && registryid.Item[sl.Item] == item {
			return i, true
		}
	}
	return 0, false
}

// spareHotbar: una casilla de la barra para traer algo: una vacía, o la
// última que no sea la de la mano (la de la mano suele ser el arma).
func spareHotbar(sc *screen.Manager) int {
	sc.Lock()
	defer sc.Unlock()
	for h := 0; h < 9; h++ {
		if sc.Inventory.Slots[36+h].Count <= 0 {
			return h
		}
	}
	if sc.HeldSlot == 8 {
		return 7
	}
	return 8
}

// walkTo: unos pasos hacia el centro de c, hasta estar a menos de near (en horizontal).
func (f *Fire) walkTo(s *control.State, bp *basic.Player, pos basic.Position, c world.BlockPos, near float64) {
	dx, dz := float64(c.X)+0.5-pos.X, float64(c.Z)+0.5-pos.Z
	if math.Hypot(dx, dz) <= near {
		return
	}
	yaw := float32(-math.Atan2(dx, dz) * 180 / math.Pi)
	bp.UpdatePosition(func(p basic.Position) basic.Position { p.Yaw = yaw; return p })
	s.Input = control.Input{Forward: true}
}

// isSourceAt: en c hay una fuente de agua (la que el cubo recoge; el agua que
// corre, no).
func isSourceAt(w *world.World, c world.BlockPos) bool {
	st, ok := w.BlockAt(c)
	if !ok {
		return false
	}
	n := block.StateList[st].ID()
	return strings.HasPrefix(n, "minecraft:water") && (!strings.Contains(n, "level=") || strings.Contains(n, "level=0"))
}

// refill: sin cubo de agua, con uno vacío en la barra y una fuente de agua al
// alcance, tranquilo: la recoge (el estado 2). Así siempre lleva agua para
// la próxima (el fuego, una caída). Dice si empieza.
func (f *Fire) refill(pos basic.Position) bool {
	if f.Calm == nil || !f.Calm() || f.Hands.Busy() {
		return false
	}
	if _, ok := hotbarWith(f.Screens, "minecraft:water_bucket"); ok {
		return false
	}
	if _, ok := inventoryWith(f.Screens, "minecraft:water_bucket"); ok {
		return false
	}
	if _, ok := hotbarWith(f.Screens, "minecraft:bucket"); !ok {
		return false
	}
	ex, ey, ez := pos.X, pos.Y+1.62, pos.Z
	best, bestD := world.BlockPos{}, math.Inf(1)
	for dx := -4; dx <= 4; dx++ {
		for dy := -3; dy <= 2; dy++ {
			for dz := -4; dz <= 4; dz++ {
				c := world.BlockPos{X: int(math.Floor(pos.X)) + dx, Y: int(math.Floor(pos.Y)) + dy, Z: int(math.Floor(pos.Z)) + dz}
				if !isSourceAt(f.World, c) {
					continue
				}
				// (la cara de arriba al aire: el cubo la ve)
				if st, ok := f.World.BlockAt(world.BlockPos{X: c.X, Y: c.Y + 1, Z: c.Z}); !ok || !strings.HasSuffix(block.StateList[st].ID(), ":air") {
					continue
				}
				d := math.Sqrt((float64(c.X)+0.5-ex)*(float64(c.X)+0.5-ex) + (float64(c.Y)+0.5-ey)*(float64(c.Y)+0.5-ey) + (float64(c.Z)+0.5-ez)*(float64(c.Z)+0.5-ez))
				if d < bestD && d <= 4.4 {
					best, bestD = c, d
				}
			}
		}
	}
	if math.IsInf(bestD, 1) {
		return false
	}
	f.logf("cubo vacío: lo relleno con el agua de %v", best)
	f.water, f.state, f.ticks, f.pending = best, 2, 0, false
	return true
}
