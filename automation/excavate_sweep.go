package automation

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/world"
)

// El barrido de /excavate: la caja se vacía en pasadas de hasta 4 capas
// desde arriba y, dentro de cada pasada, capa a capa en toda la caja (el
// terreno baja parejo: no franjas de 4 de hondo una al lado de otra), y cada
// pasada en filas a lo largo de x, en zigzag (al acabar una fila, la
// siguiente de vuelta). Cuántas pasadas y de cuántas capas sale del alto de
// la caja (passPlan): las menos posibles, repartidas (10 de alto: 4, 3 y 3).
//
//   - Normal: filas de 3 de ancho; en cada paso pica la columna de delante
//     (cabeza y pies) y las de los lados (z±1), y avanza una celda.
//   - Corriendo: si con lo que lleva (Haste, un pico de diamante o
//     netherita con Efficiency…) los bloques se rompen al instante, filas de 1
//     de ancho sin pararse: corre hacia delante y rompe lo que tiene enfrente
//     según llega (de lado no: el cuerpo va hacia donde mira).
//
// Lo que el barrido no pudo (agua, una cueva sin suelo, algo que no se dejó)
// lo hace después la limpieza (run: el bloque más alto que quede).

// errSlow: el bloque de delante no se rompe al instante: se sigue andando.
var errSlow = errors.New("no se rompe al instante")

// passMax: el alto de una pasada (lo que el jugador llama "una capa": 4
// bloques). Cada pasada se vacía entera en toda la caja antes de bajar a la
// siguiente: la caja baja de 4 en 4, pareja. Desde el fondo, el bot alcanza
// los 4 de alto (el de arriba, 1,9 por encima de los ojos).
const passMax = 4

// passPlan son las pasadas de la caja, de arriba abajo: la capa de abajo de
// cada una (donde anda el bot) y cuántas tiene. Las menos posibles (de
// passMax), con las capas repartidas: las de arriba con una más si no es exacto.
func passPlan(box Box) [][2]int {
	h := box.Max.Y - box.Min.Y + 1
	n := (h + passMax - 1) / passMax
	var out [][2]int
	top := box.Max.Y
	for i := 0; i < n; i++ {
		size := h / n
		if i < h%n {
			size++
		}
		out = append(out, [2]int{top - size + 1, size})
		top -= size
	}
	return out
}

// passTries: las veces que se repite una pasada (bloque a bloque) para dejarla vacía.
const passTries = 3

// sweep hace las pasadas, de arriba abajo. Una pasada tiene que quedar
// vacía del todo antes de bajar a la siguiente (lo que no se puede cavar,
// como el agua o lo que falló excavateFails veces, no cuenta): si no lo
// consigue, se para diciendo por qué (no deja capas a medias encima).
func (e *excavation) sweep() error {
	j := e.j
	plan := passPlan(e.box)
	defer func() { e.h = passMax }()
	for k, ps := range plan {
		L, h := ps[0], ps[1]
		// sin ver la caja entera, sus bloques parecen aire: primero, que se carguen
		if err := e.ensureLoaded(); err != nil {
			return err
		}
		// la pasada entera (h de alto) en toda la caja antes de bajar: la caja
		// baja h bloques pareja
		e.h = h
		if err := e.layer(L, k, len(plan), L, L+h-1); err != nil {
			return err
		}
		j.Logf("excavar: capas %d-%d listas (pasada %d de %d)", L, L+h-1, k+1, len(plan))
		// ahora, con las capas listas, lo que cayó (antes no: primero cavar)
		e.collectPass(L, L+h-1)
	}
	return nil
}

