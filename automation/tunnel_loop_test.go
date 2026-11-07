package automation_test

import (
	"context"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"

	"minecraft-bot/automation"
)

// TestDescendNoPingPong: debajo del bot (3x3) hay lava a dos bloques: no se
// puede bajar por ahí. Antes iba y venía entre dos bloques a nivel durante
// minutos (cada uno elegía el otro como "siguiente escalón"); tiene que
// bajar por otro sitio, o rendirse enseguida, sin volver a pisar lo pisado.
func TestDescendNoPingPong(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester diamond_pickaxe 1", "give Tester cobblestone 64",
			"fill -1 60 -1 1 61 1 minecraft:lava",
		}})
		r := runner(t, g)
		g.Step(5)
		var err error
		var y int
		r.Start(context.Background(), "bajar", func(j *automation.Job) error {
			err = j.DescendTo(40)
			y = j.Feet().Y
			return err
		})
		ticks := 0
		ended := g.Until(240*20, func() bool { ticks++; return r.Status() == "" })
		p := g.B.Player.Position()
		t.Logf("terminó %v en %d s; error %v; el bot en %.1f %.1f %.1f (y %d)", ended, ticks/20, err, p.X, p.Y, p.Z, y)
		if !ended {
			t.Fatalf("en 4 minutos no terminó de bajar (dando vueltas)")
		}
		if ticks > 60*20 { // bajar 24 bloques cavando: menos de un minuto
			t.Errorf("tardó %d s en bajar 24 bloques (vueltas sin avanzar)", ticks/20)
		}
		if err != nil || y > 41 {
			t.Errorf("no bajó a y=40 por otro sitio (y=%d): %v", y, err)
		}
	})
}
