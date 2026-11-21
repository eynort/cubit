package automation_test

import (
	"bytes"
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"

	"cubit/automation"
)

// Las pruebas de /mine. El mundo plano: piedra hasta y=60, tierra 61 y 62,
// hierba 63; el bot en (0, 64, 0) y un cofre en (-3, 64, 0). Con la capa 50 la
// escalera baja hacia +x (cada escalón, un bloque a +x y uno abajo) y llega al
// túnel principal en x=14; el túnel sigue por +x, y las ramas (cada 3) van a
// +z (lado +) y -z.

const (
	tunnelX0 = 14 // donde acaba la escalera
	layerY   = 50
)

func at(g *simworld.Game, x, y, z int) string {
	s := g.S.World.At(x, y, z)
	if block.IsAir(s) {
		return "air"
	}
	return strings.TrimPrefix(block.StateList[s].ID(), "minecraft:")
}

// mine lanza /mine y espera a que acabe; devuelve el error y el log.
func mine(t *testing.T, g *simworld.Game, cfg automation.MineConfig, ticks int) (error, string) {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	r, _ := safeRunner(t, g)
	g.Step(5)
	var err error
	r.Start(context.Background(), "minar", func(j *automation.Job) error {
		err = automation.MineOres(cfg)(j)
		return err
	})
	g.Until(ticks, func() bool { return r.Status() == "" })
	return err, buf.String()
}

func ores(t *testing.T, list string) []automation.OreSpec {
	t.Helper()
	os, err := automation.ParseOres(list)
	if err != nil {
		t.Fatal(err)
	}
	return os
}

