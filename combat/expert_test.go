package combat_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
)

// expertFight es arsenalFight con equipo encantado antes de empezar.
func expertFight(t *testing.T, setup []string, ench map[string]map[string]int, dx, dz float64, k simworld.Kit) (*simworld.Game, *logged, int32) {
	g, f := simFighter(t, setup...)
	lg := &logged{}
	f.Log = func(format string, args ...any) { t.Logf(format, args...); lg.add(fmt.Sprintf(format, args...)) }
	for item, e := range ench {
		g.GiveEnchanted(item, e)
	}
	g.Step(3 * 20)
	if err := g.SummonDuelistKit(dx, dz, k); err != nil {
		t.Fatal(err)
	}
	id := g.LastMob()
	g.Step(lag + 2)
	f.Engage(id, "rival")
	return g, lg, id
}

var blastArmor = map[string]map[string]int{
	"minecraft:diamond_helmet": {"blast_protection": 4}, "minecraft:diamond_chestplate": {"blast_protection": 4},
	"minecraft:diamond_leggings": {"blast_protection": 4}, "minecraft:diamond_boots": {"blast_protection": 4},
}

// TestRetotem: con dos tótems y a punto de morir: se gasta uno y el otro
// pasa a la mano izquierda al momento.
func TestRetotem(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, lg, _ := expertFight(t, []string{"give Tester iron_sword 1", "give Tester totem_of_undying 2"}, nil, 3, 0,
			simworld.Kit{Weapon: "minecraft:diamond_sword", Sharpness: 5})
		if err := g.SetHealth(2); err != nil {
			t.Fatal(err)
		}
		popped := g.Until(20*20, func() bool { return g.TotemsUsed() > 0 })
		if !popped {
			t.Fatal("no se gastó ningún tótem")
		}
		back := g.Until(30, func() bool { return g.Offhand() == "minecraft:totem_of_undying" && g.TotemsUsed() > 0 })
		t.Logf("tótems gastados %d, en la mano izquierda %q", g.TotemsUsed(), g.Offhand())
		if !back || !lg.has("otro a la mano izquierda") {
			t.Error("no puso el segundo tótem al momento")
		}
	})
}

// TestCrystal: con protección contra explosiones, cristales y obsidiana
// contra un rival sin armadura a 3: obsidiana a su lado, cristal y lo rompe.
func TestCrystal(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, lg, id := expertFight(t, []string{"give Tester iron_sword 1", "give Tester end_crystal 4", "give Tester obsidian 8"}, blastArmor, 3.5, 0, simworld.Kit{Eating: true})
		blown := g.Until(15*20, func() bool { return g.Arsenal().CrystalBlasts > 0 })
		g.Step(10)
		t.Logf("explosiones %d, vida del rival %.1f, la del bot %.1f", g.Arsenal().CrystalBlasts, mobHP(g, id), g.Health)
		if !blown || !lg.has("cristal junto a") {
			t.Fatal("no usó el cristal")
		}
		if mobHP(g, id) > 5 {
			t.Error("el cristal apenas le hizo daño")
		}
		if g.Health < 8 {
			t.Errorf("el bot se hizo demasiado daño (vida %.1f)", g.Health)
		}
	})
}

// TestCrystalSafe: sin armadura, solo usa un cristal donde algo le tapa
// de la explosión: nunca se quita más de 6.
func TestCrystalSafe(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, _, _ := expertFight(t, []string{"give Tester iron_sword 1", "give Tester end_crystal 4", "give Tester obsidian 8"}, nil, 3.5, 0, simworld.Kit{Eating: true})
		low := g.Health
		g.Until(10*20, func() bool { low = min(low, g.Health); return false })
		t.Logf("explosiones %d, vida mínima %.1f", g.Arsenal().CrystalBlasts, low)
		if low < 14 {
			t.Errorf("sus cristales le quitaron demasiado (vida mínima %.1f)", low)
		}
	})
}

