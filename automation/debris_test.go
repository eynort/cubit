package automation_test

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/automation"
	"minecraft-bot/pathing"
)

func runner(t *testing.T, g *simworld.Game) *automation.Runner {
	b := g.B
	b.Mover.Food = func() int { return 20 }
	w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
	bd := &pathing.HandsBuilder{Screens: b.Screens, Hands: b.Hands, Ctl: b.Ctl, Ctx: context.Background()}
	w.SetBuilder(bd)
	b.Ctl.Think = w.Think
	return automation.New(automation.Deps{
		Client: b.C, Player: b.Player, World: b.World, Entities: b.Entities, Screens: b.Screens,
		Hands: b.Hands, Ctl: b.Ctl, Walker: w, Builder: bd, Fighting: func() bool { return false },
		HasFood: func() bool { return true },
	})
}

// TestDiamondPickaxe: con un pico de hierro y madera, baja cavando hasta los
// diamantes (a -50), saca 3, sube por su túnel y se hace el pico de diamante.
func TestDiamondPickaxe(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester iron_pickaxe 1", "give Tester oak_log 8",
			"fill 6 -52 3 8 -52 3 minecraft:diamond_ore",
		}})
		r := runner(t, g)
		g.Step(5)
		var err error
		r.Start(context.Background(), "pico", func(j *automation.Job) error {
			err = j.EnsureDiamondPickaxe()
			return err
		})
		g.Until(1500*20, func() bool { return r.Status() == "" })
		p := g.B.Player.Position()
		t.Logf("error %v; el bot en %.1f %.1f %.1f; pico de diamante %d", err, p.X, p.Y, p.Z, g.Count("minecraft:diamond_pickaxe"))
		if err != nil || g.Count("minecraft:diamond_pickaxe") == 0 {
			t.Fatalf("no hay pico de diamante")
		}
		if p.Y < 60 {
			t.Errorf("se quedó abajo (y=%.1f)", p.Y)
		}
	})
}

// TestStripMine: con pico de diamante, a la altura 40, un escombro ancestral
// lejos (fuera de lo que mira al principio): galerías hasta verlo y sacarlo.
func TestStripMine(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester diamond_pickaxe 1", "give Tester cobblestone 32",
			"fill 40 40 1 40 40 1 minecraft:ancient_debris",
		}})
		r := runner(t, g)
		g.Step(5)
		var err error
		r.Start(context.Background(), "escombros", func(j *automation.Job) error {
			have := func() int { return j.Count("ancient_debris") }
			err = j.MineAt("escombros", func(n string) bool { return n == "ancient_debris" }, have, 1, 40, 10)
			return err
		})
		g.Until(1500*20, func() bool { return r.Status() == "" })
		p := g.B.Player.Position()
		t.Logf("error %v; el bot en %.1f %.1f %.1f; escombros %d", err, p.X, p.Y, p.Z, g.Count("minecraft:ancient_debris"))
		if g.Count("minecraft:ancient_debris") == 0 {
			t.Fatalf("no sacó el escombro")
		}
	})
}

// TestBuildPortal: con 10 de obsidiana, un mechero y piedra, levanta el
// marco del portal (simworld no lo enciende: se mira el marco).
func TestBuildPortal(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester obsidian 10", "give Tester flint_and_steel 1", "give Tester cobblestone 8",
		}})
		r := runner(t, g)
		g.Step(5)
		var err error
		r.Start(context.Background(), "portal", func(j *automation.Job) error {
			_, err = j.BuildPortal()
			return err
		})
		g.Until(120*20, func() bool { return r.Status() == "" })
		obs := 0
		for x := -12; x <= 12; x++ {
			for z := -12; z <= 12; z++ {
				for y := 63; y <= 70; y++ {
					if st, ok := g.B.World.BlockAt(world.BlockPos{X: x, Y: y, Z: z}); ok && strings.Contains(world.StateString(st), "obsidian") {
						obs++
					}
				}
			}
		}
		t.Logf("error %v; obsidiana puesta %d", err, obs)
		if obs != 10 {
			t.Errorf("el marco tiene %d de obsidiana, no 10", obs)
		}
	})
}

// TestToSky: bajo un techo de piedra (5 de grueso, paredes alrededor), sale
// cavando una escalera hasta tener el cielo encima.
func TestToSky(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester stone_pickaxe 1", "give Tester cobblestone 16",
			"fill -8 64 -8 8 71 8 minecraft:stone", "fill 0 64 0 0 65 0 minecraft:air",
		}})
		r := runner(t, g)
		g.Step(5)
		var err error
		under, after := false, true
		r.Start(context.Background(), "subir", func(j *automation.Job) error {
			under = j.Underground()
			err = j.ToSky()
			after = j.Underground()
			return err
		})
		g.Until(300*20, func() bool { return r.Status() == "" })
		p := g.B.Player.Position()
		t.Logf("bajo tierra al empezar %v; error %v; el bot en %.1f %.1f %.1f", under, err, p.X, p.Y, p.Z)
		if !under || err != nil || after || p.Y < 69 {
			t.Fatalf("no salió a la superficie")
		}
	})
}

