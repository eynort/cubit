package simworld

// PATCH: falling blocks and suffocation (a bot digging up under gravel buried
// itself in the Nether: 20 to 12 health in four seconds).

import (
	"math"

	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// fallsDown: a block that falls when what is under it goes (FallingBlock).
func fallsDown(state block.StateID) bool {
	switch block.StateList[state].ID() {
	case "minecraft:gravel", "minecraft:sand", "minecraft:red_sand":
		return true
	}
	return false
}

// Suffocations is how many times the player was hurt inside a block (a test's look).
func (g *Game) Suffocations() int { return g.suffocations }

// settleAbove lets the column of falling blocks over x y z come down after the block there went. The
// fall is at once (the game's falling block entity takes a few ticks): each lands on the first thing
// with a collision under it — the player is not one: a block falls into the player's cells (FallingBlockEntity
// places itself where it stops, whoever stands there).
func (g *Game) settleAbove(x, y, z int) error {
	for top := y + 1; fallsDown(g.S.World.At(x, top, z)); top++ {
		st := g.S.World.At(x, top, z)
		land := top
		for land-1 > -64 && len(block.CollisionShape(g.S.World.At(x, land-1, z))) == 0 {
			land--
		}
		if land == top {
			continue
		}
		g.S.World.SetState(x, top, z, 0)
		g.S.World.SetState(x, land, z, st)
		for _, c := range []struct {
			y  int
			st block.StateID
		}{{top, 0}, {land, st}} {
			if err := g.S.Send(play.BlockUpdate{Pos: pk.Position{X: x, Y: c.y, Z: z}, BlockState: pk.VarInt(c.st)}); err != nil {
				return err
			}
		}
	}
	return nil
}

// suffocateTick is LivingEntity.baseTick's in-wall damage: the eyes inside a full block, 1 every 10 ticks.
func (g *Game) suffocateTick() error {
	if g.dead {
		return nil
	}
	eye := g.S.World.At(int(math.Floor(g.pos[0])), int(math.Floor(g.pos[1]+1.62)), int(math.Floor(g.pos[2])))
	if len(block.CollisionShape(eye)) == 0 {
		g.inWall = 0
		return nil
	}
	if g.inWall++; g.inWall%10 == 0 {
		if hurt, err := g.hurtPlayer(1, "minecraft:in_wall", -1, -1, 0, "suffocated", nil); err != nil {
			return err
		} else if hurt {
			g.suffocations++
		}
	}
	return nil
}
