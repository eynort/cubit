package automation

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/protocol/types"
)

// /excavate x1 y1 z1 x2 y2 z2: vaciar una caja de bloques entera, capa a capa
// de arriba abajo (nunca se cava bajo los pies: se baja por lo ya cavado).
//
// La herramienta, la de cada bloque (tierra, pala; madera, hacha; piedra,
// pico; hojas, azada) del mejor material que lleve; sin ella, la mano.
//
// Los picos: los que lleve y los del cofre de minar (/mine chest) o los
// cofres de picos del almacén, sin romper ninguno (con pickReserve usos se
// dejan de usar y se devuelven al cofre). Sin pico que valga, se para.
//
// Lo cavado: cuando el inventario se llena, al cofre de minar de la
// dimensión; si no hay o se llena, al almacén (que pone cofres nuevos según
// hagan falta). Se queda un montón de bloques para tapar lava y subir.

// Box es una caja de bloques (las dos esquinas incluidas).
type Box struct{ Min, Max world.BlockPos }

// NewBox es la caja entre dos esquinas cualesquiera.
func NewBox(a, b world.BlockPos) Box {
	return Box{
		Min: world.BlockPos{X: min(a.X, b.X), Y: min(a.Y, b.Y), Z: min(a.Z, b.Z)},
		Max: world.BlockPos{X: max(a.X, b.X), Y: max(a.Y, b.Y), Z: max(a.Z, b.Z)},
	}
}

// Contains: p está dentro de la caja.
func (b Box) Contains(p world.BlockPos) bool {
	return p.X >= b.Min.X && p.X <= b.Max.X && p.Y >= b.Min.Y && p.Y <= b.Max.Y && p.Z >= b.Min.Z && p.Z <= b.Max.Z
}

// Volume son los bloques de la caja.
func (b Box) Volume() int {
	return (b.Max.X - b.Min.X + 1) * (b.Max.Y - b.Min.Y + 1) * (b.Max.Z - b.Min.Z + 1)
}

func (b Box) String() string { return fmt.Sprintf("%v → %v", b.Min, b.Max) }

// excavateProtectR: a cuántos bloques de donde el servidor dijo "no tienes
// permiso" (mientras se excava) se deja de cavar: 0, solo esa celda. El aviso
// llega con el bot donde está (casi siempre en la parte libre, junto al
// borde): un radio dejaba sin cavar bloques libres. Lo que no se deja quitar
// se ve igual: vuelve a aparecer (checkRevert).
const excavateProtectR = 0

// protectedColumns: cuántas columnas de la caja caen en zonas protegidas apuntadas.
func protectedColumns(box Box, st *Storage) int {
	n := 0
	for x := box.Min.X; x <= box.Max.X; x++ {
		for z := box.Min.Z; z <= box.Max.Z; z++ {
			if st.IsProtected(world.BlockPos{X: x, Y: box.Max.Y, Z: z}) {
				n++
			}
		}
	}
	return n
}

// newlyProtected: los bloques sin cavar de la caja que se saltaron porque el
// servidor dijo que no durante esta excavación.
func (e *excavation) newlyProtected() int {
	j := e.j
	n := 0
	for y := e.box.Min.Y; y <= e.box.Max.Y; y++ {
		for x := e.box.Min.X; x <= e.box.Max.X; x++ {
			for z := e.box.Min.Z; z <= e.box.Max.Z; z++ {
				p := world.BlockPos{X: x, Y: y, Z: z}
				if j.solid(p) && j.protected(p) {
					n++
				}
			}
		}
	}
	return n
}

// revertMax: las veces que un bloque puede volver a aparecer después de
// picarlo (el servidor lo repone: un terreno reclamado, el spawn) antes de
// dejarlo y seguir con lo demás.
const revertMax = 2

// noteBroken apunta que c se rompió (era el bloque name): si vuelve a
// aparecer, el servidor no dejó (checkRevert).
func (e *excavation) noteBroken(c world.BlockPos, name string) {
	if e.broken == nil {
		e.broken = map[world.BlockPos]string{}
	}
	e.broken[c] = name
}

