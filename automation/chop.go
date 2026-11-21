package automation

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/world"

	"cubit/pathing"
)

// /chop <tipos> <x> <y> <z>: va hasta ese sitio y tala los árboles enteros de
// esos tipos de madera que haya a menos de ChopRadius bloques (en horizontal).
//
// Un árbol entero son todos sus troncos, también los altos: el pathfinding
// pone bloques para subir y al acabar se quitan, y no se deja ningún tronco
// flotando. Lo que cae se recoge. La herramienta, el hacha que lleve (la
// mejor, sin romperla: toolReserve); sin hacha, a mano.
//
// Solo se talan árboles, no construcciones: un grupo de troncos cuenta como
// árbol si tiene hojas naturales (persistent=false; en el Nether, bloques de
// verruga) junto a sus troncos y ningún bloque de jugador pegado (tablones,
// escaleras, puertas, cristal, antorchas, cofres…). Los troncos pelados
// (stripped_*) no se tocan nunca. Un tronco caído suelto en el bosque, sin
// hojas, tampoco.

// chopCollectUp: hasta qué altura sobre el pie del árbol se sube a por la
// madera que quedó sobre las hojas.
const chopCollectUp = 7

// chopTreeBudget: lo máximo que se dedica a rematar los troncos que no alcanza
// de un árbol; pasado eso se deja (y se dice) en vez de quedarse allí.
const chopTreeBudget = 150 * time.Second

// ChopRadius: a cuántos bloques (en horizontal) del punto se talan árboles.
const ChopRadius = 24

// WoodKinds son los tipos de madera que se pueden pedir (el prefijo del tronco).
var WoodKinds = []string{"oak", "birch", "spruce", "jungle", "acacia", "dark_oak", "mangrove", "cherry", "pale_oak", "crimson", "warped"}

// ParseWoods: "oak,birch" → [oak birch] (sin repetir; error si no conoce alguno).
func ParseWoods(s string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, k := range strings.Split(strings.ToLower(strings.TrimSpace(s)), ",") {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		ok := false
		for _, w := range WoodKinds {
			ok = ok || w == k
		}
		if !ok {
			return nil, fmt.Errorf("unknown wood %q (try: %s)", k, strings.Join(WoodKinds, ", "))
		}
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no wood given (try: %s)", strings.Join(WoodKinds, ", "))
	}
	return out, nil
}

// woodSet: los tipos de madera que se talan.
type woodSet map[string]bool

// kindOf: de qué madera es el bloque, si es un tronco (log o stem) sin pelar.
func logKindOf(name string) (string, bool) {
	if strings.HasPrefix(name, "stripped_") {
		return "", false
	}
	for _, suf := range []string{"_log", "_stem"} {
		if strings.HasSuffix(name, suf) {
			return strings.TrimSuffix(name, suf), true
		}
	}
	return "", false
}

// has: el bloque es un tronco de uno de los tipos pedidos.
func (w woodSet) has(name string) bool {
	k, ok := logKindOf(name)
	return ok && w[k]
}

// isLogBlock: cualquier tronco (sin pelar).
func isLogBlock(name string) bool { _, ok := logKindOf(name); return ok }

// playerMade: bloques que no crecen con un árbol (lo que se ve en una
// construcción): un tronco pegado a uno es parte de una casa, un puente…
func playerMade(n string) bool {
	for _, s := range []string{"_planks", "_stairs", "_slab", "_door", "_fence", "_fence_gate", "_trapdoor", "_sign",
		"_bricks", "_wall", "_pane", "_button", "_pressure_plate", "_wool", "_bed", "_banner", "_concrete",
		"_terracotta", "_lantern", "_shelf"} {
		if strings.HasSuffix(n, s) {
			return true
		}
	}
	switch n {
	case "glass", "torch", "wall_torch", "soul_torch", "ladder", "crafting_table", "chest", "trapped_chest", "barrel",
		"furnace", "cobblestone", "stone_bricks", "bookshelf", "lantern", "campfire", "item_frame", "painting",
		"scaffolding", "rail", "hopper", "bell", "composter", "smoker", "blast_furnace", "lectern", "loom",
		"stonecutter", "grindstone", "cartography_table", "smithing_table", "fletching_table", "anvil":
		return true
	}
	return false
}

// chopTree es un árbol por talar: sus troncos (de abajo arriba).
type chopTree struct {
	logs []world.BlockPos
	foot world.BlockPos // el tronco más bajo (el más cercano al centro si hay varios)
	kind string         // la madera del pie
}

type chopper struct {
	j      *Job
	ws     woodSet
	center world.BlockPos
	skip   map[world.BlockPos]bool // troncos de árboles que se dejaron (imposibles o a medias)
	noted  map[world.BlockPos]bool // grupos de troncos ya dicho que no son árbol
	until  time.Time               // hasta cuándo se intenta terminar el árbol de ahora (chopTreeBudget)

	trees     map[string]int // árboles talados, por madera
	logs      map[string]int // troncos cortados, por madera
	left      int            // troncos que se dejaron en pie
	abandoned int            // árboles que no se pudieron talar
}

