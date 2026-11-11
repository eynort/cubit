package elytra

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/component"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Las casillas del inventario del jugador (el menú 0, como en el protocolo).
const (
	slotChest   = 6
	slotMain    = 9
	slotHotbar  = 36
	slotOffhand = 45
)

const (
	elytraItem = "minecraft:elytra"
	rocketItem = "minecraft:firework_rocket"
	// MinRockets: con menos no se despega (uno para despegar, alguno para subir).
	MinRockets = 3
)

// Deps es lo que el vuelo usa del bot.
type Deps struct {
	Client   *bot.Client
	Player   *basic.Player
	World    physics.Blocks
	Entities *entities.Manager
	Screens  *screen.Manager
	Hands    *act.Hands
	Ctl      *control.Controller
	Mover    *physics.Mover
	Logf     func(format string, args ...any)
}

// Flight vuela el bot con élitros: New engancha la física (el planeo, el
// cohete) y FlyTo vuela; mientras, Think es el que lleva las teclas y la
// mirada (Active).
type Flight struct {
	d      Deps
	mu     sync.Mutex
	pilot  *Pilot
	tick   int
	active atomic.Bool
	// sinking: el vuelo acabó en el agua con los élitros abiertos (en el agua
	// no se cierran; solo tocando suelo): agachado al fondo hasta cerrarlos,
	// hasta sinkUntil (un fondo muy hondo: se deja, y a respirar)
	sinking   bool
	sinkUntil int

	glideOn   atomic.Bool   // el flag de planeo como lo dijo el servidor
	glideTold atomic.Uint64 // cuántas veces lo dijo
}

// New prepara el vuelo: la física sabe desde ya si lleva élitros, si un
// cohete tira de él y lo que el servidor dice del planeo.
func New(d Deps) *Flight {
	f := &Flight{d: d}
	d.Mover.CanGlide = f.canGlide
	d.Mover.Rocket = f.boosting
	d.Mover.FallFlying = func() (bool, uint64) { return f.glideOn.Load(), f.glideTold.Load() }
	d.Client.Events.AddListener(bot.PacketHandler{Priority: 10, ID: packetid.ClientboundPlaySetEntityData, F: f.onData})
	return f
}

func (f *Flight) logf(format string, a ...any) {
	if f.d.Logf != nil {
		f.d.Logf(format, a...)
	}
}

func (f *Flight) selfID() int32 { return int32(f.d.Player.Login.PlayerID) }

// onData: los flags compartidos del propio jugador (el 7, 0x80: planeando).
func (f *Flight) onData(p pk.Packet) error {
	var d play.SetEntityData
	if err := p.Scan(&d); err != nil {
		return nil // (no es cosa nuestra que no se lea: lo dice quien lo necesite)
	}
	if int32(d.ID) != f.selfID() {
		return nil
	}
	for _, v := range d.PackedItems {
		if v.Index != entitydata.EntitySharedFlags {
			continue
		}
		if b, ok := v.Value.(*pk.Byte); ok {
			f.glideOn.Store(uint8(*b)&0x80 != 0)
			f.glideTold.Add(1)
		}
	}
	return nil
}

// Active: está volando (Think lleva el control).
func (f *Flight) Active() bool { return f.active.Load() }

// Status describe el vuelo en curso ("" si no vuela).
func (f *Flight) Status() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.active.Load() || f.pilot == nil {
		return ""
	}
	return f.pilot.String()
}

func (f *Flight) inventory() ([slotOffhand + 1]screen.Slot, int) {
	f.d.Screens.Lock()
	defer f.d.Screens.Unlock()
	return f.d.Screens.Inventory.Slots, f.d.Screens.HeldSlot
}

func nameOf(s screen.Slot) string {
	if s.Count <= 0 {
		return ""
	}
	if id := int(s.Item); id >= 0 && id < len(registryid.Item) {
		return registryid.Item[id]
	}
	return ""
}

