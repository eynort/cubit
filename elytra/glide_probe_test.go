package elytra

import (
	"math"
	"os"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/physics"
)

// TestGlideProbe (PROBE=1) mide el planeo con cada inclinación: con qué
// velocidad de lado y de bajada se queda (los ajustes de pilot.go salen de aquí).
func TestGlideProbe(t *testing.T) {
	if os.Getenv("PROBE") == "" {
		t.Skip()
	}
	w := &terrain{height: func(int, int) int { return -60 }}
	for _, pitch := range []float32{-60, -50, -40, -30, -20, -10, 0, 10, 20, 30, 40} {
		for _, v0 := range []float64{0.35, 1.5} {
			p := physics.NewPlayer(physics.Vec3{Y: 300})
			p.FallFlying, p.CanGlide = true, true
			p.Yaw, p.Pitch = -90, pitch
			p.Vel = physics.Vec3{X: v0}
			y0 := p.Pos.Y
			var hs, vy float64
			for i := 0; i < 600; i++ {
				p.Tick(w, nil, physics.Keys{}, physics.DefaultAttributes)
				if i == 10 || i == 20 || i == 40 || i == 80 || i == 160 || i == 599 {
					hs, vy = math.Hypot(p.Vel.X, p.Vel.Z), p.Vel.Y
					t.Logf("pitch %3.0f v0 %.1f tick %3d: hs %.2f vy %+.2f  (angle %.1f°) bajado %.0f", pitch, v0, i, hs, vy, math.Atan2(-vy, hs)*180/math.Pi, y0-p.Pos.Y)
				}
			}
		}
	}
}