// Chop es la tarea: talar los árboles de los tipos kinds alrededor de center.
func Chop(kinds []string, center world.BlockPos) func(j *Job) error {
	return func(j *Job) error {
		j.toolFloor = toolReserve // el hacha no se rompe
		j.goLimit = 20 * 10       // 20 s por trayecto: más y se pasa el tiempo yendo y viniendo
		defer func() { j.toolFloor, j.goLimit = 0, 0 }()
		c := &chopper{j: j, ws: woodSet{}, center: center, skip: map[world.BlockPos]bool{},
			trees: map[string]int{}, logs: map[string]int{}}
		for _, k := range kinds {
			c.ws[k] = true
		}
		before := c.carried()
		defer func() { c.summary(before) }() // también al pararla (/stop): lo que se llevaba talado
		// solo se recoge la madera (no las hojas: manzanas, palos, brotes…: ir
		// a por lo que cae sobre las hojas era subir y bajar para nada)
		j.ignore = func(item string) bool { return !isLogBlock(item) }
		defer func() { j.ignore = nil }()
		j.Logf("talar %s alrededor de %v (radio %d)", strings.Join(kinds, ","), center, ChopRadius)
		if axe := j.bestAxe(); axe != "" {
			j.Logf("uso el %s (sin romperlo)", axe)
		} else {
			j.Logf("sin hacha: talo a mano (más lento)")
		}
		if err := c.travel(center); err != nil {
			return err
		}
		for {
			if err := j.Wait(1); err != nil {
				return err
			}
			if j.Fatal() != nil {
				return j.Fatal()
			}
			j.Status("buscando árboles (%d talados)", c.total(c.trees))
			t, ok := c.next()
			if !ok {
				break
			}
			c.process(t)
			if j.Fatal() != nil {
				return j.Fatal()
			}
		}
		if c.total(c.trees) == 0 {
			return fmt.Errorf("no encontré árboles de %s a menos de %d bloques de %v (hojas naturales y chunks cargados)", strings.Join(kinds, ","), ChopRadius, center)
		}
		return nil
	}
}

func (c *chopper) total(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

// carried: los troncos que lleva, por tipo de bloque.
func (c *chopper) carried() map[string]int {
	out := map[string]int{}
	for it, n := range c.j.Items() {
		if isLogBlock(it) {
			out[it] = n
		}
	}
	return out
}

// summary: el resumen final en el log.
func (c *chopper) summary(before map[string]int) {
	j := c.j
	var parts []string
	for _, k := range WoodKinds {
		if c.trees[k] > 0 || c.logs[k] > 0 {
			parts = append(parts, fmt.Sprintf("%s: %d árboles, %d troncos", k, c.trees[k], c.logs[k]))
		}
	}
	var got []string
	now := c.carried()
	names := make([]string, 0, len(now))
	for n := range now {
		names = append(names, n)
	}
	sort.Strings(names)
	gotN := 0
	for _, n := range names {
		if d := now[n] - before[n]; d > 0 {
			got = append(got, fmt.Sprintf("%s %d", n, d))
			gotN += d
		}
	}
	j.Logf("talado: %d árboles y %d troncos (%s); recogidos: %s", c.total(c.trees), c.total(c.logs), strings.Join(parts, "; "), strings.Join(got, ", "))
	if loose := c.total(c.logs) - gotN; loose > 0 {
		j.Logf("%d troncos cortados quedaron sin recoger (sobre las hojas de la copa o sin camino)", loose)
	}
	if n, first := c.remaining(); n > 0 {
		j.Logf("quedan %d troncos de esas maderas a menos de %d bloques que no son de un árbol (construcciones, troncos caídos) o que dejé (el primero en %v)", n, ChopRadius, first)
	}
	if c.left > 0 || c.abandoned > 0 {
		j.Problem("talar: dejé %d árboles con %d troncos en pie (sin camino, zona protegida…)", c.abandoned, c.left)
	}
}

// remaining cuenta los troncos de las maderas pedidas que siguen en pie en el radio.
func (c *chopper) remaining() (n int, first world.BlockPos) {
	j := c.j
	r := ChopRadius
	for y := c.center.Y - 24; y <= c.center.Y+48; y++ {
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				if p := (world.BlockPos{X: c.center.X + dx, Y: y, Z: c.center.Z + dz}); dx*dx+dz*dz <= r*r && c.ws.has(j.Block(p)) {
					if n == 0 {
						first = p
					}
					n++
				}
			}
		}
	}
	return n, first
}

// bestAxe es el hacha que lleva con la que talaría (la mejor): "" si no lleva ninguna.
func (j *Job) bestAxe() string {
	if i := j.toolSlot("axe", j.toolFloor); i >= 0 {
		s := j.r.d.Screens
		s.Lock()
		defer s.Unlock()
		return strings.TrimPrefix(itemName(int32(s.Inventory.Slots[i].Item)), "minecraft:")
	}
	return ""
}

