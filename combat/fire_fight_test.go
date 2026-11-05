package combat_test

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/simworld"

	"minecraft-bot/combat"
	"minecraft-bot/safety"
)

// TestFireInFight: ardiendo en plena pelea (aspecto ígneo, lava), con un cubo
// de agua: se apaga y lo recoge, aunque la pelea quiera la espada en la mano
// (antes el combate volvía a poner la espada cada paso y el cubo no salía
// nunca: en el servidor, muerto quemado). Con el cubo en la barra y en el
// inventario (lo pasa a la barra).
func TestFireInFight(t *testing.T) {
	for _, where := range []string{"barra", "inventario"} {
		t.Run(where, func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				setup := append([]string{}, kit...)
				if where == "inventario" { // la barra llena: el cubo va al inventario
					for i := 0; i < 6; i++ {
						setup = append(setup, "give Tester cobblestone 64")
					}
				}
				setup = append(setup, "give Tester water_bucket 1")
				g, f := simFighter(t, setup...)
				b := g.B
				fire := &safety.Fire{Client: b.C, Player: b.Player, World: b.World, Mover: b.Mover, Screens: b.Screens, Hands: b.Hands, Logf: t.Logf}
				fire.Register()
				fight := b.Ctl.Think
				b.Ctl.Think = func(s *control.State, bp *basic.Player) { // (como main.go: el reflejo antes que la pelea)
					if fire.Think(s, bp) {
						return
					}
					fight(s, bp)
				}
				f.Reflex = fire.Active
				fire.BeforeUse = f.ReleaseHands
				g.Step(3 * 20)
				if err := g.SummonDuelistKit(4, 0, simworld.Kit{Still: true}); err != nil {
					t.Fatal(err)
				}
				id := g.LastMob()
				g.Step(lag + 2)
				f.Engage(id, "rival")
				g.Step(20)
				if err := g.SetOnFire(160); err != nil {
					t.Fatal(err)
				}
				out := g.Until(6*20, func() bool { return fire.Outs() > 0 })
				g.Step(3 * 20)
				t.Logf("apagado %v (%d); fuego que queda %d; cubos de agua %d, vacíos %d; vida %.1f; alrededor %s", out, fire.Outs(), g.OnFire(), g.Count("minecraft:water_bucket"), g.Count("minecraft:bucket"), g.Health, g.Around(int(g.Pos()[0]), 64, int(g.Pos()[2])))
				if !out {
					t.Fatal("no se apagó con el cubo peleando")
				}
				if g.Count("minecraft:water_bucket") != 1 {
					t.Error("no recogió el agua")
				}
			})
		})
	}
}

// TestFireToWater: ardiendo peleando y sin cubo, con agua a 5 bloques: va a
// ella y se apaga (antes ardía entero: en los logs, 7 de vida así).
func TestFireToWater(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, f := simFighter(t, append([]string{"fill -6 63 -1 -5 63 1 water"}, kit...)...)
		b := g.B
		fire := &safety.Fire{Client: b.C, Player: b.Player, World: b.World, Mover: b.Mover, Screens: b.Screens, Hands: b.Hands, Logf: t.Logf}
		fire.Register()
		f.Burning = fire.Burning
		g.Step(3 * 20)
		if err := g.SummonDuelistKit(4, 0, simworld.Kit{Still: true}); err != nil {
			t.Fatal(err)
		}
		id := g.LastMob()
		g.Step(lag + 2)
		f.Engage(id, "rival")
		g.Step(10)
		if err := g.SetOnFire(160); err != nil {
			t.Fatal(err)
		}
		out := g.Until(5*20, func() bool { return g.OnFire() == 0 })
		t.Logf("apagado %v; fuego que queda %d; vida %.1f; en %.1f", out, g.OnFire(), g.Health, g.Pos())
		if !out {
			t.Error("no fue al agua")
		}
	})
}