// TestMineStrip: baja por una escalera (no en vertical), hace el túnel
// principal y las ramas cada 3, pica el hierro que queda a la vista y la veta
// que se destapa, y NO toca el hierro escondido en la roca (el anti-xray): ese
// no tiene ninguna cara al aire del túnel, aunque el mundo cargado lo tenga a
// pocos bloques.
func TestMineStrip(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"give Tester diamond_pickaxe 1", "give Tester cobblestone 16",
			// a la vista: en la pared de la rama de x=17, y una veta de 3 que sale del final de la rama (+z)
			"setblock 18 50 3 minecraft:iron_ore",
			"setblock 17 50 6 minecraft:iron_ore", "setblock 17 50 7 minecraft:iron_ore", "setblock 17 50 8 minecraft:iron_ore",
			// escondido: a 3-5 bloques del túnel, sin ninguna cara al aire
			"setblock 19 49 4 minecraft:iron_ore", "setblock 16 54 0 minecraft:iron_ore",
			"setblock 22 46 0 minecraft:iron_ore", "setblock 18 55 -3 minecraft:iron_ore",
			"setblock 21 50 8 minecraft:iron_ore", // (justo pasada la rama de x=20, que acaba en z=5: a 3 de ella)
		}})
		l := layerY
		err, out := mine(t, g, automation.MineConfig{
			Ores: ores(t, "iron"), Chest: world.BlockPos{X: -3, Y: 64, Z: 0},
			Layer: &l, MainLen: 9, BranchLen: 5, Every: 100,
		}, 12000*20)
		t.Log(out)
		if err != nil {
			t.Fatalf("minar: %v", err)
		}
		// la escalera: un hueco por escalón; la columna bajo el bot no se cavó
		for k := 1; k <= 14; k++ {
			if at(g, k, 64-k, 0) != "air" || at(g, k, 65-k, 0) != "air" {
				t.Errorf("falta el escalón %d en %d %d 0", k, k, 64-k)
			}
		}
		for y := 50; y <= 62; y++ {
			if at(g, 0, y, 0) == "air" {
				t.Errorf("cavó en vertical bajo el bot (0 %d 0)", y)
			}
		}
		// el túnel principal y las ramas de x=14, 17, 20, 23 (cada 3)
		for x := tunnelX0; x <= tunnelX0+9; x++ {
			for _, y := range []int{layerY, layerY + 1} {
				if at(g, x, y, 0) != "air" {
					t.Errorf("falta el túnel principal en %d %d 0", x, y)
				}
			}
		}
		for _, bx := range []int{14, 17, 20, 23} {
			for _, z := range []int{5, -5} {
				if at(g, bx, layerY, z) != "air" || at(g, bx, layerY+1, z) != "air" {
					t.Errorf("falta el final de la rama de x=%d en z=%d", bx, z)
				}
			}
		}
		// entre dos ramas (x=15,16) no se cavó nada: queda la roca
		for _, x := range []int{15, 16, 18, 19} {
			if at(g, x, layerY, 3) == "air" && !(x == 18) { // (18 50 3 era el hierro de la pared)
				t.Errorf("cavó entre las ramas: %d %d 3", x, layerY)
			}
		}
		// lo que se veía, fuera: en el cofre (se deja al acabar)
		for _, p := range [][3]int{{18, 50, 3}, {17, 50, 6}, {17, 50, 7}, {17, 50, 8}} {
			if n := at(g, p[0], p[1], p[2]); n != "air" {
				t.Errorf("el hierro de %v a la vista sigue ahí (%s)", p, n)
			}
		}
		if n := g.ChestCount(-3, 64, 0, "minecraft:raw_iron"); n != 4 {
			t.Errorf("en el cofre hay %d de hierro en bruto, tenían que ser 4 (los que se veían)", n)
		}
		// lo escondido, intacto (anti-xray)
		for _, p := range [][3]int{{19, 49, 4}, {16, 54, 0}, {22, 46, 0}, {18, 55, -3}, {21, 50, 8}} {
			if n := at(g, p[0], p[1], p[2]); n != "iron_ore" {
				t.Errorf("XRAY: el hierro escondido de %v ya no está (%s): fue a por un mineral que no se veía", p, n)
			}
		}
		if g.Count("minecraft:diamond_pickaxe") != 1 {
			t.Errorf("perdió el pico (lleva %d)", g.Count("minecraft:diamond_pickaxe"))
		}
		if g.ChestCount(-3, 64, 0, "minecraft:diamond_pickaxe") != 0 || g.ChestCount(-3, 64, 0, "minecraft:cobblestone") != 0 {
			t.Errorf("dejó en el cofre una herramienta o basura")
		}
	})
}

// TestMineNoXray: un mundo con minerales solo escondidos (ninguno con cara al
// aire): al acabar la zona no ha sacado nada ni ha cavado hacia ellos.
func TestMineNoXray(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		hidden := [][3]int{{16, 50, 9}, {16, 50, -9}, {16, 56, 0}, {19, 44, 2}, {13, 47, -6}}
		setup := []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"give Tester diamond_pickaxe 1",
		}
		for _, p := range hidden {
			setup = append(setup, "setblock "+itoa(p[0])+" "+itoa(p[1])+" "+itoa(p[2])+" minecraft:diamond_ore")
		}
		g := simworld.Start(t, simworld.Options{Setup: setup})
		l := layerY
		err, out := mine(t, g, automation.MineConfig{
			Ores: ores(t, "diamond"), Chest: world.BlockPos{X: -3, Y: 64, Z: 0},
			Layer: &l, MainLen: 6, BranchLen: 6,
		}, 12000*20)
		t.Log(out)
		if err != nil {
			t.Fatalf("minar: %v", err)
		}
		for _, p := range hidden {
			if n := at(g, p[0], p[1], p[2]); n != "diamond_ore" {
				t.Errorf("XRAY: el diamante escondido de %v ya no está (%s)", p, n)
			}
		}
		if n := g.Count("minecraft:diamond") + g.ChestCount(-3, 64, 0, "minecraft:diamond"); n != 0 {
			t.Errorf("sacó %d diamantes sin que se vieran", n)
		}
	})
}

