package act

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/data/version"
	"github.com/mj41/go-mc26/level/block"
	"github.com/mj41/go-mc26/level/component"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Use is the result of a use: what the block became, or why not.
type Use struct {
	State block.StateID
	Ticks int
	Err   error
}

// offsets of the faces, in Face order.
var faceOffset = [6][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

func opposite(f Face) Face { return f ^ 1 }

// Place puts the held block at pos, as a person right-clicking the face of a
// block next to it does: the block below first, then the sides, then above.
// done is called once the server shows the block there, or it gave up.
func (h *Hands) Place(pos world.BlockPos, done func(Use)) {
	h.start(&use{target: pos, place: true, done: done})
}

// PlaceOn puts the held block at pos against the block against, which must
// touch it — a torch on a wall, not hung from the roof the nearest face would
// be.
func (h *Hands) PlaceOn(pos, against world.BlockPos, done func(Use)) {
	h.start(&use{target: pos, place: true, against: &against, done: done})
}

// PlaceOnNow is PlaceOn as the game's client places: the block is in the
// world at once (predicted, as the client's own, till the server's ack puts
// back what the server says if it refused it) and done is called when the
// click went out, not after the server's answer — a place every two ticks
// (the aim, the click), not one a round trip. A held block whose placed state
// is not plain (it has properties that depend on the look) is waited for as
// PlaceOn.
func (h *Hands) PlaceOnNow(pos, against world.BlockPos, done func(Use)) {
	h.start(&use{target: pos, place: true, against: &against, predict: true, done: done})
}

// PlaceOnAt is PlaceOn clicking the face of against at cursor (where on the
// face, 0 to 1 each way, in the block's own coordinates) instead of its
// centre: the click's height picks a slab's or a stair's half, and the point
// looked at picks the way the player faces (stairs, furnaces, signs).
func (h *Hands) PlaceOnAt(pos, against world.BlockPos, cursor [3]float32, done func(Use)) {
	h.start(&use{target: pos, place: true, against: &against, cursor: cursor, fixedCursor: true, done: done})
}

// placedState is the state a held block item puts down when it has no
// properties to choose (stone, dirt, planks…).
func placedState(s types.ItemStack) (block.StateID, bool) {
	if s.Count <= 0 || int(s.Item) < 0 || int(s.Item) >= len(registryid.Item) {
		return 0, false
	}
	b, ok := block.FromID[registryid.Item[s.Item]]
	if !ok {
		return 0, false
	}
	st, ok := block.ToStateID[b]
	return st, ok
}

// UseBlock right-clicks the block at pos (a door, a button, a lever) on the
// face that looks at the player; done is called once its state changed.
func (h *Hands) UseBlock(pos world.BlockPos, done func(Use)) {
	h.start(&use{target: pos, done: done})
}

// UseBlockAt right-clicks the block at pos on the given face at cursor (where
// on the block, 0 to 1 each way) — for a block seen only through a gap: the
// point that is in sight, not the face's centre. done once its state changed.
func (h *Hands) UseBlockAt(pos world.BlockPos, face Face, cursor [3]float32, done func(Use)) {
	h.start(&use{target: pos, face: face, cursor: cursor, fixed: true, done: done})
}

// Interact right-clicks the block at pos and is done once the click went
// out — for a block whose answer is a screen (a chest, a crafting table), not
// a new state.
func (h *Hands) Interact(pos world.BlockPos, done func(Use)) {
	h.start(&use{target: pos, noWait: true, done: done})
}

type use struct {
	target  world.BlockPos
	place   bool
	against *world.BlockPos // the block to place against, when the caller chose
	sneak   bool            // shift held for the click: the block clicked has a use of its own
	noWait  bool
	predict bool // PlaceOnNow
	fixed   bool // face and cursor chosen by the caller
	// fixedCursor: PlaceOnAt, the cursor chosen by the caller (the face is against's)
	fixedCursor bool
	done        func(Use)
	clicked     world.BlockPos
	face        Face
	cursor      [3]float32
	aimed       bool
	sent        bool
	before      block.StateID
	waited      int
	seq         int32 // the click's sequence: its ack says the server has answered
}

func (u *use) finish(r Use) bool {
	if u.done != nil {
		u.done(r)
	}
	return true
}

func (u *use) tick(h *Hands, s *control.State, bp *basic.Player) bool {
	done := u.step(h, s, bp)
	if done && u.sneak {
		s.Input.Shift = false
	}
	return done
}

func (u *use) step(h *Hands, s *control.State, bp *basic.Player) bool {
	cur, ok := h.World.BlockAt(u.target)
	if !ok {
		return u.finish(Use{Err: errors.New("act: the block is not loaded")})
	}
	if !u.aimed {
		ex, ey, ez := Eye(bp.Position(), s.Input.Shift)
		if u.place {
			if !block.Replaceable(cur) {
				return u.finish(Use{Err: fmt.Errorf("act: %s is in the way", world.StateString(cur))})
			}
			if a := u.against; a != nil {
				f := -1
				for i, o := range faceOffset {
					if a.X+o[0] == u.target.X && a.Y+o[1] == u.target.Y && a.Z+o[2] == u.target.Z {
						f = i
					}
				}
				if f < 0 {
					return u.finish(Use{Err: errors.New("act: the block to place against does not touch the place")})
				}
				u.clicked, u.face = *a, Face(f)
				if !u.fixedCursor {
					u.cursor = faceCentre(u.face)
				}
			} else if !u.findFace(h, ex, ey, ez) {
				return u.finish(Use{Err: errors.New("act: no block next to it to place against")})
			}
		} else if u.fixed {
			u.clicked = u.target
		} else {
			u.clicked = u.target
			u.face = facing(ex, ey, ez, u.target)
			u.cursor = faceCentre(u.face)
		}
		if cs, ok := h.World.BlockAt(u.clicked); ok && u.place && Usable(cs) {
			u.sneak = true // as a person shift-clicks to place against it
			s.Input.Shift = true
		}
		hx := float64(u.clicked.X) + float64(u.cursor[0])
		hy := float64(u.clicked.Y) + float64(u.cursor[1])
		hz := float64(u.clicked.Z) + float64(u.cursor[2])
		if d := math.Sqrt((hx-ex)*(hx-ex) + (hy-ey)*(hy-ey) + (hz-ez)*(hz-ez)); d > reach(bp) {
			return u.finish(Use{Err: fmt.Errorf("act: %.1f blocks away, out of reach", d)})
		}
		LookAt(bp, s.Input.Shift, hx, hy, hz)
		u.before, u.aimed = cur, true
		return false
	}
	if !u.sent {
		seq, err := h.useItemOn(u.clicked, u.face, u.cursor)
		if err != nil {
			return u.finish(Use{Err: err})
		}
		u.seq = seq
		u.sent = true
		if u.predict {
			if st, ok := placedState(h.held()); ok {
				h.World.Predict(u.target, st, seq)
				return u.finish(Use{State: st})
			}
		}
		if u.noWait {
			return u.finish(Use{State: cur})
		}
		return false
	}
	// the server answers with the block's new state, then the click's ack:
	// acked and the block the same, the place was refused. Waited for by the
	// ack, not a count of the client's ticks: a busy server answered after
	// 20 of them, and a place it made was taken for refused (botprobe's hut)
	if cur != u.before {
		return u.finish(Use{State: cur, Ticks: u.waited})
	}
	if h.World.Acked() >= u.seq {
		return u.finish(Use{State: cur, Err: errors.New("act: the server did not change the block")})
	}
	if u.waited++; u.waited > 200 {
		return u.finish(Use{State: cur, Err: errors.New("act: the server never answered the place")})
	}
	return false
}

// findFace picks the neighbour to click and the face of it that touches the
// target: of the solid neighbours, the one whose face centre is nearest the
// eye, as a person aims at the face they see best.
func (u *use) findFace(h *Hands, ex, ey, ez float64) bool {
	best := math.MaxFloat64
	for _, toward := range []Face{Down, North, South, West, East, Up} {
		o := faceOffset[toward]
		n := world.BlockPos{X: u.target.X + o[0], Y: u.target.Y + o[1], Z: u.target.Z + o[2]}
		s, ok := h.World.BlockAt(n)
		if !ok || len(block.CollisionShape(s)) == 0 || block.Replaceable(s) {
			continue
		}
		face := opposite(toward)
		c := faceCentre(face)
		hx, hy, hz := float64(n.X)+float64(c[0]), float64(n.Y)+float64(c[1]), float64(n.Z)+float64(c[2])
		d := (hx-ex)*(hx-ex) + (hy-ey)*(hy-ey) + (hz-ez)*(hz-ez)
		if Usable(s) {
			d += 100 // a crafting table opens when clicked: another face if there is one
		}
		if d < best {
			best, u.clicked, u.face, u.cursor = d, n, face, c
		}
	}
	return best < math.MaxFloat64
}

// faceCentre is the middle of a face, in the block's own coordinates.
func faceCentre(f Face) [3]float32 {
	c := [3]float32{0.5, 0.5, 0.5}
	o := faceOffset[f]
	for i := range c {
		if o[i] > 0 {
			c[i] = 1
		} else if o[i] < 0 {
			c[i] = 0
		}
	}
	return c
}

// reach is the block interaction range (4.5 by default).
func reach(bp *basic.Player) float64 {
	if a, ok := bp.Attribute("minecraft:block_interaction_range"); ok {
		return a.Value()
	}
	return 4.5
}

// useItemOn sends ServerboundUseItemOn for the main hand: the hand, the hit
// (the block, its face, where on it, not inside, not the world border), the
// next prediction sequence. Written field by field: its hit field is named
// differently across versions. Before 26.3 the client then swings its arm.
func (h *Hands) useItemOn(pos world.BlockPos, face Face, cursor [3]float32) (int32, error) {
	seq := h.nextSequence()
	err := h.C.Conn.WritePacket(pk.Marshal(packetid.ServerboundPlayUseItemOn,
		pk.VarInt(0),
		pk.Position{X: pos.X, Y: pos.Y, Z: pos.Z}, pk.VarInt(face),
		pk.Float(cursor[0]), pk.Float(cursor[1]), pk.Float(cursor[2]),
		pk.Boolean(false), pk.Boolean(false),
		seq))
	if err != nil {
		return 0, err
	}
	if version.ProtocolVersion < 777 {
		return int32(seq), h.swing()
	}
	h.swungByServer()
	return int32(seq), nil
}

// Eat uses the held food or drink, as a person holding the use key does
// (ServerboundUseItem), for as long as its consumable takes; the server
// finishes it. done is called after that time.
func (h *Hands) Eat(done func(Use)) {
	if done == nil {
		done = func(Use) {}
	}
	h.start(&eat{done: done})
}

type eat struct {
	done  func(Use)
	ticks int
	need  int
}

func (e *eat) tick(h *Hands, s *control.State, bp *basic.Player) bool {
	if e.need == 0 {
		c := Component[*component.Consumable](h.held())
		if c == nil {
			e.done(Use{Err: errors.New("act: the held item is not eaten or drunk")})
			return true
		}
		e.need = int(math.Ceil(float64(c.ConsumeSeconds)*20)) + 2
		p := bp.Position()
		err := h.C.Conn.WritePacket(pk.Marshal(packetid.ServerboundPlayUseItem,
			pk.VarInt(0), h.nextSequence(), pk.Float(p.Yaw), pk.Float(p.Pitch)))
		if err != nil { // eating consumes: no arm swing
			e.done(Use{Err: err})
			return true
		}
		return false
	}
	if e.ticks++; e.ticks >= e.need {
		e.done(Use{Ticks: e.ticks})
		return true
	}
	return false
}

// UseItemToward uses the held item toward a point, as a person does who
// aims and right-clicks it without a block under the cursor that takes the
// click — a boat onto water, a bucket from a pool: one tick to aim, then
// ServerboundUseItem with the look; the server traces the look itself.
// done is called once it went out.
func (h *Hands) UseItemToward(x, y, z float64, done func(Use)) {
	if done == nil {
		done = func(Use) {}
	}
	h.start(&useToward{at: [3]float64{x, y, z}, done: done})
}

type useToward struct {
	at    [3]float64
	aimed bool
	done  func(Use)
}

func (u *useToward) tick(h *Hands, s *control.State, bp *basic.Player) bool {
	if !u.aimed {
		LookAt(bp, s.Input.Shift, u.at[0], u.at[1], u.at[2])
		u.aimed = true
		return false
	}
	p := bp.Position()
	err := h.C.Conn.WritePacket(pk.Marshal(packetid.ServerboundPlayUseItem,
		pk.VarInt(0), h.nextSequence(), pk.Float(p.Yaw), pk.Float(p.Pitch)))
	if err == nil && version.ProtocolVersion < 777 {
		err = h.swing()
	} else if err == nil {
		h.swungByServer()
	}
	u.done(Use{Err: err})
	return true
}

// UseItemLooking uses the held item on the next tick with the look it gives (yaw, pitch: the server traces
// the look the packet carries), without a tick to turn first — a water bucket thrown down in a fall, when
// the ground is in reach for a tick or less. The player's own look is turned too. done is called once it
// went out.
func (h *Hands) UseItemLooking(yaw, pitch float32, done func(Use)) {
	if done == nil {
		done = func(Use) {}
	}
	h.start(&useLooking{yaw: yaw, pitch: pitch, done: done})
}

type useLooking struct {
	yaw, pitch float32
	done       func(Use)
}

func (u *useLooking) tick(h *Hands, s *control.State, bp *basic.Player) bool {
	bp.UpdatePosition(func(p basic.Position) basic.Position { p.Yaw, p.Pitch = u.yaw, u.pitch; return p })
	err := h.C.Conn.WritePacket(pk.Marshal(packetid.ServerboundPlayUseItem,
		pk.VarInt(0), h.nextSequence(), pk.Float(u.yaw), pk.Float(u.pitch)))
	if err == nil && version.ProtocolVersion < 777 {
		err = h.swing()
	} else if err == nil {
		h.swungByServer()
	}
	u.done(Use{Err: err})
	return true
}

// Usable is a block that does something of its own when right-clicked — opens
// a screen, turns, rings — so a click to place against it has to be a
// shift-click (Player.isSecondaryUseActive).
func Usable(s block.StateID) bool {
	if int(s) < 0 || int(s) >= len(block.StateList) {
		return false
	}
	name := block.StateList[s].ID()
	switch name {
	case "minecraft:crafting_table", "minecraft:furnace", "minecraft:blast_furnace", "minecraft:smoker",
		"minecraft:chest", "minecraft:trapped_chest", "minecraft:ender_chest", "minecraft:barrel",
		"minecraft:anvil", "minecraft:chipped_anvil", "minecraft:damaged_anvil", "minecraft:enchanting_table",
		"minecraft:loom", "minecraft:cartography_table", "minecraft:smithing_table", "minecraft:grindstone",
		"minecraft:stonecutter", "minecraft:brewing_stand", "minecraft:hopper", "minecraft:dispenser",
		"minecraft:dropper", "minecraft:lever", "minecraft:note_block", "minecraft:jukebox", "minecraft:lectern",
		"minecraft:composter", "minecraft:cake", "minecraft:beacon", "minecraft:bell", "minecraft:respawn_anchor",
		"minecraft:repeater", "minecraft:comparator", "minecraft:daylight_detector", "minecraft:crafter",
		"minecraft:decorated_pot", "minecraft:chiseled_bookshelf", "minecraft:flower_pot":
		return true
	}
	for _, suffix := range []string{"_door", "_trapdoor", "_fence_gate", "_button", "_bed", "_shulker_box", "shulker_box", "_sign", "_copper_golem_statue"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// CancelEat stops the eating or drinking in progress, as a player who lets go of the use key: the server is
// told (release_use_item) and done gets ErrEatCancelled. Nothing happens when the hands are not eating.
func (h *Hands) CancelEat() {
	h.mu.Lock()
	e, ok := h.job.(*eat)
	if ok {
		h.job = nil
	}
	h.mu.Unlock()
	if !ok {
		return
	}
	if e.need > 0 {
		a := play.PlayerAction{Action: types.PlayerActionActionReleaseUseItem}
		_ = h.C.Conn.WritePacket(pk.Marshal(a.PacketID(), a))
	}
	e.done(Use{Err: ErrEatCancelled, Ticks: e.ticks})
}

// ErrEatCancelled ends an eating given up with [Hands.CancelEat].
var ErrEatCancelled = errors.New("act: the eating was given up")