// checkRevert: c, que se rompió, vuelve a estar (el mismo bloque n): el
// servidor lo repuso. No cuenta como cavado; a revertMax veces se deja (y
// es su columna (los terrenos reclamados van de arriba abajo), no la zona de
// alrededor, que puede ser libre).
func (e *excavation) checkRevert(c world.BlockPos, n string) {
	was, ok := e.broken[c]
	if !ok || n != was {
		return
	}
	delete(e.broken, c)
	if strings.Contains(n, "sand") || strings.Contains(n, "gravel") || strings.HasSuffix(n, "concrete_powder") {
		return // (lo que cae: otro bloque que bajó, no uno devuelto)
	}
	j := e.j
	e.dug--
	if e.reverts == nil {
		e.reverts = map[world.BlockPos]int{}
	}
	e.reverts[c]++
	if e.reverts[c] < revertMax {
		return
	}
	// los terrenos reclamados (y el spawn) son columnas enteras: lo de encima
	// y debajo de este, dentro de la caja, tampoco se deja (sin ir a probarlo)
	for y := e.box.Min.Y; y <= e.box.Max.Y; y++ {
		e.fails[world.BlockPos{X: c.X, Y: y, Z: c.Z}] = excavateFails
	}
	e.revertsLeft++
	if e.revertsLeft == 1 { // (uno solo en el log; al final, cuántos)
		j.Problem("excavar: el servidor no me deja quitar %s en %v (vuelve a aparecer): dejo esa columna de la caja y sigo con lo demás", n, c)
	}
}

// dropOldProtected: un bloque no se dejó cavar y el servidor dijo que no
// (un "no tienes permiso" hace poco, o el error de zona protegida): se deja
// su columna entera ya, al primer fallo (no al tercero: cada intento en un
// terreno reclamado es ir y volver para nada). Las columnas de al lado no:
// las zonas apuntadas son anchas y pueden tapar terreno libre, que sí hay
// que cavar. Dice si dejó la columna.
func (e *excavation) dropOldProtected(c world.BlockPos, err error) bool {
	if !errors.Is(err, ErrProtected) && !e.j.deniedSince(time.Now().Add(-30*time.Second)) {
		return false
	}
	for y := e.box.Min.Y; y <= e.box.Max.Y; y++ {
		e.fails[world.BlockPos{X: c.X, Y: y, Z: c.Z}] = excavateFails
	}
	if e.deniedCols++; e.deniedCols == 1 {
		e.j.Problem("excavar: %s en %v no se deja (%v, y el servidor dice que no tengo permiso): dejo su columna; las que estén igual, también al primer intento", e.j.Block(c), c, err)
	}
	return true
}

// cantClimb: c está por encima del alcance y el servidor no deja construir
// (sin torres, el pathfinding no sube): ir andando es buscar un camino que
// no hay.
func (e *excavation) cantClimb(c world.BlockPos) bool {
	b := e.j.r.d.Builder
	return b != nil && b.Paused() && c.Y > e.j.Feet().Y+2
}

// ExcavateMax: más bloques que estos es un error al escribir las coordenadas.
const ExcavateMax = 256 * 256 * 64

// excavateFails: un bloque que falla tantas veces se deja (y se dice al acabar).
const excavateFails = 3

// toolReserve: con estos usos o menos una pala, hacha o azada ya no se usa
// (se guarda, para repararla): se cava con la mano.
const toolReserve = 10

// excavateKeep: los bloques de relleno que se queda al guardar (tapar lava, torres para salir).
const excavateKeep = 64

// noExcavate: lo que no se rompe nunca (no se puede, o tiene cosas dentro).
func noExcavate(n string) bool {
	switch n {
	case "", "air", "cave_air", "void_air", "bedrock", "barrier", "light", "end_portal", "end_portal_frame",
		"end_gateway", "nether_portal", "reinforced_deepslate", "structure_block", "structure_void", "jigsaw",
		"command_block", "chain_command_block", "repeating_command_block", "fire", "soul_fire",
		"chest", "trapped_chest", "barrel", "ender_chest", "furnace", "blast_furnace", "smoker", "hopper",
		"dispenser", "dropper", "brewing_stand", "water", "lava", "bubble_column":
		return true
	}
	return strings.HasSuffix(n, "shulker_box")
}

