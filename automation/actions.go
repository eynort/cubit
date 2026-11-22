package automation

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/level/block"
	"github.com/mj41/go-mc26/protocol/types"

	"github.com/mj41/go-mc26-kit/bot/world"
)

// reachDist es hasta dónde llega la mano (el juego: 4.5 desde los ojos; con margen).
const reachDist = 4.3

func blockName(s block.StateID) string {
	if int(s) < 0 || int(s) >= len(block.StateList) {
		return ""
	}
	return strings.TrimPrefix(block.StateList[s].ID(), "minecraft:")
}

// Reaches: el bloque p está al alcance de la mano desde donde está el bot.
func (j *Job) Reaches(p world.BlockPos) bool {
	e := j.Pos()
	dx, dy, dz := float64(p.X)+0.5-e.X, float64(p.Y)+0.5-(e.Y+1.62), float64(p.Z)+0.5-e.Z
	return math.Sqrt(dx*dx+dy*dy+dz*dz) <= reachDist
}

// GoNear camina hasta tener el bloque p al alcance (con el pathfinding:
// saltos, puentes, torres). Error si no llega.
func (j *Job) GoNear(p world.BlockPos) error {
	if j.Reaches(p) {
		return nil
	}
	w := j.r.d.Walker
	w.SetSprint(true)
	w.Goto(p)
	defer w.Stop()
	stuck := j.stuckFor(p)
	for t := 0; ; t++ {
		if err := j.Wait(2); err != nil {
			return err
		}
		if j.Reaches(p) && j.r.d.Ctl.State().OnGround {
			return nil
		}
		if stuck() {
			return fmt.Errorf("no avanzo hacia %v (sin moverme; %s)", p, j.fromHere(p))
		}
		switch st := w.Status(); {
		case st == "arrived":
			if j.Reaches(p) {
				return nil
			}
			return fmt.Errorf("llegué cerca de %v pero no lo alcanzo (%s)", p, j.fromHere(p))
		case strings.HasPrefix(st, "failed"):
			return fmt.Errorf("no llego a %v: %s (%s)", p, st, j.fromHere(p))
		case st == "stopped": // una pelea paró el walker: retomar
			w.Goto(p)
		}
		if t > j.walkLimit() { // 60 s
			return fmt.Errorf("tardo demasiado en llegar a %v (%s)", p, j.fromHere(p))
		}
	}
}

// walkLimit: las vueltas (de 2 ticks) que GoNear y GoTo esperan antes de
// rendirse: 60 s, o lo que diga goLimit (chop: 20 s).
func (j *Job) walkLimit() int {
	if j.goLimit > 0 {
		return j.goLimit
	}
	return 60 * 10
}

// GoTo camina hasta la celda p (para estar en ella).
func (j *Job) GoTo(p world.BlockPos) error {
	w := j.r.d.Walker
	w.SetSprint(true)
	w.Goto(p)
	defer w.Stop()
	stuck := j.stuckWatch()
	for t := 0; ; t++ {
		if err := j.Wait(2); err != nil {
			return err
		}
		if stuck() {
			if j.Feet() == p { // (ya está en la celda: el walker no lo dio por llegado, pero lo está)
				return nil
			}
			return fmt.Errorf("no avanzo hacia %v (sin moverme; %s)", p, j.fromHere(p))
		}
		switch st := w.Status(); {
		case st == "arrived":
			return nil
		case strings.HasPrefix(st, "failed"):
			return fmt.Errorf("no llego a %v: %s (%s)", p, st, j.fromHere(p))
		case st == "stopped":
			w.Goto(p)
		}
		if t > j.walkLimit() {
			return fmt.Errorf("tardo demasiado en llegar a %v (%s)", p, j.fromHere(p))
		}
	}
}

// protected: p está en una zona protegida (las que cuentan para la tarea:
// protectSince/protectR).
func (j *Job) protected(p world.BlockPos) bool {
	if !j.protectSince.IsZero() { // (la tarea mira solo los "no" de ahora: /excavate)
		return j.r.Store.DeniedNear(p, j.protectR, j.protectSince)
	}
	return j.r.Store.IsProtected(p)
}

// ErrProtected: el sitio es de una zona donde el servidor no deja tocar nada.
var ErrProtected = errors.New("zona protegida: aquí no se toca nada")