// TestDebrisNextToLava: un escombro con lava al lado y encima. Antes se
// dejaba (abrirlo era dejar entrar la lava); ahora tapa la lava antes de
// picarlo y lo saca sin quemarse.
func TestDebrisNextToLava(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester diamond_pickaxe 1", "give Tester netherrack 32",
			"fill 6 40 1 6 40 1 minecraft:ancient_debris",
			"setblock 7 40 1 minecraft:lava", "setblock 6 41 1 minecraft:lava", "setblock 6 40 2 minecraft:lava",
		}})
		r := runner(t, g)
		g.Step(5)
		var err error
		r.Start(context.Background(), "escombros", func(j *automation.Job) error {
			have := func() int { return j.Count("ancient_debris") }
			err = j.MineAt("escombros", func(n string) bool { return n == "ancient_debris" }, have, 1, 40, 4)
			return err
		})
		g.Until(1500*20, func() bool { return r.Status() == "" })
		p := g.B.Player.Position()
		t.Logf("error %v; el bot en %.1f %.1f %.1f; escombros %d; muertes %d", err, p.X, p.Y, p.Z, g.Count("minecraft:ancient_debris"), g.Deaths())
		if g.Count("minecraft:ancient_debris") == 0 {
			t.Fatalf("no sacó el escombro junto a la lava")
		}
		for _, c := range [][3]int{{7, 40, 1}, {6, 41, 1}, {6, 40, 2}} {
			st, _ := g.B.World.BlockAt(world.BlockPos{X: c[0], Y: c[1], Z: c[2]})
			if s := world.StateString(st); strings.Contains(s, "lava") {
				t.Errorf("la lava de %v sigue sin tapar (%s)", c, s)
			}
		}
		if g.Deaths() > 0 || g.OnFire() > 0 {
			t.Errorf("se quemó: muertes %d, fuego %d", g.Deaths(), g.OnFire())
		}
	})
}

// TestDebrisVein: una veta de tres escombros juntos, con lava pegada a dos de
// ellos: los saca todos (nada se queda en un hueco cerrado por la lava tapada).
func TestDebrisVein(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester diamond_pickaxe 1", "give Tester netherrack 32",
			"fill 6 40 1 7 40 1 minecraft:ancient_debris", "setblock 6 40 2 minecraft:ancient_debris",
			"setblock 8 40 1 minecraft:lava", "setblock 6 41 2 minecraft:lava", "setblock 7 41 1 minecraft:lava",
		}})
		r := runner(t, g)
		g.Step(5)
		var err error
		r.Start(context.Background(), "escombros", func(j *automation.Job) error {
			have := func() int { return j.Count("ancient_debris") }
			err = j.MineAt("escombros", func(n string) bool { return n == "ancient_debris" }, have, 3, 40, 4)
			return err
		})
		g.Until(1500*20, func() bool { return r.Status() == "" })
		t.Logf("error %v; escombros %d; muertes %d", err, g.Count("minecraft:ancient_debris"), g.Deaths())
		if n := g.Count("minecraft:ancient_debris"); n != 3 {
			t.Fatalf("sacó %d de 3", n)
		}
		if g.Deaths() > 0 {
			t.Errorf("murió")
		}
	})
}