func itoa(n int) string { return strconv.Itoa(n) }

// TestMineChest: sin pico, lo coge del cofre (el de piedra no vale para
// diamantes: coge el de hierro); deja lo minado cada 2 y no deja el pico ni
// la comida; al acabar (total 4) lo que lleve va al cofre.
func TestMineChest(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"give Tester bread 5", "give Tester torch 8",
			// diamantes en la pared de las ramas (a la vista al pasar)
			"setblock 16 50 2 minecraft:diamond_ore", "setblock 16 50 -2 minecraft:deepslate_diamond_ore",
			"setblock 19 51 2 minecraft:diamond_ore", "setblock 19 50 -3 minecraft:diamond_ore",
			"setblock 22 50 3 minecraft:diamond_ore", "setblock 22 51 -3 minecraft:diamond_ore",
		}})
		g.PutInChest(-3, 64, 0, "minecraft:stone_pickaxe", 1)
		g.PutInChest(-3, 64, 0, "minecraft:iron_pickaxe", 1)
		l := layerY
		err, out := mine(t, g, automation.MineConfig{
			Ores: ores(t, "diamond"), Chest: world.BlockPos{X: -3, Y: 64, Z: 0},
			Layer: &l, MainLen: 9, BranchLen: 5, Every: 2, Total: 4,
		}, 20000*20)
		t.Log(out)
		if err != nil {
			t.Fatalf("minar: %v", err)
		}
		inChest := g.ChestCount(-3, 64, 0, "minecraft:diamond")
		if inChest < 4 || g.Count("minecraft:diamond") != 0 {
			t.Errorf("diamantes: %d en el cofre, %d encima (tenía que dejar todo)", inChest, g.Count("minecraft:diamond"))
		}
		if g.Count("minecraft:iron_pickaxe") != 1 {
			t.Errorf("no cogió el pico de hierro del cofre (lleva %d)", g.Count("minecraft:iron_pickaxe"))
		}
		if g.Count("minecraft:bread") != 5 || g.Count("minecraft:torch") == 0 {
			t.Errorf("dejó la comida o las antorchas: pan %d, antorchas %d", g.Count("minecraft:bread"), g.Count("minecraft:torch"))
		}
		if strings.Count(out, "al cofre") < 3 { // (al empezar sin pico, y al menos dos veces por llevar 2)
			t.Errorf("tenía que ir al cofre varias veces:\n%s", out)
		}
		if !strings.Contains(out, "resumen") {
			t.Errorf("sin resumen")
		}
	})
}

// TestMineVein: un mineral a la vista y detrás, en línea, otros dos que solo
// se ven al picar el primero: los saca los tres (la veta).
func TestMineVein(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"give Tester diamond_pickaxe 1",
			"setblock 17 50 7 minecraft:coal_ore", "setblock 17 50 8 minecraft:coal_ore", "setblock 17 51 8 minecraft:coal_ore",
			"setblock 17 50 9 minecraft:coal_ore",
		}})
		l := layerY
		err, out := mine(t, g, automation.MineConfig{
			Ores: ores(t, "coal"), Chest: world.BlockPos{X: -3, Y: 64, Z: 0},
			Layer: &l, MainLen: 3, BranchLen: 6, Every: 100,
		}, 12000*20)
		t.Log(out)
		if err != nil {
			t.Fatalf("minar: %v", err)
		}
		if n := g.ChestCount(-3, 64, 0, "minecraft:coal"); n != 4 {
			t.Errorf("la veta: %d de carbón en el cofre, tenían que ser 4", n)
		}
	})
}

