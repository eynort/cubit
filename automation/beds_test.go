package automation_test

import (
	"context"
	"math"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/automation"
	"minecraft-bot/pathing"
	"minecraft-bot/safety"
)

// safeRunner es runner con el reflejo de la lava (y el fuego) delante del walker.
func safeRunner(t *testing.T, g *simworld.Game) (*automation.Runner, *safety.Lava) {
	b := g.B
	b.Mover.Food = func() int { return 20 }
	w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
	bd := &pathing.HandsBuilder{Screens: b.Screens, Hands: b.Hands, Ctl: b.Ctl, Ctx: context.Background()}
	w.SetBuilder(bd)
	lava := &safety.Lava{World: b.World, Mover: b.Mover, Stop: w.Stop, Logf: t.Logf,
		Extinguish: func(p world.BlockPos) { b.Hands.Dig(p, nil) }}
	b.Ctl.Think = func(s *control.State, bp *basic.Player) {
		if lava.Think(s, bp) {
			return
		}
		w.Think(s, bp)
	}
	return automation.New(automation.Deps{
		Client: b.C, Player: b.Player, World: b.World, Entities: b.Entities, Screens: b.Screens,
		Hands: b.Hands, Ctl: b.Ctl, Walker: w, Builder: bd, Fighting: func() bool { return false },
		HasFood: func() bool { return true }, Escaping: lava.Escaping,
	}), lava
}

// TestBedMining: un bloque de netherrack bajo el bot con escombros dentro; las
// camas explotan (como en el Nether): baja, abre pasillo, pone la cama en la
// postura escondida, la pulsa y recoge lo que el cráter destapa. La explosión
// es la del juego (rayos, lo que se ve de la caja, el daño): medio corazón
// como mucho por disparo, y del fuego sale antes de prender.
func TestBedMining(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester diamond_pickaxe 1", "give Tester white_bed 6", "give Tester netherrack 64",
			"fill -12 46 -14 40 63 14 minecraft:netherrack",
			"setblock 10 57 2 minecraft:ancient_debris", "setblock 11 56 -2 minecraft:ancient_debris",
			"setblock 19 57 1 minecraft:ancient_debris", "setblock 26 56 -1 minecraft:ancient_debris",
		}})
		g.BedsExplode = true
		r, lava := safeRunner(t, g)
		g.Step(5)
		var err error
		minHealth := float32(20)
		r.Start(context.Background(), "camas", func(j *automation.Job) error {
			if err = j.DescendTo(57); err != nil {
				return err
			}
			have := func() int { return j.Count("ancient_debris") }
			err = j.BedMine(have, 4)
			return err
		})
		g.Until(1500*20, func() bool {
			minHealth = min(minHealth, g.Health)
			return r.Status() == ""
		})
		p := g.B.Player.Position()
		t.Logf("error %v; bot en %.1f %.1f %.1f; vida mínima %.1f; escombros %d; camas %d; explosiones %d; quemado %d veces; huidas %d",
			err, p.X, p.Y, p.Z, minHealth, g.Count("minecraft:ancient_debris"), g.Count("minecraft:white_bed"), g.Blasts(), g.FireHurts, lava.Escapes())
		if g.Blasts() == 0 {
			t.Fatalf("no explotó ninguna cama")
		}
		if minHealth < 20-float32(g.Blasts())-3 {
			t.Errorf("demasiado daño: vida mínima %.1f con %d explosiones", minHealth, g.Blasts())
		}
		if g.BurnedLong() > 0 {
			t.Errorf("se quedó en el fuego hasta prenderse (%d veces)", g.BurnedLong())
		}
		if g.Count("minecraft:ancient_debris") == 0 {
			t.Errorf("no sacó escombros")
		}
	})
}

// TestLavaEscape: el bot quieto con lava al lado: se aparta; metido en lava: sale.
func TestLavaEscape(t *testing.T) {
	for _, tc := range []struct{ name, line string }{
		{"al lado", "setblock 1 64 0 minecraft:lava"},
		{"dentro", "setblock 0 64 0 minecraft:lava"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				g := simworld.Start(t, simworld.Options{Setup: []string{
					"fill -6 63 -6 6 63 6 minecraft:stone", tc.line,
				}})
				_, lava := safeRunner(t, g)
				g.Step(60)
				p := g.B.Player.Position()
				t.Logf("bot en %.2f %.2f %.2f; huidas %d", p.X, p.Y, p.Z, lava.Escapes())
				if lava.Escapes() == 0 {
					t.Fatalf("no reaccionó a la lava")
				}
				if math.Floor(p.X) == 0 && math.Floor(p.Z) == 0 || math.Hypot(p.X-1.5, p.Z-0.5) < 1.2 {
					t.Errorf("sigue en la lava o pegado a ella")
				}
			})
		})
	}
}

