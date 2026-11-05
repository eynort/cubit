package combat_test

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
)

// (duelFrom, en bench_test.go: el duelo desde un ángulo y una distancia)

// duel: el bot contra un duelista (un jugador medio con espada de hierro) en
// llano. Dice si ganó, la vida más baja, cuánto tardó y lo que gastó.
type duelResult struct {
	won, died      bool
	minHealth      float32
	ticks          int
	totems, blocks int
}

func duel(t *testing.T, setup ...string) duelResult {
	var r duelResult
	simworld.Run(t, func(t *testing.T) {
		g, _ := simFighter(t, setup...)
		g.Step(5 * 20)
		if err := g.SummonDuelist(8, 0); err != nil {
			t.Fatal(err)
		}
		id := g.LastMob()
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

// TestDuel: contra un jugador medio (espada de hierro, corre, pega en
// cuanto tiene el golpe cargado) con espada, escudo y tótem: gana sin gastar
// el tótem, tanto si va él a por el rival (/fight) como si el rival viene a
// por él (en guardia: escudo arriba, su golpe da en el escudo, y a pelear).
// (Sin escudo, contra un igual que pega primero, el simulador da casi
// siempre la victoria al que empieza: no se exige.)
func TestDuel(t *testing.T) {
	for _, c := range []struct {
		name   string
		engage bool
		dist   float64
	}{
		{"va-a-por-el", true, 10},
		{"viene-a-por-mi", false, 10},
		{"lo-ve-venir-de-lejos", false, 20},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := duelFrom(t, 0.4, c.dist, c.engage, kit...)
			t.Logf("ganó %v, murió %v, vida mínima %.0f, %.1f s, tótems %d, bloqueos %d", r.won, r.died, r.minHealth, float64(r.ticks)/20, r.totems, r.blocks)
			if !r.won || r.died || r.totems > 0 {
				t.Errorf("perdió el duelo (o gastó el tótem)")
			}
			if r.blocks == 0 {
				t.Errorf("no paró ningún golpe con el escudo")
			}
		})
	}
}