// TestChooseLayer: la capa de cada mineral y la común de varios.
func TestChooseLayer(t *testing.T) {
	cases := []struct {
		ores    string
		surface int
		want    int
		fails   bool
	}{
		{"diamond", 64, -59, false},
		{"redstone", 64, -58, false},
		{"gold", 64, -16, false},
		{"lapis", 64, 0, false},
		{"iron", 64, 16, false},
		{"copper", 64, 48, false},
		{"coal", 200, 96, false},
		{"coal", 64, 56, false},      // (el suelo está más bajo que 96: lo más alto bajo la superficie)
		{"emerald", 260, 236, false}, // (en una montaña)
		{"emerald", 70, 62, false},   // (sin montaña: lo mejor que se puede)
		{"debris", 64, 15, false},
		{"diamond,redstone", 64, -58, false},
		{"diamond,iron", 64, -10, false},  // (donde el peor de los dos sale mejor)
		{"iron,copper", 64, 28, false},    // (entre el pico del hierro, 16, y el del cobre, 48)
		{"diamond,copper", 64, -1, false}, // (comparten -16..16)
		{"diamond,debris", 64, 0, true},   // (otra dimensión)
	}
	// sin capa común: dos rangos que no se tocan
	a, b := ores(t, "diamond")[0], ores(t, "copper")[0]
	a.Hi, b.Lo = -30, -20
	if y, _, err := automation.ChooseLayer([]automation.OreSpec{a, b}, 64); err == nil {
		t.Errorf("rangos que no se tocan: tendría que dar error, dio y=%d", y)
	}
	for _, c := range cases {
		y, why, err := automation.ChooseLayer(ores(t, c.ores), c.surface)
		if c.fails {
			if err == nil {
				t.Errorf("%s: tendría que dar error, dio y=%d", c.ores, y)
			}
			continue
		}
		if err != nil || y != c.want {
			t.Errorf("%s (suelo %d): y=%d, %v; esperaba %d", c.ores, c.surface, y, err, c.want)
		}
		t.Logf("%s: %s", c.ores, why)
	}
}

// TestMineSafety: lava junto a la rama (y encima de ella, en la pared) y un
// hueco bajo el suelo de la rama: tapa la lava antes de abrir, tapa el hueco
// en vez de cavar sin suelo, no se quema ni se cae.
func TestMineSafety(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"give Tester diamond_pickaxe 1", "give Tester cobblestone 64",
			"setblock 18 50 3 minecraft:lava", "setblock 17 52 4 minecraft:lava", // (a un lado y encima del techo de la rama)
			"fill 17 49 2 17 44 2 minecraft:air", // (sin suelo en 17 50 2)
			"setblock 17 50 6 minecraft:diamond_ore",
		}})
		l := layerY
		err, out := mine(t, g, automation.MineConfig{
			Ores: ores(t, "diamond"), Chest: world.BlockPos{X: -3, Y: 64, Z: 0},
			Layer: &l, MainLen: 3, BranchLen: 6,
		}, 12000*20)
		t.Log(out)
		if err != nil {
			t.Fatalf("minar: %v", err)
		}
		if g.Health < 20 {
			t.Errorf("se hizo daño: vida %v", g.Health)
		}
		if n := at(g, 17, 49, 2); n == "air" {
			t.Errorf("cavó sin suelo: bajo 17 50 2 sigue el hueco")
		}
		if n := at(g, 18, 50, 3); n == "lava" {
			t.Errorf("la lava junto a la rama quedó sin tapar")
		}
	})
}

// TestMineCave: una cueva enorme corta la escalera por todos lados: sube y
// empieza otra a 10 bloques, y acaba sacando el hierro de la pared.
func TestMineCave(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"give Tester diamond_pickaxe 1", "give Tester cobblestone 16",
			"fill -3 40 -3 40 56 10 minecraft:air",
			"setblock -2 50 28 minecraft:iron_ore",
		}})
		l := layerY
		err, out := mine(t, g, automation.MineConfig{
			Ores: ores(t, "iron"), Chest: world.BlockPos{X: -3, Y: 64, Z: 0},
			Layer: &l, MainLen: 6, BranchLen: 4, Every: 100,
		}, 20000*20)
		t.Log(out)
		if err != nil {
			t.Fatalf("minar: %v", err)
		}
		if !strings.Contains(out, "empiezo otra") {
			t.Errorf("no cambió de sitio la escalera")
		}
		if n := g.ChestCount(-3, 64, 0, "minecraft:raw_iron"); n != 1 {
			t.Errorf("hierro en el cofre: %d, tenía que ser 1", n)
		}
	})
}

