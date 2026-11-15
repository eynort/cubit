package automation

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/protocol/types"
)

// /mine <minerales>: una sola tarea para todos los minerales, siempre con
// strip mining; lo que cambia es la estrategia de cada uno (ores.go). Con
// varios ("diamond,iron") los viajes se turnan: cada uno a la capa y con el
// patrón de un mineral, sacando por el camino cualquiera de los pedidos.
//
// Con el cofre de /mine chest: de ahí coge picos (sin romper ninguno: con
// pickReserve usos se devuelven), comida y armadura, y ahí deja lo minado
// cada lote. Sin picos en el cofre (o sin cofre) se hace uno con la
// progresión de siempre y va igual; sin cofre, lo minado va al almacén.

// pathMax: un túnel de bajada más largo que esto se olvida (cavar otro sale antes).
const pathMax = 1500

// mineFood: con menos piezas de comida que estas coge más del cofre.
const mineFood = 12

// armorTiers: de mejor a peor, lo que se coge del cofre para cada hueco.
var armorTiers = []string{"netherite", "diamond", "iron", "chainmail", "golden", "leather"}

// armorPieces en el orden de los huecos del inventario (5 cabeza … 8 pies).
var armorPieces = []string{"helmet", "chestplate", "leggings", "boots"}

// MineOres es la tarea: target de lo que sale de esos minerales (0: hasta /stop).
func MineOres(os []Ore, target int) func(j *Job) error {
	return func(j *Job) error {
		need := 0 // el pico que hace falta: el del más duro
		for _, o := range os {
			need = max(need, o.Pick)
			if o.Dim == theNether {
				j.r.wantGold.Store(true)
			}
		}
		j.pickOK = func(n string) bool { return pickTierOf(n) >= need }
		defer func() { j.pickOK, j.pickFloor = nil, 0 }()
		have := func() int { return j.CountWhere(oreItemOf(os)) }
		defer func() { // pare como pare (menos /stop): lo que lleve, a guardar
			if have() > 0 && j.ctx.Err() == nil && j.fatal == nil {
				j.pickFloor = pickHomeward
				if err := j.stash(os, need, false); err != nil {
					j.Problem("guardar lo minado: %v", err)
				}
			}
		}()
		names := make([]string, len(os))
		for i, o := range os {
			names[i] = o.What
		}
		j.Logf("a minar %s (%s)", strings.Join(names, ", "), map[bool]string{true: "hasta /stop", false: fmt.Sprint(target)}[target == 0])
		total, empty := 0, 0
		for round := 0; ; round++ {
			o := os[round%len(os)]
			j.pickFloor = 0 // (cruzar o bajar no es minar: cualquier pico vale)
			// el cofre es el de la dimensión del mineral: a por diamantes no se
			// va al Nether (si no hay cofre aquí, se mina sin él)
			home := o.Dim
			if j.Dimension() != o.Dim {
				if err := j.toDimension(o.Dim); err != nil {
					return fmt.Errorf("ir a %s (%s): %w", dimName(o.Dim), o.What, err)
				}
			}
			j.pickFloor = pickHomeward // (los del cofre, casi gastados, se devuelven)
			if err := j.stash(os, need, true); err != nil {
				return err
			}
			j.pickFloor = 0 // (ensureMinePick y el viaje deciden si los picos son del cofre)
			own, err := j.ensureMinePick(o, need)
			if err != nil {
				return err
			}
			want := 0
			if target > 0 {
				want = target - total
			}
			got, err := j.oreTrip(o, os, want, own, need, home)
			total += got
			if j.Fatal() != nil {
				return j.Fatal()
			}
			if err != nil && !errors.Is(err, ErrNoPicks) {
				return err
			}
			j.Logf("minar: %d en este viaje (%s), %d en total", got, o.What, total)
			if target > 0 && total >= target {
				j.pickFloor = pickHomeward
				if err := j.stash(os, need, false); err != nil {
					j.Problem("guardar: %v", err)
				}
				j.Logf("minar: hecho, %d de %d", total, target)
				return nil
			}
			if got > 0 {
				empty = 0
			} else if empty++; empty >= 2*len(os)+1 {
				return fmt.Errorf("%d viajes seguidos sin sacar nada (junté %d): paro", empty, total)
			}
		}
	}
}

