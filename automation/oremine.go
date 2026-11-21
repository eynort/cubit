package automation

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/protocol/types"
)

// /mine <mineral> <x> <y> <z> [every N] [total M]: minar minerales como un
// jugador de verdad (strip mining), sin xray.
//
//   - Baja por una escalera (nunca cavando en vertical bajo los pies) hasta la
//     capa del mineral (OreSpecs, ChooseLayer).
//   - En la capa, un túnel principal de 1×2 y, cada 3 bloques, una rama a cada
//     lado (con 3 de paso, entre dos ramas quedan 2 bloques, que se ven desde
//     las dos).
//   - Solo pica lo que ve (oremine_sight.go): un mineral que toca el aire del
//     túnel, o que se ve desde él, y la veta que aparece al picarlo. Nunca va a
//     por uno escondido en la roca, aunque el mundo cargado lo tenga.
//   - Sin lava ni agua (tapa antes de abrir), sin cavar donde no hay suelo, con
//     antorchas si lleva.
//   - Los picos: los del cofre, sin romper ninguno. El cofre (x y z, o la pared
//     de cofres que lo toca) es donde coge picos y deja lo minado cada N
//     (siempre lo valioso: nunca herramientas ni comida).

// MineConfig son los datos de /mine.
type MineConfig struct {
	Ores  []OreSpec
	Chest world.BlockPos // el cofre (o uno de la pared de cofres)
	Every int            // deja lo minado cada tantos objetos (0: 8)
	Total int            // acaba al juntar tantos (0: hasta /stop)
	// Layer: otra capa (los pies) en vez de la que toca; MainLen y BranchLen:
	// otros largos (las pruebas; 0: los de siempre)
	Layer     *int
	MainLen   int
	BranchLen int
}

const (
	defaultEvery     = 8
	defaultMainLen   = 96 // el túnel principal (32 ramas por lado)
	defaultBranchLen = 16 // cada rama
	branchGap        = 3  // de una rama a la siguiente
	wantPicks        = 2  // los picos que coge del cofre
	torchEvery       = 8  // una antorcha cada tantas celdas del túnel principal
)

type oreMine struct {
	j       *Job
	cfg     MineConfig
	oreOK   func(string) bool // el bloque es uno de los minerales pedidos
	itemOK  func(string) bool // el objeto es lo que sueltan
	need    int               // el nivel del pico
	layer   int
	trail   []world.BlockPos // las celdas por las que se baja y se avanza (desde el origen)
	cur     world.BlockPos   // la celda del túnel donde sigue minando
	dir     [2]int           // hacia dónde va el túnel principal
	fails   map[world.BlockPos]int
	base    map[string]int // lo que ya llevaba al empezar
	dep     map[string]int // lo dejado en el cofre
	trips   int
	relocs  int  // las veces que cambió de sitio la escalera (una cueva la cortó)
	asked   bool // ya pidió un pico por el chat
	torched bool
	dug     int // minerales picados
	started time.Time
}

// MineOres es la tarea.
func MineOres(cfg MineConfig) func(j *Job) error {
	return func(j *Job) error {
		m := &oreMine{j: j, cfg: cfg, fails: map[world.BlockPos]int{}, dep: map[string]int{}, base: map[string]int{}, started: time.Now()}
		if m.cfg.Every <= 0 {
			m.cfg.Every = defaultEvery
		}
		if m.cfg.MainLen <= 0 {
			m.cfg.MainLen = defaultMainLen
		}
		if m.cfg.BranchLen <= 0 {
			m.cfg.BranchLen = defaultBranchLen
		}
		blocks, items := map[string]bool{}, map[string]bool{}
		var names []string
		for _, o := range cfg.Ores {
			if o.Dim != j.Dimension() {
				return fmt.Errorf("%s se mina en %s y estoy en %s: ve allí y repite la orden", o.Key, dimName(o.Dim), dimName(j.Dimension()))
			}
			m.need = max(m.need, o.Tier)
			for _, b := range o.Blocks {
				blocks[b] = true
			}
			for _, it := range o.Items {
				items[it] = true
			}
			names = append(names, o.Key)
		}
		m.oreOK = func(n string) bool { return blocks[n] }
		m.itemOK = func(n string) bool { return items[n] }
		origin := j.Feet()
		layer, why, err := ChooseLayer(cfg.Ores, origin.Y)
		if err != nil {
			return err
		}
		if cfg.Layer != nil {
			layer, why = *cfg.Layer, fmt.Sprintf("capa y=%d dada", *cfg.Layer)
		}
		m.layer = layer
		m.cur = origin
		m.trail = []world.BlockPos{origin}
		for k, v := range j.Items() {
			if m.itemOK(k) {
				m.base[k] = v
			}
		}
		j.pickOK = func(n string) bool { return pickTierOf(n) >= m.need }
		j.pickFloor = pickReserve // (ningún pico se rompe)
		defer func() { j.pickOK, j.pickFloor = nil, 0 }()
		defer m.summary()
		j.Logf("minar %s: %s; cofre en %v, lo dejo cada %d; túnel de 1×2 con ramas cada %d", strings.Join(names, ", "), why, cfg.Chest, m.cfg.Every, branchGap)

		// sin pico: primero al cofre
		if !m.hasPick() {
			if err := m.trip("sin pico", false); err != nil {
				return err
			}
		}
		err = m.goToLayer()
		if err == nil {
			err = m.run()
		}
		if errors.Is(err, errMineDone) {
			err = nil
		}
		// pare como pare (menos /stop): lo minado, al cofre
		if j.Fatal() == nil && m.carried() > 0 {
			if terr := m.trip("al acabar", true); terr != nil && err == nil {
				err = terr
			}
		}
		return err
	}
}

