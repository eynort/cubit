package automation_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/automation"
	"minecraft-bot/pathing"
)

const leaves = "minecraft:oak_leaves[distance=1,persistent=false,waterlogged=false]"

// tree pone un roble: tronco de h en (x, z) con copa de hojas naturales.
func tree(x, z, h int) []string {
	top := 63 + h
	return []string{
		sprintf("fill %d %d %d %d %d %d %s", x-2, top-1, z-2, x+2, top, z+2, leaves),
		sprintf("fill %d %d %d %d %d %d %s", x-1, top+1, z-1, x+1, top+1, z+1, leaves),
		sprintf("fill %d 64 %d %d %d %d minecraft:oak_log[axis=y]", x, z, x, top, z),
	}
}

// TestWoodcut: dos robles; el bot tala, recoge los troncos y se hace un
// hacha de madera (mesa de trabajo incluida). (Los cofres no: simworld no los abre.)
func TestWoodcut(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		setup := append(tree(6, 0, 5), tree(-5, 4, 4)...)
		setup = append(setup, "give Tester dirt 16", "give Tester iron_axe 1") // el hacha: probado en progress_test
		g := simworld.Start(t, simworld.Options{Setup: setup})
		b := g.B
		b.Mover.Food = func() int { return 20 }
		w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		bd := &pathing.HandsBuilder{Screens: b.Screens, Hands: b.Hands, Ctl: b.Ctl, Ctx: context.Background()}
		w.SetBuilder(bd)
		b.Ctl.Think = w.Think
		r := automation.New(automation.Deps{
			Client: b.C, Player: b.Player, World: b.World, Entities: b.Entities, Screens: b.Screens,
			Hands: b.Hands, Ctl: b.Ctl, Walker: w, Builder: bd, Fighting: func() bool { return false },
		})
		g.Step(5)
		r.Start(context.Background(), "talar", automation.Woodcut("oak", 9))
		g.Until(120*20, func() bool { return r.Status() == "" })
		count := func(item string) int {
			b.Screens.Lock()
			defer b.Screens.Unlock()
			n := 0
			for i := 9; i <= 44; i++ {
				s := b.Screens.Inventory.Slots[i]
				if s.Count > 0 && strings.HasSuffix(itemName(int32(s.Item)), item) {
					n += int(s.Count)
				}
			}
			return n
		}
		logs, planks, axe := count("oak_log"), count("oak_planks"), count("_axe")
		t.Logf("troncos %d, tablones %d, hachas %d, estado %q", logs, planks, axe, r.Status())
		_ = time.Second
		standing := 0 // troncos que quedan en pie de los dos árboles
		for y := 64; y <= 68; y++ {
			for _, c := range [][2]int{{6, 0}, {-5, 4}} {
				if st, ok := b.World.BlockAt(world.BlockPos{X: c[0], Y: y, Z: c[1]}); ok && strings.Contains(world.StateString(st), "_log") {
					standing++
				}
			}
		}
		if standing > 0 {
			t.Errorf("quedan %d troncos en pie", standing)
		}
		if axe == 0 {
			t.Errorf("perdió el hacha")
		}
	})
}

// TestIronAxe: con 3 de hierro en bruto y 8 adoquines, se hace un horno,
// funde el hierro (con madera) y se hace un hacha de hierro.
func TestIronAxe(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		setup := append(tree(6, 0, 4), "give Tester raw_iron 3", "give Tester cobblestone 8", "give Tester oak_log 6")
		g := simworld.Start(t, simworld.Options{Setup: setup})
		b := g.B
		b.Mover.Food = func() int { return 20 }
		w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		b.Ctl.Think = w.Think
		r := automation.New(automation.Deps{
			Client: b.C, Player: b.Player, World: b.World, Entities: b.Entities, Screens: b.Screens,
			Hands: b.Hands, Ctl: b.Ctl, Walker: w, Fighting: func() bool { return false },
		})
		g.Step(5)
		var made string
		var err error
		r.Start(context.Background(), "hacha", func(j *automation.Job) error {
			made, err = j.CraftAxe()
			return err
		})
		g.Until(120*20, func() bool { return r.Status() == "" })
		t.Logf("hizo %q, error %v", made, err)
		if made != "iron_axe" {
			t.Fatalf("no hizo el hacha de hierro")
		}
	})
}

