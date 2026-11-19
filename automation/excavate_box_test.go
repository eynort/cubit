package automation_test

import (
	"context"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"

	"minecraft-bot/automation"
)

// TestExcavateStaysInBox: la caja entre dos esquinas (dadas al revés: la
// mayor primero) se vacía entera, y fuera no se toca nada: ni las paredes de
// alrededor ni el suelo de debajo (salvo, como mucho, la escalera o torre
// por la que entra y sale).
func TestExcavateStaysInBox(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"give Tester diamond_shovel 1", "give Tester diamond_pickaxe 1",
		}})
		r, _ := safeRunner(t, g)
		g.Step(5)
		// esquinas al revés, y un rectángulo (no un cuadrado): 6 x 4 x 9
		box := automation.NewBox(world.BlockPos{X: 8, Y: 63, Z: 11}, world.BlockPos{X: 3, Y: 60, Z: 3})
		var err error
		r.Start(context.Background(), "excavar", func(j *automation.Job) error {
			err = automation.Excavate(box)(j)
			return err
		})
		g.Until(3000*20, func() bool { return r.Status() == "" })
		inside, outside := 0, 0
		for x := 1; x <= 10; x++ {
			for y := 59; y <= 63; y++ {
				for z := 1; z <= 13; z++ {
					p := world.BlockPos{X: x, Y: y, Z: z}
					air := block.IsAir(g.S.World.At(x, y, z))
					if box.Contains(p) {
						if !air {
							inside++
						}
					} else if air {
						outside++
						t.Logf("fuera de la caja, cavado: %v", p)
					}
				}
			}
		}
		t.Logf("error %v; caja %s; quedan dentro %d; cavados fuera %d", err, box, inside, outside)
		if err != nil {
			t.Fatalf("excavar: %v", err)
		}
		if box.Min != (world.BlockPos{X: 3, Y: 60, Z: 3}) || box.Max != (world.BlockPos{X: 8, Y: 63, Z: 11}) {
			t.Fatalf("la caja no es la de las dos esquinas: %s", box)
		}
		if inside > 4 { // (la torre para salir, como mucho)
			t.Errorf("quedan %d bloques dentro", inside)
		}
		if outside > 0 {
			t.Errorf("cavó %d bloques fuera de la caja", outside)
		}
	})
}

// TestExcavateNegativeCoords: con coordenadas negativas (como Z -31…-16, que
// cruza el borde de chunk entre -17 y -16) se vacía justo la caja: ni un
// bloque desplazado (el redondeo de los negativos es donde se cuelan los
// "off by one").
func TestExcavateNegativeCoords(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"give Tester diamond_shovel 1", "give Tester diamond_pickaxe 1",
		}})
		r, _ := safeRunner(t, g)
		g.Step(5)
		box := automation.NewBox(world.BlockPos{X: -4, Y: 63, Z: -14}, world.BlockPos{X: -9, Y: 61, Z: -19})
		var err error
		r.Start(context.Background(), "excavar", func(j *automation.Job) error {
			err = automation.Excavate(box)(j)
			return err
		})
		g.Until(3000*20, func() bool { return r.Status() == "" })
		inside, outside := 0, 0
		for x := -11; x <= -2; x++ {
			for y := 60; y <= 63; y++ {
				for z := -21; z <= -12; z++ {
					p := world.BlockPos{X: x, Y: y, Z: z}
					air := block.IsAir(g.S.World.At(x, y, z))
					if box.Contains(p) {
						if !air {
							inside++
							t.Logf("dentro, sin cavar: %v", p)
						}
					} else if air {
						outside++
						t.Logf("fuera de la caja, cavado: %v", p)
					}
				}
			}
		}
		t.Logf("error %v; caja %s; quedan dentro %d; cavados fuera %d", err, box, inside, outside)
		if err != nil {
			t.Fatalf("excavar: %v", err)
		}
		if box.Min != (world.BlockPos{X: -9, Y: 61, Z: -19}) || box.Max != (world.BlockPos{X: -4, Y: 63, Z: -14}) {
			t.Fatalf("la caja no es la de las dos esquinas: %s", box)
		}
		if inside > 4 {
			t.Errorf("quedan %d bloques dentro", inside)
		}
		if outside > 0 {
			t.Errorf("cavó %d bloques fuera de la caja", outside)
		}
	})
}

// TestExcavateFarBox: la caja está fuera de lo que el bot ve (vista de 2
// chunks, la caja a 5 chunks): antes daba todas las capas por acabadas al
// momento (sus bloques, sin cargar, parecían aire) y al final intentaba subir
// al aire; ahora anda hacia ella hasta verla y la vacía.
func TestExcavateFarBox(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{View: 2, Setup: []string{
			"give Tester diamond_shovel 1", "give Tester diamond_pickaxe 1",
		}})
		r, _ := safeRunner(t, g)
		g.Step(5)
		box := automation.NewBox(world.BlockPos{X: 82, Y: 100, Z: 2}, world.BlockPos{X: 86, Y: 61, Z: 6}) // (alta: casi toda aire, como la del usuario)
		if _, ok := g.B.World.BlockAt(world.BlockPos{X: 84, Y: 62, Z: 4}); ok {
			t.Fatal("la caja tenía que empezar sin cargar")
		}
		var err error
		r.Start(context.Background(), "excavar", func(j *automation.Job) error {
			err = automation.Excavate(box)(j)
			return err
		})
		g.Until(3000*20, func() bool { return r.Status() == "" })
		inside := 0
		for x := 82; x <= 86; x++ {
			for y := 61; y <= 63; y++ {
				for z := 2; z <= 6; z++ {
					if !block.IsAir(g.S.World.At(x, y, z)) {
						inside++
					}
				}
			}
		}
		p := g.B.Player.Position()
		t.Logf("error %v; el bot en %.0f %.0f %.0f; quedan %d", err, p.X, p.Y, p.Z, inside)
		if err != nil {
			t.Fatalf("excavar: %v", err)
		}
		if inside > 4 {
			t.Errorf("quedan %d bloques en la caja: no la cavó", inside)
		}
		if p.Y > 70 {
			t.Errorf("subió al aire (y %.0f)", p.Y)
		}
	})
}
