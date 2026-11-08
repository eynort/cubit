package automation

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/component"
	"github.com/mj41/go-mc26/protocol/types"
)

// Comida de los cofres: antes de salir a cazar, el bot mira en los cofres que
// puede usar y coge lo que le hace falta (no vacía el cofre). Solo estos:
//   - los cofres de comida que marcó el operador (/foodchest);
//   - los del almacén del bot que guardan comida;
//   - los que estén a menos de foodBaseRadius de la base (/base here).
// Un cofre cualquiera del mundo es de otro jugador: abrirlo para llevarse su
// comida es robar, y no se toca.

const (
	foodBaseRadius = 16  // los cofres alrededor de la base cuentan como de casa
	foodChestFar   = 160 // más lejos que esto no se va solo por comida
	foodChestWant  = 16  // lo que se coge: comida para un buen rato
	foodChestEmpty = 5 * time.Minute
)

// emptyFoodChests: los cofres en los que no había comida, y cuándo se miró
// (no se vuelve a ir a ellos en un rato).
var emptyFoodChests = struct {
	sync.Mutex
	at map[world.BlockPos]time.Time
}{at: map[world.BlockPos]time.Time{}}

func noteFoodChest(p world.BlockPos, empty bool) {
	emptyFoodChests.Lock()
	defer emptyFoodChests.Unlock()
	if empty {
		emptyFoodChests.at[p] = time.Now()
	} else {
		delete(emptyFoodChests.at, p)
	}
}

func foodChestEmptyLately(p world.BlockPos) bool {
	emptyFoodChests.Lock()
	defer emptyFoodChests.Unlock()
	t, ok := emptyFoodChests.at[p]
	return ok && time.Since(t) < foodChestEmpty
}

// AddFoodChest apunta p como cofre de comida (false: ya estaba).
func (s *Storage) AddFoodChest(p world.BlockPos) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, q := range s.FoodChests {
		if q == p {
			return false
		}
	}
	s.FoodChests = append(s.FoodChests, p)
	s.save()
	noteFoodChest(p, false)
	return true
}

// RemoveFoodChest olvida el cofre de comida p (false: no estaba).
func (s *Storage) RemoveFoodChest(p world.BlockPos) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, q := range s.FoodChests {
		if q == p {
			s.FoodChests = append(s.FoodChests[:i], s.FoodChests[i+1:]...)
			s.save()
			return true
		}
	}
	return false
}

// ClearFoodChests olvida todos los cofres de comida.
func (s *Storage) ClearFoodChests() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.FoodChests = nil
	s.save()
}

// FoodChestList son los cofres de comida marcados.
func (s *Storage) FoodChestList() []world.BlockPos {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]world.BlockPos(nil), s.FoodChests...)
}

// HasFoodSources: hay algún sitio donde mirar (cofres marcados, almacén o base).
func (s *Storage) HasFoodSources() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.FoodChests) > 0 || s.Base != nil
}

// foodChestCandidates son los cofres donde se puede buscar comida, del más
// cercano al más lejano: los marcados, los del almacén con comida y los de
// alrededor de la base (solo en el mundo normal).
func (j *Job) foodChestCandidates() []world.BlockPos {
	if j.Dimension() != overworld {
		return nil
	}
	s := j.r.Store
	seen := map[world.BlockPos]bool{}
	var out []world.BlockPos
	add := func(p world.BlockPos) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	s.mu.Lock()
	for _, p := range s.FoodChests {
		add(p)
	}
	for _, c := range s.Chests {
		if _, ok := goodFood(strings.TrimPrefix(c.Item, "minecraft:")); ok {
			add(c.Pos)
		}
	}
	var base *world.BlockPos
	if s.Base != nil {
		b := *s.Base
		base = &b
	}
	s.mu.Unlock()
	if base != nil {
		r := foodBaseRadius
		for dx := -r; dx <= r; dx++ {
			for dy := -6; dy <= 6; dy++ {
				for dz := -r; dz <= r; dz++ {
					q := world.BlockPos{X: base.X + dx, Y: base.Y + dy, Z: base.Z + dz}
					if st, ok := j.r.d.World.BlockAt(q); ok && isChest(blockName(st)) {
						add(q)
					}
				}
			}
		}
	}
	feet := j.Feet()
	dist := func(p world.BlockPos) float64 {
		return math.Hypot(float64(p.X-feet.X), float64(p.Z-feet.Z))
	}
	sort.Slice(out, func(a, b int) bool { return dist(out[a]) < dist(out[b]) })
	keep := out[:0]
	for _, p := range out {
		if dist(p) <= foodChestFar && !foodChestEmptyLately(p) {
			keep = append(keep, p)
		}
	}
	return keep
}

