package automation

import (
	"fmt"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/data/tags"
	"github.com/mj41/go-mc26/level/component"
	"github.com/mj41/go-mc26/protocol/types"
)

// Los objetos se nombran sin "minecraft:" ("oak_log").

func itemName(id int32) string {
	if id >= 0 && int(id) < len(registryid.Item) {
		return registryid.Item[id]
	}
	return ""
}

func full(item string) string {
	if strings.Contains(item, ":") {
		return item
	}
	return "minecraft:" + item
}

// slots del menú del inventario: 9..35 el inventario, 36..44 la barra.
const (
	invStart = 9
	hotbar   = 36
	invEnd   = 44
)

// Count es cuántos item lleva.
func (j *Job) Count(item string) int {
	return j.CountWhere(func(name string) bool { return name == item })
}

// CountWhere es cuántos objetos lleva cuyo nombre cumple ok.
func (j *Job) CountWhere(ok func(name string) bool) int {
	s := j.r.d.Screens
	s.Lock()
	defer s.Unlock()
	n := 0
	for i := invStart; i <= invEnd; i++ {
		sl := s.Inventory.Slots[i]
		if sl.Count > 0 && ok(strings.TrimPrefix(itemName(int32(sl.Item)), "minecraft:")) {
			n += int(sl.Count)
		}
	}
	return n
}

// Items son los objetos que lleva y cuántos de cada uno.
func (j *Job) Items() map[string]int {
	s := j.r.d.Screens
	s.Lock()
	defer s.Unlock()
	out := map[string]int{}
	for i := invStart; i <= invEnd; i++ {
		sl := s.Inventory.Slots[i]
		if sl.Count > 0 {
			out[strings.TrimPrefix(itemName(int32(sl.Item)), "minecraft:")] += int(sl.Count)
		}
	}
	return out
}

// FreeSlots son los huecos libres del inventario.
func (j *Job) FreeSlots() int {
	s := j.r.d.Screens
	s.Lock()
	defer s.Unlock()
	n := 0
	for i := invStart; i <= invEnd; i++ {
		if s.Inventory.Slots[i].Count <= 0 {
			n++
		}
	}
	return n
}

// slotOf es el slot del menú con item (-1: no lo lleva); en la barra primero.
func (j *Job) slotOf(item string) int {
	s := j.r.d.Screens
	s.Lock()
	defer s.Unlock()
	want := full(item)
	for _, rng := range [][2]int{{hotbar, invEnd}, {invStart, hotbar - 1}} {
		for i := rng[0]; i <= rng[1]; i++ {
			if sl := s.Inventory.Slots[i]; sl.Count > 0 && itemName(int32(sl.Item)) == want {
				return i
			}
		}
	}
	return -1
}

// Holding es lo que tiene en la mano.
func (j *Job) Holding() string {
	s := j.r.d.Screens
	s.Lock()
	defer s.Unlock()
	return strings.TrimPrefix(itemName(int32(s.Inventory.Slots[hotbar+s.HeldSlot].Item)), "minecraft:")
}

// Hold se pone item en la mano (pasándolo a la barra si hace falta).
func (j *Job) Hold(item string) error {
	i := j.slotOf(item)
	if i < 0 {
		return fmt.Errorf("no llevo %s", item)
	}
	j.CloseScreen()
	if i < hotbar { // del inventario a la barra, al último hueco (el 8)
		if err := j.r.d.Screens.Click(0, i, 8, types.ContainerInputSwap); err != nil {
			return err
		}
		if err := j.Wait(3); err != nil {
			return err
		}
		i = hotbar + 8
	}
	s := j.r.d.Screens
	s.Lock()
	held := s.HeldSlot
	s.Unlock()
	if held != i-hotbar {
		if err := s.SelectHotbar(i - hotbar); err != nil {
			return err
		}
		return j.Wait(1)
	}
	return nil
}

// tiers de herramienta, de mejor a peor.
var tiers = []string{"netherite", "diamond", "iron", "stone", "golden", "wooden"}

