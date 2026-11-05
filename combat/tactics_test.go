package combat_test

import (
	"fmt"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26-kit/bot/simworld"
)

// tacticsFight: el bot (espada de hierro, lo que se le dé) contra un rival
// quieto (Still) sobre lo que haya en setup; el bot va a por él.
func tacticsFight(t *testing.T, setup []string, dx, dz float64) (*simworld.Game, *logged, int32) {
	return tacticsFightKit(t, setup, dx, dz, simworld.Kit{Still: true})
}

func tacticsFightKit(t *testing.T, setup []string, dx, dz float64, kit simworld.Kit) (*simworld.Game, *logged, int32) {
	g, f := simFighter(t, append([]string{"give Tester iron_sword 1"}, setup...)...)
	lg := &logged{}
	f.Log = func(format string, args ...any) { t.Logf(format, args...); lg.add(fmt.Sprintf(format, args...)) }
	g.Step(3 * 20)
	if err := g.SummonDuelistKit(dx, dz, kit); err != nil {
		t.Fatal(err)
	}
	id := g.LastMob()
	g.Step(lag + 2)
	f.Engage(id, "rival")
	return g, lg, id
}

// TestTacticsBowHigh: el rival en lo alto de una torre de 1×1 (5 bloques), a
// 8: no se le llega a romper el suelo; con el arco, el empuje de la flecha le tira.
func TestTacticsBowHigh(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, lg, id := tacticsFight(t, []string{"fill 8 64 0 8 68 0 stone", "give Tester bow 1", "give Tester arrow 32"}, 8, 0)
		_, y0, _, _ := g.MobAt(id)
		fell := g.Until(30*20, func() bool { _, y, _, _ := g.MobAt(id); return y < y0-2 })
		_, y, _, _ := g.MobAt(id)
		t.Logf("de %.1f a %.1f; flechas %d, aciertos %d", y0, y, g.Shots(), g.ArrowHits())
		if !fell || !lg.has("arco contra") {
			t.Errorf("no le tiró de la torre (arco=%v)", lg.has("arco contra"))
		}
	})
}

// TestTacticsKnockDown: el rival en una torre de 1×1 de 3 junto al bot: le
// rompe el bloque que pisa (con pico) hasta tenerle abajo.
func TestTacticsKnockDown(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, lg, id := tacticsFight(t, []string{"fill 3 64 0 3 66 0 stone", "give Tester stone_pickaxe 1"}, 3, 0)
		_, y0, _, _ := g.MobAt(id)
		down := g.Until(20*20, func() bool { _, y, _, _ := g.MobAt(id); return y < 65.5 })
		_, y, _, _ := g.MobAt(id)
		t.Logf("de %.1f a %.1f", y0, y)
		if !down || !lg.has("le rompo el bloque que pisa") {
			t.Error("no le bajó de la torre")
		}
	})
}

// TestTacticsFluids: el rival pone agua (o lava) a su lado: el bot la
// recoge con un cubo vacío; sin cubo, la tapa con un bloque.
func TestTacticsFluids(t *testing.T) {
	for _, c := range []struct{ fluid, gear, want string }{
		{"water", "bucket", "water_bucket"},
		{"lava", "bucket", "lava_bucket"},
		{"water", "cobblestone", ""},
	} {
		t.Run(c.fluid+"-"+c.gear, func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				g, lg, _ := tacticsFight(t, []string{"give Tester " + c.gear + " 16"}, 4, 0)
				g.Step(10)
				if err := g.S.SetBlock(3, 64, 1, "minecraft:"+c.fluid); err != nil {
					t.Fatal(err)
				}
				gone := g.Until(10*20, func() bool { return g.S.World.At(3, 64, 1) != fakeserver.State("minecraft:"+c.fluid) })
				t.Logf("quitada=%v; en su sitio: %s", gone, g.Around(3, 63, 1))
				if !gone {
					t.Fatal("no la quitó")
				}
				if c.want != "" && g.Count("minecraft:"+c.want) == 0 {
					t.Errorf("no la recogió con el cubo (no tengo %s)", c.want)
				}
				if !lg.has("que puso") {
					t.Error("no lo dijo en el log")
				}
			})
		})
	}
}

