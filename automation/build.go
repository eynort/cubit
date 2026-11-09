package automation

// /build: construir un schematic entero con los materiales de unos cofres.
//
// El jugador que tiene el schematic en Litematica dice dónde va (origen,
// rotación, espejo: lo que ve en su pantalla); el bot lo coloca igual
// (schematic.Placed) y, celda a celda, deja el mundo como pide:
//
//  1. mira los cofres de materiales y cuenta lo que hay y lo que falta;
//  2. quita lo que sobra (tierra, hierba, bloques equivocados), de arriba abajo;
//  3. pone los bloques de abajo arriba, capa a capa: para cada uno busca contra
//     qué bloque pincharlo y desde dónde, para que quede con la orientación y
//     la mitad que pide; si no llega, se pone en otro sitio (andando, o
//     subiendo en torre o haciendo puente con bloques que no son del schematic);
//  4. cuando le faltan materiales vuelve a los cofres a por los de la tanda
//     siguiente;
//  5. al acabar quita sus andamios y comprueba todo otra vez, hasta que
//     esté al 100 % (o no haya manera: dice qué falta y por qué).

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
	"github.com/mj41/go-mc26/protocol/types"

	"minecraft-bot/pathing"
	"minecraft-bot/schematic"
)

// BuildOptions son las opciones de /build start.
type BuildOptions struct {
	Name    string           // el nombre del schematic (para el estado)
	NoClear bool             // no quitar lo que sobra donde el schematic tiene aire
	Chests  []world.BlockPos // los cofres de materiales (vacío: los de alrededor del bot)
}

const (
	buildReach    = 4.4 // alcance para planear (el juego: 4.5 desde los ojos)
	buildTries    = 3   // intentos por bloque antes de dejarlo
	chestAutoR    = 12  // sin cofres apuntados: los que haya a esta distancia
	scaffoldWant  = 64  // bloques de andamio que llevar
	missingExpiry = 3 * time.Minute
)

// cellInfo es lo que se sabe de un bloque de la paleta.
type cellInfo struct {
	st       schematic.State
	item     string
	n        int  // cuántos objetos
	ok       bool // el bot sabe ponerlo
	auto     bool // lo pone otro bloque (la mitad de arriba de una puerta)
	below    bool // necesita algo debajo (antorchas, alfombras, plantas, raíles)
	solid    bool // tiene colisión (no se pone donde está el cuerpo)
	key      string
	stateID  block.StateID
	hasState bool
}

type taskKind int

const (
	taskPlace taskKind = iota
	taskClear
	taskFix  // el bloque está, pero hay que pulsarlo (abrir, retardo, nota…)
	taskMore // está a medias: otra losa encima, otra vela…
)

type cellTask struct {
	pos  world.BlockPos
	idx  int32
	kind taskKind
}

type buildJob struct {
	j    *Job
	p    *schematic.Placed
	opts BuildOptions
	st   *BuildState

	info   []cellInfo
	parsed map[block.StateID]schematic.State

	tries   map[world.BlockPos]int
	skipped map[world.BlockPos]string
	badSpot map[[2]world.BlockPos]bool // (celda, sitio) desde donde no se pudo
	noReach map[world.BlockPos]int     // sitios a los que el walker no llegó (para cualquier celda)

	chests   []world.BlockPos
	index    map[world.BlockPos]map[string]int // lo que hay en cada cofre (lo visto al abrirlo)
	missing  map[string]time.Time              // objetos que no hay en ningún cofre (y cuándo se vio)
	supports map[world.BlockPos]bool           // andamios puestos por la tarea
	before   map[world.BlockPos]bool           // lo que el constructor del walker ya había puesto antes

	placed, dug int
	attempts    int  // intentos fallidos (para saber si se avanza)
	finishing   bool // los andamios ya se pueden quitar
	retries     int  // vueltas en las que se reintentó lo descartado
}

// Build es la tarea de construir el schematic colocado.
func Build(p *schematic.Placed, opts BuildOptions) func(j *Job) error {
	return func(j *Job) error {
		b := &buildJob{j: j, p: p, opts: opts, st: j.r.Build,
			parsed: map[block.StateID]schematic.State{}, tries: map[world.BlockPos]int{},
			skipped: map[world.BlockPos]string{}, badSpot: map[[2]world.BlockPos]bool{}, noReach: map[world.BlockPos]int{},
			index: map[world.BlockPos]map[string]int{}, missing: map[string]time.Time{},
			supports: map[world.BlockPos]bool{}, before: map[world.BlockPos]bool{}}
		return b.run()
	}
}

func (b *buildJob) run() error {
	j := b.j
	d := j.r.d
	b.prepareInfo()
	need := b.totalNeeds()
	j.Logf("construir %s: %s; caja %v → %v (%v); %d bloques a poner",
		b.opts.Name, b.p.Placement, b.p.Min, b.p.Max, b.p.Size(), sumNeeds(need))
	if d.Builder != nil {
		for _, q := range d.Builder.Placed() {
			b.before[q] = true
		}
		// los materiales no se gastan en torres; lo construido no se rompe para pasar
		d.Builder.SetKeep(func(item string) bool { return b.isMaterial(item) })
		d.Builder.SetNoBreak(func(q world.BlockPos) bool {
			i := b.p.Index(schematic.Pos{X: q.X, Y: q.Y, Z: q.Z})
			return i >= 0 && !b.p.Palette[i].IsAir()
		})
		defer d.Builder.SetKeep(nil)
		defer d.Builder.SetNoBreak(nil)
	}
	if d.Walker != nil {
		d.Walker.SetBuild(true)
		// sus andamios se rompen para pasar (si no, se encierra entre ellos y las paredes)
		d.Walker.SetPassable(b.isScaffold)
		defer d.Walker.SetPassable(nil)
		d.Walker.SetSafeFalls(true)
		defer d.Walker.SetSafeFalls(false)
	}
	// 1. los cofres
	b.chests = b.opts.Chests
	if len(b.chests) == 0 {
		b.chests = b.chestsAround(j.Feet(), chestAutoR)
		if len(b.chests) > 0 {
			j.Logf("cofres de materiales: no hay apuntados, uso los %d de alrededor: %v", len(b.chests), b.chests)
		}
	}
	if len(b.chests) > 0 {
		j.Status("mirando los cofres de materiales")
		for _, c := range b.chests {
			if err := b.visitChest(c, nil); err != nil {
				if j.Fatal() != nil {
					return j.Fatal()
				}
				j.Logf("cofre %v: %v", c, err)
			}
		}
	} else {
		j.Logf("cofres de materiales: ninguno (/build chest add): construyo con lo que llevo")
	}
	b.reportMaterials(need)
	if d.Builder != nil && d.Builder.Blocks() < 16 {
		scaffold := false
		for _, in := range b.index {
			for it, n := range in {
				if n > 0 && !b.isMaterial(it) && pathing.IsBuildBlock(it) {
					scaffold = true
				}
			}
		}
		if !scaffold {
			j.Logf("ojo: no llevo bloques para andamios (tierra, adoquines…) ni los veo en los cofres: sin ellos no subo a lo alto")
		}
	}
	// 2-5. el bucle: limpiar, poner, reponer, hasta que esté
	return b.loop()
}

// ---- lo que pide cada celda ----

