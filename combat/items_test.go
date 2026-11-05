package combat

import (
	"math"
	"slices"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/types"
)

func stack(t *testing.T, name string) screen.Slot {
	t.Helper()
	i := slices.Index(registryid.Item, name)
	if i < 0 {
		t.Fatalf("no existe %s", name)
	}
	return types.ItemStack{Count: 1, Item: pk.VarInt(i)}
}

func TestWeaponOf(t *testing.T) {
	cases := []struct {
		item          string
		damage, speed float64
	}{
		{"minecraft:diamond_sword", 7, 1.6},
		{"minecraft:diamond_axe", 9, 1.0},
		{"minecraft:stick", 1, 4},
	}
	for _, c := range cases {
		w := weaponOf(stack(t, c.item))
		if math.Abs(w.damage-c.damage) > 1e-6 || math.Abs(w.speed-c.speed) > 1e-6 {
			t.Errorf("%s: daño %.2f vel %.2f, se esperaba %.2f %.2f", c.item, w.damage, w.speed, c.damage, c.speed)
		}
	}
}

func TestArmor(t *testing.T) {
	a, tough := armorPiece(stack(t, "minecraft:diamond_chestplate"))
	if a != 8 || tough != 2 {
		t.Errorf("peto de diamante: armadura %.0f dureza %.0f", a, tough)
	}
	// armadura completa de diamante (20, dureza 8) contra 10 de daño: 10*(1-max(4, 20-40/16)/25)
	got := afterArmor(10, 20, 8)
	want := 10 * (1 - math.Min(20, math.Max(4, 20-4*10/(8+8.0)))/25)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("afterArmor = %.3f, se esperaba %.3f", got, want)
	}
}

func TestTimeToKill(t *testing.T) {
	// 20 de vida, golpes de 7, 1.6 por segundo: 3 golpes, 2 esperas
	if got := timeToKill(20, 7, 1.6); math.Abs(got-2/1.6) > 1e-9 {
		t.Errorf("timeToKill = %.3f", got)
	}
	if got := timeToKill(5, 7, 1.6); got != 0 {
		t.Errorf("un golpe mata: %.3f", got)
	}
}

func TestPickFood(t *testing.T) {
	foods := []food{
		{slot: 1, nutrition: 8, saturation: 12.8}, // filete
		{slot: 2, nutrition: 1, saturation: 0.6},  // galleta... casi nada
		{slot: 3, nutrition: 4, saturation: 9.6, golden: true},
	}
	if fd, ok := pickFood(foods, 10, false); !ok || fd.slot != 1 {
		t.Errorf("con hambre debería comer el filete, eligió %+v", fd)
	}
	if fd, ok := pickFood(foods, 10, true); !ok || fd.slot != 3 {
		t.Errorf("dorada: eligió %+v", fd)
	}
}