// excavateKeeps: lo que no se guarda en el cofre (herramientas, armadura, comida…).
func excavateKeeps(n string) bool {
	for _, s := range []string{"_pickaxe", "_axe", "_shovel", "_hoe", "_sword", "_helmet", "_chestplate", "_leggings", "_boots"} {
		if strings.HasSuffix(n, s) {
			return true
		}
	}
	switch n {
	case "shears", "flint_and_steel", "shield", "elytra", "bow", "crossbow", "arrow", "trident", "torch",
		"bucket", "water_bucket", "lava_bucket", "ender_pearl", "firework_rocket", "totem_of_undying":
		return true
	}
	_, food := goodFood(n)
	return food
}

func anyPick(n string) bool { return strings.HasSuffix(n, "_pickaxe") }

// Excavate es la tarea: vaciar la caja.
func Excavate(box Box) func(j *Job) error {
	return func(j *Job) error {
		if v := box.Volume(); v > ExcavateMax {
			return fmt.Errorf("la caja tiene %d bloques: demasiado (máximo %d)", v, ExcavateMax)
		}
		if _, ok := j.r.Store.MineChest(j.Dimension()); !ok && j.r.Store.Base == nil && len(j.chestsNear(nearChestR)) == 0 {
			return errors.New("no tengo dónde guardar lo cavado: no hay cofres cerca; marca uno con /mine chest (ahí también busco picos) o pon el almacén con /base here")
		}
		j.pickOK = anyPick // cualquier pico vale, pero ninguno se rompe
		j.pickFloor = pickReserve
		j.toolFloor = toolReserve // (ni las palas, hachas y azadas)
		defer func() { j.pickOK, j.pickFloor, j.toolFloor = nil, 0, 0 }()
		// las zonas protegidas apuntadas antes no bloquean la caja (la eligió
		// el jugador; se apuntan a 16 de donde estaba el bot): se intenta, y si
		// el servidor dice que no, eso sí se respeta (a 4 bloques de ahí)
		j.protectSince, j.protectR = time.Now(), excavateProtectR
		defer func() { j.protectSince, j.protectR = time.Time{}, 0 }()
		if n := protectedColumns(box, j.r.Store); n > 0 {
			j.Logf("excavar: %d columnas de la caja caen en zonas que apunté como protegidas antes; como la caja la eliges tú, las intento (si el servidor no me deja, lo apunto y las salto; /protected para verlas o borrarlas)", n)
		}

		e := &excavation{j: j, box: box, top: box.Max.Y, low: box.Min.Y, h: passMax, fails: map[world.BlockPos]int{}}
		var sizes []string
		for _, ps := range passPlan(box) {
			sizes = append(sizes, fmt.Sprint(ps[1]))
		}
		j.Logf("a excavar %s (%d bloques como mucho): %d de alto, de 4 en 4 (%d pasadas de %s): cada pasada entera en toda la caja antes de bajar a la siguiente; en cada una, desde el fondo, pico los 4 de alto que alcanzo y voy al siguiente sitio",
			box, box.Volume(), box.Max.Y-box.Min.Y+1, len(sizes), strings.Join(sizes, "+"))
		if j.UsablePicks() == 0 {
			if err := e.stash(true); err != nil {
				return err
			}
		}
		// las pasadas (capa a capa, cada una entera antes de bajar); luego, lo
		// que quedara, bloque a bloque
		err := e.sweep()
		swept := e.dug
		if err == nil && j.Fatal() == nil {
			e.top = box.Max.Y
			err = e.run()
			if e.dug > swept {
				j.Logf("excavar: el barrido cavó %d; la limpieza, %d más (lo que el barrido no alcanzó)", swept, e.dug-swept)
			}
		}
		if j.Fatal() != nil {
			return j.Fatal()
		}
		e.collectPass(box.Min.Y, box.Max.Y) // lo que cayó en la limpieza final
		// pare como pare (menos /stop): lo cavado, a guardar
		if serr := e.stash(false); serr != nil {
			j.Problem("guardar lo cavado: %v", serr)
		}
		if err != nil {
			return fmt.Errorf("%w (cavé %d bloques)", err, e.dug)
		}
		e.fromRim()
		left := e.leftovers()
		if e.dug == 0 && left != "" {
			why := "no pude cavar ningún bloque"
			if n := e.newlyProtected(); n > 0 {
				why = fmt.Sprintf("el servidor no me deja cavar aquí (\"no tienes permiso\"): %d bloques de la caja en zona protegida", n)
			}
			return fmt.Errorf("%s; quedan: %s", why, left)
		}
		if e.revertsLeft > 0 {
			j.Problem("excavar: dejé %d columnas que el servidor no me dejaba quitar (los bloques volvían a aparecer: terreno de otro, el spawn…)", e.revertsLeft)
		}
		if left == "" {
			j.Logf("excavado %s: %d bloques", box, e.dug)
		} else {
			j.Logf("excavado %s: %d bloques; quedan sin cavar: %s", box, e.dug, left)
		}
		return nil
	}
}

