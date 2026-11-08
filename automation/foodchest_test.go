package automation_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/automation"
)

// TestFoodFromChest: con hambre y nada que comer, y un cofre de comida
// marcado (/foodchest add) a unos bloques, no sale a cazar (la vaca de al lado
// sigue viva): va al cofre, coge la comida que más alimenta y solo la que le
// hace falta (el resto del pan se queda), y come.
func TestFoodFromChest(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester iron_sword 1", "setblock 8 64 3 minecraft:chest[facing=north,type=single,waterlogged=false]",
		}})
		r, _ := safeRunner(t, g)
		g.PutInChest(8, 64, 3, "minecraft:bread", 40)
		g.PutInChest(8, 64, 3, "minecraft:cooked_beef", 5)
		g.Step(5)
		if err := g.SummonNear("minecraft:cow", -6, 2); err != nil {
			t.Fatal(err)
		}
		if err := g.SetFood(6); err != nil {
			t.Fatal(err)
		}
		r.Store.AddFoodChest(world.BlockPos{X: 8, Y: 64, Z: 3})
		g.Step(2)
		var err error
		r.Start(context.Background(), "comida", func(j *automation.Job) error {
			err = automation.FindFood()(j)
			return err
		})
		g.Until(120*20, func() bool { return r.Status() == "" })
		bread, beef := g.Count("minecraft:bread"), g.Count("minecraft:cooked_beef")
		leftBread := g.ChestCount(8, 64, 3, "minecraft:bread")
		t.Logf("error %v; comida %d; lleva pan %d, filete %d; en el cofre quedan %d panes y %d filetes; vacas %d",
			err, g.Food, bread, beef, leftBread, g.ChestCount(8, 64, 3, "minecraft:cooked_beef"), g.Mobs())
		if err != nil {
			t.Fatal(err)
		}
		if g.Food <= 6 {
			t.Errorf("no comió (comida %d)", g.Food)
		}
		if g.Mobs() == 0 {
			t.Errorf("mató a la vaca: cazó en vez de coger la comida del cofre")
		}
		if bread+beef == 0 {
			t.Errorf("no cogió comida del cofre")
		}
		if leftBread < 20 {
			t.Errorf("vació el cofre (quedan %d panes de 40)", leftBread)
		}
	})
}

// TestFoodChestStranger: un cofre con comida que no es suyo (ni marcado, ni
// del almacén, ni junto a la base) no se toca: caza.
func TestFoodChestStranger(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester iron_sword 1", "give Tester cobblestone 8", "give Tester oak_log 6", "give Tester crafting_table 1",
			"setblock 8 64 3 minecraft:chest[facing=north,type=single,waterlogged=false]",
		}})
		r, _ := safeRunner(t, g)
		g.PutInChest(8, 64, 3, "minecraft:bread", 40)
		g.Step(5)
		if err := g.SummonNear("minecraft:cow", -6, 2); err != nil {
			t.Fatal(err)
		}
		if err := g.SetFood(6); err != nil {
			t.Fatal(err)
		}
		g.Step(2)
		r.Start(context.Background(), "comida", automation.FindFood())
		g.Until(300*20, func() bool { return r.Status() == "" })
		t.Logf("comida %d; pan en el cofre %d; vacas %d", g.Food, g.ChestCount(8, 64, 3, "minecraft:bread"), g.Mobs())
		if g.ChestCount(8, 64, 3, "minecraft:bread") != 40 {
			t.Errorf("cogió pan de un cofre que no es suyo")
		}
	})
}

// TestFoodChestByBase: un cofre a unos bloques de la base (/base here) cuenta
// como de casa, aunque no esté marcado.
func TestFoodChestByBase(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester iron_sword 1", "setblock 8 64 3 minecraft:chest[facing=north,type=single,waterlogged=false]",
		}})
		r, _ := safeRunner(t, g)
		g.PutInChest(8, 64, 3, "minecraft:baked_potato", 20)
		g.Step(5)
		r.Store.SetBase(world.BlockPos{X: 2, Y: 64, Z: 0})
		if err := g.SetFood(6); err != nil {
			t.Fatal(err)
		}
		r.Start(context.Background(), "comida", automation.FindFood())
		g.Until(120*20, func() bool { return r.Status() == "" })
		left := g.ChestCount(8, 64, 3, "minecraft:baked_potato")
		t.Logf("comida %d; patatas: lleva %d, quedan %d", g.Food, g.Count("minecraft:baked_potato"), left)
		if left == 20 {
			t.Errorf("no cogió del cofre de junto a la base")
		}
		if g.Food <= 6 {
			t.Errorf("no comió")
		}
	})
}

// TestFoodChestAllKinds: un cofre de comida con solo zanahorias doradas (y
// comida mala: carne podrida, ojos de araña): coge las zanahorias y deja lo
// malo (en el servidor dijo "no hay comida en los cofres" con zanahorias
// doradas en ellos).
func TestFoodChestAllKinds(t *testing.T) {
	for _, food := range []string{"golden_carrot", "cookie", "dried_kelp", "cooked_cod", "honey_bottle"} {
		t.Run(food, func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				g := simworld.Start(t, simworld.Options{Setup: []string{
					"give Tester iron_sword 1", "setblock 8 64 3 minecraft:chest[facing=north,type=single,waterlogged=false]",
				}})
				r, _ := safeRunner(t, g)
				g.PutInChest(8, 64, 3, "minecraft:rotten_flesh", 20)
				g.PutInChest(8, 64, 3, "minecraft:spider_eye", 5)
				g.PutInChest(8, 64, 3, "minecraft:"+food, 32)
				r.Store.AddFoodChest(world.BlockPos{X: 8, Y: 64, Z: 3})
				g.Step(5)
				var err error
				r.Start(context.Background(), "comida", func(j *automation.Job) error {
					err = automation.FoodRestock()(j)
					return err
				})
				g.Until(60*20, func() bool { return r.Status() == "" })
				t.Logf("error %v; lleva %s %d, carne podrida %d, ojos %d", err, food, g.Count("minecraft:"+food), g.Count("minecraft:rotten_flesh"), g.Count("minecraft:spider_eye"))
				if err != nil || g.Count("minecraft:"+food) == 0 {
					t.Errorf("no cogió %s del cofre", food)
				}
				if g.Count("minecraft:rotten_flesh")+g.Count("minecraft:spider_eye") > 0 {
					t.Error("cogió comida mala")
				}
			})
		})
	}
}

// TestFoodChestNoneKnown: sin cofres de comida conocidos dice eso (no "no
// hay comida en los cofres", que parecía que los había mirado).
func TestFoodChestNoneKnown(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{"give Tester iron_sword 1"}})
		r, _ := safeRunner(t, g)
		g.Step(5)
		var err error
		r.Start(context.Background(), "comida", func(j *automation.Job) error {
			err = automation.FoodRestock()(j)
			return err
		})
		g.Until(10*20, func() bool { return r.Status() == "" })
		t.Logf("error: %v", err)
		if err == nil || !strings.Contains(err.Error(), "no conozco cofres") {
			t.Errorf("mensaje: %v", err)
		}
	})
}