// TestMineFloorOre: una veta bajo el suelo de la rama, a la vista desde el
// túnel: no se pica (dejaría un hueco en el camino; nunca se cava hacia
// abajo), y la rama llega hasta el final con el suelo entero.
func TestMineFloorOre(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"give Tester diamond_pickaxe 1", "give Tester cobblestone 16",
			"setblock 17 49 2 minecraft:iron_ore", "setblock 17 48 2 minecraft:iron_ore",
			"setblock 18 49 4 minecraft:iron_ore", "setblock 18 51 4 minecraft:iron_ore", // (una en la pared y otra encima del suelo sí)
		}})
		l := layerY
		err, out := mine(t, g, automation.MineConfig{
			Ores: ores(t, "iron"), Chest: world.BlockPos{X: -3, Y: 64, Z: 0},
			Layer: &l, MainLen: 3, BranchLen: 5, Every: 100,
		}, 12000*20)
		t.Log(out)
		if err != nil {
			t.Fatalf("minar: %v", err)
		}
		if n := g.ChestCount(-3, 64, 0, "minecraft:raw_iron"); n != 1 {
			t.Errorf("hierro: %d, tenía que ser 1 (el de la pared; los del suelo no)", n)
		}
		for _, p := range [][3]int{{17, 49, 2}, {17, 48, 2}, {18, 49, 4}} {
			if n := at(g, p[0], p[1], p[2]); n != "iron_ore" {
				t.Errorf("picó el mineral del suelo %v (%s)", p, n)
			}
		}
		if at(g, 17, 50, 5) != "air" {
			t.Errorf("la rama no llegó al final")
		}
	})
}

// TestMineReturnCave: una cueva cerca de la superficie atraviesa la escalera;
// tras ir al cofre (cada 1) vuelve a la mina por su propia escalera, sin
// empezar otra.
func TestMineReturnCave(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"give Tester diamond_pickaxe 1", "give Tester cobblestone 16",
			"fill 2 57 -3 8 62 3 minecraft:air",
			// el suelo de los escalones de la escalera, de hojas (la copa de un árbol, como en el servidor real)
			"setblock 2 61 0 minecraft:oak_leaves[persistent=true]", "setblock 3 60 0 minecraft:oak_leaves[persistent=true]",
			"setblock 1 62 0 minecraft:oak_leaves[persistent=true]",
			"give Tester raw_iron 1",
		}})
		l := layerY
		err, out := mine(t, g, automation.MineConfig{
			Ores: ores(t, "iron"), Chest: world.BlockPos{X: -3, Y: 64, Z: 0},
			Layer: &l, MainLen: 6, BranchLen: 4, Every: 1,
		}, 30000*20)
		t.Log(out)
		if err != nil {
			t.Fatalf("minar: %v", err)
		}
		if strings.Contains(out, "construir:") {
			t.Errorf("el pathfinding construyó dentro de la escalera (hojas de suelo):\n%s", out)
		}
		if strings.Contains(out, "otra escalera") {
			t.Errorf("empezó otra escalera en vez de volver por la suya")
		}
		if n := g.ChestCount(-3, 64, 0, "minecraft:raw_iron"); n != 1 {
			t.Errorf("hierro en el cofre: %d, tenía que ser 1", n)
		}
		if !strings.Contains(out, "vuelvo a la mina") || !strings.Contains(out, "túnel principal llegó") {
			t.Errorf("no volvió a la mina y siguió:\n%s", out)
		}
	})
}
