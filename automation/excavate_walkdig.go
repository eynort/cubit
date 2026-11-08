package automation

import (
	"math"
	"sort"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/world"
)

// Moverse sin soltar el botón: mientras /excavate va de un sitio a otro, pica
// a la vez el bloque de la pasada más cercano que tenga a mano. Y recoge por
// el camino: lo que hay en el suelo cerca, a su altura y de camino (lo que
// queda, collectPass al final de la pasada). Recoger además alrededor de cada
// sitio antes de irse salía más lento (lo recién caído no se coge en 10 ticks).

// pickDetour: hasta dónde se desvía a por algo del suelo yendo a otro sitio.
const pickDetour = 4.0

// digWhileWalking va derecho a la celda s (por un paso abierto, a la misma
// altura) picando por el camino y cogiendo lo que haya en el suelo de camino.
// Lo que no se rompe en el camino lo pica al llegar (digBatch).
func (e *excavation) digWhileWalking(s world.BlockPos, y int) error {
	j := e.j
	tx, tz := float64(s.X)+0.5, float64(s.Z)+0.5
	skip := map[int32]bool{}
	arrived, err := e.moveDigging(y, skip, 200, func(me world.BlockPos, px, pz float64) (float64, float64, int32, bool) {
		if math.Hypot(tx-px, tz-pz) < 0.35 && me == s {
			return 0, 0, 0, true
		}
		// algo del suelo de camino (no más lejos del destino que el bot)
		toGoal := math.Hypot(tx-px, tz-pz)
		if it, ok := e.itemNear(me, px, pz, pickDetour, skip, func(ix, iz float64) bool {
			return math.Hypot(tx-ix, tz-iz) <= toGoal+1
		}); ok {
			return it.X, it.Z, it.ID, false
		}
		return tx, tz, 0, false
	})
	if err != nil || arrived {
		return err
	}
	if j.Feet() == s {
		return nil
	}
	return j.GoTo(s)
}

// moveDigging es el bucle: cada tick, goal dice hacia dónde ir
// (x, z, el objeto que se persigue o 0) o que ya está; el bot va con las
// teclas que tocan según mira, y pica a la vez lo que tenga a mano. Un objeto
// al que no se acerca en 12 ticks se salta; el destino, a los 30 se deja (y
// devuelve false: algo en medio). true: llegó.
func (e *excavation) moveDigging(y int, skip map[int32]bool, maxTicks int,
	goal func(me world.BlockPos, px, pz float64) (x, z float64, item int32, done bool)) (bool, error) {
	j := e.j
	ctl := j.r.d.Ctl
	hands := j.r.d.Hands
	j.r.d.Walker.Stop()
	defer ctl.SetInput(control.Input{})
	type res struct {
		c    world.BlockPos
		name string
		d    act.Dig
	}
	done := make(chan res, 1)
	digging := false
	finishDig := func(r res) {
		digging = false
		if r.d.Err == nil {
			e.dug++
			e.noteDug(r.c)
			e.noteBroken(r.c, r.name)
		}
	}
	defer func() { // (una picada a medias al acabar: se espera, que es de la pasada)
		for t := 0; t < 40 && digging; t++ {
			select {
			case r := <-done:
				finishDig(r)
			default:
				if ctl.WaitTicks(j.ctx, 1) != nil {
					hands.CancelDig()
					return
				}
			}
		}
	}()
	chasing, best, still := int32(-1), math.Inf(1), 0
	for t := 0; t < maxTicks; t++ {
		select {
		case r := <-done:
			finishDig(r)
		default:
		}
		pos := j.Pos()
		x, z, item, arrived := goal(j.Feet(), pos.X, pos.Z)
		if arrived {
			ctl.SetInput(control.Input{})
			return true, nil
		}
		if !digging && !hands.Busy() {
			if c, ok := e.nextOnTheWay(y); ok {
				name := j.Block(c)
				j.HoldBestTool(name)
				digging = true
				hands.Dig(c, func(d act.Dig) { done <- res{c, name, d} })
			}
		}
		d := math.Hypot(x-pos.X, z-pos.Z)
		if item != chasing { // otro objetivo (el anterior, cogido o saltado)
			chasing, best, still = item, d, 0
		} else if d < best-0.02 {
			best, still = d, 0
		} else if still++; item != 0 && still > 12 {
			skip[item] = true // (a ese objeto no se acerca)
		} else if item == 0 && still > 30 {
			break // (algo en medio)
		}
		ctl.SetInput(e.keysToward(x, z, d))
		if err := j.Wait(1); err != nil {
			return false, err
		}
	}
	ctl.SetInput(control.Input{})
	return false, nil
}

