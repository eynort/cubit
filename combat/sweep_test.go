package combat_test

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
)

// TestSweepHorde: cuatro zombis juntos y una espada de hierro: barre (el
// golpe en el suelo les da a los de al lado y los aparta) y acaba con todos
// sin morir.
func TestSweepHorde(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, _ := simFighter(t, "give Tester iron_sword 1", "give Tester cooked_beef 8")
		g.Step(20)
		var ids []int32
		for _, d := range [][2]float64{{5, 0}, {5.5, 0.8}, {5.5, -0.8}, {6.2, 0}} {
			if err := g.SummonAdultNear("minecraft:zombie", d[0], d[1]); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, g.LastMob())
		}
		minHealth := g.Health
		g.Until(90*20, func() bool {
			minHealth = min(minHealth, g.Health)
			for _, id := range ids {
				if _, alive := g.MobHealth(id); alive {
					return g.Deaths() > 0
				}
			}
			return true
		})
		dead := 0
		for _, id := range ids {
			if _, alive := g.MobHealth(id); !alive {
				dead++
			}
		}
		t.Logf("zombis muertos %d/4 en %.1f s, vida mínima %.0f, barridos que dieron %d, murió %v", dead, float64(g.Tick)/20, minHealth, g.Sweeps(), g.Deaths() > 0)
		if g.Deaths() > 0 || dead < 4 {
			t.Errorf("no acabó con la horda")
		}
		if g.Sweeps() == 0 {
			t.Errorf("no barrió nunca")
		}
	})
}
