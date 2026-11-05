package combat_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"

	"minecraft-bot/combat"
	"minecraft-bot/pathing"
)

// lag: los ticks que tardan en llegar al bot los paquetes del servidor (el
// real va a unos 100-150 ms por sentido). Sin él, lo que depende de esperar la
// respuesta del servidor (un click, la tecla F) funciona siempre en el
// simulador y falla en el servidor de verdad.
const lag = 4

// simFighter pone en marcha el módulo de pelea en un mundo con setup (con lag).
func simFighter(t *testing.T, setup ...string) (*simworld.Game, *combat.Fighter) {
	g := simworld.Start(t, simworld.Options{Setup: setup, Lag: lag})
	b := g.B
	w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
	builder := &pathing.HandsBuilder{Screens: b.Screens, Hands: b.Hands, Ctl: b.Ctl, Ctx: context.Background()}
	w.SetBuilder(builder) // (como el bot: torres y puentes)
	f := combat.New(combat.Deps{
		Client: b.C, Player: b.Player, Entities: b.Entities, Screens: b.Screens, World: b.World,
		Hands: b.Hands, Ctl: b.Ctl, Walker: w, Mover: b.Mover, Builder: builder,
		NameOf: func(uuid.UUID) string { return "" },
	})
	f.Log = t.Logf
	b.Ctl.Think = f.Think
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = f.Run(ctx) }()
	return g, f
}

// el equipo del bot en los logs: espada de hierro, escudo, tótem y comida.
var kit = []string{"give Tester iron_sword 1", "give Tester shield 1", "give Tester totem_of_undying 1", "give Tester cooked_beef 8"}

// TestOffhandPolicy: fuera de pelea, el tótem en la mano izquierda (lo
// inesperado: una caída, la lava); peleando contra lo que se para con escudo
// (un esqueleto), el escudo.
func TestOffhandPolicy(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, _ := simFighter(t, kit...)
		g.Until(5*20, func() bool { return g.Offhand() != "" })
		if o := g.Offhand(); o != "minecraft:totem_of_undying" {
			t.Fatalf("tranquilo: en la mano izquierda %q, quería el tótem", o)
		}
		if err := g.SummonNear("minecraft:skeleton", 12, 0); err != nil {
			t.Fatal(err)
		}
		if !g.Until(10*20, func() bool { return g.Offhand() == "minecraft:shield" }) {
			t.Fatalf("contra un esqueleto: en la mano izquierda %q, quería el escudo", g.Offhand())
		}
	})
}

// TestCreeperShield: un creeper con el bot armado y con escudo: lo mata, o si
// se enciende para la explosión con el escudo. Ni muere ni gasta el tótem
// (en los logs: cada creeper, un tótem o la muerte).
func TestCreeperShield(t *testing.T) {
	for _, dist := range []float64{7, 2.5} { // de frente, y encima (sin verlo venir)
		simworld.Run(t, func(t *testing.T) {
			g, _ := simFighter(t, kit...)
			g.Step(20)
			if err := g.SummonNear("minecraft:creeper", dist, 0); err != nil {
				t.Fatal(err)
			}
			id := g.LastMob()
			minHealth := g.Health
			g.Until(40*20, func() bool {
				minHealth = min(minHealth, g.Health)
				_, alive := g.MobHealth(id)
				return !alive
			})
			t.Logf("a %.1f: vida %.0f (mínima %.0f), explosiones %d, bloqueados %d, tótems %d",
				dist, g.Health, minHealth, g.Blasts(), g.Blocks(), g.TotemsUsed())
			if g.Deaths() > 0 || g.TotemsUsed() > 0 {
				t.Errorf("a %.1f: murió %d veces, gastó %d tótems", dist, g.Deaths(), g.TotemsUsed())
			}
			if minHealth < 10 {
				t.Errorf("a %.1f: bajó a %.0f de vida", dist, minHealth)
			}
		})
	}
}

// TestSkeletonShield: un esqueleto a 16 bloques: va a por él cubriéndose
// cuando tensa el arco, y lo mata sin dejarse media vida (en los logs: lo
// persiguió a pecho descubierto y murió a flechazos).
func TestSkeletonShield(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, _ := simFighter(t, kit...)
		g.Step(20)
		if err := g.SummonNear("minecraft:skeleton", 16, 0); err != nil {
			t.Fatal(err)
		}
		id := g.LastMob()
		minHealth := g.Health
		dead := g.Until(60*20, func() bool {
			minHealth = min(minHealth, g.Health)
			_, alive := g.MobHealth(id)
			return !alive
		})
		t.Logf("muerto %v; vida %.0f (mínima %.0f), bloqueados %d, tótems %d", dead, g.Health, minHealth, g.Blocks(), g.TotemsUsed())
		if !dead {
			t.Errorf("no mató al esqueleto en 60 s")
		}
		if g.Deaths() > 0 || g.TotemsUsed() > 0 || minHealth < 10 {
			t.Errorf("murió %d, tótems %d, vida mínima %.0f", g.Deaths(), g.TotemsUsed(), minHealth)
		}
		if g.Blocks() == 0 {
			t.Errorf("no paró ninguna flecha con el escudo")
		}
	})
}

