package combat

import (
	"math"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// flatWorld: piedra hasta y=63, y lo puesto en set.
type flatWorld map[world.BlockPos]block.StateID

func (w flatWorld) BlockAt(p world.BlockPos) (block.StateID, bool) {
	if s, ok := w[p]; ok {
		return s, true
	}
	if p.Y <= 63 {
		return block.ToStateID[block.Stone{}], true
	}
	return block.ToStateID[block.Air{}], true
}

func TestPearlLanding(t *testing.T) {
	// lanzada como un jugador (velocidad 1.5, a 30° hacia arriba) desde los ojos
	v := physics.Vec3{X: 1.5 * math.Cos(math.Pi/6), Y: 1.5 * math.Sin(math.Pi/6)}
	land, ok := pearlLanding(flatWorld{}, physics.Vec3{X: 0.5, Y: 65.62, Z: 0.5}, v)
	t.Logf("cae en %.1f %.1f %.1f", land.X, land.Y, land.Z)
	if !ok || land.Y < 63.9 || land.Y > 64.5 || land.X < 40 || land.X > 75 {
		t.Fatalf("en llano: cae en %.1f %.1f (ok=%v), se esperaba en el suelo a 40-75 bloques", land.X, land.Y, ok)
	}
	// un muro a 20 bloques: se queda antes
	w := flatWorld{}
	for y := 64; y < 120; y++ {
		for z := -3; z <= 3; z++ {
			w[world.BlockPos{X: 20, Y: y, Z: z}] = block.ToStateID[block.Stone{}]
		}
	}
	land, ok = pearlLanding(w, physics.Vec3{X: 0.5, Y: 65.62, Z: 0.5}, v)
	t.Logf("con muro: cae en %.1f %.1f %.1f", land.X, land.Y, land.Z)
	if !ok || land.X > 20 || land.X < 19 {
		t.Fatalf("con un muro a x=20 cae en %.1f (ok=%v)", land.X, ok)
	}
}