// errMineDone: se juntó lo pedido (total).
var errMineDone = errors.New("hecho")

func (m *oreMine) hasPick() bool { return m.j.UsablePicks() > 0 }

// carried: lo minado que lleva encima.
func (m *oreMine) carried() int { return m.j.CountWhere(m.itemOK) }

// mined: lo minado en total (lo que lleva, más lo dejado, menos lo que ya llevaba).
func (m *oreMine) mined() map[string]int {
	out := map[string]int{}
	for k, v := range m.j.Items() {
		if m.itemOK(k) {
			out[k] += v
		}
	}
	for k, v := range m.dep {
		out[k] += v
	}
	for k, v := range m.base {
		out[k] = max(out[k]-v, 0)
	}
	return out
}

func (m *oreMine) minedTotal() int {
	n := 0
	for _, v := range m.mined() {
		n += v
	}
	return n
}

func (m *oreMine) deposited() int {
	n := 0
	for _, v := range m.dep {
		n += v
	}
	return n
}

func (m *oreMine) status(what string) {
	m.j.Status("minar: %s (capa y=%d; llevo %d, dejados %d)", what, m.layer, m.carried(), m.deposited())
}

// summary: el resumen, al acabar o parar.
func (m *oreMine) summary() {
	var parts []string
	mined := m.mined()
	keys := make([]string, 0, len(mined))
	for k := range mined {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, mined[k]))
	}
	if len(parts) == 0 {
		parts = []string{"nada"}
	}
	m.j.Logf("minar: resumen: minado %s (en total %d; en el cofre %d, encima %d); %d viajes al cofre; %s",
		strings.Join(parts, ", "), m.minedTotal(), m.deposited(), m.carried(), m.trips, time.Since(m.started).Round(time.Second))
}

// note apunta la celda por la que se pasa (el camino de vuelta al cofre).
func (m *oreMine) note(p world.BlockPos) {
	if len(m.trail) == 0 || m.trail[len(m.trail)-1] != p {
		m.trail = append(m.trail, p)
	}
	m.cur = p
}

// loopTrail quita las vueltas de un recorrido (de una celda a la misma otra vez).
func loopTrail(t []world.BlockPos) []world.BlockPos {
	var out []world.BlockPos
	at := map[world.BlockPos]int{}
	for _, p := range t {
		if i, ok := at[p]; ok {
			for _, q := range out[i+1:] {
				delete(at, q)
			}
			out = out[:i+1]
			continue
		}
		at[p] = len(out)
		out = append(out, p)
	}
	return out
}

// turns son las cuatro direcciones, en el orden en que se prueban.
var turns = [][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}}

