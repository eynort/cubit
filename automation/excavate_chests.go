package automation

// Los cofres de /excavate (solo de /excavate: ninguna otra cosa los usa).
// Se dan con /excavate chests <x> <y> <z> (un cofre, un barril, una caja de
// shulker; uno doble vale con cualquiera de sus dos mitades) y se guardan con
// las zonas protegidas, así que siguen ahí tras reiniciar.
//
// Cuando el inventario se llena cavando, primero se tira la basura (tierra,
// piedra, grava…) como siempre; si sigue sin sitio, va al cofre más cercano y
// guarda lo valioso (minerales, lo que no es basura). Nunca guarda lo que
// usa: herramientas, armas, armadura, comida, cubos, el tótem… ni el montón
// de bloques que se queda para tapar lava. Un cofre lleno: al siguiente. Al
// acabar la excavación, también guarda lo que lleve.

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/protocol/types"
)

// excavateChestsMax: cuántos cofres se apuntan como mucho.
const excavateChestsMax = 16

// chestFar: más lejos que esto (en bloques) no se va a un cofre a guardar.
const chestFar = 160

// AddExcavateChest apunta p como cofre de /excavate. Dice si era nuevo.
func (s *Storage) AddExcavateChest(p world.BlockPos) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, q := range s.ExcavateChests {
		if q == p {
			return false, nil
		}
	}
	if len(s.ExcavateChests) >= excavateChestsMax {
		return false, fmt.Errorf("ya hay %d cofres apuntados (el máximo)", excavateChestsMax)
	}
	s.ExcavateChests = append(s.ExcavateChests, p)
	s.save()
	return true, nil
}

// RemoveExcavateChest olvida el cofre p (o todos, con nil). Dice cuántos olvidó.
func (s *Storage) RemoveExcavateChest(p *world.BlockPos) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	var keep []world.BlockPos
	for _, q := range s.ExcavateChests {
		if p != nil && q != *p {
			keep = append(keep, q)
		}
	}
	n := len(s.ExcavateChests) - len(keep)
	s.ExcavateChests = keep
	s.save()
	return n
}

// ExcavateChestList son los cofres de /excavate.
func (s *Storage) ExcavateChestList() []world.BlockPos {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]world.BlockPos(nil), s.ExcavateChests...)
}

// IsContainer: el bloque guarda cosas (cofre, cofre trampa, barril, caja de shulker).
func IsContainer(name string) bool {
	n := strings.TrimPrefix(name, "minecraft:")
	if i := strings.IndexByte(n, '['); i >= 0 {
		n = n[:i]
	}
	return n == "chest" || n == "trapped_chest" || n == "barrel" || strings.HasSuffix(n, "shulker_box")
}

// storable: lo que se guarda en el cofre (lo valioso: ni la basura, que se
// tira, ni lo que el bot usa).
func storable(n string) bool {
	return n != "" && !excavateKeeps(n) && !junk(n)
}

// toStore: los slots del inventario con algo que guardar.
func (j *Job) toStore() []int {
	var out []int
	s := j.r.d.Screens
	s.Lock()
	defer s.Unlock()
	for i := invStart; i <= invEnd; i++ {
		sl := s.Inventory.Slots[i]
		if sl.Count > 0 && storable(strings.TrimPrefix(itemName(int32(sl.Item)), "minecraft:")) {
			out = append(out, i)
		}
	}
	return out
}

// deposit lleva lo valioso a los cofres de /excavate (el más cercano
// primero). Dice si guardó algo. final: al acabar (guarda aunque haya sitio).
func (e *excavation) deposit(final bool) bool {
	j := e.j
	chests := j.r.Store.ExcavateChestList()
	if len(chests) == 0 || len(j.toStore()) == 0 {
		return false
	}
	if e.chestFull == nil {
		e.chestFull = map[world.BlockPos]bool{}
	}
	here := j.Feet()
	sort.Slice(chests, func(a, b int) bool { return dist2(chests[a], here) < dist2(chests[b], here) })
	stored := 0
	for _, c := range chests {
		if len(j.toStore()) == 0 {
			break
		}
		if e.chestFull[c] {
			continue
		}
		if d := math.Sqrt(float64(dist2(c, here))); d > chestFar {
			if !e.farTold {
				e.farTold = true
				j.Logf("excavar: el cofre %v está a %.0f bloques: demasiado lejos para ir a guardar", c, d)
			}
			continue
		}
		n, full, err := e.storeIn(c)
		stored += n
		if err != nil {
			j.Logf("excavar: no pude guardar en el cofre %v: %v", c, err)
			e.chestFull[c] = true // (no se insiste con él en esta excavación)
			continue
		}
		if full {
			j.Logf("excavar: el cofre %v está lleno", c)
			e.chestFull[c] = true
		}
	}
	j.CloseScreen()
	if stored > 0 {
		j.Logf("excavar: guardé %d montones en los cofres", stored)
	}
	if len(j.toStore()) > 0 && !e.chestsFullTold && !final {
		e.chestsFullTold = true
		j.say("Los cofres de /excavate están llenos (o no llego a ellos): sigo cavando y tiro la basura; lo valioso que no quepa se queda en el suelo.")
	}
	return stored > 0
}

// storeIn va al cofre c, lo abre y pasa lo valioso (mayúsculas + clic).
// Devuelve cuántos montones pasó y si se quedó sin sitio.
func (e *excavation) storeIn(c world.BlockPos) (int, bool, error) {
	j := e.j
	if b := j.Block(c); b != "" && !IsContainer(b) {
		return 0, false, fmt.Errorf("ahí no hay un cofre (hay %s)", strings.TrimPrefix(b, "minecraft:"))
	}
	j.Status("guardando en el cofre %v", c)
	if err := j.Open(c); err != nil {
		return 0, false, err
	}
	m, ok := j.r.d.Screens.Open()
	if !ok || m.PlayerStart() < 0 {
		return 0, false, fmt.Errorf("no se abrió")
	}
	start := m.PlayerStart()
	moved := 0
	for _, i := range j.toStore() {
		before := j.slotCount(i)
		// (el inventario del jugador en el menú: los 27 de dentro, luego la barra)
		if err := j.r.d.Screens.Click(m.ID, start+(i-invStart), 0, types.ContainerInputQuickMove); err != nil {
			return moved, false, err
		}
		if err := j.Wait(2); err != nil {
			return moved, false, err
		}
		if after := j.slotCount(i); after < before {
			moved++
			if after > 0 {
				return moved, true, nil // (cupo una parte: lleno)
			}
		} else {
			return moved, true, nil // (no cupo nada)
		}
	}
	return moved, false, nil
}

// slotCount: cuántos hay en el slot i del inventario.
func (j *Job) slotCount(i int) int {
	s := j.r.d.Screens
	s.Lock()
	defer s.Unlock()
	return int(s.Inventory.Slots[i].Count)
}

func dist2(a, b world.BlockPos) int {
	dx, dy, dz := a.X-b.X, a.Y-b.Y, a.Z-b.Z
	return dx*dx + dy*dy + dz*dz
}