type excavation struct {
	j *Job
	// deniedCols: columnas dejadas porque el servidor no da permiso (dropOldProtected)
	deniedCols int
	box        Box
	// extent: lo cavado en cada capa: x mín, x máx, z mín, z máx, cuántos (layerReport)
	extent map[int]*[5]int
	// broken: lo que se rompió (y qué era); reverts: las veces que el
	// servidor lo repuso (checkRevert)
	broken      map[world.BlockPos]string
	reverts     map[world.BlockPos]int
	revertsLeft int                    // columnas dejadas porque el servidor reponía sus bloques
	seen        map[int32]uint64       // cuándo (tick) vio cada objeto del suelo por primera vez (pickable)
	top         int                    // la capa más alta que puede tener algo (las de encima ya están vacías)
	low         int                    // la más baja que mira la limpieza (run): la de la pasada, o el fondo
	h           int                    // las capas de la pasada en curso (de la de los pies hacia arriba)
	fails       map[world.BlockPos]int // los bloques que no se dejaron cavar
	dug         int
	// approached: veces seguidas que fue hacia la caja sin verla cargada
	approached int
	// wasFast, toldMode: si la pasada anterior fue corriendo (y si ya se dijo), para decirlo al cambiar
	wasFast, toldMode bool
}

// errMoved: next se movió (se apartó, o se acercó a la caja): mirar otra vez.
var errMoved = errors.New("me moví")

func (e *excavation) run() error {
	j := e.j
	for {
		if err := j.Wait(1); err != nil {
			return err
		}
		if j.FreeSlots() < 3 {
			if err := e.stash(false); err != nil {
				return err
			}
			e.top = e.box.Max.Y // (al volver, por si cayó algo o el camino puso bloques)
		}
		target, ok, err := e.next()
		if errors.Is(err, errMoved) {
			continue
		}
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		name := j.Block(target)
		j.Status("excavando: capa %d de %d…%d, %s en %v (llevo %d)", target.Y, e.box.Max.Y, e.box.Min.Y, name, target, e.dug)
		if toolFor(name) == "pickaxe" && j.bestPickSlot(pickReserve) < 0 {
			if err := e.restockPicks(); err != nil {
				return err
			}
		}
		if err := e.dig(target); err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			if errors.Is(err, ErrNoPicks) {
				if err := e.restockPicks(); err != nil {
					return err
				}
				continue
			}
			if errors.Is(err, errDigLost) {
				continue // (no es culpa del bloque: otra vez)
			}
			if e.dropOldProtected(target, err) {
				continue
			}
			e.fails[target]++
			if e.fails[target] == excavateFails {
				j.Problem("excavar: dejo %s en %v: %v", name, target, err)
			}
			continue
		}
		e.dug++ // (lo que cae se recoge al acabar la pasada: collectPass)
		e.noteBroken(target, name)
	}
}

// diggable: el bloque p de la caja hay que cavarlo (y se puede).
func (e *excavation) diggable(p world.BlockPos) bool {
	j := e.j
	n := j.Block(p)
	e.checkRevert(p, n)
	if noExcavate(n) || j.fluidBlock(p) || e.fails[p] >= excavateFails {
		return false
	}
	st := j.r.Store
	return !j.protected(p) && !st.isMineChest(p) && !st.taken(p)
}

