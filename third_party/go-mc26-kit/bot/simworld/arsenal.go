package simworld

// PATCH (arsenal): the PvP weapons and tricks beyond the sword and the bow, each a little of what the game
// does — enough for the bot's combat tests to see a use work or fail:
//
//   - The spear (26.2): its jab is a STAB player action, the blow along the look to every mob between its
//     minimum and maximum reach (the attack_range component, the box grown by its hitbox margin), only fully
//     charged (minimum_attack_charge 1). Its charge is a use: after the delay, every tick the mobs in front
//     within reach whose speed toward them (the player's minus theirs, along the look, blocks a second) is at
//     least the damage condition's take that speed times the multiplier (kinetic_weapon), each once in ten
//     ticks.
//   - The mace: a blow falling more than 1.5 blocks adds 4 a block for the first 3, 2 a block for the next 5
//     and 1 a block after (MaceItem.getAttackDamageBonus), and the fall is forgotten.
//   - The wind charge: a use throws it along the look at 1.5 blocks a tick, no gravity; it bursts on the first
//     block or mob it meets (radius 1.2): who is within 2.4 is pushed from the burst along the way to its eyes,
//     by (1 − distance/2.4) × 1.22. The player thrown by its own charge takes no fall damage down to the height
//     it burst at. One use, 10 ticks to the next.
//   - The lava bucket: poured as the water bucket (bucket.go), the cell before the first block of the look.
//     A mob in lava takes 4 (in the ten ticks after a blow only the greater counts) and burns 15 s.
//   - The cobweb: placed as a block, also where a mob stands (it has no shape); a mob in one moves at a
//     quarter of its speed, and falls at a twentieth.
//   - Flint and steel: on a block's top, fire in the cell over it; a mob in fire takes 1 and burns 8 s.
//   - The trident: a use draws it, the release (after 10 ticks) throws it at 2.5 blocks a tick (gravity 0.05,
//     drag 0.99): 8 to the first mob it meets. With loyalty it comes back (it never leaves the inventory
//     here); without, it lies where it stopped, an item to pick up.