// TestShearForBeds: tijeras con 2 lingotes, esquila las ovejas de cerca y
// hace camas con la lana (y la de las que vuelven a crecer, rápido aquí).
func TestShearForBeds(t *testing.T) {
	simworld.FastWool = true
	defer func() { simworld.FastWool = false }()
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester iron_ingot 2", "give Tester oak_log 12", "give Tester crafting_table 1",
		}})
		r, _ := safeRunner(t, g)
		g.Step(5)
		for _, d := range [][2]float64{{6, 3}, {-5, 4}} {
			if err := g.SummonNear("minecraft:sheep", d[0], d[1]); err != nil {
				t.Fatal(err)
			}
		}
		var err error
		r.Start(context.Background(), "camas", func(j *automation.Job) error {
			err = j.EnsureBeds(2)
			return err
		})
		g.Until(600*20, func() bool { return r.Status() == "" })
		t.Logf("error %v; camas %d; lana %d; tijeras %d", err, g.Count("minecraft:white_bed"), g.Count("minecraft:white_wool"), g.Count("minecraft:shears"))
		if g.Count("minecraft:white_bed") < 2 {
			t.Fatalf("no hizo las camas")
		}
	})
}

// TestPenAndLure: hace el corral (vallas y puerta) y mete dos ovejas con trigo.
func TestPenAndLure(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester oak_log 24", "give Tester wheat 6", "give Tester crafting_table 1",
		}})
		r, _ := safeRunner(t, g)
		g.Step(5)
		for _, d := range [][2]float64{{-10, -3}, {-11, -1}} {
			if err := g.SummonNear("minecraft:sheep", d[0], d[1]); err != nil {
				t.Fatal(err)
			}
		}
		var err error
		in := 0
		r.Start(context.Background(), "granja", func(j *automation.Job) error {
			pen, e := j.EnsurePen()
			if e != nil {
				err = e
				return e
			}
			err = j.LureSheep(pen, 2)
			in = j.SheepInPen(pen)
			return err
		})
		g.Until(900*20, func() bool { return r.Status() == "" })
		t.Logf("error %v; ovejas dentro %d; %s", err, in, sheepWhere(g))
		if in < 2 {
			t.Fatalf("no metió las ovejas en el corral")
		}
	})
}

func sheepWhere(g *simworld.Game) string {
	s := ""
	for _, sh := range g.Sheep() {
		s += sprintf("[%.1f %.1f %.1f] ", sh.X, sh.Y, sh.Z)
	}
	return s
}

// TestWheatField: azada de madera, ara la tierra junto a la base y siembra.
func TestWheatField(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester oak_log 4", "give Tester wheat_seeds 8", "give Tester crafting_table 1",
		}})
		r, _ := safeRunner(t, g)
		g.Step(5)
		var err error
		sown := 0
		r.Start(context.Background(), "granja", func(j *automation.Job) error {
			_, err = j.TendField()
			f := j.Feet()
			for dx := -9; dx <= 9; dx++ {
				for dz := -9; dz <= 9; dz++ {
					if j.Block(world.BlockPos{X: f.X + dx, Y: 64, Z: f.Z + dz}) == "wheat" {
						sown++
					}
				}
			}
			return err
		})
		g.Until(600*20, func() bool { return r.Status() == "" })
		t.Logf("error %v; sembradas %d; semillas %d", err, sown, g.Count("minecraft:wheat_seeds"))
		if sown < 6 {
			t.Fatalf("sembró %d", sown)
		}
	})
}

// TestGoldBoots: con 4 lingotes de oro hace unas botas y se las pone (los piglins).
func TestGoldBoots(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester gold_ingot 4", "give Tester crafting_table 1", "give Tester oak_log 2",
		}})
		r, _ := safeRunner(t, g)
		g.Step(5)
		var err error
		wearing := false
		r.Start(context.Background(), "oro", func(j *automation.Job) error {
			err = j.EnsureGoldArmor()
			wearing = j.WearingGold()
			return err
		})
		g.Until(120*20, func() bool { return r.Status() == "" })
		t.Logf("error %v; oro puesto %v", err, wearing)
		if !wearing {
			t.Fatal("no lleva oro puesto")
		}
	})
}

// TestLavaEdgeIsCalm: junto al borde quieto de un charco de lava (nivel 6, ya
// no avanza) se puede estar: el reflejo no salta (antes saltaba sin parar).
func TestLavaEdgeIsCalm(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"fill -6 63 -6 6 63 6 minecraft:stone", "setblock 1 64 0 minecraft:lava[level=6]",
		}})
		_, lava := safeRunner(t, g)
		g.Step(60)
		if lava.Escapes() != 0 {
			t.Fatalf("saltó %d veces junto a lava que no avanza", lava.Escapes())
		}
	})
}