// TestTallTreeNoBlocks: un árbol alto (7) y el bot sin bloques de construir:
// junta tierra, sube y lo tala entero.
func TestTallTreeNoBlocks(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: append(tree(5, 0, 7), "give Tester iron_axe 1")})
		b := g.B
		b.Mover.Food = func() int { return 20 }
		w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		bd := &pathing.HandsBuilder{Screens: b.Screens, Hands: b.Hands, Ctl: b.Ctl, Ctx: context.Background()}
		w.SetBuilder(bd)
		b.Ctl.Think = w.Think
		r := automation.New(automation.Deps{
			Client: b.C, Player: b.Player, World: b.World, Entities: b.Entities, Screens: b.Screens,
			Hands: b.Hands, Ctl: b.Ctl, Walker: w, Builder: bd, Fighting: func() bool { return false },
		})
		g.Step(5)
		r.Start(context.Background(), "talar", automation.Woodcut("oak", 7))
		g.Until(120*20, func() bool { return r.Status() == "" })
		standing := 0
		for y := 64; y <= 70; y++ {
			if st, ok := b.World.BlockAt(world.BlockPos{X: 5, Y: y, Z: 0}); ok && strings.Contains(world.StateString(st), "_log") {
				standing++
			}
		}
		p := b.Player.Position()
		if g.Items() > 0 {
			t.Errorf("dejó objetos tirados: %s", g.ItemsAt())
		}
		t.Logf("quedan %d troncos; el bot en %.1f %.1f %.1f", standing, p.X, p.Y, p.Z)
		if standing > 0 {
			t.Errorf("quedan %d troncos en pie", standing)
		}
		if p.Y > 65.5 {
			t.Errorf("se quedó arriba (y=%.1f)", p.Y)
		}
	})
}

// TestDenseTree: un árbol de 9 con hojas desde el cuarto bloque hasta arriba,
// dos bloques alrededor por todos lados (no hay sitio para una torre por
// fuera): sube por dentro del tronco rompiendo las hojas y lo tala entero.
func TestDenseTree(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		x, z, h := 5, 0, 9
		setup := []string{
			sprintf("fill %d %d %d %d %d %d %s", x-2, 67, z-2, x+2, 63+h+1, z+2, leaves),
			sprintf("fill %d 64 %d %d %d %d minecraft:spruce_log[axis=y]", x, z, x, 63+h, z),
			"give Tester dirt 16", "give Tester oak_log 2", "give Tester iron_axe 1",
		}
		g := simworld.Start(t, simworld.Options{Setup: setup})
		b := g.B
		b.Mover.Food = func() int { return 20 }
		w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		bd := &pathing.HandsBuilder{Screens: b.Screens, Hands: b.Hands, Ctl: b.Ctl, Ctx: context.Background()}
		w.SetBuilder(bd)
		b.Ctl.Think = w.Think
		r := automation.New(automation.Deps{
			Client: b.C, Player: b.Player, World: b.World, Entities: b.Entities, Screens: b.Screens,
			Hands: b.Hands, Ctl: b.Ctl, Walker: w, Builder: bd, Fighting: func() bool { return false },
		})
		g.Step(5)
		r.Start(context.Background(), "talar", automation.Woodcut("spruce", h))
		g.Until(180*20, func() bool { return r.Status() == "" })
		standing := 0
		for y := 64; y <= 63+h; y++ {
			if st, ok := b.World.BlockAt(world.BlockPos{X: x, Y: y, Z: z}); ok && strings.Contains(world.StateString(st), "_log") {
				standing++
			}
		}
		p := b.Player.Position()
		t.Logf("ITEMS servidor: %s", g.ItemsAt())
		t.Logf("quedan %d troncos; el bot en %.1f %.1f %.1f", standing, p.X, p.Y, p.Z)
		if standing > 0 {
			t.Errorf("quedan %d troncos en pie", standing)
		}
		if p.Y > 65.5 {
			t.Errorf("se quedó arriba (y=%.1f)", p.Y)
		}
	})
}

// TestLeavesToTheGround: un abeto con hojas desde el suelo, dos bloques
// alrededor por todos lados: no hay camino al tronco sin romperlas. Las rompe,
// llega y lo tala entero.
func TestLeavesToTheGround(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		x, z, h := 5, 0, 7
		setup := []string{
			sprintf("fill %d %d %d %d %d %d %s", x-2, 64, z-2, x+2, 63+h+1, z+2, leaves),
			sprintf("fill %d 64 %d %d %d %d minecraft:spruce_log[axis=y]", x, z, x, 63+h, z),
			"give Tester dirt 16", "give Tester oak_log 2", "give Tester iron_axe 1",
		}
		g := simworld.Start(t, simworld.Options{Setup: setup})
		r := runner(t, g)
		g.Step(5)
		r.Start(context.Background(), "talar", automation.Woodcut("spruce", h))
		g.Until(240*20, func() bool { return r.Status() == "" })
		standing := 0
		for y := 64; y <= 63+h; y++ {
			if st, ok := g.B.World.BlockAt(world.BlockPos{X: x, Y: y, Z: z}); ok && strings.Contains(world.StateString(st), "_log") {
				standing++
			}
		}
		p := g.B.Player.Position()
		t.Logf("ITEMS servidor: %s", g.ItemsAt())
		t.Logf("quedan %d troncos; el bot en %.1f %.1f %.1f", standing, p.X, p.Y, p.Z)
		if standing > 0 {
			t.Errorf("quedan %d troncos en pie", standing)
		}
		if n := scaffoldLeft(g, x-3, x+8, z-3, z+3); n > 0 {
			t.Errorf("dejó %d bloques de andamio", n)
		}
	})
}

