package combat_test

import (
	"math"
	"os"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
)

// duelFrom: un duelo con el duelista saliendo a dist bloques en el ángulo a.
func duelFrom(t *testing.T, a, dist float64, engage bool, setup ...string) duelResult {
	var r duelResult
	simworld.Run(t, func(t *testing.T) {
		g, f := simFighter(t, setup...)
		g.Step(5 * 20)
		if err := g.SummonDuelist(dist*math.Cos(a), dist*math.Sin(a)); err != nil {
			t.Fatal(err)
		}
		id := g.LastMob()
		if engage {
			g.Step(1)
			g.Step(lag + 2) // (que la entidad haya llegado al bot)
			f.Engage(id, "duelista")
		}
		start := g.Tick
		r.minHealth = g.Health
		g.Until(90*20, func() bool {
			r.minHealth = min(r.minHealth, g.Health)
			_, alive := g.MobHealth(id)
			return !alive || g.Deaths() > 0
		})
		_, alive := g.MobHealth(id)
		r.won, r.died, r.ticks = !alive, g.Deaths() > 0, g.Tick-start
		r.totems, r.blocks = g.TotemsUsed(), g.Blocks()
	})
	return r
}

func TestDuelBench(t *testing.T) {
	if os.Getenv("BENCH") == "" {
		t.Skip("medida larga: BENCH=1 go test -run TestDuelBench -v ./combat")
	}
	kits := map[string][]string{
		"escudo": kit,
		"comida": {"give Tester iron_sword 1", "give Tester cooked_beef 8"},
		"sola":   {"give Tester iron_sword 1"},
	}
	for _, engage := range []bool{true, false} {
		for _, name := range []string{"escudo", "comida", "sola"} {
			wins, hp := 0, 0.0
			for i := 0; i < 12; i++ {
				r := duelFrom(t, float64(i)*math.Pi/6, 6+float64(i%3)*2, engage, kits[name]...)
				if r.won && !r.died {
					wins++
					hp += float64(r.minHealth)
				}
			}
			t.Logf("BENCH %-6s %s: gana %2d/12, vida mínima media al ganar %.1f", name, map[bool]string{true: "duelo   ", false: "emboscada"}[engage], wins, hp/math.Max(1, float64(wins)))
		}
	}
}