// layer vacía la capa y en toda la caja (e.h = 1): la barre fila a fila y
// repite bloque a bloque lo que quede (passTries veces). Si no queda vacía,
// para (no se baja a la siguiente con esta a medias).
func (e *excavation) layer(y, k, passes, lo, hi int) error {
	j := e.j
	var last error
	for try := 0; try < passTries && e.passWork(y) > 0; try++ {
		var err error
		if try == 0 {
			// sin pico y con piedra en la pasada: a por uno antes (no correr por
			// la capa picando piedra con la mano)
			if e.needsPick(y) && j.bestPickSlot(pickReserve) < 0 {
				if err := e.restockPicks(); err != nil {
					return fmt.Errorf("%w: %v", errNoPicksLeft, err)
				}
			}
			e.fastPass(y) // (dice con qué y a qué ritmo se pica)
			j.Status("excavando: pasada %d de %d (capas %d-%d)", k+1, passes, lo, hi)
			err = e.layerReach(y) // por alcance: desde cada sitio, toda la altura de la pasada a mano
		} else {
			j.Logf("excavar: a las capas %d-%d les quedan %d bloques: otra vez, bloque a bloque (%d de %d)", lo, hi, e.passWork(y), try+1, passTries)
			err = e.passCleanup(y)
		}
		if j.Fatal() != nil {
			return j.Fatal()
		}
		if errors.Is(err, errNoRoom) || errors.Is(err, errNoPicksLeft) {
			return err
		}
		if err != nil {
			last = err
			j.Problem("excavar: capas %d-%d: %v", lo, hi, err)
		}
	}
	if left := e.passWork(y); left > 0 {
		why := ""
		if last != nil {
			why = ": " + last.Error()
		}
		return fmt.Errorf("las capas %d-%d no quedan vacías (faltan %d bloques%s); no bajo sin acabarlas", lo, hi, left, why)
	}
	for cy := hi; cy >= lo; cy-- {
		j.Logf("excavar: capa %d lista: %s", cy, e.layerReport(cy))
	}
	return nil
}

// noteDug apunta un bloque cavado (para layerReport: hasta dónde llegó en cada capa).
func (e *excavation) noteDug(c world.BlockPos) {
	if e.extent == nil {
		e.extent = map[int]*[5]int{}
	}
	x := e.extent[c.Y]
	if x == nil {
		e.extent[c.Y] = &[5]int{c.X, c.X, c.Z, c.Z, 1}
		return
	}
	x[0], x[1], x[2], x[3], x[4] = min(x[0], c.X), max(x[1], c.X), min(x[2], c.Z), max(x[3], c.Z), x[4]+1
}

// layerReport dice lo de una capa acabada: cuánto cavó y entre qué x y z, y
// lo sólido que queda en ella sin contar como "por cavar" (y por qué): para
// ver en el log si quedó algo y qué.
func (e *excavation) layerReport(y int) string {
	j := e.j
	out := "no cavé nada en ella (era aire)"
	if x := e.extent[y]; x != nil {
		out = fmt.Sprintf("cavé %d bloques, de x %d a %d y de z %d a %d (la caja: x %d a %d, z %d a %d)",
			x[4], x[0], x[1], x[2], x[3], e.box.Min.X, e.box.Max.X, e.box.Min.Z, e.box.Max.Z)
	}
	why := map[string]int{}
	for x := e.box.Min.X; x <= e.box.Max.X; x++ {
		for z := e.box.Min.Z; z <= e.box.Max.Z; z++ {
			p := world.BlockPos{X: x, Y: y, Z: z}
			n := j.Block(p)
			switch {
			case n == "":
				why["sin cargar"]++
			case n == "air" || n == "cave_air" || n == "void_air" || !j.solid(p) && !j.fluidBlock(p):
			case j.fluidBlock(p):
				why[n]++
			case j.protected(p):
				why["protegido: "+n]++
			case e.fails[p] >= excavateFails:
				why["no se dejó: "+n]++
			case noExcavate(n):
				why["no se toca: "+n]++
			default:
				why["otro: "+n]++
			}
		}
	}
	if len(why) > 0 {
		var l []string
		for k, n := range why {
			l = append(l, fmt.Sprintf("%d %s", n, k))
		}
		sort.Strings(l)
		out += "; quedan sin cavar: " + strings.Join(l, ", ")
	}
	return out
}