// TestWalkThroughLeaves: el sitio al que va está encerrado en hojas (un
// arbusto grande): el camino las rompe para entrar, no se rinde.
func TestWalkThroughLeaves(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			sprintf("fill 6 64 -2 10 66 2 %s", leaves),
			"fill 8 64 0 8 65 0 minecraft:air",
		}})
		r := runner(t, g)
		g.Step(5)
		var err error
		r.Start(context.Background(), "ir", func(j *automation.Job) error {
			err = j.GoTo(world.BlockPos{X: 8, Y: 64, Z: 0})
			return err
		})
		g.Until(60*20, func() bool { return r.Status() == "" })
		p := g.B.Player.Position()
		t.Logf("error %v; el bot en %.2f %.2f %.2f", err, p.X, p.Y, p.Z)
		if err != nil || int(p.X) != 8 || int(p.Z) != 0 && p.Z > 0 {
			t.Fatalf("no llegó a través de las hojas")
		}
	})
}

// TestKeepsPlacedLeaves: un seto de hojas puestas por un jugador
// (persistent=true) no se rompe: rodea.
func TestKeepsPlacedLeaves(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		hedge := "minecraft:oak_leaves[distance=7,persistent=true,waterlogged=false]"
		g := simworld.Start(t, simworld.Options{Setup: []string{
			sprintf("fill 4 64 -3 4 65 3 %s", hedge),
		}})
		r := runner(t, g)
		g.Step(5)
		var err error
		r.Start(context.Background(), "ir", func(j *automation.Job) error {
			err = j.GoTo(world.BlockPos{X: 7, Y: 64, Z: 0})
			return err
		})
		g.Until(60*20, func() bool { return r.Status() == "" })
		broken := 0
		for z := -3; z <= 3; z++ {
			for y := 64; y <= 65; y++ {
				if st, _ := g.B.World.BlockAt(world.BlockPos{X: 4, Y: y, Z: z}); !strings.Contains(world.StateString(st), "leaves") {
					broken++
				}
			}
		}
		t.Logf("error %v; hojas del seto rotas %d", err, broken)
		if err != nil || broken > 0 {
			t.Fatalf("rompió el seto o no llegó")
		}
	})
}

// TestBranchOutOfReach: un roble con una rama larga (troncos en diagonal que
// se alejan 5 del tronco): desde dentro del tronco no se alcanza la punta.
// No pasa a otro árbol hasta talarla también (va a por ella con una torre).
func TestBranchOutOfReach(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		x, z, h := 5, 0, 8
		top := 63 + h
		setup := append(tree(x, z, h),
			sprintf("setblock %d %d %d minecraft:oak_log[axis=x]", x+1, top, z),
			sprintf("setblock %d %d %d minecraft:oak_log[axis=x]", x+2, top+1, z),
			sprintf("setblock %d %d %d minecraft:oak_log[axis=x]", x+3, top+2, z),
			sprintf("setblock %d %d %d minecraft:oak_log[axis=x]", x+4, top+3, z),
			sprintf("setblock %d %d %d minecraft:oak_log[axis=x]", x+5, top+4, z),
			sprintf("fill %d %d %d %d %d %d %s", x+4, top+4, z-1, x+6, top+5, z+1, leaves),
			sprintf("setblock %d %d %d minecraft:oak_log[axis=x]", x+5, top+4, z),
			"give Tester dirt 4", "give Tester oak_log 2", "give Tester iron_axe 1",
		)
		g := simworld.Start(t, simworld.Options{Setup: setup})
		r := runner(t, g)
		g.Step(5)
		r.Start(context.Background(), "talar", automation.Woodcut("oak", h+5))
		g.Until(400*20, func() bool { return r.Status() == "" })
		standing := 0
		for dx := 0; dx <= 5; dx++ {
			for y := 64; y <= top+4; y++ {
				if st, ok := g.B.World.BlockAt(world.BlockPos{X: x + dx, Y: y, Z: z}); ok && strings.Contains(world.StateString(st), "_log") {
					standing++
				}
			}
		}
		p := g.B.Player.Position()
		t.Logf("ITEMS servidor: %s", g.ItemsAt())
		t.Logf("quedan %d troncos; el bot en %.1f %.1f %.1f", standing, p.X, p.Y, p.Z)
		if standing > 0 {
			t.Errorf("quedan %d troncos en pie", standing)
		}
		if n := scaffoldLeft(g, x-3, x+8, z-3, z+3); n > 0 {
			t.Errorf("dejó %d bloques de andamio", n)
		}
	})
}

