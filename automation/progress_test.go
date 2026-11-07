package automation_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/automation"
	"minecraft-bot/pathing"
)

// TestIronAxeFromNothing: sin nada en el inventario, con árboles y hierro
// enterrado (piedra debajo de la tierra): tala, pico de madera, cava piedra,
// pico de piedra, cava hasta el hierro, horno, funde y hacha de hierro.
func TestIronAxeFromNothing(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		setup := append(tree(6, 0, 5), tree(-5, 4, 5)...)
		setup = append(setup, tree(2, -7, 5)...)
		setup = append(setup, "fill 3 52 9 4 52 9 minecraft:iron_ore", "fill 3 51 9 3 51 9 minecraft:iron_ore")
		g := simworld.Start(t, simworld.Options{Setup: setup})
		b := g.B
		b.Mover.Food = func() int { return 20 }
		w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		bd := &pathing.HandsBuilder{Screens: b.Screens, Hands: b.Hands, Ctl: b.Ctl, Ctx: context.Background()}
		w.SetBuilder(bd)
		b.Ctl.Think = w.Think
		r := automation.New(automation.Deps{
			Client: b.C, Player: b.Player, World: b.World, Entities: b.Entities, Screens: b.Screens,
			Hands: b.Hands, Ctl: b.Ctl, Walker: w, Builder: bd, Fighting: func() bool { return false },
		})
		g.Step(5)
		var err error
		r.Start(context.Background(), "hacha", func(j *automation.Job) error {
			err = j.EnsureIronAxe("")
			return err
		})
		g.Until(900*20, func() bool { return r.Status() == "" })
		has := false
		b.Screens.Lock()
		for i := 9; i <= 45; i++ {
			s := b.Screens.Inventory.Slots[i]
			if s.Count > 0 && strings.HasSuffix(itemName(int32(s.Item)), "iron_axe") {
				has = true
			}
		}
		b.Screens.Unlock()
		if err != nil || !has {
			t.Fatalf("no hay hacha de hierro (error %v, estado %q)", err, r.Status())
		}
	})
}

// TestChopSetsUpBase: /chop desde cero: hacha de hierro, luego la base (mesa
// y cofre doble junto a donde empezó) y luego a talar.
func TestChopSetsUpBase(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		setup := append(tree(6, 0, 5), tree(-5, 4, 5)...)
		setup = append(setup, tree(2, -7, 5)...)
		setup = append(setup, tree(-8, -6, 5)...)
		setup = append(setup, "fill 3 52 9 4 52 9 minecraft:iron_ore", "fill 3 51 9 3 51 9 minecraft:iron_ore")
		g := simworld.Start(t, simworld.Options{Setup: setup})
		b := g.B
		b.Mover.Food = func() int { return 20 }
		w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		bd := &pathing.HandsBuilder{Screens: b.Screens, Hands: b.Hands, Ctl: b.Ctl, Ctx: context.Background()}
		w.SetBuilder(bd)
		b.Ctl.Think = w.Think
		r := automation.New(automation.Deps{
			Client: b.C, Player: b.Player, World: b.World, Entities: b.Entities, Screens: b.Screens,
			Hands: b.Hands, Ctl: b.Ctl, Walker: w, Builder: bd, Fighting: func() bool { return false },
		})
		g.Step(5)
		r.Start(context.Background(), "talar", automation.Woodcut("", 4))
		g.Until(900*20, func() bool { return r.Status() == "" })
		t.Logf("almacén: %s", r.Store.Describe())
		base := r.Store.Base
		if base == nil {
			t.Fatalf("sin base")
		}
		table, chests, double := false, 0, false
		for dx := -4; dx <= 4; dx++ {
			for dz := -1; dz <= 4; dz++ {
				for dy := -2; dy <= 2; dy++ {
					st, ok := b.World.BlockAt(world.BlockPos{X: base.X + dx, Y: base.Y + dy, Z: base.Z + dz})
					if !ok {
						continue
					}
					s := world.StateString(st)
					table = table || strings.Contains(s, "crafting_table")
					if strings.Contains(s, "minecraft:chest") {
						chests++
						double = double || strings.Contains(s, "type=left") || strings.Contains(s, "type=right")
						t.Logf("cofre: %s", s)
					}
				}
			}
		}
		if !table || chests < 2 {
			t.Errorf("base incompleta: mesa %v, cofres %d", table, chests)
		}
		if !double {
			t.Logf("(simworld no junta los cofres: no se comprueba que sea doble)")
		}
	})
}

// TestStopsWhenHurtNoFood: con 5 de vida, hambre 15 y nada de comer no se
// cura nunca: la tarea no sigue jugándose la vida, para y lo dice.
func TestStopsWhenHurtNoFood(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: tree(6, 0, 5)})
		g.Food, g.Health = 15, 5
		b := g.B
		w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		b.Ctl.Think = w.Think
		r := automation.New(automation.Deps{
			Client: b.C, Player: b.Player, World: b.World, Entities: b.Entities, Screens: b.Screens,
			Hands: b.Hands, Ctl: b.Ctl, Walker: w, Fighting: func() bool { return false },
			HasFood: func() bool { return false },
		})
		g.Step(5)
		var err error
		r.Start(context.Background(), "talar", func(j *automation.Job) error {
			err = automation.Woodcut("", 0)(j)
			return err
		})
		g.Until(30*20, func() bool { return r.Status() == "" })
		t.Logf("error: %v", err)
		if err == nil || !strings.Contains(err.Error(), "sin comida") {
			t.Errorf("siguió (o paró por otra cosa) con 5 de vida y sin comida")
		}
	})
}
