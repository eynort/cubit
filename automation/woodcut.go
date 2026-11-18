package automation

import (
	"minecraft-bot/pathing"
	"time"

	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/control"

	"github.com/mj41/go-mc26-kit/bot/world"
)

// woodKinds traduce el tipo de madera pedido (en español o inglés) al nombre
// del bloque de tronco del juego.
var woodKinds = map[string]string{
	"oak": "oak", "roble": "oak",
	"spruce": "spruce", "abeto": "spruce", "pino": "spruce",
	"birch": "birch", "abedul": "birch",
	"jungle": "jungle", "jungla": "jungle", "selva": "jungle",
	"acacia":   "acacia",
	"dark_oak": "dark_oak", "roble_oscuro": "dark_oak",
	"mangrove": "mangrove", "mangle": "mangrove", "manglar": "mangrove",
	"cherry": "cherry", "cerezo": "cherry",
	"pale_oak": "pale_oak", "roble_palido": "pale_oak", "roble_pálido": "pale_oak",
	"crimson": "crimson", "carmesi": "crimson", "carmesí": "crimson",
	"warped": "warped", "distorsionado": "warped",
}

// WoodKinds son los tipos de madera que se pueden pedir (para el autocompletado).
func WoodKinds() []string {
	return []string{"cualquiera", "roble", "abeto", "abedul", "jungla", "acacia", "roble_oscuro", "mangle", "cerezo", "roble_palido", "carmesi", "distorsionado"}
}

// ParseWood da el prefijo del tronco ("oak") de lo que se pidió ("" cualquiera).
func ParseWood(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "cualquiera" || s == "any" || s == "todo" || s == "todos" {
		return "", nil
	}
	if k, ok := woodKinds[s]; ok {
		return k, nil
	}
	return "", fmt.Errorf("no conozco la madera %q (prueba: %s)", s, strings.Join(WoodKinds(), ", "))
}

// logOf: el bloque es un tronco natural (de la madera kind, "" cualquiera).
func logOf(name, kind string) bool {
	if !isLog(name) || strings.HasPrefix(name, "stripped_") {
		return false
	}
	return kind == "" || name == kind+"_log" || name == kind+"_stem"
}

// natural: hojas que crecieron solas (las que pone un jugador son "persistent=true").
func (j *Job) naturalLeaves(p world.BlockPos) bool {
	st, ok := j.r.d.World.BlockAt(p)
	if !ok {
		return false
	}
	s := world.StateString(st)
	if strings.Contains(s, "_leaves") {
		return strings.Contains(s, "persistent=false")
	}
	// los árboles del Nether: sus "hojas" son bloques de verruga
	return strings.Contains(s, "wart_block") || strings.Contains(s, "shroomlight")
}

// treeLogs son los troncos del árbol al que pertenece el tronco p: unidos a
// él (lados, aristas, esquinas: las ramas), hacia arriba, a menos de 6 en horizontal.
func (j *Job) treeLogs(p world.BlockPos, kind string) []world.BlockPos {
	seen := map[world.BlockPos]bool{p: true}
	queue := []world.BlockPos{p}
	var out []world.BlockPos
	for len(queue) > 0 && len(out) < 300 {
		q := queue[0]
		queue = queue[1:]
		out = append(out, q)
		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				for dz := -1; dz <= 1; dz++ {
					n := world.BlockPos{X: q.X + dx, Y: q.Y + dy, Z: q.Z + dz}
					if seen[n] || abs(n.X-p.X) > 6 || abs(n.Z-p.Z) > 6 || n.Y < p.Y || n.Y-p.Y > 32 {
						continue
					}
					seen[n] = true
					if logOf(j.Block(n), kind) {
						queue = append(queue, n)
					}
				}
			}
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Y < out[b].Y })
	return out
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// isTree: los troncos forman un árbol natural (hay hojas naturales junto a
// los de arriba): no la pared de una casa.
func (j *Job) isTree(logs []world.BlockPos) bool {
	if len(logs) == 0 {
		return false
	}
	top := logs[len(logs)-1].Y
	for _, l := range logs {
		if l.Y < top-3 {
			continue
		}
		for dx := -2; dx <= 2; dx++ {
			for dy := -1; dy <= 2; dy++ {
				for dz := -2; dz <= 2; dz++ {
					if j.naturalLeaves(world.BlockPos{X: l.X + dx, Y: l.Y + dy, Z: l.Z + dz}) {
						return true
					}
				}
			}
		}
	}
	return false
}

