package combat_test

import (
	"fmt"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
)

// arsenalFight: el bot con lo de setup (sin la espada que dan las otras
// tácticas) contra un rival con kit, a dx, dz; el bot va a por él.
func arsenalFight(t *testing.T, setup []string, dx, dz float64, k simworld.Kit) (*simworld.Game, *logged, int32) {
	g, f := simFighter(t, setup...)
	lg := &logged{}
	f.Log = func(format string, args ...any) { t.Logf(format, args...); lg.add(fmt.Sprintf(format, args...)) }
	g.Step(3 * 20)
	if err := g.SummonDuelistKit(dx, dz, k); err != nil {
		t.Fatal(err)
	}
	id := g.LastMob()
	g.Step(lag + 2)
	f.Engage(id, "rival")
	return g, lg, id
}

func mobHP(g *simworld.Game, id int32) float32 {
	h, ok := g.MobHealth(id)
	if !ok {
		return 0
	}
	return h
}

// TestSpearStab: con la lanza, la estocada (de 2 a 4,5 bloques) le da a un
// rival quieto.
func TestSpearStab(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, _, id := arsenalFight(t, []string{"give Tester iron_spear 1"}, 5, 0, simworld.Kit{Eating: true})
		hurt := g.Until(15*20, func() bool { return g.Arsenal().Stabs >= 3 })
		t.Logf("estocadas %d, vida del rival %.1f (%+v)", g.Arsenal().Stabs, mobHP(g, id), g.Arsenal())
		if !hurt {
			t.Error("la lanza no le dio (o no pinchó)")
		}
	})
}

// TestSpearLunge: con la lanza y el rival quieto a 10: la embestida
// (corriendo hacia él con el uso mantenido) le da.
func TestSpearLunge(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, lg, id := arsenalFight(t, []string{"give Tester iron_spear 1"}, 10, 0, simworld.Kit{Eating: true})
		hit := g.Until(10*20, func() bool { return g.Arsenal().Lunges > 0 })
		t.Logf("embestidas que dieron %d, vida del rival %.1f", g.Arsenal().Lunges, mobHP(g, id))
		if !hit || !lg.has("embestida") {
			t.Error("no le embistió")
		}
	})
}

// TestWebSpear: telaraña a los pies de un rival con espada (a 4, sin que
// se acerque) y, atrapado, la lanza desde fuera de su alcance: le mata sin
// recibir un golpe.
func TestWebSpear(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, lg, id := arsenalFight(t, []string{"give Tester iron_sword 1", "give Tester iron_spear 1", "give Tester cobweb 8"}, 4, 0, simworld.Kit{Still: true})
		dead := g.Until(40*20, func() bool { return mobHP(g, id) <= 0 })
		a := g.Arsenal()
		t.Logf("muerto=%v; telarañas %d, estocadas %d, golpes recibidos %d, vida %.1f", dead, a.Webs, a.Stabs, g.MobHits(), g.Health)
		if a.Webs == 0 || !lg.has("telaraña a los pies") {
			t.Error("no le puso la telaraña")
		}
		if a.Stabs == 0 {
			t.Error("no usó la lanza con él atrapado")
		}
		if !dead {
			t.Error("no le mató")
		}
		if g.MobHits() > 1 { // (uno puede entrar antes de la telaraña)
			t.Errorf("le dio %d golpes (atrapado y a 4 no le llega)", g.MobHits())
		}
	})
}

// TestLavaPour: lava a los pies del rival (a 4, quieto): arde y pierde vida;
// a los 3 s el bot la recoge (y vuelve a tener el cubo lleno).
func TestLavaPour(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, lg, id := arsenalFight(t, []string{"give Tester iron_sword 1", "give Tester lava_bucket 1"}, 4, 0, simworld.Kit{Eating: true})
		h0 := mobHP(g, id)
		poured := g.Until(10*20, func() bool { return g.Arsenal().Lava > 0 })
		g.Step(20)
		t.Logf("lava=%v, vida del rival de %.1f a %.1f, ardiendo=%v", poured, h0, mobHP(g, id), g.Burning(id))
		if !poured || !lg.has("lava a los pies") {
			t.Fatal("no le echó la lava")
		}
		if mobHP(g, id) >= h0 {
			t.Error("la lava no le quitó vida")
		}
		back := g.Until(10*20, func() bool { return g.Count("minecraft:lava_bucket") > 0 })
		if !back {
			t.Error("no recogió su lava")
		}
		if g.Health < 20 {
			t.Errorf("el bot se quemó (vida %.1f)", g.Health)
		}
	})
}

