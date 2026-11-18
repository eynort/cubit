package automation_test

import (
	"context"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"

	"minecraft-bot/automation"
)

// TestExcavateEntersFromPit: el bot está fuera de la caja, en un hoyo sin
// salida andando (y sin bloques para torres), y en la pasada de arriba solo
// hay trabajo en una colina al otro lado de la caja. Antes iba al borde más
// cercano de la caja (sin nada que cavar en esas capas) y, sin camino, se
// quedaba; ahora va hacia la colina, cavando si no hay camino.
func TestExcavateEntersFromPit(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"give Tester diamond_pickaxe 1", "give Tester diamond_shovel 1",
			"fill -2 58 -6 18 63 -6 minecraft:stone", // el suelo, macizo
			"fill 12 64 3 16 67 8 minecraft:stone",   // la colina, al otro lado
			"fill 0 59 0 0 63 0 minecraft:air",       // el hoyo, fuera de la caja
		}})
		r, _ := safeRunner(t, g)
		g.Step(5)
		g.Teleport(0.5, 59, 0.5)
		g.Step(20)
		box := automation.NewBox(world.BlockPos{X: 3, Y: 67, Z: 3}, world.BlockPos{X: 16, Y: 64, Z: 8})
		var err error
		r.Start(context.Background(), "excavar", func(j *automation.Job) error {
			err = automation.Excavate(box)(j)
			return err
		})
		ticks := 0
		g.Until(1500*20, func() bool { ticks++; return r.Status() == "" })
		left := 0
		for x := 12; x <= 16; x++ {
			for y := 64; y <= 67; y++ {
				for z := 3; z <= 8; z++ {
					if !block.IsAir(g.S.World.At(x, y, z)) {
						left++
					}
				}
			}
		}
		t.Logf("error %v; quedan %d de la colina; %d ticks (%.0f s)", err, left, ticks, float64(ticks)/20)
		if err != nil {
			t.Fatalf("excavar: %v", err)
		}
		if left > 4 {
			t.Errorf("quedan %d bloques de la colina", left)
		}
	})
}