// collectPass recoge lo que quedó en el suelo de las capas lo…hi de la caja
// (toda la pasada, no solo lo de cerca). Se hace con la pasada lista: al
// cavar no se para a recoger.
func (e *excavation) collectPass(lo, hi int) {
	j := e.j
	b := e.box
	cx, cz := float64(b.Min.X+b.Max.X)/2+0.5, float64(b.Min.Z+b.Max.Z)/2+0.5
	r := math.Hypot(float64(b.Max.X-b.Min.X+1), float64(b.Max.Z-b.Min.Z+1))/2 + 2
	j.Status("excavar: recogiendo lo de las capas %d-%d", lo, hi)
	// solo lo de dentro de la caja, a la altura de la pasada (no ir por el
	// terreno de fuera, sin cavar, a por lo que rodó lejos)
	j.collectOnly = func(c world.BlockPos) bool {
		return c.X >= b.Min.X-1 && c.X <= b.Max.X+1 && c.Z >= b.Min.Z-1 && c.Z <= b.Max.Z+1 && c.Y >= lo-1 && c.Y <= hi+1
	}
	defer func() { j.collectOnly = nil }()
	if err := j.CollectAround(cx, float64(lo), cz, r); err != nil && j.Fatal() == nil {
		j.Problem("excavar: recoger: %v", err)
	}
}

// passTop es la capa de arriba de la pasada que anda por L.
func (e *excavation) passTop(L int) int { return min(L+e.h-1, e.box.Max.Y) }

// passWork: cuántos bloques hay que cavar en las capas de la pasada de L.
func (e *excavation) passWork(L int) int {
	n := 0
	for y := L; y <= e.passTop(L); y++ {
		for x := e.box.Min.X; x <= e.box.Max.X; x++ {
			for z := e.box.Min.Z; z <= e.box.Max.Z; z++ {
				if e.diggable(world.BlockPos{X: x, Y: y, Z: z}) {
					n++
				}
			}
		}
	}
	return n
}

// instant: con la mejor herramienta que lleva, p se rompe al primer golpe
// (lo que cuenta el juego: herramienta, Efficiency, Haste, Mining Fatigue, el
// agua, estar en el suelo). Se pone esa herramienta en la mano.
func (e *excavation) instant(p world.BlockPos) bool {
	j := e.j
	st, ok := j.r.d.World.BlockAt(p)
	if !ok {
		return false
	}
	j.HoldBestTool(blockName(st))
	cs := j.r.d.Ctl.State()
	cs.OnGround = true // (lo que cuenta es picando en el suelo, que es como pica)
	return j.r.d.Hands.DestroyProgress(st, &cs, j.r.d.Player) >= 1
}

// fastPass decide si la pasada L va corriendo: los bloques que más hay en
// ella se rompen al instante.
func (e *excavation) fastPass(L int) bool {
	j := e.j
	count := map[string]int{}
	sample := map[string]world.BlockPos{}
	for y := L; y <= e.passTop(L); y++ {
		for x := e.box.Min.X; x <= e.box.Max.X; x++ {
			for z := e.box.Min.Z; z <= e.box.Max.Z; z++ {
				p := world.BlockPos{X: x, Y: y, Z: z}
				if e.diggable(p) {
					n := j.Block(p)
					count[n]++
					sample[n] = p
				}
			}
		}
	}
	most, best := "", 0
	for n, c := range count {
		if c > best {
			most, best = n, c
		}
	}
	if most == "" {
		return false
	}
	fast := e.instant(sample[most])
	if fast != e.wasFast || !e.toldMode {
		e.toldMode = true
		tool := orHand(j.Holding())
		if eff := e.heldEfficiency(); eff > 0 {
			tool += fmt.Sprintf(" Efficiency %s", roman(eff))
		}
		how := fmt.Sprintf("%d ticks por bloque", e.ticksFor(sample[most]))
		if fast {
			how = "al instante (un bloque por tick)"
		}
		j.Logf("excavar: capas %d-%d: sobre todo %s, con %s y %s: %s", L, e.passTop(L), most, tool, e.hasteDesc(), how)
	}
	e.wasFast = fast
	return fast
}

