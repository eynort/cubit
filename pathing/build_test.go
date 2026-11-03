package pathing_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/pathing"
)

func TestBuild(t *testing.T) {
	// 6 de ancho, 9 de hondo y más larga que lo que ve el bot: ni saltar, ni bajar, ni rodear
	trench := []string{"fill 3 55 -200 8 63 200 air"}
	for _, c := range []struct {
		name    string
		setup   []string
		goal    world.BlockPos
		reach   bool
		usedMin int // bloques gastados, como mínimo
		lag     int // ticks que tardan en llegar los paquetes del servidor
	}{
		{"puente", append(trench, "give Tester cobblestone 32"), world.BlockPos{X: 10, Y: 64, Z: 0}, true, 3, 0},               // (puede acabar saltando un hueco de 2)
		{"torre", []string{"fill 4 64 -3 8 66 3 stone", "give Tester dirt 16"}, world.BlockPos{X: 6, Y: 67, Z: 0}, true, 2, 0}, // (el último bloque, de un salto)
		{"sin-bloques", trench, world.BlockPos{X: 10, Y: 64, Z: 0}, false, 0, 0},
		// la barra rápida llena (la tierra en el inventario) y con el lag de un
		// servidor de verdad: en los logs, 70 torres de "10 s sin avanzar" (el
		// click que pasa la tierra a la barra no se veía hasta la respuesta, y
		// el siguiente la devolvía)
		{"torre-con-lag", []string{"fill 4 64 -3 8 66 3 stone", "give Tester iron_sword 1", "give Tester stone_axe 1",
			"give Tester stone_pickaxe 1", "give Tester shield 1", "give Tester golden_carrot 8", "give Tester stick 4",
			"give Tester string 4", "give Tester bone 4", "give Tester feather 4", "give Tester dirt 16"},
			world.BlockPos{X: 6, Y: 67, Z: 0}, true, 2, 4},
		// (un puente sobre lava o agua no se prueba aquí: simworld no modela poner
		// bloques en líquidos; un servidor de verdad sí lo permite)
	} {
		t.Run(c.name, func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				g := simworld.Start(t, simworld.Options{Setup: c.setup, Lag: c.lag})
				b := g.B
				b.Mover.Food = func() int { return 20 }
				w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
				bd := &pathing.HandsBuilder{Screens: b.Screens, Hands: b.Hands, Ctl: b.Ctl, Ctx: context.Background()}
				w.SetBuilder(bd)
				b.Ctl.Think = w.Think
				g.Step(5)
				before := bd.Blocks()
				st, _ := b.World.BlockAt(world.BlockPos{X: 5, Y: 63, Z: 0})
				t.Logf("bloques en el inventario: %d; bloque en 5 63 0: %s", before, world.StateString(st))
				w.Goto(c.goal)
				t0 := b.Ctl.State().Teleports
				ok := g.Until(60*20, func() bool {
					if testing.Verbose() && g.Tick < 120 {
						p := b.Player.Position()
						t.Logf("t=%d %.2f %.2f %.2f suelo=%v ocupado=%v %s", g.Tick, p.X, p.Y, p.Z, b.Ctl.State().OnGround, bd.Busy(), w.Debug())
					}
					st := w.Status()
					return st == "arrived" || strings.HasPrefix(st, "failed")
				})
				used, tps := before-bd.Blocks(), b.Ctl.State().Teleports-t0
				p := b.Player.Position()
				t.Logf("%s en %d ticks; en %.1f %.1f %.1f; bloques gastados %d; correcciones %d", w.Status(), g.Tick, p.X, p.Y, p.Z, used, tps)
				if arrived := ok && w.Status() == "arrived"; arrived != c.reach {
					t.Fatalf("llegó=%v, se esperaba %v (%s)", arrived, c.reach, w.Status())
				}
				if used < c.usedMin {
					t.Errorf("gastó %d bloques, se esperaban al menos %d", used, c.usedMin)
				}
				if tps > 0 && c.lag == 0 || tps > 3 { // (con lag, alguna: el servidor ve el bloque tarde)
					t.Errorf("el servidor lo devolvió %d veces", tps)
				}
				if c.lag > 0 && g.Tick > 160 {
					t.Errorf("tardó %d ticks (sin lag, unos 55): se atasca subiendo", g.Tick)
				}
			})
		})
	}
}

// TestPillarUp: subir 3 bloques en el sitio (la torre de la pelea), con lag.
func TestPillarUp(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{"give Tester dirt 16"}, Lag: 3})
		b := g.B
		b.Mover.Food = func() int { return 20 }
		w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		bd := &pathing.HandsBuilder{Screens: b.Screens, Hands: b.Hands, Ctl: b.Ctl, Ctx: context.Background()}
		w.SetBuilder(bd)
		b.Ctl.Think = w.Think
		g.Step(10)
		y0 := b.Player.Position().Y
		if !w.PillarUp(b.Player.Position(), 3) {
			t.Fatal("PillarUp: false con 16 de tierra")
		}
		g.Until(10*20, func() bool { return w.Status() != "walking" })
		y := b.Player.Position().Y
		t.Logf("%s; de %.1f a %.1f en %d ticks", w.Status(), y0, y, g.Tick)
		if w.Status() != "arrived" || y-y0 < 2.9 {
			t.Errorf("no subió 3 bloques: %s, %.1f", w.Status(), y-y0)
		}
	})
}
