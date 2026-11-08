package automation_test

import (
	"context"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/automation"
)

// TestFarChestIgnored: el cofre de minar y la base del almacén están a miles
// de bloques (el bot se teletransportó con /tpa a otra zona). Antes iba
// andando hacia ellos al guardar; ahora no los usa: termina aquí, sin irse.
func TestFarChestIgnored(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester diamond_shovel 1", "give Tester diamond_pickaxe 1",
		}})
		r, _ := safeRunner(t, g)
		far := world.BlockPos{X: -347, Y: 83, Z: 9155}
		r.Store.SetMineChest("minecraft:overworld", &far)
		r.Store.SetBase(world.BlockPos{X: -133, Y: 63, Z: 9025})
		g.Step(5)
		box := automation.NewBox(world.BlockPos{X: 3, Y: 63, Z: 3}, world.BlockPos{X: 6, Y: 62, Z: 6})
		var err error
		r.Start(context.Background(), "excavar", func(j *automation.Job) error {
			err = automation.Excavate(box)(j)
			return err
		})
		ticks := 0
		g.Until(600*20, func() bool { ticks++; return r.Status() == "" })
		p := g.B.Player.Position()
		t.Logf("error %v; el bot en %.0f %.0f %.0f; %d ticks", err, p.X, p.Y, p.Z, ticks)
		if r.Status() != "" {
			t.Fatalf("no terminó: %s", r.Status())
		}
		if p.X < -20 || p.X > 30 || p.Z < -20 || p.Z > 30 {
			t.Fatalf("se fue hacia el cofre lejano (está en %.0f %.0f)", p.X, p.Z)
		}
	})
}