// goToLayer baja (o sube) a la capa en una escalera: un paso en horizontal
// y uno en vertical cada vez, con suelo firme debajo. Nada de cavar bajo los
// pies. Si el paso no se puede (una cueva, lava), gira.
func (m *oreMine) goToLayer() error {
	j := m.j
	if f := j.Feet(); f.Y != m.layer {
		j.Logf("minar: %s hasta y=%d en una escalera", map[bool]string{true: "bajo", false: "subo"}[f.Y > m.layer], m.layer)
	}
	turn, bad := 0, 0
	for {
		if err := j.Wait(1); err != nil {
			return err
		}
		f := j.Feet()
		if len(m.trail) > 0 && m.trail[len(m.trail)-1] != f { // (una pelea lo movió, un tropiezo: el recorrido tiene que seguir siendo contiguo)
			m.note(f)
		}
		if f.Y == m.layer {
			m.dir = turns[turn%4]
			return nil
		}
		if !m.hasPick() {
			if err := m.trip("sin pico", false); err != nil {
				return err
			}
			continue
		}
		m.status(fmt.Sprintf("escalera a y=%d (estoy en y=%d)", m.layer, f.Y))
		d := turns[turn%4]
		dy := -1
		if f.Y < m.layer {
			dy = 1
		}
		n := world.BlockPos{X: f.X + d[0], Y: f.Y + dy, Z: f.Z + d[1]}
		var open []world.BlockPos
		var floor world.BlockPos
		if dy < 0 { // los pies bajan: se abre el paso a la altura de ahora y la celda de abajo (y una más arriba: para volver a subir saltando)
			open = []world.BlockPos{{X: n.X, Y: f.Y + 2, Z: n.Z}, {X: n.X, Y: f.Y + 1, Z: n.Z}, {X: n.X, Y: f.Y, Z: n.Z}, n}
			floor = world.BlockPos{X: n.X, Y: n.Y - 1, Z: n.Z}
		} else { // sube: sitio para saltar encima de la cabeza
			open = []world.BlockPos{{X: f.X, Y: f.Y + 2, Z: f.Z}, {X: n.X, Y: n.Y + 1, Z: n.Z}, n}
			floor = world.BlockPos{X: n.X, Y: n.Y - 1, Z: n.Z}
		}
		if err := m.openPath(open, floor); err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			if errors.Is(err, ErrNoPicks) {
				if terr := m.trip("sin pico", false); terr != nil {
					return terr
				}
				continue
			}
			j.Logf("minar: la escalera no sigue hacia %v (%v): giro", d, err)
			turn++
			if bad++; bad >= 8 {
				if rerr := m.relocate(err); rerr != nil {
					return rerr
				}
				turn, bad = m.relocs, 0
			}
			continue
		}
		if err := j.GoTo(n); err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			j.Logf("minar: no llego al escalón %v (%v): giro", n, err)
			turn++
			if bad++; bad >= 8 {
				if rerr := m.relocate(err); rerr != nil {
					return rerr
				}
				turn, bad = m.relocs, 0
			}
			continue
		}
		bad = 0
		m.note(n)
		// (a la vista: la escalera también destapa cosas)
		if err := m.harvest(); err != nil {
			return err
		}
	}
}

// relocate: una cueva, lava o agua corta la escalera por todos lados: sube por
// ella y empieza otra a 10 bloques, en otra dirección (como mucho 3 veces).
func (m *oreMine) relocate(why error) error {
	j := m.j
	if m.relocs >= 3 {
		return fmt.Errorf("no consigo bajar a y=%d (estoy en %v, y ya probé otros 3 sitios): %w", m.layer, j.Feet(), why)
	}
	m.relocs++
	origin := m.trail[0]
	j.Logf("minar: la escalera se corta en %v (cueva, lava o agua): subo y empiezo otra a 10 bloques hacia %v", j.Feet(), turns[m.relocs%4])
	if len(m.trail) > 1 {
		out := loopTrail(m.trail)
		slices.Reverse(out)
		if err := m.walkTrail(out); err != nil {
			return fmt.Errorf("subir por la escalera: %w", err)
		}
	}
	d := turns[m.relocs%4]
	x, z := origin.X+d[0]*10, origin.Z+d[1]*10
	spot, found := world.BlockPos{}, false
	for y := origin.Y + 6; y >= origin.Y-6; y-- { // el suelo de esa columna (la superficie que se ve)
		p := world.BlockPos{X: x, Y: y, Z: z}
		if j.solid(world.BlockPos{X: x, Y: y - 1, Z: z}) && !j.liquid(world.BlockPos{X: x, Y: y - 1, Z: z}) && j.airCell(p) && j.airCell(world.BlockPos{X: x, Y: y + 1, Z: z}) {
			spot, found = p, true
			break
		}
	}
	if !found {
		return fmt.Errorf("no veo dónde pisar a 10 bloques de la escalera: %w", why)
	}
	if err := j.GoTo(spot); err != nil {
		return fmt.Errorf("ir a %v para otra escalera: %w", spot, err)
	}
	m.trail = []world.BlockPos{spot}
	m.cur = spot
	return nil
}

// openPath deja libres las celdas (de arriba abajo) con suelo firme en
// floor: sin agua ni lava (se tapa antes de abrir), sin hueco debajo.
func (m *oreMine) openPath(cells []world.BlockPos, floor world.BlockPos) error {
	j := m.j
	if j.liquid(floor) {
		return fmt.Errorf("agua o lava bajo %v", floor)
	}
	if !j.solid(floor) { // sin suelo: un bloque si lleva; si no, no se sigue
		fill := j.filler()
		if fill == "" {
			return fmt.Errorf("sin suelo en %v y nada con qué taparlo", floor)
		}
		if perr := j.Place(fill, floor); perr != nil || !j.solid(floor) {
			return fmt.Errorf("sin suelo en %v (no pude taparlo con %s: %v)", floor, fill, perr)
		}
	}
	for _, c := range cells {
		if j.solid(c) && !j.safeToOpen(c) && !j.pluggable(c) {
			return fmt.Errorf("agua o lava junto a %v que no puedo tapar", c)
		}
	}
	for _, c := range cells {
		if err := j.clearCell(c); err != nil {
			return err
		}
	}
	return nil
}

