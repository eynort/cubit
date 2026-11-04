package safety_test

import (
	"testing"

	"github.com/mj41/go-mc26/data/entitydata"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/simworld"

	"minecraft-bot/safety"
)

// TestPutOutFire: ardiendo 8 s (un punto por segundo), con un cubo de agua se
// apaga al momento (agua a los pies) y la recoge; sin cubo, arde entero.
func TestPutOutFire(t *testing.T) {
	for _, bucket := range []bool{true, false} {
		simworld.Run(t, func(t *testing.T) {
			setup := []string{"give Tester iron_sword 1"}
			if bucket {
				setup = append(setup, "give Tester water_bucket 1")
			}
			g := simworld.Start(t, simworld.Options{Setup: setup, Lag: 3})
			b := g.B
			f := &safety.Fire{Client: b.C, Player: b.Player, World: b.World, Mover: b.Mover, Screens: b.Screens, Hands: b.Hands, Logf: t.Logf}
			f.Register()
			b.Ctl.Think = func(s *control.State, bp *basic.Player) { f.Think(s, bp) }
			g.Step(10)
			if err := g.SetOnFire(160); err != nil {
				t.Fatal(err)
			}
			g.Step(12 * 20)
			t.Logf("con cubo %v: vida %.0f, fuego que queda %d, cubos de agua %d, apagado %d", bucket, g.Health, g.OnFire(), g.Count("minecraft:water_bucket"), f.Outs())
			if bucket && (g.Health < 19 || g.Count("minecraft:water_bucket") != 1) {
				t.Errorf("no se apagó a tiempo o no recogió el agua")
			}
			if !bucket && g.Health > 17 { // (ardió: con el hambre llena se regenera a la vez)
				t.Errorf("sin cubo no ardió (el fuego del simulador no funciona)")
			}
		})
	}
}

// TestRefillBucket: tranquilo, con un cubo vacío y agua a unos bloques: lo
// rellena (siempre lleva agua para la próxima).
func TestRefillBucket(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{"give Tester bucket 1", "setblock 3 63 0 water"}, Lag: 4})
		b := g.B
		f := &safety.Fire{Client: b.C, Player: b.Player, World: b.World, Mover: b.Mover, Screens: b.Screens, Hands: b.Hands, Logf: t.Logf,
			Calm: func() bool { return true }}
		f.Register()
		b.Ctl.Think = func(s *control.State, bp *basic.Player) { f.Think(s, bp) }
		full := g.Until(5*20, func() bool { return g.Count("minecraft:water_bucket") == 1 })
		t.Logf("lleno %v; cubos vacíos %d", full, g.Count("minecraft:bucket"))
		if !full {
			t.Error("no rellenó el cubo")
		}
	})
}

// TestSurfaceToBreathe: en el fondo de un lago con poco aire (el servidor lo
// dice en los datos de la entidad): nada hacia arriba hasta respirar (en los
// logs: ahogado peleando en un lago).
func TestSurfaceToBreathe(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{"fill -3 56 -3 3 63 3 water"}, Lag: 4})
		b := g.B
		f := &safety.Fire{Client: b.C, Player: b.Player, World: b.World, Mover: b.Mover, Screens: b.Screens, Hands: b.Hands, Logf: t.Logf}
		f.Register()
		b.Ctl.Think = func(s *control.State, bp *basic.Player) { f.Think(s, bp) }
		g.Teleport(0.5, 56, 0.5)
		g.Step(20)
		air := pk.VarInt(100)
		if err := g.S.Send(play.SetEntityData{ID: 1, PackedItems: types.EntityData{{Index: entitydata.EntityAirSupply, Serializer: 1, Value: &air}}}); err != nil {
			t.Fatal(err)
		}
		y0 := g.Pos()[1]
		up := g.Until(10*20, func() bool { return g.Pos()[1] > 62.5 })
		t.Logf("de %.1f a %.1f; aire visto %d", y0, g.Pos()[1], f.Air())
		if !up {
			t.Error("no subió a respirar")
		}
	})
}