// scaffoldLeft cuenta los bloques de tierra puestos por encima del suelo (y > 63).
func scaffoldLeft(g *simworld.Game, x0, x1, z0, z1 int) int {
	n := 0
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			for y := 64; y <= 80; y++ {
				if st, ok := g.B.World.BlockAt(world.BlockPos{X: x, Y: y, Z: z}); ok {
					if s := world.StateString(st); strings.Contains(s, "dirt") || strings.Contains(s, "cobblestone") {
						n++
					}
				}
			}
		}
	}
	return n
}

// TestTreeOnStone: un árbol alto sobre piedra (sin tierra ni bloques en el
// inventario): pica la piedra de alrededor para tener con qué subir, lo tala
// entero y quita la torre.
func TestTreeOnStone(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		x, z, h := 5, 0, 9
		setup := append([]string{"fill -12 61 -12 16 63 12 minecraft:stone"}, tree(x, z, h)...)
		setup = append(setup, "give Tester iron_axe 1", "give Tester stone_pickaxe 1")
		g := simworld.Start(t, simworld.Options{Setup: setup})
		r := runner(t, g)
		g.Step(5)
		r.Start(context.Background(), "talar", automation.Woodcut("oak", h))
		g.Until(400*20, func() bool { return r.Status() == "" })
		standing := 0
		for y := 64; y <= 63+h; y++ {
			if st, ok := g.B.World.BlockAt(world.BlockPos{X: x, Y: y, Z: z}); ok && strings.Contains(world.StateString(st), "_log") {
				standing++
			}
		}
		p := g.B.Player.Position()
		t.Logf("ITEMS servidor: %s", g.ItemsAt())
		t.Logf("quedan %d troncos; el bot en %.1f %.1f %.1f", standing, p.X, p.Y, p.Z)
		if standing > 0 {
			t.Errorf("quedan %d troncos en pie", standing)
		}
		if n := scaffoldLeft(g, x-3, x+3, z-3, z+3); n > 0 {
			t.Errorf("dejó %d bloques de andamio", n)
		}
	})
}

// TestChopMakesStoneAxe: sin nada, /chop se hace un hacha de piedra (no va a
// por hierro) y tala.
func TestChopMakesStoneAxe(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		setup := append(tree(6, 0, 5), tree(-5, 4, 5)...)
		setup = append(setup, tree(2, -7, 5)...)
		setup = append(setup, tree(-6, -6, 5)...)
		g := simworld.Start(t, simworld.Options{Setup: setup})
		r := runner(t, g)
		g.Step(5)
		r.Start(context.Background(), "talar", automation.Woodcut("oak", 12))
		g.Until(600*20, func() bool {
			return g.Count("minecraft:stone_axe") > 0 && g.Count("minecraft:oak_log") >= 4 || r.Status() == ""
		})
		r.Stop()
		t.Logf("hacha de piedra %d, de hierro %d, troncos %d", g.Count("minecraft:stone_axe"), g.Count("minecraft:iron_axe"), g.Count("minecraft:oak_log"))
		if g.Count("minecraft:stone_axe") == 0 || g.Count("minecraft:iron_axe") > 0 {
			t.Fatalf("quería un hacha de piedra (y no de hierro)")
		}
	})
}

// TestOutOfWater: dentro de una charca de 2 de hondo, sale a la orilla
// (nadando hacia arriba con el salto pulsado) y llega.
func TestOutOfWater(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"fill -3 62 -3 6 63 3 minecraft:water",
		}})
		r := runner(t, g)
		g.Step(20)
		var err error
		r.Start(context.Background(), "ir", func(j *automation.Job) error {
			err = j.GoTo(world.BlockPos{X: 10, Y: 64, Z: 0})
			return err
		})
		g.Until(60*20, func() bool { return r.Status() == "" })
		p := g.B.Player.Position()
		t.Logf("error %v; el bot en %.2f %.2f %.2f", err, p.X, p.Y, p.Z)
		if err != nil || p.X < 9 {
			t.Fatalf("no salió del agua")
		}
	})
}
