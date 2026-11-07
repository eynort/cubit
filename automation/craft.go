package automation

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// Las recetas se hacen a mano (CraftByHand: ingrediente a ingrediente en la
// cuadrícula), sin depender de que el servidor las haya desbloqueado.

func isLog(item string) bool {
	return (strings.HasSuffix(item, "_log") || strings.HasSuffix(item, "_stem")) && !strings.Contains(item, "mushroom")
}

func isPlanks(item string) bool { return strings.HasSuffix(item, "_planks") }

// planksOf son los tablones que da un tronco (oak_log, stripped_oak_log → oak_planks).
func planksOf(log string) string {
	l := strings.TrimPrefix(log, "stripped_")
	l = strings.TrimSuffix(strings.TrimSuffix(l, "_log"), "_stem")
	return l + "_planks"
}

// most es el objeto del que más lleva entre los que cumplen ok ("" ninguno).
func (j *Job) most(ok func(string) bool) string {
	best, n := "", 0
	for it, c := range j.Items() {
		if ok(it) && c > n {
			best, n = it, c
		}
	}
	return best
}

// craftHere hace times veces la receta en la cuadrícula abierta (la de la
// mesa, o la del inventario si no hay ninguna abierta).
func (j *Job) craftHere(cells []string, width, times int) error {
	if times <= 0 {
		return fmt.Errorf("nada que hacer (0 veces)")
	}
	full := make([]string, len(cells))
	for i, c := range cells {
		if c != "" {
			full[i] = "minecraft:" + c
		}
	}
	_, err := j.r.ui.CraftByHand(j.ctx, full, width, times)
	if err != nil {
		return err
	}
	return j.Wait(5) // que el servidor confirme el inventario antes de seguir
}

// EnsureSamePlanks consigue n tablones de un mismo tipo (las recetas de
// mesa, cofre, palos y hacha los quieren iguales) y dice de cuál: de los que
// ya lleva, o haciéndolos de los troncos de esa madera.
func (j *Job) EnsureSamePlanks(n int) (string, error) {
	items := j.Items()
	best, bestHave := "", -1
	for it, c := range items {
		if isPlanks(it) && c >= n {
			return it, nil
		}
		if isPlanks(it) || isLog(it) {
			p := it
			if isLog(it) {
				p = planksOf(it)
			}
			have := items[p]
			for l, lc := range items { // todos los troncos que dan ese tablón
				if isLog(l) && planksOf(l) == p {
					have += 4 * lc
				}
			}
			if have >= n && have > bestHave {
				best, bestHave = p, have
			}
		}
	}
	if best == "" {
		return "", fmt.Errorf("faltan %d tablones iguales (y troncos de una misma madera)", n)
	}
	for tries := 0; j.Count(best) < n && tries < 4; tries++ {
		var log string
		for l := range j.Items() {
			if isLog(l) && planksOf(l) == best {
				log = l
				break
			}
		}
		if log == "" {
			break
		}
		times := min(j.Count(log), (n-j.Count(best)+3)/4)
		j.CloseScreen()
		if err := j.craftHere([]string{log}, 1, times); err != nil {
			return "", fmt.Errorf("tablones: %w", err)
		}
	}
	if j.Count(best) < n {
		return "", fmt.Errorf("solo tengo %d de %s (hacen falta %d)", j.Count(best), best, n)
	}
	return best, nil
}

// EnsurePlanks consigue n tablones (iguales) y no dice de cuál.
func (j *Job) EnsurePlanks(n int) error {
	if j.CountWhere(isPlanks) >= n {
		return nil
	}
	_, err := j.EnsureSamePlanks(n)
	return err
}

// EnsureSticks hace palos hasta tener n.
func (j *Job) EnsureSticks(n int) error {
	have := j.Count("stick")
	if have >= n {
		return nil
	}
	times := (n - have + 3) / 4
	p, err := j.EnsureSamePlanks(2 * times)
	if err != nil {
		return fmt.Errorf("palos: %w", err)
	}
	j.CloseScreen()
	if err := j.craftHere([]string{p, p}, 1, times); err != nil {
		return fmt.Errorf("palos: %w", err)
	}
	return nil
}

// near busca el bloque más cercano cuyo nombre cumple ok, a menos de r bloques.
func (j *Job) near(r int, ok func(string) bool) (world.BlockPos, bool) {
	return j.nearWhere(r, ok, nil)
}