// ensureMinePick: si no lleva picos del cofre, uno propio del nivel que hace
// falta (con la progresión: madera, piedra, hierro, diamantes). own: es suyo
// (se usa hasta romperse).
func (j *Job) ensureMinePick(o Ore, need int) (own bool, err error) {
	if j.UsablePicks() > 0 {
		return false, nil
	}
	j.pickFloor = 0
	if j.pickTier() >= need {
		return true, nil
	}
	if j.inNether() {
		return true, fmt.Errorf("estoy en el Nether sin pico que valga para %s y aquí no puedo hacerme uno", o.What)
	}
	if _, ok := j.r.Store.MineChest(o.Dim); ok {
		j.Logf("en el cofre de minar no hay picos que valgan para %s: me hago uno", o.What)
	} else {
		j.Logf("no hay cofre de minar (/mine chest): me hago un pico para %s", o.What)
	}
	switch {
	case need >= tierDiamond:
		err = j.EnsureDiamondPickaxe()
	case need >= tierIron:
		err = j.EnsureIronPickaxe()
	default:
		err = j.ensureStonePickaxe("", 0)
	}
	if err != nil && j.Fatal() == nil {
		err = fmt.Errorf("sin pico para %s: %w (deja picos en el cofre de minar, o dame uno)", o.What, err)
	}
	return true, err
}

// oreTrip es un viaje: a la dimensión del mineral (con oro y comida si es el
// Nether), por el túnel del viaje anterior, galerías hasta el lote (o gastar
// los picos, o la bolsa llena) y de vuelta por el túnel. Dice cuánto sacó.
func (j *Job) oreTrip(o Ore, os []Ore, want int, own bool, need int, home string) (int, error) {
	if j.FoodItems() == 0 && !j.inNether() {
		if err := j.EnsureFood(8); err != nil {
			if j.Fatal() != nil {
				return 0, j.Fatal()
			}
			if o.Dim == theNether {
				return 0, fmt.Errorf("para el Nether necesito comida (déjala en el cofre): %w", err)
			}
			j.Logf("comida: %v (voy sin ella)", err)
		}
	}
	if o.Gold {
		if err := j.EnsureGoldArmor(); err != nil {
			j.Logf("sin oro puesto (%v): los piglins me atacarán. Deja una pieza de oro en el cofre", err)
		}
	}
	if j.Dimension() != o.Dim {
		if err := j.toDimension(o.Dim); err != nil {
			return 0, err
		}
	}
	have := func() int { return j.CountWhere(oreItemOf(os)) }
	start := have()
	batch := o.Batch
	if want > 0 {
		batch = min(want, batch)
	}
	goal := start + batch
	j.trail = nil
	// por el túnel del viaje anterior (si empieza cerca de aquí)
	if plan := j.r.strips[o.Key]; plan != nil && len(plan.path) > 0 && plan.dim == j.Dimension() {
		f, p0 := j.Feet(), plan.path[0]
		if abs(f.X-p0.X)+abs(f.Y-p0.Y)+abs(f.Z-p0.Z) <= 8 {
			j.Status("bajando por el túnel del viaje anterior (%d celdas)", len(plan.path))
			if err := j.walkCells(plan.path); err != nil {
				if j.Fatal() != nil {
					return 0, j.Fatal()
				}
				j.Problem("por el túnel de antes: %v (sigo desde aquí)", err)
				if i := slices.Index(plan.path, j.Feet()); i >= 0 {
					j.trail = append(j.trail, plan.path[:i+1]...)
				}
			} else {
				j.trail = append(j.trail, plan.path...)
			}
		}
	}
	stop := func() error {
		if own && j.pickTier() < need || !own && j.UsablePicks() == 0 {
			return ErrNoPicks
		}
		return nil
	}
	j.pickFloor = 0
	if !own {
		j.pickFloor = pickReserve
	}
	j.plugged = 0
	j.Logf("a por %s a y=%d: galerías de %d cada %d (%s)", o.What, o.Y, o.Branch, o.Spacing, j.pickDesc(own))
	err := j.stripMine(o, oreBlockOf(os), have, goal, stop)
	if j.Fatal() != nil {
		return have() - start, j.Fatal()
	}
	got := have() - start
	if err != nil {
		j.Problem("%s: %v (vuelvo con %d)", o.What, err, got)
	}
	if j.plugged > 0 {
		j.Logf("lava tapada: %d", j.plugged)
	}
	// el túnel para el viaje siguiente (sin las vueltas)
	if plan := j.r.strips[o.Key]; plan != nil {
		plan.path = nil
		if t := loopless(j.trail); len(t) <= pathMax {
			plan.path = t
		} else {
			j.Logf("el túnel ya es muy largo (%d celdas): el viaje siguiente baja por otro sitio", len(t))
		}
	}
	// de vuelta: los picos casi gastados ya pueden servir (salir del túnel)
	if !own {
		j.pickFloor = pickHomeward
	}
	if rerr := j.retrace(); rerr != nil {
		if j.Fatal() != nil {
			return got, j.Fatal()
		}
		j.Problem("volver por el túnel: %v (salgo por donde pueda)", rerr)
		if plan := j.r.strips[o.Key]; plan != nil {
			plan.path = nil
		}
		if !j.inNether() {
			if serr := j.Surface(); serr != nil {
				j.Logf("%v", serr)
			}
		}
	}
	j.trail = nil
	// a la dimensión del cofre (o de la base)
	if j.Dimension() != home {
		if err := j.toDimension(home); err != nil {
			return got, fmt.Errorf("volver a %s: %w", dimName(home), err)
		}
	}
	return got, nil
}

