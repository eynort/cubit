package simworld

// PATCH (minecraft-bot): elytra and firework rockets, as the game has them for
// a player:
//
//   - START_FALL_FLYING (ServerGamePacketListenerImpl.handlePlayerCommand):
//     with an elytra worn and off the ground the glide starts; else it is
//     stopped (Player.stopFallFlying sets the flag twice, so the client is
//     told even when it was already off). The shared flag 7 is told.
//   - While gliding the player's box is 0.6 high (Pose.FALL_FLYING), and the
//     fall is forgotten while it is not steep (checkFallDistanceAccumulation:
//     going down slower than half a block a tick, the fall is a block at
//     most). Landed, the glide ends.
//   - A firework rocket used (FireworkRocketItem.use) while gliding is spent
//     and spawns a rocket attached to the player (its owner in the add
//     packet's data); it lives 10×(flight+1) + rand(6) + rand(7) ticks with
//     a flight of 1, then it is gone. Used on the ground it is only spent.
//     Its pull on the player is the client's to simulate (FireworkRocketEntity.tick).

import (
	"slices"

	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

type rocket struct {
	id             int32
	life, lifetime int
}

// Gliding reports whether the player glides with an elytra, as the server has it.
func (g *Game) Gliding() bool { return g.gliding }

// Rockets is how many rockets are in flight.
func (g *Game) Rockets() int { return len(g.rockets) }

// startGlide is START_FALL_FLYING.
func (g *Game) startGlide() error {
	if g.Worn(6) == "minecraft:elytra" && !g.onGround && !g.bodyInWater() {
		g.gliding = true
	} else {
		g.gliding = false
	}
	return g.tellGlide()
}

func (g *Game) tellGlide() error {
	var flags pk.Byte
	if g.gliding {
		flags = -0x80 // (the shared flags' fall-flying bit, 0x80 as a signed byte)
	}
	return g.S.Send(play.SetEntityData{ID: 1, PackedItems: types.EntityData{
		{Index: entitydata.EntitySharedFlags, Serializer: 0, Value: ptr(flags)}}})
}

// useRocket is a UseItem with a firework rocket in hand.
func (g *Game) useRocket(seq pk.VarInt) error {
	hand := 36 + g.held
	g.slots[hand].n--
	if g.slots[hand].n <= 0 {
		g.slots[hand] = slotItem{}
	}
	g.broadcast()
	if !g.gliding {
		return g.ack(seq)
	}
	g.nextID++
	r := &rocket{id: g.nextID, lifetime: 20 + g.rng.Intn(6) + g.rng.Intn(7)}
	g.rockets = append(g.rockets, r)
	var u pk.UUID
	u[15], u[14] = byte(r.id), byte(r.id>>8)
	u[0] = 3 // (apart from the items', the mobs' and the arrows')
	if err := g.S.Send(play.AddEntity{ID: pk.VarInt(r.id), UUID: u, Type: pk.VarInt(slices.Index(registryid.EntityType, "minecraft:firework_rocket")),
		X: pk.Double(g.pos[0]), Y: pk.Double(g.pos[1]), Z: pk.Double(g.pos[2]), Data: 1}); err != nil {
		return err
	}
	return g.ack(seq)
}

// rocketsTick ages the rockets; one past its lifetime explodes, gone.
func (g *Game) rocketsTick() error {
	for i := 0; i < len(g.rockets); i++ {
		r := g.rockets[i]
		if r.life++; r.life > r.lifetime {
			g.rockets = slices.Delete(g.rockets, i, i+1)
			i--
			if err := g.S.Send(play.RemoveEntities{EntityIds: []pk.VarInt{pk.VarInt(r.id)}}); err != nil {
				return err
			}
		}
	}
	return nil
}
