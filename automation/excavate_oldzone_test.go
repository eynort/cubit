package automation_test

import (
	"context"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"

	"minecraft-bot/automation"
)

// TestExcavateOldZoneDropped: media caja (x de 9 a 20) es un terreno
// reclamado, y una zona apuntada de antes (ancha) tapa la caja entera, lo
// libre también. Lo libre se vacía (la zona vieja no lo deja fuera) y lo
// reclamado se deja sin insistir: a la primera, cada columna.
func TestExcavateOldZoneDropped(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"effect give Tester haste 2",
		}})
		g.GiveEnchanted("minecraft:diamond_pickaxe", map[string]int{"efficiency": 5})
		g.GiveEnchanted("minecraft:diamond_shovel", map[string]int{"efficiency": 5})
		g.Protect(9, 3, 20, 12)
		r, _ := safeRunner(t, g)
		r.Store.NoteProtected(world.BlockPos{X: 15, Y: 63, Z: 7}) // apuntado en otra sesión
		g.Step(5)
		box := automation.NewBox(world.BlockPos{X: 3, Y: 63, Z: 3}, world.BlockPos{X: 20, Y: 60, Z: 12})
		var err error
		start := g.Tick
		r.Start(context.Background(), "excavar", func(j *automation.Job) error {
			err = automation.Excavate(box)(j)
			return err
		})
		finished := g.Until(1500*20, func() bool { return r.Status() == "" })
		free := 0
		for x := 3; x <= 8; x++ {
			for y := 60; y <= 63; y++ {
				for z := 3; z <= 12; z++ {
					if !block.IsAir(g.S.World.At(x, y, z)) {
						free++
					}
				}
			}
		}
		t.Logf("terminó %v (%d ticks); error %v; quedan %d en lo libre; picadas devueltas %d", finished, g.Tick-start, err, free, g.ProtectedTries())
		if !finished {
			t.Fatalf("no terminó (%s)", r.Status())
		}
		if free > 4 {
			t.Errorf("quedan %d bloques en lo libre", free)
		}
		if g.ProtectedTries() > 3*12*10 { // (como mucho 3 por columna: lo que hace falta para ver que el servidor lo repone)
			t.Errorf("%d picadas en el terreno reclamado: lo intentó columna a columna", g.ProtectedTries())
		}
	})
}
