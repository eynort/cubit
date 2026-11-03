package pathing_test

import (
	"context"
	"math"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/simworld"

	"minecraft-bot/pathing"
)

// TestFollowUpPillar: alguien se sube a un pilar de 1x1 de 3 de alto para
// escapar; el bot, con bloques, hace su propia torre al lado hasta quedar a
// su altura (o un bloque por debajo, a tiro de espada).
func TestFollowUpPillar(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{"fill 6 64 0 6 66 0 stone", "give Tester dirt 16"}})
		b := g.B
		b.Mover.Food = func() int { return 20 }
		w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		w.SetBuilder(&pathing.HandsBuilder{Screens: b.Screens, Hands: b.Hands, Ctl: b.Ctl, Ctx: context.Background()})
		b.Ctl.Think = w.Think
		g.Step(5)
		tx, ty, tz := 6.5, 67.0, 0.5 // encima del pilar
		w.Follow(func() (float64, float64, float64, bool) { return tx, ty, tz, true }, 2)
		ok := g.Until(40*20, func() bool {
			p := b.Player.Position()
			return math.Hypot(p.X-tx, p.Z-tz) < 2.5 && p.Y >= ty-1.01 && b.Ctl.State().OnGround
		})
		p := b.Player.Position()
		t.Logf("en %.1f %.1f %.1f tras %d ticks (%s)", p.X, p.Y, p.Z, g.Tick, w.Debug())
		if !ok {
			t.Fatal("no subió hasta el que está en el pilar")
		}
	})
}
