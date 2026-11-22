package automation_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"cubit/automation"
)

func leafOf(kind string) string {
	return fmt.Sprintf("minecraft:%s_leaves[distance=1,persistent=false,waterlogged=false]", kind)
}

// treeOf pone un árbol de la madera kind: un tronco de h en (x, z) con copa de hojas naturales.
func treeOf(kind string, x, z, h int) []string {
	top := 63 + h
	return []string{
		fmt.Sprintf("fill %d %d %d %d %d %d %s", x-2, top-1, z-2, x+2, top, z+2, leafOf(kind)),
		fmt.Sprintf("fill %d %d %d %d %d %d %s", x-1, top+1, z-1, x+1, top+1, z+1, leafOf(kind)),
		fmt.Sprintf("fill %d 64 %d %d %d %d minecraft:%s_log[axis=y]", x, z, x, top, z, kind),
	}
}

// bigTree: un árbol de tronco 2×2 (x..x+1, z..z+1).
func bigTree(kind string, x, z, h int) []string {
	top := 63 + h
	return []string{
		fmt.Sprintf("fill %d %d %d %d %d %d %s", x-2, top-1, z-2, x+3, top, z+3, leafOf(kind)),
		fmt.Sprintf("fill %d %d %d %d %d %d %s", x-1, top+1, z-1, x+2, top+1, z+2, leafOf(kind)),
		fmt.Sprintf("fill %d 64 %d %d %d %d minecraft:%s_log[axis=y]", x, z, x+1, top, z+1, kind),
	}
}

// logsLeft cuenta los troncos (de cualquier madera) que quedan en la columna de (x..x2, z..z2).
func logsLeft(g *simworld.Game, x, z, x2, z2 int) int {
	n := 0
	for xx := x; xx <= x2; xx++ {
		for zz := z; zz <= z2; zz++ {
			for y := 64; y <= 90; y++ {
				if st, ok := g.B.World.BlockAt(world.BlockPos{X: xx, Y: y, Z: zz}); ok && strings.Contains(world.StateString(st), "_log") {
					n++
				}
			}
		}
	}
	return n
}

func runChop(t *testing.T, g *simworld.Game, kinds []string, center world.BlockPos) error {
	r := runner(t, g)
	g.Step(5)
	var err error
	r.Start(context.Background(), "talar", func(j *automation.Job) error {
		err = automation.Chop(kinds, center)(j)
		return err
	})
	g.Until(300*20, func() bool { return r.Status() == "" })
	return err
}

// TestChopOak: un roble alto: tala todos los troncos (subiendo con bloques),
// los recoge, no rompe el hacha y no se queda arriba.
func TestChopOak(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: append(treeOf("oak", 6, 0, 8), "give Tester dirt 16", "give Tester iron_axe 1")})
		err := runChop(t, g, []string{"oak"}, world.BlockPos{X: 0, Y: 64, Z: 0})
		p := g.B.Player.Position()
		t.Logf("error %v; el bot en %.1f %.1f %.1f; troncos %d", err, p.X, p.Y, p.Z, g.Count("minecraft:oak_log"))
		if err != nil {
			t.Fatal(err)
		}
		if n := logsLeft(g, 6, 0, 6, 0); n > 0 {
			t.Errorf("quedan %d troncos en pie", n)
		}
		if n := g.Count("minecraft:oak_log"); n != 8 {
			t.Errorf("recogió %d troncos, eran 8", n)
		}
		if g.Count("minecraft:iron_axe") != 1 {
			t.Errorf("rompió el hacha")
		}
		if g.Items() > 0 {
			t.Errorf("dejó objetos tirados: %s", g.ItemsAt())
		}
		if p.Y > 65.5 {
			t.Errorf("se quedó arriba (y=%.1f)", p.Y)
		}
	})
}

// TestChopBigTree: un dark_oak de tronco 2×2: los 4 troncos de cada piso.
func TestChopBigTree(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: append(bigTree("dark_oak", 6, 0, 7), "give Tester dirt 16", "give Tester iron_axe 1")})
		err := runChop(t, g, []string{"dark_oak"}, world.BlockPos{X: 0, Y: 64, Z: 0})
		p := g.B.Player.Position()
		t.Logf("error %v; el bot en %.1f %.1f %.1f; troncos %d", err, p.X, p.Y, p.Z, g.Count("minecraft:dark_oak_log"))
		if err != nil {
			t.Fatal(err)
		}
		if n := logsLeft(g, 6, 0, 7, 1); n > 0 {
			t.Errorf("quedan %d troncos en pie", n)
		}
		if n := g.Count("minecraft:dark_oak_log"); n != 28 {
			t.Errorf("recogió %d troncos, eran 28", n)
		}
	})
}

