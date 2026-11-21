package combat

// El arco (y la ballesta): contra lo que está lejos (de 9 a 40 bloques) y no va a llegar
// antes de tensarlo (un zombi que viene; un jugador que huye o que no se
// puede alcanzar: en una torre, al otro lado de un río). Tensado entero (20
// ticks: la flecha a 3 bloques por tick, ~9 de daño), quieto (tensando se va
// al 20 %), con la puntería de la trayectoria de verdad (gravedad 0.05,
// rozamiento 0.99 por tick) y adelantada a su movimiento lo que dura el vuelo.

import (
	"context"
	"math"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/component"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// La ballesta (CrossbowItem): se carga tensando 25 ticks (soltarla antes no
// la carga); cargada, un clic dispara al momento (la flecha a 3.15). Lleva la
// carga puesta: se puede cargar antes de que llegue y disparar cuando toque.

const (
	crossbowSpeed = 3.15 // bloques por tick
	crossbowLoad  = 26   // ticks tensando para cargarla (25, y uno de margen)
	arrowSpeed    = 3.0  // bloques por tick, tensado entero
	arrowGravity  = 0.05 // AbstractArrow: la gravedad por tick
	arrowDrag     = 0.99 // y el rozamiento
	bowFull       = 21   // ticks tensando para el tiro entero (20, y uno de margen)
)

// bowSlot: el arco del inventario (o, sin arco, la ballesta), si hay flechas
// para él (o la ballesta ya está cargada).
func (f *Fighter) bowSlot() (int, bool) {
	inv, _ := f.inventory()
	bow, xbow, arrows := -1, -1, false
	for i := slotMainStart; i <= slotOffhand; i++ {
		switch nameOf(inv[i]) {
		case "minecraft:bow":
			if bow < 0 || i >= slotHotbar {
				bow = i
			}
		case "minecraft:crossbow":
			if xbow < 0 || i >= slotHotbar || loaded(inv[i]) {
				xbow = i
			}
		case "minecraft:arrow", "minecraft:spectral_arrow", "minecraft:tipped_arrow":
			arrows = true
		}
	}
	if bow >= 0 && arrows {
		return bow, true
	}
	if xbow >= 0 && (arrows || loaded(inv[xbow])) {
		return xbow, true
	}
	// el tridente (arsenal.go): con lealtad vuelve solo; sin ella, solo a uno
	// al que no se llega (y luego se recoge)
	if s, ok := f.tridentSlot(true); ok {
		return s, true
	}
	if f.ars.lostOK.Load() {
		if s, ok := f.tridentSlot(false); ok {
			return s, true
		}
	}
	return -1, false
}

// loaded: una ballesta cargada (lleva la flecha puesta).
func loaded(s screen.Slot) bool {
	c := act.Component[*component.ChargedProjectiles](s)
	return c != nil && len(c.Value) > 0
}

// wantBow: contra t, a esta distancia, mejor el arco que ir a por él.
func (f *Fighter) wantBow(t *foe) bool {
	f.ars.lostOK.Store(f.chaseID == t.id && f.steps-f.chaseAt > 60 && t.dist >= 5)
	if _, ok := f.bowSlot(); !ok || t.dist > 40 || t.e.Type == "minecraft:creeper" {
		return false
	}
	// la flecha tiene que llegar: su vuelo de verdad (en arco) libre de
	// bloques, de los ojos a él. Antes la regla de lejos no lo miraba, y las
	// otras miraban la recta: tiraba contra su propia pared (en el búnker),
	// contra una que tenía delante, o a uno que se tapó con bloques encima
	if !f.arrowPathClear(f.d.Player.Position(), t.e, f.t.seenVel[0], f.t.seenVel[1], f.bowSpeed()) {
		return false
	}
	if t.e.Type == "minecraft:player" {
		p := f.d.Player.Position()
		if blocking(t.e) && facing(t.e, p) {
			return false // (el escudo arriba y de cara: la flecha no entra)
		}
		if f.tac.id == t.id && f.steps < f.tac.bowOff {
			return false // (con flechas no se le tiró: a subir a por él)
		}
		// subido a algo (2 o más por encima) y sin poder romperle el suelo:
		// el empuje de la flecha le tira. Si con unas flechas no cae y hay
		// bloques, a subir donde está (el pathfinding, con torre)
		// (a menos de 5 no: baja de un salto y tensando no hay escudo ni espada;
		// en los logs, arco a 3 bloques contra una espada de diamante)
		if t.e.Y-p.Y >= 2 && math.Hypot(t.e.X-p.X, t.e.Z-p.Z) >= 5 {
			// (sin línea de tiro desde aquí: tampoco la regla de lejos, que
			// tiraría a la pared; el pathfinding sube o se acerca)
			return f.clearShot(t.e) && !f.bowFailed(t)
		}
		f.tac.bowOn = false
		// no se le llega (detrás de un hueco, de agua, en lo alto sin camino):
		// el walker no avanza en 3 s y se le ve: flechas mientras
		if f.chaseID == t.id && f.steps-f.chaseAt > 60 && t.dist >= 5 && f.clearShot(t.e) {
			return true
		}
		// comiendo o bebiendo de lejos: que no se cure tranquilo
		if eatingNow(t.e) && t.dist >= 5 && f.clearShot(t.e) {
			return true
		}
	}
	if t.dist < 9 {
		return false
	}
	// lo que se acerca mientras se tensa (unos 24 ticks con el giro): que
	// siga a más de 5 al soltar
	p := f.d.Player.Position()
	closing := f.closingSpeed(p, t.e, f.t.seenVel[0], f.t.seenVel[1])
	return t.dist-math.Max(0, closing)*24 >= 5
}

// ballistic es el ángulo (pitch, en grados de Minecraft: negativo hacia
// arriba) para que una flecha a arrowSpeed llegue a h bloques en horizontal y
// dy en vertical, y los ticks que tarda; ok false si no llega.
func ballistic(h, dy, speed float64) (pitch float64, ticks int, ok bool) {
	best := math.Inf(1)
	for a := -30.0; a <= 60; a += 0.25 { // (el arco bajo: el primero que llega)
		r := a * math.Pi / 180
		x, y := 0.0, 0.0
		vx, vy := speed*math.Cos(r), speed*math.Sin(r)
		for n := 1; n <= 100; n++ {
			px, py := x, y
			x, y = x+vx, y+vy
			vx, vy = vx*arrowDrag, vy*arrowDrag-arrowGravity
			if x >= h {
				yh := py + (y-py)*(h-px)/(x-px) // (la altura justo a h: entre los dos ticks)
				if e := math.Abs(yh - dy); e < best {
					best, pitch, ticks = e, -a, n
				}
				break
			}
		}
	}
	return pitch, ticks, best < 0.5
}

// aimBow: la mirada para darle a e (con lo que se mueve mientras vuela la
// flecha y lo que tarda en llegar al servidor). ok false si no llega.
func (f *Fighter) aimBow(pos basic.Position, e entities.Entity, vx, vz, speed float64) (yaw, pitch float32, ok bool) {
	ex, ey, ez := pos.X, pos.Y+1.62-0.1, pos.Z
	tx, ty, tz := e.X, e.Y+0.9, e.Z
	_, ticks, ok := ballistic(math.Hypot(tx-ex, tz-ez), ty-ey, speed)
	if !ok {
		return 0, 0, false
	}
	lead := float64(ticks + aimLead)
	tx, tz = tx+vx*lead, tz+vz*lead
	p, _, ok := ballistic(math.Hypot(tx-ex, tz-ez), ty-ey, speed)
	return float32(-math.Atan2(tx-ex, tz-ez) * 180 / math.Pi), float32(p), ok
}

// thinkBow: quieto, apuntando, tensando; suelta con el arco entero y la
// mirada ya donde va.
func (f *Fighter) thinkBow(s *control.State, bp *basic.Player, pl *plan, e entities.Entity) {
	f.lowerShield()
	s.Input = control.Input{}
	pos := bp.Position()
	vx, vz := f.track(e, s.Ticks)
	inv, held := f.inventory()
	hand := inv[slotHotbar+held]
	crossbow := nameOf(hand) == "minecraft:crossbow"
	trident := nameOf(hand) == "minecraft:trident"
	speed, full := arrowSpeed, uint64(bowFull)
	if crossbow {
		speed = crossbowSpeed
	}
	if trident {
		speed, full = tridentSpeed, tridentDraw
	}
	yaw, pitch, ok := f.aimBow(pos, e, vx, vz, speed)
	if !ok {
		f.stopDraw()
		return
	}
	f.turnTo(bp, yaw, pitch, 35, 25)
	cur := bp.Position()
	settled := math.Abs(float64(wrap(cur.Yaw-yaw))) < 2 && math.Abs(float64(cur.Pitch-pitch)) < 1.5
	if crossbow {
		switch {
		case loaded(hand) && settled && !f.d.Hands.Busy() && f.arrowPathClear(cur, e, vx, vz, speed): // cargada: un clic, y sale
			u := play.UseItem{Hand: types.InteractionHandMainHand, YRot: pk.Float(cur.Yaw), XRot: pk.Float(cur.Pitch)}
			if f.d.Client.Conn.WritePacket(pk.Marshal(u.PacketID(), u)) == nil {
				f.shots.Add(1)
			}
		case !loaded(hand) && !f.t.drawing && !f.d.Hands.Busy(): // a cargarla
			u := play.UseItem{Hand: types.InteractionHandMainHand, YRot: pk.Float(cur.Yaw), XRot: pk.Float(cur.Pitch)}
			if f.d.Client.Conn.WritePacket(pk.Marshal(u.PacketID(), u)) == nil {
				f.t.drawing, f.t.drawFrom = true, s.Ticks
			}
		case f.t.drawing && s.Ticks-f.t.drawFrom >= crossbowLoad:
			f.stopDraw() // cargada (el disparo es el clic siguiente)
		}
		return
	}
	if !f.t.drawing {
		if f.d.Hands.Busy() {
			return
		}
		u := play.UseItem{Hand: types.InteractionHandMainHand, YRot: pk.Float(cur.Yaw), XRot: pk.Float(cur.Pitch)}
		if f.d.Client.Conn.WritePacket(pk.Marshal(u.PacketID(), u)) == nil {
			f.t.drawing, f.t.drawFrom = true, s.Ticks
		}
		return
	}
	// soltar: el tiro, si la flecha llega (se tapó, se puso detrás de algo:
	// se espera tensado; el arco entero aguanta)
	if s.Ticks-f.t.drawFrom >= full && settled && f.arrowPathClear(cur, e, vx, vz, speed) {
		f.stopDraw()
		f.shots.Add(1)
		if trident && f.enchant(hand, "loyalty") == 0 {
			f.ars.tridentOut.Store(true) // (sin lealtad: queda en el suelo, a recogerlo)
		}
	}
}

// bowSpeed: la velocidad de la flecha con lo que lleve (arco o ballesta).
func (f *Fighter) bowSpeed() float64 {
	slot, ok := f.bowSlot()
	if !ok {
		return arrowSpeed
	}
	inv, _ := f.inventory()
	switch nameOf(inv[slot]) {
	case "minecraft:crossbow":
		return crossbowSpeed
	case "minecraft:trident":
		return tridentSpeed
	}
	return arrowSpeed
}

// arrowPathClear: la flecha, con la puntería de aimBow, vuela hasta e sin
// dar en un bloque (el arco de verdad: gravedad y rozamiento, paso a paso y
// en trozos de un cuarto de bloque).
func (f *Fighter) arrowPathClear(pos basic.Position, e entities.Entity, vx, vz, speed float64) bool {
	yaw, pitch, ok := f.aimBow(pos, e, vx, vz, speed)
	if !ok {
		return false
	}
	x, y, z := pos.X, pos.Y+1.62-0.1, pos.Z
	ry, up := float64(yaw)*math.Pi/180, -float64(pitch)*math.Pi/180
	dx, dz := -math.Sin(ry), math.Cos(ry)
	h, v := speed*math.Cos(up), speed*math.Sin(up)
	// hasta donde estará él (lo que se mueve mientras vuela), menos medio cuerpo
	_, ticks, _ := ballistic(math.Hypot(e.X-x, e.Z-z), e.Y+0.9-y, speed)
	lead := float64(ticks + aimLead)
	far := math.Hypot(e.X+vx*lead-x, e.Z+vz*lead-z) - 0.4
	gone := 0.0
	for n := 0; n < 100 && gone < far; n++ {
		const parts = 8
		for k := 0; k < parts && gone < far; k++ {
			x, z = x+dx*h/parts, z+dz*h/parts
			y += v / parts
			gone += h / parts
			if f.solid(world.BlockPos{X: int(math.Floor(x)), Y: int(math.Floor(y)), Z: int(math.Floor(z))}) {
				return false
			}
		}
		h, v = h*arrowDrag, v*arrowDrag-arrowGravity
	}
	return true
}

// dropDraw deja de tensar sin soltar (sin disparo): el cambio de objeto que
// viene (el arma) corta el uso en el servidor. Para cuando la flecha daría en
// un bloque.
func (f *Fighter) dropDraw() { f.t.drawing = false }

// stopDraw suelta el arco (si estaba tensando, es el disparo).
func (f *Fighter) stopDraw() {
	if !f.t.drawing {
		return
	}
	a := play.PlayerAction{Action: types.PlayerActionActionReleaseUseItem}
	_ = f.d.Client.Conn.WritePacket(pk.Marshal(a.PacketID(), a))
	f.t.drawing = false
}

// Shots es cuántas flechas ha tirado (para los tests y el log).
func (f *Fighter) Shots() int64 { return f.shots.Load() }

// bowFight: con el arco contra t (lo pone en la mano). Dice si va con el arco.
func (f *Fighter) bowFight(ctx context.Context, t *foe) bool {
	if !f.wantBow(t) {
		return false
	}
	slot, _ := f.bowSlot()
	inv, held := f.inventory()
	if name := nameOf(inv[slot]); slot != slotHotbar+held {
		if f.d.Walker.Building() || !f.holdChecked(ctx, slot, name) {
			return false
		}
	}
	if cur := *f.plan.Load(); !cur.bow || cur.target != t.id {
		f.logf("arco contra %s (a %.0f bloques)", t.name, t.dist)
	}
	f.stopMoving()
	f.setPlan(plan{mode: fighting, target: t.id, closeIn: true, bow: true})
	return true
}

// bowTries: las flechas a uno subido a algo antes de ir a por él con bloques.
const bowTries = 5

// bowFailed: tirándole de donde se subió, bowTries flechas y no bajó: si hay
// bloques para subir, se deja el arco un rato (30 s) y se va a por él.
func (f *Fighter) bowFailed(t *foe) bool {
	if f.tac.id != t.id {
		return false
	}
	if !f.tac.bowOn || t.e.Y < f.tac.bowY-1 {
		f.tac.bowOn, f.tac.bowShots, f.tac.bowY = true, f.Shots(), t.e.Y // (empieza, o bajó y se subió a otro sitio)
		return false
	}
	if f.Shots()-f.tac.bowShots < bowTries {
		return false
	}
	climb := t.e.Y - f.d.Player.Position().Y
	if f.d.Walker.BuildBlocks() < int(math.Ceil(climb))+1 {
		f.tac.bowShots = f.Shots() // (sin bloques para subir: otra tanda)
		return false
	}
	f.logf("%d flechas y %s sigue arriba: subo a por él con bloques", bowTries, t.name)
	f.tac.bowOn, f.tac.bowOff = false, f.steps+600
	return true
}