// heldEfficiency: el nivel de Efficiency de lo que tiene en la mano.
func (e *excavation) heldEfficiency() int {
	s := e.j.r.d.Screens
	s.Lock()
	held := s.Inventory.Slots[hotbar+s.HeldSlot]
	s.Unlock()
	return e.j.r.d.Hands.EnchantLevel(held, "minecraft:efficiency")
}

// ticksFor: lo que tarda en romper p con lo que tiene en la mano (en el suelo).
func (e *excavation) ticksFor(p world.BlockPos) int {
	j := e.j
	st, ok := j.r.d.World.BlockAt(p)
	if !ok {
		return 0
	}
	cs := j.r.d.Ctl.State()
	cs.OnGround = true
	step := j.r.d.Hands.DestroyProgress(st, &cs, j.r.d.Player)
	if step <= 0 {
		return 0
	}
	return int(math.Ceil(float64(1 / step)))
}

func orHand(item string) string {
	if item == "" || item == "air" {
		return "la mano"
	}
	return item
}

// hasteDesc dice los efectos que cambian lo que tarda en picar.
func (e *excavation) hasteDesc() string {
	st := e.j.r.d.Player.Status()
	out := "sin Haste"
	if h, ok := st.Effects["minecraft:haste"]; ok {
		out = fmt.Sprintf("Haste %s", roman(h.Amplifier+1))
	} else if c, ok := st.Effects["minecraft:conduit_power"]; ok {
		out = fmt.Sprintf("Conduit Power %s", roman(c.Amplifier+1))
	}
	if f, ok := st.Effects["minecraft:mining_fatigue"]; ok {
		out += fmt.Sprintf(", Mining Fatigue %s", roman(f.Amplifier+1))
	}
	return out
}

func roman(n int) string {
	if n >= 1 && n <= 5 {
		return []string{"I", "II", "III", "IV", "V"}[n-1]
	}
	return fmt.Sprint(n)
}

// rowWidth: lo ancha que es una fila (el bot por el centro y una a cada
// lado). Lo que cae a un bloque del cuerpo se recoge solo al pasar: con
// filas más anchas, lo de los lados (a 2) se queda en el suelo y luego hay
// que ir a por cada cosa (era más de la mitad del tiempo).
const rowWidth = 3

// stride: cuánto avanza de una parada a la siguiente (lo que pica por delante en cada una).
const stride = 3

// rows son los centros de las filas de la pasada (z), cada rowWidth, desde el
// lado más cercano al bot.
func (e *excavation) rows(width int) []int {
	var zs []int
	half := width / 2
	for z := e.box.Min.Z + half; z-half <= e.box.Max.Z; z += width {
		zs = append(zs, min(z, e.box.Max.Z))
	}
	f := e.j.Feet()
	if abs(f.Z-e.box.Max.Z) < abs(f.Z-e.box.Min.Z) {
		for i, k := 0, len(zs)-1; i < k; i, k = i+1, k-1 {
			zs[i], zs[k] = zs[k], zs[i]
		}
	}
	return zs
}