// TestTacticsCage: el rival comiendo (quieto, una dorada en la mano) a 4: le
// pone bloques detrás y a los lados (pies y cabeza), dejando abierto el lado
// del bot.
func TestTacticsCage(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, lg, _ := tacticsFightKit(t, []string{"give Tester cobblestone 32"}, 4, 0, simworld.Kit{Eating: true})
		around := func() int {
			n := 0
			for _, d := range [][2]int{{1, 0}, {0, 1}, {0, -1}} { // (detrás, x+; y los lados)
				for dy := 0; dy <= 1; dy++ {
					if g.S.World.At(4+d[0], 64+dy, 0+d[1]) != 0 {
						n++
					}
				}
			}
			return n
		}
		caged := g.Until(10*20, func() bool { return around() == 6 })
		t.Logf("bloques alrededor: %d; delante (el lado del bot): %d %d", around(), g.S.World.At(3, 64, 0), g.S.World.At(3, 65, 0))
		if !caged || !lg.has("le encierro con bloques") {
			t.Error("no le encerró")
		}
		if g.S.World.At(3, 64, 0) != 0 || g.S.World.At(3, 65, 0) != 0 {
			t.Error("tapó también el lado del bot")
		}
	})
}

// TestTacticsCageKill (medida, no exige nada): lo que tarda en matar a uno
// que come quieto (full diamante), con bloques (le encierra) y sin ellos.
// Medido: 16.9 s contra 15.5. Encerrarle cuesta (6 bloques, y la espada otra
// vez en la mano: el golpe sin cargar) y el rival del simulador no huye ni se
// mueve aunque le empujen: la ventaja de la jaula (que no se vaya, que el
// empujón no le aleje) es contra uno que sí lo hace, y eso no está modelado.
func TestTacticsCageKill(t *testing.T) {
	took := map[bool]float64{}
	for _, blocks := range []bool{true, false} {
		t.Run(map[bool]string{true: "con-bloques", false: "sin-bloques"}[blocks], func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				var setup []string
				if blocks {
					setup = []string{"give Tester cobblestone 32"}
				}
				g, _, id := tacticsFightKit(t, setup, 4, 0, simworld.Kit{Eating: true, Armor: "diamond"})
				t0 := g.Tick
				dead := g.Until(60*20, func() bool { _, alive := g.MobHealth(id); return !alive })
				took[blocks] = float64(g.Tick-t0) / 20
				t.Logf("muerto=%v en %.1f s", dead, took[blocks])
			})
		})
	}
	t.Logf("con bloques %.1f s, sin ellos %.1f s", took[true], took[false])
}

// TestTacticsBowEating: uno comiendo a 6 bloques (más cerca de lo que el arco
// se usa contra uno que viene): flechas, que no se cure tranquilo.
func TestTacticsBowEating(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, lg, _ := tacticsFightKit(t, []string{"give Tester bow 1", "give Tester arrow 16"}, 6, 0, simworld.Kit{Eating: true})
		g.Until(10*20, func() bool { return g.ArrowHits() > 0 })
		t.Logf("flechas %d, aciertos %d", g.Shots(), g.ArrowHits())
		if !lg.has("arco contra") || g.ArrowHits() == 0 {
			t.Error("no le disparó mientras comía")
		}
	})
}

