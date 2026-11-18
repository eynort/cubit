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
