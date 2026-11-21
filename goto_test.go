package main

import (
	"context"
	"math"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"

	"cubit/combat"
	"cubit/pathing"
)

// gotoBot es un bot con el walker sobre un servidor falso (mundo plano); fill
// prepara el mundo antes de entrar.
func gotoBot(t *testing.T, fill func(*fakeserver.Server)) (*Bot, *fakeserver.Bot) {
	s := fakeserver.StartMem(t)
	if fill != nil {
		fill(s)
	}
	fb := fakeserver.JoinStepped(t, s, "Tester")
	if err := fb.Loaded(3 * time.Second); err != nil {
		t.Fatal(err)
	}
	fb.Mover.Food = func() int { return 20 }
	b := &Bot{Player: fb.Player, Ctl: fb.Ctl}
	b.Walker = pathing.New(fb.World, fb.Player, physics.ClientTags{T: &fb.C.Tags})
	b.Combat = combat.New(combat.Deps{Client: fb.C, Player: fb.Player, Entities: fb.Entities, Walker: b.Walker})
	fb.Ctl.Think = b.Walker.Think
	return b, fb
}

// runGoto lanza gotoCell y espera a que termine; devuelve si llegó y dónde quedó.
func runGoto(t *testing.T, b *Bot, fb *fakeserver.Bot, x, y, z float64) (bool, basic.Position) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan bool, 1)
	go func() { done <- b.gotoCell(ctx, x, y, z, nil) }()
	var ok bool
	finished := func() bool {
		select {
		case ok = <-done:
			return true
		default:
			return false
		}
	}
	if !fb.Until(90*20, finished) {
		t.Fatalf("no terminó; está en %+v", fb.Player.Position())
	}
	return ok, fb.Player.Position()
}

// TestGotoCoords: /goto a 30 bloques con un muro de 3 de alto en medio: llega
// rodeándolo, a la celda exacta (la y de los pies del bot).
func TestGotoCoords(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b, fb := gotoBot(t, func(s *fakeserver.Server) { s.World.Fill(15, 64, -4, 15, 66, 4, "minecraft:stone") })
		y0 := math.Floor(fb.Player.Position().Y)
		ok, p := runGoto(t, b, fb, 30.5, y0, 0.5)
		if !ok {
			t.Fatalf("no llegó; está en %+v", p)
		}
		if d := math.Hypot(p.X-30.5, p.Z-0.5); d > 1.6 || math.Floor(p.Y) != y0 {
			t.Fatalf("terminó lejos del destino (en %.1f %.1f %.1f)", p.X, p.Y, p.Z)
		}
	})
}

// TestGotoYTop: la y es lo alto de una estructura (una torre de 3 bloques):
// el bot sube a ella.
func TestGotoYTop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b, fb := gotoBot(t, func(s *fakeserver.Server) {
			s.World.Fill(6, 64, 0, 9, 66, 3, "minecraft:stone")
			s.World.Fill(3, 64, 0, 3, 64, 3, "minecraft:stone") // escalera de 1, 2 y 3 de alto
			s.World.Fill(4, 64, 0, 4, 65, 3, "minecraft:stone")
			s.World.Fill(5, 64, 0, 5, 66, 3, "minecraft:stone")
		})
		y0 := math.Floor(fb.Player.Position().Y)
		ok, p := runGoto(t, b, fb, 7.5, y0+3, 1.5) // los pies sobre la torre
		if !ok || math.Floor(p.Y) != y0+3 || math.Hypot(p.X-7.5, p.Z-1.5) > 1 {
			t.Fatalf("ok=%v, quedó en %.1f %.1f %.1f (quería y=%.0f)", ok, p.X, p.Y, p.Z, y0+3)
		}
	})
}

// TestGotoYAir: una y en el aire (10 sobre el suelo) o dentro de un bloque: va
// al suelo más cercano de la columna.
func TestGotoYAir(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b, fb := gotoBot(t, func(s *fakeserver.Server) {
			s.World.Fill(6, 64, 0, 9, 66, 3, "minecraft:stone")
			s.World.Fill(3, 64, 0, 3, 64, 3, "minecraft:stone")
			s.World.Fill(4, 64, 0, 4, 65, 3, "minecraft:stone")
			s.World.Fill(5, 64, 0, 5, 66, 3, "minecraft:stone")
		})
		y0 := math.Floor(fb.Player.Position().Y)
		ok, p := runGoto(t, b, fb, 12.5, y0+10, 0.5)
		if !ok || math.Floor(p.Y) != y0 {
			t.Fatalf("en el aire: ok=%v, quedó en %.1f %.1f %.1f (el suelo es y=%.0f)", ok, p.X, p.Y, p.Z, y0)
		}
		// dentro del bloque (la torre llega a y0+2): el suelo más cercano es su cima
		ok, p = runGoto(t, b, fb, 7.5, y0+1, 1.5)
		if !ok || math.Floor(p.Y) != y0+3 {
			t.Fatalf("en un bloque: ok=%v, quedó en %.1f %.1f %.1f (la cima es y=%.0f)", ok, p.X, p.Y, p.Z, y0+3)
		}
	})
}

// TestGotoYNoGround: una columna sin suelo a menos de 24 bloques de esa y: falla sin moverse a ciegas.
func TestGotoYNoGround(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b, fb := gotoBot(t, nil)
		y0 := math.Floor(fb.Player.Position().Y)
		if ok, _ := runGoto(t, b, fb, 5.5, y0+60, 0.5); ok {
			t.Fatal("no debería haber llegado a una altura sin suelo cerca")
		}
	})
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