// FoodFromChests va a los cofres de comida (del más cercano al más lejano)
// hasta llevar want piezas; dice cuántas cogió. Sin cofres donde mirar, o
// sin comida en ellos, 0 y sin error (se caza como siempre).
func (j *Job) FoodFromChests(want int) (int, error) {
	got := 0
	j.foodSeen = 0
	cands := j.foodChestCandidates()
	// la base sin cargar (reapareció lejos, en el spawn): sus cofres no se ven
	// desde aquí. Antes, "no hay comida en los cofres" sin mirar ninguno
	if base, far := j.baseUnloaded(); far && len(cands) == 0 {
		j.Status("a la base (%v) a mirar sus cofres de comida", base)
		j.Logf("la base (%v) no está cargada: voy a mirar sus cofres", base)
		if err := j.GoNear(base); err != nil {
			if j.ctx.Err() != nil {
				return 0, j.ctx.Err()
			}
			j.Logf("no llego a la base: %v", err)
		}
		cands = j.foodChestCandidates()
	}
	for _, c := range cands {
		if j.FoodItems() >= want {
			break
		}
		if n := j.Block(c); n != "" && !isChest(n) {
			j.Logf("cofre de comida en %v: ya no hay un cofre (hay %s)", c, n)
			noteFoodChest(c, true)
			continue
		}
		j.Status("a por comida al cofre de %v", c)
		j.foodSeen++
		n, err := j.takeFoodFrom(c, want)
		if err != nil {
			if j.ctx.Err() != nil {
				return got, j.ctx.Err()
			}
			j.Logf("cofre de comida en %v: %v", c, err)
			noteFoodChest(c, true)
			continue
		}
		noteFoodChest(c, n == 0)
		got += n
	}
	if got > 0 {
		j.Logf("comida de los cofres: cogí %d (llevo %d)", got, j.FoodItems())
	}
	return got, nil
}

// takeFoodFrom abre el cofre c y coge comida, la que más alimenta primero,
// montón a montón, hasta llevar want piezas (el resto se queda: es de todos).
func (j *Job) takeFoodFrom(c world.BlockPos, want int) (int, error) {
	if err := j.Open(c); err != nil {
		return 0, err
	}
	defer j.CloseScreen()
	m, ok := j.r.d.Screens.Open()
	if !ok {
		return 0, errors.New("no se abrió")
	}
	type stack struct {
		slot, n, value int
		item           string
	}
	var stacks []stack
	for i := 0; i < m.PlayerStart(); i++ {
		sl := m.Slots[i]
		if sl.Count <= 0 {
			continue
		}
		it := strings.TrimPrefix(itemName(int32(sl.Item)), "minecraft:")
		if v, ok := foodValue(it, sl); ok {
			stacks = append(stacks, stack{slot: i, n: int(sl.Count), value: v, item: it})
		}
	}
	if len(stacks) == 0 {
		j.Logf("cofre de comida en %v: no hay comida", c)
		return 0, nil
	}
	sort.SliceStable(stacks, func(a, b int) bool { return stacks[a].value > stacks[b].value })
	before := j.FoodItems()
	for _, s := range stacks {
		need := want - j.FoodItems()
		if need <= 0 {
			break
		}
		if s.n > need {
			if ok, err := j.takePart(m.ID, s.slot, need); err != nil {
				return j.FoodItems() - before, err
			} else if ok {
				continue
			}
		}
		if err := j.r.d.Screens.Click(m.ID, s.slot, 0, types.ContainerInputQuickMove); err != nil {
			return j.FoodItems() - before, err
		}
		if err := j.Wait(3); err != nil {
			return j.FoodItems() - before, err
		}
	}
	got := j.FoodItems() - before
	if got <= 0 {
		return 0, fmt.Errorf("no pude coger la comida (¿inventario lleno?)")
	}
	return got, nil
}

