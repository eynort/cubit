package automation_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/automation"
	"minecraft-bot/schematic"
)

// hut es una caseta de 3×7×3: suelo de ladrillo de piedra, un anillo de
// tablones con una escalera mirando al norte y un tronco tumbado en x, una
// losa de arriba con un cristal encima, una losa doble, una columna de
// ladrillo hasta 6 de alto (para subirse) y aire dentro.
func hut() *schematic.Schematic {
	pal := []schematic.State{
		{Name: "air"},
		{Name: "stone_bricks"},
		{Name: "oak_planks"},
		{Name: "oak_stairs", Props: map[string]string{"facing": "north", "half": "bottom", "shape": "straight", "waterlogged": "false"}},
		{Name: "oak_log", Props: map[string]string{"axis": "x"}},
		{Name: "oak_slab", Props: map[string]string{"type": "top", "waterlogged": "false"}},
		{Name: "oak_slab", Props: map[string]string{"type": "double", "waterlogged": "false"}},
		{Name: "glass"},
	}
	sx, sy, sz := 3, 7, 3
	cells := make([]int32, sx*sy*sz)
	set := func(x, y, z int, i int32) { cells[x+z*sx+y*sx*sz] = i }
	for x := 0; x < 3; x++ {
		for z := 0; z < 3; z++ {
			set(x, 0, z, 1)
			if x != 1 || z != 1 {
				set(x, 1, z, 2)
			}
		}
	}
	set(1, 1, 0, 3) // la escalera, al norte
	set(0, 1, 1, 4) // el tronco, en x
	set(0, 2, 0, 5) // losa de arriba
	set(0, 3, 0, 7) // cristal encima
	set(2, 2, 2, 6) // losa doble
	for y := 2; y <= 6; y++ {
		set(2, y, 0, 1) // la columna
	}
	return &schematic.Schematic{Name: "caseta", Palette: pal, Regions: []schematic.Region{{Name: "main", Size: schematic.Pos{X: sx, Y: sy, Z: sz}, Cells: cells}}}
}

// TestBuildHut: el bot construye la caseta girada 90° (como la tendría
// colocada un jugador en Litematica) con los materiales de un cofre: quita el
// adoquín que sobra dentro, pone cada bloque con su orientación y su mitad, se
// sube para la columna, y al final todo encaja.
func TestBuildHut(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester diamond_pickaxe 1", "give Tester dirt 32",
			"setblock -3 64 -3 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"setblock 2 65 4 minecraft:cobblestone", // dentro de la caseta, donde va aire
		}})
		r, _ := safeRunner(t, g)
		for item, n := range map[string]int{"stone_bricks": 32, "oak_planks": 16, "oak_stairs": 4, "oak_log": 4, "oak_slab": 8, "glass": 4} {
			g.PutInChest(-3, 64, -3, "minecraft:"+item, n)
		}
		g.Step(5)
		pl := schematic.Placement{Origin: schematic.Pos{X: 3, Y: 64, Z: 3}, Rotation: 1}
		p, err := hut().Place(pl)
		if err != nil {
			t.Fatal(err)
		}
		var buildErr error
		r.Start(context.Background(), "construir", func(j *automation.Job) error {
			buildErr = automation.Build(p, automation.BuildOptions{Name: "caseta",
				Chests: []world.BlockPos{{X: -3, Y: 64, Z: -3}}})(j)
			return buildErr
		})
		g.Until(20*60*20, func() bool { return r.Status() == "" })
		bad := 0
		p.Each(func(sp schematic.Pos, idx int32) {
			want := p.Palette[idx]
			have := schematic.ParseState(world.StateString(g.S.World.At(sp.X, sp.Y, sp.Z)))
			if !schematic.Matches(want, have) {
				bad++
				t.Errorf("%v: hay %s, quiero %s", sp, have, want)
			}
		})
		// la escalera mira al norte en el schematic: girada 90° mira al este
		st := world.StateString(g.S.World.At(3, 65, 4))
		t.Logf("escalera: %s; error: %v", st, buildErr)
		if !strings.Contains(st, "facing=east") {
			t.Errorf("la escalera no mira al este: %s", st)
		}
		if buildErr != nil {
			t.Errorf("la tarea acabó con error: %v", buildErr)
		}
		// sin andamios por ahí
		for x := -1; x <= 6; x++ {
			for z := -1; z <= 7; z++ {
				for y := 64; y <= 72; y++ {
					if p.Inside(schematic.Pos{X: x, Y: y, Z: z}) {
						continue
					}
					if n := world.StateString(g.S.World.At(x, y, z)); n == "minecraft:dirt" {
						t.Errorf("andamio sin quitar en %d %d %d", x, y, z)
					}
				}
			}
		}
	})
}

