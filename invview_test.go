package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/types"
)

func stack(name string, n int) types.ItemStack {
	return types.ItemStack{Item: pk.VarInt(slices.Index(registryid.Item, "minecraft:"+name)), Count: pk.VarInt(n)}
}

// TestRenderInventory: la vista tiene la armadura, las 4 filas de 9, la
// casilla en la mano y la leyenda con los nombres enteros.
func TestRenderInventory(t *testing.T) {
	slots := make([]types.ItemStack, 46)
	slots[5] = stack("diamond_helmet", 1)
	slots[8] = stack("golden_boots", 1)
	slots[9] = stack("cobblestone", 64)
	slots[36] = stack("diamond_sword", 1)
	slots[37] = stack("iron_pickaxe", 1)
	slots[44] = stack("cooked_beef", 10)
	slots[45] = stack("shield", 1)
	out := stripANSI(renderInventory("Inventory of Bot", slots, 1, "♥ 20/20"))
	t.Log("\n" + out)
	for _, want := range []string{"Diamon", "helmet", "Golden", "Cobble", "64", "Iron", "pickax", "Cooked", "10",
		"in hand: iron_pickaxe (slot 2)", "cobblestone ×64", "Crafting", "off hand", "Shield"} {
		if !strings.Contains(out, want) {
			t.Errorf("falta %q", want)
		}
	}
	if !strings.Contains(out, "┏") { // la casilla en la mano, con el borde grueso
		t.Errorf("la casilla en la mano no está resaltada")
	}
	if n := strings.Count(out, "╭"); n != 4+4+1+1+27+8 { // armadura, crafteo, resultado, mano izq., inventario, barra (menos la resaltada)
		t.Errorf("casillas: %d", n)
	}
}

// TestRenderInventoryList: la vista tiene el equipo, la barra casilla a
// casilla (la de la mano marcada), el inventario de arriba con lo suyo sumado
// (y "vacío" si no hay nada) y es estrecha.
func TestRenderInventoryList(t *testing.T) {
	slots := make([]types.ItemStack, 46)
	slots[5] = stack("diamond_helmet", 1)
	slots[9] = stack("cobblestone", 64)
	slots[10] = stack("cobblestone", 20)
	slots[11] = stack("raw_iron", 7)
	slots[36] = stack("diamond_sword", 1)
	slots[37] = stack("iron_pickaxe", 1)
	slots[44] = stack("cooked_beef", 10)
	slots[45] = stack("shield", 1)
	out := stripANSI(renderInventoryList("Inventory of Bot", slots, 1, "health 20/20"))
	t.Log("\n" + out)
	for _, want := range []string{"Hand        iron_pickaxe", "Helmet      diamond_helmet", "Boots       ·",
		"▶ 2 iron_pickaxe", "  3 ·", "9 cooked_beef", "×10", "Hotbar (3/9)", "Inventory (3/27)",
		"cobblestone", "×84  (2 stacks)", "raw_iron", "30 free slots"} {
		if !strings.Contains(out, want) {
			t.Errorf("falta %q", want)
		}
	}
	for _, l := range strings.Split(out, "\n") {
		if w := lipgloss.Width(l); w > 50 {
			t.Errorf("línea de %d de ancho (no cabe en el móvil): %q", w, l)
		}
	}
	// sin nada arriba: lo dice
	slots[9], slots[10], slots[11] = types.ItemStack{}, types.ItemStack{}, types.ItemStack{}
	if out := stripANSI(renderInventoryList("x", slots, 0, "")); !strings.Contains(out, "Inventory (0/27)\n    empty") {
		t.Errorf("con el inventario de arriba vacío no lo dice:\n%s", out)
	}
}
