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
}

// thrown is a wind charge or a trident in flight.
type thrown struct {
	wind       bool
	x, y, z    float64
	vx, vy, vz float64
	age        int
	loyal      bool
}

// Arsenal says what the player did with the weapons (a test's look).
type Arsenal struct {
	Stabs, Lunges, Smashes, Winds, Lava, Webs, Fires, Tridents, TridentHits int
	SmashMax                                                                float64
}

// Arsenal is a test's look at the weapons used.
func (g *Game) Arsenal() Arsenal {
	a := &g.ars
	return Arsenal{Stabs: a.stabs, Lunges: a.lunges, Smashes: a.smashes, Winds: a.winds, Lava: a.lavaPoured,
		Webs: a.webs, Fires: a.fires, Tridents: a.tridents, TridentHits: a.tridentHit, SmashMax: a.smashMax}
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
		if !p.wind {
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
