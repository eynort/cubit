package automation_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/automation"
)

// TestMineStashNoFarTrips: el cofre de minar está a 9000 bloques (el bot
// vino con /tpa). (a) Con pico y comida y nada que guardar, a ningún cofre.
// (b) Con diamantes y troncos, se hace un cofre aquí y los guarda. (c) Con
// 20 diamantes y sin madera, sigue (sin irse) y avisa una sola vez.
func TestMineStashNoFarTrips(t *testing.T) {
	for _, c := range []struct {
		name   string
		give   []string
		chest  bool // tiene que hacerse un cofre y dejar ahí los diamantes
		warned bool // tiene que avisar de que no tiene dónde guardar
	}{
		{"con todo, nada que guardar", []string{"give Tester diamond_pickaxe 1", "give Tester cooked_beef 16"}, false, false},
		{"diamantes y troncos", []string{"give Tester diamond_pickaxe 1", "give Tester cooked_beef 16", "give Tester diamond 5", "give Tester oak_log 4"}, true, false},
		{"20 diamantes sin madera", []string{"give Tester diamond_pickaxe 1", "give Tester cooked_beef 16", "give Tester diamond 20"}, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				g := simworld.Start(t, simworld.Options{Setup: c.give})
				r, _ := safeRunner(t, g)
				far := world.BlockPos{X: -347, Y: 83, Z: 9155}
				r.Store.SetMineChest("minecraft:overworld", &far)
				var said []string
				r.SetSay(func(s string) { said = append(said, s) })
				g.Step(5)
				var err error
				r.Start(context.Background(), "minar", func(j *automation.Job) error {
					for i := 0; i < 3; i++ { // (tres rondas: el aviso, una sola vez)
						if err = j.StashForTest("diamond", true); err != nil {
							return err
						}
					}
					return nil
				})
				g.Until(300*20, func() bool { return r.Status() == "" })
				p := g.B.Player.Position()
				inChest := 0
				for x := -6; x <= 6; x++ {
					for y := 62; y <= 66; y++ {
						for z := -6; z <= 6; z++ {
							inChest += g.ChestCount(x, y, z, "minecraft:diamond")
						}
					}
				}
				t.Logf("error %v; el bot en %.0f %.0f %.0f; diamantes en un cofre cercano %d, encima %d; mensajes %q",
					err, p.X, p.Y, p.Z, inChest, g.Count("minecraft:diamond"), said)
				if err != nil {
					t.Fatalf("stash: %v", err)
				}
				if p.X < -15 || p.X > 15 || p.Z < -15 || p.Z > 15 {
					t.Fatalf("se fue hacia el cofre lejano (está en %.0f %.0f)", p.X, p.Z)
				}
				if c.chest && inChest != 5 {
					t.Errorf("tenía que hacerse un cofre y guardar los 5 diamantes (hay %d)", inChest)
				}
				if c.warned != (len(said) == 1 && strings.Contains(said[0], "no tengo dónde guardarlos")) || len(said) > 1 {
					t.Errorf("avisos: %q (quería aviso: %v)", said, c.warned)
				}
			})
		})
	}
}
