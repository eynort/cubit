package automation_test

import (
	"context"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"

	"minecraft-bot/automation"
)

// TestExcavateClimbsOutOfPit: el bot está metido en un hoyo de la caja, por
// debajo de la pasada que toca, y sin bloques para hacer torres (como en una
// zona donde el servidor no deja construir). Antes se quedaba quieto
// probando sitios a los que no llegaba andando; ahora sube cavando una
// escalera y vacía la pasada.
func TestExcavateClimbsOutOfPit(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"give Tester diamond_pickaxe 1", "give Tester diamond_shovel 1",
			"fill 3 54 3 12 63 12 minecraft:stone",
			"fill 7 55 7 7 58 7 minecraft:air", // el hoyo: 4 de hondo, de 1x1
		}})
		r, _ := safeRunner(t, g)
		g.Step(5)
		g.Teleport(7.5, 55, 7.5) // dentro del hoyo
		g.Step(20)
		box := automation.NewBox(world.BlockPos{X: 3, Y: 63, Z: 3}, world.BlockPos{X: 12, Y: 60, Z: 12})
		var err error
		r.Start(context.Background(), "excavar", func(j *automation.Job) error {
			err = automation.Excavate(box)(j)
			return err
		})
		ticks := 0
		g.Until(1500*20, func() bool { ticks++; return r.Status() == "" })
		left := 0
		for x := 3; x <= 12; x++ {
			for y := 60; y <= 63; y++ {
				for z := 3; z <= 12; z++ {
					if !block.IsAir(g.S.World.At(x, y, z)) {
						left++
					}
				}
			}
		}
		t.Logf("error %v; quedan %d; %d ticks (%.0f s)", err, left, ticks, float64(ticks)/20)
		if err != nil {
			t.Fatalf("excavar: %v", err)
		}
		if left > 6 {
			t.Errorf("quedan %d bloques en la pasada", left)
		}
	})
}