// TestChopFilters: un roble, un abedul y un pilar de roble de jugador (sin
// hojas, con tablones al lado). Con "oak" solo cae el roble; el abedul y el
// pilar se quedan; después "birch" tala el abedul.
func TestChopFilters(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		setup := append(treeOf("oak", 6, 0, 5), treeOf("birch", -6, 4, 5)...)
		setup = append(setup,
			"fill 0 64 9 0 67 9 minecraft:oak_log[axis=y]", "fill 1 64 9 1 66 9 minecraft:oak_planks",
			"give Tester dirt 16", "give Tester iron_axe 1")
		g := simworld.Start(t, simworld.Options{Setup: setup})
		err := runChop(t, g, []string{"oak"}, world.BlockPos{X: 0, Y: 64, Z: 0})
		t.Logf("error %v; roble %d, abedul %d", err, g.Count("minecraft:oak_log"), g.Count("minecraft:birch_log"))
		if err != nil {
			t.Fatal(err)
		}
		if n := logsLeft(g, 6, 0, 6, 0); n != 0 {
			t.Errorf("el roble sigue en pie (%d)", n)
		}
		if n := logsLeft(g, -6, 4, -6, 4); n != 5 {
			t.Errorf("el abedul no era de los pedidos: quedan %d de 5", n)
		}
		if n := logsLeft(g, 0, 9, 0, 9); n != 4 {
			t.Errorf("el pilar de jugador se tocó: quedan %d de 4", n)
		}
		if g.Count("minecraft:birch_log") != 0 || g.Count("minecraft:oak_log") != 5 {
			t.Errorf("recogió roble %d (eran 5) y abedul %d (0)", g.Count("minecraft:oak_log"), g.Count("minecraft:birch_log"))
		}
	})
}

// TestChopSeveral: dos maderas a la vez (roble y abedul) y una tercera (abeto) que no se pide.
func TestChopSeveral(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		setup := append(treeOf("oak", 6, 0, 5), treeOf("birch", -6, 4, 4)...)
		setup = append(setup, treeOf("spruce", 4, -8, 5)...)
		setup = append(setup, "give Tester dirt 16", "give Tester iron_axe 1")
		g := simworld.Start(t, simworld.Options{Setup: setup})
		err := runChop(t, g, []string{"oak", "birch"}, world.BlockPos{X: 0, Y: 64, Z: 0})
		t.Logf("error %v; roble %d, abedul %d", err, g.Count("minecraft:oak_log"), g.Count("minecraft:birch_log"))
		if err != nil {
			t.Fatal(err)
		}
		if g.Count("minecraft:oak_log") != 5 || g.Count("minecraft:birch_log") != 4 {
			t.Errorf("recogió roble %d (5) y abedul %d (4)", g.Count("minecraft:oak_log"), g.Count("minecraft:birch_log"))
		}
		if n := logsLeft(g, 4, -8, 4, -8); n != 5 {
			t.Errorf("el abeto no se pedía: quedan %d de 5", n)
		}
	})
}

// TestChopNether: un hongo del Nether (tallo de warped y verruga en la copa) y
// otro de crimson que no se pide: tala solo el warped.
func TestChopNether(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		stem := func(kind, wart string, x, z, h int) []string {
			top := 63 + h
			return []string{
				fmt.Sprintf("fill %d %d %d %d %d %d minecraft:%s", x-2, top-1, z-2, x+2, top, z+2, wart),
				fmt.Sprintf("fill %d 64 %d %d %d %d minecraft:%s_stem[axis=y]", x, z, x, top, z, kind),
			}
		}
		setup := append(stem("warped", "warped_wart_block", 6, 0, 7), stem("crimson", "nether_wart_block", -6, 3, 6)...)
		setup = append(setup, "give Tester dirt 16", "give Tester iron_axe 1")
		g := simworld.Start(t, simworld.Options{Setup: setup})
		err := runChop(t, g, []string{"warped"}, world.BlockPos{X: 0, Y: 64, Z: 0})
		t.Logf("error %v; warped_stem %d", err, g.Count("minecraft:warped_stem"))
		if err != nil {
			t.Fatal(err)
		}
		if g.Count("minecraft:warped_stem") != 7 {
			t.Errorf("recogió %d tallos de warped, eran 7", g.Count("minecraft:warped_stem"))
		}
		if st, ok := g.B.World.BlockAt(world.BlockPos{X: -6, Y: 64, Z: 3}); !ok || !strings.Contains(world.StateString(st), "crimson_stem") {
			t.Errorf("el tallo de crimson no se pedía y ya no está")
		}
	})
}

// TestChopBush: un arbusto de la selva (un tronco rodeado de hojas por todas
// partes, también por encima): el tronco que suelta queda encerrado y se abre
// paso entre las hojas para recogerlo.
func TestChopBush(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		setup := []string{
			fmt.Sprintf("fill 5 64 -1 7 65 1 %s", leafOf("jungle")),
			"fill 6 64 0 6 64 0 minecraft:jungle_log[axis=y]",
			"give Tester dirt 16", "give Tester iron_axe 1",
		}
		g := simworld.Start(t, simworld.Options{Setup: setup})
		err := runChop(t, g, []string{"jungle"}, world.BlockPos{X: 0, Y: 64, Z: 0})
		t.Logf("error %v; troncos %d", err, g.Count("minecraft:jungle_log"))
		if err != nil {
			t.Fatal(err)
		}
		if g.Count("minecraft:jungle_log") != 1 {
			t.Errorf("recogió %d troncos, era 1", g.Count("minecraft:jungle_log"))
		}
		if g.Items() > 0 {
			t.Errorf("dejó objetos tirados: %s", g.ItemsAt())
		}
	})
}

