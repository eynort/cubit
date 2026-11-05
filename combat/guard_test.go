package combat_test

import (
	"context"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// attacked dice si el bot mandó un ataque contra la entidad id.
func attacked(s *fakeserver.Server, bot string, id int32) bool {
	for _, p := range s.Player(bot).Got(packetid.ServerboundPlayAttack) {
		var a play.Attack
		if (pk.Packet{ID: int32(p.ID), Data: p.Data}).Scan(&a) == nil && int32(a.EntityID) == id {
			return true
		}
	}
	return false
}

// TestGuard: el bot protege a "Jefe". Si "Malo" le pega a Jefe, el bot va a
// por Malo; si Jefe le pega al bot sin querer, el bot no se vuelve contra él.
func TestGuard(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		s.MaxPlayers = 3
		b, f := fighter(t, s, "Bot")
		boss := fakeserver.JoinStepped(t, s, "Jefe")
		bad := fakeserver.JoinStepped(t, s, "Malo")
		for _, x := range []*fakeserver.Bot{boss, bad} {
			if err := x.Loaded(3 * time.Second); err != nil {
				t.Fatal(err)
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = f.Run(ctx) }()
		b.Step(5)
		bossID, badID := int32(boss.Player.Login.PlayerID), int32(bad.Player.Login.PlayerID)
		e, ok := b.Entities.Get(bossID)
		if !ok {
			t.Fatal("el bot no ve a Jefe")
		}
		f.SetGuard(e.UUID, "Jefe")
		botID := int32(b.Player.Login.PlayerID)
		hit := func(victim, by int32) {
			ev := play.DamageEvent{EntityID: pk.VarInt(victim), SourceCauseID: pk.VarInt(by + 1), SourceDirectID: pk.VarInt(by + 1)}
			if err := s.Player("Bot").Send(ev); err != nil {
				t.Fatal(err)
			}
		}

		hit(botID, bossID) // Jefe le pega al bot: no pasa nada
		b.Step(40)
		if attacked(s, "Bot", bossID) || f.Fighting() {
			t.Fatalf("se volvió contra el protegido: %s", f.Status())
		}

		hit(bossID, badID) // Malo le pega a Jefe: a por él
		if !b.Until(5*20, func() bool { return attacked(s, "Bot", badID) }) {
			t.Fatalf("no defendió a Jefe: %s", f.Status())
		}
		if attacked(s, "Bot", bossID) {
			t.Fatal("atacó al protegido")
		}
		t.Logf("estado: %s", f.Status())

		// un zombi le pega a Jefe: también se le defiende de los mobs
		f.Reset()
		f.SetGuard(e.UUID, "Jefe")
		const zombie = 500
		zt := slices.Index(registryid.EntityType, "minecraft:zombie")
		add := play.AddEntity{ID: zombie, UUID: pk.UUID(uuid.New()), Type: pk.VarInt(zt), X: 1.5, Y: 64, Z: 0.5}
		if err := s.Player("Bot").Send(add); err != nil {
			t.Fatal(err)
		}
		b.Step(3)
		hit(bossID, zombie)
		if !b.Until(5*20, func() bool { return attacked(s, "Bot", zombie) }) {
			t.Fatalf("no defendió a Jefe del zombi: %s", f.Status())
		}
	})
}
