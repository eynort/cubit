package combat_test

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
)

var ironArmor = []string{"give Tester iron_helmet 1", "give Tester iron_chestplate 1", "give Tester iron_leggings 1", "give Tester iron_boots 1"}

// logged guarda el log del módulo (para ver lo que decidió).
type logged struct {
	mu    sync.Mutex
	lines []string
}

func (l *logged) add(s string) {
	l.mu.Lock()
	l.lines = append(l.lines, s)
	l.mu.Unlock()
}

func (l *logged) has(s string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, x := range l.lines {
		if strings.Contains(x, s) {
			return true
		}
	}
	return false
}

// TestMatchupOutclassed: el bot full hierro con espada de hierro; el rival
// full diamante protección IV con espada de diamante filo V: no se le gana
// (36 golpes contra 7). El bot lo ve venir y se encierra con bloques.
//
//   - sin pico (no rompe bloques): ni un golpe en un minuto.
//   - sin pico y el bot herido (vida 8): se cura dentro (manzanas, comida).
//   - con pico (rompe uno cada 2 s si no le ve), herido: repone lo que le
//     rompe y aguanta más que sin bloques (pero acaba entrando: no hay
//     búnker contra un pico).
func TestMatchupOutclassed(t *testing.T) {
	cases := []struct {
		name   string
		blocks bool
		breaks int
		health float32
	}{
		{"sin-pico", true, 0, 20},
		{"sin-pico-herido", true, 0, 8},
		{"con-pico-herido", true, 40, 8},
		{"sin-bloques-herido", false, 40, 8},
	}
	alive := map[string]float64{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				setup := append(append([]string{}, ironArmor...), "give Tester iron_sword 1", "give Tester cooked_beef 16", "give Tester golden_apple 4")
				if c.blocks {
					setup = append(setup, "give Tester cobblestone 64")
				}
				g, f := simFighter(t, setup...)
				var lg logged
				f.Log = func(format string, args ...any) {
					t.Logf(format, args...)
					lg.add(format)
				}
				g.Until(5*20, func() bool { return g.Worn(8) != "" })
				if err := g.SetHealth(c.health); err != nil {
					t.Fatal(err)
				}
				rival := simworld.Kit{Weapon: "minecraft:diamond_sword", Sharpness: 5, Armor: "diamond", Protection: 4, BreakTicks: c.breaks}
				if err := g.SummonDuelistKit(12, 0, rival); err != nil {
					t.Fatal(err)
				}
				ticks, best := 0, g.Health
				g.Until(60*20, func() bool {
					if !g.Dead() {
						ticks++
						best = max(best, g.Health)
					}
					return g.Dead()
				})
				alive[c.name] = float64(ticks) / 20
				t.Logf("%s: vivo %.1f s, vida máxima %.1f, golpes recibidos %d, bloques rotos por él %d, puestos %d",
					c.name, alive[c.name], best, g.MobHits(), g.Broken(), f.ShelterBlocks())
				if !lg.has("contra %s: %s") {
					t.Error("no evaluó al rival")
				}
				switch c.name {
				case "sin-pico":
					if !lg.has("me encierro con bloques") || g.Dead() || g.MobHits() > 0 {
						t.Errorf("sin pico: encerrado=%v muerto=%v golpes=%d", lg.has("me encierro con bloques"), g.Dead(), g.MobHits())
					}
				case "sin-pico-herido":
					if best < 18 || g.MobHits() > 0 {
						t.Errorf("no se curó dentro: vida máxima %.1f, golpes %d", best, g.MobHits())
					}
				case "con-pico-herido":
					if f.ShelterBlocks() <= 8 {
						t.Errorf("no repuso lo roto: %d bloques puestos", f.ShelterBlocks())
					}
				}
			})
		})
	}
	if a, b := alive["con-pico-herido"], alive["sin-bloques-herido"]; a <= b {
		t.Errorf("con bloques aguanta %.1f s, sin ellos %.1f", a, b)
	}
}