// advance cava la celda next (los pies; y la cabeza encima) y se pone en ella.
func (m *oreMine) advance(next world.BlockPos) error {
	j := m.j
	if !m.hasPick() {
		if err := m.trip("sin pico", false); err != nil {
			return err
		}
	}
	prev := j.Feet()
	head := world.BlockPos{X: next.X, Y: next.Y + 1, Z: next.Z}
	floor := world.BlockPos{X: next.X, Y: next.Y - 1, Z: next.Z}
	err := m.openPath([]world.BlockPos{head, next}, floor)
	if errors.Is(err, ErrNoPicks) {
		if terr := m.trip("sin pico", false); terr != nil {
			return terr
		}
		err = m.openPath([]world.BlockPos{head, next}, floor)
	}
	if err != nil {
		return err
	}
	if err := j.GoTo(next); err != nil {
		return err
	}
	m.note(next)
	m.torch(prev)
	return nil
}

// torch pone una antorcha (si lleva) cada torchEvery celdas, en la celda de atrás.
func (m *oreMine) torch(at world.BlockPos) {
	j := m.j
	if j.Count("torch") == 0 || len(m.trail)%torchEvery != 0 || at.Y != m.layer {
		return
	}
	if err := j.Place("torch", at); err != nil && !strings.Contains(j.Block(at), "torch") && !m.torched {
		m.torched = true
		j.Logf("minar: no pude poner antorchas (%v): sigo sin ellas", err)
	}
}

// run es el túnel principal y sus ramas.
func (m *oreMine) run() error {
	j := m.j
	start := j.Feet()
	d := m.dir
	perp := [2]int{-d[1], d[0]}
	at := func(i, s int) world.BlockPos {
		return world.BlockPos{X: start.X + d[0]*i + perp[0]*s, Y: start.Y, Z: start.Z + d[1]*i + perp[1]*s}
	}
	for i := 0; i <= m.cfg.MainLen; i++ {
		if i > 0 {
			if err := m.advance(at(i, 0)); err != nil {
				if j.Fatal() != nil {
					return j.Fatal()
				}
				return fmt.Errorf("el túnel principal no sigue en %v: %w (acabo aquí)", at(i, 0), err)
			}
		}
		if err := m.upkeep(); err != nil {
			return err
		}
		if i%branchGap != 0 {
			continue
		}
		for _, s := range []int{1, -1} {
			if err := m.branch(at, i, s); err != nil {
				return err
			}
		}
	}
	j.Logf("minar: el túnel principal llegó a su largo (%d): zona acabada", m.cfg.MainLen)
	return nil
}

// branch es una rama a un lado del túnel principal (en la celda i), y la vuelta.
func (m *oreMine) branch(at func(i, s int) world.BlockPos, i, s int) error {
	j := m.j
	main := at(i, 0)
	if err := m.standAt(main); err != nil {
		return err
	}
	keep := len(m.trail)
	for k := 1; k <= m.cfg.BranchLen; k++ {
		if err := j.Wait(1); err != nil {
			return err
		}
		if err := m.advance(at(i, k*s)); err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			j.Logf("minar: la rama %d (lado %+d) acaba en %d: %v", i, s, k-1, err)
			break
		}
		if err := m.upkeep(); err != nil {
			return err
		}
	}
	// de vuelta al túnel principal (el camino de ida desaparece del recorrido)
	if err := m.standAt(main); err != nil {
		return err
	}
	if keep <= len(m.trail) {
		m.trail = m.trail[:keep]
	}
	m.cur = main
	return nil
}

// standAt vuelve a la celda p del túnel (andando; si no, cavando hasta allí).
func (m *oreMine) standAt(p world.BlockPos) error {
	j := m.j
	if j.Feet() == p {
		return nil
	}
	if err := j.GoTo(p); err == nil && j.Feet() == p {
		return nil
	}
	if j.Fatal() != nil {
		return j.Fatal()
	}
	return j.standAt(p)
}

// upkeep: lo que toca tras cada celda: lo que se ve, tirar basura, picos, dejar lo minado.
func (m *oreMine) upkeep() error {
	j := m.j
	if err := m.harvest(); err != nil {
		return err
	}
	if m.cfg.Total > 0 && m.minedTotal() >= m.cfg.Total {
		j.Logf("minar: juntados %d de %d", m.minedTotal(), m.cfg.Total)
		return errMineDone // (lo que lleve se deja al acabar)
	}
	j.makeRoom()
	why := ""
	switch {
	case !m.hasPick():
		why = "sin pico"
	case m.carried() >= m.cfg.Every:
		why = fmt.Sprintf("llevo %d", m.carried())
	case j.FreeSlots() <= 1 && len(m.toStore()) > 0:
		why = "inventario lleno"
	}
	if why != "" {
		if err := m.trip(why, false); err != nil {
			return err
		}
	}
	if m.cfg.Total > 0 && m.minedTotal() >= m.cfg.Total && why != "" {
		return errMineDone // (los que faltaban para el total se juntaron con esa vuelta)
	}
	m.status("minando")
	return nil
}