// takePart coge n del montón del slot del cofre abierto (no el montón
// entero), como una persona: lo coge con el cursor, deja n en un hueco de su
// inventario (clic derecho: de uno en uno) y devuelve el resto a su sitio.
// false: sin hueco libre en el inventario (se coge el montón entero).
func (j *Job) takePart(id, slot, n int) (bool, error) {
	m, ok := j.r.d.Screens.Open()
	if !ok || m.ID != id {
		return false, errors.New("el cofre se cerró")
	}
	empty := -1
	for i := m.PlayerStart(); i < len(m.Slots); i++ {
		if m.Slots[i].Count <= 0 {
			empty = i
			break
		}
	}
	if empty < 0 {
		return false, nil
	}
	sc := j.r.d.Screens
	if err := sc.Click(id, slot, 0, types.ContainerInputPickup); err != nil {
		return true, err
	}
	if err := j.Wait(1); err != nil {
		return true, err
	}
	for k := 0; k < n; k++ {
		if err := sc.Click(id, empty, 1, types.ContainerInputPickup); err != nil {
			return true, err
		}
	}
	if err := j.Wait(1); err != nil {
		return true, err
	}
	if err := sc.Click(id, slot, 0, types.ContainerInputPickup); err != nil { // el resto, a su sitio
		return true, err
	}
	return true, j.Wait(3)
}

// foodValue: lo que alimenta el objeto de una casilla, si es comida buena:
// por la lista, o por su componente food (comida que la lista no conoce).
func foodValue(it string, sl screen.Slot) (int, bool) {
	if badFood[it] {
		return 0, false
	}
	if n, ok := goodFood(it); ok {
		return n, true
	}
	if c := act.Component[*component.Food](sl); c != nil && c.Nutrition > 0 {
		return int(c.Nutrition), true
	}
	return 0, false
}

// FoodRestock es la tarea de ir a los cofres a por comida (sin cazar): para
// cuando no lleva nada de comer aunque aún no tenga hambre.
func FoodRestock() func(j *Job) error {
	return func(j *Job) error {
		n, err := j.FoodFromChests(foodChestWant)
		if err != nil {
			return err
		}
		if n == 0 && j.foodSeen == 0 {
			return errors.New("no conozco cofres de comida a mano (márcalos con /foodchest, o /base here junto a ellos)")
		}
		if n == 0 {
			return fmt.Errorf("miré %d cofres de comida y no había", j.foodSeen)
		}
		return nil
	}
}

// baseUnloaded: hay base, a una distancia a la que se va por comida, y su
// zona no está cargada (sus cofres no se pueden buscar desde aquí).
func (j *Job) baseUnloaded() (world.BlockPos, bool) {
	if j.Dimension() != overworld {
		return world.BlockPos{}, false
	}
	s := j.r.Store
	s.mu.Lock()
	var base *world.BlockPos
	if s.Base != nil {
		b := *s.Base
		base = &b
	}
	s.mu.Unlock()
	if base == nil {
		return world.BlockPos{}, false
	}
	feet := j.Feet()
	if math.Hypot(float64(base.X-feet.X), float64(base.Z-feet.Z)) > foodChestFar {
		return *base, false
	}
	if _, ok := j.r.d.World.BlockAt(*base); ok {
		return *base, false
	}
	return *base, true
}
