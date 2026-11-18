package automation_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"

	"minecraft-bot/automation"
)

// TestExcavateWaitsForPick: una caja de piedra y el bot sin pico: lo pide
// una vez por el chat y espera; se le da uno y sigue hasta vaciar la caja.
func TestExcavateWaitsForPick(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		defer automation.SetPickWait(20 * time.Second)()
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"fill 3 61 3 6 63 6 minecraft:stone",
		}})
		r, _ := safeRunner(t, g)
		var said []string
		r.SetSay(func(s string) { said = append(said, s) })
		g.Step(5)
		box := automation.NewBox(world.BlockPos{X: 3, Y: 63, Z: 3}, world.BlockPos{X: 6, Y: 61, Z: 6})
		var err error
		r.Start(context.Background(), "excavar", func(j *automation.Job) error {
			err = automation.Excavate(box)(j)
			return err
		})
		// hasta que lo pide (sin haber picado piedra con la mano), y entonces se le da
		asked := g.Until(60*20, func() bool { return len(said) > 0 })
		g.Step(5 * 20)
		g.Give("minecraft:diamond_pickaxe", 1)
		g.Until(1500*20, func() bool { return r.Status() == "" })
		left := 0
		for x := 3; x <= 6; x++ {
			for y := 61; y <= 63; y++ {
				for z := 3; z <= 6; z++ {
					if !block.IsAir(g.S.World.At(x, y, z)) {
						left++
					}
				}
			}
		}
		t.Logf("pidió %v; mensajes %q; error %v; quedan %d", asked, said, err, left)
		if !asked || len(said) != 1 || !strings.Contains(said[0], "pico") {
			t.Fatalf("tenía que pedir el pico una vez por el chat: %q", said)
		}
		if err != nil {
			t.Fatalf("con el pico tenía que seguir: %v", err)
		}
		if left > 4 {
			t.Errorf("quedan %d bloques en la caja", left)
		}
	})
}