// next es el siguiente bloque: de la capa más alta que quede, el más cercano
// que se pueda picar desde aquí; si ninguno, el más cercano (hay que ir).
// ok false: no queda nada.
func (e *excavation) next() (world.BlockPos, bool, error) {
	j := e.j
	f := j.Feet()
	for y := e.top; y >= e.low; y-- {
		var near, reach world.BlockPos
		nearD, reachD := math.Inf(1), math.Inf(1)
		unloaded, underMe := false, false
		for x := e.box.Min.X; x <= e.box.Max.X; x++ {
			for z := e.box.Min.Z; z <= e.box.Max.Z; z++ {
				p := world.BlockPos{X: x, Y: y, Z: z}
				if _, ok := j.r.d.World.BlockAt(p); !ok {
					unloaded = true
					continue
				}
				if !e.diggable(p) {
					continue
				}
				if x == f.X && z == f.Z && y < f.Y {
					underMe = true // (bajo los pies no: se va al lado)
					continue
				}
				d := j.eyeDist(p)
				if d < nearD {
					near, nearD = p, d
				}
				if d < reachD && j.canDig(p) {
					reach, reachD = p, d
				}
			}
		}
		if nearD < math.Inf(1) {
			e.approached = 0
		}
		switch {
		case reachD < math.Inf(1):
			e.top = y
			return reach, true, nil
		case nearD < math.Inf(1):
			e.top = y
			return near, true, nil
		case underMe:
			e.top = y
			if err := e.stepAside(); err != nil {
				if j.Fatal() != nil {
					return world.BlockPos{}, false, j.Fatal()
				}
				// no hay dónde apartarse (un rincón de la caja, encima de la
				// torre por la que se sale): lo de debajo se deja, no se para todo
				for cy := e.box.Min.Y; cy < f.Y; cy++ {
					e.fails[world.BlockPos{X: f.X, Y: cy, Z: f.Z}] = excavateFails
				}
				j.Logf("excavar: no puedo apartarme de %v: dejo lo de debajo (%v)", f, err)
			}
			return world.BlockPos{}, false, errMoved
		case unloaded:
			// lejos: hacia el centro de la capa, que se carguen los chunks
			c := world.BlockPos{X: (e.box.Min.X + e.box.Max.X) / 2, Y: y, Z: (e.box.Min.Z + e.box.Max.Z) / 2}
			j.Status("excavar: voy hacia %v (aún no lo veo)", c)
			if e.approached++; e.approached > 3 {
				return world.BlockPos{}, false, fmt.Errorf("no veo los bloques de %v (¿fuera de lo que carga el bot?)", c)
			}
			if err := j.GoNear(c); err != nil {
				return world.BlockPos{}, false, fmt.Errorf("no llego a la zona a excavar: %w", err)
			}
			return world.BlockPos{}, false, errMoved
		}
	}
	return world.BlockPos{}, false, nil
}