// pass hace la pasada de las capas de L a passTop(L): fila a fila donde hay suelo, y
// luego, ahí mismo, lo que quedó de esas dos capas bloque a bloque (antes de
// bajar: nada se queda flotando encima de lo cavado). Una pasada con poco
// (la cima de una colina, unos bloques sueltos) va directamente bloque a
// bloque: barrerla sería andar por el aire.
func (e *excavation) pass(L int, fast bool) error {
	j := e.j
	width := rowWidth
	area := (e.box.Max.X - e.box.Min.X + 1) * (e.box.Max.Z - e.box.Min.Z + 1) * (e.passTop(L) - L + 1)
	work := e.passWork(L)
	if work*4 < area {
		j.Logf("excavar: capas %d-%d: solo %d bloques sueltos de %d: bloque a bloque, sin barrer", L, e.passTop(L), work, area)
		return e.passCleanup(L)
	}
	failed, skipped := 0, 0
	for _, zc := range e.rows(width) {
		lo, hi, ok := e.rowSpan(L, zc, width)
		if !ok {
			continue
		}
		// la fila empieza por el extremo más cercano al bot (el zigzag sale
		// solo), si ahí hay suelo; si no, por el otro; si en ninguno, la limpieza
		xa, xb := lo, hi
		if f := j.Feet(); abs(f.X-xb) < abs(f.X-xa) {
			xa, xb = xb, xa
		}
		if !e.standable(world.BlockPos{X: xa, Y: L, Z: zc}) {
			xa, xb = xb, xa
			if !e.standable(world.BlockPos{X: xa, Y: L, Z: zc}) {
				skipped++
				continue
			}
		}
		j.Status("excavando: capas %d-%d, fila z=%d (%d→%d)%s (llevo %d)", L, e.passTop(L), zc, xa, xb,
			map[bool]string{true: " corriendo", false: ""}[fast], e.dug)
		err := e.row(L, zc, width, xa, xb, fast)
		switch {
		case err == nil:
			failed = 0
		case j.Fatal() != nil || errors.Is(err, errNoRoom) || errors.Is(err, errNoPicksLeft):
			return err
		case errors.Is(err, errNoFloor):
			skipped++
		default:
			j.Problem("excavar: fila z=%d de las capas %d-%d: %v", zc, L, e.passTop(L), err)
			if failed++; failed >= 2 {
				j.Problem("excavar: dos filas seguidas sin poder entrar: el resto de las capas %d-%d, bloque a bloque", L, e.passTop(L))
				return e.passCleanup(L)
			}
		}
	}
	if skipped > 0 {
		j.Logf("excavar: capas %d-%d: %d filas (o tramos) sin suelo donde pisar: bloque a bloque", L, e.passTop(L), skipped)
	}
	return e.passCleanup(L)
}

// passCleanup cava lo que quede de las capas de la pasada, bloque a bloque.
func (e *excavation) passCleanup(L int) error {
	e.top, e.low = e.passTop(L), L
	defer func() { e.low = e.box.Min.Y }()
	before := e.dug
	err := e.run()
	if n := e.dug - before; n > 0 {
		e.j.Logf("excavar: capas %d-%d: %d bloque a bloque", L, e.passTop(L), n)
	}
	return err
}

// errNoFloor: delante no hay suelo (aire, agua): la fila acaba ahí (lo demás, bloque a bloque).
var errNoFloor = errors.New("sin suelo donde pisar")

// standable: en p se puede estar al cavar su fila: hay suelo firme debajo
// (lo de p y encima se cava al entrar).
func (e *excavation) standable(p world.BlockPos) bool {
	floor := world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z}
	return e.j.solid(floor) && !e.j.liquid(floor)
}

// rowSpan: el tramo de x de la fila con algo que cavar.
func (e *excavation) rowSpan(L, zc, width int) (lo, hi int, ok bool) {
	for x := e.box.Min.X; x <= e.box.Max.X; x++ {
		for _, c := range e.column(x, L, zc, width) {
			if e.diggable(c) {
				if !ok {
					lo, ok = x, true
				}
				hi = x
				break
			}
		}
	}
	return lo, hi, ok
}

// rowWork: los bloques por cavar de la fila (z de zc-w/2 a zc+w/2).
func (e *excavation) rowWork(L, zc, width int) int {
	n := 0
	for x := e.box.Min.X; x <= e.box.Max.X; x++ {
		for _, c := range e.column(x, L, zc, width) {
			if e.diggable(c) {
				n++
			}
		}
	}
	return n
}

// column son las celdas de la caja de la columna x de la fila: el centro
// (las capas de la pasada: encima de la cabeza, cabeza y pies) primero, luego los lados; de arriba abajo (lo que caiga, a
// lo que aún no se cavó).
func (e *excavation) column(x, L, zc, width int) []world.BlockPos {
	zs := []int{zc}
	for d := 1; d <= width/2; d++ {
		zs = append(zs, zc-d, zc+d)
	}
	var out []world.BlockPos
	for _, z := range zs {
		for y := e.passTop(L); y >= L; y-- {
			if c := (world.BlockPos{X: x, Y: y, Z: z}); e.box.Contains(c) {
				out = append(out, c)
			}
		}
	}
	return out
}