func (b *buildJob) prepareInfo() {
	b.info = make([]cellInfo, len(b.p.Palette))
	byName := map[string][]block.StateID{}
	for _, s := range b.p.Palette {
		byName["minecraft:"+s.Name] = nil
	}
	for i, bl := range block.StateList {
		if _, ok := byName[bl.ID()]; ok {
			byName[bl.ID()] = append(byName[bl.ID()], block.StateID(i))
		}
	}
	for i, s := range b.p.Palette {
		item, n, ok := schematic.Item(s)
		ci := cellInfo{st: s, item: item, n: n, ok: ok, auto: ok && n == 0 && item == "", key: ruleKey(s)}
		// el estado del juego que más se le parece (para su forma)
		bestScore := -1
		for _, id := range byName["minecraft:"+s.Name] {
			have := schematic.ParseState(world.StateString(id))
			score := 0
			for k, v := range s.Props {
				if have.Props[k] == v {
					score++
				}
			}
			if score > bestScore {
				bestScore, ci.stateID, ci.hasState = score, id, true
			}
		}
		if ci.hasState {
			ci.solid = len(block.CollisionShape(ci.stateID)) > 0
		}
		ci.below = needsBelow(s, ci.solid) || schematic.Falls(s.Name)
		b.info[i] = ci
	}
}

// ruleKey es la clave de lo aprendido de un bloque: su nombre y cómo se pega (face).
func ruleKey(s schematic.State) string {
	if f := s.Props["face"]; f != "" {
		return s.Name + "|" + f
	}
	return s.Name
}

// needsBelow: el bloque se cae (o no se puede poner) sin algo debajo.
func needsBelow(s schematic.State, solid bool) bool {
	n := s.Name
	if schematic.Wall(n) || schematic.Hanging(n) || s.Props["hanging"] == "true" || s.Props["face"] == "wall" || s.Props["face"] == "ceiling" {
		return false
	}
	for _, k := range []string{"vine", "lichen", "hanging_roots", "spore_blossom", "sculk_vein", "dripstone", "ladder", "cocoa", "tripwire_hook", "_button", "lever", "chain"} {
		if strings.Contains(n, k) {
			return false
		}
	}
	if !solid {
		return true
	}
	for _, k := range []string{"carpet", "repeater", "comparator", "snow", "candle", "sea_pickle", "turtle_egg", "flower_pot", "rail", "pressure_plate", "_door", "_bed", "lantern", "bell"} {
		if strings.Contains(n, k) {
			return k != "lantern" || s.Props["hanging"] != "true"
		}
	}
	return false
}

// stateAt es el bloque del mundo en p como State (ok false: sin cargar).
func (b *buildJob) stateAt(p world.BlockPos) (schematic.State, block.StateID, bool) {
	st, ok := b.j.r.d.World.BlockAt(p)
	if !ok {
		return schematic.State{}, 0, false
	}
	if s, ok := b.parsed[st]; ok {
		return s, st, true
	}
	s := schematic.ParseState(world.StateString(st))
	b.parsed[st] = s
	return s, st, true
}

// matches: el bloque del mundo vale (lo que no hay manera de dejar bien, no cuenta).
func (b *buildJob) matches(want, have schematic.State) bool {
	if schematic.Matches(want, have) {
		return true
	}
	if want.Name != have.Name || !b.st.loose(ruleKey(want)+"|facing") {
		return false
	}
	w := schematic.State{Name: want.Name, Props: map[string]string{}}
	for k, v := range want.Props {
		if k != "facing" {
			w.Props[k] = v
		}
	}
	return schematic.Matches(w, have)
}

func (b *buildJob) isMaterial(item string) bool {
	for _, ci := range b.info {
		if ci.item == item {
			return true
		}
	}
	return false
}

// ---- el estado de la obra ----

type scanResult struct {
	place, clear, fix []cellTask
	unknown           []world.BlockPos
	done, total       int
	unsupported       map[string]int
}

func toPos(p schematic.Pos) world.BlockPos { return world.BlockPos{X: p.X, Y: p.Y, Z: p.Z} }

func (b *buildJob) scan() scanResult {
	r := scanResult{unsupported: map[string]int{}}
	b.p.Each(func(sp schematic.Pos, idx int32) {
		pos := toPos(sp)
		ci := &b.info[idx]
		want := ci.st
		have, st, ok := b.stateAt(pos)
		if !ok {
			if !want.IsAir() {
				r.unknown = append(r.unknown, pos)
			}
			return
		}
		if want.IsAir() {
			if have.IsAir() || b.opts.NoClear || !b.clearable(pos, have, st) {
				return
			}
			if !b.finishing && b.isScaffold(pos) {
				return // un andamio: se quita al final
			}
			r.clear = append(r.clear, cellTask{pos: pos, idx: idx, kind: taskClear})
			return
		}
		if !ci.ok {
			if !have.IsAir() && have.Name == want.Name {
				return // (ya está: fluido, portal…)
			}
			r.unsupported[want.Name]++
			return
		}
		r.total++
		if ci.auto { // la mitad de arriba: la pone la de abajo
			if have.Name == want.Name {
				r.done++
			} else if !block.Replaceable(st) && b.clearable(pos, have, st) && (b.finishing || !b.isScaffold(pos)) {
				r.clear = append(r.clear, cellTask{pos: pos, idx: idx, kind: taskClear})
			}
			return
		}
		if b.matches(want, have) {
			r.done++
			return
		}
		if have.Name == want.Name {
			switch {
			case b.fixable(want, have):
				r.fix = append(r.fix, cellTask{pos: pos, idx: idx, kind: taskFix})
				return
			case b.moreNeeded(want, have):
				r.place = append(r.place, cellTask{pos: pos, idx: idx, kind: taskMore})
				return
			}
		}
		if block.Replaceable(st) {
			r.place = append(r.place, cellTask{pos: pos, idx: idx, kind: taskPlace})
			return
		}
		if b.clearable(pos, have, st) && (b.finishing || !b.isScaffold(pos)) {
			r.clear = append(r.clear, cellTask{pos: pos, idx: idx, kind: taskClear})
		}
	})
	return r
}

// clearable: el bloque se puede (y se debe) quitar: no un fluido, ni la roca
// madre, ni un cofre (los materiales), ni una zona protegida.
func (b *buildJob) clearable(p world.BlockPos, have schematic.State, st block.StateID) bool {
	if block.DestroySpeed(st) < 0 || block.FluidOf(st) != nil && len(block.CollisionShape(st)) == 0 {
		return false
	}
	switch have.Name {
	case "water", "lava", "bubble_column", "fire", "soul_fire", "nether_portal", "end_portal":
		return false
	}
	if isChest(have.Name) || strings.HasSuffix(have.Name, "shulker_box") {
		return false
	}
	for _, c := range b.chests {
		if c == p {
			return false
		}
	}
	return !b.j.r.Store.IsProtected(p)
}

func (b *buildJob) isScaffold(p world.BlockPos) bool {
	if b.supports[p] {
		return true
	}
	bd := b.j.r.d.Builder
	return bd != nil && bd.WasPlaced(p) && !b.before[p]
}

// cyclable son las propiedades que se cambian pulsando el bloque.
func cyclable(name, k string) bool {
	switch k {
	case "open":
		return !strings.HasPrefix(name, "iron_")
	case "delay", "mode", "note", "inverted":
		return true
	case "powered":
		return name == "lever"
	}
	return false
}

// fixable: solo difieren propiedades que se cambian pulsando.
func (b *buildJob) fixable(want, have schematic.State) bool {
	diff := false
	for k, v := range want.Props {
		if !schematic.Controlled(want.Name, k, v) || have.Props[k] == v {
			continue
		}
		if !cyclable(want.Name, k) {
			return false
		}
		diff = true
	}
	return diff
}

// moreNeeded: es el mismo bloque a medias (una losa de las dos, menos velas).
func (b *buildJob) moreNeeded(want, have schematic.State) bool {
	if want.Props["type"] == "double" && (have.Props["type"] == "bottom" || have.Props["type"] == "top") {
		return true
	}
	for _, k := range []string{"candles", "pickles", "eggs", "layers", "flower_amount", "segment_amount"} {
		if w, ok := want.Props[k]; ok {
			if atoiDef(have.Props[k], 0) < atoiDef(w, 0) {
				for k2, v2 := range want.Props { // lo demás igual (si no, quitar y poner)
					if k2 != k && schematic.Controlled(want.Name, k2, v2) && have.Props[k2] != v2 {
						return false
					}
				}
				return true
			}
		}
	}
	return false
}