// nearWhere es near con un filtro más de la posición (nil: ninguno).
func (j *Job) nearWhere(r int, ok func(string) bool, keep func(world.BlockPos) bool) (world.BlockPos, bool) {
	f := j.Feet()
	best, bestD, found := world.BlockPos{}, math.MaxFloat64, false
	for dy := -8; dy <= 8; dy++ {
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				p := world.BlockPos{X: f.X + dx, Y: f.Y + dy, Z: f.Z + dz}
				st, okb := j.r.d.World.BlockAt(p)
				if !okb || block.IsAir(st) || !ok(blockName(st)) || keep != nil && !keep(p) {
					continue
				}
				if d := float64(dx*dx + dz*dz + 4*dy*dy); d < bestD {
					best, bestD, found = p, d, true
				}
			}
		}
	}
	return best, found
}

// FreeSpot es una celda cerca del bot donde poner un bloque (una mesa, un
// cofre): libre, con suelo firme debajo y aire encima, sin ser donde está él.
func (j *Job) FreeSpot() (world.BlockPos, bool) { return j.freeSpotExcept(nil) }

func (j *Job) freeSpotExcept(skip map[world.BlockPos]bool) (world.BlockPos, bool) {
	f := j.Feet()
	type cand struct {
		p world.BlockPos
		d int
	}
	var cs []cand
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			if dx == 0 && dz == 0 {
				continue
			}
			for dy := -1; dy <= 1; dy++ {
				cs = append(cs, cand{world.BlockPos{X: f.X + dx, Y: f.Y + dy, Z: f.Z + dz}, dx*dx + dz*dz + dy*dy})
			}
		}
	}
	sort.Slice(cs, func(a, b int) bool { return cs[a].d < cs[b].d })
	for _, c := range cs {
		if !skip[c.p] && j.canPlaceAt(c.p) {
			return c.p, true
		}
	}
	return world.BlockPos{}, false
}

// canPlaceAt: la celda está libre, con un bloque entero debajo y aire encima.
func (j *Job) canPlaceAt(p world.BlockPos) bool {
	st, ok := j.r.d.World.BlockAt(p)
	if !ok || !block.Replaceable(st) {
		return false
	}
	below, ok := j.r.d.World.BlockAt(world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z})
	if !ok || len(block.CollisionShape(below)) == 0 || block.Replaceable(below) {
		return false
	}
	above, ok := j.r.d.World.BlockAt(world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z})
	return ok && block.IsAir(above) && j.Reaches(p) && !j.inBody(p)
}

// inBody: la celda p toca el cuerpo del bot (el servidor no deja poner ahí).
func (j *Job) inBody(p world.BlockPos) bool {
	e := j.Pos()
	const hw = 0.3
	return float64(p.X) < e.X+hw && float64(p.X+1) > e.X-hw &&
		float64(p.Z) < e.Z+hw && float64(p.Z+1) > e.Z-hw &&
		float64(p.Y) < e.Y+1.8 && float64(p.Y+1) > e.Y
}

// placeNear pone item (que lleva) en un sitio libre junto al bot; si el
// servidor no lo deja en uno, prueba otro.
func (j *Job) placeNear(item string) (world.BlockPos, error) {
	skip := map[world.BlockPos]bool{}
	var last error
	for tries := 0; tries < 4; tries++ {
		spot, ok := j.freeSpotExcept(skip)
		if !ok {
			break
		}
		if last = j.Place(item, spot); last == nil {
			return spot, nil
		}
		skip[spot] = true
	}
	if last == nil {
		last = fmt.Errorf("no encuentro sitio")
	}
	return world.BlockPos{}, fmt.Errorf("poner %s: %w", item, last)
}

// tablePos es una mesa de trabajo: la que haya cerca, o una nueva que hace
// y pone. Se asegura antes que los ingredientes: la mesa gasta 4 tablones.
func (j *Job) tablePos() (world.BlockPos, error) {
	t, ok := j.near(24, func(n string) bool { return n == "crafting_table" })
	if !ok {
		if j.Count("crafting_table") == 0 {
			p, err := j.EnsureSamePlanks(4)
			if err != nil {
				return t, fmt.Errorf("mesa de trabajo: %w", err)
			}
			j.CloseScreen()
			if err := j.craftHere([]string{p, p, p, p}, 2, 1); err != nil {
				return t, fmt.Errorf("mesa de trabajo: %w", err)
			}
		}
		spot, err := j.placeNear("crafting_table")
		if err != nil {
			return t, err
		}
		t = spot
	}
	return t, nil
}