// row barre la fila zc de xa a xb como un jugador: en cada parada, quieto,
// pica de un tirón todo lo de la fila que alcanza por delante (stride
// columnas, rowWidth de ancho, todas las capas de la pasada; de lo más cerca
// a lo más lejos, sin soltar el botón) y luego avanza esas columnas andando
// derecho por el pasillo abierto (sin buscar camino).
func (e *excavation) row(L, zc, width, xa, xb int, fast bool) error {
	j := e.j
	dir := 1
	if xb < xa {
		dir = -1
	}
	start := world.BlockPos{X: xa, Y: L, Z: zc}
	if err := e.enter(start); err != nil {
		return fmt.Errorf("entrar en la fila por %v: %w", start, err)
	}
	x := xa
	for {
		if err := j.Wait(0); err != nil {
			return err
		}
		here := world.BlockPos{X: x, Y: L, Z: zc}
		if j.FreeSlots() < 3 { // lleno: a guardar, y de vuelta aquí
			if err := e.stash(false); err != nil {
				return err
			}
			if err := e.enter(here); err != nil {
				return err
			}
		}
		// lo de esta parada: desde la columna donde está hasta stride por delante
		last := x + dir*stride
		if (last-xb)*dir > 0 {
			last = xb
		}
		var cells []world.BlockPos
		for cx := x; ; cx += dir {
			cells = append(cells, e.column(cx, L, zc, width)...)
			if cx == last {
				break
			}
		}
		if err := e.digBatch(cells); err != nil {
			return err
		}
		if x == xb {
			return nil
		}
		// avanzar: lo más lejos que se pueda hasta last (suelo y paso abiertos)
		to := x
		for cx := x + dir; (cx-last)*dir <= 0; cx += dir {
			n := world.BlockPos{X: cx, Y: L, Z: zc}
			if !e.standable(n) || j.solid(n) || j.solid(world.BlockPos{X: cx, Y: L + 1, Z: zc}) {
				break
			}
			to = cx
		}
		if to == x {
			n := world.BlockPos{X: x + dir, Y: L, Z: zc}
			if !e.standable(n) {
				return errNoFloor
			}
			return fmt.Errorf("no puedo abrir el paso a %v (%s)", n, j.Block(n))
		}
		if err := e.walkTo(world.BlockPos{X: to, Y: L, Z: zc}); err != nil {
			return err
		}
		x = to
	}
}

// walkTo anda derecho hasta la celda p por un pasillo ya abierto (en línea
// recta, sin buscar camino: corriendo y frenando al llegar). Si no llega
// (algo en medio), con el pathfinding.
func (e *excavation) walkTo(p world.BlockPos) error {
	j := e.j
	ctl := j.r.d.Ctl
	j.r.d.Walker.Stop()
	defer ctl.SetInput(control.Input{})
	cx, cz := float64(p.X)+0.5, float64(p.Z)+0.5
	best, still := math.Inf(1), 0
	for t := 0; t < 100; t++ {
		pos := j.Pos()
		d := math.Hypot(cx-pos.X, cz-pos.Z)
		if d < 0.35 && j.Feet() == p {
			ctl.SetInput(control.Input{})
			// que frene dentro (lo que patina con la velocidad que llevaba)
			for k := 0; k < 3; k++ {
				if err := j.Wait(1); err != nil {
					return err
				}
			}
			return nil
		}
		act.LookAt(j.r.d.Player, false, cx, pos.Y+1.62, cz)
		in := control.Input{Forward: true, Sprint: d > 1.5}
		if d < 0.8 { // cerca: sin correr, que no se pase
			in.Sprint = false
		}
		ctl.SetInput(in)
		if d < best-0.02 {
			best, still = d, 0
		} else if still++; still > 10 {
			break // (algo en medio)
		}
		if err := j.Wait(1); err != nil {
			return err
		}
	}
	ctl.SetInput(control.Input{})
	return j.GoTo(p)
}