// findTree busca el pie del árbol más cercano de la madera kind (a menos de r
// bloques), saltándose los de skip.
func (j *Job) findTree(kind string, r int, skip map[world.BlockPos]bool) (world.BlockPos, bool) {
	f := j.Feet()
	type cand struct {
		p world.BlockPos
		d float64
	}
	var cs []cand
	for dy := -12; dy <= 16; dy++ {
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				p := world.BlockPos{X: f.X + dx, Y: f.Y + dy, Z: f.Z + dz}
				if skip[p] || !logOf(j.Block(p), kind) || logOf(j.Block(world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z}), "") || j.r.Store.IsProtected(p) {
					continue // solo el pie del tronco: debajo no hay otro tronco
				}
				cs = append(cs, cand{p, math.Sqrt(float64(dx*dx+dz*dz)) + 1.5*math.Abs(float64(dy))})
			}
		}
	}
	sort.Slice(cs, func(a, b int) bool { return cs[a].d < cs[b].d })
	for _, c := range cs {
		if logs := j.treeLogs(c.p, kind); j.isTree(logs) {
			return c.p, true
		}
		skip[c.p] = true // una pared de troncos, no un árbol
	}
	return world.BlockPos{}, false
}

// axeTier es lo buena que es el hacha que lleva (0: ninguna).
func (j *Job) axeTier() int {
	items := j.Items()
	for i, t := range tiers {
		if items[t+"_axe"] > 0 {
			return len(tiers) - i
		}
	}
	return 0
}

// possibleAxeTier es la mejor hacha que permiten los materiales que lleva.
func (j *Job) possibleAxeTier() int {
	switch {
	case j.Count("iron_ingot")+j.Count("raw_iron") >= 3:
		return len(tiers) - 2 // hierro
	case j.Count(j.most(stoneMaterial)) >= 3 && j.most(stoneMaterial) != "":
		return len(tiers) - 3 // piedra
	case j.CountWhere(isLog)*4+j.CountWhere(isPlanks) >= j.woodAxeCost():
		return 1 // madera
	}
	return 0
}

// EnsureStoneAxe: si no lleva ningún hacha, hace una de piedra (con lo que
// haga falta: madera, pico de madera, 3 adoquines). Si lleva una, sea cual
// sea, nada (al talar se usa la mejor que lleve).
func (j *Job) EnsureStoneAxe(kind string) error {
	if j.axeTier() > 0 {
		return nil
	}
	j.Logf("objetivo: hacha de piedra")
	if err := j.ensureStonePickaxe(kind, 3+2); err != nil { // 3 del hacha y 2 de la espada
		return err
	}
	mat := j.most(stoneMaterial)
	if mat == "" || j.Count(mat) < 3 {
		return fmt.Errorf("faltan adoquines (3)")
	}
	t, err := j.tablePos()
	if err != nil {
		return err
	}
	if err := j.EnsureSticks(2); err != nil {
		return err
	}
	if err := j.craftOnTable(t, []string{mat, mat, mat, "stick", "", "stick"}, 2, 1); err != nil {
		return fmt.Errorf("hacha de piedra: %w", err)
	}
	if j.Count("stone_axe") == 0 {
		return fmt.Errorf("hacha de piedra: la mesa no la dio")
	}
	j.Logf("hice un stone_axe")
	return nil
}