// TestJunkPit: con el inventario lleno de basura de cavar, la tira a un hoyo
// junto al túnel y lo tapa; se queda con 3 montones de netherrack para la
// lava y no vuelve a recoger lo tirado (sigue minando y saca el escombro).
func TestJunkPit(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		setup := []string{"give Tester diamond_pickaxe 1", "fill 6 40 1 6 40 1 minecraft:ancient_debris"}
		for item, stacks := range map[string]int{"netherrack": 6, "soul_sand": 8, "cobblestone": 10, "gravel": 9} {
			for k := 0; k < stacks; k++ {
				setup = append(setup, "give Tester "+item+" 64")
			}
		}
		g := simworld.Start(t, simworld.Options{Setup: setup})
		r := runner(t, g)
		g.Step(5)
		var err error
		r.Start(context.Background(), "escombros", func(j *automation.Job) error {
			j.SetJunkPit(true)
			have := func() int { return j.Count("ancient_debris") }
			err = j.MineAt("escombros", func(n string) bool { return n == "ancient_debris" }, have, 1, 40, 4)
			return err
		})
		g.Until(1500*20, func() bool { return r.Status() == "" })
		t.Logf("error %v; netherrack %d, soul_sand %d, cobblestone %d, gravel %d; escombros %d; en el suelo: %s",
			err, g.Count("minecraft:netherrack"), g.Count("minecraft:soul_sand"), g.Count("minecraft:cobblestone"),
			g.Count("minecraft:gravel"), g.Count("minecraft:ancient_debris"), g.ItemsAt())
		if g.Count("minecraft:ancient_debris") != 1 {
			t.Errorf("no sacó el escombro")
		}
		if n := g.Count("minecraft:netherrack"); n < 3*64-16 || n > 4*64 {
			t.Errorf("netherrack: %d, se esperaban unos 3 montones", n)
		}
		for _, it := range []string{"soul_sand", "gravel"} {
			if n := g.Count("minecraft:" + it); n > 0 {
				t.Errorf("%s: recogió o se quedó %d", it, n)
			}
		}
		if n := g.Count("minecraft:cobblestone"); n > 64 { // (lo de cavar piedra después sí)
			t.Errorf("cobblestone: %d, recogió lo tirado", n)
		}
		// lo tirado quedó en hoyos tapados (un bloque macizo encima), no por ahí
		p := g.B.Player.Position()
		for _, e := range g.B.Entities.Nearby(p.X, p.Y, p.Z, 64) {
			if e.Type != "minecraft:item" {
				continue
			}
			above := world.BlockPos{X: int(math.Floor(e.X)), Y: int(math.Floor(e.Y)) + 1, Z: int(math.Floor(e.Z))}
			if st, _ := g.B.World.BlockAt(above); world.StateString(st) == "minecraft:air" {
				t.Errorf("objeto fuera de un hoyo tapado en %.1f %.1f %.1f", e.X, e.Y, e.Z)
			}
		}
	})
}

// TestGravelOverhead: bajo tierra, con grava encima de la columna del bot,
// un túnel hacia arriba. Antes cavaba justo encima de la cabeza, la grava le
// caía encima y se asfixiaba (en el Nether, de 20 a 12 de vida); ahora da un
// paso a un lado y sube desde allí.
func TestGravelOverhead(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester diamond_pickaxe 1", "give Tester netherrack 32",
			"fill -8 64 -8 8 72 8 minecraft:stone", "fill 0 64 0 0 65 0 minecraft:air",
			"fill 0 67 0 0 70 0 minecraft:gravel",
			"setblock 6 70 0 minecraft:ancient_debris", "setblock 7 70 0 minecraft:air", // (un hueco al lado: se puede picar)
		}})
		r := runner(t, g)
		g.Step(5)
		var err error
		r.Start(context.Background(), "subir", func(j *automation.Job) error {
			err = j.TunnelTo(world.BlockPos{X: 6, Y: 70, Z: 0})
			return err
		})
		g.Until(300*20, func() bool { return r.Status() == "" })
		p := g.B.Player.Position()
		t.Logf("error %v; el bot en %.1f %.1f %.1f; vida %.1f; asfixias %d", err, p.X, p.Y, p.Z, g.B.Player.Status().Health, g.Suffocations())
		if g.Suffocations() > 0 {
			t.Errorf("se enterró en grava: %d golpes de asfixia", g.Suffocations())
		}
		if err != nil {
			t.Errorf("no llegó: %v", err)
		}
	})
}

// TestGravelNextToLava: el túnel pasa bajo grava que tiene lava al lado, más
// arriba. Picar debajo deja caer la grava y la lava baja por el hueco (así
// murió en el Nether). Ahora tapa esa lava antes de picar.
func TestGravelNextToLava(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester diamond_pickaxe 1", "give Tester netherrack 32",
			"fill -8 64 -8 8 72 8 minecraft:stone", "fill 0 64 0 0 65 0 minecraft:air",
			"fill 1 66 0 1 67 0 minecraft:gravel", "setblock 2 67 0 minecraft:lava",
			"setblock 6 64 0 minecraft:ancient_debris", "setblock 7 64 0 minecraft:air",
		}})
		r := runner(t, g)
		g.Step(5)
		var err error
		r.Start(context.Background(), "túnel", func(j *automation.Job) error {
			err = j.TunnelTo(world.BlockPos{X: 6, Y: 64, Z: 0})
			return err
		})
		// en cada tick: la grava junto a la lava no se mueve mientras la lava siga
		// ahí (en el juego, en ese momento la lava entra; aquí no corre)
		raced := false
		at := func(x, y, z int) string {
			return world.StateString(g.S.World.At(x, y, z)) // (el del servidor: lo de verdad)
		}
		g.Until(300*20, func() bool {
			if at(1, 67, 0) != "minecraft:gravel" && strings.Contains(at(2, 67, 0), "lava") {
				raced = true
			}
			return r.Status() == ""
		})
		t.Logf("error %v; en 2 67 0: %s; muertes %d", err, at(2, 67, 0), g.Deaths())
		if raced {
			t.Errorf("soltó la grava con la lava al lado sin tapar")
		}
		if g.Deaths() > 0 {
			t.Errorf("murió")
		}
	})
}