// enter lleva al bot a la celda p de una fila (los pies en p): si está justo
// encima, primero se aparta; luego abre p (cabeza y pies) y entra.
func (e *excavation) enter(p world.BlockPos) error {
	j := e.j
	if j.Feet() == p {
		return nil
	}
	if f := j.Feet(); f.Y == p.Y && e.box.Contains(f) {
		return e.walkDig(p) // (de una fila a otra: abriendo el pasillo)
	}
	if f := j.Feet(); f.X == p.X && f.Z == p.Z && f.Y > p.Y {
		if err := e.stepAside(); err != nil {
			return err
		}
	}
	for _, c := range []world.BlockPos{{X: p.X, Y: p.Y + 1, Z: p.Z}, p} {
		if (j.solid(c) || j.fluidBlock(c)) && e.box.Contains(c) {
			if err := e.cell(c, true); err != nil {
				return err
			}
		}
	}
	return j.GoTo(p)
}

// walkDig va a p por la misma capa, celda a celda (primero en z, luego en
// x), abriendo cabeza y pies de cada una: de una fila a la siguiente.
func (e *excavation) walkDig(p world.BlockPos) error {
	j := e.j
	for steps := 0; j.Feet() != p; steps++ {
		if steps > 2*(e.box.Max.X-e.box.Min.X+e.box.Max.Z-e.box.Min.Z)+8 {
			return fmt.Errorf("no llego a %v por la capa", p)
		}
		f := j.Feet()
		n := f
		if f.Z != p.Z {
			n.Z += sign(p.Z - f.Z)
		} else {
			n.X += sign(p.X - f.X)
		}
		for _, c := range []world.BlockPos{{X: n.X, Y: n.Y + 1, Z: n.Z}, n} {
			if j.solid(c) && e.diggable(c) {
				if err := e.cell(c, true); err != nil {
					return err
				}
			}
		}
		if j.solid(n) || j.solid(world.BlockPos{X: n.X, Y: n.Y + 1, Z: n.Z}) {
			return fmt.Errorf("no puedo abrir el paso a %v (%s)", n, j.Block(n))
		}
		if err := j.GoTo(n); err != nil {
			return err
		}
	}
	return nil
}

// digBatch cava de un tirón (DigChain: sin pausas entre bloque y bloque)
// las celdas de cs que haya que cavar y se alcancen desde donde está. La
// que soltaría agua o lava, antes y sola (se tapa). Si encima de alguna había
// grava, espera a que caiga y la cava también. Error solo si hay que parar.
func (e *excavation) digBatch(cs []world.BlockPos) error {
	j := e.j
	pending := func() []world.BlockPos {
		var out []world.BlockPos
		for _, c := range cs {
			if e.diggable(c) && j.canDig(c) {
				out = append(out, c)
			}
		}
		return out
	}
	// (cada vuelta pica lo que ya tiene una cara al aire; al quitarlo, se
	// destapa lo de detrás para la siguiente)
	for tries := 0; tries < 48; tries++ {
		var todo []world.BlockPos
		for _, c := range pending() {
			if !j.safeToOpen(c) {
				if err := e.cell(c, false); err != nil {
					return err
				}
				continue
			}
			todo = append(todo, c)
		}
		if len(todo) == 0 {
			break
		}
		// por herramienta (cambiarla es una pausa) y, en cada una, de lo más
		// cerca a lo más lejos (lo de cerca destapa lo de detrás)
		sort.SliceStable(todo, func(a, b int) bool {
			ka, kb := toolFor(j.Block(todo[a])), toolFor(j.Block(todo[b]))
			if ka != kb {
				return ka < kb
			}
			return j.eyeDist(todo[a]) < j.eyeDist(todo[b])
		})
		if toolFor(j.Block(todo[0])) == "pickaxe" && j.bestPickSlot(pickReserve) < 0 {
			if err := e.restockPicks(); err != nil {
				return fmt.Errorf("%w: %v", errNoPicksLeft, err)
			}
		}
		gravel := false
		for _, c := range todo {
			gravel = gravel || j.fallsAbove(c)
		}
		names := make(map[world.BlockPos]string, len(todo))
		for _, c := range todo {
			names[c] = j.Block(c)
		}
		broke, err := j.DigChain(todo)
		e.dug += len(broke)
		for c := range broke {
			e.noteDug(c)
			e.noteBroken(c, names[c])
		}
		if err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			if errors.Is(err, ErrNoPicks) {
				if err := e.restockPicks(); err != nil {
					return fmt.Errorf("%w: %v", errNoPicksLeft, err)
				}
				continue
			}
			if errors.Is(err, errDigLost) {
				continue // (no es culpa de los bloques: otra vez)
			}
		}
		for _, c := range todo {
			if !broke[c] && e.diggable(c) {
				if e.fails[c]++; e.fails[c] == excavateFails {
					j.Problem("excavar: dejo %s en %v", j.Block(c), c)
				}
			}
		}
		if gravel {
			if err := j.Wait(4); err != nil { // (la grava tarda en caer: luego, también)
				return err
			}
		}
	}
	j.plugExposed(2)
	return nil
}

