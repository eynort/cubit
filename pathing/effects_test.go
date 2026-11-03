package pathing_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"

	"minecraft-bot/pathing"
)

// TestJumpBoost: un muro de 2 de alto, largo, sin otra forma de subir. Sin
// efecto no hay camino; con Salto II (un beacon) el bot lo sube de un salto,
// sin que el servidor lo devuelva.
func TestJumpBoost(t *testing.T) {
	for _, c := range []struct {
		name  string
		boost bool
	}{{"sin-efecto", false}, {"salto-II", true}} {
		t.Run(c.name, func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				g := simworld.Start(t, simworld.Options{Setup: []string{"fill 3 64 -200 3 65 200 stone", "fill 4 64 -200 8 65 200 stone"}})
				b := g.B
				b.Mover.Food = func() int { return 20 }
				if c.boost {
					id := slices.Index(registryid.MobEffect, "minecraft:jump_boost")
					e := play.UpdateMobEffect{EntityID: pk.VarInt(b.Player.Login.PlayerID), Effect: pk.VarInt(id), EffectAmplifier: 1, EffectDurationTicks: 100000}
					if err := g.S.Send(e); err != nil {
						t.Fatal(err)
					}
				}
				g.Step(5)
				w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
				b.Ctl.Think = w.Think
				w.Goto(world.BlockPos{X: 6, Y: 66, Z: 0})
				t0 := b.Ctl.State().Teleports
				g.Until(30*20, func() bool {
					st := w.Status()
					return st == "arrived" || strings.HasPrefix(st, "failed")
				})
				p := b.Player.Position()
				tps := b.Ctl.State().Teleports - t0
				t.Logf("%s en %.1f %.1f %.1f, correcciones %d", w.Status(), p.X, p.Y, p.Z, tps)
				if arrived := w.Status() == "arrived"; arrived != c.boost {
					t.Errorf("llegó=%v, se esperaba %v", arrived, c.boost)
				}
				if tps > 0 {
					t.Errorf("el servidor lo devolvió %d veces", tps)
				}
			})
		})
	}
}