// TestAnchor: ancla y piedra luminosa, con protección contra explosiones:
// la pone junto al rival, la carga y la hace explotar.
func TestAnchor(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, lg, id := expertFight(t, []string{"give Tester iron_sword 1", "give Tester respawn_anchor 2", "give Tester glowstone 4"}, blastArmor, 3.5, 0, simworld.Kit{Eating: true})
		blown := g.Until(15*20, func() bool { return g.Arsenal().AnchorBlasts > 0 })
		g.Step(10)
		t.Logf("anclas %d, vida del rival %.1f, la del bot %.1f", g.Arsenal().AnchorBlasts, mobHP(g, id), g.Health)
		if !blown || !lg.has("¡ancla!") {
			t.Fatal("no hizo explotar el ancla")
		}
		if g.Health < 8 {
			t.Errorf("el bot se hizo demasiado daño (vida %.1f)", g.Health)
		}
	})
}

// moat: un foso ancho entre el bot y el rival (no hay camino).
func moat(x1, x2 int) string { return fmt.Sprintf("fill %d 40 -30 %d 63 30 air", x1, x2) }

// TestRodPull: el rival al otro lado de un foso: no se llega; la caña le
// engancha y le trae (cae al foso).
func TestRodPull(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, lg, id := expertFight(t, []string{"give Tester iron_sword 1", "give Tester fishing_rod 1", moat(3, 6)}, nil, 7.5, 0, simworld.Kit{Still: true})
		pulled := g.Until(20*20, func() bool { return g.Arsenal().Pulls > 0 })
		g.Step(40)
		x, y, _, _ := g.MobAt(id)
		t.Logf("tirones %d; el rival en x=%.1f y=%.1f", g.Arsenal().Pulls, x, y)
		if !pulled || !lg.has("caña a") {
			t.Error("no le enganchó con la caña")
		}
	})
}

// TestPearlChase: el rival al otro lado de un foso de 12: no se llega; con
// una perla, a su lado.
func TestPearlChase(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, lg, id := expertFight(t, []string{"give Tester iron_sword 1", "give Tester ender_pearl 4", moat(3, 14)}, nil, 16.5, 0, simworld.Kit{Still: true})
		landed := g.Until(25*20, func() bool { return g.Arsenal().PearlLanded > 0 })
		x, _, z, _ := g.MobAt(id)
		p := g.Pos()
		t.Logf("perlas %d, aterrizó %d; a %.1f bloques de él", g.Arsenal().Pearls, g.Arsenal().PearlLanded, math.Hypot(x-p[0], z-p[2]))
		if !landed || !lg.has("perla para alcanzar") {
			t.Fatal("no usó la perla")
		}
		if math.Hypot(x-p[0], z-p[2]) > 6 {
			t.Error("la perla no le dejó cerca de él")
		}
	})
}

// TestElytraMace: élitros, cohetes y maza, el rival a 20 en campo abierto:
// sube, cae en picado, se quita los élitros encima de él y le da con la maza.
func TestElytraMace(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, lg, id := expertFight(t, []string{"give Tester iron_sword 1", "give Tester mace 1", "give Tester elytra 1",
			"give Tester firework_rocket 16", "give Tester water_bucket 1", "give Tester iron_chestplate 1"}, nil, 20, 0, simworld.Kit{Eating: true})
		smashed := g.Until(40*20, func() bool { return g.Arsenal().Smashes > 0 })
		a := g.Arsenal()
		t.Logf("golpes de maza %d (el mayor +%.0f), vida del rival %.1f, la del bot %.1f, planeando=%v", a.Smashes, a.SmashMax, mobHP(g, id), g.Health, g.Gliding())
		if !lg.has("picado con élitros") {
			t.Fatal("no intentó el picado")
		}
		if !smashed || a.SmashMax < 20 {
			t.Error("no cayó encima con la maza desde lo alto")
		}
	})
}

// TestSmite: contra un zombi, la espada de hierro con golpe V (6 + 12,5: de
// un crítico) y no la de piedra (5: tres).
func TestSmite(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, f := simFighter(t, "give Tester stone_sword 1")
		lg := &logged{}
		f.Log = func(format string, args ...any) { t.Logf(format, args...); lg.add(fmt.Sprintf(format, args...)) }
		g.GiveEnchanted("minecraft:iron_sword", map[string]int{"smite": 5})
		g.Step(3 * 20)
		if err := g.SummonNear("minecraft:zombie", 5, 0); err != nil {
			t.Fatal(err)
		}
		id := g.LastMob()
		g.Until(15*20, func() bool { return mobHP(g, id) <= 0 })
		if !lg.has("arma: iron_sword") {
			t.Error("no eligió la espada con golpe contra el zombi")
		}
	})
}
