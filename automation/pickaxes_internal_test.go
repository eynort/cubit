package automation

import (
	"slices"
	"testing"

	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/component"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/types"
)

func pick(name string, damage int) types.ItemStack {
	s := types.ItemStack{Count: 1, Item: pk.VarInt(slices.Index(registryid.Item, "minecraft:"+name))}
	if damage > 0 {
		s.Components.Positive = append(s.Components.Positive, component.Typed{Value: &component.Damage{Value: pk.VarInt(damage)}})
	}
	return s
}

// TestPickLeft: los usos que le quedan a un pico salen de su daño y su
// durabilidad (1561 el de diamante, 2031 el de netherita).
func TestPickLeft(t *testing.T) {
	for _, c := range []struct {
		s    types.ItemStack
		want int
	}{
		{pick("diamond_pickaxe", 0), 1561},
		{pick("diamond_pickaxe", 1530), 31},
		{pick("netherite_pickaxe", 31), 2000},
		{pick("iron_pickaxe", 0), -1}, // no saca escombros
		{pick("netherrack", 0), -1},
	} {
		if got := pickLeft(c.s); got != c.want {
			t.Errorf("%s: %d usos, se esperaba %d", itemName(int32(c.s.Item)), got, c.want)
		}
	}
	if l := pickLeft(pick("diamond_pickaxe", 1530)); l > pickReserve {
		t.Errorf("un pico con %d usos debería estar por debajo de la reserva (%d)", l, pickReserve)
	}
}

func TestToolForDebris(t *testing.T) {
	for _, b := range []string{"ancient_debris", "obsidian", "netherrack", "magma_block"} {
		if toolFor(b) != "pickaxe" {
			t.Errorf("%s: %q, se esperaba pico", b, toolFor(b))
		}
	}
}

// TestToolFor: la herramienta de cada bloque, la del juego: tierra pala,
// madera hacha, piedra pico, hojas azada; el cristal, la mano.
func TestToolFor(t *testing.T) {
	for b, want := range map[string]string{
		"dirt": "shovel", "grass_block": "shovel", "sand": "shovel", "gravel": "shovel", "rooted_dirt": "shovel",
		"oak_log": "axe", "spruce_planks": "axe", "oak_fence": "axe", "bookshelf": "axe",
		"stone": "pickaxe", "deepslate": "pickaxe", "iron_ore": "pickaxe", "bricks": "pickaxe", "terracotta": "pickaxe",
		"oak_leaves": "hoe", "hay_block": "hoe",
		"glass": "", "white_wool": "",
	} {
		if got := toolFor(b); got != want {
			t.Errorf("%s: %q, se esperaba %q", b, got, want)
		}
	}
}

// TestLiquidState: agua y lava, y lo que lleva agua dentro, sí; las hojas,
// losas o escaleras que podrían llevarla pero no la llevan, no (antes sí:
// "waterlogged=false" contiene "water", y junto a un árbol no se cavaba).
func TestLiquidState(t *testing.T) {
	for s, want := range map[string]bool{
		"minecraft:water[level=0]": true, "minecraft:lava[level=4]": true, "minecraft:bubble_column[drag=true]": true,
		"minecraft:kelp_plant": true, "minecraft:oak_leaves[distance=1,persistent=false,waterlogged=true]": true,
		"minecraft:azalea_leaves[distance=1,persistent=false,waterlogged=false]":           false,
		"minecraft:flowering_azalea_leaves[distance=2,persistent=false,waterlogged=false]": false,
		"minecraft:stone_slab[type=bottom,waterlogged=false]":                              false,
		"minecraft:stone": false, "minecraft:water_cauldron[level=3]": false,
	} {
		if got := liquidState(s); got != want {
			t.Errorf("%s: %v, se esperaba %v", s, got, want)
		}
	}
}
