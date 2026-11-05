package combat_test

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
)

// platform: una plataforma de 9×9 en el aire, con 20 bloques de caída
// alrededor (el bot y el rival encima): el que se cae, pierde.
var platform = []string{"fill -14 43 -14 14 63 14 air", "fill -4 63 -4 4 63 4 stone"}

// TestEdgeDuel: peleando en una plataforma, con un rival que empuja (cada
// golpe suyo con sprint), el bot no se deja tirar: no pelea de espaldas al
// borde (se mueve de lado, no retrocede hacia el vacío).
// Con escudo casi no le empujan: ninguna caída. Sin escudo pierde el duelo
// (ver PVP.md) y recibe todos los empujones: con la lógica de bordes mirando
// solo 2 bloques se caía 6 de 16 (13 sin ella); mirando lo que lleva un
// empujón en el aire (3,5 bloques, think.go: knockbackReach), 0 de 160. Se
// pide como mucho 1 de 4 (margen para no fallar por azar).
func TestEdgeDuel(t *testing.T) {
	edgeDuel(t, kit, 0)
	edgeDuel(t, []string{"give Tester iron_sword 1", "give Tester cooked_beef 8"}, 1)
}

func edgeDuel(t *testing.T, gear []string, maxFalls int) {
	falls := 0
	for i := 0; i < 4; i++ {
		simworld.Run(t, func(t *testing.T) {
			g, f := simFighter(t, append(append([]string{}, platform...), gear...)...)
			g.Teleport(0.5, 64, 0.5)
			g.Step(5 * 20)
			dx, dz := []float64{4, 0, -4, 0}[i], []float64{0, 4, 0, -4}[i] // (empiezan separados, cada uno en un lado)
			if err := g.SummonDuelist(dx, dz); err != nil {
				t.Fatal(err)
			}
			id := g.LastMob()
			g.Step(lag + 2)
			f.Engage(id, "duelista")
			lowest := 64.0
			g.Until(40*20, func() bool {
				y := g.Pos()[1]
				lowest = min(lowest, y)
				_, alive := g.MobHealth(id)
				return !alive || g.Deaths() > 0 || y < 55
			})
			_, alive := g.MobHealth(id)
			y := g.Pos()[1]
			t.Logf("desde %v,%v: ganó %v, cayó %v (y más baja %.1f)", dx, dz, !alive, lowest < 60, lowest)
			if lowest < 60 || y < 60 {
				falls++
			}
		})
	}
	t.Logf("con %v: se cayó %d de 4 veces", gear, falls)
	if falls > maxFalls {
		t.Errorf("se cayó de la plataforma %d de 4 veces (como mucho %d)", falls, maxFalls)
	}
}