// stepAside se quita de encima del último bloque de la capa: a una celda de
// al lado (a su altura o una más abajo, ya cavada) o, si no hay, abriéndola.
func (e *excavation) stepAside() error {
	j := e.j
	f := j.Feet()
	dirs := [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	// a la celda de al lado, a su altura o dejándose caer (hasta 3: sin hacerse daño)
	for _, d := range dirs {
		if j.solid(world.BlockPos{X: f.X + d[0], Y: f.Y, Z: f.Z + d[1]}) ||
			j.solid(world.BlockPos{X: f.X + d[0], Y: f.Y + 1, Z: f.Z + d[1]}) {
			continue // (no se pasa)
		}
		for dy := 0; dy >= -3; dy-- {
			n := world.BlockPos{X: f.X + d[0], Y: f.Y + dy, Z: f.Z + d[1]}
			floor := world.BlockPos{X: n.X, Y: n.Y - 1, Z: n.Z}
			if j.solid(n) || j.liquid(n) {
				break
			}
			if j.solid(floor) {
				if j.liquid(floor) {
					break
				}
				return j.GoTo(n)
			}
		}
	}
	for _, d := range dirs { // abrir un hueco al lado (dentro de la caja, que hay que cavarlo igual)
		n := world.BlockPos{X: f.X + d[0], Y: f.Y, Z: f.Z + d[1]}
		floor := world.BlockPos{X: n.X, Y: n.Y - 1, Z: n.Z}
		if !e.box.Contains(n) || !j.solid(floor) || j.liquid(floor) {
			continue
		}
		if err := j.clearCell(world.BlockPos{X: n.X, Y: n.Y + 1, Z: n.Z}); err != nil {
			continue
		}
		if err := j.clearCell(n); err != nil {
			continue
		}
		return j.GoTo(n)
	}
	return fmt.Errorf("no puedo apartarme de %v para cavar lo de debajo", f)
}

// dig cava p: acercándose si hace falta (andando o con un túnel), tapando
// antes la lava que entraría.
func (e *excavation) dig(p world.BlockPos) error {
	j := e.j
	if !j.canDig(p) {
		// cerca: el túnel directamente (anda si hay camino, y si no cava; lo
		// de andar antes era esperar dos veces a un camino que no hay)
		if j.eyeDist(p) <= nearGoal || e.cantClimb(p) {
			if err := j.TunnelTo(p); err != nil {
				return err
			}
		} else if err := j.GoNear(p); err != nil || !j.canDig(p) {
			if err := j.TunnelTo(p); err != nil {
				return err
			}
		}
		if !j.canDig(p) {
			return fmt.Errorf("no llego a picar %v", p)
		}
	}
	if !j.makeSafe(p) {
		return fmt.Errorf("agua o lava junto a %v", p)
	}
	gravel := j.fallsAbove(p)
	if err := j.Dig(p); err != nil {
		return err
	}
	if gravel { // (solo si encima hay grava o arena: tarda en caer)
		if err := j.Wait(4); err != nil {
			return err
		}
	}
	j.plugExposed(2)
	return nil
}

// fromRim quita, desde fuera de la caja, lo que quede al alcance: la torre
// que el camino puso para salir del hoyo (para quitarla entera habría que
// bajar, y para volver a salir, otra torre). Lo que se cava desde fuera.
func (e *excavation) fromRim() {
	j := e.j
	e.top = e.box.Max.Y
	inPit := func() bool {
		f := j.Feet()
		return f.X >= e.box.Min.X && f.X <= e.box.Max.X && f.Z >= e.box.Min.Z && f.Z <= e.box.Max.Z && f.Y <= e.box.Max.Y
	}
	for tries := 0; tries < 64; tries++ {
		p, ok, err := e.next()
		if err != nil || !ok || inPit() {
			return
		}
		if !j.canDig(p) {
			if tries > 0 { // (solo se acerca una vez: luego, lo que alcance desde ahí)
				return
			}
			if err := j.GoNear(p); err != nil || inPit() || !j.canDig(p) {
				return
			}
		}
		if err := e.dig(p); err != nil {
			return
		}
		e.dug++
	}
}

// leftovers dice lo que quedó sin cavar en la caja ("" nada): agua, lava, lo
// que no se rompe y lo que falló.
func (e *excavation) leftovers() string {
	j := e.j
	count := map[string]int{}
	cols := map[[2]int]bool{}
	for y := e.box.Min.Y; y <= e.box.Max.Y; y++ {
		for x := e.box.Min.X; x <= e.box.Max.X; x++ {
			for z := e.box.Min.Z; z <= e.box.Max.Z; z++ {
				p := world.BlockPos{X: x, Y: y, Z: z}
				n := j.Block(p)
				switch n {
				case "", "air", "cave_air", "void_air":
					continue
				}
				if !j.solid(p) && !j.fluidBlock(p) {
					continue // (hierba, antorchas… si quedó algo así no importa)
				}
				count[n]++
				cols[[2]int{x, z}] = true
			}
		}
	}
	var out []string
	for n, c := range count {
		out = append(out, fmt.Sprintf("%d %s", c, n))
	}
	sort.Strings(out)
	if len(cols) > 0 && len(cols) <= 3 {
		var cs []string
		for c := range cols {
			cs = append(cs, fmt.Sprintf("x %d z %d", c[0], c[1]))
		}
		sort.Strings(cs)
		out = append(out, "en "+strings.Join(cs, ", ")+" (si es la torre por la que salí: para quitarla hay que bajar)")
	}
	return strings.Join(out, ", ")
}

// restockPicks va a por picos: guarda lo cavado y coge los del cofre de minar
// o del almacén. Error si no hay ninguno que valga.
func (e *excavation) restockPicks() error {
	j := e.j
	j.Logf("excavar: no me quedan picos sin gastar: voy a por más")
	if err := e.stash(true); err != nil {
		return err
	}
	if j.bestPickSlot(pickReserve) < 0 && !e.waitForPick() {
		return fmt.Errorf("no quedan picos con más de %d usos (no rompo ninguno): deja picos en el cofre de minar (/mine chest) o dame uno", pickReserve)
	}
	return nil
}

// waitForPick: sin pico que valga (ni en el cofre ni en el almacén), lo pide
// por el chat una vez en la tarea y espera storeWait a que se lo den (en la
// mano, o en el cofre si alguien contesta). Dice si ya tiene uno.
func (e *excavation) waitForPick() bool {
	j := e.j
	if j.pickAsked {
		return false
	}
	j.pickAsked = true
	secs := int(storeWait / time.Second)
	j.say(fmt.Sprintf("No tengo pico (o los que tengo están gastados). Dame uno o déjalo en el cofre de minar (/mine chest); te espero %d s.", secs))
	asked := j.r.chats.Load()
	for waited := 0; waited < secs; waited++ {
		j.Status("esperando un pico (%d s de %d)", waited, secs)
		if err := j.Rest(20); err != nil {
			return false
		}
		if j.bestPickSlot(pickReserve) >= 0 {
			j.Logf("excavar: ya tengo pico: sigo")
			return true
		}
		if n := j.r.chats.Load(); n > asked { // contestaron: quizá lo dejaron en el cofre
			asked = n
			if err := e.stash(true); err == nil && j.bestPickSlot(pickReserve) >= 0 {
				j.Logf("excavar: cogí un pico del cofre: sigo")
				return true
			}
		}
	}
	return false
}

// loot: lo que lleva que se guarda (todo menos herramientas, armadura, comida…).
func (e *excavation) loot() map[string]int {
	out := map[string]int{}
	for n, c := range e.j.Items() {
		if !excavateKeeps(n) {
			out[n] = c
		}
	}
	return out
}

// stash guarda lo cavado (el cofre de minar; si no hay o está lleno, el
// almacén) y devuelve los picos gastados; con restock coge picos.
func (e *excavation) stash(restock bool) error {
	j := e.j
	if j.storingOff() {
		return nil // nadie contestó a "no puedo guardar": se sigue cavando sin guardar
	}
	// (sin recoger antes: lo del suelo, al acabar la pasada)
	keep := j.filler() // un montón se queda: tapar lava, subir del hoyo
	dim := j.Dimension()
	if c, ok := j.nearMineChest(dim); ok {
		if n := j.Block(c); n != "" && !isChest(n) {
			if near, ok := j.r.NearestChest(c, 3); ok {
				j.r.Store.SetMineChest(dim, &near)
				c = near
			}
		}
		if err := e.chestTrip(c, keep, restock); err != nil {
			j.Problem("cofre de minar: %v", err)
		}
	}
	// lo que no cupo (o sin cofre de minar): a los cofres de alrededor, el
	// más cercano primero
	var denied []world.BlockPos
	if len(e.loot()) > 0 {
		kept := 0
		_, denied = j.StoreNearby(func(n string, s types.ItemStack) bool {
			if excavateKeeps(n) && !wornTool(s) {
				return false
			}
			if n == keep && kept+int(s.Count) <= excavateKeep {
				kept += int(s.Count)
				return false
			}
			return true
		})
	}
	// y lo que siga sin sitio: al almacén
	stuck := ""
	for it, n := range e.loot() {
		if it == keep && n <= excavateKeep {
			continue
		}
		if j.r.Store.Base == nil || j.tooFar(*j.r.Store.Base, "la base del almacén") {
			if j.FreeSlots() >= 3 {
				break // (hay sitio para seguir: lo que no cupo, encima)
			}
			stuck = "el inventario está lleno y ningún cofre de alrededor me deja guardar"
			break
		}
		if err := j.Deposit(it); err != nil {
			stuck = depositWhy(err)
			j.Problem("guardar %s en el almacén: %v", it, err)
			break
		}
	}
	if stuck != "" && j.FreeSlots() < 3 {
		// una sola pregunta por el chat; con respuesta, otra vez; sin ella,
		// se sigue cavando (lo cavado se queda en el suelo)
		if j.storageStuck(stuck, denied) {
			return e.stash(restock)
		}
		return nil
	}
	if restock && j.bestPickSlot(pickReserve) < 0 {
		e.picksFromStorage()
	}
	return nil
}

// chestTrip abre el cofre c: deja lo cavado (menos un montón de keep) y los
// picos gastados; con restock, coge los buenos.
func (e *excavation) chestTrip(c world.BlockPos, keep string, restock bool) error {
	j := e.j
	j.Status("excavar: al cofre de minar (%v)", c)
	if err := j.Open(c); err != nil {
		return err
	}
	defer j.CloseScreen()
	m, ok := j.r.d.Screens.Open()
	if !ok {
		return errors.New("no se abrió")
	}
	start := m.PlayerStart()
	slot := func(i int) types.ItemStack {
		s := j.r.d.Screens
		s.Lock()
		defer s.Unlock()
		return s.Inventory.Slots[i]
	}
	kept, stored := 0, 0
	for i := invStart; i <= invEnd; i++ {
		s := slot(i)
		n := strings.TrimPrefix(itemName(int32(s.Item)), "minecraft:")
		if s.Count <= 0 {
			continue
		}
		if excavateKeeps(n) && !wornTool(s) {
			continue
		}
		if n == keep && kept+int(s.Count) <= excavateKeep { // (un montón como mucho)
			kept += int(s.Count)
			continue
		}
		if err := j.r.d.Screens.Click(m.ID, start+i-invStart, 0, types.ContainerInputQuickMove); err != nil {
			return err
		}
		stored += int(s.Count)
		if err := j.Wait(2); err != nil {
			return err
		}
	}
	if stored > 0 {
		j.Logf("cofre de minar: dejo %d objetos", stored)
	}
	took := false
	if restock || j.UsablePicks() == 0 {
		for _, t := range tiers {
			if _, err := j.r.ui.Move(j.ctx, full(t+"_pickaxe"), false); err != nil {
				return err
			}
		}
		took = true
	}
	// una pala y un hacha, si no lleva (la tierra y la madera, más rápido y sin gastar picos)
	for _, kind := range []string{"shovel", "axe"} {
		if j.toolSlot(kind, toolReserve) >= 0 {
			continue
		}
		for _, t := range tiers {
			if n, err := j.r.ui.Move(j.ctx, full(t+"_"+kind), false); err != nil {
				return err
			} else if n > 0 {
				took = true
				if j.toolSlot(kind, toolReserve) >= 0 {
					break
				}
			}
		}
	}
	if took {
		// los gastados que vinieran con los buenos, de vuelta
		for i := invStart; i <= invEnd; i++ {
			if wornTool(slot(i)) {
				_ = j.r.d.Screens.Click(m.ID, start+i-invStart, 0, types.ContainerInputQuickMove)
				_ = j.Wait(2)
			}
		}
		j.Logf("cofre de minar: llevo %d picos sin gastar (%d usos)", j.UsablePicks(), j.pickUses())
	}
	return nil
}

// wornTool: un pico, pala, hacha o azada ya casi rota (no se usa: al cofre, a repararla).
func wornTool(s types.ItemStack) bool {
	n := strings.TrimPrefix(itemName(int32(s.Item)), "minecraft:")
	floor := toolReserve
	switch {
	case anyPick(n):
		floor = pickReserve
	case strings.HasSuffix(n, "_shovel"), strings.HasSuffix(n, "_axe"), strings.HasSuffix(n, "_hoe"):
	default:
		return false
	}
	l := pickLeftAs(s, func(string) bool { return true })
	return l >= 0 && l <= floor
}

// picksFromStorage coge picos de los cofres del almacén que los guardan.
func (e *excavation) picksFromStorage() {
	j := e.j
	st := j.r.Store
	st.mu.Lock()
	var cs []Chest
	for _, c := range st.Chests {
		if anyPick(c.Item) {
			cs = append(cs, c)
		}
	}
	st.mu.Unlock()
	for _, c := range cs {
		if j.bestPickSlot(pickReserve) >= 0 {
			return
		}
		if err := j.Open(c.Pos); err != nil {
			j.Problem("almacén: abrir %v: %v", c.Pos, err)
			continue
		}
		n, _ := j.r.ui.Move(j.ctx, full(c.Item), false)
		j.CloseScreen()
		if n > 0 {
			j.Logf("almacén: cojo %d %s", n, c.Item)
		}
	}
}

// depositWhy: por qué no se pudo guardar en el almacén, en corto (para el chat).
func depositWhy(err error) string {
	switch msg := err.Error(); {
	case strings.Contains(msg, "tablones"), strings.Contains(msg, "troncos"):
		return "el almacén necesita otro cofre y no tengo madera para hacerlo"
	case strings.Contains(msg, "sitio"):
		return "no encuentro sitio para otro cofre en el almacén"
	default:
		return "el almacén no me deja guardar"
	}
}