// harvest pica lo que se ve ahora (y la veta que aparece al picarlo) y lo recoge.
func (m *oreMine) harvest() error {
	j := m.j
	took := false
	for round := 0; round < 80; round++ {
		if err := j.Wait(1); err != nil {
			return err
		}
		skip := map[world.BlockPos]bool{}
		for p, n := range m.fails {
			if n >= 3 {
				skip[p] = true
			}
		}
		f := j.Feet()
		ps := j.visibleOres(sightRadius, m.oreOK, skip)
		// nada por debajo del suelo que pisa: picarlo deja un hueco en el camino, y
		// cavar hacia abajo es lo que no se hace (las menas de la capa, a la
		// altura del túnel o por encima, son las que se sacan)
		ps = slices.DeleteFunc(ps, func(p world.BlockPos) bool { return p.Y < f.Y })
		if len(ps) == 0 {
			break
		}
		p := ps[0]
		if !j.canDig(p) && j.eyeDist(p) > 6 {
			break // (lejos y sin llegar: cuando se acerque)
		}
		if !m.hasPick() {
			break // (upkeep va al cofre)
		}
		m.status(fmt.Sprintf("%s en %v", j.Block(p), p))
		if m.take(p) {
			took = true
		}
		if j.Fatal() != nil {
			return j.Fatal()
		}
	}
	if took {
		// (solo lo que sueltan los minerales: lo demás cae en las celdas que se andan)
		if err := m.fetchDrops(); err != nil {
			return err
		}
	}
	if j.Feet() != m.cur {
		if err := m.standAt(m.cur); err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			return fmt.Errorf("no vuelvo al túnel (%v): %w", m.cur, err)
		}
	}
	return nil
}

// take pica el mineral p (que se ve): llega hasta él y lo saca. Dice si lo sacó.
func (m *oreMine) take(p world.BlockPos) bool {
	j := m.j
	fail := func(why string, err error) bool {
		m.fails[p]++
		if m.fails[p] == 3 {
			j.Logf("minar: dejo %s en %v: %s %v", j.Block(p), p, why, err)
		}
		return false
	}
	up := world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}
	if j.liquid(up) && !j.fluidBlock(up) {
		m.fails[p] = 3 // (bajo un bloque inundado: eso no se tapa)
		return false
	}
	if !j.canDig(p) {
		if err := m.digTo(p, func() bool { return j.canDig(p) }); err != nil {
			return fail("no llego:", err)
		}
	}
	if !j.makeSafe(p) {
		m.fails[p] = 3
		j.Logf("minar: %s en %v tiene lava o agua al lado que no puedo tapar: lo dejo", j.Block(p), p)
		return false
	}
	name := j.Block(p)
	if err := j.Dig(p); err != nil {
		return fail("no se deja picar:", err)
	}
	if m.oreOK(j.Block(p)) {
		return fail("sigue ahí:", errors.New("el bloque no se rompió"))
	}
	m.dug++
	j.Logf("minar: piqué %s en %v", name, p)
	return true
}

// trip: al cofre (por el túnel que se cavó, a pie por la superficie), deja lo
// minado, coge picos si hacen falta y vuelve al sitio donde estaba.
func (m *oreMine) trip(why string, final bool) error {
	j := m.j
	m.trips++
	back := m.cur
	m.status("al cofre (" + why + ")")
	j.Logf("minar: al cofre %v (%s)", m.cfg.Chest, why)
	if len(m.trail) > 1 {
		if err := m.standAt(m.cur); err != nil {
			return err
		}
		out := loopTrail(m.trail)
		slices.Reverse(out)
		if err := m.walkTrail(out); err != nil {
			return fmt.Errorf("salir por el túnel: %w", err)
		}
	}
	if err := m.visitChests(); err != nil {
		return err
	}
	if !m.hasPick() && !m.waitForPick() {
		return fmt.Errorf("no tengo ningún pico de nivel %d o más con más de %d usos (no rompo ninguno): déjalos en el cofre %v o tírame uno, y repite la orden", m.need, pickReserve, m.cfg.Chest)
	}
	if final {
		return nil
	}
	if len(m.trail) > 1 {
		j.Logf("minar: vuelvo a la mina")
		fwd := loopTrail(m.trail)
		if err := j.GoTo(fwd[0]); err != nil && j.Feet() != fwd[0] {
			return fmt.Errorf("volver a la mina (%v): %w", fwd[0], err)
		}
		if err := m.walkTrail(fwd); err != nil {
			return fmt.Errorf("volver por el túnel: %w", err)
		}
	}
	m.cur = back
	return nil
}

