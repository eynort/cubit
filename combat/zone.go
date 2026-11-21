package combat

import (
	"fmt"
	"math"
	"time"
)

// Zone es un rectángulo del mundo (en x y z, cualquier altura): con una zona
// puesta el bot pelea con todo jugador que esté dentro (la orden "fight in").
type Zone struct {
	MinX, MaxX, MinZ, MaxZ float64
}

// ZoneAround es la zona de radio r alrededor de (x, z) (un cuadrado: en
// Minecraft se piensa en bloques, no en círculos).
func ZoneAround(x, z, r float64) Zone {
	return Zone{MinX: x - r, MaxX: x + r, MinZ: z - r, MaxZ: z + r}
}

// ZoneBetween es la zona entre dos esquinas (de bloques: ambas incluidas).
func ZoneBetween(x1, z1, x2, z2 float64) Zone {
	return Zone{
		MinX: math.Floor(math.Min(x1, x2)), MaxX: math.Floor(math.Max(x1, x2)) + 1,
		MinZ: math.Floor(math.Min(z1, z2)), MaxZ: math.Floor(math.Max(z1, z2)) + 1,
	}
}

// Contains dice si (x, z) está dentro.
func (z Zone) Contains(x, zz float64) bool {
	return x >= z.MinX && x <= z.MaxX && zz >= z.MinZ && zz <= z.MaxZ
}

// grow es la zona con m bloques más por cada lado.
func (z Zone) grow(m float64) Zone {
	return Zone{MinX: z.MinX - m, MaxX: z.MaxX + m, MinZ: z.MinZ - m, MaxZ: z.MaxZ + m}
}

// zoneMargin: a uno de la zona se le sigue hasta que se aleja tanto del borde.
// Sin margen, uno que anda por el borde entraba y salía de la pelea cada
// segundo, y entre medias la orden en curso (follow, goto) movía al bot.
const zoneMargin = 4

// Center es el centro de la zona.
func (z Zone) Center() (float64, float64) { return (z.MinX + z.MaxX) / 2, (z.MinZ + z.MaxZ) / 2 }

func (z Zone) String() string {
	return fmt.Sprintf("x %.0f..%.0f, z %.0f..%.0f", z.MinX, z.MaxX, z.MinZ, z.MaxZ)
}

// SetZone pone (o con nil quita) la zona: desde ya pelea con quien esté dentro.
func (f *Fighter) SetZone(z *Zone) {
	f.zone.Store(z)
	if z != nil {
		f.logf("zona de pelea: %s", z)
	}
}

// CurrentZone es la zona puesta (nil: ninguna).
func (f *Fighter) CurrentZone() *Zone { return f.zone.Load() }

// scanZone marca como enemigo a todo jugador dentro de la zona y suelta a los
// que salieron (salvo que le hayan pegado al bot: a esos los sigue como siempre).
func (f *Fighter) scanZone() {
	z := f.zone.Load()
	if z == nil {
		return
	}
	p := f.d.Player.Position()
	cx, cz := z.Center()
	// todo lo que se ve a esta distancia del centro (la zona entera y algo más)
	r := math.Hypot(z.MaxX-z.MinX, z.MaxZ-z.MinZ)/2 + 64
	inside := map[int32]bool{}
	for _, e := range f.d.Entities.Nearby(cx, p.Y, cz, r) {
		if e.Type != "minecraft:player" || f.isGuarded(e) || dead(e) || f.d.Ally != nil && f.d.Ally(e.UUID) {
			continue // (los otros bots del panel no son rivales de la zona)
		}
		if !z.Contains(e.X, e.Z) {
			// justo fuera del borde: el que ya era de la zona lo sigue siendo
			if z.grow(zoneMargin).Contains(e.X, e.Z) {
				f.mu.Lock()
				if a, ok := f.aggressors[e.ID]; ok && a.fromZone {
					inside[e.ID] = true
				}
				f.mu.Unlock()
			}
			continue
		}
		inside[e.ID] = true
		f.mu.Lock()
		if until, ok := f.ignored[e.ID]; ok && f.steps < until {
			f.mu.Unlock()
			continue // no se llega a él (giveUp): un rato sin ir a por él
		}
		a, known := f.aggressors[e.ID]
		if !known {
			a = &aggressor{id: e.ID, name: f.nameOf(e.UUID, e.ID), fromZone: true}
			f.aggressors[e.ID] = a
		}
		if a.fromZone {
			a.lastHit = time.Now() // mientras esté dentro no se olvida
		}
		f.mu.Unlock()
		if !known {
			f.logf("%s está en la zona: a por él", a.name)
		}
	}
	f.mu.Lock()
	for id, a := range f.aggressors {
		if a.fromZone && !inside[id] && a.hits == 0 {
			f.logf("%s salió de la zona", a.name)
			delete(f.aggressors, id)
		}
	}
	f.mu.Unlock()
}

// toZone: sin nadie con quien pelear y fuera de la zona, volver a ella.
func (f *Fighter) toZone() {
	z := f.zone.Load()
	if z == nil {
		f.zoneGoal = false
		return
	}
	p := f.d.Player.Position()
	if z.Contains(p.X, p.Z) {
		if f.zoneGoal && f.d.Walker.Status() == "walking" {
			f.d.Walker.Stop() // ya dentro: esperar aquí
		}
		f.zoneGoal = false
		return
	}
	if f.zoneGoal && f.d.Walker.Status() == "walking" {
		return
	}
	if f.d.Ordered != nil && f.d.Ordered() {
		// otra orden lleva al bot (follow, goto, volver a donde murió, una
		// tarea): la última manda. Antes las dos le daban destino al walker a
		// cada tick y el bot iba a trompicones entre una y otra
		f.zoneGoal = false
		return
	}
	cx, cz := z.Center()
	if g, ok := f.d.Walker.Ground(int(math.Floor(cx)), int(math.Floor(cz)), int(math.Floor(p.Y))); ok {
		f.d.Walker.SetSprint(true)
		f.d.Walker.Goto(g)
		f.zoneGoal = true
	}
}