// travel va hasta p por tramos (el bot solo conoce los chunks cargados): cada
// uno a lo sumo 40 bloques más allá, más corto y desviado tras un fallo.
func (c *chopper) travel(p world.BlockPos) error {
	j := c.j
	fails := 0
	for steps := 0; steps < 400; steps++ {
		pos := j.Pos()
		d := math.Hypot(float64(p.X)+0.5-pos.X, float64(p.Z)+0.5-pos.Z)
		if d <= 3 {
			return nil
		}
		j.Status("yendo a %v (a %.0f bloques)", p, d)
		goal, ok := c.segment(pos.X, pos.Y, pos.Z, p, d, fails)
		if !ok {
			if d <= ChopRadius/2 {
				j.Logf("no encuentro dónde pisar en %v (estoy a %.0f): talo desde aquí", p, d)
				return nil
			}
			return fmt.Errorf("no encuentro por dónde seguir hacia %v (a %.0f bloques)", p, d)
		}
		if err := c.goTo(goal); err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			if fails++; fails > 6 {
				if d <= ChopRadius/2 {
					j.Logf("%v; talo desde aquí (a %.0f del punto)", err, d)
					return nil
				}
				return fmt.Errorf("no llego a %v: %w", p, err)
			}
			continue
		}
		fails = 0
	}
	return fmt.Errorf("no llego a %v", p)
}

func (c *chopper) segment(x, y, z float64, p world.BlockPos, d float64, fails int) (world.BlockPos, bool) {
	dx, dz := (float64(p.X)+0.5-x)/d, (float64(p.Z)+0.5-z)/d
	w := c.j.r.d.Walker
	for _, step := range []float64{40, 24, 12, 6, 3} {
		step = math.Min(step, d) / float64(1+fails/3)
		for _, off := range []float64{0, 0.35, -0.35, 0.7, -0.7} {
			if fails == 0 && off != 0 && step == math.Min(40, d) {
				continue
			}
			cs, sn := math.Cos(off), math.Sin(off)
			tx, tz := x+(dx*cs-dz*sn)*step, z+(dx*sn+dz*cs)*step
			near := int(math.Floor(y))
			if step >= d {
				near = p.Y
			}
			if g, ok := w.Ground(int(math.Floor(tx)), int(math.Floor(tz)), near); ok {
				return g, true
			}
		}
	}
	return world.BlockPos{}, false
}

// scan encuentra los árboles de los tipos pedidos cuyo pie está a menos de
// ChopRadius del centro y que son árboles de verdad (no construcciones).
func (c *chopper) scan() []*chopTree {
	j := c.j
	seen := map[world.BlockPos]bool{}
	var out []*chopTree
	r := ChopRadius
	for y := c.center.Y - 24; y <= c.center.Y+48; y++ {
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				if dx*dx+dz*dz > r*r {
					continue
				}
				p := world.BlockPos{X: c.center.X + dx, Y: y, Z: c.center.Z + dz}
				if seen[p] || c.skip[p] || !c.ws.has(j.Block(p)) {
					continue
				}
				logs := c.cluster(p, seen)
				if len(logs) == 0 {
					continue
				}
				for _, t := range c.split(logs) {
					if c.skip[t.foot] || !c.natural(t) {
						continue
					}
					out = append(out, t)
				}
			}
		}
	}
	return out
}

// cluster son los troncos de los tipos pedidos unidos a p (lados, aristas y
// esquinas: las ramas). Vacío si es enorme.
func (c *chopper) cluster(p world.BlockPos, seen map[world.BlockPos]bool) []world.BlockPos {
	j := c.j
	seen[p] = true
	queue := []world.BlockPos{p}
	var out []world.BlockPos
	for len(queue) > 0 {
		q := queue[0]
		queue = queue[1:]
		out = append(out, q)
		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				for dz := -1; dz <= 1; dz++ {
					n := world.BlockPos{X: q.X + dx, Y: q.Y + dy, Z: q.Z + dz}
					if seen[n] || !c.ws.has(j.Block(n)) {
						continue
					}
					seen[n] = true
					queue = append(queue, n)
				}
			}
		}
	}
	if len(out) > 5000 {
		return nil
	}
	return out
}

// treeSpan: a cuántos bloques (en horizontal) del pie llegan los troncos de un
// árbol. En un bosque denso los árboles se tocan y forman un grupo de cientos
// de troncos: se parte en árboles (de abajo arriba: el pie es el tronco más
// bajo que queda) y lo que no cae en uno sale en el siguiente.
const treeSpan = 8

func (c *chopper) split(logs []world.BlockPos) []*chopTree {
	ctr := c.center
	sort.SliceStable(logs, func(a, b int) bool {
		if logs[a].Y != logs[b].Y {
			return logs[a].Y < logs[b].Y
		}
		return colDist2(logs[a], ctr) < colDist2(logs[b], ctr)
	})
	used := make([]bool, len(logs))
	var out []*chopTree
	for i, foot := range logs {
		if used[i] {
			continue
		}
		var part []world.BlockPos
		for k := i; k < len(logs); k++ {
			if !used[k] && abs(logs[k].X-foot.X) <= treeSpan && abs(logs[k].Z-foot.Z) <= treeSpan {
				used[k] = true
				part = append(part, logs[k])
			}
		}
		if t := c.makeTree(part); t != nil {
			out = append(out, t)
		}
	}
	return out
}

func (c *chopper) makeTree(logs []world.BlockPos) *chopTree {
	ctr := c.center
	sort.SliceStable(logs, func(a, b int) bool {
		if logs[a].Y != logs[b].Y {
			return logs[a].Y < logs[b].Y
		}
		return colDist2(logs[a], ctr) < colDist2(logs[b], ctr)
	})
	foot := logs[0]
	if colDist2(foot, ctr) > ChopRadius*ChopRadius { // el pie fuera del radio: no es de aquí
		return nil
	}
	k, _ := logKindOf(c.j.Block(foot))
	return &chopTree{logs: logs, foot: foot, kind: k}
}