// Woodcut es la tarea de talar: árboles de la madera kind ("" cualquiera),
// hasta juntar target troncos (0: sin parar), guardándolos en el almacén.
func Woodcut(kind string, target int) func(j *Job) error {
	return func(j *Job) error {
		cut, explored := 0, 0
		skip := map[world.BlockPos]bool{}
		start := j.Feet()
		if j.r.Store.Base == nil { // la base: donde empieza (antes de irse a minar)
			j.r.Store.SetBase(start)
			j.Logf("almacén: base aquí, en %v", start)
		}
		// 0. bajo tierra (una mina de antes, una home en una cueva): arriba primero
		if j.Underground() {
			if err := j.ToSky(); err != nil {
				if j.Fatal() != nil {
					return j.Fatal()
				}
				j.Logf("%v", err)
			}
		}
		// 1. un hacha: si ya lleva una (la que sea, se usa la mejor), esa; si
		// no, una de piedra (madera, pico de madera, 3 adoquines)
		if err := j.EnsureStoneAxe(kind); err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			j.Logf("hacha de piedra: %v (talo con lo que tenga)", err)
		}
		// 2. la base: mesa de trabajo y cofre doble. Lejos y sin cofres (no
		// guarda nada: una base que no se llegó a montar) se pone aquí; lejos y
		// con cofres no se va ahora: se tala aquí y la madera se lleva al llenarse
		if b := j.r.Store.Base; b != nil && j.baseDist() > baseNear && j.r.Store.chestCount() == 0 && !j.r.Store.IsProtected(j.Feet()) {
			j.Logf("la base de %v está a %.0f bloques y no tiene cofres: la pongo aquí", *b, j.baseDist())
			j.r.Store.SetBase(j.Feet())
		}
		if !j.r.Chests() {
			j.storingOff() // (lo dice por el chat una vez)
		} else if c, ok := j.nearMineChest(j.Dimension()); ok {
			j.Logf("guardo la madera en el cofre marcado de %v (/chest)", c)
		} else if b := j.r.Store.Base; b != nil && j.baseDist() > baseNear {
			j.Logf("la base está lejos (%v, a %.0f bloques, con cofres): talo aquí y llevo la madera al llenarme; /base here la pone donde estoy", *b, j.baseDist())
		} else if err := j.SetupBase(); err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			j.Logf("base: %v", err)
			j.noStoreNotice("no pude montar la base con su cofre: " + err.Error())
		}
		// 3. ahora sí, a talar
		j.Logf("a talar (%s)", j.best("axe"))
		start = j.Feet()
		stoneFailed := false     // ya se intentó y no salió: no repetir el viaje
		depositNext := depositAt // con la base cerca, se va a guardar con estos troncos
		// (con condición de parada: se mira entre árbol y árbol, así acaba el
		// que tiene empezado y guarda la madera)
		for (target == 0 || cut < target) && !j.StopReached() {
			if err := j.Wait(1); err != nil {
				return err
			}
			// se rompió el hacha: otra de piedra
			if j.axeTier() == 0 && !stoneFailed {
				j.Logf("me quedé sin hacha: hago otra de piedra")
				if err := j.EnsureStoneAxe(kind); err != nil {
					if j.Fatal() != nil {
						return j.Fatal()
					}
					j.Logf("hacha de piedra: %v", err)
					stoneFailed = true
				}
			}
			j.makeRoom() // la tierra de los andamios: se queda con un montón, la demás fuera
			// a guardar: con el inventario casi lleno; con la base cerca, en
			// cuanto lleva unos montones (el viaje es corto)
			if j.r.Chests() && (j.FreeSlots() <= 3 || (j.baseDist() <= baseNear && j.CountWhere(isLog) >= depositNext)) {
				if err := j.depositWood(); err != nil {
					j.Logf("%v", err)
					j.noStoreNotice("no pude guardar la madera: " + err.Error())
					depositNext = j.CountWhere(isLog) + depositAt // no salió: otra vez con más, no en cada vuelta
				} else {
					depositNext = depositAt
				}
			}
			if j.Underground() { // los árboles están arriba
				if err := j.ToSky(); err != nil {
					if j.Fatal() != nil {
						return j.Fatal()
					}
					j.Logf("%v", err)
				}
			}
			j.Status("buscando árboles (%d troncos)", cut)
			base, ok := j.findTree(kind, 40, skip)
			if !ok {
				if explored >= 6 {
					j.Logf("no encuentro más árboles cerca")
					break
				}
				explored++
				if err := j.explore(start, explored); err != nil {
					j.Logf("explorar: %v", err)
				}
				continue
			}
			explored = 0
			// el árbol entero, identificado antes de empezar: no se pasa al
			// siguiente mientras le quede madera (y se pueda llegar a ella)
			logs := j.treeLogs(base, kind)
			n, err := j.fell(base, kind)
			cut += n
			j.Tally("logs", n)
			if err != nil {
				j.Logf("árbol en %v: %v", base, err)
			}
			if j.Fatal() != nil {
				return j.Fatal()
			}
			n = j.finishTree(logs, kind, base.Y)
			cut += n
			j.Tally("logs", n)
			j.Tally("trees", 1)
			if left := j.leftOf(logs, kind); len(left) > 0 {
				j.Logf("dejo el árbol de %v: %d troncos que no alcanzo (%v)", base, len(left), left[0])
				for _, l := range logs { // ni el resto flotando cuenta como árbol nuevo
					skip[l] = true
				}
			}
			j.Status("recogiendo (%d troncos)", cut)
			if err := j.collectTree(logs); err != nil {
				return err
			}
		}
		if !j.r.Chests() {
			return nil
		}
		if err := j.depositWood(); err != nil {
			j.noStoreNotice("no pude guardar la madera: " + err.Error())
		}
		return nil
	}
}

