package automation_test

import (
	"context"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"

	"minecraft-bot/automation"
)

// TestFetchDiamondDrop: un diamante se quedó en un hueco cerrado de la roca
// (sin camino andando). Lo que suelta una mena de diamante es "diamond", no
// la mena: antes no lo reconocía y se quedaba en el suelo. Cava hasta él.
func TestFetchDiamondDrop(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester diamond_pickaxe 1", "give Tester cobblestone 16",
			"fill 4 60 0 4 60 0 minecraft:air",
		}})
		r := runner(t, g)
		g.Step(5)
		if err := g.Drop("minecraft:diamond", 2, 4, 60, 0); err != nil {
			t.Fatal(err)
		}
		g.Step(40)
		var err error
		r.Start(context.Background(), "recoger", func(j *automation.Job) error {
			err = j.FetchDropsForTest("deepslate_diamond_ore")
			return err
		})
		g.Until(300*20, func() bool { return r.Status() == "" })
		p := g.B.Player.Position()
		t.Logf("error %v; el bot en %.1f %.1f %.1f; diamantes %d", err, p.X, p.Y, p.Z, g.Count("minecraft:diamond"))
		if g.Count("minecraft:diamond") < 2 {
			t.Fatalf("se dejó los diamantes en el hueco")
		}
	})
}

// TestStripTakesWallOres: menas de diamante en las paredes, el suelo y el
// techo de la primera galería: las saca todas (y recoge lo que suelta).
func TestStripTakesWallOres(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		ore := "minecraft:deepslate_diamond_ore"
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester diamond_pickaxe 1", "give Tester cobblestone 32",
			"fill 10 -52 1 10 -52 1 " + ore, "fill 10 -51 -1 10 -51 -1 " + ore,
			"fill 11 -53 0 11 -53 0 " + ore, "fill 11 -50 0 11 -50 0 " + ore,
			"fill 12 -52 1 12 -51 1 " + ore,
		}})
		r := runner(t, g)
		g.Step(5)
		var err error
		r.Start(context.Background(), "minar", func(j *automation.Job) error {
			err = j.StripMineForTest("diamond", 6)
			return err
		})
		g.Until(3000*20, func() bool { return r.Status() == "" })
		t.Logf("error %v; diamantes %d", err, g.Count("minecraft:diamond"))
		if g.Count("minecraft:diamond") < 6 {
			t.Fatalf("sacó %d de 6", g.Count("minecraft:diamond"))
		}
	})
}