func colDist2(a, b world.BlockPos) int {
	return (a.X-b.X)*(a.X-b.X) + (a.Z-b.Z)*(a.Z-b.Z)
}

// natural: el grupo de troncos es un árbol: hojas naturales cerca de alguno
// de sus troncos y ninguna cosa de jugador pegada a ellos.
func (c *chopper) natural(t *chopTree) bool {
	ok, why := c.isNatural(t)
	if !ok && !c.noted[t.foot] { // (una vez por grupo: se vuelve a mirar en cada vuelta)
		if c.noted == nil {
			c.noted = map[world.BlockPos]bool{}
		}
		c.noted[t.foot] = true
		c.j.Logf("no es un árbol (%d troncos en %v): %s", len(t.logs), t.foot, why)
	}
	return ok
}

func (c *chopper) isNatural(t *chopTree) (bool, string) {
	j := c.j
	leaves := false
	for _, l := range t.logs {
		for _, d := range [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
			if n := j.Block(add(l, d)); playerMade(n) {
				return false, fmt.Sprintf("%s pegado al tronco de %v (construcción)", n, l)
			}
		}
		if leaves {
			continue
		}
	scan:
		for dx := -2; dx <= 2; dx++ {
			for dy := -1; dy <= 2; dy++ {
				for dz := -2; dz <= 2; dz++ {
					if j.naturalLeaves(world.BlockPos{X: l.X + dx, Y: l.Y + dy, Z: l.Z + dz}) {
						leaves = true
						break scan
					}
				}
			}
		}
	}
	if !leaves {
		return false, "sin hojas naturales cerca (troncos caídos, un tronco suelto, una construcción)"
	}
	return true, ""
}

// next: el árbol más cercano al bot de los que quedan.
func (c *chopper) next() (*chopTree, bool) {
	f := c.j.Feet()
	var best *chopTree
	bd := math.Inf(1)
	for _, t := range c.scan() {
		d := math.Hypot(float64(t.foot.X-f.X), float64(t.foot.Z-f.Z)) + 1.5*math.Abs(float64(t.foot.Y-f.Y))
		if d < bd {
			best, bd = t, d
		}
	}
	return best, best != nil
}

// cut: rompe el tronco l (si es de los pedidos y lo alcanza) y lo apunta.
func (c *chopper) cut(l world.BlockPos) bool {
	j := c.j
	name := j.Block(l)
	if !c.ws.has(name) || !j.Reaches(l) {
		return false
	}
	if err := j.Dig(l); err != nil || c.ws.has(j.Block(l)) {
		return false
	}
	k, _ := logKindOf(name)
	c.logs[k]++
	return true
}

func (c *chopper) standing(logs []world.BlockPos) []world.BlockPos {
	var out []world.BlockPos
	for _, l := range logs {
		if c.ws.has(c.j.Block(l)) {
			out = append(out, l)
		}
	}
	return out
}

// process tala un árbol entero y recoge lo que soltó.
func (c *chopper) process(t *chopTree) {
	j := c.j
	top := t.logs[len(t.logs)-1].Y
	j.Logf("árbol de %s en %v: %d troncos, de %d de alto", t.kind, t.foot, len(t.logs), top-t.foot.Y+1)
	c.until = time.Now().Add(chopTreeBudget)
	ground := t.foot.Y
	if err := c.fell(t); err != nil {
		if j.Fatal() != nil {
			return
		}
		j.Logf("árbol en %v: %v", t.foot, err)
	}
	if j.Fatal() != nil {
		return
	}
	c.finish(t, ground)
	if j.Fatal() != nil {
		return
	}
	if left := c.standing(t.logs); len(left) > 0 {
		j.Logf("dejo %d troncos del árbol de %v que no alcanzo (%v)", len(left), t.foot, left[0])
		c.left += len(left)
		c.abandoned++
		for _, l := range t.logs {
			c.skip[l] = true
		}
	}
	c.trees[t.kind]++
	j.Status("recogiendo lo que cayó")
	c.removeScaffold(t.logs)
	c.descend(ground)
	if err := c.collect(t); err != nil && j.Fatal() != nil {
		return
	}
}

// fell: desde el suelo los troncos que alcanza; para el resto se mete en el
// hueco del pie y sube con una torre por dentro, talando lo que alcanza en
// cada piso; al final baja deshaciendo la torre.
func (c *chopper) fell(t *chopTree) error {
	j := c.j
	base := t.foot
	top := t.logs[len(t.logs)-1].Y
	if top-base.Y > 3 { // habrá que subir: bloques para la torre
		c.gatherScaffold(top - base.Y - 2)
	}
	digReachable := func() {
		for _, l := range t.logs {
			c.cut(l)
		}
	}
	if err := c.goNear(base); err != nil {
		if j.Fatal() != nil {
			return j.Fatal()
		}
		j.Logf("al árbol: %v; abro paso entre las hojas", err)
		if err := c.clearWayTo(base, func() bool { return j.Reaches(base) }); err != nil {
			return err
		}
	}
	digReachable()
	if len(c.standing(t.logs)) == 0 {
		c.climbDown(base.Y)
		return nil
	}
	if c.ws.has(j.Block(base)) {
		return fmt.Errorf("el pie del tronco sigue ahí")
	}
	if err := c.goTo(base); err != nil {
		if j.Fatal() != nil {
			return j.Fatal()
		}
		j.Logf("al hueco del tronco: %v; abro paso entre las hojas", err)
		if err := c.clearWayTo(base, func() bool { return j.Feet() == base }); err != nil {
			// no se entra en el hueco (las raíces del manglar, un pie en alto): por debajo
			if left := c.standing(t.logs); len(left) > 0 && c.towerTo(t, left[0]) {
				return nil
			}
			return fmt.Errorf("al hueco del tronco: %w", err)
		}
	}
	c.climbCut(t)
	c.climbDown(base.Y)
	return nil
}