// baseNear: la base está cerca (para ir a guardar sin esperar a llenarse, y
// para montarla al empezar); depositAt: los troncos con los que se va a guardar
// si está cerca.
const (
	baseNear  = 96
	depositAt = 128
)

// baseDist es la distancia en horizontal a la base (infinita sin base).
func (j *Job) baseDist() float64 {
	b := j.r.Store.Base
	if b == nil {
		return math.Inf(1)
	}
	f := j.Feet()
	return math.Hypot(float64(b.X-f.X), float64(b.Z-f.Z))
}

// depositWood guarda en el almacén los troncos que lleva (cada madera en su
// cofre), quedándose con unos pocos para hacer herramientas y cofres.
func (j *Job) depositWood() error {
	j.Status("guardando la madera")
	// antes de guardar: tablones y palos de reserva (cofres, hachas)
	if j.CountWhere(isPlanks) < 16 && j.CountWhere(isLog) > 4 {
		_ = j.EnsurePlanks(16)
	}
	for it := range j.Items() {
		if isLog(it) {
			if err := j.Deposit(it); err != nil {
				return err
			}
		}
	}
	return nil
}

// explore camina unos 32 bloques en una dirección nueva (alrededor de donde empezó) buscando árboles.
func (j *Job) explore(from world.BlockPos, try int) error {
	a := float64(try) * math.Pi * 0.6
	x := from.X + int(math.Round(math.Cos(a)*32))
	z := from.Z + int(math.Round(math.Sin(a)*32))
	g, ok := j.r.d.Walker.Ground(x, z, from.Y)
	if !ok {
		return fmt.Errorf("no veo suelo hacia %d %d", x, z)
	}
	j.Status("explorando hacia %d %d", x, z)
	return j.GoTo(g)
}