// tower es una torre hueca de 5×9×5: suelo, paredes de 8 con una puerta de
// 2 de alto, techo; dentro un horno mirando al norte y una antorcha en la
// pared mirando al sur.
func tower() *schematic.Schematic {
	pal := []schematic.State{
		{Name: "air"},
		{Name: "stone_bricks"},
		{Name: "furnace", Props: map[string]string{"facing": "north", "lit": "false"}},
		{Name: "wall_torch", Props: map[string]string{"facing": "south"}},
	}
	sx, sy, sz := 5, 10, 5
	cells := make([]int32, sx*sy*sz)
	set := func(x, y, z int, i int32) { cells[x+z*sx+y*sx*sz] = i }
	for x := 0; x < sx; x++ {
		for z := 0; z < sz; z++ {
			set(x, 0, z, 1) // suelo
			set(x, 9, z, 1) // techo
			for y := 1; y <= 8; y++ {
				if x == 0 || z == 0 || x == sx-1 || z == sz-1 {
					set(x, y, z, 1)
				}
			}
		}
	}
	set(2, 1, 4, 0) // la puerta (al sur)
	set(2, 2, 4, 0)
	set(1, 1, 1, 2) // el horno
	set(2, 3, 1, 3) // la antorcha, en la pared del norte, mirando al sur
	return &schematic.Schematic{Name: "torre", Palette: pal, Regions: []schematic.Region{{Name: "main", Size: schematic.Pos{X: sx, Y: sy, Z: sz}, Cells: cells}}}
}

// TestBuildTower: la torre con espejo y media vuelta: no cabe todo de una vez
// (varios viajes al cofre), hay que subirse con andamios para las paredes
// altas y el techo, el horno y la antorcha van orientados.
func TestBuildTower(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester diamond_pickaxe 1", "give Tester dirt 64", "give Tester bread 16",
			"setblock -4 64 -4 minecraft:chest[facing=north,type=single,waterlogged=false]",
		}})
		r, _ := safeRunner(t, g)
		g.PutInChest(-4, 64, -4, "minecraft:stone_bricks", 64*4)
		g.PutInChest(-4, 64, -4, "minecraft:furnace", 1)
		g.PutInChest(-4, 64, -4, "minecraft:torch", 4)
		g.PutInChest(-4, 64, -4, "minecraft:dirt", 64)
		// relleno del inventario: deja sitio para poco (obliga a varios viajes)
		for _, it := range []string{"oak_log", "cobblestone", "netherrack", "obsidian", "crafting_table"} {
			g.Give("minecraft:"+it, 64*4)
		}
		g.Step(5)
		pl := schematic.Placement{Origin: schematic.Pos{X: 8, Y: 64, Z: 2}, Rotation: 2, Mirror: schematic.MirrorLeftRight}
		p, err := tower().Place(pl)
		if err != nil {
			t.Fatal(err)
		}
		var buildErr error
		r.Start(context.Background(), "construir", func(j *automation.Job) error {
			buildErr = automation.Build(p, automation.BuildOptions{Name: "torre",
				Chests: []world.BlockPos{{X: -4, Y: 64, Z: -4}}})(j)
			return buildErr
		})
		g.Until(60*60*20, func() bool { return r.Status() == "" })
		bad := 0
		p.Each(func(sp schematic.Pos, idx int32) {
			want := p.Palette[idx]
			have := schematic.ParseState(world.StateString(g.S.World.At(sp.X, sp.Y, sp.Z)))
			if !schematic.Matches(want, have) {
				bad++
				if bad < 10 {
					t.Errorf("%v: hay %s, quiero %s", sp, have, want)
				}
			}
		})
		t.Logf("mal: %d; error: %v", bad, buildErr)
		if buildErr != nil {
			t.Errorf("la tarea acabó con error: %v", buildErr)
		}
	})
}
