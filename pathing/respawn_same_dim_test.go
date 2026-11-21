package pathing_test

import (
	"testing"

	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"

	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/simworld"

	"cubit/pathing"
)

// TestSkinRefreshRespawn: tras un /skin, SkinsRestorer "refresca" al
// jugador con un respawn a la misma dimensión, sin volver a mandar los
// chunks (el cliente de verdad conserva el mundo: solo lo cambia con la
// dimensión). En el log de las 02:08, el bot lo borraba todo, esperaba 30 s
// a su chunk y luego caía por un mundo vacío, devuelto por el servidor dos
// veces por segundo, sin fin. Tiene que seguir de pie, con el mundo y las
// entidades de antes, y poder andar.
func TestSkinRefreshRespawn(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{})
		b := g.B
		b.Mover.Food = func() int { return 20 }
		w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		b.Ctl.Think = w.Think
		g.Step(20)
		if err := g.Summon("minecraft:cow", 4.5, 0.5); err != nil {
			t.Fatal(err)
		}
		g.Step(5)
		cols := len(b.World.Columns)
		p0 := b.Player.Position()
		// lo que manda el plugin: respawn (misma dimensión), y la posición
		if err := g.S.Send(play.Respawn{CommonPlayerSpawnInfo: types.CommonPlayerSpawnInfo{DimensionType: 0, Dimension: "minecraft:overworld",
			GameType: 0, PreviousGameType: -1, SeaLevel: 63}, DataToKeep: 3}); err != nil {
			t.Fatal(err)
		}
		if err := g.S.Send(play.PlayerPosition{ID: 900, Change: types.PositionMoveRotation{Position: types.Vec3{X: pk.Double(p0.X), Y: pk.Double(p0.Y), Z: pk.Double(p0.Z)}}}); err != nil {
			t.Fatal(err)
		}
		t0 := b.Ctl.State().Teleports
		g.Step(40 * 20) // (más que los 30 s que espera a su chunk)
		p := b.Player.Position()
		t.Logf("chunks %d → %d; en %.2f %.2f %.2f (antes %.2f); correcciones %d; entidades %d", cols, len(b.World.Columns), p.X, p.Y, p.Z, p0.Y, b.Ctl.State().Teleports-t0, len(b.Entities.Nearby(p.X, p.Y, p.Z, 16)))
		if len(b.World.Columns) == 0 {
			t.Errorf("el respawn a la misma dimensión borró el mundo")
		}
		if p.Y < p0.Y-0.5 {
			t.Errorf("se cayó: y %.2f (antes %.2f)", p.Y, p0.Y)
		}
		if n := b.Ctl.State().Teleports - t0; n > 1 {
			t.Errorf("el servidor lo devolvió atrás %d veces", n)
		}
		if len(b.Entities.Nearby(p.X, p.Y, p.Z, 16)) == 0 {
			t.Errorf("olvidó las entidades (la vaca de al lado)")
		}
	})
}
