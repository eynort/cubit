package automation_test

import (
	"context"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"

	"minecraft-bot/automation"
)

// TestHuntWhenHungry: con hambre (comida 6) y nada que comer, la tarea no
// sigue a lo suyo: caza la vaca de al lado, cocina la carne y come.
func TestHuntWhenHungry(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester iron_sword 1", "give Tester cobblestone 8", "give Tester oak_log 6", "give Tester crafting_table 1",
		}})
		r, _ := safeRunner(t, g)
		g.Step(5)
		if err := g.SummonNear("minecraft:cow", 6, 2); err != nil {
			t.Fatal(err)
		}
		if err := g.SetFood(6); err != nil {
			t.Fatal(err)
		}
		g.Step(2)
		var err error
		r.Start(context.Background(), "algo", func(j *automation.Job) error {
			for i := 0; i < 20; i++ { // una tarea cualquiera: en cada Wait mira el hambre
				if err = j.Wait(5); err != nil {
					return err
				}
			}
			return nil
		})
		g.Until(300*20, func() bool { return r.Status() == "" })
		t.Logf("error %v; comida %d; vacas %d; carne cocinada %d, cruda %d", err, g.Food, g.Mobs(), g.Count("minecraft:cooked_beef"), g.Count("minecraft:beef"))
		if err != nil {
			t.Fatal(err)
		}
		if g.Food <= 6 {
			t.Errorf("no comió (comida %d)", g.Food)
		}
		if g.Count("minecraft:cooked_beef")+g.Count("minecraft:beef") == 0 && g.Food <= 10 {
			t.Errorf("no cazó")
		}
	})
}
