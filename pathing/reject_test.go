package pathing_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/world"

	"cubit/pathing"
)

// TestRejectedStepIsAvoided: un muro de un bloque entre el bot y la meta; un
// "anticheat" devuelve al bot atrás cada vez que sube al muro. El walker deja
// de insistir y lo rodea (el muro acaba en z=4).
func TestRejectedStepIsAvoided(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		s.World.Fill(3, 64, -3, 3, 64, 3, "minecraft:stone")
		b := fakeserver.JoinStepped(t, s, "Tester")
		if err := b.Loaded(3 * time.Second); err != nil {
			t.Fatal(err)
		}
		b.Mover.Food = func() int { return 20 }
		w := ourWalker(b).(interface {
			mover
			Rejected() string
		})
		b.Ctl.Think = w.Think
		w.Goto(world.BlockPos{X: 6, Y: 64, Z: 0})
		rejected := 0
		ok := b.Until(30*20, func() bool {
			p := b.Player.Position()
			if p.Y > 64.5 && p.X > 2 && p.X < 4.5 { // subió al muro: rechazado
				rejected++
				b.Player.UpdatePosition(func(q basic.Position) basic.Position {
					q.X, q.Y, q.Z, q.VX, q.VY, q.VZ = 1.5, 64, 0.5, 0, 0, 0
					return q
				})
				t.Logf("rechazo %d: %s", rejected, w.Rejected())
			}
			return w.Status() == "arrived"
		})
		p := b.Player.Position()
		if !ok {
			t.Fatalf("no llegó (%s) en %.1f %.1f %.1f tras %d rechazos", w.Status(), p.X, p.Y, p.Z, rejected)
		}
		if rejected > 4 {
			t.Errorf("insistió demasiado: %d rechazos", rejected)
		}
		t.Logf("llegó rodeando tras %d rechazos, en %.1f %.1f %.1f", rejected, p.X, p.Y, p.Z)

		// otra vez, con un walker nuevo (sin las celdas a evitar de este) y la
		// misma memoria: ni un intento por el muro
		mem := w.(interface{ Memory() *pathing.Memory }).Memory()
		w2 := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		w2.SetMemory(mem)
		b.Ctl.Think = w2.Think
		b.Player.UpdatePosition(func(q basic.Position) basic.Position {
			q.X, q.Y, q.Z, q.VX, q.VY, q.VZ = 1.5, 64, 0.5, 0, 0, 0
			return q
		})
		b.Step(5)
		w2.Goto(world.BlockPos{X: 6, Y: 64, Z: 0})
		again := 0
		ok = b.Until(30*20, func() bool {
			p := b.Player.Position()
			if p.Y > 64.5 && p.X > 2 && p.X < 4.5 {
				again++
				b.Player.UpdatePosition(func(q basic.Position) basic.Position {
					q.X, q.Y, q.Z, q.VX, q.VY, q.VZ = 1.5, 64, 0.5, 0, 0, 0
					return q
				})
				w2.Rejected()
			}
			return w2.Status() == "arrived"
		})
		t.Logf("segunda vez: llegó=%v, %d rechazos, de memoria %d, búsquedas %d", ok, again, mem.Reused.Load(), mem.Searched.Load())
		if !ok || again > 0 {
			t.Errorf("segunda vez: llegó=%v con %d rechazos (se esperaba 0)", ok, again)
		}
	})
}
