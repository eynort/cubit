package automation

import (
	"errors"
	"fmt"
	"math"

	"github.com/mj41/go-mc26-kit/bot/world"
)

// Cada pasada de /excavate (passMax = 4 bloques de alto), por alcance: el
// bot anda por el fondo de la pasada (el suelo es la pasada de debajo,
// intacta) y, desde cada sitio, pica toda la altura de la pasada que alcanza
// alrededor (unos 4 bloques); luego va al sitio desde el que más le queda, y
// así hasta vaciarla en toda la caja: la caja baja 4 bloques pareja, y luego
// los 4 siguientes. Para llegar al fondo de una pasada baja por una escalera
// que cava dentro de ella. Nunca pica por debajo del fondo de la pasada: lo
// que cae se queda en ese suelo llano, y se recoge al acabarla (collectPass).

// stationTries: sitios seguidos a los que no llega (o desde los que no pica
// nada) antes de dejar lo que quede de la capa al bloque a bloque.
const stationTries = 6

// stationNear: hasta dónde se buscan sitios primero (con una caja enorme,
// mirar todas las celdas en cada parada sería lento).
const stationNear = 12

// layerReach vacía la pasada que tiene el fondo en y (e.h de alto) por alcance.
func (e *excavation) layerReach(y int) error {
	j := e.j
	if err := e.enterLayer(y); err != nil {
		return fmt.Errorf("bajar al fondo de la pasada (%d): %w", y, err)
	}
	tried := map[world.BlockPos]int{}
	misses := 0
	for {
		if err := j.Wait(0); err != nil {
			return err
		}
		if j.FreeSlots() < 3 { // lleno: a guardar, y de vuelta a la capa
			if err := e.stash(false); err != nil {
				return err
			}
			if err := e.enterLayer(y); err != nil {
				return fmt.Errorf("volver al fondo de la pasada (%d): %w", y, err)
			}
		}
		// por debajo del fondo de la pasada (se cayó a un hoyo): a subir
		// cavando una escalera, no esperar a un camino que no hay (sin poder
		// construir, el pathfinding no sube de un hoyo)
		if j.Feet().Y < y {
			if err := e.climbTo(y); err != nil {
				if j.Fatal() != nil {
					return j.Fatal()
				}
				return fmt.Errorf("salir del hoyo a la pasada (%d): %w", y, err)
			}
		}
		before := e.dug
		if j.Feet().Y == y {
			p := j.Pos()
			if cells := e.reachable(y, j.Feet(), [3]float64{p.X, p.Y + 1.62, p.Z}); len(cells) > 0 {
				err := e.digBatch(cells)
				if err != nil && (j.Fatal() != nil || errors.Is(err, errNoRoom) || errors.Is(err, errNoPicksLeft)) {
					return err
				}
			}
		}
		if e.dug > before {
			misses = 0

		}
		st, ok := e.bestStation(y, tried)
		if !ok {
			return nil // (lo que quede: bloque a bloque, en layer)
		}
		tried[st]++
		j.Status("excavar: capas %d-%d, a %v (llevo %d)", y, e.passTop(y), st, e.dug)
		if err := e.moveTo(st); err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			tried[st] += 2 // (ahí no se llega)
		}
		if e.dug == before {
			if misses++; misses >= stationTries {
				return nil
			}
		}
	}
}

// reachable: las celdas de la pasada de fondo y por cavar al alcance de los
// ojos eye (estando con los pies en f), de arriba abajo; nunca por debajo de
// y (el fondo) ni de los pies.
func (e *excavation) reachable(y int, f world.BlockPos, eye [3]float64) []world.BlockPos {
	var out []world.BlockPos
	r := int(math.Ceil(reachDist))
	for cy := e.passTop(y); cy >= max(y, f.Y); cy-- {
		for x := f.X - r; x <= f.X+r; x++ {
			for z := f.Z - r; z <= f.Z+r; z++ {
				c := world.BlockPos{X: x, Y: cy, Z: z}
				if !e.box.Contains(c) {
					continue
				}
				dx, dy, dz := float64(x)+0.5-eye[0], float64(cy)+0.5-eye[1], float64(z)+0.5-eye[2]
				if dx*dx+dy*dy+dz*dz > reachDist*reachDist || !e.diggable(c) {
					continue
				}
				out = append(out, c)
			}
		}
	}
	return out
}