// usesLeft: cuántos usos le quedan a una pieza (sin desgaste: muchos).
func usesLeft(s screen.Slot) int {
	max := act.Component[*component.MaxDamage](s)
	if max == nil || max.Value <= 0 {
		return math.MaxInt32
	}
	dmg := 0
	if d := act.Component[*component.Damage](s); d != nil {
		dmg = int(d.Value)
	}
	return int(max.Value) - dmg
}

// canGlide: lleva puestos unos élitros que no están a punto de romperse
// (Player.canGlide: ni con un uso).
func (f *Flight) canGlide() bool {
	inv, _ := f.inventory()
	s := inv[slotChest]
	return nameOf(s) == elytraItem && usesLeft(s) > 1
}

// boosting: un cohete suyo tira de él (el servidor crea el cohete con él de
// dueño, enganchado, al usarlo planeando: FireworkRocketEntity).
func (f *Flight) boosting() bool {
	p := f.d.Player.Position()
	for _, e := range f.d.Entities.Nearby(p.X, p.Y, p.Z, 4) {
		if e.Type == rocketItem && e.SpawnData == f.selfID() {
			return true
		}
	}
	return false
}

// count cuenta lo que lleva de item (inventario, barra y mano izquierda).
func (f *Flight) count(name string) int {
	inv, _ := f.inventory()
	n := 0
	for i := slotMain; i <= slotOffhand; i++ {
		if nameOf(inv[i]) == name {
			n += int(inv[i].Count)
		}
	}
	return n
}

// elytraUses: los usos de los mejores élitros que lleva (puestos o no).
func (f *Flight) elytraUses() int {
	inv, _ := f.inventory()
	best := 0
	for _, i := range append([]int{slotChest}, rangeSlots(slotMain, slotOffhand)...) {
		if nameOf(inv[i]) == elytraItem && usesLeft(inv[i]) > best {
			best = usesLeft(inv[i])
		}
	}
	return best
}

func rangeSlots(a, b int) []int {
	var out []int
	for i := a; i <= b; i++ {
		out = append(out, i)
	}
	return out
}

// Ready dice si se puede volar dist bloques: élitros con usos para el viaje
// (se gasta uno por segundo planeando) y cohetes.
func (f *Flight) Ready(dist float64) (bool, string) {
	uses := f.elytraUses()
	if uses == 0 {
		return false, "no llevo élitros"
	}
	need := int(dist/20) + 30 // ~25 bloques por segundo, y margen para aterrizar
	if uses < need {
		return false, fmt.Sprintf("a los élitros les quedan %d usos (hacen falta ~%d)", uses, need)
	}
	// despegar y subir gasta ~3 (con un servidor lento, más); luego ~1 cada
	// 100 bloques (las pruebas: 400 bloques, 6-10). Con menos, se queda sin
	// ellos a medio camino (en los logs: 3 cohetes, sin ellos a los 5 s)
	need = MinRockets + int(dist/100)
	if n := f.count(rocketItem); n < need {
		return false, fmt.Sprintf("me quedan %d cohetes (para %.0f bloques hacen falta %d)", n, dist, need)
	}
	return true, ""
}

// hold pone en la mano lo de la casilla i (de la mochila a la barra si hace falta).
func (f *Flight) hold(ctx context.Context, i int) error {
	if i >= slotHotbar && i < slotHotbar+9 {
		_, held := f.inventory()
		if held != i-slotHotbar {
			if err := f.d.Screens.SelectHotbar(i - slotHotbar); err != nil {
				return err
			}
		}
		return f.d.Ctl.WaitTicks(ctx, 1)
	}
	h := f.spareHotbar()
	if err := f.d.Screens.Click(0, i, h, types.ContainerInputSwap); err != nil {
		return err
	}
	if err := f.d.Ctl.WaitTicks(ctx, 2); err != nil {
		return err
	}
	return f.hold(ctx, slotHotbar+h)
}

