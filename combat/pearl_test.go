package combat_test

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// TestChasePearl: peleando, el objetivo lanza una perla (hacia un lado): el
// bot va a donde cae, no a donde está él; cuando cae, lo sigue como siempre.
func TestChasePearl(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g, f := simFighter(t, kit...)
		var logs []string
		f.Log = func(format string, args ...any) {
			t.Logf(format, args...)
			logs = append(logs, format)
		}
		g.Step(20)
		if err := g.Summon("minecraft:zombie", 8.5, 0.5); err != nil {
			t.Fatal(err)
		}
		id := g.LastMob()
		g.Step(5)
		f.Engage(id, "zombi")
		g.Step(10)
		zx, zy, zz, _ := g.MobAt(id)
		// la perla sale de sus ojos hacia +z (perpendicular a donde está el bot)
		var u pk.UUID
		u[0], u[15] = 9, 1
		if err := g.S.Send(play.AddEntity{ID: 9001, UUID: u, Type: pk.VarInt(slices.Index(registryid.EntityType, "minecraft:ender_pearl")),
			X: pk.Double(zx), Y: pk.Double(zy + 1.5), Z: pk.Double(zz), Movement: types.LpVec3{X: 0, Y: 0.6, Z: 1.3}, Data: pk.VarInt(id)}); err != nil {
			t.Fatal(err)
		}
		went := g.Until(8*20, func() bool { return g.B.Player.Position().Z > 15 })
		p := g.B.Player.Position()
		t.Logf("fue hacia la perla=%v: en %.1f %.1f %.1f (el zombi en %.1f %.1f)", went, p.X, p.Y, p.Z, zx, zz)
		if !went {
			t.Fatalf("no fue a donde cae la perla (z=%.1f)", p.Z)
		}
		if !slices.ContainsFunc(logs, func(s string) bool { return strings.Contains(s, "lanzó una perla") }) {
			t.Errorf("no dijo que iba a por la perla")
		}
		// cae: el servidor la quita; vuelve a ir a por el zombi
		if err := g.S.Send(play.RemoveEntities{EntityIds: []pk.VarInt{9001}}); err != nil {
			t.Fatal(err)
		}
		back := g.Until(10*20, func() bool {
			x, _, z, ok := g.MobAt(id)
			q := g.B.Player.Position()
			return !ok || math.Hypot(x-q.X, z-q.Z) < 3
		})
		if !back {
			t.Errorf("tras caer la perla no volvió a por el zombi")
		}
	})
}