// fell tala el árbol cuyo pie es base, como un jugador: desde el suelo los
// troncos que alcanza; para el resto se mete en el hueco del tronco y sube
// con una torre por dentro, rompiendo las hojas que tiene encima y talando
// lo que alcanza en cada piso; al final baja deshaciendo la torre. Devuelve
// cuántos troncos cortó.
func (j *Job) fell(base world.BlockPos, kind string) (int, error) {
	logs := j.treeLogs(base, kind)
	j.Status("talando un árbol de %d troncos en %v", len(logs), base)
	top := logs[len(logs)-1].Y
	if top-base.Y > 3 { // habrá que subir: bloques para la torre (tierra de alrededor si no lleva)
		j.gatherScaffold(top - base.Y - 2)
	}
	cut := 0
	digReachable := func() {
		for _, l := range logs {
			if logOf(j.Block(l), kind) && j.Reaches(l) {
				if err := j.Dig(l); err == nil {
					cut++
				}
			}
		}
	}
	// desde el suelo: hasta el pie (el pathfinding lo lleva; si las hojas
	// tapan el paso, rompiéndolas, como un jugador) y lo que alcance
	if err := j.GoNear(base); err != nil {
		j.Logf("al árbol: %v; abro paso entre las hojas", err)
		if err := j.clearWayTo(base, func() bool { return j.Reaches(base) }); err != nil {
			return 0, err
		}
	}
	digReachable()
	if !j.anyLeft(logs, kind) {
		j.climbDown(base.Y)
		return cut, nil
	}
	// al hueco del tronco (el pie ya talado) y arriba por dentro
	if logOf(j.Block(base), kind) {
		return cut, fmt.Errorf("el pie del tronco sigue ahí")
	}
	if err := j.GoTo(base); err != nil {
		j.Logf("al hueco del tronco: %v; abro paso entre las hojas", err)
		if err := j.clearWayTo(base, func() bool { return j.Feet() == base }); err != nil {
			return cut, fmt.Errorf("al hueco del tronco: %w", err)
		}
	}
	for floors := 0; floors < 32 && j.anyLeft(logs, kind); floors++ {
		digReachable()
		if !j.anyLeft(logs, kind) || !j.higherLeft(logs, kind) {
			break
		}
		j.Status("subiendo por el tronco (%d troncos)", cut)
		if err := j.pillarUp(); err != nil {
			j.Logf("subir: %v", err)
			break
		}
	}
	digReachable()
	j.climbDown(base.Y)
	return cut, nil
}

// leftOf son los troncos del árbol que siguen en pie (los de abajo primero).
func (j *Job) leftOf(logs []world.BlockPos, kind string) []world.BlockPos {
	var out []world.BlockPos
	for _, l := range logs {
		if logOf(j.Block(l), kind) {
			out = append(out, l)
		}
	}
	return out
}

// finishTree termina un árbol: va a por cada tronco que quede (el
// pathfinding llega con torres, puentes y rompiendo hojas), con bloques para
// subir de sobra (tierra de alrededor o, si no hay, tablones de su madera).
// Sigue mientras avance; tres vueltas sin cortar nada: no se puede (una zona
// protegida, un tronco imposible), y se rinde. Devuelve los que cortó.
func (j *Job) finishTree(logs []world.BlockPos, kind string, ground int) int {
	cut, stuck := 0, 0
	for round := 0; round < 20 && stuck < 3; round++ {
		left := j.leftOf(logs, kind)
		if len(left) == 0 {
			break
		}
		if j.Fatal() != nil || j.r.Store.IsProtected(left[0]) {
			return cut
		}
		j.Status("terminando el árbol (quedan %d troncos)", len(left))
		j.Logf("al árbol le quedan %d troncos: voy a por ellos", len(left))
		j.ensureScaffold(left[len(left)-1].Y - ground)
		before := len(left)
		for _, l := range left {
			if !logOf(j.Block(l), kind) {
				continue
			}
			if !j.Reaches(l) {
				if err := j.GoNear(l); err != nil {
					if j.Fatal() != nil {
						return cut
					}
					j.Logf("tronco en %v: %v", l, err)
					continue
				}
			}
			for _, o := range j.leftOf(logs, kind) { // este y los que alcance desde aquí
				if j.Reaches(o) && j.Dig(o) == nil {
					cut++
				}
			}
		}
		j.climbDown(ground)
		_ = j.collectTree(logs)
		if len(j.leftOf(logs, kind)) < before {
			stuck = 0
		} else {
			stuck++
			if b := j.r.d.Builder; b != nil && b.Paused() {
				j.Logf("el servidor no me deja construir aquí (zona protegida): dejo el árbol")
				break
			}
			// sin avanzar: bloques de sobra (cavando si hace falta) y otra vuelta
			if left := j.leftOf(logs, kind); len(left) > 0 {
				j.ensureScaffold(left[len(left)-1].Y - ground + 4*stuck)
			}
		}
	}
	j.removePlacedNear(logs)
	return cut
}