// Dig rompe el bloque p (al alcance) con la mejor herramienta que lleve.
func (j *Job) Dig(p world.BlockPos) error {
	st, ok := j.r.d.World.BlockAt(p)
	if !ok {
		return fmt.Errorf("%v no está cargado", p)
	}
	if block.IsAir(st) {
		return nil
	}
	if !j.Reaches(p) {
		return fmt.Errorf("%v fuera de alcance", p)
	}
	if j.protected(p) {
		return fmt.Errorf("%w (%v)", ErrProtected, p)
	}
	name := blockName(st)
	j.HoldBestTool(name)
	// los picos del cofre no se rompen: si los que lleva están todos gastados,
	// no se pica (sin ninguno de esos, con lo que tenga, como cualquiera)
	if j.pickFloor > 0 && toolFor(name) == "pickaxe" && j.bestPickSlot(j.pickFloor) < 0 && j.CountWhere(j.isPick) > 0 {
		return ErrNoPicks
	}
	j.r.d.Walker.Stop()
	j.r.d.Ctl.SetInput(control.Input{})
	done := make(chan act.Dig, 1)
	j.r.d.Hands.Dig(p, func(d act.Dig) { done <- d })
	lost := j.digWatch(func(q world.BlockPos) bool { return q == p })
	for t := 0; ; t++ { // tick a tick: el resultado llega en el tick en que se rompe
		select {
		case d := <-done:
			if d.Err != nil {
				return d.Err
			}
			return j.Wait(0) // (sin pausa: el siguiente bloque, ya)
		default:
		}
		if err := lost(); err != nil {
			return err
		}
		if t > 30*20 {
			j.r.d.Hands.CancelDig()
			return errors.New("cavar: no termina")
		}
		if err := j.r.d.Ctl.WaitTicks(j.ctx, 1); err != nil {
			j.r.d.Hands.CancelDig()
			return err
		}
	}
}

// errDigLost: otra cosa usó las manos (la pelea, la seguridad de caídas o
// del fuego, comer…) y la picada se perdió: se vuelve a intentar.
var errDigLost = errors.New("las manos dejaron de picar")

// digWatch vigila una picada en curso: error si las manos ya no pican
// ninguno de los bloques (ours) durante unos ticks seguidos (sin eso, se
// esperaba al plazo de 30 s, con el bot quieto).
func (j *Job) digWatch(ours func(world.BlockPos) bool) func() error {
	idle := 0
	return func() error {
		if pos, what, ok := j.r.d.Hands.Target(); ok && what == "dig" && ours(pos) {
			idle = 0
			return nil
		}
		if idle++; idle < 4 { // (al acabar, el resultado llega en el mismo tick: margen)
			return nil
		}
		_, what, ok := j.r.d.Hands.Target()
		if !ok {
			what = "nada"
		}
		j.Logf("cavar: las manos dejaron de picar (ahora: %s): lo vuelvo a intentar", what)
		return errDigLost
	}
}

// DigChain rompe los bloques de cells (al alcance) seguidos, como un jugador
// que barre con el botón apretado: al romperse uno ya apunta al siguiente y
// lo empieza en el tick de después, sin pausas (act.Hands.DigNext). La
// herramienta, la de cada tramo de bloques del mismo tipo (cambiarla sí es
// una pausa). Los que no se alcanzan, son aire o no se tocan, se saltan.
// Devuelve cuáles rompió; el error, si hay que parar (sin picos, /stop).
func (j *Job) DigChain(cells []world.BlockPos) (map[world.BlockPos]bool, error) {
	return j.digChain(cells, false)
}

