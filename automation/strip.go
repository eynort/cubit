package automation

import (
	"errors"
	"fmt"
	"sort"

	"github.com/mj41/go-mc26-kit/bot/world"
)

// El strip mining: galerías paralelas de 1x2 a la altura del mineral, en
// serpiente (la 0 hacia un lado, un pasillo corto de Spacing bloques, la 1 de
// vuelta, …), con coordenadas fijas desde un origen: si se aparta (a por un
// mineral que ve, por lava) vuelve a la celda que tocaba, y el viaje
// siguiente sigue en la misma galería. Cada mineral tiene su plan (su altura).

// stripPlan es el plan de galerías de un mineral.
type stripPlan struct {
	dim    string
	origin world.BlockPos // los pies al empezar la galería 0
	a      [2]int         // hacia dónde van las galerías (x o z)
	s      [2]int         // hacia dónde se pasa a la siguiente
	k      int            // la galería por la que va
	conn   bool           // en el pasillo de la galería k a la k+1
	pos    int            // lo cavado del tramo
	turns  int            // las veces que se giró el plan (una zona imposible)
	// path: el túnel desde donde empiezan los viajes hasta donde se dejó (el
	// siguiente viaje baja por él en vez de cavar otra escalera)
	path []world.BlockPos
}

// segment es el tramo en curso: desde dónde, hacia dónde y cuántos pasos.
func (p *stripPlan) segment(o Ore) (from world.BlockPos, d [2]int, n int) {
	L := o.Branch - 1
	sp := o.Spacing
	at := func(k, along int) world.BlockPos {
		return world.BlockPos{
			X: p.origin.X + k*sp*p.s[0] + along*p.a[0],
			Y: p.origin.Y,
			Z: p.origin.Z + k*sp*p.s[1] + along*p.a[1],
		}
	}
	start, end := 0, L // la galería k: de start a end a lo largo de a
	if p.k%2 == 1 {
		start, end = L, 0
	}
	if !p.conn {
		d = p.a
		if end < start {
			d = [2]int{-p.a[0], -p.a[1]}
		}
		return at(p.k, start), d, L
	}
	return at(p.k, end), p.s, sp
}

// next pasa al tramo siguiente.
func (p *stripPlan) next() {
	if p.conn {
		p.k++
	}
	p.conn = !p.conn
	p.pos = 0
}

// skipBranch deja la galería (o el pasillo) y va al principio de la siguiente.
func (p *stripPlan) skipBranch() {
	p.k++
	p.conn = false
	p.pos = 0
}

// newStripPlan empieza un plan aquí (los pies a la altura del mineral),
// girado turns cuartos de vuelta.
func newStripPlan(at world.BlockPos, dim string, turns int) *stripPlan {
	dirs := [][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}}
	return &stripPlan{dim: dim, origin: at, a: dirs[turns%4], s: dirs[(turns+1)%4], turns: turns}
}

// levelTo lleva los pies a la altura y (bajando o subiendo en escalera).
func (j *Job) levelTo(y int) error {
	f := j.Feet()
	switch {
	case f.Y > y+1:
		return j.DescendTo(y)
	case f.Y < y-1:
		j.Logf("subo cavando hasta y=%d", y)
		to := world.BlockPos{X: f.X + 8, Y: y, Z: f.Z}
		if err := j.tunnelTo(to, func() bool { return j.Feet().Y >= y-1 }); err != nil {
			return fmt.Errorf("subir a %d: %w", y, err)
		}
	}
	return nil
}

// standAt deja los pies en p (andando, o cavando hasta allí).
func (j *Job) standAt(p world.BlockPos) error {
	if j.Feet() == p {
		return nil
	}
	if err := j.GoTo(p); err == nil && j.Feet() == p {
		return nil
	}
	if err := j.TunnelTo(p); err != nil {
		return err
	}
	for _, c := range []world.BlockPos{{X: p.X, Y: p.Y + 1, Z: p.Z}, p} {
		if err := j.clearCell(c); err != nil {
			return err
		}
	}
	if f := j.Feet(); f != p && (abs(f.X-p.X)+abs(f.Z-p.Z) > 1 || f.Y != p.Y) {
		// no al lado: una vuelta más de túnel
		if err := j.TunnelTo(p); err != nil {
			return err
		}
	}
	if len(j.trail) == 0 || j.trail[len(j.trail)-1] != j.Feet() {
		j.trail = append(j.trail, j.Feet())
	}
	return j.GoTo(p)
}