// removePlacedNear quita los bloques que puso para llegar al árbol (torres,
// puentes): el árbol talado no deja andamios.
func (j *Job) removePlacedNear(logs []world.BlockPos) {
	b := j.r.d.Builder
	if b == nil || len(logs) == 0 {
		return
	}
	c := logs[0]
	for _, p := range b.Placed() {
		if abs(p.X-c.X) > 10 || abs(p.Z-c.Z) > 10 || j.Fatal() != nil {
			continue
		}
		if j.Block(p) == "" || j.Block(p) == "air" {
			b.Forget(p)
			continue
		}
		f := j.Feet()
		if p.X == f.X && p.Z == f.Z && p.Y < f.Y { // bajo los pies: bajar deshaciendo
			j.climbDown(p.Y)
			continue
		}
		if !j.Reaches(p) {
			if err := j.GoNear(p); err != nil {
				continue
			}
		}
		j.Status("quitando los andamios")
		if err := j.Dig(p); err == nil {
			b.Forget(p)
		}
	}
	_ = j.collectTree(logs)
}

// collectTree recoge lo que soltó el árbol: alrededor de su pie y hasta su
// copa (no alrededor de donde acabó el bot, que puede estar a un lado).
func (j *Job) collectTree(logs []world.BlockPos) error {
	if len(logs) == 0 {
		return j.Collect(10)
	}
	base, top := logs[0], logs[0].Y
	for _, l := range logs {
		top = max(top, l.Y)
	}
	return j.CollectAround(float64(base.X)+0.5, float64(base.Y), float64(base.Z)+0.5, float64(max(10, top-base.Y+6)))
}

// ensureScaffold: n bloques para subir (tierra de alrededor; si no hay,
// tablones de los troncos que lleve: 4 por tronco).
func (j *Job) ensureScaffold(n int) {
	b := j.r.d.Builder
	if b == nil || n <= 0 || b.Blocks() >= n {
		return
	}
	j.gatherScaffold(n)
	if missing := n - b.Blocks(); missing > 0 {
		for l, c := range j.Items() {
			if !isLog(l) {
				continue
			}
			times := min(c, (missing+3)/4)
			j.CloseScreen()
			if err := j.craftHere([]string{l}, 1, times); err != nil {
				j.Logf("tablones para subir: %v", err)
			}
			break
		}
	}
	if b.Blocks() < n {
		j.Logf("solo tengo %d bloques para subir (hacen falta %d)", b.Blocks(), n)
	}
}

// anyLeft: quedan troncos del árbol en pie.
func (j *Job) anyLeft(logs []world.BlockPos, kind string) bool {
	for _, l := range logs {
		if logOf(j.Block(l), kind) {
			return true
		}
	}
	return false
}