// walkTrail anda por las celdas del recorrido en ese orden. Si una no se
// alcanza (algo cambió: una pelea lo apartó, cayó grava), prueba con las
// siguientes: lo que importa es llegar al final.
func (m *oreMine) walkTrail(cells []world.BlockPos) error {
	j := m.j
	pf := j.pickFloor
	j.pickFloor = 0 // (el camino es todo aire: aunque el pico esté casi gastado)
	defer func() { j.pickFloor = pf }()
	for i := 0; i < len(cells); {
		err := j.walkCells(cells[i : i+1])
		if err == nil {
			i++
			continue
		}
		if j.Fatal() != nil {
			return j.Fatal()
		}
		// algo se cerró (grava, un bloque que puso el pathfinding): cavar hasta la celda
		if serr := j.standAt(cells[i]); serr == nil && j.Feet() == cells[i] {
			i++
			continue
		}
		if j.Fatal() != nil {
			return j.Fatal()
		}
		skipped := false
		for k := 1; k <= 6 && i+k < len(cells); k++ {
			if j.walkCells(cells[i+k:i+k+1]) == nil {
				j.Logf("minar: salto %d celdas del recorrido (no llegaba a %v: %v)", k, cells[i], err)
				i += k + 1
				skipped = true
				break
			}
			if j.Fatal() != nil {
				return j.Fatal()
			}
		}
		if !skipped {
			// último recurso: el camino hasta la última celda (el pathfinding sabe andar por lo cavado)
			if gerr := j.GoTo(cells[len(cells)-1]); gerr == nil {
				return nil
			}
			return err
		}
	}
	return nil
}

// wall son los cofres (del más cercano al más lejano): el dado (o el
// contenedor pegado a él, un bloque de error al dar las coordenadas) y los que
// lo tocan por una cara (una pared de cofres se da con uno solo).
func (m *oreMine) wall() []world.BlockPos {
	j := m.j
	c := m.cfg.Chest
	start, ok := c, IsContainer(j.Block(c))
	if !ok && j.Block(c) != "" {
		for _, d := range neighbours26 {
			if n := add(c, d); IsContainer(j.Block(n)) {
				start, ok = n, true
				break
			}
		}
	}
	if !ok {
		return []world.BlockPos{c} // (sin cargar: se va y se mira allí)
	}
	seen := map[world.BlockPos]bool{}
	var out []world.BlockPos
	queue := []world.BlockPos{start}
	for len(queue) > 0 && len(out) < 128 {
		p := queue[0]
		queue = queue[1:]
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
		for _, d := range faces {
			n := add(p, d)
			if !seen[n] && abs(n.X-start.X) <= 6 && abs(n.Y-start.Y) <= 6 && abs(n.Z-start.Z) <= 6 && IsContainer(j.Block(n)) {
				queue = append(queue, n)
			}
		}
	}
	here := j.Feet()
	sort.SliceStable(out, func(a, b int) bool { return dist2(out[a], here) < dist2(out[b], here) })
	return out
}

// visitChests abre los cofres de la pared: en cada uno deja lo minado y lo
// valioso (nunca herramientas ni comida), devuelve los picos gastados y coge
// picos hasta llevar wantPicks buenos.
func (m *oreMine) visitChests() error {
	j := m.j
	twin := map[world.BlockPos]bool{}
	opened, full := 0, map[world.BlockPos]bool{}
	for _, c := range m.wall() {
		pending := len(m.toStore()) > 0 || m.wornPicks() > 0
		if !pending && j.UsablePicks() >= wantPicks {
			break
		}
		if twin[c] || full[c] {
			continue
		}
		if b := j.Block(c); b != "" && !IsContainer(b) {
			continue
		}
		j.Status("minar: en el cofre %v", c)
		if err := j.Open(c); err != nil {
			j.Logf("minar: no pude abrir el cofre %v: %v", c, err)
			continue
		}
		opened++
		scr, ok := j.r.d.Screens.Open()
		if !ok || scr.PlayerStart() < 0 {
			continue
		}
		start := scr.PlayerStart()
		before := map[string]int{}
		for k, v := range j.Items() {
			if m.itemOK(k) {
				before[k] = v
			}
		}
		click := func(slot int) bool {
			return j.r.d.Screens.Click(scr.ID, slot, 0, types.ContainerInputQuickMove) == nil
		}
		// 1. lo minado y lo valioso
		for _, i := range m.toStore() {
			cnt := j.slotCount(i)
			if !click(start + (i - invStart)) {
				break
			}
			_ = j.Wait(2)
			if after := j.slotCount(i); after >= cnt {
				full[c] = true // (no cupo nada)
				break
			} else if after > 0 {
				full[c] = true // (cupo una parte)
				break
			}
		}
		// 2. los picos gastados, para repararlos
		for i := invStart; i <= invEnd; i++ {
			if l := j.pickLeft(j.slotStack(i)); l >= 0 && l <= pickReserve {
				if click(start+(i-invStart)) && j.Wait(2) == nil {
					j.Logf("minar: dejo un pico gastado (%d usos)", l)
				}
			}
		}
		// 3. picos buenos (los de más usos)
		if j.UsablePicks() < wantPicks {
			type pick struct{ slot, left int }
			var picks []pick
			for i := 0; i < start; i++ {
				if l := j.pickLeft(types.ItemStack(scr.Slots[i])); scr.Slots[i].Count > 0 && l > pickReserve {
					picks = append(picks, pick{i, l})
				}
			}
			sort.Slice(picks, func(a, b int) bool { return picks[a].left > picks[b].left })
			for _, p := range picks {
				if j.UsablePicks() >= wantPicks || j.FreeSlots() == 0 {
					break
				}
				if click(p.slot) {
					_ = j.Wait(2)
					j.Logf("minar: cogí un pico del cofre (%d usos)", p.left)
				}
			}
		}
		now := j.Items()
		for k, v := range before {
			if now[k] < v {
				m.dep[k] += v - now[k]
			}
		}
		if start >= 54 { // un cofre doble: su otra mitad es el mismo
			for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				if n := (world.BlockPos{X: c.X + d[0], Y: c.Y, Z: c.Z + d[1]}); strings.Contains(j.Block(n), "chest") {
					twin[n] = true
				}
			}
		}
	}
	j.CloseScreen()
	if opened == 0 {
		return fmt.Errorf("no pude abrir ningún cofre en %v", m.cfg.Chest)
	}
	if n := len(m.toStore()); n > 0 {
		j.Logf("minar: los cofres están llenos (o no llego a ellos): me quedan %d montones por dejar", n)
	}
	j.Logf("minar: tras el cofre: llevo %d picos buenos, %d minerales encima, %d dejados en total", j.UsablePicks(), m.carried(), m.deposited())
	return nil
}