// bestStation es el siguiente sitio de la capa (los pies en y, dentro de la
// caja, donde quepa el bot): con más por picar al alcance y cerca (lo lejos
// resta; sin paso derecho, más). Primero cerca; si nada, en toda la capa.
// ok false: ninguno sirve.
func (e *excavation) bestStation(y int, tried map[world.BlockPos]int) (world.BlockPos, bool) {
	j := e.j
	f := j.Feet()
	for _, rad := range []int{stationNear, 1 << 20} {
		var best world.BlockPos
		bestV := 0.0
		for x := max(e.box.Min.X, f.X-rad); x <= min(e.box.Max.X, f.X+rad); x++ {
			for z := max(e.box.Min.Z, f.Z-rad); z <= min(e.box.Max.Z, f.Z+rad); z++ {
				s := world.BlockPos{X: x, Y: y, Z: z}
				if tried[s] >= 2 || !e.standHere(s) {
					continue
				}
				n := float64(len(e.reachable(y, s, [3]float64{float64(x) + 0.5, float64(y) + 1.62, float64(z) + 0.5})))
				if n == 0 {
					continue
				}
				d := math.Hypot(float64(x-f.X), float64(z-f.Z))
				if f.Y != y || !e.lineClear(f, s) {
					d += 8
				}
				if v := n / (1 + d/3); v > bestV {
					best, bestV = s, v
				}
			}
		}
		if bestV > 0 {
			return best, true
		}
	}
	return world.BlockPos{}, false
}

// moveTo va a la celda s: derecho si está a la misma altura y con el paso
// abierto (sin buscar camino); si no, con el pathfinding.
func (e *excavation) moveTo(s world.BlockPos) error {
	j := e.j
	if f := j.Feet(); f.Y == s.Y && e.lineClear(f, s) {
		return e.digWhileWalking(s, s.Y) // (picando por el camino: sin soltar el botón)
	}
	err := j.GoTo(s)
	if err == nil || j.Fatal() != nil {
		return err
	}
	// sin camino andando (un hoyo, o el servidor no deja construir torres ni
	// puentes): cavando hasta allí, en vez de probar otro sitio y otro
	j.Status("excavar: sin camino a %v: cavo hasta allí", s)
	return j.standAt(s)
}

// climbTo sube cavando una escalera hasta tener los pies en la capa y
// (dentro de la caja, hacia su celda más cercana): para salir de un hoyo al
// que se cayó por debajo de la pasada.
func (e *excavation) climbTo(y int) error {
	j := e.j
	f := j.Feet()
	// la escalera sube un bloque por cada paso: el destino, en la dirección
	// con más sitio dentro de la caja, a tantos pasos como hay que subir (y uno más)
	c := world.BlockPos{X: min(max(f.X, e.box.Min.X), e.box.Max.X), Y: y, Z: min(max(f.Z, e.box.Min.Z), e.box.Max.Z)}
	need := y - f.Y + 1
	best, room := [2]int{1, 0}, -1
	for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		r := 0
		switch d {
		case [2]int{1, 0}:
			r = e.box.Max.X - c.X
		case [2]int{-1, 0}:
			r = c.X - e.box.Min.X
		case [2]int{0, 1}:
			r = e.box.Max.Z - c.Z
		case [2]int{0, -1}:
			r = c.Z - e.box.Min.Z
		}
		if r > room {
			best, room = d, r
		}
	}
	steps := min(max(need, 1), max(room, 1))
	// (apuntando por encima de la capa: el túnel no pisa su destino, y se
	// para en cuanto los pies llegan a y)
	t := world.BlockPos{X: c.X + best[0]*steps, Y: y + 3, Z: c.Z + best[1]*steps}
	j.Logf("excavar: estoy en y=%d, por debajo de la pasada (%d): subo cavando una escalera", f.Y, y)
	return j.tunnelTo(t, func() bool { return j.Feet().Y >= y })
}

// enterLayer lleva al bot al fondo de la pasada (los pies en y, dentro de
// la caja): andando si ya hay un sitio abierto ahí (al volver de guardar); si
// no, bajando por una escalera que cava dentro de la caja (solo bloques de la
// pasada, que hay que quitar igual).
func (e *excavation) enterLayer(y int) error {
	j := e.j
	if f := j.Feet(); f.Y == y && e.inside(f) {
		return nil
	}
	f := j.Feet()
	var open world.BlockPos
	bestD := math.Inf(1)
	for x := e.box.Min.X; x <= e.box.Max.X; x++ {
		for z := e.box.Min.Z; z <= e.box.Max.Z; z++ {
			s := world.BlockPos{X: x, Y: y, Z: z}
			if e.standHere(s) {
				if d := math.Hypot(float64(x-f.X), float64(z-f.Z)); d < bestD {
					open, bestD = s, d
				}
			}
		}
	}
	if bestD < math.Inf(1) {
		err := j.GoTo(open)
		if err == nil || j.Fatal() != nil {
			return err
		}
	}
	return e.stairsDown(y)
}

// inside: la celda está dentro de la caja en horizontal.
func (e *excavation) inside(p world.BlockPos) bool {
	return p.X >= e.box.Min.X && p.X <= e.box.Max.X && p.Z >= e.box.Min.Z && p.Z <= e.box.Max.Z
}

