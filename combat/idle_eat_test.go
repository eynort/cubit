package combat_test

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
)

// TestEatsWhenHungryIdle: sin pelea (como el bot en /afk, o en cualquier
// tarea) come solo cuando tiene hambre.
func TestEatsWhenHungryIdle(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, _ := simFighter(t, "give Tester cooked_beef 4")
		g.Step(20)
		if err := g.SetFood(8); err != nil {
			t.Fatal(err)
		}
		ate := g.Until(20*20, func() bool { return g.Food > 8 })
		t.Logf("comió=%v, comida %d", ate, g.Food)
		if !ate {
			t.Error("con hambre y comida en el inventario, no comió")
		}
	})
}