// valuable: lo que se deja en el cofre: lo que sueltan los minerales (de
// cualquiera de los que /mine conoce), lo que sale de fundirlos y las gemas y
// cosas raras que aparezcan. Nunca herramientas, armadura ni comida, ni la
// basura natural (hojarasca, plantones, palos…).
func valuable(n string) bool {
	if n == "" || excavateKeeps(n) || junk(n) {
		return false
	}
	for _, o := range OreSpecs {
		if slices.Contains(o.Items, n) {
			return true
		}
	}
	switch {
	case strings.HasSuffix(n, "_ore"), strings.HasPrefix(n, "raw_"), strings.HasSuffix(n, "_ingot"), strings.HasSuffix(n, "_nugget"),
		strings.HasPrefix(n, "enchanted_"), strings.HasSuffix(n, "_upgrade_smithing_template"), strings.HasSuffix(n, "_armor_trim_smithing_template"):
		return true
	}
	switch n {
	case "diamond", "emerald", "lapis_lazuli", "redstone", "quartz", "amethyst_shard", "coal", "ancient_debris", "netherite_scrap",
		"gold_ingot", "iron_ingot", "copper_ingot", "glowstone_dust", "name_tag", "saddle", "heart_of_the_sea", "nautilus_shell",
		"diamond_block", "emerald_block", "gold_block", "iron_block", "lapis_block", "redstone_block", "netherite_block", "ender_pearl", "echo_shard", "totem_of_undying":
		return !excavateKeeps(n)
	}
	return false
}

// toStore: los slots del inventario con algo que dejar en el cofre.
func (m *oreMine) toStore() []int {
	var out []int
	for i := invStart; i <= invEnd; i++ {
		sl := m.j.slotStack(i)
		if sl.Count > 0 && valuable(strings.TrimPrefix(itemName(int32(sl.Item)), "minecraft:")) {
			out = append(out, i)
		}
	}
	return out
}

// wornPicks: los picos gastados que lleva (para devolverlos al cofre).
func (m *oreMine) wornPicks() int {
	n := 0
	for i := invStart; i <= invEnd; i++ {
		if l := m.j.pickLeft(m.j.slotStack(i)); l >= 0 && l <= pickReserve {
			n++
		}
	}
	return n
}

// slotStack es lo que hay en el slot i del inventario.
func (j *Job) slotStack(i int) types.ItemStack {
	s := j.r.d.Screens
	s.Lock()
	defer s.Unlock()
	return s.Inventory.Slots[i]
}

// waitForPick: sin pico, lo pide por el chat una vez y espera pickWait a que se
// lo tiren. Dice si ya tiene uno.
func (m *oreMine) waitForPick() bool {
	j := m.j
	if m.asked {
		return false
	}
	m.asked = true
	secs := int(pickWait / time.Second)
	j.say(fmt.Sprintf("No tengo pico para minar (hace falta uno de nivel %d o más con usos de sobra) y no hay en el cofre %v. Déjalos en el cofre o tírame uno; te espero %d s.",
		m.need, m.cfg.Chest, secs))
	for waited := 0; waited < secs; waited++ {
		j.Status("esperando un pico (%d s de %d)", waited, secs)
		if err := j.Rest(20); err != nil {
			return false
		}
		if m.hasPick() {
			j.Logf("minar: ya tengo pico: sigo")
			return true
		}
	}
	return false
}

