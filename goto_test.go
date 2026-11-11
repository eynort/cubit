package main

import (
	"context"
	"math"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"

	"minecraft-bot/combat"
	"minecraft-bot/pathing"
)

// TestGotoCoords: /goto a 30 bloques con un muro de 3 de alto en medio: llega rodeándolo.
func TestGotoCoords(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		s.World.Fill(15, 64, -4, 15, 66, 4, "minecraft:stone")
		fb := fakeserver.JoinStepped(t, s, "Tester")
		if err := fb.Loaded(3 * time.Second); err != nil {
			t.Fatal(err)
		}
		fb.Mover.Food = func() int { return 20 }
		b := &Bot{Player: fb.Player, Ctl: fb.Ctl}
		b.Walker = pathing.New(fb.World, fb.Player, physics.ClientTags{T: &fb.C.Tags})
		b.Combat = combat.New(combat.Deps{Client: fb.C, Player: fb.Player, Entities: fb.Entities, Walker: b.Walker})
		fb.Ctl.Think = b.Walker.Think
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan struct{})
		go func() { b.gotoCoords(ctx, 30.5, 0.5, nil); close(done) }()
		finished := func() bool {
			select {
			case <-done:
				return true
			default:
				return false
			}
		}
		if !fb.Until(60*20, finished) {
			t.Fatalf("no terminó; está en %+v", fb.Player.Position())
		}
		p := fb.Player.Position()
		if d := math.Hypot(p.X-30.5, p.Z-0.5); d > 1.6 {
			t.Fatalf("terminó a %.1f bloques del destino (en %.1f %.1f %.1f)", d, p.X, p.Y, p.Z)
		}
	})
}

func TestParseCoords(t *testing.T) {
	for _, c := range []struct {
		in   string
		x, z float64
		ok   bool
	}{
		{"120 -450", 120, -450, true},
		{"120 64 -450", 120, -450, true},
		{"x: 12.5, y: 70, z: -3", 12.5, -3, true},
		{"hola", 0, 0, false},
		{"tengo 3 manzanas", 0, 0, false},
	} {
		x, z, ok := parseCoords(c.in)
		if ok != c.ok || ok && (x != c.x || z != c.z) {
			t.Errorf("parseCoords(%q) = %v %v %v", c.in, x, z, ok)
		}
	}
}

// TestGotoPlayerFar: el jugador no está a la vista. El bot va a donde lo vio
// por última vez; al no encontrarlo allí, se lo pregunta por /msg; con las
// coordenadas que contesta, va hasta ellas.
func TestGotoPlayerFar(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		s.MaxPlayers = 2
		fb := fakeserver.JoinStepped(t, s, "Tester")
		if err := fb.Loaded(3 * time.Second); err != nil {
			t.Fatal(err)
		}
		fb.Mover.Food = func() int { return 20 }
		b := &Bot{Player: fb.Player, Ctl: fb.Ctl, Entities: fb.Entities, Client: fb.C}
		b.Walker = pathing.New(fb.World, fb.Player, physics.ClientTags{T: &fb.C.Tags})
		b.Combat = combat.New(combat.Deps{Client: fb.C, Player: fb.Player, Entities: fb.Entities, Walker: b.Walker})
		fb.Ctl.Think = b.Walker.Think
		b.watchSeen()

		far := fakeserver.JoinStepped(t, s, "Lejos")
		if err := far.Loaded(3 * time.Second); err != nil {
			t.Fatal(err)
		}
		fb.Step(5)
		e, ok := fb.Entities.Get(int32(far.Player.Login.PlayerID))
		if !ok {
			t.Fatal("no ve a Lejos")
		}
		uid := e.UUID
		online := []string{"Lejos"}
		onlineNames.Store(&online)
		// que deje de verlo (como si se hubiera alejado), visto por última vez en 25 5
		var ids types.List[pk.VarInt, *pk.VarInt]
		ids = append(ids, pk.VarInt(far.Player.Login.PlayerID))
		if err := s.Player("Tester").Send(play.RemoveEntities{EntityIds: ids}); err != nil {
			t.Fatal(err)
		}
		fb.Step(3)
		travel.Lock()
		travel.seen[uid] = seenPos{x: 25.5, y: 64, z: 5.5, at: time.Now()}
		travel.Unlock()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go b.gotoPlayer(ctx, "Lejos", uid)

		near := func(x, z float64) func() bool {
			return func() bool { p := fb.Player.Position(); return math.Hypot(p.X-x, p.Z-z) < 2 }
		}
		if !fb.Until(40*20, near(25.5, 5.5)) {
			t.Fatalf("no fue a donde lo vio: está en %+v", fb.Player.Position())
		}
		asked := func() bool {
			for _, p := range s.Player("Tester").Got(packetid.ServerboundPlayChatCommand) {
				var c play.ChatCommand
				if (pk.Packet{ID: int32(p.ID), Data: p.Data}).Scan(&c) == nil && strings.HasPrefix(string(c.Command), "msg Lejos") {
					return true
				}
			}
			return false
		}
		if !fb.Until(10*20, asked) {
			t.Fatal("no le preguntó dónde está")
		}
		noteCoords(uid, "estoy en 30 64 -10") // su respuesta
		if !fb.Until(40*20, near(30.5, -9.5)) {
			t.Fatalf("no fue a las coordenadas que le dio: está en %+v", fb.Player.Position())
		}
	})
}

func TestSystemCoords(t *testing.T) {
	travel.Lock()
	travel.coords = map[uuid.UUID]coordsMsg{}
	id := uuid.New()
	travel.asked = map[string]uuid.UUID{"alex": id}
	travel.Unlock()
	t0 := time.Now().Add(-time.Second)
	noteSystemCoords("[Alex -> yo] 120 64 -450")
	if c, ok := coordsSince(id, t0); !ok || c.x != 120 || c.z != -450 {
		t.Fatalf("privado de un plugin: %+v %v", c, ok)
	}
	noteSystemCoords("[12:30] Otro: 1 2") // de otro: no cuenta
	if c, _ := coordsSince(id, t0); c.x != 120 {
		t.Fatalf("tomó las de otro: %+v", c)
	}
}