// TestCreeperNoShield: sin escudo, con el tótem: no muere (huye o el tótem lo salva).
func TestCreeperNoShield(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, _ := simFighter(t, "give Tester iron_sword 1", "give Tester totem_of_undying 1", "give Tester cooked_beef 8")
		g.Step(20)
		if err := g.SummonNear("minecraft:creeper", 7, 0); err != nil {
			t.Fatal(err)
		}
		g.Until(40*20, func() bool { return g.Deaths() > 0 })
		t.Logf("vida %.0f, explosiones %d, tótems %d", g.Health, g.Blasts(), g.TotemsUsed())
		if g.Deaths() > 0 {
			t.Errorf("murió")
		}
	})
}

// TestCreeperFromLogs: los dos casos de los logs. Talando (el hacha de
// piedra en la mano: cambiar a la espada reinicia la carga del golpe) y con
// poca vida (antes no se vigilaba a un creeper que no hubiera pegado: lo vio
// a medio bloque). Ninguno le cuesta un tótem ni la vida.
func TestCreeperFromLogs(t *testing.T) {
	for _, c := range []struct {
		name   string
		health float32
		dist   float64
	}{{"talando", 20, 3}, {"con poca vida", 9, 7}} {
		t.Run(c.name, func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				g, _ := simFighter(t, append([]string{"give Tester stone_axe 1"}, kit...)...)
				g.Health = c.health
				g.Step(20)
				if err := g.SummonNear("minecraft:creeper", c.dist, 0); err != nil {
					t.Fatal(err)
				}
				id := g.LastMob()
				minHealth := g.Health
				g.Until(40*20, func() bool {
					minHealth = min(minHealth, g.Health)
					_, alive := g.MobHealth(id)
					return !alive
				})
				t.Logf("vida %.0f (mínima %.0f), explosiones %d, bloqueados %d, tótems %d",
					g.Health, minHealth, g.Blasts(), g.Blocks(), g.TotemsUsed())
				if g.Deaths() > 0 || g.TotemsUsed() > 0 || minHealth < c.health-6 {
					t.Errorf("murió %d, tótems %d, vida mínima %.0f", g.Deaths(), g.TotemsUsed(), minHealth)
				}
			})
		})
	}
}

// TestEatFromPack: la comida en el inventario (la barra rápida llena) y con
// lag: tiene que pasarla a la barra y comérsela. En los logs, 122 intentos
// seguidos de "the held item is not eaten or drunk": el click que la pasa a
// la barra no se veía hasta que contestaba el servidor, y comía antes.
func TestEatFromPack(t *testing.T) {
	setup := []string{"give Tester iron_sword 1"}
	for i := 0; i < 8; i++ {
		setup = append(setup, "give Tester dirt 64")
	}
	setup = append(setup, "give Tester golden_carrot 8")
	simworld.Run(t, func(t *testing.T) {
		g, _ := simFighter(t, setup...)
		g.Food, g.Health = 12, 10
		g.Step(15 * 20)
		t.Logf("comida %d, vida %.0f, zanahorias %d", g.Food, g.Health, g.Count("minecraft:golden_carrot"))
		if n := g.Count("minecraft:golden_carrot"); n > 6 { // (vida 10, comida 12: dos)
			t.Errorf("comió %d de 2 veces", 8-n)
		}
	})
}

// TestOffhandNoFlapping: con lag, la F se pulsa una vez por cambio (antes,
// sin ver aún el cambio, se pulsaba otra vez y se deshacía).
func TestOffhandNoFlapping(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, _ := simFighter(t, kit...)
		g.Step(10 * 20)
		if o := g.Offhand(); o != "minecraft:totem_of_undying" {
			t.Errorf("tranquilo: en la mano izquierda %q, quería el tótem", o)
		}
		swaps := 0
		for _, p := range g.S.Got(packetid.ServerboundPlayPlayerAction) {
			var a play.PlayerAction
			if p.Scan(&a) == nil && a.Action == types.PlayerActionActionSwapItemWithOffhand {
				swaps++
			}
		}
		if swaps > 1 {
			t.Errorf("pulsó la F %d veces para un cambio", swaps)
		}
	})
}

// TestGiveUpUnreachable: un zombi encerrado (sin bloques para llegar): en vez
// de dar vueltas en el sitio sin fin, lo deja (y no vuelve a por él).
func TestGiveUpUnreachable(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, f := simFighter(t, "fill 8 64 -1 10 66 1 stone", "fill 9 64 0 9 66 0 air",
			"fill 8 67 -1 10 67 1 minecraft:oak_leaves[persistent=true]", "give Tester iron_sword 1")
		g.Step(20)
		if err := g.Summon("minecraft:zombie", 9.5, 0.5); err != nil {
			t.Fatal(err)
		}
		id := g.LastMob()
		g.Step(5)
		f.Engage(id, "zombie encerrado")
		gaveUp := g.Until(20*20, func() bool { return !f.Fighting() })
		g.Step(10 * 20)
		t.Logf("lo dejó %v; peleando ahora %v", gaveUp, f.Fighting())
		if !gaveUp || f.Fighting() {
			t.Errorf("sigue con un zombi al que no llega")
		}
	})
}