// TestMaceSmash: maza y cargas de viento, el rival a 2 bloques: salta con
// la carga a los pies y cae encima con la maza (el golpe con la caída).
func TestMaceSmash(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, lg, id := arsenalFight(t, []string{"give Tester iron_sword 1", "give Tester mace 1", "give Tester wind_charge 8"}, 2.5, 0, simworld.Kit{Eating: true})
		smashed := g.Until(15*20, func() bool { return g.Arsenal().Smashes > 0 })
		a := g.Arsenal()
		t.Logf("cargas %d, golpes de maza cayendo %d (el mayor +%.0f), vida del rival %.1f, la del bot %.1f", a.Winds, a.Smashes, a.SmashMax, mobHP(g, id), g.Health)
		if !lg.has("carga de viento a los pies") || a.Winds == 0 {
			t.Fatal("no saltó con la carga de viento")
		}
		if !smashed || a.SmashMax < 8 {
			t.Error("no le dio con la maza cayendo (o de poca altura)")
		}
		if g.Health < 20 {
			t.Errorf("se hizo daño en la caída (vida %.1f)", g.Health)
		}
	})
}

// TestTridentThrow: tridente con lealtad y el rival quieto a 14: lo lanza.
func TestTridentThrow(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, f := simFighter(t)
		lg := &logged{}
		f.Log = func(format string, args ...any) { t.Logf(format, args...); lg.add(fmt.Sprintf(format, args...)) }
		g.GiveEnchanted("minecraft:trident", map[string]int{"loyalty": 3})
		g.Step(3 * 20)
		if err := g.SummonDuelistKit(14, 0, simworld.Kit{Still: true}); err != nil {
			t.Fatal(err)
		}
		id := g.LastMob()
		g.Step(lag + 2)
		f.Engage(id, "rival")
		hit := g.Until(15*20, func() bool { return g.Arsenal().TridentHits > 0 })
		a := g.Arsenal()
		t.Logf("lanzados %d, aciertos %d, vida del rival %.1f", a.Tridents, a.TridentHits, mobHP(g, id))
		if !hit {
			t.Error("no le dio con el tridente")
		}
		if g.Count("minecraft:trident") == 0 {
			t.Error("perdió el tridente (tiene lealtad)")
		}
	})
}

// TestWindPushEdge: el rival con un foso detrás: carga de viento.
func TestWindPushEdge(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, lg, _ := arsenalFight(t, []string{"give Tester iron_sword 1", "give Tester wind_charge 4", "fill 7 50 -4 12 63 4 air"}, 5, 0, simworld.Kit{Still: true})
		g.Until(10*20, func() bool { return g.Arsenal().Winds > 0 })
		if !lg.has("vacío detrás") {
			t.Error("no le tiró la carga de viento con el foso detrás")
		}
	})
}

// TestNoFleeFromPlayer: contra uno que le supera, con poca vida, perlas y
// comida pero sin bloques: no huye (ni corriendo ni con la perla), pelea.
func TestNoFleeFromPlayer(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, lg, _ := arsenalFight(t, append([]string{"give Tester ender_pearl 8"}, kit...), 6, 0,
			simworld.Kit{Weapon: "minecraft:diamond_sword", Sharpness: 5, Armor: "diamond", Protection: 4})
		if err := g.SetHealth(8); err != nil {
			t.Fatal(err)
		}
		g.Until(20*20, func() bool { return g.Health <= 0 })
		if lg.has("me retiro") || lg.has("perla de ender para irme") {
			t.Error("huyó de un jugador")
		}
		if g.Health > 0 && g.Count("minecraft:ender_pearl") < 8 {
			t.Error("gastó una perla para irse")
		}
	})
}
