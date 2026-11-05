package combat_test

import (
	"context"
	"math"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"

	"minecraft-bot/combat"
	"minecraft-bot/pathing"
)

func TestZoneGeometry(t *testing.T) {
	z := combat.ZoneBetween(10, -5, 2, 3) // esquinas en cualquier orden, bloques incluidos
	for _, c := range []struct {
		x, z float64
		in   bool
	}{{2, -5, true}, {10.9, 3.9, true}, {6, 0, true}, {1.9, 0, false}, {11.1, 0, false}, {6, 4.1, false}} {
		if got := z.Contains(c.x, c.z); got != c.in {
			t.Errorf("(%v, %v) dentro=%v, se esperaba %v (%s)", c.x, c.z, got, c.in, z)
		}
	}
	a := combat.ZoneAround(0, 0, 8)
	if !a.Contains(7.9, -7.9) || a.Contains(8.1, 0) {
		t.Errorf("radio 8: %s", a)
	}
}

// fighter monta el módulo de pelea sobre un bot del servidor falso.
func fighter(t *testing.T, s *fakeserver.Server, name string, opts ...func(*combat.Deps)) (*fakeserver.Bot, *combat.Fighter) {
	b := fakeserver.JoinStepped(t, s, name)
	if err := b.Loaded(3 * time.Second); err != nil {
		t.Fatal(err)
	}
	b.Mover.Food = func() int { return 20 }
	w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
	d := combat.Deps{
		Client: b.C, Player: b.Player, Entities: b.Entities, Screens: b.Screens, World: b.World,
		Hands: b.Hands, Ctl: b.Ctl, Walker: w, Mover: b.Mover,
		NameOf: func(uuid.UUID) string { return "" },
	}
	for _, o := range opts {
		o(&d)
	}
	f := combat.New(d)
	f.Log = t.Logf
	b.Ctl.Think = f.Think
	// el servidor falso no manda la vida: sin ella el módulo cree que está muerto
	if err := s.Player(name).Send(play.SetHealth{Health: 20, Food: 20, Saturation: 5}); err != nil {
		t.Fatal(err)
	}
	return b, f
}

// TestFightIn: con una zona alrededor del punto de aparición, el bot ataca al
// otro jugador que está dentro; con la zona en otro sitio, no.
func TestFightIn(t *testing.T) {
	for _, tc := range []struct {
		name   string
		zone   combat.Zone
		attack bool
	}{
		{"dentro", combat.ZoneAround(0.5, 0.5, 8), true},
		{"fuera", combat.ZoneBetween(30, 30, 40, 40), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := fakeserver.StartMem(t)
				s.MaxPlayers = 2
				b, f := fighter(t, s, "Bot")
				target := fakeserver.JoinStepped(t, s, "Victima")
				if err := target.Loaded(3 * time.Second); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() { _ = f.Run(ctx) }()
				b.Step(5)
				f.SetZone(&tc.zone)
				victim := int32(target.Player.Login.PlayerID)
				hit := func() bool {
					for _, p := range s.Player("Bot").Got(packetid.ServerboundPlayAttack) {
						var a play.Attack
						if (pk.Packet{ID: int32(p.ID), Data: p.Data}).Scan(&a) == nil && int32(a.EntityID) == victim {
							return true
						}
					}
					return false
				}
				got := b.Until(5*20, hit)
				if got != tc.attack {
					t.Fatalf("atacó=%v, se esperaba %v (estado: %s)", got, tc.attack, f.Status())
				}
				if !tc.attack { // sin nadie dentro: va hacia la zona
					if p := b.Player.Position(); p.X < 5 || p.Z < 5 {
						t.Errorf("no fue hacia la zona: está en %.1f %.1f", p.X, p.Z)
					}
				}
				t.Logf("estado: %s, en %.1f %.1f", f.Status(), b.Player.Position().X, b.Player.Position().Z)
			})
		})
	}
}

// TestZoneYieldsToOrder: con otra orden en marcha (un follow, un goto) el bot
// no se va hacia la zona: antes las dos mandaban al walker a la vez.
func TestZoneYieldsToOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		b, f := fighter(t, s, "Bot", func(d *combat.Deps) { d.Ordered = func() bool { return true } })
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = f.Run(ctx) }()
		b.Step(5)
		start := b.Player.Position()
		z := combat.ZoneBetween(30, 30, 40, 40)
		f.SetZone(&z)
		b.Step(3 * 20)
		if p := b.Player.Position(); math.Hypot(p.X-start.X, p.Z-start.Z) > 1 {
			t.Errorf("con otra orden en marcha se fue hacia la zona: de %.1f %.1f a %.1f %.1f", start.X, start.Z, p.X, p.Z)
		}
	})
}
