package combat

import (
	"math"

	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"

	"minecraft-bot/pathing"
)

// Perlas de ender: el objetivo lanza una para escapar (o para caerle encima).
// Mientras vuela, el bot no va a donde está él (en un momento ya no estará
// ahí) sino a donde va a caer la perla: allí aparecerá. Cuando cae (el
// servidor la quita), se le sigue como siempre, desde ya mucho más cerca.

// pearlChase es el valor de following mientras el walker va a donde cae una perla.
const pearlChase int32 = -2

const (
	pearlGravity = 0.03 // ThrowableProjectile: la gravedad de una perla
	pearlDrag    = 0.99 // y su rozamiento por tick (en el agua 0.8)
	pearlTicks   = 200  // más no se mira (se fue al vacío, o muy lejos)
)

// pearlOf es la perla en vuelo que lanzó owner (el servidor dice quién la
// lanzó en el dato del add_entity de un proyectil).
func (f *Fighter) pearlOf(owner int32) (entities.Entity, bool) {
	p := f.d.Player.Position()
	for _, e := range f.d.Entities.Nearby(p.X, p.Y, p.Z, 128) {
		if e.Type == "minecraft:ender_pearl" && e.SpawnData == owner {
			return e, true
		}
	}
	return entities.Entity{}, false
}

// pearlLanding es dónde caerá una perla que está en pos con velocidad vel:
// ThrowableProjectile.tick (gravedad, rozamiento, y moverse hasta el primer
// bloque que toque por el camino). ok false si no cae (al vacío, o sin
// cargar por donde va).
func pearlLanding(w physics.Blocks, pos, vel physics.Vec3) (physics.Vec3, bool) {
	for t := 0; t < pearlTicks; t++ {
		vel.Y -= pearlGravity
		drag := pearlDrag
		if s, ok := w.BlockAt(world.BlockPos{X: fl(pos.X), Y: fl(pos.Y), Z: fl(pos.Z)}); ok && isWater(s) {
			drag = 0.8
		}
		vel = vel.Scale(drag)
		next := pos.Add(vel)
		if hit, at, known := firstHit(w, pos, next); !known {
			return physics.Vec3{}, false
		} else if hit {
			return at, true
		}
		pos = next
		if pos.Y < -64 {
			return physics.Vec3{}, false
		}
	}
	return physics.Vec3{}, false
}

// firstHit recorre el segmento a-b en pasos cortos: el primer punto dentro de
// la forma de un bloque. known false si pasa por algo sin cargar.
func firstHit(w physics.Blocks, a, b physics.Vec3) (hit bool, at physics.Vec3, known bool) {
	d := b.Sub(a)
	n := int(math.Ceil(math.Sqrt(d.LengthSqr())/0.25)) + 1
	prev := a
	for i := 1; i <= n; i++ {
		q := a.Add(d.Scale(float64(i) / float64(n)))
		bp := world.BlockPos{X: fl(q.X), Y: fl(q.Y), Z: fl(q.Z)}
		s, ok := w.BlockAt(bp)
		if !ok {
			return false, physics.Vec3{}, false
		}
		for _, box := range block.CollisionShape(s) {
			lx, ly, lz := q.X-float64(bp.X), q.Y-float64(bp.Y), q.Z-float64(bp.Z)
			if lx >= box.MinX && lx <= box.MaxX && ly >= box.MinY && ly <= box.MaxY && lz >= box.MinZ && lz <= box.MaxZ {
				return true, prev, true
			}
		}
		prev = q
	}
	return false, physics.Vec3{}, true
}

func isWater(s block.StateID) bool {
	return pathing.IsWaterName(world.StateString(s))
}

func fl(v float64) int { return int(math.Floor(v)) }

// chasePearl: si el objetivo id tiene una perla en vuelo, el walker va a
// donde caerá (true: el tick está decidido).
func (f *Fighter) chasePearl(id int32, name string) bool {
	pe, ok := f.pearlOf(id)
	if !ok {
		f.pearlID = 0
		return false
	}
	land, ok := pearlLanding(f.d.World, physics.Vec3{X: pe.X, Y: pe.Y, Z: pe.Z}, physics.Vec3{X: pe.VX, Y: pe.VY, Z: pe.VZ})
	if !ok {
		return false // no se sabe dónde cae: seguirlo a él como siempre
	}
	goal, ok := f.d.Walker.Ground(fl(land.X), fl(land.Z), fl(land.Y))
	if !ok {
		return false
	}
	f.chaseAt = f.steps // (ir hacia la perla es avanzar, aunque él se aleje)
	if pe.ID != f.pearlID {
		f.pearlID = pe.ID
		p := f.d.Player.Position()
		f.logf("%s lanzó una perla: voy a donde cae (%d %d %d, a %.0f bloques)", name, goal.X, goal.Y, goal.Z,
			math.Hypot(land.X-p.X, land.Z-p.Z))
	}
	if f.following != pearlChase || goal != f.pearlGoal {
		dx, dz := goal.X-f.pearlGoal.X, goal.Z-f.pearlGoal.Z
		// (la predicción se afina con cada velocidad que manda el servidor: solo
		// se cambia la meta si se movió de verdad, o el walker empieza de cero)
		if f.following != pearlChase || dx*dx+dz*dz > 4 {
			f.d.Walker.SetSprint(true)
			f.d.Walker.Goto(goal)
			f.following, f.pearlGoal = pearlChase, goal
		}
	}
	return true
}