// digChain es DigChain; moving: sin parar al bot (corre mientras pica).
func (j *Job) digChain(cells []world.BlockPos, moving bool) (map[world.BlockPos]bool, error) {
	broke := map[world.BlockPos]bool{}
	var todo []world.BlockPos
	for _, p := range cells {
		st, ok := j.r.d.World.BlockAt(p)
		if !ok || block.IsAir(st) || !j.Reaches(p) || j.protected(p) {
			continue
		}
		todo = append(todo, p)
	}
	for len(todo) > 0 {
		// el tramo con la misma herramienta
		kind := toolFor(j.Block(todo[0]))
		n := 1
		for n < len(todo) && toolFor(j.Block(todo[n])) == kind {
			n++
		}
		run := todo[:n]
		todo = todo[n:]
		j.HoldBestTool(j.Block(run[0]))
		if j.pickFloor > 0 && kind == "pickaxe" && j.bestPickSlot(j.pickFloor) < 0 && j.CountWhere(j.isPick) > 0 {
			return broke, ErrNoPicks
		}
		if !moving {
			j.r.d.Walker.Stop()
			j.r.d.Ctl.SetInput(control.Input{})
		}
		type res struct {
			i int
			d act.Dig
		}
		done := make(chan res, len(run))
		dig := func(i int, first bool) {
			cb := func(d act.Dig) { done <- res{i, d} }
			if first {
				j.r.d.Hands.Dig(run[i], cb)
			} else {
				j.r.d.Hands.DigNext(run[i], cb)
			}
		}
		dig(0, true)
		if len(run) > 1 {
			dig(1, false)
		}
		got := 0
		pending := map[world.BlockPos]bool{}
		for _, c := range run {
			pending[c] = true
		}
		lost := j.digWatch(func(q world.BlockPos) bool { return pending[q] })
		for t := 0; got < len(run); t++ {
			for drained := false; !drained; {
				select {
				case r := <-done:
					got++
					t = 0
					delete(pending, run[r.i])
					if r.d.Err == nil {
						broke[run[r.i]] = true
					}
					// el que sigue ya va (lo cogió en este tick): detrás, el siguiente
					if k := r.i + 2; k < len(run) {
						dig(k, false)
					}
				default:
					drained = true
				}
			}
			if got == len(run) {
				break
			}
			if err := lost(); err != nil {
				return broke, err
			}
			if t > 30*20 {
				j.r.d.Hands.CancelDig()
				return broke, errors.New("cavar: no termina")
			}
			if err := j.r.d.Ctl.WaitTicks(j.ctx, 1); err != nil {
				j.r.d.Hands.CancelDig()
				return broke, err
			}
		}
		if err := j.Wait(0); err != nil { // (las comprobaciones de siempre: pelea, lava, hambre)
			return broke, err
		}
	}
	return broke, nil
}

// Place pone el objeto item en la celda p (contra un bloque de al lado).
func (j *Job) Place(item string, p world.BlockPos) error { return j.placeAgainst(item, p, nil) }

// PlaceOn pone item en p contra el bloque against (que la toca).
func (j *Job) PlaceOn(item string, p, against world.BlockPos) error {
	return j.placeAgainst(item, p, &against)
}

func (j *Job) placeAgainst(item string, p world.BlockPos, against *world.BlockPos) error {
	if j.protected(p) {
		return fmt.Errorf("%w (%v)", ErrProtected, p)
	}
	if err := j.Hold(item); err != nil {
		return err
	}
	j.r.d.Walker.Stop()
	j.r.d.Ctl.SetInput(control.Input{})
	if err := j.Wait(2); err != nil {
		return err
	}
	done := make(chan act.Use, 1)
	if against != nil {
		j.r.d.Hands.PlaceOn(p, *against, func(u act.Use) { done <- u })
	} else {
		j.r.d.Hands.Place(p, func(u act.Use) { done <- u })
	}
	select {
	case u := <-done:
		if u.Err != nil {
			return u.Err
		}
		return j.Wait(2)
	case <-time.After(10 * time.Second):
		return errors.New("poner: el servidor no contesta")
	case <-j.ctx.Done():
		return j.ctx.Err()
	}
}

// Open abre el bloque p (un cofre, una mesa, un horno) y espera su pantalla.
func (j *Job) Open(p world.BlockPos) error {
	if err := j.GoNear(p); err != nil {
		return err
	}
	j.CloseScreen()
	done := make(chan act.Use, 1)
	j.r.d.Hands.Interact(p, func(u act.Use) { done <- u })
	select {
	case u := <-done:
		if u.Err != nil {
			return u.Err
		}
	case <-time.After(5 * time.Second):
		return errors.New("abrir: no contesta")
	case <-j.ctx.Done():
		return j.ctx.Err()
	}
	for t := 0; t < 40; t++ {
		if m, ok := j.r.d.Screens.Open(); ok && m.PlayerStart() >= 0 {
			return j.Wait(2)
		}
		if err := j.r.d.Ctl.WaitTicks(j.ctx, 1); err != nil {
			return err
		}
	}
	return fmt.Errorf("%v no se abrió", p)
}

// CloseScreen cierra la pantalla abierta (si hay).
func (j *Job) CloseScreen() {
	if m, ok := j.r.d.Screens.Open(); ok {
		_ = j.r.d.Screens.Close(m.ID)
		_ = j.r.d.Ctl.WaitTicks(j.ctx, 2)
	}
}