import (
	"math"
	"slices"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/entitydata"
	itemdata "github.com/mj41/go-mc26/data/item"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/block"
	"github.com/mj41/go-mc26/level/component"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

const (
	windSpeed     = 1.5
	windRadius    = 1.2
	windKnock     = 1.22
	windCooldown  = 10
	tridentSpeed  = 2.5
	tridentDamage = 8
	lavaDamage    = 4
)

// arsenalSim is the state of the weapons above.
type arsenalSim struct {
	vel        [3]float64 // the player's motion of its last move (the spear's charge)
	spearFrom  int        // the tick the spear's charge began (0: none)
	contact    map[int32]int
	windAt     int     // the tick of the last wind charge thrown
	windY      float64 // the player thrown by its own wind charge: no fall damage down to this height
	windOn     bool
	tridentAt  int // the tick the trident's draw began (0: none)
	flying     []*thrown
	stabs      int // jabs that hit
	lunges     int // charge hits
	smashes    int // mace blows with a fall
	smashMax   float64
	winds      int // wind charges thrown
	lavaPoured int
	webs       int
	fires      int
	tridents   int // tridents thrown
	tridentHit int
	// II
	crystals      []*crystal
	hook          *hook
	crystalBlasts int
	anchorBlasts  int
	pearls        int
	pearlLanded   int
	pulls         int
}

// thrown is a wind charge or a trident in flight.
type thrown struct {
	wind       bool
	pearl      bool
	x, y, z    float64
	vx, vy, vz float64
	age        int
	loyal      bool
}

// Arsenal says what the player did with the weapons (a test's look).
type Arsenal struct {
	Stabs, Lunges, Smashes, Winds, Lava, Webs, Fires, Tridents, TridentHits int
	SmashMax                                                                float64
	CrystalBlasts, AnchorBlasts, Pearls, PearlLanded, Pulls                 int
}

// Arsenal is a test's look at the weapons used.
func (g *Game) Arsenal() Arsenal {
	a := &g.ars
	return Arsenal{Stabs: a.stabs, Lunges: a.lunges, Smashes: a.smashes, Winds: a.winds, Lava: a.lavaPoured,
		Webs: a.webs, Fires: a.fires, Tridents: a.tridents, TridentHits: a.tridentHit, SmashMax: a.smashMax,
		CrystalBlasts: a.crystalBlasts, AnchorBlasts: a.anchorBlasts, Pearls: a.pearls, PearlLanded: a.pearlLanded, Pulls: a.pulls}
}

func heldComponent[T component.DataComponent](item string) T {
	return itemdata.DefaultComponent[T](itemdata.ID(slices.Index(registryid.Item, item)))
}

// look is the way the player looks (a unit vector).
func (g *Game) look() [3]float64 {
	yaw, pitch := g.yaw*math.Pi/180, g.pitch*math.Pi/180
	return [3]float64{-math.Sin(yaw) * math.Cos(pitch), -math.Sin(pitch), math.Cos(yaw) * math.Cos(pitch)}
}

// useArsenal is a UseItem with one of the weapons above in hand; it says whether it was one.
func (g *Game) useArsenal(u play.UseItem) (bool, error) {
	held := g.heldItem()
	switch {
	case heldComponent[*component.KineticWeapon](held.item) != nil:
		g.ars.spearFrom = g.Tick
		return true, g.ack(u.Sequence)
	case held.item == "minecraft:wind_charge":
		if g.Tick-g.ars.windAt < windCooldown && g.ars.windAt > 0 {
			return true, g.ack(u.Sequence)
		}
		g.ars.windAt = g.Tick
		g.yaw, g.pitch = float64(u.YRot), float64(u.XRot)
		d := g.look()
		g.takeHeld()
		g.broadcast()
		g.ars.winds++
		g.ars.flying = append(g.ars.flying, &thrown{wind: true, x: g.pos[0], y: g.pos[1] + playerEye - 0.1, z: g.pos[2],
			vx: d[0] * windSpeed, vy: d[1] * windSpeed, vz: d[2] * windSpeed})
		return true, g.ack(u.Sequence)
	case held.item == "minecraft:lava_bucket":
		return true, g.pourLava(u)
	case held.item == "minecraft:trident":
		g.ars.tridentAt = g.Tick
		return true, g.ack(u.Sequence)
	}
	if ok, err := g.throwPearl(u); ok || err != nil {
		return ok, err
	}
	if ok, err := g.castRod(u); ok || err != nil {
		return ok, err
	}
	return false, nil
}

// releaseArsenal is a RELEASE_USE_ITEM: the spear's charge ends; a trident drawn 10 ticks is thrown.
func (g *Game) releaseArsenal() error {
	g.ars.spearFrom = 0
	if g.ars.tridentAt == 0 {
		return nil
	}
	drawn := g.Tick - g.ars.tridentAt
	g.ars.tridentAt = 0
	if drawn < 10 || g.heldItem().item != "minecraft:trident" {
		return nil
	}
	loyal := level(g.heldItem().ench, "loyalty") > 0
	if !loyal {
		g.takeHeld()
		g.broadcast()
	}
	d := g.look()
	g.ars.tridents++
	g.ars.flying = append(g.ars.flying, &thrown{x: g.pos[0], y: g.pos[1] + playerEye - 0.1, z: g.pos[2],
		vx: d[0] * tridentSpeed, vy: d[1] * tridentSpeed, vz: d[2] * tridentSpeed, loyal: loyal})
	return nil
}

// pourLava pours the lava bucket as the water bucket pours (bucket.go).
func (g *Game) pourLava(u play.UseItem) error {
	yaw, pitch := float64(u.YRot)*math.Pi/180, float64(u.XRot)*math.Pi/180
	dx, dy, dz := -math.Sin(yaw)*math.Cos(pitch), -math.Sin(pitch), math.Cos(yaw)*math.Cos(pitch)
	ex, ey, ez := g.pos[0], g.pos[1]+playerEye, g.pos[2]
	prev := [3]int{int(math.Floor(ex)), int(math.Floor(ey)), int(math.Floor(ez))}
	for d := 0.0; d <= bucketReach; d += 0.05 {
		c := [3]int{int(math.Floor(ex + dx*d)), int(math.Floor(ey + dy*d)), int(math.Floor(ez + dz*d))}
		if c == prev && d > 0 {
			continue
		}
		if len(block.CollisionShape(g.S.World.At(c[0], c[1], c[2]))) > 0 {
			at := g.S.World.At(prev[0], prev[1], prev[2])
			if at == 0 || block.StateList[at].ID() == "minecraft:cobweb" || block.StateList[at].ID() == "minecraft:fire" {
				g.S.World.Set(prev[0], prev[1], prev[2], "minecraft:lava")
				if err := g.S.Send(play.BlockUpdate{Pos: pk.Position{X: prev[0], Y: prev[1], Z: prev[2]}, BlockState: pk.VarInt(g.S.World.At(prev[0], prev[1], prev[2]))}); err != nil {
					return err
				}
				g.swapHeld("minecraft:bucket")
				g.ars.lavaPoured++
			}
			return g.ack(u.Sequence)
		}
		prev = c
	}
	return g.ack(u.Sequence)
}

// lightFire: flint and steel on a block's face lights the cell before it, if it is air.
func (g *Game) lightFire(x, y, z int) error {
	if g.S.World.At(x, y, z) != 0 {
		return nil
	}
	g.S.World.Set(x, y, z, "minecraft:fire")
	g.ars.fires++
	return g.S.Send(play.BlockUpdate{Pos: pk.Position{X: x, Y: y, Z: z}, BlockState: pk.VarInt(g.S.World.At(x, y, z))})
}

// stab is the spear's jab (the STAB player action).
func (g *Game) stab() error {
	held := g.heldItem()
	r := heldComponent[*component.AttackRange](held.item)
	if r == nil || heldComponent[*component.PiercingWeapon](held.item) == nil {
		return nil
	}
	dmg, speed := g.heldAttack()
	if l := level(held.ench, "sharpness"); l > 0 {
		dmg += 0.5*float64(l) + 0.5
	}
	scale := math.Min(1, (float64(g.Tick-g.lastSwing)+0.5)/(20/speed))
	if scale < 1 {
		return nil // (minimum_attack_charge 1: nothing)
	}
	g.lastSwing = g.Tick
	eye := [3]float64{g.pos[0], g.pos[1] + playerEye, g.pos[2]}
	d := g.look()
	far := float64(r.MaxReach)
	ray := [3]float64{d[0] * far, d[1] * far, d[2] * far}
	tBlock, hitBlock := g.clipBlocks(eye, [3]float64{eye[0] + ray[0], eye[1] + ray[1], eye[2] + ray[2]})
	m := float64(r.HitboxMargin)
	for _, o := range append([]*mob(nil), g.mobs...) {
		if o.dying > 0 {
			continue
		}
		lo := [3]float64{o.x - o.half - m, o.y - m, o.z - o.half - m}
		hi := [3]float64{o.x + o.half + m, o.y + o.high + m, o.z + o.half + m}
		t, ok := rayBox(eye, ray, lo, hi)
		if !ok || hitBlock && t > tBlock || t*far < float64(r.MinReach) {
			continue
		}
		g.ars.stabs++
		if err := g.blowMob(o, dmg, 0.4, d); err != nil {
			return err
		}
	}
	return nil
}

// blowMob is a blow of the player on the mob: its armor, the ten ticks after a blow, the knockback along way.
func (g *Game) blowMob(m *mob, dmg, knock float64, way [3]float64) error {
	dmg = m.def.reduce(dmg)
	if m.invuln > 10 {
		if float32(dmg) <= m.lastHurt {
			return nil
		}
		dmg -= float64(m.lastHurt)
	}
	m.lastHurt, m.invuln = float32(dmg), 20
	m.health -= float32(dmg)
	m.playerHitAt = g.Tick
	if n := math.Hypot(way[0], way[2]); n > 1e-4 && knock > 0 {
		m.vx, m.vz = m.vx/2+way[0]/n*knock, m.vz/2+way[2]/n*knock
		if m.onGround {
			m.vy = math.Min(0.4, m.vy/2+0.4)
		}
	}
	if err := g.S.Send(play.DamageEvent{EntityID: pk.VarInt(m.id), SourceType: damageTypeID("minecraft:player_attack"), SourceCauseID: 2, SourceDirectID: 2}); err != nil {
		return err
	}
	if m.health <= 0 {
		return g.killMob(m, g.killContext(m))
	}
	return g.S.Send(play.EntityEvent{EntityID: pk.Int(m.id), EventID: 2})
}

// smashBonus is the mace's for a fall (MaceItem.getAttackDamageBonus).
func smashBonus(fall float64) float64 {
	if fall <= 1.5 {
		return 0
	}
	return 4*math.Min(fall, 3) + 2*math.Min(math.Max(fall-3, 0), 5) + math.Max(fall-8, 0)
}

// arsenalTick is the weapons' tick: the spear's charge, what flies, the mobs in lava and fire.
func (g *Game) arsenalTick() error {
	if err := g.lungeTick(); err != nil {
		return err
	}
	if err := g.thrownTick(); err != nil {
		return err
	}
	if err := g.hookTick(); err != nil {
		return err
	}
	if err := g.glideTick(); err != nil {
		return err
	}
	for _, m := range append([]*mob(nil), g.mobs...) {
		if m.dying > 0 {
			continue
		}
		lava, fire := false, false
		for x := int(math.Floor(m.x - m.half)); x <= int(math.Floor(m.x+m.half)); x++ {
			for z := int(math.Floor(m.z - m.half)); z <= int(math.Floor(m.z+m.half)); z++ {
				for y := int(math.Floor(m.y)); y <= int(math.Floor(m.y+m.high-0.01)); y++ {
					n := block.StateList[g.S.World.At(x, y, z)].ID()
					lava = lava || n == "minecraft:lava"
					fire = fire || n == "minecraft:fire"
				}
			}
		}
		switch {
		case lava:
			m.fire = max(m.fire, 300)
			if _, err := g.hurtMob(m, lavaDamage, "minecraft:on_fire"); err != nil {
				return err
			}
		case fire:
			m.fire = max(m.fire, 160)
			if _, err := g.hurtMob(m, 1, "minecraft:on_fire"); err != nil {
				return err
			}
		}
	}
	return nil
}

// inWeb: the mob's box is in a cobweb.
func (g *Game) inWeb(m *mob) bool {
	for x := int(math.Floor(m.x - m.half)); x <= int(math.Floor(m.x+m.half)); x++ {
		for z := int(math.Floor(m.z - m.half)); z <= int(math.Floor(m.z+m.half)); z++ {
			for y := int(math.Floor(m.y)); y <= int(math.Floor(m.y+m.high-0.01)); y++ {
				if block.StateList[g.S.World.At(x, y, z)].ID() == "minecraft:cobweb" {
					return true
				}
			}
		}
	}
	return false
}

// lungeTick is the spear's charge.
func (g *Game) lungeTick() error {
	held := g.heldItem()
	k := heldComponent[*component.KineticWeapon](held.item)
	if g.ars.spearFrom == 0 || k == nil {
		g.ars.spearFrom = 0
		return nil
	}
	t := g.Tick - g.ars.spearFrom
	if t < int(k.DelayTicks) {
		return nil
	}
	if g.ars.contact == nil {
		g.ars.contact = map[int32]int{}
	}
	r := heldComponent[*component.AttackRange](held.item)
	far := 4.5
	if r != nil {
		far = float64(r.MaxReach)
	}
	d := g.look()
	eye := [3]float64{g.pos[0], g.pos[1] + playerEye, g.pos[2]}
	for _, m := range append([]*mob(nil), g.mobs...) {
		if m.dying > 0 || g.Tick-g.ars.contact[m.id] < 10 && g.ars.contact[m.id] > 0 {
			continue
		}
		c := [3]float64{m.x - eye[0], m.y + m.high/2 - eye[1], m.z - eye[2]}
		dist := math.Sqrt(c[0]*c[0] + c[1]*c[1] + c[2]*c[2])
		if dist > far+m.half || (c[0]*d[0]+c[1]*d[1]+c[2]*d[2])/math.Max(dist, 1e-6) < 0.7 {
			continue
		}
		rel := ((g.ars.vel[0]-m.vx)*d[0] + (g.ars.vel[1]-m.vy)*d[1] + (g.ars.vel[2]-m.vz)*d[2]) * 20
		cond := k.DamageConditions
		if !cond.Has || t-int(k.DelayTicks) > int(cond.Val.MaxDurationTicks) || rel < float64(cond.Val.MinRelativeSpeed) {
			continue
		}
		g.ars.contact[m.id] = g.Tick
		g.ars.lunges++
		dmg, _ := g.heldAttack()
		if err := g.blowMob(m, dmg+rel*float64(k.DamageMultiplier), 0.6, d); err != nil {
			return err
		}
	}
	return nil
}

// thrownTick moves the wind charges and the tridents.
func (g *Game) thrownTick() error {
	for i := 0; i < len(g.ars.flying); i++ {
		p := g.ars.flying[i]
		p.age++
		from := [3]float64{p.x, p.y, p.z}
		to := [3]float64{p.x + p.vx, p.y + p.vy, p.z + p.vz}
		way := [3]float64{p.vx, p.vy, p.vz}
		tBlock, hitBlock := g.clipBlocks(from, to)
		var hit *mob
		bestT := math.Inf(1)
		for _, m := range g.mobs {
			if m.dying > 0 {
				continue
			}
			lo := [3]float64{m.x - m.half - 0.3, m.y - 0.3, m.z - m.half - 0.3}
			hi := [3]float64{m.x + m.half + 0.3, m.y + m.high + 0.3, m.z + m.half + 0.3}
			if t, ok := rayBox(from, way, lo, hi); ok && (!hitBlock || t <= tBlock) && t < bestT {
				hit, bestT = m, t
			}
		}
		gone := false
		switch {
		case p.pearl && (hit != nil || hitBlock):
			t := tBlock
			if hit != nil {
				t = bestT
			}
			t = math.Max(0, t-0.05)
			at := [3]float64{p.x + way[0]*t, p.y + way[1]*t, p.z + way[2]*t}
			if hit == nil { // (on a block's top: stand on it)
				if c := [3]int{int(math.Floor(at[0])), int(math.Floor(at[1] - 0.1)), int(math.Floor(at[2]))}; len(block.CollisionShape(g.S.World.At(c[0], c[1], c[2]))) > 0 {
					at[1] = float64(c[1] + 1)
				}
			}
			if err := g.pearlLands(at); err != nil {
				return err
			}
			gone = true
		case hit != nil:
			at := [3]float64{p.x + way[0]*bestT, p.y + way[1]*bestT, p.z + way[2]*bestT}
			if p.wind {
				if err := g.windBurst(at); err != nil {
					return err
				}
			} else {
				g.ars.tridentHit++
				if _, err := g.hurtMob(hit, float32(hit.def.reduce(tridentDamage)), "minecraft:player_attack"); err != nil {
					return err
				}
				hit.playerHitAt = g.Tick
				if !p.loyal {
					if err := g.addItem(at[0], at[1], at[2], 0, 0.1, 0, slotItem{item: "minecraft:trident", n: 1}, 0); err != nil {
						return err
					}
				}
			}
			gone = true
		case hitBlock:
			back := math.Max(0, tBlock-0.05)
			at := [3]float64{p.x + way[0]*back, p.y + way[1]*back, p.z + way[2]*back}
			if p.wind {
				if err := g.windBurst(at); err != nil {
					return err
				}
			} else if !p.loyal {
				if err := g.addItem(at[0], at[1], at[2], 0, 0, 0, slotItem{item: "minecraft:trident", n: 1}, 0); err != nil {
					return err
				}
			}
			gone = true
		}
		if gone || p.age > 200 {
			g.ars.flying = slices.Delete(g.ars.flying, i, i+1)
			i--
			continue
		}
		p.x, p.y, p.z = to[0], to[1], to[2]
		switch {
		case p.pearl:
			p.vx, p.vy, p.vz = p.vx*0.99, p.vy*0.99-0.03, p.vz*0.99
		case !p.wind:
			p.vx, p.vy, p.vz = p.vx*0.99, p.vy*0.99-0.05, p.vz*0.99
		}
	}
	return nil
}

// windBurst is a wind charge's burst at c: who is near is pushed away (to the eyes).
func (g *Game) windBurst(c [3]float64) error {
	reach := 2 * windRadius
	push := func(x, y, z, eye float64) ([3]float64, bool) {
		d := math.Sqrt(sq(x-c[0]) + sq(y-c[1]) + sq(z-c[2]))
		if d > reach {
			return [3]float64{}, false
		}
		w := [3]float64{x - c[0], y + eye - c[1], z - c[2]}
		n := math.Sqrt(w[0]*w[0] + w[1]*w[1] + w[2]*w[2])
		if n < 1e-6 {
			return [3]float64{}, false
		}
		f := (1 - d/reach) * windKnock
		return [3]float64{w[0] / n * f, w[1] / n * f, w[2] / n * f}, true
	}
	if v, ok := push(g.pos[0], g.pos[1], g.pos[2], playerEye); ok && !g.dead {
		g.ars.windOn, g.ars.windY = true, c[1]
		g.fall = 0
		if err := g.S.Send(play.SetEntityMotion{ID: 1, Movement: types.LpVec3{X: v[0], Y: v[1], Z: v[2]}}); err != nil {
			return err
		}
	}
	for _, m := range g.mobs {
		if m.dying > 0 {
			continue
		}
		if v, ok := push(m.x, m.y, m.z, m.high*0.85); ok {
			m.vx, m.vy, m.vz = m.vx+v[0], m.vy+v[1], m.vz+v[2]
			m.onGround = false
		}
	}
	return nil
}

// PATCH (arsenal, II): end crystals, respawn anchors, the ender pearl, the fishing rod; an elytra taken off
// ends the glide.
//
//   - An end crystal used on the top of obsidian or bedrock, with the two cells over it free of blocks and of
//     anyone (EndCrystalItem.useOn), is an entity there; a blow on it is a blast of 6 where it stands, and it
//     is gone (EndCrystal.hurt).
//   - A respawn anchor placed is a block with no charge; glowstone used on it charges it (to 4); anything else
//     used on it charged blows it up — out of the Nether — with a blast of 5 at its centre (RespawnAnchorBlock).
//   - An ender pearl thrown (1.5 a tick, gravity 0.03, drag 0.99) takes the player where it lands, with 5 of
//     damage (ThrownEnderpearl.onHit).
//   - A fishing rod cast throws the hook (about 1.1 a tick, gravity 0.03, drag 0.92) that hooks the first mob
//     it meets and rides it; used again it comes back, pulling a hooked mob toward the player by a tenth of
//     the way (FishingHook.pullEntity). The bobber is told as an entity, its owner in the add packet's data
//     and the mob it hooked in its data (id + 1).

type crystal struct {
	id      int32
	x, y, z float64
}

type hook struct {
	id         int32
	x, y, z    float64
	vx, vy, vz float64
	hooked     *mob
	ground     bool
}

const crystalPower, anchorPower = 6.0, 5.0

func (g *Game) addEntity(kind string, x, y, z float64, data int32) (int32, error) {
	g.nextID++
	id := g.nextID
	var u pk.UUID
	u[15], u[14] = byte(id), byte(id>>8)
	u[0] = 4
	return id, g.S.Send(play.AddEntity{ID: pk.VarInt(id), UUID: u, Type: pk.VarInt(slices.Index(registryid.EntityType, kind)),
		X: pk.Double(x), Y: pk.Double(y), Z: pk.Double(z), Data: pk.VarInt(data)})
}

// placeCrystal is an end crystal used on (x, y, z) from the face; it says whether it was one.
func (g *Game) placeCrystal(x, y, z, face int, seq pk.VarInt) (bool, error) {
	if g.heldItem().item != "minecraft:end_crystal" {
		return false, nil
	}
	base := block.StateList[g.S.World.At(x, y, z)].ID()
	if face != 1 || base != "minecraft:obsidian" && base != "minecraft:bedrock" || g.S.World.At(x, y+1, z) != 0 || g.S.World.At(x, y+2, z) != 0 {
		return true, g.ack(seq)
	}
	box := func(px, py, pz, half, high float64) bool {
		return px+half > float64(x) && px-half < float64(x+1) && pz+half > float64(z) && pz-half < float64(z+1) &&
			py+high > float64(y+1) && py < float64(y+3)
	}
	if box(g.pos[0], g.pos[1], g.pos[2], playerHalf, playerHigh) {
		return true, g.ack(seq)
	}
	for _, m := range g.mobs {
		if m.dying == 0 && box(m.x, m.y, m.z, m.half, m.high) {
			return true, g.ack(seq)
		}
	}
	c := &crystal{x: float64(x) + 0.5, y: float64(y + 1), z: float64(z) + 0.5}
	id, err := g.addEntity("minecraft:end_crystal", c.x, c.y, c.z, 0)
	if err != nil {
		return true, err
	}
	c.id = id
	g.ars.crystals = append(g.ars.crystals, c)
	g.takeHeld()
	g.broadcast()
	return true, g.ack(seq)
}

// hitCrystal: a blow on a crystal blows it up; it says whether id was one.
func (g *Game) hitCrystal(id int32) (bool, error) {
	for i, c := range g.ars.crystals {
		if c.id != id {
			continue
		}
		g.ars.crystals = slices.Delete(g.ars.crystals, i, i+1)
		g.ars.crystalBlasts++
		if err := g.S.Send(play.RemoveEntities{EntityIds: []pk.VarInt{pk.VarInt(id)}}); err != nil {
			return true, err
		}
		return true, g.explode([3]float64{c.x, c.y, c.z}, crystalPower, &mob{id: -1, kind: "minecraft:end_crystal"})
	}
	return false, nil
}

// useAnchor is a use on a respawn anchor: glowstone charges it, anything else blows a charged one up.
func (g *Game) useAnchor(pos pk.Position, seq pk.VarInt) error {
	st := world.StateString(g.S.World.At(pos.X, pos.Y, pos.Z))
	charges := 0
	if i := strings.Index(st, "charges="); i >= 0 {
		charges = int(st[i+8] - '0')
	}
	if g.heldItem().item == "minecraft:glowstone" && charges < 4 {
		g.takeHeld()
		g.S.World.SetState(pos.X, pos.Y, pos.Z, stateByText("minecraft:respawn_anchor[charges="+string(rune('0'+charges+1))+"]"))
		if err := g.S.Send(play.BlockUpdate{Pos: pos, BlockState: pk.VarInt(g.S.World.At(pos.X, pos.Y, pos.Z))}); err != nil {
			return err
		}
		g.broadcast()
		return g.ack(seq)
	}
	if charges == 0 {
		return g.ack(seq)
	}
	g.S.World.Set(pos.X, pos.Y, pos.Z, "minecraft:air")
	if err := g.S.Send(play.BlockUpdate{Pos: pos, BlockState: 0}); err != nil {
		return err
	}
	g.ars.anchorBlasts++
	if err := g.explode([3]float64{float64(pos.X) + 0.5, float64(pos.Y) + 0.5, float64(pos.Z) + 0.5}, anchorPower, &mob{id: -1, kind: "minecraft:respawn_anchor"}); err != nil {
		return err
	}
	return g.ack(seq)
}

// throwPearl and castRod are their UseItems; they say whether the hand held one.
func (g *Game) throwPearl(u play.UseItem) (bool, error) {
	if g.heldItem().item != "minecraft:ender_pearl" {
		return false, nil
	}
	g.yaw, g.pitch = float64(u.YRot), float64(u.XRot)
	d := g.look()
	g.takeHeld()
	g.broadcast()
	g.ars.pearls++
	g.ars.flying = append(g.ars.flying, &thrown{pearl: true, x: g.pos[0], y: g.pos[1] + playerEye - 0.1, z: g.pos[2],
		vx: d[0] * 1.5, vy: d[1] * 1.5, vz: d[2] * 1.5})
	return true, g.ack(u.Sequence)
}

func (g *Game) castRod(u play.UseItem) (bool, error) {
	if g.heldItem().item != "minecraft:fishing_rod" {
		return false, nil
	}
	if h := g.ars.hook; h != nil { // reeled in
		g.ars.hook = nil
		if h.hooked != nil && h.hooked.dying == 0 {
			m := h.hooked
			m.vx += (g.pos[0] - m.x) * 0.1
			m.vy += (g.pos[1] - m.y) * 0.1
			m.vz += (g.pos[2] - m.z) * 0.1
			m.onGround = false
			g.ars.pulls++
		}
		if err := g.S.Send(play.RemoveEntities{EntityIds: []pk.VarInt{pk.VarInt(h.id)}}); err != nil {
			return true, err
		}
		return true, g.ack(u.Sequence)
	}
	g.yaw, g.pitch = float64(u.YRot), float64(u.XRot)
	d := g.look()
	h := &hook{x: g.pos[0], y: g.pos[1] + playerEye, z: g.pos[2], vx: d[0] * 1.1, vy: d[1] * 1.1, vz: d[2] * 1.1}
	id, err := g.addEntity("minecraft:fishing_bobber", h.x, h.y, h.z, 1)
	if err != nil {
		return true, err
	}
	h.id = id
	g.ars.hook = h
	return true, g.ack(u.Sequence)
}

// hookTick moves the hook, or keeps it on the mob it hooked.
func (g *Game) hookTick() error {
	h := g.ars.hook
	if h == nil {
		return nil
	}
	switch {
	case h.hooked != nil:
		if h.hooked.dying > 0 {
			h.hooked = nil
			break
		}
		h.x, h.y, h.z = h.hooked.x, h.hooked.y+h.hooked.high*0.8, h.hooked.z
	case !h.ground:
		from := [3]float64{h.x, h.y, h.z}
		way := [3]float64{h.vx, h.vy, h.vz}
		tBlock, hitBlock := g.clipBlocks(from, [3]float64{h.x + h.vx, h.y + h.vy, h.z + h.vz})
		for _, m := range g.mobs {
			if m.dying > 0 {
				continue
			}
			lo := [3]float64{m.x - m.half - 0.1, m.y - 0.1, m.z - m.half - 0.1}
			hi := [3]float64{m.x + m.half + 0.1, m.y + m.high + 0.1, m.z + m.half + 0.1}
			if t, ok := rayBox(from, way, lo, hi); ok && (!hitBlock || t <= tBlock) {
				h.hooked = m
				if err := g.S.Send(play.SetEntityData{ID: pk.VarInt(h.id), PackedItems: types.EntityData{
					{Index: entitydata.FishingHookHookedEntity, Serializer: 1, Value: ptr(pk.VarInt(m.id + 1))}}}); err != nil {
					return err
				}
				break
			}
		}
		if h.hooked == nil {
			if hitBlock {
				h.ground = true
				h.x, h.y, h.z = h.x+way[0]*tBlock, h.y+way[1]*tBlock, h.z+way[2]*tBlock
			} else {
				h.x, h.y, h.z = h.x+h.vx, h.y+h.vy, h.z+h.vz
				h.vx, h.vy, h.vz = h.vx*0.92, h.vy*0.92-0.03, h.vz*0.92
			}
		}
	}
	return g.S.Send(positionSync(h.id, h.x, h.y, h.z, 0, 0, h.ground))
}

// pearlLands takes the player where its pearl landed.
func (g *Game) pearlLands(at [3]float64) error {
	g.tp++
	g.pos, g.fall, g.tpWait = at, 0, true
	g.ars.pearlLanded++
	if err := g.S.Send(play.PlayerPosition{ID: pk.VarInt(g.tp), Change: types.PositionMoveRotation{Position: types.Vec3{X: pk.Double(at[0]), Y: pk.Double(at[1]), Z: pk.Double(at[2])}}}); err != nil {
		return err
	}
	return g.hurt(5)
}

// glideTick: a glide with no elytra worn ends (LivingEntity.updateFallFlying: canGlide).
func (g *Game) glideTick() error {
	if g.gliding && g.Worn(6) != "minecraft:elytra" {
		g.gliding = false
		return g.tellGlide()
	}
	return nil
}
