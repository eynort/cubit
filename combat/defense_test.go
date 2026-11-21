package combat_test

import (
	"context"
	"fmt"
	"log"
	"testing"

	"github.com/google/uuid"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"cubit/combat"
	"cubit/pathing"
	"cubit/safety"
)

// simFighterSafe es simFighter con los reflejos del bot de verdad (main.go):
// la lava, la caída y el fuego mandan antes que la pelea.
func simFighterSafe(t *testing.T, lg *logged, setup ...string) (*simworld.Game, *combat.Fighter) {
	g := simworld.Start(t, simworld.Options{Setup: setup, Lag: lag})
	b := g.B
	w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
	builder := &pathing.HandsBuilder{Screens: b.Screens, Hands: b.Hands, Ctl: b.Ctl, Ctx: context.Background()}
	w.SetBuilder(builder)
	logf := func(tag string) func(string, ...any) {
		return func(format string, args ...any) {
			s := fmt.Sprintf(tag+format, args...)
			t.Log(s)
			lg.add(s)
		}
	}
	f := combat.New(combat.Deps{
		Client: b.C, Player: b.Player, Entities: b.Entities, Screens: b.Screens, World: b.World,
		Hands: b.Hands, Ctl: b.Ctl, Walker: w, Mover: b.Mover, Builder: builder,
		NameOf: func(uuid.UUID) string { return "" },
	})
	f.Log = logf("")
	fire := &safety.Fire{Client: b.C, Player: b.Player, World: b.World, Mover: b.Mover, Screens: b.Screens, Hands: b.Hands, Logf: logf("[fuego] ")}
	fire.Register()
	fall := &safety.Fall{World: b.World, Mover: b.Mover, Screens: b.Screens, Hands: b.Hands, Logf: logf("[caída] ")}
	lava := &safety.Lava{World: b.World, Mover: b.Mover, Stop: w.Stop, Logf: logf("[lava] "),
		Extinguish: func(p world.BlockPos) { b.Hands.Dig(p, nil) }}
	lava.Scoop = f.ScoopLava
	f.Reflex = func() bool { return fire.Active() || fall.Active() }
	fire.BeforeUse = f.ReleaseHands
	f.Burning = fire.Burning
	b.Ctl.Think = func(s *control.State, bp *basic.Player) {
		if lava.Think(s, bp) || fall.Think(s, bp) || fire.Think(s, bp) {
			return
		}
		f.Think(s, bp)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = f.Run(ctx) }()
	_ = log.Printf
	return g, f
}

// dirtyFight: el bot con lo de setup contra un rival con trucos a dx.
func dirtyFight(t *testing.T, setup []string, dx float64, k simworld.Kit) (*simworld.Game, *logged, int32) {
	lg := &logged{}
	g, f := simFighterSafe(t, lg, setup...)
	g.Step(3 * 20)
	if err := g.SummonDuelistKit(dx, 0, k); err != nil {
		t.Fatal(err)
	}
	id := g.LastMob()
	g.Step(lag + 2)
	f.Engage(id, "rival")
	return g, lg, id
}

var defenseKit = []string{"give Tester iron_sword 1", "give Tester shield 1", "give Tester cooked_beef 16",
	"give Tester iron_helmet 1", "give Tester iron_chestplate 1", "give Tester iron_leggings 1", "give Tester iron_boots 1",
	"give Tester water_bucket 1", "give Tester bucket 1", "give Tester cobblestone 64", "give Tester iron_pickaxe 1"}

// TestDefendMace: un rival que salta con carga de viento y cae con la maza.
func TestDefendMace(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, _, id := dirtyFight(t, defenseKit, 6, simworld.Kit{Weapon: "minecraft:mace", Tricks: simworld.Tricks{Mace: true}})
		dead := g.Until(40*20, func() bool { return mobHP(g, id) <= 0 || g.Health <= 0 })
		d := g.Dirty()
		t.Logf("RESULT maza: rival muerto=%v; mazazos %d (parados %d); vida del bot %.1f", dead && mobHP(g, id) <= 0, d.Smashes, d.SmashBlocked, g.Health)
		if g.Health <= 0 {
			t.Error("le mataron a mazazos")
		}
		if d.Smashes-d.SmashBlocked > 1 {
			t.Errorf("le entraron %d mazazos", d.Smashes-d.SmashBlocked)
		}
	})
}

// TestDefendLava: un rival que le echa lava a los pies.
func TestDefendLava(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, _, id := dirtyFight(t, defenseKit, 4, simworld.Kit{Tricks: simworld.Tricks{Lava: true}})
		g.Until(40*20, func() bool { return mobHP(g, id) <= 0 || g.Health <= 0 })
		t.Logf("RESULT lava: rival muerto=%v; lava echada %d, ticks en la lava %d; vida del bot %.1f", mobHP(g, id) <= 0, g.Dirty().LavaPoured, g.LavaHurts(), g.Health)
		if g.Health <= 0 {
			t.Error("murió con la lava")
		}
		if g.LavaHurts() > 10 { // (la ida y vuelta al servidor: unos 8 ticks)
			t.Errorf("se quedó en la lava (%d veces quemado)", g.LavaHurts())
		}
	})
}

// TestDefendTrap: un rival que le encierra con bloques y le echa lava dentro.
func TestDefendTrap(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, lg, id := dirtyFight(t, defenseKit, 4, simworld.Kit{Tricks: simworld.Tricks{Trap: true}})
		g.Until(40*20, func() bool { return mobHP(g, id) <= 0 || g.Health <= 0 })
		t.Logf("RESULT trampa: rival muerto=%v; encerrado %d, ticks en la lava %d; vida del bot %.1f", mobHP(g, id) <= 0, g.Dirty().Trapped, g.LavaHurts(), g.Health)
		if g.Health <= 0 {
			t.Error("murió encerrado")
		}
		if g.LavaHurts() > 10 { // (la ida y vuelta al servidor: unos 8 ticks)
			t.Errorf("se quedó en la lava de la trampa (%d)", g.LavaHurts())
		}
		_ = lg
	})
}
