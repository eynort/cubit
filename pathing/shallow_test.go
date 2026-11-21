package pathing_test

import (
	"os"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"cubit/pathing"
)

// TestShallowWaterBank: agua de un bloque de hondo (de pie en el fondo) y la
// orilla un bloque más alta que el fondo. En los logs (01:16) el bot pulsaba
// saltar un tick (en el agua no es un salto: no sube) y a los 1.5 s daba el
// salto por fallido, celda tras celda de la misma orilla. Hay que mantener
// saltar contra la pared: al subir flotando, el impulso de salir del agua lo
// sube a la orilla.
func TestShallowWaterBank(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"fill -10 63 -6 10 63 6 stone",
			"fill -6 63 -3 6 63 3 water",
			"fill -6 63 -3 -4 63 3 seagrass", // algas junto a la orilla: agua sin "waterlogged"
		}})
		b := g.B
		b.Mover.Food = func() int { return 20 }
		w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		b.Ctl.Think = w.Think
		if os.Getenv("TRACE") != "" {
			b.Ctl.Think = func(s *control.State, bp *basic.Player) {
				w.Think(s, bp)
				p := bp.Position()
				t.Logf("t=%d %.2f %.3f %.2f suelo=%v choque=%v J=%v F=%v %s", s.Ticks, p.X, p.Y, p.Z, s.OnGround, s.HorizontalCollision, s.Input.Jump, s.Input.Forward, w.Debug())
			}
		}
		g.Teleport(0.5, 63, 0.5)
		g.Step(20)
		goal := world.BlockPos{X: -9, Y: 64, Z: 0}
		w.Goto(goal)
		t0 := b.Ctl.State().Teleports
		ok := g.Until(30*20, func() bool { return w.Status() == "arrived" })
		p := b.Player.Position()
		t.Logf("llegó=%v en %d ticks (%s), en %.1f %.1f %.1f, correcciones %d", ok, g.Tick, w.Status(), p.X, p.Y, p.Z, b.Ctl.State().Teleports-t0)
		if !ok {
			t.Errorf("no salió del agua a la orilla: %s", w.Debug())
		}
		if g.Tick > 20*12 {
			t.Errorf("tardó %d ticks en salir", g.Tick)
		}
	})
}