// Collect recoge lo que hay tirado a menos de radius bloques del bot (lo que
// soltó un bloque roto): camina a cada objeto hasta que lo coge.
func (j *Job) Collect(radius float64) error {
	p := j.Pos()
	return j.CollectAround(p.X, p.Y, p.Z, radius)
}

// CollectAround recoge lo que hay tirado a menos de radius de (x, y, z): va al
// más cercano, y al siguiente. Lo que se quedó encima de unas hojas (los
// troncos de la copa y de las ramas caen ahí) lo hace caer rompiéndolas. Al
// acabar, deja en el log lo que no pudo coger.
func (j *Job) CollectAround(x, y, z, radius float64) error {
	if j.FreeSlots() == 0 {
		return nil // (lleno: lo del suelo se queda)
	}
	tried := map[int32]int{}
	leaves := map[int32]int{} // hojas rotas bajo cada objeto
	opened := map[int32]bool{}
	usable := func(e entities.Entity) (string, bool) {
		if e.Type != "minecraft:item" || e.Y < y-3 {
			return "", false
		}
		if j.collectOnly != nil && !j.collectOnly(itemCell(e)) {
			return "", false // (fuera de donde se recoge ahora: la caja de /excavate)
		}
		v, ok := e.Data[entitydata.ItemEntityItem]
		if !ok {
			return "", false
		}
		name := ""
		if st, ok := v.Value.(*types.ItemStack); ok {
			name = strings.TrimPrefix(itemName(int32(st.Item)), "minecraft:")
		}
		if name != "" && j.ignore != nil && j.ignore(name) {
			return "", false // basura que tiró él
		}
		if j.burning(itemCell(e)) {
			return "", false // en el fuego o la lava: se quema, y él también
		}
		return name, true
	}
	falling := 0 // ticks esperando a objetos que siguen en el aire
	for quiet := 0; quiet < 10; {
		if !j.collectUntil.IsZero() && time.Now().After(j.collectUntil) {
			j.Logf("recoger: se acabó el tiempo (lo que queda se deja)")
			break
		}
		var target, onLeaves *entities.Entity
		inAir := false
		bestD := math.Inf(1)
		me := j.Pos()
		for _, e := range j.r.d.Entities.Nearby(x, y, z, radius) {
			if _, ok := usable(e); !ok || tried[e.ID] >= 3 {
				continue
			}
			// el servidor dice dónde está un objeto una vez por segundo: hasta
			// entonces se ve donde salió, en el aire. Ahí no se va (se haría una
			// torre hacia la nada): se espera a que caiga
			under := itemCell(e)
			under.Y--
			if j.naturalLeaves(under) {
				if onLeaves == nil && leaves[e.ID] < 8 {
					onLeaves = &e
				}
				continue
			}
			if n := j.Block(under); n == "air" || n == "cave_air" || n == "void_air" {
				inAir = true
				continue
			}
			// el más cercano al bot (no el primero de la lista: eso era cruzar
			// la zona de punta a punta una y otra vez)
			if d := math.Abs(e.X-me.X) + math.Abs(e.Y-me.Y) + math.Abs(e.Z-me.Z); d < bestD {
				e := e
				target, bestD = &e, d
			}
		}
		if target == nil && onLeaves != nil {
			// encima de unas hojas (los troncos de la copa y de las ramas
			// caen ahí): se rompen para que caiga
			e := *onLeaves
			under := itemCell(e)
			under.Y--
			quiet = 0
			leaves[e.ID]++
			j.Status("tirando de las hojas lo que se quedó encima")
			if err := j.GoNear(under); err == nil {
				_ = j.Dig(under)
			} else if j.Fatal() != nil {
				return j.Fatal()
			}
			if err := j.Wait(4); err != nil {
				return err
			}
			continue
		}
		if target == nil {
			if inAir && falling < 60 { // 3 s como mucho: luego, lo que hubiera
				falling += 2
			} else {
				quiet++
			}
			if err := j.Wait(2); err != nil {
				return err
			}
			continue
		}
		quiet = 0
		tried[target.ID]++
		got, err := j.walkToItem(*target)
		if err != nil {
			return err
		}
		if !got && tried[target.ID] >= 2 && !opened[target.ID] && j.collectOpen != nil {
			opened[target.ID] = true // (una sola apertura por objeto)
			if j.collectOpen(itemCell(*target)) {
				tried[target.ID] = 1
			}
		}
	}
	var left []string
	for _, e := range j.r.d.Entities.Nearby(x, y, z, radius) {
		if name, ok := usable(e); ok {
			left = append(left, fmt.Sprintf("%s en %v", name, itemCell(e)))
		}
	}
	if len(left) > 0 {
		j.Logf("recoger: me dejo %d objetos que no alcanzo (%s; %d huecos libres)", len(left), strings.Join(left, ", "), j.FreeSlots())
	}
	return nil
}

