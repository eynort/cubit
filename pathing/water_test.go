package pathing_test

import (
	"os"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/pathing"
)

// waterCourse: el bot empieza en el agua y tiene que salir a la meta.
type waterCourse struct {
	name  string
	setup []string
	start [3]float64
	goal  world.BlockPos
	ticks int
}

var waterCourses = []waterCourse{
	// la orilla está un bloque por encima del agua en todo el borde (nadando no
	// se sube): la única salida es un escalón en una esquina, lejos de la meta
	{"orilla-alta", []string{
		"fill -6 60 -3 6 62 3 water",
		"fill -6 63 -3 6 63 3 air",
		"setblock 6 62 3 stone",
	}, [3]float64{0.5, 62, 0.5}, world.BlockPos{X: 0, Y: 64, Z: 7}, 60 * 20},
	// un muro parte la piscina de arriba abajo salvo dos bloques en el fondo;
	// la mitad del bot no tiene salida: hay que bucear por debajo del muro
	{"bucear", []string{
		"fill -8 56 -3 8 62 3 water",
		"fill -8 63 -3 8 63 3 air",
		"fill 0 58 -3 0 66 3 stone",
		"fill 0 63 -6 0 66 6 stone",
		"setblock 8 62 0 stone",
	}, [3]float64{-4.5, 62, 0.5}, world.BlockPos{X: 11, Y: 64, Z: 0}, 60 * 20},
}

func TestWaterExits(t *testing.T) {
	for _, c := range waterCourses {
		t.Run(c.name, func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				g := simworld.Start(t, simworld.Options{Setup: c.setup})
				b := g.B
				b.Mover.Food = func() int { return 20 }
				w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
				b.Ctl.Think = w.Think
				if os.Getenv("TRACE") == c.name {
					b.Ctl.Think = func(s *control.State, bp *basic.Player) {
						w.Think(s, bp)
						p := bp.Position()
						t.Logf("tp=%d t=%d %.2f %.2f %.2f suelo=%v choque=%v J=%v S=%v F=%v %s", s.Teleports, s.Ticks, p.X, p.Y, p.Z, s.OnGround, s.HorizontalCollision, s.Input.Jump, s.Input.Shift, s.Input.Forward, w.Debug())
					}
				}
				g.Teleport(c.start[0], c.start[1], c.start[2])
				g.Step(20)
				w.Goto(c.goal)
				t0 := b.Ctl.State().Teleports
				ok := g.Until(c.ticks, func() bool { return w.Status() == "arrived" })
				p := b.Player.Position()
				tps := b.Ctl.State().Teleports - t0
				t.Logf("llegó=%v en %d ticks (%s), en %.1f %.1f %.1f, vida %.0f, correcciones %d", ok, g.Tick, w.Status(), p.X, p.Y, p.Z, g.Health, tps)
				if !ok {
					t.Errorf("no llegó a %v: %s (camino %s)", c.goal, w.Status(), w.Debug())
				}
				if tps > 0 {
					t.Errorf("el servidor lo devolvió atrás %d veces", tps)
				}
			})
		})
	}
}

// TestFollowOutOfWater: siguiendo a alguien de pie en la orilla a 3 bloques,
// con la orilla un bloque por encima del agua: en los logs, el bot iba "en
// línea recta" contra esa orilla (sin salto posible nadando) y tiraba el
// camino que salía por el escalón. Tiene que salir y llegar a su lado.
func TestFollowOutOfWater(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: waterCourses[0].setup})
		b := g.B
		b.Mover.Food = func() int { return 20 }
		w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		b.Ctl.Think = w.Think
		g.Teleport(0.5, 62, 1.5)
		g.Step(20)
		w.Follow(func() (float64, float64, float64, bool) { return 0.5, 64, 5.5, true }, 2)
		near := func() bool {
			p := b.Player.Position()
			return p.Y > 63.9 && (p.X-0.5)*(p.X-0.5)+(p.Z-5.5)*(p.Z-5.5) < 3.5*3.5
		}
		ok := g.Until(60*20, near)
		p := b.Player.Position()
		t.Logf("llegó=%v en %d ticks, en %.1f %.1f %.1f", ok, g.Tick, p.X, p.Y, p.Z)
		if !ok {
			t.Errorf("no salió del agua hasta el que sigue (camino %s)", w.Debug())
		}
	})
}