// loopless quita las vueltas de un rastro (de una celda a la misma otra vez).
func loopless(t []world.BlockPos) []world.BlockPos {
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

// pickDesc resume los picos que lleva para el log.
func (j *Job) pickDesc(own bool) string {
	if own {
		return "con mi " + j.best("pickaxe")
	}
	return fmt.Sprintf("%d picos del cofre, %d usos", j.UsablePicks(), j.pickUses())
}

// stash guarda lo minado: en el cofre de /mine chest (y, con restock, coge
// de ahí picos, comida y armadura) o, sin cofre, en el almacén.
func (j *Job) stash(os []Ore, need int, restock bool) error {
	if j.storingOff() && !j.r.Chests() {
		return nil // (cofres desactivados: lo minado, encima)
	}
	dim := j.Dimension()
	loot := j.CountWhere(oreItemOf(ores))
	// con lo que hace falta encima (un pico que vale, comida) y nada que
	// guardar: a ningún cofre (ir a por cosas que ya lleva es andar para nada)
	needs := restock && (j.UsablePicks() == 0 && j.pickTier() < need || j.FoodItems() == 0)
	if !needs && loot == 0 {
		return nil
	}
	here, ok := j.nearMineChest(dim)
	if !ok && loot > 0 {
		// hay que guardar y no hay cofre cerca: uno propio, aquí
		here, ok = j.makeMineChest(dim)
	}
	if !ok {
		j.storeMissing = true
		// sin cofre (ni madera para hacerlo): se sigue minando con lo minado
		// encima; con muchos, se avisa (lootCheck)
		if loot > 0 && !j.inNether() {
			if err := j.depositOres(); err != nil {
				j.Problem("no pude guardar lo minado (%v): lo llevo encima y sigo", err)
			}
		}
		j.lootCheck(j.CountWhere(oreItemOf(os)), os)
		return nil
	}
	j.storeMissing = false
	c := &here
	if n := j.Block(*c); n != "" && !isChest(n) {
		// las coordenadas a ojo suelen ir un bloque desviadas: el más cercano
		near, ok := j.r.NearestChest(*c, 3)
		if !ok {
			return fmt.Errorf("en %v no hay un cofre (hay %s): ponme junto a él y dime /mine chest", *c, n)
		}
		j.Logf("en %v no hay cofre (hay %s): uso el de %v", *c, n, near)
		j.r.Store.SetMineChest(dim, &near)
		c = &near
	}
	j.Status("al cofre de minar (%v)", *c)
	if err := j.Open(*c); err != nil {
		return fmt.Errorf("cofre de minar: %w", err)
	}
	m, ok := j.r.d.Screens.Open()
	if !ok {
		j.CloseScreen()
		return errors.New("cofre de minar: no se abrió")
	}
	start := m.PlayerStart()
	slot := func(i int) types.ItemStack {
		s := j.r.d.Screens
		s.Lock()
		defer s.Unlock()
		return s.Inventory.Slots[i]
	}
	backWorn := func() (int, error) {
		n := 0
		for i := invStart; i <= invEnd; i++ {
			if l := j.pickLeft(slot(i)); l >= 0 && l <= pickReserve {
				if err := j.r.d.Screens.Click(m.ID, start+i-invStart, 0, types.ContainerInputQuickMove); err != nil {
					return n, err
				}
				n++
				if err := j.Wait(2); err != nil {
					return n, err
				}
			}
		}
		return n, nil
	}
	fail := func(err error) error { j.CloseScreen(); return err }
	// 1. lo minado (de cualquier mineral conocido: lo de viajes anteriores también)
	for _, it := range oreItems() {
		if j.Count(it) == 0 {
			continue
		}
		moved, err := j.r.ui.Move(j.ctx, full(it), true)
		if err != nil {
			return fail(err)
		}
		if moved > 0 {
			j.Logf("cofre de minar: dejo %d %s", moved, it)
		}
	}
	// 2. los picos gastados (para repararlos)
	if n, err := backWorn(); err != nil {
		return fail(err)
	} else if n > 0 {
		j.Logf("cofre de minar: dejo %d picos gastados", n)
	}
	if restock {
		// 3. los picos buenos que valgan (el cofre da por nombre: los gastados, de vuelta)
		for i := 0; i < len(tiers) && len(tiers)-i >= need; i++ {
			if _, err := j.r.ui.Move(j.ctx, full(tiers[i]+"_pickaxe"), false); err != nil {
				return fail(err)
			}
		}
		if _, err := backWorn(); err != nil {
			return fail(err)
		}
		// 4. comida
		if j.FoodItems() < mineFood {
			for _, it := range chestFoods {
				if n, _ := j.r.ui.Move(j.ctx, full(it), false); n > 0 && j.FoodItems() >= mineFood {
					break
				}
			}
		}
		// 5. oro para el Nether (si va y no lleva)
		if j.r.wantGold.Load() && !j.WearingGold() && j.CountWhere(isGoldArmor) == 0 {
			for _, it := range []string{"golden_boots", "golden_helmet", "golden_leggings", "golden_chestplate"} {
				if n, _ := j.r.ui.Move(j.ctx, full(it), false); n > 0 {
					break
				}
			}
		}
		// 6. armadura para los huecos vacíos: la mejor que haya
		for _, piece := range armorPieces {
			if j.wornIn(piece) != "" || j.carriedArmor(piece) != "" {
				continue
			}
			for _, t := range armorTiers {
				if n, _ := j.r.ui.Move(j.ctx, full(t+"_"+piece), false); n > 0 {
					break
				}
			}
		}
	}
	j.CloseScreen()
	if restock {
		j.Logf("cofre de minar: llevo %d picos (%d usos), %d de comida", j.UsablePicks(), j.pickUses(), j.FoodItems())
		if j.r.wantGold.Load() && !j.WearingGold() && j.CountWhere(isGoldArmor) > 0 {
			_ = j.EnsureGoldArmor()
		}
		j.wearArmor()
	}
	if j.CountWhere(oreItemOf(ores)) > 0 && !j.inNether() { // no cupo: al almacén
		j.Logf("el cofre de minar está lleno: el resto al almacén")
		return j.depositOres()
	}
	return nil
}

// oreItems: lo que sale de todos los minerales conocidos.
func oreItems() []string {
	var out []string
	for _, o := range ores {
		for _, it := range o.Items {
			if !slices.Contains(out, it) {
				out = append(out, it)
			}
		}
	}
	return out
}

// depositOres guarda en el almacén lo minado que lleve.
func (j *Job) depositOres() error {
	if b := j.r.Store.Base; b != nil && j.tooFar(*b, "la base del almacén") {
		return nil // (lejos: lo minado, encima, hasta que haya dónde guardar cerca)
	}
	for _, it := range oreItems() {
		if j.Count(it) == 0 {
			continue
		}
		if err := j.Deposit(it); err != nil {
			return fmt.Errorf("guardar %s: %w", it, err)
		}
	}
	return nil
}

func isGoldArmor(n string) bool {
	return strings.HasPrefix(n, "golden_") && armorSlot[n[strings.LastIndex(n, "_")+1:]] > 0
}

// wornIn es la pieza puesta en el hueco piece ("helmet"…), "" si nada.
func (j *Job) wornIn(piece string) string {
	s := j.r.d.Screens
	s.Lock()
	defer s.Unlock()
	if sl := s.Inventory.Slots[armorSlot[piece]]; sl.Count > 0 {
		return strings.TrimPrefix(itemName(int32(sl.Item)), "minecraft:")
	}
	return ""
}

// carriedArmor es la mejor pieza de ese hueco que lleva en el inventario.
func (j *Job) carriedArmor(piece string) string {
	for _, t := range armorTiers {
		if j.Count(t+"_"+piece) > 0 {
			return t + "_" + piece
		}
	}
	return ""
}

// wearArmor se pone lo que lleve para los huecos que tiene vacíos.
func (j *Job) wearArmor() {
	for _, piece := range armorPieces {
		if j.wornIn(piece) != "" {
			continue
		}
		if it := j.carriedArmor(piece); it != "" {
			if err := j.wear(it); err != nil {
				j.Logf("armadura: %v", err)
			}
		}
	}
}

// toDimension va al mundo normal o al Nether (por el portal conocido o uno nuevo).
func (j *Job) toDimension(dim string) error {
	switch {
	case j.Dimension() == dim:
		return nil
	case dim == theNether:
		return j.goToNether()
	case j.inNether():
		return j.leaveNether()
	}
	return fmt.Errorf("no sé ir a %s", dimName(dim))
}

// dimName es el nombre de una dimensión para el log.
func dimName(dim string) string {
	switch dim {
	case theNether:
		return "el Nether"
	case overworld:
		return "el mundo normal"
	}
	return dim
}

// lootWarnAt: sin dónde guardar, con tantos de lo minado se avisa al jugador.
const lootWarnAt = 16

// makeMineChest se hace un cofre (con la madera que lleva) y lo pone junto a
// él: el cofre de minar de esta tarea (el marcado con /mine chest, lejos, no
// se borra). Sin madera, no: se intenta una vez por tarea.
func (j *Job) makeMineChest(dim string) (world.BlockPos, bool) {
	if j.chestTried {
		return world.BlockPos{}, false
	}
	j.chestTried = true
	if j.Count("chest") == 0 {
		if err := j.craftChests(1); err != nil {
			j.Logf("no hay cofre de minar cerca y no puedo hacerme uno (%v): sigo minando con lo minado encima", err)
			return world.BlockPos{}, false
		}
	}
	spot, err := j.placeNear("chest")
	if err != nil {
		j.Problem("no pude poner mi cofre de minar: %v", err)
		return world.BlockPos{}, false
	}
	j.localChest, j.localChestDim = &spot, dim
	j.Logf("no había cofre de minar cerca: me he hecho uno aquí, en %v (lo uso en esta tarea)", spot)
	return spot, true
}

// lootCheck: sin dónde guardar (storeMissing), con lootWarnAt de lo minado,
// se avisa por el chat (una vez en la tarea).
func (j *Job) lootCheck(n int, os []Ore) {
	if !j.storeMissing || j.lootWarned || n < lootWarnAt {
		return
	}
	j.lootWarned = true
	what := os[0].What
	if len(os) > 1 {
		what = "cosas minadas"
	}
	j.say(fmt.Sprintf("Llevo %d %s y no tengo dónde guardarlos: no hay cofre cerca y no tengo madera para hacer uno. ¿Me das un cofre o madera, o vienes a por ellos?", n, what))
}