// toolFor es el tipo de herramienta que rompe antes el bloque ("" la mano):
// la del juego (las etiquetas minecraft:mineable/…: tierra, pala; madera,
// hacha; piedra, pico; hojas, azada) y, si no la trae, por el nombre.
func toolFor(block string) string {
	for _, kind := range []string{"pickaxe", "shovel", "axe", "hoe"} {
		if tags.BlockHas("minecraft:mineable/"+kind, full(block)) {
			return kind
		}
	}
	switch {
	case strings.HasSuffix(block, "_log"), strings.HasSuffix(block, "_wood"), strings.HasSuffix(block, "_stem"),
		strings.HasSuffix(block, "_hyphae"), strings.HasSuffix(block, "_planks"), strings.Contains(block, "chest"),
		block == "crafting_table":
		return "axe"
	case block == "ancient_debris", block == "obsidian", block == "crying_obsidian", block == "magma_block",
		block == "netherite_block", block == "respawn_anchor":
		// (sin pico no sueltan nada: los escombros, con la mano, eran 150 s para nada)
		return "pickaxe"
	case strings.Contains(block, "stone"), strings.HasSuffix(block, "_ore"), strings.Contains(block, "deepslate"),
		block == "netherrack", block == "furnace", strings.HasSuffix(block, "_bricks"), block == "andesite",
		block == "diorite", block == "granite", block == "tuff", block == "blackstone", block == "basalt":
		return "pickaxe"
	case block == "dirt", block == "grass_block", block == "sand", block == "gravel", block == "clay",
		block == "coarse_dirt", block == "podzol", block == "mud", block == "soul_sand", block == "soul_soil", block == "snow_block":
		return "shovel"
	case strings.HasSuffix(block, "_leaves"):
		return "hoe" // (las hojas: la azada es la herramienta rápida en el juego actual)
	}
	return ""
}

// BestTool es la mejor herramienta que lleva para romper el bloque ("" ninguna).
func (j *Job) BestTool(block string) string {
	kind := toolFor(block)
	if kind == "" {
		return ""
	}
	items := j.Items()
	for _, t := range tiers {
		if items[t+"_"+kind] > 0 {
			return t + "_" + kind
		}
	}
	return ""
}

// HoldBestTool se pone en la mano la mejor herramienta para el bloque: la
// suya del mejor material que lleve (con más de toolFloor usos: no se rompe
// ninguna); si no la lleva, la mano (otra herramienta no rompe más rápido y
// se gastaría: un pico en tierra pierde usos igual).
func (j *Job) HoldBestTool(block string) {
	kind := toolFor(block)
	if kind == "pickaxe" {
		// el pico bueno con durabilidad de sobra (no el primero que salga)
		if i := j.bestPickSlot(j.pickFloor); i >= 0 {
			_ = j.holdSlot(i)
			return
		}
		if j.pickFloor > 0 && j.CountWhere(j.isPick) > 0 {
			return // solo quedan gastados: no se rompen (Dig lo dice)
		}
	}
	if kind != "" {
		floor := j.toolFloor
		if kind == "pickaxe" {
			floor = j.pickFloor
		}
		if i := j.toolSlot(kind, floor); i >= 0 {
			_ = j.holdSlot(i)
			return
		}
	}
	j.holdHand()
}

// toolSlot es el slot de la mejor herramienta de ese tipo (netherita,
// diamante, hierro…) con más de floor usos; -1 si no hay.
func (j *Job) toolSlot(kind string, floor int) int {
	s := j.r.d.Screens
	s.Lock()
	defer s.Unlock()
	for _, t := range tiers {
		name := t + "_" + kind
		ok := func(n string) bool { return n == name }
		for _, rng := range [][2]int{{hotbar, invEnd}, {invStart, hotbar - 1}} {
			for i := rng[0]; i <= rng[1]; i++ {
				if l := pickLeftAs(s.Inventory.Slots[i], ok); l >= 0 && (floor <= 0 || l > floor) {
					return i
				}
			}
		}
	}
	return -1
}

// wears: el objeto pierde usos al romper bloques (una herramienta, un arma).
func wears(s types.ItemStack) bool {
	return s.Count > 0 && act.Component[*component.MaxDamage](s) != nil
}

// holdHand cambia a una casilla de la barra que no se gaste (vacía, o un
// bloque…): romper con la mano, sin gastar ninguna herramienta.
func (j *Job) holdHand() {
	s := j.r.d.Screens
	s.Lock()
	cur := s.Inventory.Slots[hotbar+s.HeldSlot]
	pick := -1
	if wears(cur) {
		for k := 0; k < 9; k++ {
			sl := s.Inventory.Slots[hotbar+k]
			if sl.Count <= 0 {
				pick = k
				break
			}
			if pick < 0 && !wears(sl) {
				pick = k
			}
		}
	}
	s.Unlock()
	if pick >= 0 {
		_ = j.holdSlot(hotbar + pick)
	}
}