// CraftAtTable hace times veces una receta de 3x3 en una mesa de trabajo.
// prepare (puede ser nil) junta los ingredientes, después de tener la mesa.
func (j *Job) CraftAtTable(cells []string, width, times int, prepare func() error) error {
	t, err := j.tablePos()
	if err != nil {
		return err
	}
	if prepare != nil {
		if err := prepare(); err != nil {
			return err
		}
	}
	return j.craftOnTable(t, cells, width, times)
}

// stoneMaterial: lo que vale como piedra para herramientas y hornos.
func stoneMaterial(it string) bool {
	return it == "cobblestone" || it == "cobbled_deepslate" || it == "blackstone"
}

// CraftAxe hace la mejor hacha que permiten los materiales que lleva: de
// hierro (3 lingotes; o hierro en bruto que funde), de piedra (3 adoquines)
// o de madera (3 tablones). Devuelve cuál hizo.
func (j *Job) CraftAxe() (string, error) {
	if j.Count("iron_ingot") < 3 && j.Count("raw_iron") >= 3-j.Count("iron_ingot") {
		if err := j.Smelt("raw_iron", 3-j.Count("iron_ingot")); err != nil {
			j.Logf("fundir hierro: %v", err)
		}
	}
	t, err := j.tablePos() // la mesa primero: gasta tablones
	if err != nil {
		return "", err
	}
	var mat, axe string
	switch {
	case j.Count("iron_ingot") >= 3:
		mat, axe = "iron_ingot", "iron_axe"
	case j.most(stoneMaterial) != "" && j.Count(j.most(stoneMaterial)) >= 3:
		mat, axe = j.most(stoneMaterial), "stone_axe"
	default:
		axe = "wooden_axe"
	}
	if err := j.EnsureSticks(2); err != nil {
		return "", err
	}
	if mat == "" { // de madera: 3 tablones iguales (después de los palos, que gastan)
		if mat, err = j.EnsureSamePlanks(3); err != nil {
			return "", fmt.Errorf("hacha de madera: %w", err)
		}
	}
	before := j.Count(axe)
	if err := j.craftOnTable(t, []string{mat, mat, mat, "stick", "", "stick"}, 2, 1); err != nil {
		return "", fmt.Errorf("%s: %w", axe, err)
	}
	if j.Count(axe) <= before {
		return "", fmt.Errorf("%s: la mesa no la dio", axe)
	}
	return axe, nil
}

// craftOnTable abre la mesa t y hace la receta.
func (j *Job) craftOnTable(t world.BlockPos, cells []string, width, times int) error {
	if err := j.Open(t); err != nil {
		return err
	}
	defer j.CloseScreen()
	return j.craftHere(cells, width, times)
}

// Smelt funde n de input en un horno (el que haya cerca, o uno nuevo de 8
// adoquines), con carbón o madera de combustible.
func (j *Job) Smelt(input string, n int) error {
	f, ok := j.near(24, func(b string) bool { return b == "furnace" })
	if !ok {
		if j.Count("furnace") == 0 {
			mat := j.most(stoneMaterial)
			if mat == "" || j.Count(mat) < 8 {
				return fmt.Errorf("para un horno hacen falta 8 adoquines")
			}
			if err := j.CraftAtTable([]string{mat, mat, mat, mat, "", mat, mat, mat, mat}, 3, 1, nil); err != nil {
				return fmt.Errorf("horno: %w", err)
			}
		}
		spot, err := j.placeNear("furnace")
		if err != nil {
			return err
		}
		f = spot
	}
	fuel := ""
	for _, c := range []string{"coal", "charcoal"} {
		if j.Count(c) > 0 {
			fuel = c
		}
	}
	if fuel == "" {
		if err := j.EnsurePlanks((n*2 + 2) / 3); err != nil { // un tablón funde 1.5
			return fmt.Errorf("combustible: %w", err)
		}
		fuel = j.most(isPlanks)
	}
	if err := j.Open(f); err != nil {
		return err
	}
	defer j.CloseScreen()
	ctx, cancel := context.WithCancel(j.ctx)
	defer cancel()
	_, err := j.r.ui.Smelt(ctx, full(input), full(fuel), n)
	return err
}