// TestTacticsBreakIn: el rival encerrado en su búnker (piedra a los cuatro
// lados, pies y cabeza, y techo): el bot le rompe la pared de su lado (con
// pico) y le mata. Comiendo dentro (no pega), y armado (se la rompe desde
// fuera de su alcance: sale, y en campo abierto con escudo le gana).
func TestTacticsBreakIn(t *testing.T) {
	for _, c := range []struct {
		name string
		kit  simworld.Kit
		gear []string
	}{
		{"comiendo", simworld.Kit{Eating: true}, []string{"give Tester stone_pickaxe 1"}},
		{"armado", simworld.Kit{}, []string{"give Tester stone_pickaxe 1", "give Tester shield 1", "give Tester totem_of_undying 1", "give Tester cooked_beef 8"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				g, f := simFighter(t, append([]string{"give Tester iron_sword 1"}, c.gear...)...)
				lg := &logged{}
				f.Log = func(format string, args ...any) { t.Logf(format, args...); lg.add(fmt.Sprintf(format, args...)) }
				g.Step(3 * 20)
				g.Teleport(0.5, 64, 0.5)
				g.Step(5)
				if err := g.SummonDuelistKit(5, 0, c.kit); err != nil { // (en 5.5: la celda 5)
					t.Fatal(err)
				}
				id := g.LastMob()
				// su búnker: la celda 5,64,0 cerrada (pies y cabeza a los cuatro lados) y con techo
				for _, b := range [][3]int{{4, 64, 0}, {4, 65, 0}, {6, 64, 0}, {6, 65, 0}, {5, 64, 1}, {5, 65, 1}, {5, 64, -1}, {5, 65, -1}, {5, 66, 0}} {
					if err := g.S.SetBlock(b[0], b[1], b[2], "minecraft:stone"); err != nil {
						t.Fatal(err)
					}
				}
				g.Step(lag + 2)
				f.Engage(id, "rival")
				dead := g.Until(60*20, func() bool { _, alive := g.MobHealth(id); return !alive || g.Dead() })
				_, alive := g.MobHealth(id)
				t.Logf("rival muerto=%v (bot muerto=%v, vida %.1f) en %.1f s; pared: %d %d; golpes al bot %d, al escudo %d", !alive, g.Dead(), g.Health, float64(g.Tick)/20, g.S.World.At(4, 64, 0), g.S.World.At(4, 65, 0), g.MobHits(), g.Blocks())
				if !lg.has("se ha encerrado: le rompo la pared") {
					t.Error("no vio que estaba encerrado")
				}
				if !dead || alive || g.Dead() {
					t.Error("no le mató")
				}
			})
		})
	}
}

// TestTacticsBowThenClimb: el rival arriba de una torre de 5×5 (5 de alto),
// en el centro: las flechas no le tiran (el empuje no le saca de ella). Tras
// unas flechas (o sin línea de tiro ya), con bloques, sube a por él (torre
// del pathfinding) en vez de quedarse abajo tirando o dejarlo.
func TestTacticsBowThenClimb(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, lg, _ := tacticsFight(t, []string{"fill 10 64 -2 14 68 2 stone", "give Tester bow 1", "give Tester arrow 32", "give Tester cobblestone 32"}, 12, 0)
		top := 0.0
		g.Until(40*20, func() bool { top = max(top, g.Pos()[1]); return top >= 68.9 })
		t.Logf("flechas %d, aciertos %d; lo más alto: %.1f", g.Shots(), g.ArrowHits(), top)
		if !lg.has("arco contra") {
			t.Error("no usó el arco primero")
		}
		if top < 68.9 {
			t.Errorf("no subió a por él (lo más alto %.1f)", top)
		}
	})
}

// TestTacticsFluidNoPingPong: el agua que pone el rival se recoge una vez y
// el cubo se queda lleno: ni la vuelve a echar ni queda agua esparcida (en
// los logs, con 200 ms de ida y vuelta: recoger, echar, recoger… 6 s).
func TestTacticsFluidNoPingPong(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, _, _ := tacticsFight(t, append([]string{"give Tester bucket 1"}, kit...), 4, 0)
		g.Step(10)
		if err := g.S.SetBlock(3, 64, 1, "minecraft:water"); err != nil {
			t.Fatal(err)
		}
		// recogida, y 3 s después sigue en el cubo (no la vuelve a echar)
		g.Until(5*20, func() bool { return g.Count("minecraft:water_bucket") == 1 })
		g.Step(3 * 20)
		water := 0
		for x := -6; x <= 10; x++ {
			for z := -6; z <= 6; z++ {
				for y := 63; y <= 66; y++ {
					if g.S.World.At(x, y, z) == fakeserver.State("minecraft:water") {
						water++
					}
				}
			}
		}
		t.Logf("cubos llenos %d, vacíos %d, fuentes de agua alrededor %d; muertes %d, mano izquierda %q, en el suelo %s", g.Count("minecraft:water_bucket"), g.Count("minecraft:bucket"), water, g.Deaths(), g.Offhand(), g.ItemsAt())
		if g.Count("minecraft:water_bucket") != 1 || water != 0 {
			t.Errorf("el agua no acabó en el cubo")
		}
	})
}
