package pathing_test

import (
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/pathing"
)

// TestDropFromTower: encima de una torre de 5 sin bloques para bajar
// construyendo: salta y se come el daño de la caída (5-3 = 2), si su vida lo
// aguanta; no se queda arriba.
func TestDropFromTower(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{"fill 5 64 0 5 68 0 stone"}})
		b := g.B
		b.Mover.Food = func() int { return 20 }
		w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		b.Ctl.Think = w.Think
		g.Teleport(5.5, 69, 0.5)
		g.Step(10)
		before := g.Health
		w.Goto(world.BlockPos{X: 9, Y: 64, Z: 0})
		g.Until(20*20, func() bool {
			st := w.Status()
			return st == "arrived" || strings.HasPrefix(st, "failed")
		})
		p := b.Player.Position()
		t.Logf("%s en %.1f %.1f %.1f; vida %.0f → %.0f", w.Status(), p.X, p.Y, p.Z, before, g.Health)
		if w.Status() != "arrived" {
			t.Fatal("se quedó arriba")
		}
		if lost := before - g.Health; lost < 1.5 || lost > 2.5 {
			t.Errorf("perdió %.1f de vida; una caída de 5 quita 2", lost)
		}
	})
}

// TestChainedDescents: bajando una ladera de escalones (1 y luego 3) no se
// encadenan en una caída de 4 que duele: andando, aterriza en cada uno.
func TestChainedDescents(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"fill 2 64 -3 4 67 3 stone", // arriba: pies a 68
			"fill 5 64 -3 5 66 3 stone", // un escalón: 67
		}})
		b := g.B
		b.Mover.Food = func() int { return 20 }
		w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		b.Ctl.Think = w.Think
		g.Teleport(2.5, 68, 0.5)
		g.Step(10)
		before := g.Health
		w.Goto(world.BlockPos{X: 12, Y: 64, Z: 0})
		g.Until(20*20, func() bool {
			st := w.Status()
			return st == "arrived" || strings.HasPrefix(st, "failed")
		})
		t.Logf("%s; vida %.0f → %.0f", w.Status(), before, g.Health)
		if w.Status() != "arrived" {
			t.Fatal("no llegó")
		}
		if g.Health < before {
			t.Errorf("se hizo daño bajando: %.0f → %.0f", before, g.Health)
		}
	})
}

// TestNoRiskyGapWhenHurt: entre dos torres de 10 hay un hueco de 1. Con la
// vida llena lo salta (fallar dolería pero no mataría); con 5 de vida no: un
// mal salto sería morir.
func TestNoRiskyGapWhenHurt(t *testing.T) {
	for _, hp := range []float32{20, 5} {
		simworld.Run(t, func(t *testing.T) {
			g := simworld.Start(t, simworld.Options{Setup: []string{
				"fill 0 64 -1 3 72 1 stone", "fill 5 64 -1 9 72 1 stone", // pies a 73; hueco en x=4
			}})
			b := g.B
			b.Mover.Food = func() int { return 20 }
			g.Health = hp
			w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
			b.Ctl.Think = w.Think
			g.Teleport(1.5, 73, 0.5)
			g.Step(20)
			w.Goto(world.BlockPos{X: 8, Y: 73, Z: 0})
			g.Until(20*20, func() bool {
				st := w.Status()
				return st == "arrived" || strings.HasPrefix(st, "failed")
			})
			p := b.Player.Position()
			t.Logf("vida %.0f: %s en %.1f %.1f %.1f (vida al final %.0f)", hp, w.Status(), p.X, p.Y, p.Z, g.Health)
			if hp >= 20 && w.Status() != "arrived" {
				t.Errorf("con la vida llena no saltó el hueco")
			}
			if hp < 10 && (w.Status() == "arrived" || p.Y < 72) {
				t.Errorf("con %.0f de vida se arriesgó a saltar", hp)
			}
		})
	}
}

// TestFloatWhenIdle: quieto y sin camino en agua honda, no se hunde (sale a respirar).
func TestFloatWhenIdle(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{"fill -4 56 -4 4 66 4 water"}})
		b := g.B
		w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		b.Ctl.Think = w.Think
		g.Teleport(0.5, 58, 0.5)
		g.Step(200)
		p := b.Player.Position()
		t.Logf("tras 10 s en el agua: y=%.1f, vida %.0f", p.Y, g.Health)
		if p.Y+1.62 < 66.5 {
			t.Errorf("sigue con la cabeza bajo el agua (y=%.1f)", p.Y)
		}
	})
}
