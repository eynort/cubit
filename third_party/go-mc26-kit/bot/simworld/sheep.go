package simworld

import (
	"math"
	"slices"

	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Sheep, a little of what the game does (Sheep, TemptGoal, BreedGoal, EatBlockGoal):
//   - with wheat in the player's hand within 10 blocks it walks after the player, to 2.5 off;
//   - shears on an adult not shorn: 1 to 3 wool where it stands, shorn (DATA_WOOL_ID's bit 16);
//   - wheat on an adult not in love: in love 600 ticks; two in love within 8 blocks: a lamb (they
//     stay out of love); a lamb grows up in RegrowTicks… (AgeableMob: 24000, here LambTicks);
//   - a shorn sheep on grass eats it (grass to dirt) and grows its wool again, one tick in 1000
//     (EatBlockGoal's chance for an adult), or every tick while FastWool is set (a test's).

// LambTicks is how long a lamb takes to grow up here (the game's 24000; a test may shorten it).
var LambTicks = 24000

// FastWool makes a shorn sheep on grass grow its wool on its next tick (a test's).
var FastWool = false

func (g *Game) tempts(m *mob, dist float64) bool {
	h := g.heldItem()
	return h.n > 0 && h.item == "minecraft:wheat" && dist <= 10 && dist > 2.5 && math.Abs(g.pos[1]-m.y) < 4
}

func (g *Game) sheepTick(m *mob) error {
	if m.love > 0 {
		m.love--
	}
	if m.isBaby {
		if m.age++; m.age >= LambTicks {
			m.isBaby, m.half, m.high = false, m.half*2, m.high*2
			if err := g.S.Send(play.SetEntityData{ID: pk.VarInt(m.id), PackedItems: types.EntityData{
				{Index: entitydata.AgeableMobBaby, Serializer: 8, Value: ptr(pk.Boolean(false))}}}); err != nil {
				return err
			}
		}
	}
	if m.sheared && (FastWool || g.ai.Float32() < 0.001) {
		x, y, z := int(math.Floor(m.x)), int(math.Floor(m.y))-1, int(math.Floor(m.z))
		if block.StateList[g.S.World.At(x, y, z)].ID() == "minecraft:grass_block" {
			g.S.World.Set(x, y, z, "minecraft:dirt")
			if err := g.S.Send(play.BlockUpdate{Pos: pk.Position{X: x, Y: y, Z: z}, BlockState: pk.VarInt(g.S.World.At(x, y, z))}); err != nil {
				return err
			}
			m.sheared = false
			return g.tellWool(m)
		}
	}
	return nil
}

func (g *Game) tellWool(m *mob) error {
	var b pk.Byte // white
	if m.sheared {
		b |= 16
	}
	return g.S.Send(play.SetEntityData{ID: pk.VarInt(m.id), PackedItems: types.EntityData{
		{Index: entitydata.SheepWool, Serializer: 0, Value: &b}}})
}

// interactMob: the player right-clicked a mob with what it holds.
func (g *Game) interactMob(id int32) error {
	var m *mob
	for _, x := range g.mobs {
		if x.id == id && x.dying == 0 {
			m = x
		}
	}
	if m == nil || m.kind != "minecraft:sheep" {
		return nil // (no use modelled)
	}
	h := g.heldItem()
	switch {
	case h.item == "minecraft:shears" && !m.sheared && !m.isBaby:
		m.sheared = true
		n := 1 + g.rng.Intn(3)
		if err := g.addItem(m.x, m.y+1, m.z, 0, 0.1, 0, slotItem{item: "minecraft:white_wool", n: n}, pickupDelay); err != nil {
			return err
		}
		return g.tellWool(m)
	case h.item == "minecraft:wheat" && !m.isBaby && m.love == 0:
		g.takeHeld()
		m.love = 600
		for _, o := range g.mobs {
			if o != m && o.kind == m.kind && o.love > 0 && o.dying == 0 && math.Hypot(o.x-m.x, o.z-m.z) < 8 {
				o.love, m.love = 0, 0
				return g.lamb(m)
			}
		}
	}
	return nil
}

// lamb: a baby sheep where m is.
func (g *Game) lamb(m *mob) error {
	g.nextID++
	b := &mob{id: g.nextID, kind: m.kind, k: m.k, x: m.x + 0.3, y: m.y, z: m.z, health: m.k.health, onGround: true,
		half: m.half / 2, high: m.high / 2, isBaby: true, swellDir: -1, strafing: -1, attackTime: -1, using: -1}
	g.mobs = append(g.mobs, b)
	var u pk.UUID
	u[15], u[14] = byte(b.id), byte(b.id>>8)
	u[0] = 1
	if err := g.S.Send(play.AddEntity{ID: pk.VarInt(b.id), UUID: u, Type: pk.VarInt(slices.Index(registryid.EntityType, b.kind)),
		X: pk.Double(b.x), Y: pk.Double(b.y), Z: pk.Double(b.z)}); err != nil {
		return err
	}
	return g.S.Send(play.SetEntityData{ID: pk.VarInt(b.id), PackedItems: types.EntityData{
		{Index: entitydata.AgeableMobBaby, Serializer: 8, Value: ptr(pk.Boolean(true))}}})
}

// Sheep are the sheep alive: id, shorn, baby, where.
func (g *Game) Sheep() []SheepInfo {
	var out []SheepInfo
	for _, m := range g.mobs {
		if m.kind == "minecraft:sheep" && m.dying == 0 {
			out = append(out, SheepInfo{ID: m.id, Sheared: m.sheared, Baby: m.isBaby, X: m.x, Y: m.y, Z: m.z})
		}
	}
	return out
}

// SheepInfo is a sheep as a test sees it.
type SheepInfo struct {
	ID            int32
	Sheared, Baby bool
	X, Y, Z       float64
}
