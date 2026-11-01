package simworld

import (
	"math"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// A bed used where one cannot sleep (the Nether, the End) blows up
// (BedBlock.useWithoutItem): both halves gone, an explosion of radius 5 at
// the head's centre that sets fires — on one in three of the cells it
// cleared that have a solid block under them (ServerExplosion.createFire).
// Standing in fire hurts (1, the hurt time limiting it to one in 10 ticks);
// 20 ticks in it set the player on fire (not modelled: counted in BurnedLong).

// BurnedLong is how many times the player stood 20 ticks in fire (it would have caught fire).
func (g *Game) BurnedLong() int { return g.burnedLong }

func (g *Game) bedBlast(x, y, z int, seq pk.VarInt) error {
	st := world.StateString(g.S.World.At(x, y, z))
	head := [3]int{x, y, z}
	if strings.Contains(st, "part=foot") {
		d := map[string][2]int{"north": {0, -1}, "south": {0, 1}, "west": {-1, 0}, "east": {1, 0}}
		for k, v := range d {
			if strings.Contains(st, "facing="+k) {
				head = [3]int{x + v[0], y, z + v[1]}
			}
		}
	}
	for _, c := range [][3]int{{x, y, z}, head} {
		g.S.World.Set(c[0], c[1], c[2], "minecraft:air")
		if err := g.S.Send(play.BlockUpdate{Pos: pk.Position{X: c[0], Y: c[1], Z: c[2]}, BlockState: 0}); err != nil {
			return err
		}
	}
	if err := g.ack(seq); err != nil {
		return err
	}
	src := &mob{id: -1, kind: "bed"}
	if err := g.explode([3]float64{float64(head[0]) + 0.5, float64(head[1]) + 0.5, float64(head[2]) + 0.5}, 5, src); err != nil {
		return err
	}
	for _, at := range g.blown {
		if g.S.World.At(at.X, at.Y, at.Z) != 0 || g.ai.Intn(3) != 0 {
			continue
		}
		below := g.S.World.At(at.X, at.Y-1, at.Z)
		if len(block.CollisionShape(below)) == 0 {
			continue
		}
		g.S.World.Set(at.X, at.Y, at.Z, "minecraft:fire")
		if err := g.S.Send(play.BlockUpdate{Pos: pk.Position{X: at.X, Y: at.Y, Z: at.Z}, BlockState: pk.VarInt(g.S.World.At(at.X, at.Y, at.Z))}); err != nil {
			return err
		}
	}
	return nil
}

// fireTick: the player standing in fire is hurt.
func (g *Game) fireTick() error {
	if g.dead {
		return nil
	}
	in := false
	for x := int(math.Floor(g.pos[0] - playerHalf)); x <= int(math.Floor(g.pos[0]+playerHalf)); x++ {
		for z := int(math.Floor(g.pos[2] - playerHalf)); z <= int(math.Floor(g.pos[2]+playerHalf)); z++ {
			for y := int(math.Floor(g.pos[1])); y <= int(math.Floor(g.pos[1]+playerHigh-0.01)); y++ {
				if strings.HasSuffix(block.StateList[g.S.World.At(x, y, z)].ID(), "fire") {
					in = true
				}
			}
		}
	}
	if err := g.suffocateTick(); err != nil { // PATCH: gravel.go
		return err
	}
	if err := g.playerBurnTick(); err != nil {
		return err
	}
	if !in {
		g.inFire = 0
		return nil
	}
	if g.inFire++; g.inFire == 20 {
		g.burnedLong++
		if err := g.SetOnFire(160); err != nil { // (20 ticks en el fuego: prende, 8 s)
			return err
		}
	}
	hurt, err := g.hurtPlayer(1, "minecraft:on_fire", -1, -1, 0, "burned", nil)
	if hurt {
		g.FireHurts++
	}
	return err
}

// SetOnFire sets the player on fire for ticks (Entity.igniteForTicks): it is hurt one a second
// (Entity.baseTick: every 20 ticks of the remaining fire) till the fire is out or it touches water
// (Entity.updateInWaterStateAndDoFluidPushing: clearFire). The client sees it as the shared flag 0x01.
func (g *Game) SetOnFire(ticks int) error {
	was := g.burning > 0
	g.burning = max(g.burning, ticks)
	if !was {
		return g.tellBurning(true)
	}
	return nil
}

// OnFire is how many ticks of fire the player has left (a test's look).
func (g *Game) OnFire() int { return g.burning }

// playerBurnTick is the fire on the player: water puts it out; else it hurts every 20 ticks and burns down.
func (g *Game) playerBurnTick() error {
	if g.burning <= 0 {
		return nil
	}
	feet := block.StateList[g.S.World.At(int(math.Floor(g.pos[0])), int(math.Floor(g.pos[1]+0.1)), int(math.Floor(g.pos[2])))].ID()
	if strings.Contains(feet, "water") {
		g.burning = 0
		return g.tellBurning(false)
	}
	if g.burning%20 == 0 {
		if hurt, err := g.hurtPlayer(1, "minecraft:on_fire", -1, -1, 0, "burned", nil); err != nil {
			return err
		} else if hurt {
			g.FireHurts++
		}
	}
	if g.burning--; g.burning == 0 {
		return g.tellBurning(false)
	}
	return nil
}

// tellBurning tells the client the shared flags of the player's entity (bit 0x01: on fire).
func (g *Game) tellBurning(on bool) error {
	var flags pk.Byte
	if on {
		flags = 1
	}
	return g.S.Send(play.SetEntityData{ID: 1, PackedItems: types.EntityData{{Index: entitydata.EntitySharedFlags, Serializer: 0, Value: &flags}}})
}
