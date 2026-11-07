package automation_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/automation"
)

const spruceLeaves = "minecraft:spruce_leaves[distance=1,persistent=false,waterlogged=false]"

// spruce pone un abeto como los de la taiga: tronco de h y una copa en cono,
// capas de radio 2 y 1 alternadas desde 2 bloques sobre el suelo, hasta la punta.
func spruce(x, z, h int) []string { return spruceAt(x, 64, z, h) }

// spruceAt es spruce con el pie del tronco a la altura y0.
func spruceAt(x, y0, z, h int) []string {
	var s []string
	top := y0 - 1 + h
	for y := y0 + 2; y <= top+1; y++ {
		r := 1
		if (y-y0-2)%2 == 0 && y < top-1 {
			r = 2
		}
		if y > top-1 {
			r = 1
		}
		s = append(s, sprintf("fill %d %d %d %d %d %d %s", x-r, y, z-r, x+r, y, z+r, spruceLeaves))
	}
	s = append(s, sprintf("fill %d %d %d %d %d %d minecraft:spruce_log[axis=y]", x, y0, z, x, top, z))
	return s
}

// TestTallSpruce: un abeto de 14 con la copa hasta abajo: tala entero sin
// quedarse atascado entre las hojas.
func TestTallSpruce(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		x, z, h := 5, 0, 14
		setup := append(spruce(x, z, h), "give Tester dirt 32", "give Tester oak_log 2", "give Tester stone_axe 1")
		g := simworld.Start(t, simworld.Options{Setup: setup})
		r := runner(t, g)
		g.Step(5)
		r.Start(context.Background(), "talar", automation.Woodcut("spruce", h))
		g.Until(400*20, func() bool { return r.Status() == "" })
		standing := 0
		for y := 64; y <= 63+h; y++ {
			if st, ok := g.B.World.BlockAt(world.BlockPos{X: x, Y: y, Z: z}); ok && strings.Contains(world.StateString(st), "_log") {
				standing++
			}
		}
		p := g.B.Player.Position()
		t.Logf("quedan %d troncos; el bot en %.1f %.1f %.1f; tick %d", standing, p.X, p.Y, p.Z, g.Tick)
		if standing > 0 {
			t.Errorf("quedan %d troncos en pie", standing)
		}
	})
}

// TestSpruceOnHill: el abeto en lo alto de un escalón de 5 (como en la taiga
// de las montañas), con la copa hasta abajo y pegada al borde: sube con una
// torre y tala entero sin quedarse en bucle entre las hojas.
func TestSpruceOnHill(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		x, z, h := 6, 0, 14
		setup := []string{"fill 4 64 -8 20 68 8 minecraft:stone"}
		setup = append(setup, spruceAt(x, 69, z, h)...)
		setup = append(setup, "give Tester dirt 32", "give Tester oak_log 2", "give Tester stone_axe 1")
		g := simworld.Start(t, simworld.Options{Setup: setup})
		r := runner(t, g)
		g.Step(5)
		r.Start(context.Background(), "talar", automation.Woodcut("spruce", h))
		g.Until(400*20, func() bool { return r.Status() == "" })
		standing := 0
		for y := 69; y <= 68+h; y++ {
			if st, ok := g.B.World.BlockAt(world.BlockPos{X: x, Y: y, Z: z}); ok && strings.Contains(world.StateString(st), "_log") {
				standing++
			}
		}
		p := g.B.Player.Position()
		t.Logf("quedan %d troncos; el bot en %.1f %.1f %.1f; tick %d", standing, p.X, p.Y, p.Z, g.Tick)
		if standing > 0 {
			t.Errorf("quedan %d troncos en pie", standing)
		}
	})
}
