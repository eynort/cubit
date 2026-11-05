package combat_test

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
)

// TestPillarVsZombies: tres zombis viniendo y poca vida, en campo abierto:
// llevar tierra (la torre) no empeora nada (retirarse a comer es lo que
// funciona contra zombis, que son lentos): acaba con ellos sin morir, con y
// sin tierra.
func TestPillarVsZombies(t *testing.T) {
	for _, dirt := range []bool{false, true} {
		pillarVsZombies(t, dirt)
	}
}

func pillarVsZombies(t *testing.T, dirt bool) {
	setup := []string{"give Tester stone_sword 1", "give Tester cooked_beef 8"}
	if dirt {
		setup = append(setup, "give Tester dirt 16")
	}
	simworld.Run(t, func(t *testing.T) {
		g, _ := simFighter(t, setup...)
		g.Health = 7
		g.Step(20)
		var ids []int32
		for _, d := range [][2]float64{{6, 0}, {-6, 0.5}, {0.5, 6}} {
			if err := g.SummonAdultNear("minecraft:zombie", d[0], d[1]); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, g.LastMob())
		}
		minHealth := g.Health
		dead := func() int {
			n := 0
			for _, id := range ids {
				if _, alive := g.MobHealth(id); !alive {
					n++
				}
			}
			return n
		}
		g.Until(90*20, func() bool {
			minHealth = min(minHealth, g.Health)
			return dead() == len(ids) || g.Deaths() > 0
		})
		t.Logf("con tierra %v: zombis muertos %d/3, vida %.0f (mínima %.0f), murió %v, %.1f s", dirt, dead(), g.Health, minHealth, g.Deaths() > 0, float64(g.Tick)/20)
		if g.Deaths() > 0 {
			t.Errorf("murió")
		}
		if dead() < 3 {
			t.Errorf("no acabó con los zombis en 90 s")
		}
	})
}

// TestPillarDeadEnd: en un callejón sin salida (paredes por tres lados, la
// salida hacia los zombis) con poca vida: no hay adónde huir, así que torre
// de 3, a comer arriba, y abajo a pelear curado. Sin morir.
func TestPillarDeadEnd(t *testing.T) {
	for _, dirt := range []bool{false, true} {
		setup := []string{"fill -2 64 -2 2 67 2 stone", "fill -1 64 -1 1 67 1 air", "fill 2 64 -1 2 67 1 air",
			"give Tester stone_sword 1", "give Tester cooked_beef 8"}
		if dirt {
			setup = append(setup, "give Tester dirt 16")
		}
		simworld.Run(t, func(t *testing.T) {
			g, _ := simFighter(t, setup...)
			g.Health = 6
			g.Step(20)
			var ids []int32
			for _, d := range [][2]float64{{7, 0}, {7, 1.5}, {8, -1.5}} {
				if err := g.SummonAdultNear("minecraft:zombie", d[0], d[1]); err != nil {
					t.Fatal(err)
				}
				ids = append(ids, g.LastMob())
			}
			minHealth := g.Health
			g.Until(90*20, func() bool {
				minHealth = min(minHealth, g.Health)
				return g.Deaths() > 0
			})
			dead := 0
			for _, id := range ids {
				if _, alive := g.MobHealth(id); !alive {
					dead++
				}
			}
			t.Logf("con tierra %v: zombis muertos %d/3, vida %.0f (mínima %.0f), murió %v", dirt, dead, g.Health, minHealth, g.Deaths() > 0)
			if dirt && g.Deaths() > 0 {
				t.Errorf("murió en el callejón con tierra para la torre")
			}
		})
	}
}
