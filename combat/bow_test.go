package combat_test

import (
	"math"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
)

// TestBowVsZombie: un zombi que viene de lejos: con arco y flechas, flechazos
// mientras llega (acierta y le quita vida antes del cuerpo a cuerpo), y luego
// la espada. Lo mata.
func TestBowVsZombie(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, f := simFighter(t, "give Tester iron_sword 1", "give Tester bow 1", "give Tester arrow 32")
		g.Step(20)
		if err := g.SummonAdultNear("minecraft:zombie", 20, 3); err != nil {
			t.Fatal(err)
		}
		id := g.LastMob()
		g.Step(lag + 2)
		f.Engage(id, "zombi")
		hpAtMelee := float32(20)
		g.Until(60*20, func() bool {
			h, alive := g.MobHealth(id)
			x, _, z, _ := g.MobAt(id)
			p := g.Pos()
			if d := (x-p[0])*(x-p[0]) + (z-p[2])*(z-p[2]); d < 9 && hpAtMelee == 20 {
				hpAtMelee = h
			}
			return !alive
		})
		_, alive := g.MobHealth(id)
		t.Logf("flechas %d, aciertos %d; el zombi llegó al cuerpo a cuerpo con %.0f de vida; muerto %v", g.Shots(), g.ArrowHits(), hpAtMelee, !alive)
		if g.ArrowHits() == 0 {
			t.Errorf("no acertó ni una flecha")
		}
		if alive {
			t.Errorf("no lo mató")
		}
	})
}

// TestBowBlocked: con bloques en medio no tira (antes tiraba contra su propia
// pared en el búnker, o a uno que se tapó con bloques encima): la flecha se
// mira por su vuelo de verdad, en arco. En campo abierto, sí.
func TestBowBlocked(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup []string
		shoot bool
	}{
		{"abierto", nil, true},
		// el bot, encerrado (paredes de 2 y techo) como en su búnker
		{"bunker", []string{"fill -1 64 -1 1 65 1 cobblestone", "setblock 0 64 0 air", "setblock 0 65 0 air", "setblock 0 66 0 cobblestone"}, false},
		// el rival, tapado con bloques: alrededor y encima (se ponen tras aparecer)
		{"tapado", nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				g, _, id := tacticsFight(t, append([]string{"give Tester bow 1", "give Tester arrow 32"}, c.setup...), 15, 0)
				if c.name == "tapado" {
					mx, my, mz, _ := g.MobAt(id)
					x, y, z := int(math.Floor(mx)), int(math.Floor(my)), int(math.Floor(mz))
					for dx := -1; dx <= 1; dx++ {
						for dz := -1; dz <= 1; dz++ {
							for dy := 0; dy <= 2; dy++ {
								if dx == 0 && dz == 0 && dy < 2 {
									continue
								}
								if err := g.S.SetBlock(x+dx, y+dy, z+dz, "minecraft:cobblestone"); err != nil {
									t.Fatal(err)
								}
							}
						}
					}
				}
				g.Step(8 * 20)
				t.Logf("flechas %d, aciertos %d", g.Shots(), g.ArrowHits())
				if c.shoot && g.Shots() == 0 {
					t.Error("en campo abierto no tiró")
				}
				if !c.shoot && g.Shots() > 0 {
					t.Errorf("tiró %d flechas con bloques en medio", g.Shots())
				}
			})
		})
	}
}
