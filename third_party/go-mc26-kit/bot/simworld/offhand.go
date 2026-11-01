package simworld

import (
	"math"
	"slices"

	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// The offhand (slot 45), the shield and the totem of undying.
//
//   - The F key (PlayerAction SWAP_ITEM_WITH_OFFHAND) swaps the held hotbar slot with the offhand; a click
//     with the swap button 40 swaps a slot of the menu with it (click.go).
//   - A shield is raised with a UseItem of the hand that holds it and lowered with RELEASE_USE_ITEM. It
//     blocks from the fifth tick it is up (the shield's block delay: 0.25 s), and then it blocks all the
//     damage that comes from in front (LivingEntity.resolveBlockedDamage: the source's position against
//     the way the player looks, flat) — a blow, an arrow, a blast. Its wear and the axe that disables it
//     are not modelled.
//   - A totem in either hand (the offhand first, as LivingEntity.checkTotemDeathProtection) saves the
//     player from a death: it is used up, the health set to one, the effects given (regeneration II for
//     45 s, absorption II for 5 s, fire resistance for 40 s) and the entity event 35 told. Absorption's
//     extra hearts are not modelled.

const (
	offhandSlot = 45
	shieldDelay = 5 // ticks a raised shield takes to block
)

// Crits is how many of the player's blows were criticals (a test's look).
func (g *Game) Crits() int { return g.crits }

// Blocks is how many hits the shield has blocked (a test's look).
func (g *Game) Blocks() int { return g.blocked }

// TotemsUsed is how many totems have saved the player (a test's look).
func (g *Game) TotemsUsed() int { return g.totems }

// Offhand is what the player holds in the offhand (a test's look): the item's name, "" for nothing.
func (g *Game) Offhand() string {
	if g.slots[offhandSlot].n == 0 {
		return ""
	}
	return g.slots[offhandSlot].item
}

// swapOffhand is the F key: the held hotbar slot and the offhand change places (ServerGamePacketListenerImpl's
// SWAP_ITEM_WITH_OFFHAND); a raised shield goes down.
func (g *Game) swapOffhand() error {
	if g.IgnoreSwapKey {
		return nil
	}
	g.blocking = false
	g.eating = nil
	h := &g.slots[36+g.held]
	*h, g.slots[offhandSlot] = g.slots[offhandSlot], *h
	g.broadcast()
	return nil
}

// raiseShield is a UseItem with a shield in that hand: it is up from now (it blocks shieldDelay ticks later).
// It says whether the hand holds a shield.
func (g *Game) raiseShield(u play.UseItem) (bool, error) {
	s := g.heldItem()
	if u.Hand == types.InteractionHandOffHand {
		s = g.slots[offhandSlot]
	}
	if s.n == 0 || s.item != "minecraft:shield" {
		return false, nil
	}
	if !g.blocking {
		g.blocking, g.blockSince = true, g.Tick
	}
	return true, g.ack(u.Sequence)
}

// blocks says whether the raised shield takes the damage that comes from (x, z): up long enough, and the
// source in front (the flat way to it against the way the player looks).
func (g *Game) blocks(from *[3]float64) bool {
	if !g.blocking || g.Tick-g.blockSince < shieldDelay || from == nil {
		return false
	}
	dx, dz := from[0]-g.pos[0], from[2]-g.pos[2]
	n := math.Hypot(dx, dz)
	if n < 1e-6 {
		return false
	}
	yaw := g.yaw * math.Pi / 180
	return (-math.Sin(yaw)*dx+math.Cos(yaw)*dz)/n > 0
}

// totem saves the player from a death with a totem in a hand, the offhand first; it says whether one did.
func (g *Game) totem() (bool, error) {
	var s *slotItem
	for _, i := range []int{offhandSlot, 36 + g.held} {
		if g.slots[i].n > 0 && g.slots[i].item == "minecraft:totem_of_undying" {
			s = &g.slots[i]
			break
		}
	}
	if s == nil {
		return false, nil
	}
	if s.n--; s.n == 0 {
		*s = slotItem{}
	}
	g.totems++
	g.Health = 1
	for _, e := range []struct {
		name     string
		amp, dur int
	}{{"minecraft:regeneration", 1, 900}, {"minecraft:absorption", 1, 100}, {"minecraft:fire_resistance", 0, 800}} {
		if err := g.S.Send(play.UpdateMobEffect{EntityID: 1, Effect: pk.VarInt(slices.Index(registryid.MobEffect, e.name)),
			EffectAmplifier: pk.VarInt(e.amp), EffectDurationTicks: pk.VarInt(e.dur), Flags: 2}); err != nil {
			return true, err
		}
	}
	g.regen = g.Tick + 900
	if err := g.S.Send(play.EntityEvent{EntityID: 1, EventID: 35}); err != nil {
		return true, err
	}
	g.broadcast()
	return true, g.tellHealth([3]float32{})
}
