package simworld

import (
	"math"
	"slices"

	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// The player's bow (BowItem): a UseItem with a bow in hand and an arrow in the inventory starts the draw; a
// RELEASE_USE_ITEM looses it with the power of the ticks drawn (BowItem.getPowerForTime: t/20, then
// (f² + 2f)/3, at most 1; under 0.1 nothing), along the look, at 3 blocks a tick times the power, from the
// eyes less 0.1, with the arrow's base damage 2 (the hit's damage: ceil(speed × 2)). One arrow is used up.
// It hits the mobs (arrowsTick); the crossbow is not modelled.

// Shots is how many arrows the player has loosed (a test's look).
func (g *Game) Shots() int { return g.shots }

// drawBow starts the draw (a bow in hand, an arrow to shoot); it says whether it was a bow.
func (g *Game) drawBow(u play.UseItem) (bool, error) {
	if g.heldItem().item != "minecraft:bow" {
		return false, nil
	}
	if g.Count("minecraft:arrow") > 0 {
		g.drawing, g.drawSince = true, g.Tick
	}
	return true, g.ack(u.Sequence)
}

// looseBow shoots a drawn bow.
func (g *Game) looseBow() error {
	if !g.drawing {
		return nil
	}
	g.drawing = false
	f := float64(g.Tick-g.drawSince) / 20
	if f = (f*f + 2*f) / 3; f > 1 {
		f = 1
	}
	if f < 0.1 || g.heldItem().item != "minecraft:bow" {
		return nil
	}
	for i := range g.slots { // an arrow used up
		if g.slots[i].item == "minecraft:arrow" && g.slots[i].n > 0 {
			if g.slots[i].n--; g.slots[i].n == 0 {
				g.slots[i] = slotItem{}
			}
			break
		}
	}
	g.broadcast()
	yaw, pitch := g.yaw*math.Pi/180, g.pitch*math.Pi/180
	dir := [3]float64{-math.Sin(yaw) * math.Cos(pitch), -math.Sin(pitch), math.Cos(yaw) * math.Cos(pitch)}
	g.nextID++
	a := &arrow{id: g.nextID, owner: 1, ownerKind: "player", x: g.pos[0], y: g.pos[1] + 1.62 - 0.1, z: g.pos[2],
		vx: dir[0] * f * 3, vy: dir[1] * f * 3, vz: dir[2] * f * 3, damage: 2}
	g.arrows = append(g.arrows, a)
	g.shots++
	var u pk.UUID
	u[15], u[14] = byte(a.id), byte(a.id>>8)
	u[0] = 2
	return g.S.Send(play.AddEntity{ID: pk.VarInt(a.id), UUID: u, Type: pk.VarInt(slices.Index(registryid.EntityType, "minecraft:arrow")),
		X: pk.Double(a.x), Y: pk.Double(a.y), Z: pk.Double(a.z), Movement: types.LpVec3{X: a.vx, Y: a.vy, Z: a.vz}, Data: 1})
}

// playerArrowHits: a player's arrow this tick meets a mob first (before the block it would meet at tBlock):
// it hurts it (ceil(speed × damage)) and is gone. It says whether it hit.
func (g *Game) playerArrowHits(a *arrow, from, d [3]float64, tBlock float64, hitBlock bool) (bool, error) {
	var best *mob
	bestT := math.Inf(1)
	for _, m := range g.mobs {
		if m.dying > 0 {
			continue
		}
		lo := [3]float64{m.x - m.half - arrowInflate, m.y - arrowInflate, m.z - m.half - arrowInflate}
		hi := [3]float64{m.x + m.half + arrowInflate, m.y + m.high + arrowInflate, m.z + m.half + arrowInflate}
		if t, ok := rayBox(from, d, lo, hi); ok && (!hitBlock || t <= tBlock) && t < bestT {
			best, bestT = m, t
		}
	}
	if best == nil {
		return false, nil
	}
	speed := math.Sqrt(a.vx*a.vx + a.vy*a.vy + a.vz*a.vz)
	if _, err := g.hurtMob(best, float32(math.Ceil(speed*a.damage)), "minecraft:arrow"); err != nil {
		return true, err
	}
	// the hit's knockback (LivingEntity.hurt: 0.4 along the arrow's way), up a little from the ground:
	// what throws one off a pillar
	if n := math.Hypot(a.vx, a.vz); n > 1e-4 {
		best.vx, best.vz = best.vx/2+a.vx/n*0.4, best.vz/2+a.vz/n*0.4
		if best.onGround {
			best.vy = math.Min(0.4, best.vy/2+0.4)
		}
	}
	g.arrowHits++
	return true, nil
}

// ArrowHits is how many of the player's arrows hit a mob (a test's look).
func (g *Game) ArrowHits() int { return g.arrowHits }

// Sweeps is how many mobs the player's sweeps hit (a test's look).
func (g *Game) Sweeps() int { return g.sweeps }