// climbCut sube en el sitio (una torre de bloques, rompiendo lo de encima) y
// tala lo que alcanza en cada piso, hasta que no queda nada más arriba.
func (c *chopper) climbCut(t *chopTree) {
	j := c.j
	cutAll := func() {
		for _, l := range t.logs {
			c.cut(l)
		}
	}
	for floors := 0; floors < 48 && len(c.standing(t.logs)) > 0; floors++ {
		cutAll()
		if !c.higherLeft(t.logs) {
			break
		}
		j.Status("subiendo (%d cortados)", c.total(c.logs))
		if err := c.pillarUp(); err != nil {
			j.Logf("subir: %v", err)
			break
		}
	}
	cutAll()
}

// towerTo: a un tronco que no se alcanza andando se llega por debajo: al suelo
// de su columna y arriba con una torre, rompiendo las hojas de encima.
func (c *chopper) towerTo(t *chopTree, l world.BlockPos) bool {
	j := c.j
	g, ok := j.r.d.Walker.Ground(l.X, l.Z, t.foot.Y)
	if !ok || g.Y >= l.Y-1 {
		return false
	}
	c.ensureScaffold(l.Y - g.Y)
	j.Logf("al tronco de %v por debajo: torre desde %v", l, g)
	if err := c.goTo(g); err != nil {
		if j.Fatal() != nil {
			return false
		}
		return false
	}
	c.climbCut(t)
	c.climbDown(g.Y)
	return !c.ws.has(j.Block(l))
}

// higherLeft: quedan troncos por encima de lo que alcanza (hay que subir).
func (c *chopper) higherLeft(logs []world.BlockPos) bool {
	j := c.j
	f := j.Feet()
	for _, l := range logs {
		if c.ws.has(j.Block(l)) && !j.Reaches(l) && l.Y > f.Y {
			return true
		}
	}
	return false
}

// finish termina un árbol: va a por cada tronco que quede (el pathfinding
// llega con torres, puentes y rompiendo hojas). Sigue mientras avance; tres
// vueltas sin cortar nada: no se puede, y se rinde.
func (c *chopper) finish(t *chopTree, ground int) {
	j := c.j
	stuck := 0
	for round := 0; round < 24 && stuck < 3; round++ {
		left := c.standing(t.logs)
		if len(left) == 0 {
			break
		}
		if j.Fatal() != nil || j.protected(left[0]) {
			return
		}
		if time.Now().After(c.until) {
			j.Logf("llevo %v con este árbol: dejo los %d troncos que quedan", chopTreeBudget, len(left))
			return
		}
		j.Status("terminando el árbol (quedan %d troncos)", len(left))
		j.Logf("al árbol le quedan %d troncos: voy a por ellos", len(left))
		c.ensureScaffold(left[len(left)-1].Y - ground)
		before := len(left)
		fails := 0
		for _, l := range left {
			if !c.ws.has(j.Block(l)) {
				continue
			}
			if fails >= 3 || time.Now().After(c.until) { // tres seguidos sin camino: otra vuelta (con más bloques) en vez de 10 s por tronco
				break
			}
			if !j.Reaches(l) {
				if err := c.goNear(l); err != nil {
					if j.Fatal() != nil {
						return
					}
					j.Logf("tronco en %v: %v; abro paso entre las hojas", l, err)
					if err := c.clearWayTo(l, func() bool { return j.Reaches(l) }); err != nil {
						if j.Fatal() != nil {
							return
						}
						if !c.towerTo(t, l) {
							fails++
							continue
						}
					}
				}
			}
			fails = 0
			for _, o := range c.standing(t.logs) { // este y los que alcance desde aquí
				c.cut(o)
			}
		}
		c.climbDown(ground)
		_ = c.collect(t)
		if len(c.standing(t.logs)) < before {
			stuck = 0
			continue
		}
		stuck++
		if b := j.r.d.Builder; b != nil && b.Paused() {
			j.Logf("el servidor no me deja construir aquí (zona protegida): dejo el árbol")
			return
		}
		if left := c.standing(t.logs); len(left) > 0 {
			c.ensureScaffold(left[len(left)-1].Y - ground + 4*stuck)
		}
	}
}

