package pathing_test

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/pathing"
)

// TestMemoryReuse: rodea un muro hasta la meta; la vuelta (la ruta al revés)
// y la ida otra vez salen de la memoria, sin buscar.
func TestMemoryReuse(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{"fill 3 64 -3 3 66 3 stone"}})
		b := g.B
		b.Mover.Food = func() int { return 20 }
		w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		b.Ctl.Think = w.Think
		m := w.Memory()
		trip := func(goal world.BlockPos) {
			t.Helper()
			w.Goto(goal)
			t0 := g.Tick
			if !g.Until(40*20, func() bool { return w.Status() == "arrived" }) {
				t.Fatalf("no llegó a %v: %s", goal, w.Status())
			}
			t.Logf("a %v en %d ticks: búsquedas %d, de memoria %d, rutas %d",
				goal, g.Tick-t0, m.Searched.Load(), m.Reused.Load(), m.Routes())
		}
		trip(world.BlockPos{X: 6, Y: 64, Z: 0})
		if m.Routes() != 1 {
			t.Fatalf("rutas recordadas: %d, se esperaba 1", m.Routes())
		}
		searched := m.Searched.Load()
		trip(world.BlockPos{X: 0, Y: 64, Z: 0})
		trip(world.BlockPos{X: 6, Y: 64, Z: 0})
		if m.Reused.Load() < 2 || m.Searched.Load() != searched {
			t.Errorf("de memoria %d (se esperaban 2), búsquedas %d → %d (no debía buscar)",
				m.Reused.Load(), searched, m.Searched.Load())
		}
	})
}
