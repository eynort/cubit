package automation

import (
	"math"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// Lo que ve un jugador (la regla anti-xray de /mine).
//
// El mundo cargado trae TODOS los bloques, también los que están dentro de la
// roca, y el servidor los manda enteros. Un jugador no los ve: ve las caras
// de los bloques que tocan aire por donde le llega la vista. Aquí, un bloque
// "se ve" solo si
//
//  1. alguna de sus seis caras toca una celda sin colisión (aire, agua, una
//     antorcha…), y
//  2. los ojos ven en línea recta el centro de esa celda, sin nada macizo por
//     el camino.
//
// Lo que no cumple eso no se mira siquiera: el nombre del bloque solo se lee
// cuando ya se sabe que se ve (visibleOres). Un mineral dentro de la roca no
// existe para el bot hasta que cavar (el túnel, una rama) destapa una cara.

// sightRadius: hasta dónde se mira (en bloques, en cada eje).
const sightRadius = 7

// airCell: p está cargado y no estorba (se ve a través y se anda por él).
func (j *Job) airCell(p world.BlockPos) bool {
	st, ok := j.r.d.World.BlockAt(p)
	return ok && len(block.CollisionShape(st)) == 0
}

// clearLine: desde (ex, ey, ez) se llega en línea recta al centro de la celda
// to sin cruzar ninguna celda maciza (la propia to no cuenta).
func (j *Job) clearLine(ex, ey, ez float64, to world.BlockPos) bool {
	tx, ty, tz := float64(to.X)+0.5, float64(to.Y)+0.5, float64(to.Z)+0.5
	dx, dy, dz := tx-ex, ty-ey, tz-ez
	x, y, z := int(math.Floor(ex)), int(math.Floor(ey)), int(math.Floor(ez))
	sx, sy, sz := step(dx), step(dy), step(dz)
	// distancia (en t, de 0 a 1) hasta cruzar la primera cara de cada eje
	tMax := func(pos float64, cell int, d float64, s int) float64 {
		if s == 0 {
			return math.Inf(1)
		}
		edge := float64(cell)
		if s > 0 {
			edge++
		}
		return (edge - pos) / d
	}
	tDelta := func(d float64, s int) float64 {
		if s == 0 {
			return math.Inf(1)
		}
		return math.Abs(1 / d)
	}
	mx, my, mz := tMax(ex, x, dx, sx), tMax(ey, y, dy, sy), tMax(ez, z, dz, sz)
	ddx, ddy, ddz := tDelta(dx, sx), tDelta(dy, sy), tDelta(dz, sz)
	for i := 0; i < 64; i++ {
		c := world.BlockPos{X: x, Y: y, Z: z}
		if c == to {
			return true
		}
		if !j.airCell(c) {
			return false
		}
		switch {
		case mx <= my && mx <= mz:
			if mx > 1 {
				return false
			}
			x, mx = x+sx, mx+ddx
		case my <= mz:
			if my > 1 {
				return false
			}
			y, my = y+sy, my+ddy
		default:
			if mz > 1 {
				return false
			}
			z, mz = z+sz, mz+ddz
		}
	}
	return false
}

func step(d float64) int {
	switch {
	case d > 0:
		return 1
	case d < 0:
		return -1
	}
	return 0
}

// seen: el bloque p se ve desde donde está el bot (las dos condiciones de
// arriba), con los ojos o agachado (a la altura del pecho: por el hueco de un
// bloque de alto se ve la veta que sigue). No lee qué bloque es p.
func (j *Job) seen(p world.BlockPos) bool {
	e := j.Pos()
	for _, d := range faces {
		n := add(p, d)
		if !j.airCell(n) {
			continue
		}
		if j.clearLine(e.X, e.Y+1.62, e.Z, n) || j.clearLine(e.X, e.Y+0.6, e.Z, n) {
			return true
		}
	}
	return false
}

// visibleOres son los bloques que cumplen ok (por el nombre) de los que se
// ven ahora mismo, a menos de radius en cada eje, del más cercano al más
// lejano. Primero se mira si se ve y solo entonces qué es.
func (j *Job) visibleOres(radius int, ok func(string) bool, skip map[world.BlockPos]bool) []world.BlockPos {
	f := j.Feet()
	e := j.Pos()
	type cand struct {
		p world.BlockPos
		d float64
	}
	var cs []cand
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				p := world.BlockPos{X: f.X + dx, Y: f.Y + dy, Z: f.Z + dz}
				if skip[p] || !j.seen(p) {
					continue
				}
				if n := j.Block(p); n != "" && ok(n) {
					ddx, ddy, ddz := float64(p.X)+0.5-e.X, float64(p.Y)+0.5-(e.Y+1.62), float64(p.Z)+0.5-e.Z
					cs = append(cs, cand{p, math.Sqrt(ddx*ddx + ddy*ddy + ddz*ddz)})
				}
			}
		}
	}
	for i := 1; i < len(cs); i++ { // (insertion sort: pocas)
		for k := i; k > 0 && cs[k].d < cs[k-1].d; k-- {
			cs[k], cs[k-1] = cs[k-1], cs[k]
		}
	}
	out := make([]world.BlockPos, len(cs))
	for i, c := range cs {
		out[i] = c.p
	}
	return out
}
