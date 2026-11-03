package pathing_test

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/pathing"
)

// simCourse: un recorrido en simworld, que hace de servidor de verdad: cada
// movimiento que dejaría al jugador dentro de un bloque lo devuelve atrás.
type simCourse struct {
	name  string
	setup []string
	goal  world.BlockPos
}

var simCourses = []simCourse{
	{"escalera", []string{
		"fill 2 64 -1 2 64 1 stone", "fill 3 64 -1 3 65 1 stone", "fill 4 64 -1 4 66 1 stone",
		"fill 5 64 -1 5 67 1 stone", "fill 6 64 -1 9 68 1 stone",
	}, world.BlockPos{X: 8, Y: 69, Z: 0}},
	{"zigzag", []string{
		"fill 2 64 -1 4 64 1 stone", "fill 4 64 1 6 65 3 stone", "fill 6 64 3 8 66 5 stone", "fill 8 64 5 10 67 7 stone",
	}, world.BlockPos{X: 9, Y: 68, Z: 6}},
	{"muro-contra", []string{"fill 3 64 -3 3 66 3 stone"}, world.BlockPos{X: 6, Y: 64, Z: 0}},
}

// TestSimCorrections: ¿el servidor (simworld) devuelve atrás al bot al subir?
func TestSimCorrections(t *testing.T) {
	for _, c := range simCourses {
		t.Run(c.name, func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				g := simworld.Start(t, simworld.Options{Setup: c.setup})
				b := g.B
				b.Mover.Food = func() int { return 20 }
				w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
				b.Ctl.Think = w.Think
				w.Goto(c.goal)
				t0 := b.Ctl.State().Teleports
				ok := g.Until(40*20, func() bool { return w.Status() == "arrived" })
				tps := b.Ctl.State().Teleports - t0
				t.Logf("llegó=%v en %d ticks, el servidor lo devolvió %d veces (%s)", ok, g.Tick, tps, w.Status())
				if !ok || tps > 0 {
					t.Errorf("llegó=%v correcciones=%d", ok, tps)
				}
			})
		})
	}
}