// ---- materiales ----

// totalNeeds son los objetos que hacen falta para lo que queda por poner.
func (b *buildJob) totalNeeds() map[string]int {
	need := map[string]int{}
	r := b.scan()
	for _, t := range r.place {
		b.addNeed(need, t)
	}
	return need
}

func (b *buildJob) addNeed(need map[string]int, t cellTask) {
	ci := b.info[t.idx]
	if ci.item == "" {
		return
	}
	n := ci.n
	if t.kind == taskMore {
		have, _, _ := b.stateAt(t.pos)
		n = 1
		for _, k := range []string{"candles", "pickles", "eggs", "layers", "flower_amount", "segment_amount"} {
			if w, ok := ci.st.Props[k]; ok {
				n = atoiDef(w, 1) - atoiDef(have.Props[k], 0)
			}
		}
		if n < 1 {
			n = 1
		}
	}
	need[ci.item] += n
}

func sumNeeds(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

// reportMaterials dice en el log lo que hace falta, lo que hay y lo que falta.
func (b *buildJob) reportMaterials(need map[string]int) {
	have := b.j.Items()
	inChests := map[string]int{}
	for _, c := range b.index {
		for it, n := range c {
			inChests[it] += n
		}
	}
	type row struct {
		item              string
		need, have, chest int
	}
	var rows []row
	short := 0
	for it, n := range need {
		rw := row{it, n, have[it], inChests[it]}
		rows = append(rows, rw)
		if rw.have+rw.chest < rw.need {
			short++
		}
	}
	sort.Slice(rows, func(a, c int) bool { return rows[a].need > rows[c].need })
	var lines []string
	for _, rw := range rows {
		mark := ""
		if rw.have+rw.chest < rw.need {
			mark = fmt.Sprintf(" ← FALTAN %d", rw.need-rw.have-rw.chest)
		}
		lines = append(lines, fmt.Sprintf("%s %d (llevo %d, en cofres %d)%s", rw.item, rw.need, rw.have, rw.chest, mark))
	}
	b.j.Logf("materiales (%d tipos, %d bloques): %s", len(rows), sumNeeds(need), strings.Join(lines, "; "))
	if short > 0 {
		b.j.Logf("ojo: faltan materiales de %d tipos: construyo lo que pueda (mete lo que falta en los cofres y lo cojo en el siguiente viaje)", short)
	}
}

// maxStack es lo que cabe en un hueco del inventario.
func maxStack(item string) int {
	switch {
	case strings.HasSuffix(item, "_bed"), strings.HasSuffix(item, "bucket"), strings.HasSuffix(item, "shulker_box"),
		item == "cake":
		return 1
	case strings.HasSuffix(item, "_sign"), strings.HasSuffix(item, "_banner"), item == "egg", item == "snowball",
		item == "ender_pearl", item == "armor_stand", item == "honey_bottle":
		return 16
	}
	return 64
}

// chestsAround son los cofres a menos de r del sitio.
func (b *buildJob) chestsAround(c world.BlockPos, r int) []world.BlockPos {
	var out []world.BlockPos
	for dx := -r; dx <= r; dx++ {
		for dy := -4; dy <= 4; dy++ {
			for dz := -r; dz <= r; dz++ {
				q := world.BlockPos{X: c.X + dx, Y: c.Y + dy, Z: c.Z + dz}
				if b.p.Inside(schematic.Pos{X: q.X, Y: q.Y, Z: q.Z}) {
					if i := b.p.Index(schematic.Pos{X: q.X, Y: q.Y, Z: q.Z}); i >= 0 && isChest(b.p.Palette[i].Name) {
						continue // un cofre que es parte de la construcción
					}
				}
				if st, ok := b.j.r.d.World.BlockAt(q); ok && isChest(blockName(st)) {
					// de un cofre doble, una mitad basta
					dup := false
					for _, o := range out {
						if abs(o.X-q.X)+abs(o.Z-q.Z) == 1 && o.Y == q.Y && strings.Contains(world.StateString(st), "type=") && !strings.Contains(world.StateString(st), "type=single") {
							dup = true
						}
					}
					if !dup {
						out = append(out, q)
					}
				}
			}
		}
	}
	return out
}

// visitChest abre el cofre, apunta lo que tiene y coge lo de take (puede ser nil).
func (b *buildJob) visitChest(c world.BlockPos, take map[string]int) error {
	j := b.j
	if n := j.Block(c); n != "" && !isChest(n) {
		delete(b.index, c)
		return fmt.Errorf("ya no hay un cofre (hay %s)", n)
	}
	if err := j.Open(c); err != nil {
		return err
	}
	defer j.CloseScreen()
	read := func() (map[string]int, int) {
		m, ok := j.r.d.Screens.Open()
		if !ok {
			return nil, 0
		}
		in := map[string]int{}
		for i := 0; i < m.PlayerStart(); i++ {
			if sl := m.Slots[i]; sl.Count > 0 {
				in[strings.TrimPrefix(itemName(int32(sl.Item)), "minecraft:")] += int(sl.Count)
			}
		}
		return in, m.ID
	}
	in, id := read()
	if in == nil {
		return errors.New("no se abrió")
	}
	b.index[c] = in
	for item, want := range take {
		for want > 0 && j.FreeSlots() > 1 {
			m, ok := j.r.d.Screens.Open()
			if !ok || m.ID != id {
				return errors.New("el cofre se cerró")
			}
			slot, count := -1, 0
			for i := 0; i < m.PlayerStart(); i++ {
				if sl := m.Slots[i]; sl.Count > 0 && strings.TrimPrefix(itemName(int32(sl.Item)), "minecraft:") == item {
					slot, count = i, int(sl.Count)
					break
				}
			}
			if slot < 0 {
				break
			}
			before := j.Count(item)
			if count > want {
				if ok, err := j.takePart(id, slot, want); err != nil {
					return err
				} else if !ok {
					break
				}
			} else {
				if err := j.r.d.Screens.Click(id, slot, 0, types.ContainerInputQuickMove); err != nil {
					return err
				}
				if err := j.Wait(3); err != nil {
					return err
				}
			}
			got := j.Count(item) - before
			if got <= 0 {
				break // no cabe
			}
			want -= got
			take[item] = want
		}
	}
	in, _ = read()
	if in != nil {
		b.index[c] = in
	}
	return nil
}

// restock va a los cofres a por lo que hace falta para la tanda siguiente
// (lo de las próximas capas, hasta llenar el inventario); dice si cogió algo.
func (b *buildJob) restock(place []cellTask) bool {
	j := b.j
	if len(b.chests) == 0 {
		return false
	}
	// lo de abajo primero (es lo siguiente que se pone)
	feet := j.Feet()
	sort.Slice(place, func(a, c int) bool {
		if place[a].pos.Y != place[c].pos.Y {
			return place[a].pos.Y < place[c].pos.Y
		}
		return dist2(place[a].pos, feet) < dist2(place[c].pos, feet)
	})
	have := j.Items()
	slots := j.FreeSlots() - 2 // hueco para lo que se recoge
	want := map[string]int{}
	var order []string
	used := map[string]int{} // huecos que ya ocupa cada objeto pedido
	for _, t := range place {
		ci := b.info[t.idx]
		if ci.item == "" || b.isMissing(ci.item) {
			continue
		}
		need := map[string]int{}
		b.addNeed(need, t)
		n := need[ci.item]
		total := want[ci.item] + n
		extra := total - have[ci.item]
		if extra > 0 {
			st := maxStack(ci.item)
			slotsFor := (extra + st - 1) / st
			if slotsFor > used[ci.item] {
				if slots-(slotsFor-used[ci.item]) < 0 {
					break // lleno
				}
				slots -= slotsFor - used[ci.item]
				used[ci.item] = slotsFor
			}
		}
		if _, ok := want[ci.item]; !ok {
			order = append(order, ci.item)
		}
		want[ci.item] = total
	}
	take := map[string]int{}
	for _, it := range order {
		if n := want[it] - have[it]; n > 0 {
			take[it] = n
		}
	}
	// bloques para andamios (que no sean del schematic)
	if j.r.d.Builder != nil && j.r.d.Builder.Blocks() < scaffoldWant/2 {
		for _, c := range b.chests {
			for it, n := range b.index[c] {
				if n > 0 && !b.isMaterial(it) && pathing.IsBuildBlock(it) {
					take[it] = scaffoldWant
					break
				}
			}
			if len(take) > 0 {
				break
			}
		}
	}
	if len(take) == 0 {
		return false
	}
	var parts []string
	for it, n := range take {
		parts = append(parts, fmt.Sprintf("%s %d", it, n))
	}
	sort.Strings(parts)
	j.Status("a los cofres a por %s", strings.Join(parts, ", "))
	j.Logf("a los cofres a por: %s", strings.Join(parts, ", "))
	got := 0
	// los cofres que tienen algo de lo que falta (o que no se han mirado), del más cercano
	cs := append([]world.BlockPos(nil), b.chests...)
	sort.Slice(cs, func(a, c int) bool { return dist2(cs[a], j.Feet()) < dist2(cs[c], j.Feet()) })
	for _, c := range cs {
		useful := false
		in, seen := b.index[c]
		for it, n := range take {
			if n > 0 && (!seen || in[it] > 0) {
				useful = true
			}
		}
		if !useful {
			continue
		}
		before := j.Items()
		if err := b.visitChest(c, take); err != nil {
			if j.Fatal() != nil {
				return got > 0
			}
			j.Logf("cofre %v: %v", c, err)
			continue
		}
		after := j.Items()
		for it := range take {
			got += after[it] - before[it]
		}
		if j.FreeSlots() <= 1 {
			break
		}
	}
	// lo que no estaba en ningún cofre
	for it, n := range take {
		if n <= 0 || !b.isMaterial(it) {
			continue
		}
		total := 0
		for _, c := range b.index {
			total += c[it]
		}
		if total == 0 && j.Count(it) == 0 {
			b.missing[it] = time.Now()
			j.Logf("no hay %s en ningún cofre: lo dejo para luego", it)
		}
	}
	if got > 0 {
		for p, why := range b.skipped { // lo que se dejó por falta de material, otra vez
			if strings.HasPrefix(why, "falta ") {
				delete(b.skipped, p)
				delete(b.tries, p)
			}
		}
	}
	return got > 0
}

// needRestock: en la capa más baja que queda hay bloques cuyo material no
// lleva y puede estar en los cofres (antes de subir de capa, a por él).
func (b *buildJob) needRestock(place []cellTask) bool {
	if len(b.chests) == 0 || len(place) == 0 {
		return false
	}
	items := b.j.Items()
	minY := math.MaxInt
	for _, t := range place {
		if ci := b.info[t.idx]; ci.item != "" && !b.isMissing(ci.item) && t.pos.Y < minY {
			minY = t.pos.Y
		}
	}
	for _, t := range place {
		if t.pos.Y != minY {
			continue
		}
		if ci := b.info[t.idx]; ci.item != "" && items[ci.item] == 0 && !b.isMissing(ci.item) {
			return true
		}
	}
	return false
}

func (b *buildJob) isMissing(item string) bool {
	t, ok := b.missing[item]
	if ok && time.Since(t) > missingExpiry {
		delete(b.missing, item)
		return false
	}
	return ok
}

// makeRoom tira lo que sobra (bloques que no son del schematic, más allá de
// los del andamio) cuando el inventario se llena.
func (b *buildJob) makeRoom() {
	j := b.j
	if j.FreeSlots() > 2 {
		return
	}
	s := j.r.d.Screens
	s.Lock()
	type sl struct {
		i, c int
		n    string
	}
	var cands []sl
	for i := invStart; i <= invEnd; i++ {
		x := s.Inventory.Slots[i]
		if x.Count <= 0 {
			continue
		}
		n := strings.TrimPrefix(itemName(int32(x.Item)), "minecraft:")
		if b.isMaterial(n) {
			continue
		}
		if _, isBlock := block.FromID["minecraft:"+n]; !isBlock {
			continue // herramientas, comida, armadura: no
		}
		cands = append(cands, sl{i, int(x.Count), n})
	}
	s.Unlock()
	// se quedan los dos montones de andamio más grandes
	sort.Slice(cands, func(a, c int) bool { return cands[a].c > cands[c].c })
	var throw []junkSlot
	kept := 0
	for _, c := range cands {
		if pathing.IsBuildBlock(c.n) && kept < 2 {
			kept++
			continue
		}
		throw = append(throw, junkSlot{i: c.i, n: c.n, c: c.c})
	}
	if len(throw) == 0 {
		return
	}
	j.CloseScreen()
	n := j.throwSlots(throw, nil)
	ignore := map[string]bool{}
	for _, t := range throw {
		ignore[t.n] = true
	}
	prev := j.ignore
	j.ignore = func(it string) bool { return ignore[it] || prev != nil && prev(it) }
	j.Logf("inventario lleno: tiro %d bloques que no hacen falta", n)
}

// ---- el bucle ----

func (b *buildJob) loop() error {
	j := b.j
	best, stall := -1, 0
	unloadedTrips := 0
	for {
		if err := j.Wait(1); err != nil {
			return err
		}
		r := b.scan()
		pct := 100.0
		if r.total > 0 {
			pct = 100 * float64(r.done) / float64(r.total)
		}
		j.Status("construyendo %s: %d/%d (%.0f %%), quitar %d, poner %d", b.opts.Name, r.done, r.total, pct, len(r.clear), len(r.place)+len(r.fix))
		progress := r.done*4 - len(r.clear) + len(b.skipped) + b.attempts
		if progress > best {
			best, stall = progress, 0
		} else if stall++; stall > 80 {
			return b.finish(r, "llevo un rato sin avanzar")
		}
		clear := b.live(r.clear)
		place := b.live(r.place)
		fix := b.live(r.fix)
		if len(clear) == 0 && len(place) == 0 && len(fix) == 0 {
			if len(b.skipped) > 0 && b.retries < 2 && len(r.place)+len(r.clear)+len(r.fix) > 0 {
				// lo descartado pudo depender de lo que se puso después: otra vuelta
				b.retries++
				j.Logf("vuelvo a intentar %d bloques que dejé (vuelta %d)", len(b.skipped), b.retries)
				for p, why := range b.skipped {
					if strings.HasPrefix(why, "falta ") {
						continue
					}
					delete(b.skipped, p)
					delete(b.tries, p)
				}
				for k := range b.badSpot {
					delete(b.badSpot, k)
				}
				for k := range b.noReach {
					delete(b.noReach, k)
				}
				continue
			}
			if len(r.unknown) > 0 && unloadedTrips < 20 {
				// lejos: sin cargar. A verlo de cerca
				unloadedTrips++
				t := nearest(r.unknown, j.Feet())
				j.Status("voy hacia %v (no lo tengo cargado)", t)
				if err := j.GoNear(t); err != nil && j.Fatal() != nil {
					return j.Fatal()
				}
				continue
			}
			if !b.finishing && b.hasScaffold() {
				b.finishing = true
				j.Logf("todo puesto: quito los andamios")
				continue
			}
			return b.finish(r, "")
		}
		b.makeRoom()
		switch {
		case len(clear) > 0:
			b.doClear(clear)
		case len(fix) > 0:
			b.doFix(fix[0])
		default:
			if b.needRestock(place) && b.restock(place) {
				continue
			}
			if !b.doPlace(place) {
				// nada se puede poner ahora: si la tanda pedía materiales, a por ellos
				if !b.restock(place) {
					if !b.finishing && b.hasScaffold() {
						b.finishing = true
						continue
					}
					for _, t := range place {
						if _, ok := b.skipped[t.pos]; !ok {
							b.skipped[t.pos] = b.whyNot(t)
						}
					}
				}
			}
		}
		if j.Fatal() != nil {
			return j.Fatal()
		}
	}
}

// live quita las tareas ya descartadas.
func (b *buildJob) live(ts []cellTask) []cellTask {
	out := ts[:0:0]
	for _, t := range ts {
		if _, skip := b.skipped[t.pos]; !skip {
			out = append(out, t)
		}
	}
	return out
}

func (b *buildJob) hasScaffold() bool {
	if len(b.supports) > 0 {
		return true
	}
	if bd := b.j.r.d.Builder; bd != nil {
		for _, q := range bd.Placed() {
			if !b.before[q] {
				return true
			}
		}
	}
	return false
}

func dist2(a, c world.BlockPos) int {
	dx, dy, dz := a.X-c.X, a.Y-c.Y, a.Z-c.Z
	return dx*dx + dy*dy + dz*dz
}

func nearest(ps []world.BlockPos, to world.BlockPos) world.BlockPos {
	best := ps[0]
	for _, p := range ps[1:] {
		if dist2(p, to) < dist2(best, to) {
			best = p
		}
	}
	return best
}

// doClear quita el bloque que sobra más alto (y, de esos, el más cercano).
func (b *buildJob) doClear(ts []cellTask) {
	j := b.j
	feet := j.Feet()
	sort.Slice(ts, func(a, c int) bool {
		if ts[a].pos.Y != ts[c].pos.Y {
			return ts[a].pos.Y > ts[c].pos.Y
		}
		return dist2(ts[a].pos, feet) < dist2(ts[c].pos, feet)
	})
	// de lo más alto, lo que ya se alcanza primero
	t := ts[0]
	for _, c := range ts {
		if c.pos.Y != t.pos.Y {
			break
		}
		if j.Reaches(c.pos) {
			t = c
			break
		}
	}
	have := j.Block(t.pos)
	j.Status("quitando %s de %v", have, t.pos)
	err := j.GoNear(t.pos)
	if err == nil {
		err = j.Dig(t.pos)
	}
	if err != nil {
		if j.Fatal() != nil {
			return
		}
		b.tries[t.pos]++
		b.attempts++
		if b.tries[t.pos] >= buildTries {
			b.skipped[t.pos] = fmt.Sprintf("no pude quitar %s: %v", have, err)
		}
		return
	}
	b.dug++
	if b.supports[t.pos] {
		delete(b.supports, t.pos)
	}
	if bd := j.r.d.Builder; bd != nil {
		bd.Forget(t.pos)
	}
}

// doFix pulsa el bloque hasta que tenga las propiedades pedidas (abierto, retardo, nota…).
func (b *buildJob) doFix(t cellTask) {
	j := b.j
	want := b.info[t.idx].st
	if err := j.GoNear(t.pos); err != nil {
		b.fail(t, fmt.Sprintf("no llego: %v", err))
		return
	}
	for clicks := 0; clicks < 30; clicks++ {
		have, _, ok := b.stateAt(t.pos)
		if !ok || b.matches(want, have) || !b.fixable(want, have) {
			return
		}
		j.Status("ajustando %s en %v", want.Name, t.pos)
		done := make(chan act.Use, 1)
		j.r.d.Walker.Stop()
		j.r.d.Ctl.SetInput(control.Input{})
		j.r.d.Hands.UseBlock(t.pos, func(u act.Use) { done <- u })
		select {
		case u := <-done:
			if u.Err != nil {
				b.fail(t, fmt.Sprintf("no cambia al pulsarlo: %v", u.Err))
				return
			}
		case <-time.After(5 * time.Second):
			b.fail(t, "no contesta al pulsarlo")
			return
		case <-j.ctx.Done():
			return
		}
		if err := j.Wait(2); err != nil {
			return
		}
	}
}

func (b *buildJob) fail(t cellTask, why string) {
	b.tries[t.pos]++
	b.attempts++
	if b.tries[t.pos] >= buildTries {
		b.skipped[t.pos] = why
		b.j.Logf("dejo %s en %v: %s", b.info[t.idx].st, t.pos, why)
	}
}

// whyNot dice por qué no se puede poner (para el resumen).
func (b *buildJob) whyNot(t cellTask) string {
	ci := b.info[t.idx]
	if ci.item != "" && b.j.Count(ci.item) == 0 {
		return "falta " + ci.item
	}
	if ci.below {
		if s, _, ok := b.stateAt(world.BlockPos{X: t.pos.X, Y: t.pos.Y - 1, Z: t.pos.Z}); ok && s.IsAir() {
			return "no tiene nada debajo"
		}
	}
	return "no encuentro desde dónde ponerlo"
}

// ready: se puede intentar ya (hay material, tiene debajo lo que necesita, hay dónde pinchar).
func (b *buildJob) ready(t cellTask, items map[string]int) bool {
	ci := b.info[t.idx]
	if ci.item == "" || items[ci.item] <= 0 {
		return false
	}
	if ci.below && t.kind == taskPlace {
		st, ok := b.j.r.d.World.BlockAt(world.BlockPos{X: t.pos.X, Y: t.pos.Y - 1, Z: t.pos.Z})
		if !ok || block.Replaceable(st) {
			return false
		}
	}
	if t.kind == taskMore {
		return true
	}
	// los de dos celdas: la otra libre
	if ci.st.Props["half"] == "lower" {
		if st, ok := b.j.r.d.World.BlockAt(world.BlockPos{X: t.pos.X, Y: t.pos.Y + 1, Z: t.pos.Z}); !ok || !block.Replaceable(st) {
			return false
		}
	}
	if ci.st.Props["part"] == "foot" {
		if f, ok := faceOf(ci.st.Props["facing"]); ok {
			o := faceDir[f]
			if st, ok := b.j.r.d.World.BlockAt(world.BlockPos{X: t.pos.X + o[0], Y: t.pos.Y, Z: t.pos.Z + o[2]}); !ok || !block.Replaceable(st) {
				return false
			}
		}
	}
	// los que van pegados a un bloque concreto (antorcha de pared, escalera de
	// mano, farol colgado): ese bloque tiene que estar
	if f, ok := faceOf(ci.st.Props["facing"]); ok && !b.st.loose(ci.key+"|facing") {
		if rs := b.rulesFor(t); len(rs) > 0 && (rs[0] == ruleFace || rs[0] == ruleOppFace) {
			if rs[0] == ruleOppFace {
				f = oppFace(f)
			}
			o := faceDir[f]
			st, ok := b.j.r.d.World.BlockAt(world.BlockPos{X: t.pos.X - o[0], Y: t.pos.Y - o[1], Z: t.pos.Z - o[2]})
			return ok && len(block.CollisionShape(st)) > 0 && !block.Replaceable(st)
		}
	}
	// (sin nada contra lo que pinchar, goPlace le pone un apoyo)
	return true
}

// needsFor son las condiciones del clic para la celda (con lo aprendido).
func (b *buildJob) needsFor(t cellTask, rule facingRule) clickNeeds {
	ci := b.info[t.idx]
	n := clickNeeds{want: ci.st, rule: rule, solidT: ci.solid, onSelf: t.kind == taskMore}
	if _, ok := ci.st.Props["facing"]; ok && !b.st.loose(ci.key+"|facing") {
		n.hasRule = true
	}
	if _, ok := ci.st.Props["rotation"]; ok {
		n.rotOff = b.st.rotOff(ci.key)
		if b.st.loose(ci.key + "|rotation") {
			w := schematic.State{Name: ci.st.Name, Props: map[string]string{}}
			for k, v := range ci.st.Props {
				if k != "rotation" {
					w.Props[k] = v
				}
			}
			n.want = w
		}
	}
	return n
}

func (b *buildJob) eye() vec3 {
	p := b.j.Pos()
	return vec3{p.X, p.Y + 1.62, p.Z}
}

// doPlace pone el siguiente bloque: de la capa más baja que queda, el que se
// pueda poner desde aquí; si ninguno, va al sitio desde el que se pone el más
// cercano. false: no hay ninguno listo (faltan materiales o apoyos).
func (b *buildJob) doPlace(ts []cellTask) bool {
	j := b.j
	items := j.Items()
	var readyTs []cellTask
	minY := math.MaxInt
	for _, t := range ts {
		if b.ready(t, items) {
			readyTs = append(readyTs, t)
			if t.pos.Y < minY {
				minY = t.pos.Y
			}
		}
	}
	if len(readyTs) == 0 {
		return false
	}
	// la capa más baja con algo listo, y la de encima si se alcanza desde aquí
	eye := b.eye()
	feet := j.Feet()
	var here []cellTask
	var layer []cellTask
	for _, t := range readyTs {
		if t.pos.Y == minY {
			layer = append(layer, t)
		}
		if t.pos.Y <= minY+1 && dist2(t.pos, feet) <= 36 {
			here = append(here, t)
		}
	}
	// desde aquí
	type cand struct {
		t    cellTask
		plan clickPlan
		rule facingRule
	}
	var bestC *cand
	for _, t := range here {
		if b.inBodyCell(t) {
			continue
		}
		rule := b.ruleFor(t)
		if plan, ok := planClick(j.r.d.World, t.pos, b.needsFor(t, rule), eye, buildReach); ok {
			score := plan.score + float64(t.pos.Y-minY)*3
			if bestC == nil || score < bestC.plan.score {
				plan.score = score
				bestC = &cand{t, plan, rule}
			}
		}
	}
	if bestC != nil {
		b.placeWith(bestC.t, bestC.plan, bestC.rule)
		return true
	}
	// a otro sitio: el más cercano de la capa
	sort.Slice(layer, func(a, c int) bool { return dist2(layer[a].pos, feet) < dist2(layer[c].pos, feet) })
	for k, t := range layer {
		if k >= 6 {
			break
		}
		if b.goPlace(t) {
			return true
		}
		if j.Fatal() != nil {
			return true
		}
	}
	// ninguno de esos: el resto de la capa, en la próxima vuelta (a estos les cuenta el intento)
	return len(layer) > 0
}

// inBodyCell: el bloque tiene colisión y el bot está en su celda.
func (b *buildJob) inBodyCell(t cellTask) bool {
	return b.info[t.idx].solid && t.kind == taskPlace && b.j.inBody(t.pos)
}

// ruleFor es la regla de orientación con la que se planea: la más probable de
// las que quedan (planear con otra sería dar por cierta una que no se sabe).
func (b *buildJob) ruleFor(t cellTask) facingRule { return b.rulesFor(t)[0] }

func (b *buildJob) rulesFor(t cellTask) []facingRule {
	ci := b.info[t.idx]
	if _, ok := ci.st.Props["facing"]; !ok || b.st.loose(ci.key+"|facing") {
		return []facingRule{ruleOpp}
	}
	return b.st.rules(ci.key, defaultRules(ci.st.Name, ci.st.Props))
}

// goPlace busca un sitio desde el que poner t, va y lo pone. false: no hay sitio.
func (b *buildJob) goPlace(t cellTask) bool {
	j := b.j
	rule := b.ruleFor(t)
	spots := b.standSpots(t, rule)
	if len(spots) == 0 {
		// sin nada contra lo que pincharlo como pide (una losa de arriba en el
		// aire, un bloque que flota): un apoyo de andamio al lado
		if b.support(t, rule) {
			return true
		}
		b.fail(t, "no encuentro desde dónde ponerlo")
		return false
	}
	for i, s := range spots {
		if i >= 5 {
			break
		}
		if s.scaffold {
			b.waitBuilder()
		}
		j.Status("voy a %v para poner %s en %v", s.feet, b.info[t.idx].st.Name, t.pos)
		if err := j.GoTo(s.feet); err != nil {
			if j.Fatal() != nil {
				return true
			}
			b.badSpot[[2]world.BlockPos{t.pos, s.feet}] = true
			b.noReach[s.feet]++
			j.Logf("para %v: no llego a %v (andamio %v): %v", t.pos, s.feet, s.scaffold, err)
			continue
		}
		if plan, ok := planClick(j.r.d.World, t.pos, b.needsFor(t, rule), b.eye(), buildReach); ok && !b.inBodyCell(t) {
			b.placeWith(t, plan, rule)
			return true
		}
		b.badSpot[[2]world.BlockPos{t.pos, s.feet}] = true
	}
	b.fail(t, "desde ningún sitio llego a ponerlo bien")
	return true
}

type standSpot struct {
	feet     world.BlockPos
	score    float64
	scaffold bool // hay que subirse con andamio
}

// waitBuilder: si el constructor del walker está en pausa (el servidor le
// rechazó bloques seguidos), espera a que vuelva (un minuto como mucho) en
// vez de gastar los intentos sin poder subirse.
func (b *buildJob) waitBuilder() {
	bd := b.j.r.d.Builder
	if bd == nil || !bd.Paused() {
		return
	}
	b.j.Status("esperando para poder volver a poner andamios")
	for i := 0; i < 70*10 && bd.Paused(); i++ {
		if b.j.Wait(2) != nil {
			return
		}
	}
}

// standSpots son los sitios desde los que se puede poner t (los pies), del
// mejor al peor: cerca, con suelo (o con poco andamio debajo), sin estorbar
// lo que queda por poner.
func (b *buildJob) standSpots(t cellTask, rule facingRule) []standSpot {
	j := b.j
	w := j.r.d.World
	feet := j.Feet()
	free := func(p world.BlockPos) bool {
		st, ok := w.BlockAt(p)
		if !ok {
			return false
		}
		n := blockName(st)
		return len(block.CollisionShape(st)) == 0 && n != "lava" && n != "fire" && n != "water"
	}
	var out []standSpot
	for dy := -3; dy <= 2; dy++ {
		for dx := -4; dx <= 4; dx++ {
			for dz := -4; dz <= 4; dz++ {
				s := world.BlockPos{X: t.pos.X + dx, Y: t.pos.Y + dy, Z: t.pos.Z + dz}
				if b.badSpot[[2]world.BlockPos{t.pos, s}] || b.noReach[s] >= 2 {
					continue
				}
				head := world.BlockPos{X: s.X, Y: s.Y + 1, Z: s.Z}
				if (s == t.pos || head == t.pos) && b.info[t.idx].solid {
					continue
				}
				if !free(s) || !free(head) {
					continue
				}
				eye := vec3{float64(s.X) + 0.5, float64(s.Y) + 1.62, float64(s.Z) + 0.5}
				if _, ok := planClick(w, t.pos, b.needsFor(t, rule), eye, buildReach-0.3); !ok {
					continue
				}
				score := math.Sqrt(float64(dist2(s, feet))) + 25*float64(b.noReach[s])
				scaffold := false
				// el suelo: si no hay, andamio (más caro cuanto más alto)
				under := world.BlockPos{X: s.X, Y: s.Y - 1, Z: s.Z}
				if st, ok := w.BlockAt(under); !ok || len(block.CollisionShape(st)) == 0 {
					h := 0
					for ; h < 10; h++ {
						q := world.BlockPos{X: s.X, Y: s.Y - 2 - h, Z: s.Z}
						if st, ok := w.BlockAt(q); !ok || len(block.CollisionShape(st)) > 0 {
							break
						}
					}
					if h >= 10 {
						continue
					}
					score += 8 + 3*float64(h)
					scaffold = true
					if b.p.Inside(schematic.Pos{X: s.X, Y: b.p.Min.Y, Z: s.Z}) {
						score += 10 // una torre dentro de la obra estorba (mejor fuera)
					}
					if b.wantsBlock(under) {
						score += 6 // el andamio iría donde va un bloque
					}
				}
				// estorbar: los pies o la cabeza donde aún va un bloque
				if b.wantsBlock(s) || b.wantsBlock(head) {
					score += 12
				}
				out = append(out, standSpot{s, score, scaffold})
			}
		}
	}
	sort.Slice(out, func(a, c int) bool { return out[a].score < out[c].score })
	return out
}

// wantsBlock: el schematic pone un bloque (no aire) en p y aún no está.
func (b *buildJob) wantsBlock(p world.BlockPos) bool {
	i := b.p.Index(schematic.Pos{X: p.X, Y: p.Y, Z: p.Z})
	if i < 0 || b.p.Palette[i].IsAir() {
		return false
	}
	have, _, ok := b.stateAt(p)
	return !ok || !b.matches(b.p.Palette[i], have)
}

// placeWith pone el bloque con el clic planeado y mira cómo quedó: si no
// está bien, aprende (cómo sale la orientación) y lo quita para volver a
// probar.
func (b *buildJob) placeWith(t cellTask, plan clickPlan, rule facingRule) {
	j := b.j
	ci := b.info[t.idx]
	j.Status("poniendo %s en %v", ci.st.Name, t.pos)
	if err := j.Hold(ci.item); err != nil {
		b.fail(t, err.Error())
		return
	}
	j.r.d.Walker.Stop()
	j.r.d.Ctl.SetInput(control.Input{})
	if err := j.Wait(1); err != nil {
		return
	}
	// desde donde está ahora (Hold tardó): sigue valiendo el clic?
	if p2, ok := planClick(j.r.d.World, t.pos, b.needsFor(t, rule), b.eye(), buildReach+0.05); ok {
		plan = p2
	}
	done := make(chan act.Use, 1)
	cb := func(u act.Use) { done <- u }
	if plan.onSelf {
		j.r.d.Hands.UseBlockAt(t.pos, plan.face, plan.cursor, cb)
	} else {
		j.r.d.Hands.PlaceOnAt(t.pos, plan.against, plan.cursor, cb)
	}
	var useErr error
	select {
	case u := <-done:
		useErr = u.Err
	case <-time.After(10 * time.Second):
		useErr = errors.New("el servidor no contesta")
	case <-j.ctx.Done():
		return
	}
	if err := j.Wait(2); err != nil {
		return
	}
	have, _, ok := b.stateAt(t.pos)
	if !ok {
		return
	}
	if b.matches(ci.st, have) || have.Name == ci.st.Name && b.moreNeeded(ci.st, have) {
		b.placed++
		if b.placed%25 == 0 {
			j.Logf("llevo %d bloques puestos", b.placed)
		}
		return
	}
	if have.IsAir() || have.Name != ci.st.Name {
		why := "el servidor no lo puso"
		if useErr != nil {
			why = useErr.Error()
		}
		if !have.IsAir() && !block.Replaceable(b.mustState(t.pos)) {
			why = fmt.Sprintf("salió %s en vez de %s", have.Name, ci.st.Name)
			if b.tries[t.pos]+1 < buildTries {
				_ = j.Dig(t.pos)
			}
		}
		b.fail(t, why)
		return
	}
	// el bloque bueno con alguna propiedad mal: aprender
	b.learn(t, plan, rule, have)
	if b.matches(ci.st, have) {
		return // (era algo que ya no cuenta)
	}
	if b.fixable(ci.st, have) {
		return // se arregla pulsándolo (la próxima vuelta)
	}
	b.tries[t.pos]++
	b.attempts++
	if b.tries[t.pos] >= buildTries {
		b.skipped[t.pos] = fmt.Sprintf("queda %s en vez de %s", have, ci.st)
		j.Logf("dejo %v como %s (pedía %s)", t.pos, have, ci.st)
		return
	}
	j.Logf("%v salió %s (pedía %s): lo quito y pruebo otra vez", t.pos, have, ci.st)
	_ = j.Dig(t.pos)
}

func (b *buildJob) mustState(p world.BlockPos) block.StateID {
	st, _ := b.j.r.d.World.BlockAt(p)
	return st
}

// learn: con el bloque puesto mal, descarta las reglas de orientación que no
// explican lo que salió (y la rotación de los carteles sale directa).
func (b *buildJob) learn(t cellTask, plan clickPlan, rule facingRule, have schematic.State) {
	ci := b.info[t.idx]
	if wf, ok := ci.st.Props["facing"]; ok && have.Props["facing"] != wf {
		got, ok := faceOf(have.Props["facing"])
		if ok {
			old := b.rulesFor(t)
			var keep []facingRule
			for _, r := range old {
				if predictFacing(r, plan.look4, plan.look6, plan.face) == got {
					keep = append(keep, r)
				}
			}
			if len(keep) == 0 {
				b.j.Logf("%s: no entiendo cómo sale su orientación (salió %s): no la miro más", ci.key, have.Props["facing"])
				b.st.setLoose(ci.key + "|facing")
			} else {
				b.j.Logf("%s: la orientación sale con la regla «%s» (no «%s»)", ci.key, keep[0], rule)
				b.st.setRules(ci.key, keep)
			}
		}
	}
	if wr, ok := ci.st.Props["rotation"]; ok && have.Props["rotation"] != wr {
		look := vec3{float64(plan.against.X) + float64(plan.cursor[0]) - b.eye()[0], 0, float64(plan.against.Z) + float64(plan.cursor[2]) - b.eye()[2]}
		seg, _ := yawSegment(look)
		got := atoiDef(have.Props["rotation"], -1)
		if got >= 0 {
			off := (got - seg) & 15
			if off == b.st.rotOff(ci.key) {
				b.st.setLoose(ci.key + "|rotation")
			} else {
				b.st.setRotOff(ci.key, off)
				b.j.Logf("%s: la rotación sale con +%d", ci.key, off)
			}
		}
	}
}

// support pone un bloque de andamio junto a t para poder pincharlo como pide
// (en una celda que el schematic deja de aire o que no es suya; se quita al
// final). true: lo puso (la próxima vuelta se pone t).
func (b *buildJob) support(t cellTask, rule facingRule) bool {
	j := b.j
	w := j.r.d.World
	item := b.scaffoldItem()
	if item == "" {
		return false
	}
	n := b.needsFor(t, rule)
	type cand struct {
		cell world.BlockPos
		d    int
	}
	var cands []cand
	for fi := 0; fi < 6; fi++ {
		f := act.Face(fi)
		if !faceAllowed(n.want.Name, n.want.Props, f) {
			continue
		}
		o := faceDir[f]
		c := world.BlockPos{X: t.pos.X - o[0], Y: t.pos.Y - o[1], Z: t.pos.Z - o[2]}
		st, ok := w.BlockAt(c)
		if !ok || !block.Replaceable(st) || b.wantsBlock(c) || j.inBody(c) || b.j.r.Store.IsProtected(c) {
			continue
		}
		// algo junto a la celda del apoyo contra lo que ponerlo
		has := false
		for _, o2 := range faceDir {
			q := world.BlockPos{X: c.X + o2[0], Y: c.Y + o2[1], Z: c.Z + o2[2]}
			if q == t.pos {
				continue
			}
			if st, ok := w.BlockAt(q); ok && len(block.CollisionShape(st)) > 0 && !block.Replaceable(st) {
				has = true
			}
		}
		if !has {
			continue
		}
		// con el apoyo puesto, ¿habría desde dónde?
		sim := overlay{w, c, b.scaffoldState(item)}
		ok2 := false
		for dy := -3; dy <= 2 && !ok2; dy++ {
			for dx := -4; dx <= 4 && !ok2; dx++ {
				for dz := -4; dz <= 4 && !ok2; dz++ {
					eye := vec3{float64(t.pos.X+dx) + 0.5, float64(t.pos.Y+dy) + 1.62, float64(t.pos.Z+dz) + 0.5}
					if _, ok := planClick(sim, t.pos, n, eye, buildReach-0.3); ok {
						ok2 = true
					}
				}
			}
		}
		if ok2 {
			cands = append(cands, cand{c, dist2(c, j.Feet())})
		}
	}
	if len(cands) == 0 {
		return false
	}
	sort.Slice(cands, func(a, c int) bool { return cands[a].d < cands[c].d })
	c := cands[0].cell
	j.Status("pongo un apoyo en %v para %s", c, n.want.Name)
	if err := j.GoNear(c); err != nil {
		return false
	}
	if err := j.Place(item, c); err != nil {
		j.Logf("apoyo en %v: %v", c, err)
		return false
	}
	b.supports[c] = true
	j.Logf("apoyo de %s en %v para poner %s en %v (lo quito al final)", item, c, n.want, t.pos)
	return true
}

// scaffoldItem es el bloque de andamio que lleva ("" ninguno): de construir y no del schematic.
func (b *buildJob) scaffoldItem() string {
	best, n := "", 0
	for it, c := range b.j.Items() {
		if pathing.IsBuildBlock(it) && !b.isMaterial(it) && c > n {
			best, n = it, c
		}
	}
	return best
}

func (b *buildJob) scaffoldState(item string) block.StateID {
	if bl, ok := block.FromID["minecraft:"+item]; ok {
		if st, ok := block.ToStateID[bl]; ok {
			return st
		}
	}
	return block.ToStateID[block.FromID["minecraft:dirt"]]
}

// overlay es el mundo con un bloque más (para planear con un apoyo que aún no está).
type overlay struct {
	w    blockSource
	at   world.BlockPos
	with block.StateID
}

func (o overlay) BlockAt(p world.BlockPos) (block.StateID, bool) {
	if p == o.at {
		return o.with, true
	}
	return o.w.BlockAt(p)
}

// ---- el final ----

// finish quita los andamios que quedan fuera del schematic y dice cómo quedó.
func (b *buildJob) finish(r scanResult, why string) error {
	j := b.j
	b.removeScaffold()
	r = b.scan()
	pct := 100.0
	if r.total > 0 {
		pct = 100 * float64(r.done) / float64(r.total)
	}
	reasons := map[string]int{}
	for _, t := range append(append(r.place, r.clear...), r.fix...) {
		if s, ok := b.skipped[t.pos]; ok {
			reasons[s]++
		} else {
			reasons[b.whyNot(t)]++
		}
	}
	for n, c := range r.unsupported {
		reasons["no sé poner "+n]++
		_ = c
	}
	type kv struct {
		k string
		v int
	}
	var list []kv
	for k, v := range reasons {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(a, c int) bool { return list[a].v > list[c].v })
	var parts []string
	for i, e := range list {
		if i >= 8 {
			break
		}
		parts = append(parts, fmt.Sprintf("%d × %s", e.v, e.k))
	}
	summary := fmt.Sprintf("%s: %d/%d bloques bien (%.1f %%); puse %d, quité %d", b.opts.Name, r.done, r.total, pct, b.placed, b.dug)
	if r.done == r.total && len(r.clear) == 0 && len(r.fix) == 0 && len(r.unsupported) == 0 {
		j.Logf("¡construcción terminada! %s", summary)
		return nil
	}
	if why != "" {
		summary += "; " + why
	}
	if len(parts) > 0 {
		summary += ". Pendiente: " + strings.Join(parts, "; ")
	}
	j.Logf("construcción sin terminar: %s", summary)
	return errors.New(summary)
}

// removeScaffold quita los bloques que puso para subir (los de debajo del bot primero).
func (b *buildJob) removeScaffold() {
	j := b.j
	d := j.r.d
	var cells []world.BlockPos
	for q := range b.supports {
		cells = append(cells, q)
	}
	if d.Builder != nil {
		for _, q := range d.Builder.Placed() {
			if !b.before[q] && !b.supports[q] {
				cells = append(cells, q)
			}
		}
	}
	if len(cells) == 0 {
		return
	}
	// sin hacer más torres mientras
	if d.Walker != nil {
		d.Walker.SetBuild(false)
		defer d.Walker.SetBuild(true)
	}
	sort.Slice(cells, func(a, c int) bool { return cells[a].Y > cells[c].Y })
	removed := 0
	for _, q := range cells {
		if j.Fatal() != nil {
			return
		}
		i := b.p.Index(schematic.Pos{X: q.X, Y: q.Y, Z: q.Z})
		if i >= 0 && !b.p.Palette[i].IsAir() {
			// dentro, donde va un bloque: si el andamio sigue ahí, lo arregla el bucle
			if d.Builder != nil {
				d.Builder.Forget(q)
			}
			delete(b.supports, q)
			continue
		}
		st, ok := d.World.BlockAt(q)
		if !ok || !pathing.IsBuildBlock(blockName(st)) {
			if d.Builder != nil {
				d.Builder.Forget(q)
			}
			delete(b.supports, q)
			continue
		}
		j.Status("quitando el andamio de %v", q)
		err := j.GoNear(q)
		if err == nil {
			err = j.Dig(q)
		}
		if err != nil {
			j.Logf("andamio en %v: no lo quito (%v)", q, err)
		}
		if err == nil {
			removed++
		}
		if d.Builder != nil {
			d.Builder.Forget(q)
		}
		delete(b.supports, q)
	}
	if removed > 0 {
		j.Logf("andamios quitados: %d", removed)
	}
}

// ---- comprobar sin construir (/build check) ----

// BuildCheck compara el schematic colocado con el mundo (lo cargado): cuántos
// bloques están ya bien, cuántos mal, cuántos sin cargar, y (si hay algo
// puesto) qué otra rotación o espejo encajaría mejor.
type BuildCheck struct {
	Total, Good, Wrong, Missing, Unloaded, Extra int
	Unsupported                                  map[string]int
	Needs                                        map[string]int
}

var checkMu sync.Mutex

// CheckBuild compara sin moverse (lo que el bot tiene cargado).
func CheckBuild(w blockSource, p *schematic.Placed) BuildCheck {
	checkMu.Lock()
	defer checkMu.Unlock()
	c := BuildCheck{Unsupported: map[string]int{}, Needs: map[string]int{}}
	cache := map[block.StateID]schematic.State{}
	p.Each(func(sp schematic.Pos, idx int32) {
		want := p.Palette[idx]
		st, ok := w.BlockAt(toPos(sp))
		if !ok {
			if !want.IsAir() {
				c.Unloaded++
			}
			return
		}
		have, ok := cache[st]
		if !ok {
			have = schematic.ParseState(world.StateString(st))
			cache[st] = have
		}
		if want.IsAir() {
			if !have.IsAir() && !block.Replaceable(st) {
				c.Extra++
			}
			return
		}
		item, n, okItem := schematic.Item(want)
		if !okItem {
			c.Unsupported[want.Name]++
			return
		}
		c.Total++
		switch {
		case schematic.Matches(want, have) || item == "" && have.Name == want.Name:
			c.Good++
		case have.IsAir() || block.Replaceable(st):
			c.Missing++
			if item != "" {
				c.Needs[item] += n
			}
		default:
			c.Wrong++
			if item != "" {
				c.Needs[item] += n
			}
		}
	})
	return c
}

// FormatNeeds escribe una lista de materiales (de más a menos).
func FormatNeeds(m map[string]int) string {
	type kv struct {
		k string
		v int
	}
	var l []kv
	for k, v := range m {
		l = append(l, kv{k, v})
	}
	sort.Slice(l, func(a, c int) bool {
		if l[a].v != l[c].v {
			return l[a].v > l[c].v
		}
		return l[a].k < l[c].k
	})
	var parts []string
	for _, e := range l {
		parts = append(parts, e.k+" "+strconv.Itoa(e.v))
	}
	return strings.Join(parts, ", ")
}
