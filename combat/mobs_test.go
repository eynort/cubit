package combat_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/registryid"

	"minecraft-bot/combat"
	"minecraft-bot/pathing"
)

// TestMobs: un mob hostil aparece cerca; el bot (con espada y armadura) se
// defiende sin que nadie se lo mande, y sobrevive. Del creeper: golpe y atrás.
func TestMobs(t *testing.T) {
	for _, kind := range []string{"zombie", "skeleton", "creeper"} {
		t.Run(kind, func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				g := simworld.Start(t, simworld.Options{Setup: []string{
					"give Tester iron_sword 1", "give Tester iron_chestplate 1", "give Tester iron_helmet 1",
					"give Tester cooked_beef 8",
				}})
				b := g.B
				w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
				f := combat.New(combat.Deps{
					Client: b.C, Player: b.Player, Entities: b.Entities, Screens: b.Screens, World: b.World,
					Hands: b.Hands, Ctl: b.Ctl, Walker: w, Mover: b.Mover,
					NameOf: func(uuid.UUID) string { return "" },
				})
				f.Log = t.Logf
				b.Ctl.Think = f.Think
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() { _ = f.Run(ctx) }()
				g.Step(5)
				if err := g.SummonNear("minecraft:"+kind, 7, 0); err != nil {
					t.Fatal(err)
				}
				id := g.LastMob()
				dead := g.Until(40*20, func() bool { _, alive := g.MobHealth(id); return !alive })
				t.Logf("%s muerto=%v; vida del bot %.0f", kind, dead, g.Health)
				if g.Health <= 0 {
					t.Fatalf("el bot murió contra un %s", kind)
				}
				if !dead {
					t.Errorf("no acabó con el %s en 40 s", kind)
				}
			})
		})
	}
}

// TestZombieHordeNoFood: tres zombis, espada de piedra, sin armadura y sin
// comida: con poca vida tiene que huir (a un zombi se le deja atrás
// corriendo), no pelear hasta morir.
func TestZombieHordeNoFood(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{"give Tester stone_sword 1"}})
		g.Food = 15 // como en la sesión: con 15 de hambre no se regenera vida
		b := g.B
		w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		f := combat.New(combat.Deps{
			Client: b.C, Player: b.Player, Entities: b.Entities, Screens: b.Screens, World: b.World,
			Hands: b.Hands, Ctl: b.Ctl, Walker: w, Mover: b.Mover,
			NameOf: func(uuid.UUID) string { return "" },
		})
		f.Log = t.Logf
		b.Ctl.Think = f.Think
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = f.Run(ctx) }()
		g.Step(5)
		for _, d := range [][2]float64{{6, 0}, {6, 2}, {6, -2}} {
			if err := g.SummonNear("minecraft:zombie", d[0], d[1]); err != nil {
				t.Fatal(err)
			}
		}
		minHealth := g.Health
		g.Until(60*20, func() bool {
			minHealth = min(minHealth, g.Health)
			return g.Health <= 0
		})
		t.Logf("vida al final %.0f (mínima %.0f)", g.Health, minHealth)
		if g.Health <= 0 {
			t.Fatal("murió peleando contra los zombis en vez de huir")
		}
	})
}

// (Ponerse el equipo que le dan fuera de pelea no se prueba aquí: simworld no
// modela los huecos de armadura ni la mano izquierda. En el servidor de verdad
// el módulo de pelea ya se equipaba así en las peleas.)

// TestUnarmed: sin arma ni comida, un creeper y una araña cerca. A puñetazos
// no se busca pelea: del creeper huye (no explota encima), a la araña solo
// le pega si le ataca, y con poca vida huye aunque la crea casi muerta.
func TestUnarmed(t *testing.T) {
	for _, kind := range []string{"creeper", "zombie"} { // (simworld no tiene arañas)
		t.Run(kind, func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				g := simworld.Start(t, simworld.Options{})
				g.Food = 15
				b := g.B
				w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
				f := combat.New(combat.Deps{
					Client: b.C, Player: b.Player, Entities: b.Entities, Screens: b.Screens, World: b.World,
					Hands: b.Hands, Ctl: b.Ctl, Walker: w, Mover: b.Mover,
					NameOf: func(uuid.UUID) string { return "" },
				})
				f.Log = t.Logf
				b.Ctl.Think = f.Think
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() { _ = f.Run(ctx) }()
				g.Step(5)
				if err := g.SummonNear("minecraft:"+kind, 6, 0); err != nil {
					t.Fatal(err)
				}
				minHealth := g.Health
				g.Until(40*20, func() bool {
					minHealth = min(minHealth, g.Health)
					return g.Health <= 0
				})
				t.Logf("%s: vida al final %.0f (mínima %.0f), explosiones %d", kind, g.Health, minHealth, g.Blasts())
				if g.Health <= 0 {
					t.Fatalf("murió contra un %s sin arma", kind)
				}
			})
		})
	}
}

// TestKeepArmed: la barra rápida llena de tierra y una espada en el
// inventario: libre y sin enemigos, la pasa a la barra y la lleva en la mano.
func TestKeepArmed(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{"give Tester dirt 576", "give Tester wooden_sword 1", "give Tester iron_sword 1"}})
		b := g.B
		w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		f := combat.New(combat.Deps{
			Client: b.C, Player: b.Player, Entities: b.Entities, Screens: b.Screens, World: b.World,
			Hands: b.Hands, Ctl: b.Ctl, Walker: w, Mover: b.Mover,
			NameOf: func(uuid.UUID) string { return "" },
			Idle:   func() bool { return true },
		})
		f.Log = t.Logf
		b.Ctl.Think = f.Think
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = f.Run(ctx) }()
		held := func() string {
			b.Screens.Lock()
			defer b.Screens.Unlock()
			s := b.Screens.Inventory.Slots[36+b.Screens.HeldSlot]
			if s.Count <= 0 {
				return ""
			}
			return registryid.Item[s.Item]
		}
		if !g.Until(10*20, func() bool { return held() == "minecraft:iron_sword" }) {
			t.Fatalf("en la mano: %q, se esperaba la espada de hierro", held())
		}
	})
}

// TestArmedLetsBuild: libre, con espada y tierra, y un camino que sube una
// torre: el arma no le quita la tierra de la mano al constructor (antes se la
// cambiaba cada 2 s y el bloque no se ponía nunca).
func TestArmedLetsBuild(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{"fill 4 64 -3 8 66 3 stone", "give Tester iron_sword 1", "give Tester dirt 16"}})
		b := g.B
		b.Mover.Food = func() int { return 20 }
		w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		w.SetBuilder(&pathing.HandsBuilder{Screens: b.Screens, Hands: b.Hands, Ctl: b.Ctl, Ctx: context.Background()})
		f := combat.New(combat.Deps{
			Client: b.C, Player: b.Player, Entities: b.Entities, Screens: b.Screens, World: b.World,
			Hands: b.Hands, Ctl: b.Ctl, Walker: w, Mover: b.Mover,
			NameOf: func(uuid.UUID) string { return "" },
			Idle:   func() bool { return true },
		})
		f.Log = t.Logf
		b.Ctl.Think = f.Think
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = f.Run(ctx) }()
		g.Step(60) // ya con la espada en la mano
		w.Goto(world.BlockPos{X: 6, Y: 67, Z: 0})
		g.Until(60*20, func() bool { st := w.Status(); return st == "arrived" || strings.HasPrefix(st, "failed") })
		if st := w.Status(); st != "arrived" {
			t.Fatalf("no subió la torre: %s (en %+v)", st, b.Player.Position())
		}
	})
}