// removeScaffold quita los bloques que puso para llegar al árbol: el árbol
// talado no deja torres.
func (c *chopper) removeScaffold(logs []world.BlockPos) {
	j := c.j
	b := j.r.d.Builder
	if b == nil || len(logs) == 0 {
		return
	}
	cx, cz := logs[0].X, logs[0].Z
	placed := b.Placed()
	// de arriba abajo: si el bot está subido en una torre, la deshace desde la
	// cima (empezando por abajo se quedaba en un trozo flotando)
	sort.Slice(placed, func(a, b int) bool { return placed[a].Y > placed[b].Y })
	for _, p := range placed {
		f := j.Feet()
		near := func(x, z int) bool { return abs(p.X-x) <= 12 && abs(p.Z-z) <= 12 }
		if !near(cx, cz) && !near(f.X, f.Z) || j.Fatal() != nil {
			continue
		}
		if n := j.Block(p); n == "" || n == "air" {
			b.Forget(p)
			continue
		}
		if p.X == f.X && p.Z == f.Z && p.Y < f.Y { // bajo los pies: bajar deshaciendo
			c.climbDown(p.Y)
			continue
		}
		if !j.Reaches(p) {
			if err := j.GoNear(p); err != nil {
				continue
			}
		}
		j.Status("quitando los bloques que puse")
		if err := j.Dig(p); err == nil {
			b.Forget(p)
		}
	}
}

// treeBlock: lo que es del árbol aparte de troncos y hojas y se rompe para
// pasar: las raíces del manglar y, en el Nether, la verruga y las luciérnagas.
func (c *chopper) treeBlock(n string) bool {
	return c.ws["mangrove"] && strings.HasSuffix(n, "mangrove_roots") ||
		(c.ws["crimson"] || c.ws["warped"]) && (strings.HasSuffix(n, "_wart_block") || n == "shroomlight")
}

// goNear y goTo son los del Job, y si no se mueve (de pie en el borde de un
// bloque, con el centro en el aire, el pathfinding no sabe salir) lo centran
// sobre suelo firme y lo reintentan.
func (c *chopper) goNear(p world.BlockPos) error {
	err := c.j.GoNear(p)
	if err != nil && c.j.Fatal() == nil && strings.Contains(err.Error(), "sin moverme") && c.unstick() {
		err = c.j.GoNear(p)
	}
	return err
}

func (c *chopper) goTo(p world.BlockPos) error {
	err := c.j.GoTo(p)
	if err != nil && c.j.Fatal() == nil && strings.Contains(err.Error(), "sin moverme") && c.unstick() {
		err = c.j.GoTo(p)
	}
	return err
}

// unstick lleva al bot al centro del bloque firme más cercano (andando recto,
// sin pathfinding): devuelve si se movió.
func (c *chopper) unstick() bool {
	j := c.j
	pos := j.Pos()
	f := j.Feet()
	var best world.BlockPos
	bd, found := math.Inf(1), false
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			cell := world.BlockPos{X: f.X + dx, Y: f.Y, Z: f.Z + dz}
			if !j.solid(world.BlockPos{X: cell.X, Y: cell.Y - 1, Z: cell.Z}) || j.solid(cell) || j.solid(world.BlockPos{X: cell.X, Y: cell.Y + 1, Z: cell.Z}) {
				continue
			}
			if d := math.Hypot(float64(cell.X)+0.5-pos.X, float64(cell.Z)+0.5-pos.Z); d < bd {
				best, bd, found = cell, d, true
			}
		}
	}
	if !found || bd < 0.15 {
		return false
	}
	j.Logf("de pie en el borde de un bloque (%.1f, %.1f): me centro en %v", pos.X, pos.Z, best)
	j.r.d.Walker.Stop()
	for t := 0; t < 30; t++ {
		p := j.Pos()
		dx, dz := float64(best.X)+0.5-p.X, float64(best.Z)+0.5-p.Z
		if math.Hypot(dx, dz) < 0.12 {
			break
		}
		yaw := float32(math.Atan2(-dx, dz) * 180 / math.Pi)
		j.r.d.Player.UpdatePosition(func(q basic.Position) basic.Position { q.Yaw = yaw; return q })
		j.r.d.Ctl.SetInput(control.Input{Forward: true, Shift: true})
		if err := j.r.d.Ctl.WaitTicks(j.ctx, 1); err != nil {
			break
		}
	}
	j.r.d.Ctl.SetInput(control.Input{})
	return true
}

// descend: si el bot sigue en alto (una copa, una torre que no se deshizo), al suelo.
func (c *chopper) descend(ground int) {
	j := c.j
	c.climbDown(ground)
	f := j.Feet()
	if f.Y <= ground+1 {
		return
	}
	if g, ok := j.r.d.Walker.Ground(f.X, f.Z, ground); ok && g.Y < f.Y-1 {
		j.Logf("sigo a %d de alto: bajo al suelo (%v)", f.Y-g.Y, g)
		if err := j.GoTo(g); err != nil && j.Fatal() == nil {
			j.Logf("bajar: %v", err)
		}
	}
}

// collect recoge lo que soltó el árbol: alrededor de su pie y hasta su copa.
func (c *chopper) collect(t *chopTree) error {
	base, top := t.foot, t.foot.Y
	for _, l := range t.logs {
		top = max(top, l.Y)
	}
	// lo que quedó en la copa (muy arriba, sobre las hojas) no se va a buscar: era
	// subir torres de 20 bloques y caerse de ellas; en el resumen sale como "sueltos"
	c.j.collectOnly = func(p world.BlockPos) bool { return p.Y <= base.Y+chopCollectUp }
	defer func() { c.j.collectOnly = nil }()
	return c.j.CollectAround(float64(base.X)+0.5, float64(base.Y), float64(base.Z)+0.5, float64(max(10, top-base.Y+6)))
}