// higherLeft: quedan troncos más arriba de lo que alcanza (hay que subir).
func (j *Job) higherLeft(logs []world.BlockPos, kind string) bool {
	f := j.Feet()
	for _, l := range logs {
		if logOf(j.Block(l), kind) && !j.Reaches(l) && l.Y > f.Y {
			return true
		}
	}
	return false
}

// pillarUp sube un bloque en el sitio: rompe lo blando que tenga encima de la
// cabeza (hojas), salta y, con los pies ya por encima, pone un bloque debajo.
func (j *Job) pillarUp() error {
	b := j.r.d.Builder
	if b == nil || b.Blocks() == 0 {
		return fmt.Errorf("sin bloques para subir")
	}
	f := j.Feet()
	for _, dy := range []int{2, 3} { // la cabeza al saltar llega a +3
		over := world.BlockPos{X: f.X, Y: f.Y + dy, Z: f.Z}
		name := j.Block(over)
		switch {
		case name == "air" || name == "":
		case strings.HasSuffix(name, "_leaves") || strings.Contains(name, "vine") || isLog(name):
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
	// centrado en la celda, para que el bloque no choque con el cuerpo
	p := j.Pos()
	if math.Hypot(p.X-(float64(f.X)+0.5), p.Z-(float64(f.Z)+0.5)) > 0.15 {
		_ = j.GoTo(f)
	}
	j.r.d.Ctl.SetInput(control.Input{Jump: true})
	for t := 0; t < 12 && j.Pos().Y < float64(feet.Y)+1.05; t++ {
		if err := j.r.d.Ctl.WaitTicks(j.ctx, 1); err != nil {
			j.r.d.Ctl.SetInput(control.Input{})
			return err
		}
	}
	j.r.d.Ctl.SetInput(control.Input{})
	if !b.Place(feet, below) {
		for t := 0; t < 6 && !b.Place(feet, below); t++ { // (el bloque aún iba a la barra)
			_ = j.r.d.Ctl.WaitTicks(j.ctx, 1)
		}
	}
	for t := 0; t < 40 && b.Busy(); t++ {
		_ = j.r.d.Ctl.WaitTicks(j.ctx, 1)
	}
	for t := 0; t < 20 && !j.r.d.Ctl.State().OnGround; t++ {
		_ = j.r.d.Ctl.WaitTicks(j.ctx, 1)
	}
	if j.Pos().Y < float64(feet.Y)+0.9 {
		return fmt.Errorf("el bloque bajo los pies en %v no quedó", feet)
	}
	return nil
}

// climbDown baja de la torre que hizo el pathfinding para llegar arriba:
// rompe los bloques que puso bajo sus pies, uno a uno.
func (j *Job) climbDown(ground int) {
	for i := 0; i < 32; i++ {
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

// gatherScaffold junta n bloques de tierra de alrededor para hacer torres, si
// no lleva bloques de construir: tierra con aire encima (no se cavan hoyos
// bajo nada) y no la de debajo de sus pies.
func (j *Job) gatherScaffold(n int) {
	b := j.r.d.Builder
	if b == nil || b.Blocks() >= n {
		return
	}
	j.Status("juntando bloques para subir (%d de %d)", b.Blocks(), n)
	// subido en una torre suya (de un árbol de antes): primero al suelo
	if f := j.Feet(); b.WasPlaced(world.BlockPos{X: f.X, Y: f.Y - 1, Z: f.Z}) {
		if g, ok := j.r.d.Walker.Ground(f.X, f.Z, f.Y-1); ok {
			j.climbDown(g.Y)
		}
	}
	start := time.Now()
	skip := map[world.BlockPos]bool{}
	// primero la tierra de la superficie (con aire encima: no se cavan hoyos
	// bajo nada); si no hay, lo que haya debajo: tierra o piedra con alguna
	// cara al aire, aunque sea cavando hacia abajo (nunca la columna de los pies)
	surface := func(name string) bool {
		return name == "dirt" || name == "grass_block" || name == "coarse_dirt" || name == "rooted_dirt"
	}
	ground := func(name string) bool {
		return surface(name) || name == "stone" || name == "deepslate" || name == "andesite" || name == "diorite" ||
			name == "granite" || name == "tuff" || name == "netherrack" || name == "podzol" || name == "mud" || name == "clay"
	}
	if j.pickTier() == 0 { // la piedra a mano no suelta nada: solo tierra
		ground = surface
	}
	for tries := 0; b.Blocks() < n && tries < 3*n+12; tries++ {
		if j.Fatal() != nil {
			return
		}
		if time.Since(start) > 2*time.Minute {
			j.Logf("juntar bloques: llevo 2 minutos y tengo %d de %d: sigo con lo que hay", b.Blocks(), n)
			return
		}
		j.Status("juntando bloques para subir (%d de %d)", b.Blocks(), n)
		f := j.Feet()
		p, ok := j.nearWhere(8, surface, func(p world.BlockPos) bool {
			return !skip[p] && !(p.X == f.X && p.Z == f.Z) && j.Block(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}) == "air"
		})
		if !ok {
			p, ok = j.nearWhere(6, ground, func(p world.BlockPos) bool {
				return !skip[p] && !(p.X == f.X && p.Z == f.Z) && p.Y <= f.Y && j.exposed(p) && j.safeToOpen(p)
			})
		}
		if !ok {
			j.Logf("no encuentro tierra ni piedra para subir")
			return
		}
		skip[p] = true
		if !j.Reaches(p) {
			if err := j.GoNear(p); err != nil {
				continue
			}
		}
		if err := j.Dig(p); err != nil {
			continue
		}
		_ = j.Collect(5)
	}
}

// exposed: el bloque p tiene alguna cara al aire (se puede picar sin atravesar nada).
func (j *Job) exposed(p world.BlockPos) bool {
	for _, d := range [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
		if j.Block(world.BlockPos{X: p.X + d[0], Y: p.Y + d[1], Z: p.Z + d[2]}) == "air" {
			return true
		}
	}
	return false
}

// woodAxeCost son los tablones que cuesta un hacha de madera: 3 del hacha, 2
// de los palos y 4 de la mesa si no hay ninguna cerca.
func (j *Job) woodAxeCost() int {
	if _, ok := j.near(24, func(n string) bool { return n == "crafting_table" }); ok || j.Count("crafting_table") > 0 {
		return 5
	}
	return 9
}

// soft: lo que un jugador rompe sin pensarlo para pasar (hojas, enredaderas, telarañas).
func soft(n string) bool { return pathing.Soft(n) }

// clearWayTo va hacia target paso a paso en línea, rompiendo las hojas que le
// cierran el paso (a la altura de los pies y de la cabeza; subiendo un
// escalón, también encima), hasta que done. Para cuando el pathfinding no
// encuentra camino porque las hojas lo tapan todo (los abetos llegan al suelo).
func (j *Job) clearWayTo(target world.BlockPos, done func() bool) error {
	for steps := 0; steps < 32; steps++ {
		if done() {
			return nil
		}
		if err := j.Wait(1); err != nil {
			return err
		}
		f := j.Feet()
		dx, dz := sign(target.X-f.X), sign(target.Z-f.Z)
		var dirs [][2]int // primero el eje más lejano
		if abs(target.X-f.X) >= abs(target.Z-f.Z) {
			dirs = [][2]int{{dx, 0}, {0, dz}}
		} else {
			dirs = [][2]int{{0, dz}, {dx, 0}}
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
				for _, c := range cells {
					if j.solid(c) && (!soft(j.Block(c)) || !j.Reaches(c)) {
						ok = false
					}
				}
				if !ok {
					continue
				}
				for _, c := range cells {
					if j.solid(c) {
						j.Status("rompiendo hojas para pasar")
						if err := j.Dig(c); err != nil {
							return fmt.Errorf("hojas en %v: %w", c, err)
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