// stripMine hace las galerías del mineral o (con su plan; nuevo si no hay o
// está en otra dimensión o lejos) sacando lo que cumpla ok (todos los
// minerales pedidos) hasta have() >= goal, la bolsa llena o stop.
func (j *Job) stripMine(o Ore, ok func(string) bool, have func() int, goal int, stop func() error) error {
	plan := j.r.strips[o.Key]
	f := j.Feet()
	if plan != nil && (plan.dim != j.Dimension() || abs(plan.origin.X-f.X)+abs(plan.origin.Z-f.Z) > 400) {
		j.Logf("%s: el plan de galerías de antes queda lejos: empiezo otro aquí", o.What)
		plan = nil
	}
	if plan == nil {
		if err := j.levelTo(o.Y); err != nil {
			return err
		}
		plan = newStripPlan(j.Feet(), j.Dimension(), 0)
		j.r.strips[o.Key] = plan
		j.Logf("%s: galerías desde %v, de %d bloques cada %d", o.What, plan.origin, o.Branch, o.Spacing)
	}
	skip := map[world.BlockPos]bool{}
	oreFails := map[world.BlockPos]int{} // los intentos fallidos de llegar a cada mena
	defer func() {                       // lo que se vio y no se pudo sacar
		n := 0
		for q := range skip {
			if ok(j.Block(q)) {
				n++
			}
		}
		if n > 0 {
			j.Logf("%s: %d a la vista que no pude sacar (lava, agua o sin sitio)", o.What, n)
		}
	}()
	fails := 0
	for {
		if err := j.Wait(1); err != nil {
			return err
		}
		if stop != nil {
			if err := stop(); err != nil {
				return err
			}
		}
		if have() >= goal {
			j.sweepOres(ok, skip) // lo de alrededor no se deja para otro viaje
			return nil
		}
		j.lootCheck(have(), []Ore{o}) // (sin dónde guardar: con muchos, se avisa)
		j.makeRoom()
		if j.FreeSlots() == 0 {
			return errBagFull
		}
		if stop != nil && stop() != nil {
			j.sweepOres(ok, skip)
		}
		j.Status("galerías de %s: %d/%d (galería %d)", o.What, have(), goal, plan.k)
		// lo que se ve cerca, primero (lo destapado al cavar, una cueva)
		if p, found := j.findBlock(max(o.Detour, 5), 3, 4, ok, skip); found {
			j.takeOre(p, ok, skip, oreFails, have, goal)
			if j.Fatal() != nil {
				return j.Fatal()
			}
			continue
		}
		from, d, n := plan.segment(o)
		if plan.pos >= n {
			plan.next()
			continue
		}
		cur := world.BlockPos{X: from.X + d[0]*plan.pos, Y: from.Y, Z: from.Z + d[1]*plan.pos}
		if err := j.standAt(cur); err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			j.Problem("galería %d: no llego a %v (%v): a la siguiente", plan.k, cur, err)
			plan.skipBranch()
			if fails++; fails >= 4 {
				if plan.turns >= 3 {
					return errors.New("no hay forma de hacer galerías aquí (lava, agua o cuevas por todas partes)")
				}
				j.Logf("%s: zona mala, giro las galerías", o.What)
				plan = newStripPlan(j.Feet(), j.Dimension(), plan.turns+1)
				j.r.strips[o.Key] = plan
				fails = 0
			}
			continue
		}
		err := j.stripRun(d[0], d[1], n-plan.pos, ok, skip)
		// lo avanzado: la distancia de los pies al principio del tramo
		g := j.Feet()
		if along := (g.X-from.X)*d[0] + (g.Z-from.Z)*d[1]; g.Y == from.Y && along > plan.pos && along <= n {
			plan.pos = along
		}
		if err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			j.Problem("galería %d: %v", plan.k, err)
			plan.skipBranch()
			fails++
			continue
		}
		fails = 0
	}
}

// errBagFull: el inventario está lleno (de lo bueno): a vaciarlo.
var errBagFull = errors.New("llevo el inventario lleno")

// oreTries: las veces que se intenta llegar a una mena antes de dejarla.
const oreTries = 3

// takeOre va a por el bloque p (y los de alrededor) y recoge lo que caiga.
// Si no llega por el túnel directo, desde cada celda de al lado; una mena se
// deja (skip) tras oreTries intentos, no al primero.
func (j *Job) takeOre(p world.BlockPos, ok func(string) bool, skip map[world.BlockPos]bool, fails map[world.BlockPos]int, have func() int, goal int) {
	if j.liquid(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}) && !j.fluidBlock(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}) {
		skip[p] = true // bajo un bloque inundado: eso no se tapa
		return
	}
	if err := j.reachOre(p); err != nil {
		if j.Fatal() != nil {
			return
		}
		if fails[p]++; fails[p] >= oreTries {
			j.Problem("%s en %v: %v (lo dejo tras %d intentos)", j.Block(p), p, err, fails[p])
			skip[p] = true
		}
		return
	}
	j.digAround(ok, skip, func() bool { return have() >= goal })
	if ok(j.Block(p)) {
		if fails[p]++; fails[p] >= oreTries {
			skip[p] = true
		}
	}
	_ = j.Collect(6)
	_ = j.fetchDrops(ok, 10)
}

// reachOre llega hasta poder picar p: por un túnel directo o, si no, de pie
// en una de las celdas de al lado (a su altura o una más abajo: queda a la
// altura de la cabeza), de la más cercana a la más lejana.
func (j *Job) reachOre(p world.BlockPos) error {
	err := j.TunnelTo(p)
	if err == nil || j.Fatal() != nil {
		return err
	}
	f := j.Feet()
	var spots []world.BlockPos
	for _, dy := range []int{0, -1} {
		for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			spots = append(spots, world.BlockPos{X: p.X + d[0], Y: p.Y + dy, Z: p.Z + d[1]})
		}
	}
	sort.Slice(spots, func(a, b int) bool {
		da := abs(spots[a].X-f.X) + abs(spots[a].Y-f.Y) + abs(spots[a].Z-f.Z)
		db := abs(spots[b].X-f.X) + abs(spots[b].Y-f.Y) + abs(spots[b].Z-f.Z)
		return da < db
	})
	for _, sp := range spots[:min(4, len(spots))] {
		if j.liquid(sp) || j.liquid(world.BlockPos{X: sp.X, Y: sp.Y + 1, Z: sp.Z}) {
			continue
		}
		if e := j.standAt(sp); e != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			continue
		}
		if j.canDig(p) {
			return nil
		}
	}
	return err
}

// sweepOres, al acabar unas galerías: lo que se vea al alcance (y lo que se
// cayó) se saca antes de irse, aunque ya esté el lote.
func (j *Job) sweepOres(ok func(string) bool, skip map[world.BlockPos]bool) {
	j.digAround(ok, skip, func() bool { return false })
	_ = j.Collect(6)
	_ = j.fetchDrops(ok, 10)
}