// ensureScaffold: n bloques para subir (tierra de alrededor).
func (c *chopper) ensureScaffold(n int) {
	b := c.j.r.d.Builder
	if b == nil || n <= 0 || b.Blocks() >= n {
		return
	}
	c.gatherScaffold(n)
	if b.Blocks() < n {
		c.j.Logf("solo tengo %d bloques para subir (hacen falta %d)", b.Blocks(), n)
	}
}

// pillarUp sube un bloque en el sitio: rompe lo blando que tenga encima de la
// cabeza (hojas, troncos), salta y, con los pies por encima, pone un bloque debajo.
func (c *chopper) pillarUp() error {
	j := c.j
	b := j.r.d.Builder
	if b == nil || b.Blocks() == 0 {
		return fmt.Errorf("sin bloques para subir")
	}
	f := j.Feet()
	if j.solid(f) { // sobre barro, arena de almas…: el suelo está en esta celda, los pies en la de encima
		f.Y++
	}
	for _, dy := range []int{2, 3} { // la cabeza al saltar llega a +3
		over := world.BlockPos{X: f.X, Y: f.Y + dy, Z: f.Z}
		name := j.Block(over)
		switch {
		case name == "air" || name == "" || name == "cave_air":
		case pathing.Soft(name) || strings.Contains(name, "vine") || c.ws.has(name) || c.treeBlock(name):
			if err := j.Dig(over); err != nil {
				return fmt.Errorf("lo de encima (%s): %w", name, err)
			}
		case dy == 2:
			return fmt.Errorf("no cabe: %s encima", name)
		}
	}
	feet := f
	below := world.BlockPos{X: f.X, Y: f.Y - 1, Z: f.Z}
	j.r.d.Walker.Stop()
	p := j.Pos() // centrado en la celda, para que el bloque no choque con el cuerpo
	if math.Hypot(p.X-(float64(f.X)+0.5), p.Z-(float64(f.Z)+0.5)) > 0.15 {
		_ = j.GoTo(f)
	}
	// el bloque en la mano antes de saltar (un salto dura pocos ticks)
	for t := 0; t < 40 && !b.Equip(); t++ {
		if err := j.Wait(1); err != nil {
			return err
		}
	}
	// mirando abajo (como el pathfinding en sus torres): girar al poner lleva ticks
	for t := 0; t < 12 && j.Pos().Pitch < 89; t++ {
		j.r.d.Player.UpdatePosition(func(p basic.Position) basic.Position {
			p.Pitch += max(-10, min(10, 90-p.Pitch))
			return p
		})
		if err := j.r.d.Ctl.WaitTicks(j.ctx, 1); err != nil {
			return err
		}
	}
	j.r.d.Ctl.SetInput(control.Input{Jump: true})
	for t := 0; t < 12 && j.Pos().Y < float64(feet.Y)+1.05; t++ {
		if err := j.r.d.Ctl.WaitTicks(j.ctx, 1); err != nil {
			j.r.d.Ctl.SetInput(control.Input{})
			return err
		}
	}
	j.r.d.Ctl.SetInput(control.Input{})
	for t := 0; t < 7 && !b.Place(feet, below); t++ { // (el bloque aún iba a la barra)
		_ = j.r.d.Ctl.WaitTicks(j.ctx, 1)
	}
	for t := 0; t < 40 && b.Busy(); t++ {
		_ = j.r.d.Ctl.WaitTicks(j.ctx, 1)
	}
	for t := 0; t < 20 && !j.r.d.Ctl.State().OnGround; t++ {
		_ = j.r.d.Ctl.WaitTicks(j.ctx, 1)
	}
	if j.Pos().Y < float64(feet.Y)+0.9 {
		return fmt.Errorf("el bloque bajo los pies en %v no quedó (y %.2f, bloque ahí: %s)", feet, j.Pos().Y, j.Block(feet))
	}
	return nil
}

// climbDown baja de la torre que se hizo: rompe los bloques puestos bajo los pies, uno a uno.
func (c *chopper) climbDown(ground int) {
	j := c.j
	for i := 0; i < 48; i++ {
		f := j.Feet()
		under := world.BlockPos{X: f.X, Y: f.Y - 1, Z: f.Z}
		if f.Y <= ground || j.r.d.Builder == nil || !j.r.d.Builder.WasPlaced(under) {
			return
		}
		j.Status("bajando de la torre")
		if err := j.Dig(under); err != nil {
			return
		}
		j.r.d.Builder.Forget(under)
		for t := 0; t < 20; t++ {
			if j.Wait(1) != nil {
				return
			}
			if j.r.d.Ctl.State().OnGround && j.Pos().Y < float64(under.Y)+0.5 {
				break
			}
		}
	}
}