// TestOffhandBackToTotem: tras una pelea con escudo, el tótem vuelve a la
// mano izquierda, con una sola F (en los logs: una F cada 2 s durante 13
// minutos, sin que el tótem llegara nunca).
func TestOffhandBackToTotem(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, f := simFighter(t, kit...)
		g.Step(5 * 20)
		if err := g.SummonNear("minecraft:skeleton", 6, 0); err != nil {
			t.Fatal(err)
		}
		id := g.LastMob()
		g.Until(60*20, func() bool { _, alive := g.MobHealth(id); return !alive })
		swaps := func() int {
			n := 0
			for _, p := range g.S.Got(packetid.ServerboundPlayPlayerAction) {
				var a play.PlayerAction
				if p.Scan(&a) == nil && a.Action == types.PlayerActionActionSwapItemWithOffhand {
					n++
				}
			}
			return n
		}
		before := swaps()
		g.Until(15*20, func() bool { return !f.Fighting() && g.Offhand() == "minecraft:totem_of_undying" })
		g.Step(10 * 20)
		t.Logf("mano izquierda %q; F tras la pelea: %d", g.Offhand(), swaps()-before)
		if g.Offhand() != "minecraft:totem_of_undying" {
			t.Errorf("tras la pelea, en la mano izquierda %q", g.Offhand())
		}
		if n := swaps() - before; n > 1 {
			t.Errorf("%d F para volver al tótem", n)
		}
	})
}

// TestOffhandSwapKeyIgnored: un servidor que no hace caso a la F (en los
// logs, por lo que fuera, el tótem no llegaba): al segundo intento, con un
// click (quieto), y nada de pulsar la F cada 2 s para siempre.
func TestOffhandSwapKeyIgnored(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, _ := simFighter(t, kit...)
		g.IgnoreSwapKey = true
		g.Step(20 * 20)
		swaps := 0
		for _, p := range g.S.Got(packetid.ServerboundPlayPlayerAction) {
			var a play.PlayerAction
			if p.Scan(&a) == nil && a.Action == types.PlayerActionActionSwapItemWithOffhand {
				swaps++
			}
		}
		t.Logf("mano izquierda %q; F pulsadas %d", g.Offhand(), swaps)
		if g.Offhand() != "minecraft:totem_of_undying" {
			t.Errorf("sin el tótem en la mano izquierda: %q", g.Offhand())
		}
		if swaps > 2 {
			t.Errorf("%d F en 20 s", swaps)
		}
	})
}

// TestDeadTargetEndsFight: un objetivo con 0 de vida (un jugador muerto sigue
// ahí hasta que reaparece) ya no es pelea. En los logs, tras matar a
// detsukio, 3 minutos quieto "peleando" con su cadáver.
func TestDeadTargetEndsFight(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, f := simFighter(t, kit...)
		g.Step(20)
		if err := g.SummonNear("minecraft:zombie", 14, 0); err != nil {
			t.Fatal(err)
		}
		id := g.LastMob()
		g.Step(5)
		f.Engage(id, "zombi")
		g.Step(10)
		if !f.Fighting() {
			t.Fatal("no empezó la pelea")
		}
		h := pk.Float(0)
		if err := g.S.Send(play.SetEntityData{ID: pk.VarInt(id), PackedItems: types.EntityData{
			{Index: entitydata.LivingEntityHealth, Serializer: 3, Value: &h}}}); err != nil {
			t.Fatal(err)
		}
		ended := g.Until(3*20, func() bool { return !f.Fighting() })
		if !ended {
			t.Errorf("sigue peleando con un muerto: %s", f.Status())
		}
	})
}

// TestKeepsElytra: con unos élitros puestos y un peto en el inventario, la
// pelea no se los cambia por el peto (en los logs: /elytra wear, y al momento
// "armadura: me pongo iron_chestplate").
func TestKeepsElytra(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, _ := simFighter(t, "give Tester elytra 1", "give Tester iron_chestplate 1")
		g.Step(20)
		if err := g.B.Screens.SelectHotbar(0); err != nil { // los élitros, en la mano: usarlos los pone
			t.Fatal(err)
		}
		g.Step(lag + 2)
		g.B.Hands.UseItemToward(0, 64, 10, nil)
		g.Step(5 * 20)
		if w := g.Worn(6); w != "minecraft:elytra" {
			t.Fatalf("en el pecho %q: se quitó los élitros", w)
		}
	})
}
