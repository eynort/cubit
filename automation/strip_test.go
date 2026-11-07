package automation_test

import (
	"context"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"

	"minecraft-bot/automation"
)

// TestStripIron: con pico de piedra, baja a y=16 y hace galerías hasta dar con
// una fila de hierro lejos del principio (a 20 bloques en x, de lado a lado en z).
func TestStripIron(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester stone_pickaxe 3", "give Tester cobblestone 32",
			"fill 22 16 -40 22 17 40 minecraft:iron_ore",
		}})
		r := runner(t, g)
		g.Step(5)
		var err error
		r.Start(context.Background(), "minar", func(j *automation.Job) error {
			err = j.StripMineForTest("hierro", 3)
			return err
		})
		g.Until(1500*20, func() bool { return r.Status() == "" })
		p := g.B.Player.Position()
		t.Logf("error %v; el bot en %.1f %.1f %.1f; hierro %d", err, p.X, p.Y, p.Z, g.Count("minecraft:raw_iron"))
		if err != nil || g.Count("minecraft:raw_iron") < 3 {
			t.Fatalf("no sacó el hierro")
		}
		if p.Y > 20 || p.Y < 14 {
			t.Errorf("no minó a la altura del hierro (y=%.1f)", p.Y)
		}
	})
}

// TestParseOres: nombres en inglés y español, separados por coma, sin repetir.
func TestParseOres(t *testing.T) {
	os, err := automation.ParseOres("diamante, iron,diamonds,escombros")
	if err != nil || len(os) != 3 || os[0].Key != "diamond" || os[1].Key != "iron" || os[2].Key != "debris" {
		t.Fatalf("ParseOres: %v %v", os, err)
	}
	if _, err := automation.ParseOres("plastilina"); err == nil {
		t.Fatalf("un mineral que no existe tendría que dar error")
	}
}

// TestStripSnake: un escombro que solo se ve desde la 3ª galería (los
// escombros se buscan a 6 bloques): las galerías tienen que dar la vuelta
// en serpiente, cada 3, hasta pasar a su lado.
func TestStripSnake(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester diamond_pickaxe 1", "give Tester cobblestone 32",
			"fill 16 16 15 16 16 15 minecraft:ancient_debris",
		}})
		r := runner(t, g)
		g.Step(5)
		var err error
		r.Start(context.Background(), "minar", func(j *automation.Job) error {
			err = j.StripMineForTest("debris", 1)
			return err
		})
		g.Until(3000*20, func() bool { return r.Status() == "" })
		p := g.B.Player.Position()
		t.Logf("error %v; el bot en %.1f %.1f %.1f; escombros %d", err, p.X, p.Y, p.Z, g.Count("minecraft:ancient_debris"))
		if err != nil || g.Count("minecraft:ancient_debris") < 1 {
			t.Fatalf("no sacó el escombro")
		}
	})
}