// stairsDown baja hasta y por una escalera que cava dentro de la caja:
// primero se pone sobre la caja (en su borde más cercano), luego escalón a
// escalón (uno adelante y uno abajo: abre pies, cabeza y lo de encima),
// hacia el centro de la caja.
func (e *excavation) stairsDown(y int) error {
	j := e.j
	if f := j.Feet(); !e.inside(f) {
		// a la columna con trabajo de la pasada más cercana (no al borde de
		// la caja: ahí puede no haber nada que cavar en estas capas)
		x, z := min(max(f.X, e.box.Min.X), e.box.Max.X), min(max(f.Z, e.box.Min.Z), e.box.Max.Z)
		if c, ok := e.nearestWork(y); ok {
			x, z = c.X, c.Z
		}
		g, ok := j.r.d.Walker.Ground(x, z, max(f.Y, y))
		if !ok {
			return fmt.Errorf("no encuentro dónde ponerme sobre la caja en %d %d", x, z)
		}
		if err := j.GoTo(g); err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			// sin camino andando (un hoyo, sin poder construir): cavando hasta allí
			j.Status("excavar: sin camino a la caja (%v): cavo hasta allí", g)
			if err := j.standAt(g); err != nil {
				return err
			}
		}
	}
	cx, cz := (e.box.Min.X+e.box.Max.X)/2, (e.box.Min.Z+e.box.Max.Z)/2
	for steps := 0; j.Feet().Y > y; steps++ {
		if steps > 2*(e.box.Max.Y-e.box.Min.Y)+40 {
			return errors.New("la escalera no llega")
		}
		if err := j.Wait(0); err != nil {
			return err
		}
		f := j.Feet()
		j.Status("excavar: bajando al fondo de la pasada (%d; en %d)", y, f.Y)
		dirs := [][2]int{{sign(cx - f.X), 0}, {0, sign(cz - f.Z)}, {1, 0}, {-1, 0}, {0, 1}, {0, -1}}
		if abs(cz-f.Z) > abs(cx-f.X) {
			dirs[0], dirs[1] = dirs[1], dirs[0]
		}
		moved := false
		for _, d := range dirs {
			if d == [2]int{0, 0} {
				continue
			}
			n := world.BlockPos{X: f.X + d[0], Y: f.Y - 1, Z: f.Z + d[1]}
			floor := world.BlockPos{X: n.X, Y: n.Y - 1, Z: n.Z}
			if !e.inside(n) || j.liquid(floor) {
				continue
			}
			ok := true
			for _, c := range []world.BlockPos{{X: n.X, Y: n.Y + 2, Z: n.Z}, {X: n.X, Y: n.Y + 1, Z: n.Z}, n} {
				if !j.solid(c) && !j.fluidBlock(c) {
					continue
				}
				if !e.box.Contains(c) || !e.diggable(c) {
					ok = false // (fuera de la caja no se toca)
					break
				}
				if err := e.cell(c, false); err != nil {
					return err
				}
			}
			if !ok || j.solid(n) || j.solid(world.BlockPos{X: n.X, Y: n.Y + 1, Z: n.Z}) {
				continue
			}
			if err := j.GoTo(n); err != nil {
				continue
			}
			moved = true
			break
		}
		if !moved {
			return fmt.Errorf("no puedo bajar desde %v", f)
		}
	}
	return nil
}

// lineClear: de a hasta b (a la misma altura) se puede andar en línea recta:
// cada celda del camino con hueco para el cuerpo y suelo firme.
func (e *excavation) lineClear(a, b world.BlockPos) bool {
	n := max(abs(b.X-a.X), abs(b.Z-a.Z))
	for i := 1; i <= n; i++ {
		x := a.X + int(math.Round(float64((b.X-a.X)*i)/float64(n)))
		z := a.Z + int(math.Round(float64((b.Z-a.Z)*i)/float64(n)))
		if !e.standHere(world.BlockPos{X: x, Y: a.Y, Z: z}) {
			return false
		}
	}
	return true
}

// standHere: en s cabe el bot (pies y cabeza libres, sin agua ni lava) y
// tiene suelo firme debajo.
func (e *excavation) standHere(s world.BlockPos) bool {
	j := e.j
	head := world.BlockPos{X: s.X, Y: s.Y + 1, Z: s.Z}
	floor := world.BlockPos{X: s.X, Y: s.Y - 1, Z: s.Z}
	if _, ok := j.r.d.World.BlockAt(s); !ok {
		return false
	}
	return !j.solid(s) && !j.solid(head) && !j.fluidBlock(s) && !j.fluidBlock(head) &&
		j.solid(floor) && !j.liquid(floor) && !j.protected(s)
}

// nearestWork es la celda por cavar de la pasada que anda por L más cercana
// al bot (en horizontal).
func (e *excavation) nearestWork(L int) (world.BlockPos, bool) {
	j := e.j
	f := j.Feet()
	var best world.BlockPos
	bestD := math.Inf(1)
	for y := L; y <= e.passTop(L); y++ {
		for x := e.box.Min.X; x <= e.box.Max.X; x++ {
			for z := e.box.Min.Z; z <= e.box.Max.Z; z++ {
				p := world.BlockPos{X: x, Y: y, Z: z}
				if !e.diggable(p) {
					continue
				}
				if d := math.Hypot(float64(x-f.X), float64(z-f.Z)); d < bestD {
					best, bestD = p, d
				}
			}
		}
	}
	return best, bestD < math.Inf(1)
}