// digTo cava un túnel corto hacia target (andando por lo que ya está abierto,
// sin buscar caminos largos) hasta que there() sea cierto: un escalón cada vez,
// con suelo firme, sin agua ni lava. Para cosas que se ven cerca: lo de lejos
// (una cueva, subir a la superficie a dar la vuelta) no es esto.
func (m *oreMine) digTo(target world.BlockPos, there func() bool) error {
	j := m.j
	visited := map[world.BlockPos]bool{j.Feet(): true}
	dist := func(p world.BlockPos) int { return abs(target.X-p.X) + abs(target.Z-p.Z) + 2*abs(target.Y-p.Y) }
	if dist(j.Feet()) > 3*sightRadius {
		return fmt.Errorf("%v queda lejos", target)
	}
	best, since := dist(j.Feet()), 0
	for steps := 0; steps < 40; steps++ {
		if err := j.Wait(1); err != nil {
			return err
		}
		if there() {
			return nil
		}
		cur := j.Feet()
		visited[cur] = true
		if d := dist(cur); d < best {
			best, since = d, 0
		} else if since++; since > 6 {
			return fmt.Errorf("no avanzo hacia %v (estoy en %v)", target, cur)
		}
		next, ok := j.nextTunnelCell(cur, target, visited)
		if !ok || next == target || next == (world.BlockPos{X: target.X, Y: target.Y - 1, Z: target.Z}) {
			if there() {
				return nil
			}
			return fmt.Errorf("no puedo cavar hacia %v (agua, lava o nada donde pisar)", target)
		}
		cells := []world.BlockPos{{X: next.X, Y: next.Y + 1, Z: next.Z}, next}
		if next.Y > cur.Y {
			cells = append([]world.BlockPos{{X: cur.X, Y: cur.Y + 2, Z: cur.Z}}, cells...)
		}
		if next.Y < cur.Y {
			cells = append([]world.BlockPos{{X: next.X, Y: next.Y + 2, Z: next.Z}}, cells...)
		}
		for _, c := range cells {
			if err := j.clearCell(c); err != nil {
				return err
			}
		}
		if err := j.GoTo(next); err != nil {
			return fmt.Errorf("túnel: %w", err)
		}
	}
	return fmt.Errorf("demasiado lejos para un túnel corto: %v", target)
}

// fetchDrops recoge lo que soltaron los minerales picados (que cae en su hueco,
// a veces cerrado): se cava hasta allí, sin dar vueltas por fuera.
func (m *oreMine) fetchDrops() error {
	j := m.j
	if j.FreeSlots() == 0 {
		return nil
	}
	tried := map[int32]bool{}
	for round := 0; round < 8; round++ {
		p := j.Pos()
		var target *entities.Entity
		for _, e := range j.r.d.Entities.Nearby(p.X, p.Y, p.Z, 7) {
			if e.Type != "minecraft:item" || tried[e.ID] {
				continue
			}
			v, has := e.Data[entitydata.ItemEntityItem]
			if !has {
				continue
			}
			if st, ok := v.Value.(*types.ItemStack); ok && m.itemOK(strings.TrimPrefix(itemName(int32(st.Item)), "minecraft:")) && !j.burning(itemCell(e)) {
				e := e
				target = &e
				break
			}
		}
		if target == nil {
			return nil
		}
		tried[target.ID] = true
		id, cell := target.ID, itemCell(*target)
		gone := func() bool {
			_, still := j.r.d.Entities.Get(id)
			return !still
		}
		near := func() bool {
			f := j.Feet()
			return gone() || abs(f.X-cell.X) <= 1 && abs(f.Z-cell.Z) <= 1 && (f.Y == cell.Y || f.Y == cell.Y+1)
		}
		m.status(fmt.Sprintf("a por lo que soltó el mineral (%v)", cell))
		if err := m.digTo(cell, near); err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			j.Logf("minar: no llego a lo que soltó el mineral en %v: %v", cell, err)
			continue
		}
		for t := 0; t < 10 && !gone(); t++ {
			if err := j.Wait(1); err != nil {
				return err
			}
		}
		if gone() {
			continue
		}
		// al lado pero sin cogerlo: meterse en su celda, abriendo encima para que quepa
		var err error
		for _, dy := range []int{2, 1, 0} {
			c := world.BlockPos{X: cell.X, Y: cell.Y + dy, Z: cell.Z}
			if dy == 2 && j.Feet().Y == cell.Y {
				continue
			}
			if err = j.clearCell(c); err != nil {
				break
			}
		}
		if err == nil {
			err = j.GoTo(cell)
		}
		for t := 0; t < 20 && !gone() && err == nil; t++ {
			err = j.Wait(1)
		}
		if j.Fatal() != nil {
			return j.Fatal()
		}
		if !gone() {
			j.Logf("minar: no consigo coger lo de %v: %v", cell, err)
		}
	}
	return nil
}
