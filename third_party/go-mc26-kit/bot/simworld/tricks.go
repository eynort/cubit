package simworld

// PATCH (tricks): a duelist that plays dirty, for the bot's defence tests, and the player hurt in lava.
//
//   - Mace: on the ground within 3.5 of the player, a wind charge under its feet throws it up (1.1 a tick);
//     falling on the player (more than 1.5 down from its top, within reach) it smashes: 6 and the mace's
//     bonus for the fall (a raised shield facing it takes it all). Again 4 s later.
//   - Lava: from 2 to 4.5 off, lava poured at the player's feet (on the ground, in air), every 10 s.
//   - Trap: once, within 3: cobblestone around the player at its feet and head (the four sides), and a
//     second later lava in the player's cell.
//   - The player in lava takes 4 (as a blow: once in ten ticks unless greater) and burns 15 s.

import (
	"math"

	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

type tricks struct {
	mace, lava, trap bool
	maceNext         int
	lavaNext         int
	smashing         bool
	peakY            float64
	trapped          bool
	trapAt           int
}

// Tricks is what a duelist does beyond its blows (a test's; SummonDuelistKit's Kit).
type Tricks struct{ Mace, Lava, Trap bool }

// DirtyStats is what the dirty duelists did (a test's look).
type DirtyStats struct{ Smashes, SmashBlocked, LavaPoured, Trapped int }

// Dirty is the dirty duelists' tally.
func (g *Game) Dirty() DirtyStats { return g.dirty }

func (g *Game) setBlockTold(x, y, z int, name string) error {
	g.S.World.Set(x, y, z, name)
	return g.S.Send(play.BlockUpdate{Pos: pk.Position{X: x, Y: y, Z: z}, BlockState: pk.VarInt(g.S.World.At(x, y, z))})
}

func (g *Game) feetCell() [3]int {
	return [3]int{int(math.Floor(g.pos[0])), int(math.Floor(g.pos[1] + 1e-6)), int(math.Floor(g.pos[2]))}
}

// trickTick is a dirty duelist's tick (before its walk). It says whether the blow is its own this tick.
func (g *Game) trickTick(m *mob) (bool, error) {
	tr := m.tricks
	if tr == nil || g.dead {
		return false, nil
	}
	flat := math.Hypot(g.pos[0]-m.x, g.pos[2]-m.z)
	if tr.mace {
		if m.onGround && !tr.smashing && g.Tick >= tr.maceNext && flat <= 3.5 && flat >= 0.8 {
			m.vy, m.onGround, tr.smashing, tr.peakY = 1.1, false, true, m.y
			tr.maceNext = g.Tick + 80
		}
		if tr.smashing {
			tr.peakY = math.Max(tr.peakY, m.y)
			if flat > 0.5 { // (en el aire, hacia el jugador)
				m.vx += (g.pos[0] - m.x) / flat * 0.05
				m.vz += (g.pos[2] - m.z) / flat * 0.05
			}
			d := math.Sqrt(sq(g.pos[0]-m.x) + sq(g.pos[1]-m.y) + sq(g.pos[2]-m.z))
			if m.vy < 0 && tr.peakY-m.y > 1.5 && d <= m.k.reach {
				tr.smashing = false
				g.dirty.Smashes++
				dmg := float32(6 + smashBonus(tr.peakY-m.y))
				hurt, err := g.hurtPlayer(dmg, "minecraft:player_attack", m.id, m.id, m.yaw, "smashed by a mace", &[3]float64{m.x, m.y, m.z})
				if err != nil {
					return true, err
				}
				if !hurt {
					g.dirty.SmashBlocked++
				}
				return true, nil
			}
			if m.onGround && g.Tick > tr.maceNext-75 {
				tr.smashing = false
			}
			return true, nil // (en el aire no hay golpe normal)
		}
	}
	if tr.lava && g.Tick >= tr.lavaNext && flat >= 2 && flat <= 4.5 {
		f := g.feetCell()
		below := g.S.World.At(f[0], f[1]-1, f[2])
		if g.S.World.At(f[0], f[1], f[2]) == 0 && len(block.CollisionShape(below)) > 0 {
			tr.lavaNext = g.Tick + 200
			g.dirty.LavaPoured++
			if err := g.setBlockTold(f[0], f[1], f[2], "minecraft:lava"); err != nil {
				return false, err
			}
		}
	}
	if tr.trap && !tr.trapped && flat <= 3 {
		tr.trapped, tr.trapAt = true, g.Tick
		f := g.feetCell()
		for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			for dy := 0; dy <= 1; dy++ {
				x, y, z := f[0]+d[0], f[1]+dy, f[2]+d[1]
				if g.S.World.At(x, y, z) == 0 && !g.inPlayer(x, y, z) && !g.inMob(x, y, z) {
					if err := g.setBlockTold(x, y, z, "minecraft:cobblestone"); err != nil {
						return false, err
					}
				}
			}
		}
		g.dirty.Trapped++
	}
	if tr.trap && tr.trapped && tr.trapAt > 0 && g.Tick == tr.trapAt+20 {
		f := g.feetCell()
		if g.S.World.At(f[0], f[1], f[2]) == 0 {
			g.dirty.LavaPoured++
			if err := g.setBlockTold(f[0], f[1], f[2], "minecraft:lava"); err != nil {
				return false, err
			}
		}
	}
	return false, nil
}

// lavaTick: the player in lava burns.
func (g *Game) lavaTick() error {
	if g.dead {
		return nil
	}
	for x := int(math.Floor(g.pos[0] - playerHalf)); x <= int(math.Floor(g.pos[0]+playerHalf)); x++ {
		for z := int(math.Floor(g.pos[2] - playerHalf)); z <= int(math.Floor(g.pos[2]+playerHalf)); z++ {
			for y := int(math.Floor(g.pos[1])); y <= int(math.Floor(g.pos[1]+playerHigh-0.01)); y++ {
				if block.StateList[g.S.World.At(x, y, z)].ID() == "minecraft:lava" {
					g.lavaHurts++
					if _, err := g.hurtPlayer(4, "minecraft:on_fire", -1, -1, 0, "tried to swim in lava", nil); err != nil {
						return err
					}
					if g.burning < 300 {
						return g.SetOnFire(300)
					}
					return nil
				}
			}
		}
	}
	return nil
}

// LavaHurts is how many ticks the player spent in lava (a test's look).
func (g *Game) LavaHurts() int { return g.lavaHurts }