// TestChopFloating: lo que queda de un árbol a medias: el tronco empieza a 5
// bloques del suelo. Se llega por debajo con una torre y se tala entero.
func TestChopFloating(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		setup := []string{
			fmt.Sprintf("fill 4 69 -2 8 70 2 %s", leafOf("oak")),
			fmt.Sprintf("fill 5 71 -1 7 71 1 %s", leafOf("oak")),
			"fill 6 69 0 6 71 0 minecraft:oak_log[axis=y]",
			"give Tester dirt 16", "give Tester iron_axe 1",
		}
		g := simworld.Start(t, simworld.Options{Setup: setup})
		err := runChop(t, g, []string{"oak"}, world.BlockPos{X: 0, Y: 64, Z: 0})
		p := g.B.Player.Position()
		t.Logf("error %v; troncos %d; bot en y=%.1f", err, g.Count("minecraft:oak_log"), p.Y)
		if err != nil {
			t.Fatal(err)
		}
		if n := logsLeft(g, 6, 0, 6, 0); n > 0 {
			t.Errorf("quedan %d troncos en pie", n)
		}
		if g.Count("minecraft:oak_log") != 3 {
			t.Errorf("recogió %d troncos, eran 3", g.Count("minecraft:oak_log"))
		}
		if p.Y > 65.5 {
			t.Errorf("se quedó arriba (y=%.1f)", p.Y)
		}
	})
}

// TestChopOnPillar: un árbol cuyo tronco nace de un pilar de piedra de 3 de
// alto (como los de raíces del manglar): no se pisa su columna, y se tala
// igual, subiendo en la de al lado, y el bot acaba en el suelo.
func TestChopOnPillar(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		setup := []string{
			fmt.Sprintf("fill 4 68 -2 8 69 2 %s", leafOf("oak")),
			fmt.Sprintf("fill 5 70 -1 7 70 1 %s", leafOf("oak")),
			"fill 6 64 0 6 66 0 minecraft:stone",
			"fill 6 67 0 6 69 0 minecraft:oak_log[axis=y]",
			"give Tester dirt 16", "give Tester iron_axe 1",
		}
		g := simworld.Start(t, simworld.Options{Setup: setup})
		err := runChop(t, g, []string{"oak"}, world.BlockPos{X: 0, Y: 64, Z: 0})
		p := g.B.Player.Position()
		t.Logf("error %v; troncos %d; bot en y=%.1f", err, g.Count("minecraft:oak_log"), p.Y)
		if err != nil {
			t.Fatal(err)
		}
		if n := logsLeft(g, 6, 0, 6, 0); n > 0 {
			t.Errorf("quedan %d troncos en pie", n)
		}
		if g.Count("minecraft:oak_log") != 3 {
			t.Errorf("recogió %d troncos, eran 3", g.Count("minecraft:oak_log"))
		}
		if p.Y > 65.5 {
			t.Errorf("se quedó arriba (y=%.1f)", p.Y)
		}
	})
}

func TestParseWoods(t *testing.T) {
	got, err := automation.ParseWoods("oak, birch,oak")
	if err != nil || len(got) != 2 || got[0] != "oak" || got[1] != "birch" {
		t.Fatalf("ParseWoods = %v, %v", got, err)
	}
	if _, err := automation.ParseWoods("oak,pine"); err == nil {
		t.Fatal("pine no es una madera")
	}
	if _, err := automation.ParseWoods("crimson,warped"); err != nil {
		t.Fatal(err)
	}
}

// TestChopNoBlocks: sin bloques de construir, junta la tierra justa para la
// torre: la recoge (aunque en /chop solo se recoja madera) y no se pone a
// cavar hoyos sin parar.
func TestChopNoBlocks(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: append(treeOf("oak", 6, 0, 8), "give Tester iron_axe 1")})
		err := runChop(t, g, []string{"oak"}, world.BlockPos{X: 0, Y: 64, Z: 0})
		holes := 0
		for x := -12; x <= 18; x++ {
			for z := -12; z <= 12; z++ {
				if st, ok := g.B.World.BlockAt(world.BlockPos{X: x, Y: 63, Z: z}); ok && world.StateString(st) == "minecraft:air" {
					holes++
				}
			}
		}
		t.Logf("error %v; hoyos %d; troncos %d; tierra %d", err, holes, g.Count("minecraft:oak_log"), g.Count("minecraft:dirt"))
		if err != nil {
			t.Fatal(err)
		}
		if n := logsLeft(g, 6, 0, 6, 0); n > 0 {
			t.Errorf("quedan %d troncos en pie", n)
		}
		if holes > 10 {
			t.Errorf("cavó %d hoyos para una torre de unos 4 bloques", holes)
		}
	})
}
