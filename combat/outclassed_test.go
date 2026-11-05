package combat_test

import (
	"math"
	"os"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
)

// TestOutclassedShield: el caso de los logs. El bot full hierro, espada de
// hierro, escudo, tótem y zanahorias doradas, sin bloques; el rival full
// diamante protección IV con espada de diamante filo V, que viene a por él.
// No se le gana a golpes (le quita 1.8 por golpe, él 7): lo que se mide es
// cuánto aguanta y cuántos golpes le entran. A la defensiva (sin saltar,
// tapado salvo en su ventana) le entran menos que saltando a por críticos.
// BENCH=1: 6 salidas de 6 a 11 bloques; si no, 2.
func TestOutclassedShield(t *testing.T) {
	dists := []float64{7, 10}
	if os.Getenv("BENCH") != "" {
		dists = []float64{6, 7, 8, 9, 10, 11}
	}
	var hits, deaths, totems int
	var alive float64
	for _, d := range dists {
		simworld.Run(t, func(t *testing.T) {
			setup := append(append([]string{}, ironArmor...), "give Tester iron_sword 1", "give Tester shield 1", "give Tester totem_of_undying 1", "give Tester golden_carrot 32")
			g, f := simFighter(t, setup...)
			if os.Getenv("TRACE") == "" {
				f.Log = func(string, ...any) {}
			}
			g.Until(5*20, func() bool { return g.Worn(8) != "" && g.Offhand() != "" })
			rival := simworld.Kit{Weapon: "minecraft:diamond_sword", Sharpness: 5, Armor: "diamond", Protection: 4}
			if err := g.SummonDuelistKit(d, 0, rival); err != nil {
				t.Fatal(err)
			}
			if os.Getenv("TRACE") != "" {
				think, last := g.B.Ctl.Think, 0
				g.B.Ctl.Think = func(s *control.State, bp *basic.Player) {
					think(s, bp)
					mx, _, mz, _ := g.MobAt(g.LastMob())
					p := bp.Position()
					if h := g.MobHits(); h != last || s.Ticks%5 == 0 {
						t.Logf("t=%d golpes=%d escudo/usando=%v suelo=%v in=%+v dist=%.1f vida=%.1f", s.Ticks, h, s.UsingItem, s.OnGround, s.Input, math.Hypot(mx-p.X, mz-p.Z), g.Health)
						last = h
					}
				}
			}
			ticks := 0
			g.Until(40*20, func() bool {
				if !g.Dead() {
					ticks++
				}
				return g.Dead()
			})
			t.Logf("a %.0f: vivo %.1f s, golpes recibidos %d, bloqueados %d, tótems %d, muerto %v", d, float64(ticks)/20, g.MobHits(), g.Blocks(), g.TotemsUsed(), g.Dead())
			hits += g.MobHits()
			totems += g.TotemsUsed()
			alive += float64(ticks) / 20
			if g.Dead() {
				deaths++
			}
		})
	}
	n := float64(len(dists))
	t.Logf("media: vivo %.1f s de 40, golpes recibidos %.1f, tótems %.1f, muertes %d de %d", alive/n, float64(hits)/n, float64(totems)/n, deaths, len(dists))
}