// digHere cava c si hay que cavarlo y se alcanza desde donde está (sin
// moverse: lo que no, para la limpieza). Error solo si la excavación tiene
// que pararse (sin picos, sin sitio, /stop).
func (e *excavation) digHere(c world.BlockPos) error {
	if !e.diggable(c) || !e.j.canDig(c) {
		return nil
	}
	return e.cell(c, false)
}

// errNoRoom y errNoPicksLeft paran la excavación (no son de un bloque).
var (
	errNoRoom      = errors.New("no tengo dónde guardar")
	errNoPicksLeft = errors.New("sin picos")
)

// cell cava el bloque c (con move, acercándose si hace falta): picos si
// faltan, los fallos contados, lo cavado sumado. Error solo si hay que parar.
func (e *excavation) cell(c world.BlockPos, move bool) error {
	j := e.j
	name := j.Block(c)
	if toolFor(name) == "pickaxe" && j.bestPickSlot(pickReserve) < 0 {
		if err := e.restockPicks(); err != nil {
			return fmt.Errorf("%w: %v", errNoPicksLeft, err)
		}
	}
	var err error
	if move {
		err = e.dig(c)
	} else {
		err = e.digNoMove(c)
	}
	if err != nil {
		if j.Fatal() != nil {
			return j.Fatal()
		}
		if errors.Is(err, ErrNoPicks) {
			if err := e.restockPicks(); err != nil {
				return fmt.Errorf("%w: %v", errNoPicksLeft, err)
			}
			return nil
		}
		if errors.Is(err, errDigLost) {
			return nil // (otra vez luego: no es culpa del bloque)
		}
		if e.dropOldProtected(c, err) {
			return nil
		}
		e.fails[c]++
		if e.fails[c] == excavateFails {
			j.Problem("excavar: dejo %s en %v: %v", name, c, err)
		}
		if move {
			return err
		}
		return nil
	}
	e.dug++
	e.noteBroken(c, name)
	return nil
}

// digNoMove cava c desde donde está (tapando antes la lava que entraría).
func (e *excavation) digNoMove(c world.BlockPos) error {
	j := e.j
	if !j.makeSafe(c) {
		return fmt.Errorf("agua o lava junto a %v", c)
	}
	for tries := 0; tries < 6 && e.diggable(c); tries++ { // (otra vez si cae grava o arena)
		gravel := j.fallsAbove(c)
		if err := j.Dig(c); err != nil {
			return err
		}
		if !gravel {
			break
		}
		if err := j.Wait(4); err != nil { // (la grava tarda en caer)
			return err
		}
	}
	j.plugExposed(2)
	return nil
}

// needsPick: en la pasada que anda por L hay algo que cavar que pide pico.
func (e *excavation) needsPick(L int) bool {
	for y := L; y <= e.passTop(L); y++ {
		for x := e.box.Min.X; x <= e.box.Max.X; x++ {
			for z := e.box.Min.Z; z <= e.box.Max.Z; z++ {
				p := world.BlockPos{X: x, Y: y, Z: z}
				if e.diggable(p) && toolFor(e.j.Block(p)) == "pickaxe" {
					return true
				}
			}
		}
	}
	return false
}
