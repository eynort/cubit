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

	"cubit/internal/i18n"
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
		return false, i18n.Errorf("there are already %d chests saved (the maximum)", "ya hay %d cofres apuntados (el máximo)", excavateChestsMax)
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
	if len(j.r.Store.ExcavateChestList()) == 0 || len(j.toStore()) == 0 {
		return false
	}
	chests := j.allChests()
	if e.chestFull == nil {
		e.chestFull = map[world.BlockPos]bool{}
	}
	here := j.Feet()
	stored := 0
	for _, c := range chests {
		if len(j.toStore()) == 0 {
			break
		}
		if e.chestFull[c] || e.twin[c] {
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
		e.sameChest(c) // (un cofre doble: su otra mitad es el mismo)
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
	e.twin = nil
	if stored > 0 {
		j.Logf("excavar: guardé %d montones en los cofres", stored)
	}
	if len(j.toStore()) > 0 && !e.chestsFullTold && !final {
		e.chestsFullTold = true
		j.sayf("the chests are full (or I can't reach them). I'll keep digging, but anything good that doesn't fit stays on the ground", "los cofres están llenos (o no llego). sigo cavando, pero lo bueno que no quepa se queda en el suelo")
	}
	return stored > 0
}

// storeIn va al cofre c, lo abre y pasa lo valioso (mayúsculas + clic).
// Devuelve cuántos montones pasó y si se quedó sin sitio.
func (e *excavation) storeIn(c world.BlockPos) (int, bool, error) {
	j := e.j
	if b := j.Block(c); b != "" && !IsContainer(b) {
		return 0, false, i18n.Errorf("there's no chest there (there's %s)", "ahí no hay un cofre (hay %s)", strings.TrimPrefix(b, "minecraft:"))
	}
	j.StatusT("storing in the chest at %v", "guardando en el cofre %v", c)
	if err := j.Open(c); err != nil {
		return 0, false, err
	}
	m, ok := j.r.d.Screens.Open()
	if !ok || m.PlayerStart() < 0 {
		return 0, false, fmt.Errorf("no se abrió")
	}
	start := m.PlayerStart()
	e.lastSize = start
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

// allChests son los cofres donde guardar o de donde coger, del más cercano
// al más lejano: los apuntados (o, si en ese sitio no hay cofre, el que esté
// pegado a él: un bloque de error al dar las coordenadas) y todos los
// contenedores pegados a cada uno (una pared de cofres se da con uno solo).
func (j *Job) allChests() []world.BlockPos {
	seen := map[world.BlockPos]bool{}
	var out []world.BlockPos
	for _, c := range j.r.Store.ExcavateChestList() {
		start, ok := c, IsContainer(j.Block(c))
		if !ok && j.Block(c) != "" {
			for _, d := range neighbours26 {
				n := world.BlockPos{X: c.X + d[0], Y: c.Y + d[1], Z: c.Z + d[2]}
				if IsContainer(j.Block(n)) {
					start, ok = n, true
					break
				}
			}
		}
		if !ok {
			if !seen[c] {
				seen[c] = true
				out = append(out, c) // (no se ve desde aquí: se va y se mira allí)
			}
			continue
		}
		// la pared: contenedores pegados (caras), hasta 6 bloques del dado
		queue := []world.BlockPos{start}
		for len(queue) > 0 && len(out) < 128 {
			p := queue[0]
			queue = queue[1:]
			if seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
			for _, d := range [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
				n := world.BlockPos{X: p.X + d[0], Y: p.Y + d[1], Z: p.Z + d[2]}
				if !seen[n] && abs(n.X-start.X) <= 6 && abs(n.Y-start.Y) <= 6 && abs(n.Z-start.Z) <= 6 && IsContainer(j.Block(n)) {
					queue = append(queue, n)
				}
			}
		}
	}
	here := j.Feet()
	sort.SliceStable(out, func(a, b int) bool { return dist2(out[a], here) < dist2(out[b], here) })
	return out
}

// neighbours26: los 26 bloques de alrededor (los de las caras primero).
var neighbours26 = func() [][3]int {
	out := [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			for dz := -1; dz <= 1; dz++ {
				if abs(dx)+abs(dy)+abs(dz) >= 2 {
					out = append(out, [3]int{dx, dy, dz})
				}
			}
		}
	}
	return out
}()

// sameChest: tras abrir c, si era un cofre doble (54 casillas), su otra
// mitad (el cofre pegado de lado) es el mismo: no se vuelve a abrir.
func (e *excavation) sameChest(c world.BlockPos) {
	if e.twin == nil {
		e.twin = map[world.BlockPos]bool{}
	}
	if e.lastSize < 54 {
		return
	}
	for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		n := world.BlockPos{X: c.X + d[0], Y: c.Y, Z: c.Z + d[1]}
		if strings.Contains(e.j.Block(n), "chest") {
			e.twin[n] = true
		}
	}
}

// fetchPicks: sin picos que valgan, los busca en los cofres de /excavate y
// coge hasta dos (los de más usos). Dice si cogió alguno.
func (e *excavation) fetchPicks() bool {
	j := e.j
	if len(j.r.Store.ExcavateChestList()) == 0 {
		return false
	}
	if j.FreeSlots() == 0 {
		e.deposit(true) // (sitio para el pico)
	}
	e.twin = nil
	got := 0
	for _, c := range j.allChests() {
		if got >= 2 || j.FreeSlots() == 0 {
			break
		}
		if e.twin[c] || dist2(c, j.Feet()) > chestFar*chestFar {
			continue
		}
		if b := j.Block(c); b != "" && !IsContainer(b) {
			continue
		}
		j.StatusT("looking for pickaxes in the chest at %v", "buscando picos en el cofre %v", c)
		if err := j.Open(c); err != nil {
			j.Logf("excavar: no pude abrir el cofre %v: %v", c, err)
			continue
		}
		m, ok := j.r.d.Screens.Open()
		if !ok || m.PlayerStart() < 0 {
			continue
		}
		e.lastSize = m.PlayerStart()
		type pick struct{ slot, left int }
		var picks []pick
		for i := 0; i < m.PlayerStart(); i++ {
			if left := j.pickLeft(types.ItemStack(m.Slots[i])); m.Slots[i].Count > 0 && left > pickReserve {
				picks = append(picks, pick{i, left})
			}
		}
		sort.Slice(picks, func(a, b int) bool { return picks[a].left > picks[b].left })
		for _, p := range picks {
			if got >= 2 || j.FreeSlots() == 0 {
				break
			}
			if err := j.r.d.Screens.Click(m.ID, p.slot, 0, types.ContainerInputQuickMove); err != nil {
				break
			}
			_ = j.Wait(2)
			got++
		}
		e.sameChest(c)
	}
	j.CloseScreen()
	e.twin = nil
	if got > 0 {
		j.Logf("excavar: cogí %d picos de los cofres", got)
		j.pickAsked = false
	}
	return got > 0
}