// scaffoldGround: lo que se junta a mano para hacer torres (con pico, también roca).
func scaffoldGround(name string, pick bool) bool {
	switch name {
	case "dirt", "grass_block", "coarse_dirt", "podzol", "rooted_dirt":
		return true
	case "stone", "deepslate", "netherrack", "cobblestone", "andesite", "diorite", "granite", "tuff",
		"blackstone", "crimson_nylium", "warped_nylium":
		return pick
	}
	return false
}

// gatherScaffold junta n bloques de tierra de alrededor para las torres, si
// no lleva bloques de construir: con aire encima (no se cavan hoyos bajo
// nada) y no la columna de los pies.
func (c *chopper) gatherScaffold(n int) {
	j := c.j
	b := j.r.d.Builder
	if b == nil || b.Blocks() >= n {
		return
	}
	// subido en una torre suya (de un árbol de antes): primero al suelo
	if f := j.Feet(); b.WasPlaced(world.BlockPos{X: f.X, Y: f.Y - 1, Z: f.Z}) {
		if g, ok := j.r.d.Walker.Ground(f.X, f.Z, f.Y-1); ok {
			c.climbDown(g.Y)
		}
	}
	pick := j.Count("netherite_pickaxe")+j.Count("diamond_pickaxe")+j.Count("iron_pickaxe")+j.Count("stone_pickaxe")+j.Count("wooden_pickaxe") > 0
	skip := map[world.BlockPos]bool{}
	for tries := 0; b.Blocks() < n && tries < 3*n+12; tries++ {
		if j.Fatal() != nil {
			return
		}
		j.Status("juntando bloques para subir (%d de %d)", b.Blocks(), n)
		f := j.Feet()
		var best world.BlockPos
		bd, found := math.Inf(1), false
		for dy := -2; dy <= 2; dy++ {
			for dx := -8; dx <= 8; dx++ {
				for dz := -8; dz <= 8; dz++ {
					p := world.BlockPos{X: f.X + dx, Y: f.Y + dy, Z: f.Z + dz}
					if skip[p] || (dx == 0 && dz == 0) || !scaffoldGround(j.Block(p), pick) || j.Block(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}) != "air" || !j.safeToOpen(p) {
						continue
					}
					if d := math.Hypot(float64(dx), float64(dz)) + 1.2*math.Abs(float64(dy)); d < bd {
						best, bd, found = p, d, true
					}
				}
			}
		}
		if !found {
			j.Logf("no encuentro tierra para subir")
			return
		}
		skip[best] = true
		if !j.Reaches(best) {
			if err := j.GoNear(best); err != nil {
				continue
			}
		}
		if err := j.Dig(best); err != nil {
			continue
		}
		_ = j.Collect(5)
	}
}

// clearWayTo va hacia target paso a paso en línea, rompiendo las hojas que le
// cierran el paso, hasta que done. Para cuando el pathfinding no encuentra
// camino porque las hojas lo tapan todo (los abetos llegan al suelo).
func (c *chopper) clearWayTo(target world.BlockPos, done func() bool) error {
	j := c.j
	soft := func(n string) bool {
		return pathing.Soft(n) || c.treeBlock(n) || strings.Contains(n, "vine")
	}
	for steps := 0; steps < 32; steps++ {
		if done() {
			return nil
		}
		if err := j.Wait(1); err != nil {
			return err
		}
		f := j.Feet()
		dx, dz := sign(target.X-f.X), sign(target.Z-f.Z)
		dirs := [][2]int{{0, dz}, {dx, 0}}
		if abs(target.X-f.X) >= abs(target.Z-f.Z) {
			dirs = [][2]int{{dx, 0}, {0, dz}}
		}
		moved := false
		for _, d := range dirs {
			if d == [2]int{0, 0} {
				continue
			}
			for _, dy := range []int{0, 1, -1} {
				next := world.BlockPos{X: f.X + d[0], Y: f.Y + dy, Z: f.Z + d[1]}
				if next == target && !done() && j.solid(next) && !soft(j.Block(next)) {
					continue // el tronco mismo
				}
				if !j.solid(world.BlockPos{X: next.X, Y: next.Y - 1, Z: next.Z}) {
					continue // sin suelo
				}
				cells := []world.BlockPos{next, {X: next.X, Y: next.Y + 1, Z: next.Z}}
				if dy == 1 {
					cells = append(cells, world.BlockPos{X: f.X, Y: f.Y + 2, Z: f.Z})
				}
				if dy == -1 {
					cells = append(cells, world.BlockPos{X: next.X, Y: next.Y + 2, Z: next.Z})
				}
				ok := true
				for _, cell := range cells {
					if j.solid(cell) && (!soft(j.Block(cell)) || !j.Reaches(cell)) {
						ok = false
					}
				}
				if !ok {
					continue
				}
				for _, cell := range cells {
					if j.solid(cell) {
						j.Status("rompiendo hojas para pasar")
						if err := j.Dig(cell); err != nil {
							return fmt.Errorf("hojas en %v: %w", cell, err)
						}
					}
				}
				if err := j.GoTo(next); err != nil {
					continue
				}
				moved = true
				break
			}
			if moved {
				break
			}
		}
		if !moved {
			if done() {
				return nil
			}
			return fmt.Errorf("no consigo abrirme paso hacia %v", target)
		}
	}
	if done() {
		return nil
	}
	return fmt.Errorf("no llego a %v ni rompiendo hojas", target)
}