// TestMatchupStances: el bot full hierro con espada de hierro, contra rivales
// con distinto equipo: la postura sale de la cuenta del intercambio.
func TestMatchupStances(t *testing.T) {
	for _, c := range []struct {
		kit  simworld.Kit
		want string
	}{
		{simworld.Kit{Weapon: "minecraft:wooden_sword"}, "le gano"},
		{simworld.Kit{Armor: "iron"}, "igualado"},
		{simworld.Kit{Armor: "iron", Sharpness: 3}, "me supera"},
		{simworld.Kit{Weapon: "minecraft:diamond_sword", Sharpness: 5, Armor: "diamond", Protection: 4}, "no le gano"},
	} {
		t.Run(c.want, func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				g, f := simFighter(t, append(append([]string{}, ironArmor...), "give Tester iron_sword 1")...)
				var lg logged
				f.Log = func(format string, args ...any) { lg.add(fmt.Sprintf(format, args...)) }
				g.Until(5*20, func() bool { return g.Worn(5) != "" && g.Worn(6) != "" && g.Worn(7) != "" && g.Worn(8) != "" })
				if err := g.SummonDuelistKit(20, 0, c.kit); err != nil {
					t.Fatal(err)
				}
				f.Engage(g.LastMob(), "rival")
				g.Until(3*20, func() bool { return lg.has("le hacen falta") })
				lg.mu.Lock()
				defer lg.mu.Unlock()
				for _, l := range lg.lines {
					if strings.HasPrefix(l, "contra ") && strings.Contains(l, "le hacen falta") {
						t.Log(l)
						if !strings.HasSuffix(l, ": "+c.want) {
							t.Errorf("se esperaba %q", c.want)
						}
						return
					}
				}
				t.Error("no evaluó al rival")
			})
		})
	}
}

// TestBunkerSpeed: cuánto tarda en cerrarse del todo (las 8 paredes y el
// techo) desde que decide encerrarse, y que el techo se ponga (antes: 53
// ticks las paredes, esperando al servidor bloque a bloque, y el techo nunca).
func TestBunkerSpeed(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, f := simFighter(t, append(append([]string{}, ironArmor...), "give Tester iron_sword 1", "give Tester cobblestone 64")...)
		var lg logged
		f.Log = func(format string, args ...any) { t.Logf(format, args...); lg.add(format) }
		g.Until(5*20, func() bool { return g.Worn(5) != "" && g.Worn(6) != "" && g.Worn(7) != "" && g.Worn(8) != "" })
		rival := simworld.Kit{Weapon: "minecraft:diamond_sword", Sharpness: 5, Armor: "diamond", Protection: 4}
		if err := g.SummonDuelistKit(14, 0, rival); err != nil {
			t.Fatal(err)
		}
		if !g.Until(10*20, func() bool { return lg.has("me encierro con bloques") }) {
			t.Fatal("no se encerró")
		}
		start := g.Tick
		p := g.Pos()
		x, y, z := int(math.Floor(p[0])), int(math.Floor(p[1]+1e-6)), int(math.Floor(p[2]))
		solid := func(dx, dy, dz int) bool { return g.S.World.At(x+dx, y+dy, z+dz) != 0 }
		walls := func() int {
			n := 0
			for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				for dy := 0; dy <= 1; dy++ {
					if solid(d[0], dy, d[1]) {
						n++
					}
				}
			}
			return n
		}
		wallsAt, roofAt := -1, -1
		g.Until(15*20, func() bool {
			if wallsAt < 0 && walls() == 8 {
				wallsAt = g.Tick - start
			}
			if roofAt < 0 && solid(0, 2, 0) {
				roofAt = g.Tick - start
			}
			return wallsAt >= 0 && roofAt >= 0
		})
		t.Logf("paredes en %d ticks, techo en %d ticks; golpes recibidos %d\n%s", wallsAt, roofAt, g.MobHits(), g.Around(x, y+1, z))
		if wallsAt < 0 || wallsAt > 45 {
			t.Errorf("las 8 paredes: %d ticks (se quiere en 2,25 s o menos)", wallsAt)
		}
		if roofAt < 0 || roofAt > 35 {
			t.Errorf("el techo: %d ticks (se quiere en 1,75 s o menos)", roofAt)
		}
		if g.MobHits() > 0 {
			t.Errorf("le pegaron %d veces cerrándose", g.MobHits())
		}
	})
}

