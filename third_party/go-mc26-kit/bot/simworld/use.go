package simworld

import (
	"fmt"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// UseItemOn, as the server plays it (ServerPlayerGameMode.useItemOn): on a
// crafting table it opens the table's menu; with a hoe it tills earth, with
// seeds it sows the cell over farmland; with a block in hand it puts the block against the
// face. The packet is read field by field, as the bot writes
// it (the hit's field is named differently across versions): the hand, the
// block, the face, where on it, inside, the world border, the sequence.
func (g *Game) useItemOn(p fakeserver.Packet) error {
	var hand, face, seq pk.VarInt
	var pos pk.Position
	var cx, cy, cz pk.Float
	var inside, border pk.Boolean
	if err := p.Scan(&hand, &pos, &face, &cx, &cy, &cz, &inside, &border, &seq); err != nil {
		return err
	}
	at := g.S.World.At(pos.X, pos.Y, pos.Z)
	name := block.StateList[at].ID()
	if _, places := placeable(g.heldItem().item); g.shift && places && g.heldItem().n > 0 {
		name = "" // PATCH (build): sneaking with a block in hand, the block clicked is not used: the block is placed
	}
	switch {
	case name == "minecraft:crafting_table":
		if err := g.openTable(); err != nil {
			return err
		}
	case name == "minecraft:chest" || name == "minecraft:trapped_chest" || name == "minecraft:barrel": // PATCH (chest.go)
		if err := g.openChest([3]int{pos.X, pos.Y, pos.Z}); err != nil {
			return err
		}
		return g.ack(seq)
	case name == "minecraft:furnace":
		if err := g.openFurnace([3]int{pos.X, pos.Y, pos.Z}); err != nil {
			return err
		}
	case strings.HasSuffix(name, "_fence_gate"): // a gate opens or shuts (FenceGateBlock.useWithoutItem)
		st := world.StateString(at)
		if strings.Contains(st, "open=true") {
			st = strings.Replace(st, "open=true", "open=false", 1)
		} else {
			st = strings.Replace(st, "open=false", "open=true", 1)
		}
		g.S.World.SetState(pos.X, pos.Y, pos.Z, stateByText(st))
		if err := g.S.Send(play.BlockUpdate{Pos: pos, BlockState: pk.VarInt(g.S.World.At(pos.X, pos.Y, pos.Z))}); err != nil {
			return err
		}
	case strings.HasSuffix(name, "_bed") && g.BedsExplode: // a bed where one cannot sleep blows up (BedBlock: the Nether)
		return g.bedBlast(pos.X, pos.Y, pos.Z, seq)
	default:
		held := g.heldItem()
		if held.n == 0 {
			return g.ack(seq) // (an empty hand on a block that has no use: nothing happens)
		}
		// a hoe on earth with air over it tills it (HoeItem.useOn); seeds on
		// farmland's top with air over it are sown (the wheat's first age)
		over := g.S.World.At(pos.X, pos.Y+1, pos.Z) == 0
		switch {
		case strings.HasSuffix(held.item, "_hoe"):
			if over && (name == "minecraft:dirt" || name == "minecraft:grass_block" || name == "minecraft:dirt_path") && int(face) != 0 {
				return g.setAndAck(pos.X, pos.Y, pos.Z, "minecraft:farmland", seq)
			}
			return g.ack(seq)
		case held.item == "minecraft:wheat_seeds":
			// the seed goes where the face looks (BlockItem.place: the block
			// clicked and its face — a farmland's top, or a side of a block
			// beside the cell), if that is air and farmland is under it
			// (CropBlock.canSurvive)
			d := faceOffset(int(face))
			x, y, z := pos.X+d[0], pos.Y+d[1], pos.Z+d[2]
			if g.S.World.At(x, y, z) == 0 && block.StateList[g.S.World.At(x, y-1, z)].ID() == "minecraft:farmland" {
				g.takeHeld()
				g.crops[[3]int{x, y, z}] = true
				return g.setAndAck(x, y, z, "minecraft:wheat", seq)
			}
			return g.ack(seq)
		case held.item == "minecraft:flint_and_steel":
			// lights a nether portal when the cell it looks into is the inside
			// of an obsidian frame (2 wide, 3 high, along x); elsewhere the fire is
			// not modelled
			d := faceOffset(int(face))
			if err := g.lightPortal(pos.X+d[0], pos.Y+d[1], pos.Z+d[2]); err != nil {
				return err
			}
			if err := g.lightFire(pos.X+d[0], pos.Y+d[1], pos.Z+d[2]); err != nil { // PATCH (arsenal.go)
				return err
			}
			return g.ack(seq)
		}
		// a pickaxe or a sword has no use on a block (no useOn): nothing happens
		if strings.HasSuffix(held.item, "_pickaxe") || strings.HasSuffix(held.item, "_sword") {
			return g.ack(seq)
		}
		blockName, ok := placeable(held.item)
		// PATCH (build): a slab on the top of a bottom slab of its kind (or the
		// bottom of a top one) makes it double (SlabBlock.canBeReplaced)
		if ok && strings.HasSuffix(blockName, "_slab") && name == blockName {
			st := world.StateString(at)
			if strings.Contains(st, "type=bottom") && int(face) == 1 || strings.Contains(st, "type=top") && int(face) == 0 {
				g.takeHeld()
				g.S.World.SetState(pos.X, pos.Y, pos.Z, stateByText(blockName+"[type=double,waterlogged=false]"))
				if err := g.S.Send(play.BlockUpdate{Pos: pos, BlockState: pk.VarInt(g.S.World.At(pos.X, pos.Y, pos.Z))}); err != nil {
					return err
				}
				g.broadcast()
				return g.ack(seq)
			}
		}
		if !ok && strings.HasSuffix(name, "chest") {
			return g.ack(seq) // a chest opens whatever is held: its menu is not modelled (as with an empty hand)
		}
		if !ok {
			return fmt.Errorf("simworld: not modelled: using %s on %s", held, name)
		}
		d := faceOffset(int(face))
		x, y, z := pos.X+d[0], pos.Y+d[1], pos.Z+d[2]
		if at := g.S.World.At(x, y, z); at != 0 && !isFluid(at) { // (a fluid is replaced; other replaceable blocks — grass — are not modelled)
			return g.ack(seq)
		}
		if blockName == "minecraft:cobweb" { // PATCH (arsenal.go): no shape, so also where one stands
			g.ars.webs++
		} else if g.inPlayer(x, y, z) || g.inMob(x, y, z) { // a block where the player or a mob stands is refused (BlockItem: the shape collides with an entity)
			if err := g.S.Send(play.BlockUpdate{Pos: pk.Position{X: x, Y: y, Z: z}, BlockState: pk.VarInt(g.S.World.At(x, y, z))}); err != nil {
				return err
			}
			return g.ack(seq)
		}
		switch blockName {
		case "minecraft:furnace":
			g.placeFurnace(x, y, z)
		case "minecraft:chest": // a single chest facing the player (one beside another is not joined: not modelled)
			g.S.World.SetState(x, y, z, stateByText("minecraft:chest[facing="+g.facingOf()+",type=single,waterlogged=false]"))
		case "minecraft:white_bed": // the foot where clicked, the head on the way the player looks
			d := map[string][2]int{"north": {0, -1}, "south": {0, 1}, "west": {-1, 0}, "east": {1, 0}}[g.lookingOf()]
			hx, hz := x+d[0], z+d[1]
			if g.S.World.At(hx, y, hz) != 0 || g.inPlayer(hx, y, hz) {
				return g.ack(seq)
			}
			g.S.World.SetState(x, y, z, stateByText("minecraft:white_bed[facing="+g.lookingOf()+",occupied=false,part=foot]"))
			g.S.World.SetState(hx, y, hz, stateByText("minecraft:white_bed[facing="+g.lookingOf()+",occupied=false,part=head]"))
			if err := g.S.Send(play.BlockUpdate{Pos: pk.Position{X: hx, Y: y, Z: hz}, BlockState: pk.VarInt(g.S.World.At(hx, y, hz))}); err != nil {
				return err
			}
		case "minecraft:oak_fence_gate": // shut, facing the way the player looks
			g.S.World.SetState(x, y, z, stateByText("minecraft:oak_fence_gate[facing="+g.lookingOf()+",in_wall=false,open=false,powered=false]"))
		case "minecraft:stone_brick_stairs", "minecraft:oak_stairs": // PATCH (build): facing the way the player looks; the top half clicked from below or high on a side (StairBlock)
			half := "bottom"
			if int(face) == 0 || int(face) != 1 && float64(cy) > 0.5 {
				half = "top"
			}
			g.S.World.SetState(x, y, z, stateByText(blockName+"[facing="+g.lookingOf()+",half="+half+",shape=straight,waterlogged=false]"))
		case "minecraft:oak_slab", "minecraft:stone_brick_slab": // PATCH (build): as the stairs' half (SlabBlock)
			typ := "bottom"
			if int(face) == 0 || int(face) != 1 && float64(cy) > 0.5 {
				typ = "top"
			}
			g.S.World.SetState(x, y, z, stateByText(blockName+"[type="+typ+",waterlogged=false]"))
		case "minecraft:oak_log": // PATCH (build): along the clicked face (RotatedPillarBlock)
			g.S.World.SetState(x, y, z, stateByText("minecraft:oak_log[axis="+[...]string{"y", "y", "z", "z", "x", "x"}[int(face)]+"]"))
		case "minecraft:torch": // on the ground a torch; on a wall a wall torch pointing away from it
			if int(face) == 0 {
				return g.ack(seq) // (hung from a ceiling: it does not hold)
			}
			if int(face) == 1 {
				g.S.World.Set(x, y, z, "minecraft:torch")
			} else {
				g.S.World.SetState(x, y, z, stateByText("minecraft:wall_torch[facing="+[...]string{"", "", "north", "south", "west", "east"}[int(face)]+"]"))
			}
		default:
			g.S.World.Set(x, y, z, blockName)
		}
		g.takeHeld()
		if err := g.S.Send(play.BlockUpdate{Pos: pk.Position{X: x, Y: y, Z: z}, BlockState: pk.VarInt(g.S.World.At(x, y, z))}); err != nil {
			return err
		}
		g.broadcast()
	}
	return g.ack(seq)
}

// inPlayer: the block's cube overlaps the player's box (0.6 wide, 1.8 high).
func (g *Game) inPlayer(x, y, z int) bool {
	p := g.pos
	const e = 1e-6 // (tocando la cara no es estar dentro: AABB.intersects es estricto, y los dobles redondean)
	return float64(x) < p[0]+playerHalf-e && float64(x+1) > p[0]-playerHalf+e &&
		float64(y) < p[1]+playerHigh-e && float64(y+1) > p[1]+e &&
		float64(z) < p[2]+playerHalf-e && float64(z+1) > p[2]-playerHalf+e
}

// inMob: a living mob's box is in the cell.
func (g *Game) inMob(x, y, z int) bool {
	for _, m := range g.mobs {
		const e = 1e-6
		if m.dying == 0 && float64(x) < m.x+m.half-e && float64(x+1) > m.x-m.half+e && float64(y) < m.y+m.high-e && float64(y+1) > m.y+e &&
			float64(z) < m.z+m.half-e && float64(z+1) > m.z-m.half+e {
			return true
		}
	}
	return false
}

// takeHeld uses up one of the item in hand.
func (g *Game) takeHeld() {
	g.slots[36+g.held].n--
	if g.slots[36+g.held].n == 0 {
		g.slots[36+g.held] = slotItem{}
	}
}

// setAndAck changes a block (its plain state), tells the player and the
// others, and answers the use.
func (g *Game) setAndAck(x, y, z int, name string, seq pk.VarInt) error {
	g.S.World.Set(x, y, z, name)
	if err := g.S.Send(play.BlockUpdate{Pos: pk.Position{X: x, Y: y, Z: z}, BlockState: pk.VarInt(g.S.World.At(x, y, z))}); err != nil {
		return err
	}
	g.broadcast()
	return g.ack(seq)
}

// placeable is the block an item puts down: the blocks whose plain state is
// the right one (no facing, no half) — the others are not modelled.
func placeable(item string) (string, bool) {
	switch {
	case item == "minecraft:torch", item == "minecraft:crafting_table", item == "minecraft:furnace", item == "minecraft:chest", item == "minecraft:cobblestone", item == "minecraft:dirt", item == "minecraft:stone", item == "minecraft:obsidian", item == "minecraft:netherrack",
		item == "minecraft:white_bed", item == "minecraft:oak_fence", item == "minecraft:oak_fence_gate",
		item == "minecraft:oak_log", item == "minecraft:glass", item == "minecraft:stone_bricks", // PATCH (build)
		item == "minecraft:cobweb", // PATCH (arsenal.go)
		item == "minecraft:oak_stairs", item == "minecraft:stone_brick_stairs", item == "minecraft:oak_slab", item == "minecraft:stone_brick_slab",
		strings.HasSuffix(item, "_planks"):
		return item, true
	}
	return "", false
}

// faceOffset is the way a face (down, up, north, south, west, east) looks.
func faceOffset(face int) [3]int {
	return [...][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}[face]
}

// stateByText is the state the game writes as text (all properties).
func stateByText(text string) block.StateID {
	for i := range block.StateList {
		if world.StateString(block.StateID(i)) == text {
			return block.StateID(i)
		}
	}
	panic("no state " + text)
}

// lightPortal fills an obsidian frame (along x, its inside 2 wide and 3 high,
// with (x, y, z) in its bottom row) with nether_portal blocks, as fire lit in
// it does (PortalShape); a cell that is not such an inside lights nothing.
func (g *Game) lightPortal(x, y, z int) error {
	obs := func(x, y, z int) bool { return block.StateList[g.S.World.At(x, y, z)].ID() == "minecraft:obsidian" }
	for _, x0 := range []int{x - 1, x} { // the inside's left column
		ok := true
		for dx := 0; dx < 2 && ok; dx++ {
			ok = obs(x0+dx, y-1, z) && obs(x0+dx, y+3, z)
			for dy := 0; dy < 3 && ok; dy++ {
				ok = g.S.World.At(x0+dx, y+dy, z) == 0
			}
		}
		for dy := 0; dy < 3 && ok; dy++ {
			ok = obs(x0-1, y+dy, z) && obs(x0+2, y+dy, z)
		}
		if !ok {
			continue
		}
		for dx := 0; dx < 2; dx++ {
			for dy := 0; dy < 3; dy++ {
				g.S.World.SetState(x0+dx, y+dy, z, stateByText("minecraft:nether_portal[axis=x]"))
				if err := g.S.Send(play.BlockUpdate{Pos: pk.Position{X: x0 + dx, Y: y + dy, Z: z}, BlockState: pk.VarInt(g.S.World.At(x0+dx, y+dy, z))}); err != nil {
					return err
				}
			}
		}
		g.broadcast()
		return nil
	}
	return nil
}

// lookingOf is the way the player looks, on the level (Direction of its yaw).
func (g *Game) lookingOf() string {
	return map[string]string{"north": "south", "south": "north", "east": "west", "west": "east"}[g.facingOf()]
}

// isFluid: water or lava (any level): a block put there takes its place (BlockItem: the state is replaceable).
func isFluid(s block.StateID) bool {
	n := block.StateList[s].ID()
	return n == "minecraft:water" || n == "minecraft:lava"
}