// pickable: el objeto lleva en el suelo lo bastante para poder cogerlo (lo
// recién caído no se coge en 10 ticks: ir a por él es esperar encima).
func (e *excavation) pickable(id int32) bool {
	now := e.j.r.d.Ctl.State().Ticks
	if e.seen == nil {
		e.seen = map[int32]uint64{}
	}
	t, ok := e.seen[id]
	if !ok {
		e.seen[id] = now
		return false
	}
	return now-t >= 10
}

// itemNear es el objeto del suelo más cercano a menos de r (en horizontal)
// que se puede coger andando derecho: a la altura de los pies, quieto en el
// suelo, sin fuego, con el paso abierto (y que cumpla ok, si se da).
func (e *excavation) itemNear(me world.BlockPos, px, pz, r float64, skip map[int32]bool, ok func(x, z float64) bool) (entities.Entity, bool) {
	j := e.j
	var best entities.Entity
	bestD := math.Inf(1)
	for _, it := range j.r.d.Entities.Nearby(px, float64(me.Y), pz, r+1) {
		if it.Type != "minecraft:item" || skip[it.ID] || !e.pickable(it.ID) {
			continue
		}
		d := math.Hypot(it.X-px, it.Z-pz)
		if d > r || d >= bestD {
			continue
		}
		c := itemCell(it)
		under := c
		under.Y--
		if c.Y != me.Y || !j.solid(under) || j.burning(c) || !e.box.Contains(c) || !e.lineClear(me, c) {
			continue
		}
		if ok != nil && !ok(it.X, it.Z) {
			continue
		}
		best, bestD = it, d
	}
	return best, bestD < math.Inf(1)
}

// nextOnTheWay es el bloque de la pasada de fondo y que picar ya, andando:
// al alcance, con una cara al aire, sin agua ni lava al lado, sin grava
// encima (eso, parado: digBatch) y que se pueda picar con lo que lleva sin
// gastar picos reservados; el más cercano a los ojos.
func (e *excavation) nextOnTheWay(y int) (world.BlockPos, bool) {
	j := e.j
	p := j.Pos()
	cells := e.reachable(y, j.Feet(), [3]float64{p.X, p.Y + 1.62, p.Z})
	sort.Slice(cells, func(a, b int) bool { return j.eyeDist(cells[a]) < j.eyeDist(cells[b]) })
	for _, c := range cells {
		if !j.Reaches(c) || !j.canDig(c) || !j.safeToOpen(c) || j.fallsAbove(c) || j.dropsOnMe(c) {
			continue
		}
		if toolFor(j.Block(c)) == "pickaxe" && j.pickFloor > 0 && j.bestPickSlot(j.pickFloor) < 0 {
			continue // (sin picos que se puedan usar: ya lo resolverá digBatch)
		}
		return c, true
	}
	return world.BlockPos{}, false
}

// keysToward son las teclas para ir hacia x, z mirando a donde mira ahora (el
// bloque que pica): adelante/atrás y a los lados según el ángulo, como hace
// un jugador; corre solo si va casi de frente y lejos.
func (e *excavation) keysToward(x, z, dist float64) control.Input {
	pos := e.j.Pos()
	dx, dz := x-pos.X, z-pos.Z
	if n := math.Hypot(dx, dz); n > 0 {
		dx, dz = dx/n, dz/n
	}
	a := float64(pos.Yaw) * math.Pi / 180
	fwd := dx*-math.Sin(a) + dz*math.Cos(a) // hacia donde mira
	left := dx*math.Cos(a) + dz*math.Sin(a) // a su izquierda
	in := control.Input{
		Forward:  fwd > 0.38,
		Backward: fwd < -0.38,
		Left:     left > 0.38,
		Right:    left < -0.38,
	}
	if dist < 0.25 { // encima: quieto (lo coge solo)
		return control.Input{}
	}
	in.Sprint = in.Forward && fwd > 0.9 && dist > 1.5
	return in
}