// spareHotbar: una casilla de la barra libre, o la de la mano, o la 8.
func (f *Flight) spareHotbar() int {
	inv, held := f.inventory()
	for h := 0; h < 9; h++ {
		if inv[slotHotbar+h].Count <= 0 {
			return h
		}
	}
	for _, h := range []int{held, 8} {
		if n := nameOf(inv[slotHotbar+h]); n != elytraItem && n != rocketItem {
			return h
		}
	}
	return 8
}

// find es la casilla (barra primero) de lo que cumple ok, -1 si no hay.
func (f *Flight) find(ok func(screen.Slot) bool) int {
	inv, _ := f.inventory()
	for _, rng := range [][2]int{{slotHotbar, slotHotbar + 8}, {slotMain, slotHotbar - 1}} {
		for i := rng[0]; i <= rng[1]; i++ {
			if ok(inv[i]) {
				return i
			}
		}
	}
	return -1
}

// use usa lo que tiene en la mano (sin bloque al que apuntar: mirando al frente).
func (f *Flight) use(ctx context.Context) error {
	p := f.d.Player.Position()
	l := lookOf(p.Yaw, p.Pitch)
	x, y, z := act.Eye(p, false)
	done := make(chan error, 1)
	f.d.Hands.UseItemToward(x+l.X*20, y+l.Y*20, z+l.Z*20, func(u act.Use) { done <- u.Err })
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func lookOf(yaw, pitch float32) physics.Vec3 {
	pl := physics.Player{Yaw: yaw, Pitch: pitch}
	return pl.LookAngle()
}

// wear se pone los élitros (usarlos en la mano los cambia por lo que se
// lleva en el pecho: Equippable). Devuelve la casilla donde quedó lo que se
// quitó (-1: nada).
func (f *Flight) wear(ctx context.Context) (int, error) {
	inv, _ := f.inventory()
	if nameOf(inv[slotChest]) == elytraItem && usesLeft(inv[slotChest]) > 1 {
		return -1, nil
	}
	best, bestUses := -1, 1
	for _, i := range rangeSlots(slotMain, slotHotbar+8) {
		if nameOf(inv[i]) == elytraItem && usesLeft(inv[i]) > bestUses {
			best, bestUses = i, usesLeft(inv[i])
		}
	}
	if best < 0 {
		return -1, errors.New("no llevo élitros que sirvan")
	}
	had := nameOf(inv[slotChest])
	if err := f.hold(ctx, best); err != nil {
		return -1, err
	}
	_, held := f.inventory()
	if err := f.use(ctx); err != nil {
		return -1, err
	}
	for i := 0; i < 20; i++ {
		if err := f.d.Ctl.WaitTicks(ctx, 1); err != nil {
			return -1, err
		}
		if f.canGlide() {
			if had != "" {
				f.logf("élitros: me los pongo (el %s queda en la mano)", shortName(had))
				return slotHotbar + held, nil
			}
			f.logf("élitros: me los pongo")
			return -1, nil
		}
	}
	return -1, errors.New("no consigo ponerme los élitros")
}

// unwear se vuelve a poner lo que llevaba en el pecho (en la casilla i).
func (f *Flight) unwear(ctx context.Context, i int) {
	inv, _ := f.inventory()
	name := nameOf(inv[i])
	if name == "" || name == elytraItem {
		return
	}
	if err := f.hold(ctx, i); err != nil {
		return
	}
	if err := f.use(ctx); err != nil {
		return
	}
	for k := 0; k < 20; k++ {
		if f.d.Ctl.WaitTicks(ctx, 1) != nil {
			return
		}
		if inv, _ := f.inventory(); nameOf(inv[slotChest]) == name {
			f.logf("élitros: me vuelvo a poner el %s", shortName(name))
			return
		}
	}
}

func shortName(n string) string {
	if len(n) > 10 && n[:10] == "minecraft:" {
		return n[10:]
	}
	return n
}

// Wear se pone los élitros ya (el peto, si lo llevaba, queda en la mano).
func (f *Flight) Wear(ctx context.Context) error {
	if f.canGlide() {
		f.logf("élitros: ya los llevo puestos")
		return nil
	}
	_, err := f.wear(ctx)
	return err
}

// holdRockets: los cohetes en la mano.
func (f *Flight) holdRockets(ctx context.Context) error {
	inv, held := f.inventory()
	if nameOf(inv[slotHotbar+held]) == rocketItem {
		return nil
	}
	i := f.find(func(s screen.Slot) bool { return nameOf(s) == rocketItem })
	if i < 0 {
		return errors.New("no llevo cohetes")
	}
	return f.hold(ctx, i)
}

// ErrNoHeadroom: no hay sitio encima para despegar (árboles, un techo).
var ErrNoHeadroom = errors.New("no hay sitio encima para despegar")

// Headroom dice si desde los pies en p hay aire encima para despegar.
func Headroom(w physics.Blocks, x, y, z float64) bool {
	pl := &Pilot{World: w}
	return pl.headroom(physics.Vec3{X: x, Y: y, Z: z})
}

// FlyTo vuela hasta (x, z): se pone los élitros, despega, vuela y aterriza
// cerca (lo que quede, se anda), lo más cerca posible de la altura y (NaN:
// no se sabe; la superficie). Devuelve nil si aterrizó (o cayó al agua).
// Cancelado en el aire, aterriza donde esté antes de volver: soltar las
// teclas planeando es caer.
func (f *Flight) FlyTo(ctx context.Context, x, y, z float64) error {
	return f.fly(ctx, func() (float64, float64, float64, bool, bool) { return x, y, z, true, true })
}

// FlyFollow vuela hacia lo que diga target (un jugador que se mueve, o que
// vuela): la meta se actualiza en vuelo; su altura cuenta solo si está en el
// suelo (en el aire no es donde aterrizar: se le sigue hasta que baje).
// Aterriza cerca de él como FlyTo.
func (f *Flight) FlyFollow(ctx context.Context, target func() (x, y, z float64, onGround, ok bool)) error {
	return f.fly(ctx, target)
}

func (f *Flight) fly(ctx context.Context, target func() (x, y, z float64, onGround, ok bool)) error {
	x, y, z, onGround, ok := target()
	if !ok {
		return errors.New("no sé dónde está")
	}
	if !onGround {
		y = math.NaN()
	}
	if ok, why := f.Ready(math.Hypot(x-f.d.Player.Position().X, z-f.d.Player.Position().Z)); !ok {
		return errors.New(why)
	}
	if p := f.d.Player.Position(); !Headroom(f.d.World, p.X, p.Y, p.Z) {
		return ErrNoHeadroom
	}
	// lo de ponerse y quitarse los élitros, aunque cancelen el vuelo: con un
	// contexto propio (quitarse los élitros a medias deja el pecho vacío)
	setup := context.WithoutCancel(ctx)
	back, err := f.wear(setup)
	if err != nil {
		return err
	}
	if back >= 0 {
		defer f.unwear(setup, back)
	}
	if err := f.holdRockets(setup); err != nil {
		return err
	}
	pl := &Pilot{World: f.d.World, Goal: [2]float64{x, z}, GoalY: y, Logf: f.d.Logf}
	f.mu.Lock()
	f.pilot = pl
	f.mu.Unlock()
	f.active.Store(true)
	defer f.active.Store(false)
	cancelled := false
	for {
		if err := f.d.Ctl.WaitTicks(setup, 5); err != nil {
			return err
		}
		f.mu.Lock()
		fin, why := pl.Done()
		airborne := pl.phase != takeoff
		// la meta se mueve (él vuela, o anda): al día
		if tx, ty, tz, onGround, ok := target(); ok && !cancelled {
			if !onGround {
				ty = math.NaN()
			}
			pl.Retarget(tx, ty, tz)
		}
		f.mu.Unlock()
		if fin {
			if why != "" {
				return errors.New(why)
			}
			f.closeInWater(setup)
			if cancelled {
				return ctx.Err()
			}
			return nil
		}
		if f.d.Player.Status().Health <= 0 {
			return errors.New("he muerto volando")
		}
		if ctx.Err() != nil && !cancelled {
			cancelled = true
			if !airborne {
				return ctx.Err()
			}
			f.mu.Lock()
			p := f.d.Player.Position()
			pl.Goal, pl.GoalY = [2]float64{p.X + p.VX*30, p.Z + p.VZ*30}, math.NaN()
			pl.startLanding(Obs{Pos: physics.Vec3{X: p.X, Y: p.Y, Z: p.Z}})
			f.mu.Unlock()
			f.logf("élitros: me cancelan en el aire; aterrizo aquí")
		}
		// los cohetes en la mano (se acabó la pila de la mano y quedan otros)
		if inv, held := f.inventory(); nameOf(inv[slotHotbar+held]) != rocketItem && f.count(rocketItem) > 0 && !f.d.Hands.Busy() {
			_ = f.holdRockets(setup)
		}
	}
}

// closeInWater: si el vuelo acabó en el agua con los élitros abiertos, los
// cierra yendo al fondo (abiertos, la cabeza va a 0.4 de los pies: se ahoga
// flotando; y se gastan, uno por segundo).
func (f *Flight) closeInWater(ctx context.Context) {
	mp := f.d.Mover.Player()
	if mp == nil || !mp.FallFlying || !mp.InWater {
		return
	}
	f.logf("élitros: en el agua siguen abiertos; bajo al fondo a cerrarlos")
	f.mu.Lock()
	f.sinking, f.sinkUntil = true, f.tick+100
	f.mu.Unlock()
	for {
		if f.d.Ctl.WaitTicks(ctx, 2) != nil {
			return
		}
		f.mu.Lock()
		still := f.sinking
		f.mu.Unlock()
		if !still {
			return
		}
	}
}

// Think es el tick del vuelo: la mirada, el salto y los cohetes que decide el
// piloto. Ponlo antes que el resto mientras Active.
func (f *Flight) Think(s *control.State, bp *basic.Player) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pl := f.pilot
	mp := f.d.Mover.Player()
	if pl == nil || mp == nil {
		return
	}
	f.tick++
	if f.sinking {
		if !s.FallFlying || f.tick > f.sinkUntil {
			f.sinking = false
			if s.FallFlying {
				f.logf("élitros: no llego al fondo; subo a respirar")
			}
			return
		}
		s.Input = control.Input{Shift: true} // agachado en el agua: se hunde
		return
	}
	pos := bp.Position()
	inv, held := f.inventory()
	rockets := 0
	if nameOf(inv[slotHotbar+held]) == rocketItem {
		rockets = int(inv[slotHotbar+held].Count)
	}
	if rockets == 0 && f.count(rocketItem) > 0 {
		rockets = -1 // quedan, pero no en la mano (FlyTo los pone): no lanzar aún, sin rendirse
	}
	o := Obs{
		Pos: physics.Vec3{X: pos.X, Y: pos.Y, Z: pos.Z}, Vel: mp.Vel, Yaw: pos.Yaw, Pitch: pos.Pitch,
		OnGround: s.OnGround, FallFlying: s.FallFlying, InWater: mp.InWater || mp.InLava,
		Boosting: f.boosting(), Rockets: max(rockets, 0), Tick: f.tick,
	}
	if rockets < 0 {
		o.Rockets = 1 // (el piloto no aterriza por "sin cohetes"; Fire no se hace sin ellos en la mano)
	}
	c := pl.Step(o)
	bp.UpdatePosition(func(p basic.Position) basic.Position {
		p.Yaw, p.Pitch = c.Yaw, c.Pitch
		return p
	})
	s.Input = control.Input{Jump: c.Jump}
	if c.Fire && rockets > 0 && !f.d.Hands.Busy() {
		l := lookOf(c.Yaw, c.Pitch)
		x, y, z := act.Eye(pos, false)
		f.d.Hands.UseItemToward(x+l.X*20, y+l.Y*20, z+l.Z*20, nil)
	}
}