// TestBunkerExit: encerrado de uno al que no le gana; el rival se va: sale
// del búnker (rompe una pared) y puede volver a andar.
func TestBunkerExit(t *testing.T) {
	for _, pick := range []bool{true, false} {
		t.Run(map[bool]string{true: "con-pico", false: "a-mano"}[pick], func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				setup := append(append([]string{}, ironArmor...), "give Tester iron_sword 1", "give Tester cobblestone 64", "give Tester cooked_beef 8")
				if pick {
					setup = append(setup, "give Tester stone_pickaxe 1")
				}
				g, f := simFighter(t, setup...)
				var lg logged
				f.Log = func(format string, args ...any) { t.Logf(format, args...); lg.add(format) }
				g.Until(5*20, func() bool { return g.Worn(5) != "" && g.Worn(6) != "" && g.Worn(7) != "" && g.Worn(8) != "" })
				rival := simworld.Kit{Weapon: "minecraft:diamond_sword", Sharpness: 5, Armor: "diamond", Protection: 4}
				if err := g.SummonDuelistKit(14, 0, rival); err != nil {
					t.Fatal(err)
				}
				if !g.Until(10*20, func() bool { return lg.has("cerrado (8 paredes") }) {
					t.Fatal("no se cerró")
				}
				g.Step(5 * 20)
				if err := g.RemoveMobs(); err != nil {
					t.Fatal(err)
				}
				t0 := g.Tick
				out := g.Until(30*20, func() bool { return !f.Fighting() })
				t.Logf("fuera=%v en %.1f s; estado %s\n%s", out, float64(g.Tick-t0)/20, f.Status(), g.Around(int(math.Floor(g.Pos()[0])), int(g.Pos()[1])+1, int(math.Floor(g.Pos()[2]))))
				if !out {
					t.Fatal("no salió del búnker")
				}
			})
		})
	}
}

// TestBunkerHealAndFight: contra uno igualado (los dos full hierro), con
// poca vida (6). Sin escudo le supera: se encierra antes de que llegue, se
// cura (de 6 a 19), sale por detrás y pelea (y pierde: en el simulador un
// duelo igualado sin escudo lo gana el que pega primero, TestDuelBench 0 de
// 12). Con escudo la cuenta sale igualada: pelea sin búnker y gana.
func TestBunkerHealAndFight(t *testing.T) {
	for _, shield := range []bool{true, false} {
		t.Run(map[bool]string{true: "con-escudo", false: "sin-escudo"}[shield], func(t *testing.T) { bunkerHealAndFight(t, shield) })
	}
}

func bunkerHealAndFight(t *testing.T, shield bool) {
	simworld.Run(t, func(t *testing.T) {
		setup := append(append([]string{}, ironArmor...), "give Tester iron_sword 1", "give Tester cobblestone 64", "give Tester cooked_beef 16", "give Tester golden_apple 2", "give Tester stone_pickaxe 1")
		if shield {
			setup = append(setup, "give Tester shield 1")
		}
		g, f := simFighter(t, setup...)
		var lg logged
		f.Log = func(format string, args ...any) { t.Logf(format, args...); lg.add(format) }
		g.Until(5*20, func() bool { return g.Worn(5) != "" && g.Worn(6) != "" && g.Worn(7) != "" && g.Worn(8) != "" })
		if err := g.SetHealth(6); err != nil {
			t.Fatal(err)
		}
		if err := g.SummonDuelistKit(10, 0, simworld.Kit{Armor: "iron"}); err != nil {
			t.Fatal(err)
		}
		id := g.LastMob()
		g.Until(60*20, func() bool {
			_, alive := g.MobHealth(id)
			return g.Dead() || !alive
		})
		_, alive := g.MobHealth(id)
		t.Logf("muerto=%v, rival vivo=%v, vida %.1f, golpes recibidos %d", g.Dead(), alive, g.Health, g.MobHits())
		if shield { // (con escudo y 6 de vida la cuenta sale igualada: pelea, sin búnker)
			if g.Dead() || alive {
				t.Errorf("con escudo tenía que ganarle")
			}
			return
		}
		for _, want := range []string{"me encierro con bloques", "salgo a por %s", "abierto: fuera del búnker"} {
			if !lg.has(want) {
				t.Errorf("no pasó: %q", want)
			}
		}
	})
}