// itemCell es la celda donde está un objeto tirado.
func itemCell(e entities.Entity) world.BlockPos {
	return world.BlockPos{X: int(math.Floor(e.X)), Y: int(math.Floor(e.Y + 0.2)), Z: int(math.Floor(e.Z))}
}

// walkToItem camina hasta el objeto (siguiéndolo si se mueve); true si lo
// cogió (o ya no está).
func (j *Job) walkToItem(e entities.Entity) (bool, error) {
	w := j.r.d.Walker
	w.SetSprint(false)
	goal := itemCell(e)
	w.Goto(goal)
	defer w.Stop()
	best, still := math.Inf(1), 0
	for t := 0; t < 100; t++ {
		if err := j.Wait(2); err != nil {
			return false, err
		}
		now, ok := j.r.d.Entities.Get(e.ID)
		if !ok {
			return true, nil // cogido (o desaparecido)
		}
		// sin acercarse en 2 s (encima de un pilar, en un hoyo): se deja
		p := j.Pos()
		if d := math.Abs(now.X-p.X) + math.Abs(now.Y-p.Y) + math.Abs(now.Z-p.Z); d < best-0.1 {
			best, still = d, 0
		} else if still++; still > 20 && !j.r.d.Builder.Busy() {
			return false, nil
		}
		if c := itemCell(now); c != goal { // se movió (cayó, rodó): a donde está ahora
			goal = c
			w.Goto(goal)
			continue
		}
		if st := w.Status(); st == "arrived" || strings.HasPrefix(st, "failed") {
			for i := 0; i < 3; i++ { // a veces tarda un poco en cogerlo
				if err := j.Wait(1); err != nil {
					return false, err
				}
				if _, ok := j.r.d.Entities.Get(e.ID); !ok {
					return true, nil
				}
			}
			return false, nil
		}
	}
	return false, nil
}

// burning: en p hay fuego o lava.
func (j *Job) burning(p world.BlockPos) bool {
	n := j.Block(p)
	return n == "fire" || n == "soul_fire" || n == "lava"
}

// stuckWatch: una función que dice si el bot lleva 10 s sin moverse medio
// bloque (sin contar peleas ni huidas de la lava: ahí no anda por su cuenta).
func (j *Job) stuckWatch() func() bool { return j.stuckWatchFor(10 * time.Second) }

// nearStuck: hacia algo cerca (a nearGoal o menos), sin moverse este rato ya
// no hay camino andando: mejor cavar (esperar 10 s por cada bloque al que
// no se llega andando era ir a paso de tortuga).
const (
	nearGoal  = 6
	nearStuck = 3 * time.Second
)

// stuckWatchFor: el bot lleva limit sin moverse (ni picar, ni poner bloques).
func (j *Job) stuckWatchFor(limit time.Duration) func() bool {
	last, since := j.Pos(), time.Now()
	return func() bool {
		p := j.Pos()
		if math.Abs(p.X-last.X)+math.Abs(p.Y-last.Y)+math.Abs(p.Z-last.Z) > 0.5 ||
			j.r.d.Hands.Busy() || (j.r.d.Builder != nil && j.r.d.Builder.Busy()) {
			last, since = p, time.Now()
			return false
		}
		return time.Since(since) > limit
	}
}

// stuckFor es lo que se espera sin moverse yendo hacia p: poco si está cerca.
func (j *Job) stuckFor(p world.BlockPos) func() bool {
	if j.eyeDist(p) <= nearGoal {
		return j.stuckWatchFor(nearStuck)
	}
	return j.stuckWatch()
}

// fromHere: "desde 210 -19 -477, a 34 bloques": para los errores de camino.
func (j *Job) fromHere(p world.BlockPos) string {
	f := j.Feet()
	dx, dy, dz := float64(p.X-f.X), float64(p.Y-f.Y), float64(p.Z-f.Z)
	return fmt.Sprintf("desde %v, a %.0f bloques", f, math.Sqrt(dx*dx+dy*dy+dz*dz))
}
