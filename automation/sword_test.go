package automation_test

import (
	"context"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"

	"minecraft-bot/automation"
)

// TestWoodenSword: con troncos y una mesa, lo primero es una espada de madera.
func TestWoodenSword(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{"give Tester spruce_log 3", "give Tester stick 1", "give Tester spruce_planks 1"}})
		r, _ := safeRunner(t, g)
		g.Step(5)
		r.Start(context.Background(), "espada", func(j *automation.Job) error {
			j.EnsureSword()
			return nil
		})
		g.Until(60*20, func() bool { return r.Status() == "" })
		if g.Count("minecraft:wooden_sword") == 0 {
			t.Fatalf("no hizo la espada")
		}
	})
}