// TestBunkerUnbox: encerrado, una orden "stop" (olvida peleas y búnker) con
// el búnker cerrado: sin pelea y entre sus bloques, abre una salida solo.
func TestBunkerUnbox(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, f := simFighter(t, append(append([]string{}, ironArmor...), "give Tester iron_sword 1", "give Tester cobblestone 64", "give Tester stone_pickaxe 1")...)
		var lg logged
		f.Log = func(format string, args ...any) { t.Logf(format, args...); lg.add(format) }
		g.Until(5*20, func() bool { return g.Worn(5) != "" && g.Worn(6) != "" && g.Worn(7) != "" && g.Worn(8) != "" })
		rival := simworld.Kit{Weapon: "minecraft:diamond_sword", Sharpness: 5, Armor: "diamond", Protection: 4}
		if err := g.SummonDuelistKit(14, 0, rival); err != nil {
			t.Fatal(err)
		}
		if !g.Until(10*20, func() bool { return lg.has("cerrado (8 paredes") }) {
			t.Fatal("no se cerró")
		}
		if err := g.RemoveMobs(); err != nil { // (que no vuelva a encerrarse por él)
			t.Fatal(err)
		}
		f.Reset()
		if !g.Until(15*20, func() bool { return lg.has("abierto: fuera del búnker") }) {
			t.Fatalf("no salió: %s", f.Status())
		}
		if !lg.has("encerrado entre mis bloques") {
			t.Error("salió, pero no por el rescate")
		}
	})
}

// TestBunkerNeedsBlocks: con 4 bloques (un búnker son unos 10) no se
// encierra: a medias no para nada y es perder el tiempo con él encima.
func TestBunkerNeedsBlocks(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		setup := append(append([]string{}, ironArmor...), "give Tester iron_sword 1", "give Tester cooked_beef 16", "give Tester cobblestone 4")
		g, f := simFighter(t, setup...)
		var lg logged
		f.Log = func(format string, args ...any) { lg.add(format) }
		g.Until(5*20, func() bool { return g.Worn(8) != "" })
		if err := g.SetHealth(8); err != nil {
			t.Fatal(err)
		}
		rival := simworld.Kit{Weapon: "minecraft:diamond_sword", Sharpness: 5, Armor: "diamond", Protection: 4}
		if err := g.SummonDuelistKit(12, 0, rival); err != nil {
			t.Fatal(err)
		}
		g.Until(15*20, func() bool { return g.Dead() })
		if lg.has("me encierro con bloques (%s; tengo %d)") {
			t.Error("se encerró con 4 bloques")
		}
	})
}

// TestBunkerLeaveWithShield: contra uno al que no le gana, con escudo: se
// encierra para curarse y, curado, sale (con el escudo, a la defensiva, no
// le entra nada) en vez de esperar dentro hasta 3 minutos a que se vaya.
func TestBunkerLeaveWithShield(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		setup := append(append([]string{}, ironArmor...), "give Tester iron_sword 1", "give Tester shield 1", "give Tester golden_carrot 16", "give Tester cobblestone 64", "give Tester stone_pickaxe 1")
		g, f := simFighter(t, setup...)
		var lg logged
		f.Log = func(format string, args ...any) { t.Logf(format, args...); lg.add(format) }
		g.Until(5*20, func() bool { return g.Worn(8) != "" })
		if err := g.SetHealth(8); err != nil {
			t.Fatal(err)
		}
		rival := simworld.Kit{Weapon: "minecraft:diamond_sword", Sharpness: 5, Armor: "diamond", Protection: 4}
		if err := g.SummonDuelistKit(14, 0, rival); err != nil {
			t.Fatal(err)
		}
		out := g.Until(60*20, func() bool { return lg.has("abierto: fuera del búnker") || g.Dead() })
		t.Logf("fuera %v, muerto %v, vida %.1f", out, g.Dead(), g.Health)
		if !lg.has("me encierro con bloques (%s; tengo %d)") {
			t.Fatal("no se encerró")
		}
		if !out || g.Dead() {
			t.Error("no salió del búnker curado")
		}
	})
}