// fireFighter: simFighter con el reflejo del fuego conectado como en main.go.
func fireFighter(t *testing.T, setup ...string) (*simworld.Game, *combat.Fighter, *safety.Fire, *logged) {
	g, f := simFighter(t, setup...)
	b := g.B
	fire := &safety.Fire{Client: b.C, Player: b.Player, World: b.World, Mover: b.Mover, Screens: b.Screens, Hands: b.Hands, Logf: t.Logf}
	fire.Register()
	fight := b.Ctl.Think
	b.Ctl.Think = func(s *control.State, bp *basic.Player) {
		if fire.Think(s, bp) {
			return
		}
		fight(s, bp)
	}
	f.Reflex, f.Burning = fire.Active, fire.Burning
	fire.BeforeUse = f.ReleaseHands
	lg := &logged{}
	f.Log = func(format string, args ...any) { t.Logf(format, args...); lg.add(format) }
	return g, f, fire, lg
}

// TestFireBeforeBunker: ardiendo, sin cubo, con agua a unos pasos y uno que
// le supera viniendo: primero al agua, luego el búnker (antes se encerraba
// ardiendo y se quemaba dentro: de 14 a 9).
func TestFireBeforeBunker(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		setup := append(append([]string{"fill -5 63 -1 -4 63 1 water"}, ironArmor...), "give Tester iron_sword 1", "give Tester golden_carrot 16", "give Tester cobblestone 64")
		g, _, _, lg := fireFighter(t, setup...)
		g.Until(5*20, func() bool { return g.Worn(8) != "" })
		if err := g.SetHealth(10); err != nil {
			t.Fatal(err)
		}
		if err := g.SetOnFire(160); err != nil {
			t.Fatal(err)
		}
		rival := simworld.Kit{Weapon: "minecraft:diamond_sword", Sharpness: 5, Armor: "diamond", Protection: 4}
		if err := g.SummonDuelistKit(14, 0, rival); err != nil {
			t.Fatal(err)
		}
		bunkerBurning := false
		out := g.Until(10*20, func() bool {
			if g.OnFire() > 0 && lg.has("me encierro con bloques (%s; tengo %d)") {
				bunkerBurning = true
			}
			return g.OnFire() == 0
		})
		t.Logf("apagado %v; se encerró ardiendo %v; vida %.1f", out, bunkerBurning, g.Health)
		if !out || bunkerBurning {
			t.Error("no se apagó antes de encerrarse")
		}
	})
}

// TestFireInBunker: encerrado y comiendo, se prende (una flecha en llamas, su
// lava): con el cubo se apaga dentro (deja de comer para echarla) y lo recoge.
func TestFireInBunker(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		setup := append(append([]string{}, ironArmor...), "give Tester iron_sword 1", "give Tester golden_carrot 16", "give Tester cobblestone 64", "give Tester water_bucket 1")
		g, _, fire, lg := fireFighter(t, setup...)
		g.Until(5*20, func() bool { return g.Worn(8) != "" })
		if err := g.SetHealth(8); err != nil {
			t.Fatal(err)
		}
		rival := simworld.Kit{Weapon: "minecraft:diamond_sword", Sharpness: 5, Armor: "diamond", Protection: 4}
		if err := g.SummonDuelistKit(14, 0, rival); err != nil {
			t.Fatal(err)
		}
		if !g.Until(20*20, func() bool { return lg.has("cerrado (8 paredes%s): vida %.1f") }) {
			t.Fatal("no se encerró")
		}
		g.Step(10)
		if err := g.SetOnFire(160); err != nil {
			t.Fatal(err)
		}
		out := g.Until(5*20, func() bool { return g.OnFire() == 0 })
		g.Step(3 * 20)
		t.Logf("apagado %v (%d); cubos de agua %d; vida %.1f", out, fire.Outs(), g.Count("minecraft:water_bucket"), g.Health)
		if !out {
			t.Error("no se apagó en el búnker")
		}
		if g.Count("minecraft:water_bucket") != 1 {
			t.Error("no recogió el agua")
		}
	})
}
