package simworld

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26/data/entity"
	"github.com/mj41/go-mc26/data/entitydata"
	itemdata "github.com/mj41/go-mc26/data/item"
	"github.com/mj41/go-mc26/data/loot"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/block"
	"github.com/mj41/go-mc26/level/component"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// Mobs: the hostile ones the bench's script puts near the bot (mc26 gen/internal/e2e/botday.go), a kind at a
// time, each a little of what the game does — a zombie walks to the player and hits, a creeper walks to it and
// blows up (creeper.go), a skeleton keeps its distance and shoots (skeleton.go); each is hit back and dies.
// What a kind is lies in one table here: its speed measured on the real server, the rest the game's own
// values, which are in its code and not in the data of the version (the sizes are: data/entity). A kind that
// has no entry is not modelled, and summoning it fails the game.

// mobKind is what a kind of mob is.
type mobKind struct {
	speed  float64 // blocks a tick on the ground, walking to the player: measured on the real server (the bench's tick events, the mode of the chasing steps)
	follow float64 // the player farther than this (centre to centre) is no target: it stops (the follow range attribute, TargetGoal.canContinueToUse); 0: any
	health float32 // from the game's code, to be extracted
	damage float32 // a blow, at the normal difficulty (0: it does not hit): from the game's code, to be extracted
	reach  float64 // how near, centre to centre, it hits (MeleeAttackGoal: the width twice, squared, and the target's): from the game's code, to be extracted
	fuse   *fuse   // a creeper's
	bow    *bow    // a skeleton's
	// sunburn: it catches fire in daylight under the open sky (isSunSensitive, sun.go): from the game's code, to be extracted
	sunburn bool
	baby    *baby // its young, when it has them
	// passive: it never walks to the player to hit; it follows one holding its food (TemptGoal, sheep.go)
	passive bool
	// interval: ticks between its blows (0: a mob's 20); knock: the knockback its blow gives the player
	// (0: none — a mob's own push is not modelled)
	interval int
	knock    float64
}

// baby is how a kind's young differ (Zombie.finalizeSpawn, its baby modifiers): from the game's code, to be extracted.
type baby struct {
	odds  float64 // the share of spawns that are babies (getSpawnAsBabyOdds: a float under 0.05)
	speed float64 // the speed times this (the baby speed modifier: +50 % of the base)
	scale float64 // its box and eyes times this (the baby's scale)
}

// fuse is a creeper's (Creeper, SwellGoal): from the game's code, to be extracted.
type fuse struct {
	ticks int     // lit to the blast (maxSwell)
	start float64 // the player nearer than this lights it (the goal's distance squared under 9)
	stop  float64 // the player farther than this, or out of its sight, puts it out (squared over 49)
	power float64 // the blast's radius (explosionRadius; twice for a charged creeper, not modelled)
}

// bow is a skeleton's (AbstractSkeleton, RangedBowAttackGoal, ProjectileUtil.getMobArrow): from the game's
// code, to be extracted.
type bow struct {
	interval int     // ticks from a shot to the next draw (attackIntervalMin: 40 at the normal difficulty, 20 at the hard)
	draw     int     // ticks the bow is drawn before it shoots (20: full power)
	radius   float64 // within it shoots and strafes; farther it walks to the player (attackRadius)
	eye      float64 // its eyes' height; the arrow leaves 0.1 under them
	speed    float64 // the arrow's speed at the shot, blocks a tick
	spread   float64 // the shot's inaccuracy (14 − 4 × the difficulty's id: the normal's is 2)
	damage   float64 // the arrow's base damage at full power (a hit is its speed times it, rounded up)
}

var mobKinds = map[string]mobKind{
	// Follow ranges (Attributes.FOLLOW_RANGE): a zombie's 35, a creeper's and a skeleton's 16.
	// Zombie: 20 health, attack damage 3; the melee goal hits at (2 × 0.6)² + 0.6 = 2.04 squared.
	// Its speed is measured: the bench's 543 chasing steps of 118 zombies, most in 0.115–0.120 a tick.
	// It burns in daylight; one spawn in twenty is a baby, half its size, half again as fast (and burns too:
	// the game's Zombie.aiStep asks no age of it).
	// The duelist: a player as a test's opponent — not the game's (a player has no AI), but an average
	// one in a fight: it sprints at the bot (0.28 a tick: a sprint), hits as soon as its sword is charged
	// (an iron sword: 6, at 1.6 a second, every 13 ticks) within the player's reach (3 blocks, eyes to box:
	// some 3.3 centre to centre), and every blow sprint-knocks (0.5: the knockback and the sprint's level).
	"minecraft:player": {speed: 0.28, follow: 64, health: 20, damage: 6, reach: 3.3, interval: 13, knock: 0.5},
	"minecraft:zombie": {speed: 0.1175, follow: 35, health: 20, damage: 3, reach: 1.43, sunburn: true, baby: &baby{odds: 0.05, speed: 1.5, scale: 0.5}},
	// Creeper: 20 health, no blow; lit within 3 blocks, out beyond 7, 30 ticks to the blast of radius 3.
	// Its speed is measured: the bench's 1408 chasing steps, the most (502) in 0.085–0.090 a tick.
	"minecraft:creeper": {speed: 0.0875, follow: 16, health: 20, fuse: &fuse{ticks: 30, start: 3, stop: 7, power: 3}},
	// Skeleton: 20 health, no blow; a bow drawn 20 ticks, 40 between shots, within 15 blocks; its eyes at 1.74;
	// the arrow at 1.6 blocks a tick, inaccuracy 6, base damage 2.
	// It burns in daylight (AbstractSkeleton: sun sensitive).
	// Its speed is measured: the bench's 1124 chasing steps, the most (256) in 0.135–0.140 a tick.
	// Sheep: 8 health, passive; tempted by wheat within 10 blocks, it walks after the player (1.1 of its
	// speed 0.23 attribute; ~0.1 a block a tick) and stops 2.5 off (sheep.go).
	"minecraft:sheep": {speed: 0.1, health: 8, passive: true},
	// Cow, pig (10 health), chicken (4): passive (tempting modelled as the sheep's: wheat).
	"minecraft:cow":      {speed: 0.1, health: 10, passive: true},
	"minecraft:pig":      {speed: 0.1, health: 10, passive: true},
	"minecraft:chicken":  {speed: 0.1, health: 4, passive: true},
	"minecraft:skeleton": {speed: 0.1375, follow: 16, health: 20, bow: &bow{interval: 40, draw: 20, radius: 15, eye: 1.74, speed: 1.6, spread: 6, damage: 2}, sunburn: true},
}

// The rest of the game's own values the mobs need, from the game's code, to be extracted.
const (
	arrowGravity  = 0.05 // AbstractArrow: a tick's fall of speed
	arrowDrag     = 0.99 // the speed kept a tick in the air
	arrowLife     = 1200 // ticks an arrow lies in the ground before it is gone
	arrowInflate  = 0.3  // an arrow hits an entity whose box, so much larger each way, it passes through
	playerHalf    = 0.3  // the player's box: 0.6 wide, 1.8 high, its eyes at 1.62
	playerHigh    = 1.8
	playerEye     = 1.62
	knockStrength = 0.4 // LivingEntity.knockback of a hit
	sunFireTicks  = 160 // a sunburn sets it on fire for 8 seconds (igniteForSeconds), renewed while it burns on
	fireEvery     = 20  // a burning entity takes fireDamage every 20 ticks of its fire
	fireDamage    = 1
)

// damageTypes are the damage types the small game's registry holds, in the order of their ids (fakeserver's registries).
var damageTypes = []string{"minecraft:generic", "minecraft:mob_attack", "minecraft:player_attack", "minecraft:fall", "minecraft:arrow", "minecraft:explosion", "minecraft:on_fire"}

func damageTypeID(name string) pk.VarInt { return pk.VarInt(slices.Index(damageTypes, name)) }

// mob is a mob in the world.
type mob struct {
	id         int32
	kind       string
	k          mobKind
	x, y, z    float64
	vx, vy, vz float64 // the motion of the last tick (the knockback it was given, the push of its own walk)
	yaw        float64
	half, high float64
	health     float32
	onGround   bool
	cooldown   int     // ticks to its next blow
	invuln     int     // ticks it takes no blow (hurtTime: 20 after a hit, a hit counts again after 10)
	lastHurt   float32 // the blow it was last given
	jump       bool    // it was stopped by a block last tick: it jumps if it stands
	dying      int     // ticks since it died (it lies 20 ticks, then is gone)
	// a creeper's fuse (creeper.go)
	swell, swellDir int
	// a skeleton's bow and steps (skeleton.go)
	seeTime, strafing, attackTime, using int
	clockwise, backwards                 bool
	// sun.go
	isBaby      bool
	fire        int  // ticks it burns on (remainingFireTicks)
	toldFire    bool // on fire as the client was last told
	playerHitAt int  // the tick the player last hit it (0: never): its rare drops need a hit within 100
	// sheep.go
	sheared bool
	love    int // ticks it is in love on (fed wheat)
	age     int // a baby's ticks to grow up
	// pvpkit.go: a duelist's armor; its ticks to break a block between it and the player, and the ticks it
	// has not seen the player
	def        defense
	breakTicks int
	blind      int
	tricks     *tricks // PATCH (tricks.go): a dirty duelist's
}

// Summon puts a mob of the kind on the ground at x, z (its top block), as the harness's "summon … over
// motion_blocking_no_leaves" does, and tells the bot.
func (g *Game) Summon(kind string, x, z float64) error {
	k, ok := mobKinds[kind]
	if !ok {
		return fmt.Errorf("simworld: not modelled: the mob %s", kind)
	}
	e, ok := entity.ByName[kind]
	if !ok {
		return fmt.Errorf("simworld: no entity %s", kind)
	}
	y := 0.0
	for by := 319; by > -64; by-- {
		name := block.StateList[g.S.World.At(int(math.Floor(x)), by, int(math.Floor(z)))].ID()
		if len(block.CollisionShape(g.S.World.At(int(math.Floor(x)), by, int(math.Floor(z))))) > 0 && !slices.Contains([]string{"minecraft:oak_leaves"}, name) && len(name) > 0 && name[len(name)-7:] != "_leaves" {
			y = float64(by + 1)
			break
		}
	}
	g.nextID++
	m := &mob{id: g.nextID, kind: kind, k: k, x: x, y: y, z: z, health: k.health, onGround: true, half: e.Width / 2, high: e.Height,
		swellDir: -1, strafing: -1, attackTime: -1, using: -1}
	if k.baby != nil {
		switch g.babies {
		case 0: // (the game's chance, as finalizeSpawn draws it)
			m.isBaby = float64(g.ai.Float32()) < k.baby.odds
		case 1:
			m.isBaby = true
		}
	}
	if m.isBaby {
		m.half, m.high = m.half*k.baby.scale, m.high*k.baby.scale
		m.k.speed *= k.baby.speed
		m.k.reach = math.Sqrt(sq(4*m.half) + 2*playerHalf) // (the melee goal's reach by its own width)
	}
	g.mobs = append(g.mobs, m)
	var u pk.UUID
	u[15], u[14] = byte(m.id), byte(m.id>>8)
	u[0] = 1 // (apart from the items')
	err := g.S.Send(play.AddEntity{ID: pk.VarInt(m.id), UUID: u, Type: pk.VarInt(slices.Index(registryid.EntityType, kind)),
		X: pk.Double(x), Y: pk.Double(y), Z: pk.Double(z)})
	if err == nil && m.isBaby {
		err = g.S.Send(play.SetEntityData{ID: pk.VarInt(m.id), PackedItems: types.EntityData{
			{Index: entitydata.ZombieBaby, Serializer: 8, Value: ptr(pk.Boolean(true))}}})
	}
	if err != nil {
		return err
	}
	// summoned between two ticks (a test's): the client has read it, and the
	// bot's goroutines are still, before anything reads the bot again — as a
	// tick's own packets are (Step). Else a round of the bot's mind on the
	// same tick saw the mob in some runs and not in others: the same seed, two
	// stories (a creeper summoned on a round's tick, fled from at once or a
	// round later)
	g.sync()
	settle()
	return nil
}

// SummonBabyNear puts a baby of the kind dx, dz off the player, whatever the chance (a test's).
func (g *Game) SummonBabyNear(kind string, dx, dz float64) error {
	g.babies = 1
	defer func() { g.babies = 0 }()
	return g.SummonNear(kind, dx, dz)
}

// SummonAdultNear puts an adult of the kind dx, dz off the player, whatever the chance (a test's).
func (g *Game) SummonAdultNear(kind string, dx, dz float64) error {
	g.babies = 2
	defer func() { g.babies = 0 }()
	return g.SummonNear(kind, dx, dz)
}

// IsBaby says whether the mob is a baby.
func (g *Game) IsBaby(id int32) bool {
	for _, m := range g.mobs {
		if m.id == id {
			return m.isBaby
		}
	}
	return false
}

// SummonNear puts a mob dx, dz off the player (the harness's script: relative to the bot).
func (g *Game) SummonNear(kind string, dx, dz float64) error {
	return g.Summon(kind, g.pos[0]+dx, g.pos[2]+dz)
}

// Mobs is how many mobs live (not yet gone) in the world.
func (g *Game) Mobs() int { return len(g.mobs) }

// mobsTick is the mobs' tick: gravity, the walk to the player and a blow when near, the knockback's decay.
func (g *Game) mobsTick() error {
	for i := 0; i < len(g.mobs); i++ {
		m := g.mobs[i]
		if m.dying > 0 {
			if m.dying++; m.dying > 20 {
				g.mobs = slices.Delete(g.mobs, i, i+1)
				i--
				if err := g.S.Send(play.RemoveEntities{EntityIds: []pk.VarInt{pk.VarInt(m.id)}}); err != nil {
					return err
				}
			}
			continue
		}
		if m.invuln > 0 {
			m.invuln--
		}
		if m.cooldown > 0 {
			m.cooldown--
		}
		if died, err := g.burnTick(m); err != nil {
			return err
		} else if died {
			continue
		}
		ownBlow, err := g.trickTick(m) // PATCH (tricks.go)
		if err != nil {
			return err
		}
		// the walk: toward the player, while it lives — the push of a step is the speed a tick at rest; a
		// creeper lit stands, a skeleton in range strafes
		const drag = 0.546 // the ground's friction (0.6) times the air's (0.91)
		dx, dz := g.pos[0]-m.x, g.pos[2]-m.z
		dist := math.Hypot(dx, dz)
		var ax, az float64
		far := m.k.follow > 0 && math.Sqrt(dx*dx+dz*dz+(g.pos[1]-m.y)*(g.pos[1]-m.y)) > m.k.follow // beyond its follow range: no target
		if far && m.k.fuse != nil && m.swellDir > 0 {
			if err := g.setSwellDir(m, -1); err != nil {
				return err
			}
		}
		if m.k.passive {
			if !g.dead && g.tempts(m, dist) {
				m.yaw = math.Atan2(-dx, dz) * 180 / math.Pi
				ax, az = dx/dist*m.k.speed*(1-drag), dz/dist*m.k.speed*(1-drag)
			}
			if err := g.sheepTick(m); err != nil {
				return err
			}
		} else if !g.dead && dist > 0.5 && !far {
			m.yaw = math.Atan2(-dx, dz) * 180 / math.Pi
			ux, uz := dx/dist, dz/dist // (toward the player)
			switch {
			case m.k.fuse != nil:
				blown, err := g.creeperTick(m)
				if err != nil {
					return err
				}
				if blown { // (it is gone from the list; the others the blast killed lie dying in it)
					i--
					continue
				}
				if m.swellDir > 0 {
					ux, uz = 0, 0
				}
			case m.k.bow != nil:
				var err error
				if ux, uz, err = g.skeletonTick(m, ux, uz); err != nil {
					return err
				}
			}
			if m.k.interval > 0 && dist < 1.2 || dist < 0.8 { // (entities push each other apart: none walks into the player; a duelist keeps a sword's length)
				ux, uz = 0, 0
			}
			ax, az = ux*m.k.speed*(1-drag), uz*m.k.speed*(1-drag)
		} else if m.k.fuse != nil && m.swellDir > 0 { // (the player dead: the fuse goes out)
			if err := g.setSwellDir(m, -1); err != nil {
				return err
			}
		}
		// the move: this tick's step is the motion with the push; the next tick's motion is the step slowed
		stepX, stepZ := m.vx+ax, m.vz+az
		if g.inWeb(m) { // PATCH (arsenal.go): a cobweb (Entity.makeStuckInBlock: 0.25, 0.05)
			stepX, stepZ, m.vy = stepX*0.25, stepZ*0.25, m.vy*0.05
		}
		before := [3]float64{m.x, m.y, m.z}
		at := [3]float64{m.x, m.y, m.z}
		if m.jump && m.onGround {
			m.vy = 0.42 // it jumps at what stopped it (the game's jump: a speed up of 0.42, which clears a block)
		}
		moved := g.clip(at, m.half, m.high, 1, m.vy)
		m.onGround = m.vy <= 0 && moved != m.vy
		m.y += moved
		if moved != m.vy {
			m.vy = 0
		}
		at[1] = m.y
		m.jump = false
		if mx := g.clip(at, m.half, m.high, 0, stepX); mx != stepX {
			m.jump, stepX = true, mx
		}
		m.x += stepX
		at[0] = m.x
		if mz := g.clip(at, m.half, m.high, 2, stepZ); mz != stepZ {
			m.jump, stepZ = true, mz
		}
		m.z += stepZ
		m.vx, m.vz = stepX*drag, stepZ*drag
		m.vy = (m.vy - 0.08) * 0.98 // (gravity after the move, as the game does)
		if before != [3]float64{m.x, m.y, m.z} {
			err := g.S.Send(positionSync(m.id, m.x, m.y, m.z, float64(m.yaw), 0, m.onGround))
			if err != nil {
				return err
			}
		}
		// the blow
		if !g.dead && m.k.damage > 0 && m.breakTicks > 0 {
			if d := math.Sqrt(sq(g.pos[0]-m.x) + sq(g.pos[1]-m.y) + sq(g.pos[2]-m.z)); d < m.k.reach+1.5 && !g.seesPlayer(m) {
				if err := g.breakToward(m); err != nil {
					return err
				}
			} else {
				m.blind = 0
			}
		}
		if !g.dead && m.cooldown == 0 && m.k.damage > 0 && !ownBlow {
			d := math.Sqrt(sq(g.pos[0]-m.x) + sq(g.pos[1]-m.y) + sq(g.pos[2]-m.z))
			if d <= m.k.reach && g.seesPlayer(m) {
				m.cooldown = 20
				if m.k.interval > 0 {
					m.cooldown = m.k.interval
				}
				if err := g.mobHits(m); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func sq(x float64) float64 { return x * x }

// mobHits is a blow of the mob on the player.
func (g *Game) mobHits(m *mob) error {
	damageType := "minecraft:mob_attack"
	if m.kind == "minecraft:player" {
		damageType = "minecraft:player_attack"
		// a player's swing shows to the others (ServerPlayer.swing: the animate packet), hit or not
		if err := g.S.Send(play.Animate{ID: pk.VarInt(m.id), Action: 0}); err != nil {
			return err
		}
	}
	hurt, err := g.hurtPlayer(m.k.damage, damageType, m.id, m.id, m.yaw, fmt.Sprintf("slain by %s", m.kind), &[3]float64{m.x, m.y, m.z})
	if err != nil || !hurt || m.k.knock <= 0 {
		return err
	}
	return g.knockPlayer(m.k.knock, g.pos[0]-m.x, g.pos[2]-m.z)
}

// SummonDuelist puts a duelist (a player-like opponent: the kind "minecraft:player") at dx, dz from the
// player: it sprints at it and hits as an average player with an iron sword does.
func (g *Game) SummonDuelist(dx, dz float64) error {
	if err := g.SummonNear("minecraft:player", dx, dz); err != nil {
		return err
	}
	// its sword shows in its hand, as a player's does
	sword := itemStack(slotItem{item: "minecraft:iron_sword", n: 1})
	return g.S.Send(play.SetEquipment{Entity: pk.VarInt(g.LastMob()), Slots: types.SetEquipmentEntryList{{SlotID: 0, ItemStack: sword}}})
}

// hurtPlayer is damage to the player (Player.hurt): at the normal difficulty as it is, not taken while the
// player is in the twenty ticks after a hit unless it is the greater, and told as the game tells it — the
// damage event (its cause and what dealt it: entity ids), the hurt animation, the health. It says whether it
// was taken. The knockback is the caller's (knockPlayer).
//
// from is where the damage comes from (nil: nowhere, as a fire): a raised shield facing it takes it all
// (offhand.go); a totem in a hand saves a death.
func (g *Game) hurtPlayer(amount float32, damageType string, cause, direct int32, yaw float64, why string, from *[3]float64) (bool, error) {
	if g.dead || amount <= 0 {
		return false, nil
	}
	if g.blocks(from) {
		g.blocked++
		// the shield's sound: the entity event 29 (LivingEntity.handleEntityEvent: SHIELD_BLOCK)
		if err := g.S.Send(play.EntityEvent{EntityID: 1, EventID: 29}); err != nil {
			return false, err
		}
		// a blow on the shield pushes the one who struck back (LivingEntity.blockUsingShield: 0.5)
		for _, m := range g.mobs {
			if m.id == cause && damageType != "minecraft:arrow" && damageType != "minecraft:explosion" {
				if dx, dz := m.x-g.pos[0], m.z-g.pos[2]; math.Hypot(dx, dz) > 1e-4 {
					n := math.Hypot(dx, dz)
					m.vx, m.vz = m.vx/2+dx/n*0.5, m.vz/2+dz/n*0.5
				}
			}
		}
		return false, nil
	}
	switch damageType { // (armor and protection: blows, arrows, blasts; not a fall, not fire)
	case "minecraft:mob_attack", "minecraft:player_attack", "minecraft:arrow":
		amount = float32(g.playerDefense().reduce(float64(amount)))
	case "minecraft:explosion": // PATCH (arsenal.go): blast protection counts twice a level
		d := g.playerDefense()
		for s := 5; s <= 8; s++ {
			d.prot += 2 * float64(level(g.slots[s].ench, "blast_protection"))
		}
		amount = float32(d.reduce(float64(amount)))
	}
	full := amount
	if g.invuln > 10 && amount <= g.lastDamage {
		return false, nil
	}
	if g.invuln > 10 {
		amount -= g.lastDamage
	}
	g.invuln, g.lastDamage = 20, full
	g.Health = max(0, g.Health-amount)
	if damageType == "minecraft:mob_attack" || damageType == "minecraft:player_attack" {
		g.landed++
	}
	g.exhaust(exhaustHurt)
	err := g.S.Send(play.DamageEvent{EntityID: 1, SourceType: damageTypeID(damageType),
		SourceCauseID: pk.VarInt(cause) + 1, SourceDirectID: pk.VarInt(direct) + 1})
	if err != nil {
		return false, err
	}
	if err := g.S.Send(play.HurtAnimation{ID: 1, Yaw: pk.Float(yaw)}); err != nil {
		return false, err
	}
	if err := g.tellHealth([3]float32{}); err != nil {
		return false, err
	}
	if g.Health <= 0 {
		if saved, err := g.totem(); saved || err != nil {
			return true, err
		}
		return true, g.die(why)
	}
	return true, nil
}

// knockPlayer is LivingEntity.knockback on the player: pushed along x, z (away from the blow) by the strength, and up when it
// stands — told as its motion (the client moves itself). The motion it had is not known to the server here.
func (g *Game) knockPlayer(strength, x, z float64) error {
	n := math.Hypot(x, z)
	if n < 1e-5 || g.dead {
		return nil
	}
	vy := 0.0
	if g.onGround {
		vy = math.Min(0.4, strength) // (LivingEntity.knockback: up to 0.4)
	}
	return g.S.Send(play.SetEntityMotion{ID: 1, Movement: types.LpVec3{X: x / n * strength, Y: vy, Z: z / n * strength}})
}

// attackMob is the player's blow on a mob (ServerPlayer.attack): within the reach of three blocks of its
// box, the damage of the held item scaled by how charged the swing is, the knockback away from the player,
// no new blow for ten ticks. Criticals, a sprint's extra knockback and sweeping are not modelled.
func (g *Game) attackMob(id int32) error {
	var m *mob
	for _, x := range g.mobs {
		if x.id == id && x.dying == 0 {
			m = x
		}
	}
	if m == nil {
		return nil // (a mob that is gone: nothing)
	}
	eye := [3]float64{g.pos[0], g.pos[1] + 1.62, g.pos[2]}
	d := math.Sqrt(sq(eye[0]-math.Max(m.x-m.half, math.Min(eye[0], m.x+m.half))) + sq(eye[1]-math.Max(m.y, math.Min(eye[1], m.y+m.high))) +
		sq(eye[2]-math.Max(m.z-m.half, math.Min(eye[2], m.z+m.half))))
	if d > 3.0 {
		return nil // (too far: the server drops it)
	}
	dmg, speed := g.heldAttack()
	if l := level(g.heldItem().ench, "sharpness"); l > 0 {
		dmg += 0.5*float64(l) + 0.5
	}
	delay := 20 / speed
	scale := math.Min(1, (float64(g.Tick-g.lastSwing)+0.5)/delay)
	g.lastSwing = g.Tick
	dmg *= 0.2 + scale*scale*0.8
	// a critical (Player.attack): charged (over 0.9), falling, in the air, not sprinting — half again
	crit := scale > 0.9 && g.fall > 0 && !g.onGround && !g.sprinting
	if crit {
		dmg *= 1.5
		g.crits++
	}
	sprintHit := scale > 0.9 && g.sprinting // a sprint's blow knocks one level more, and ends the sprint
	// a sweep (Player.attack: a sword, charged, on the ground, no sprint, no critical): the others near the
	// target (its box grown by 1, 0.25, 1, within 3 of the player) take 1 and a knockback of 0.4
	if held := g.heldItem().item; scale > 0.9 && !crit && !g.sprinting && g.onGround && strings.HasSuffix(held, "_sword") {
		for _, o := range g.mobs {
			if o == m || o.dying > 0 || math.Abs(o.x-m.x) > m.half+o.half+1 || math.Abs(o.z-m.z) > m.half+o.half+1 ||
				o.y > m.y+m.high+0.25 || o.y+o.high < m.y-0.25 || sq(o.x-g.pos[0])+sq(o.z-g.pos[2]) > 9 {
				continue
			}
			if _, err := g.hurtMob(o, 1, "minecraft:player_attack"); err != nil {
				return err
			}
			if dx, dz := o.x-g.pos[0], o.z-g.pos[2]; math.Hypot(dx, dz) > 1e-4 {
				n := math.Hypot(dx, dz)
				o.vx, o.vz = o.vx/2+dx/n*0.4, o.vz/2+dz/n*0.4
			}
			g.sweeps++
		}
	}
	// PATCH (arsenal.go): the mace's smash, falling more than 1.5 (not scaled by the charge); the fall forgotten
	if g.heldItem().item == "minecraft:mace" && !g.onGround && !g.gliding && g.fall > 1.5 {
		dmg += smashBonus(g.fall)
		g.ars.smashes++
		g.ars.smashMax = math.Max(g.ars.smashMax, smashBonus(g.fall))
		g.fall = 0
	}
	dmg = m.def.reduce(dmg)
	if m.invuln > 10 {
		if float32(dmg) <= m.lastHurt {
			return nil
		}
		dmg -= float64(m.lastHurt)
	}
	m.lastHurt = float32(dmg)
	m.invuln = 20
	m.health -= float32(dmg)
	m.playerHitAt = g.Tick
	// the knockback: half the motion kept, 0.4 away from the player, up a little from the ground
	dx, dz := m.x-g.pos[0], m.z-g.pos[2]
	if n := math.Hypot(dx, dz); n > 1e-4 {
		m.vx = m.vx/2 + dx/n*0.4
		m.vz = m.vz/2 + dz/n*0.4
		if sprintHit { // (the knockback level's own push: half a block more, the sprint stopped)
			m.vx += dx / n * 0.5
			m.vz += dz / n * 0.5
			g.sprinting = false
		}
	}
	if m.onGround {
		m.vy = math.Min(0.4, m.vy/2+0.4)
	}
	err := g.S.Send(play.DamageEvent{EntityID: pk.VarInt(m.id), SourceType: damageTypeID("minecraft:player_attack"), SourceCauseID: 2, SourceDirectID: 2})
	if err != nil {
		return err
	}
	if m.health <= 0 {
		return g.killMob(m, g.killContext(m))
	}
	return g.S.Send(play.EntityEvent{EntityID: pk.Int(m.id), EventID: 2})
}

// killMob is a mob's death: it lies dying (gone 20 ticks on), the client told, its loot dropped as the kill was.
func (g *Game) killMob(m *mob, how loot.Context) error {
	m.dying = 1
	if err := g.S.Send(play.EntityEvent{EntityID: pk.Int(m.id), EventID: 3}); err != nil {
		return err
	}
	return g.mobDrops(m, how)
}

// mobDrops is what a killed mob leaves (LivingEntity.dropAllDeathLoot): its loot table played (data/loot)
// for how it was killed — by the player's blow (killContext: the weapon in hand), or a blast — with the
// game's chance (the same seed, the same drops), each stack an item where the mob died, thrown up a little (Entity.spawnAtLocation: 0.2 up,
// ±0.1 sideways). A mob here never rides; the player's blow is in none of the damage
// tags a table asks (is_projectile, is_lightning). A condition or function the evaluator does not know
// fails the game, naming it.
func (g *Game) mobDrops(m *mob, how loot.Context) error {
	got, err := loot.EntityDrops(m.kind, how)
	if err != nil {
		return fmt.Errorf("simworld: %w", err)
	}
	r := func(lo, hi float64) float64 { return lo + g.motion.Float64()*(hi-lo) }
	for _, s := range got {
		if err := g.addItem(m.x, m.y, m.z, r(-0.1, 0.1), 0.2, r(-0.1, 0.1), slotItem{item: s.Item, n: s.Count}, pickupDelay); err != nil {
			return err
		}
	}
	return nil
}

// killContext is a kill of the mob by the player's blow, with what the player holds (no enchantment is
// modelled: none is ever held), the mob as it is (burning, a baby). The chance is the game's.
func (g *Game) killContext(m *mob) loot.Context {
	weapon := loot.Tool{}
	if h := g.heldItem(); h.n > 0 {
		weapon.Item = h.item
	}
	return loot.Context{Rand: g.rng, This: g.lootEntity(m),
		Attacker: &loot.Entity{Type: "minecraft:player", Mainhand: weapon}, KilledByPlayer: true}
}

// lootEntity is the mob as a loot table asks of it.
func (g *Game) lootEntity(m *mob) *loot.Entity {
	return &loot.Entity{Type: m.kind, OnFire: m.fire > 0, Baby: m.isBaby}
}

// heldAttack is the attack damage and attack speed with the held item: 1 and 4 for a hand, the item's attribute
// modifiers added (a sword's: damage 4 for wooden, speed −2.4).
func (g *Game) heldAttack() (damage, speed float64) {
	damage, speed = 1, 4
	h := g.heldItem()
	if h.n == 0 {
		return
	}
	mods := itemdata.DefaultComponent[*component.AttributeModifiers](itemdata.ID(slices.Index(registryid.Item, h.item)))
	if mods == nil {
		return
	}
	for _, e := range mods.Modifiers {
		if e.Slot != component.EquipmentSlotGroupMainhand && e.Slot != component.EquipmentSlotGroupAny {
			continue
		}
		switch registryid.Attribute[e.Attribute] {
		case "minecraft:attack_damage":
			damage += float64(e.Modifier.Amount)
		case "minecraft:attack_speed":
			speed += float64(e.Modifier.Amount)
		}
	}
	return
}

var _ = act.Attack{}

// MobsAt says where the mobs are (a debugging aid).
func (g *Game) MobsAt() string {
	var s string
	for _, m := range g.mobs {
		s += fmt.Sprintf("[%s %.1f %.1f %.1f hp %.0f]", m.kind[10:], m.x, m.y, m.z, m.health)
	}
	return s
}

// Around says the blocks in a 3×3 column of the world round the place, from y−2 to y+1 (a debugging aid).
func (g *Game) Around(x, y, z int) string {
	var s string
	for dy := 1; dy >= -2; dy-- {
		s += fmt.Sprintf("y%d:", y+dy)
		for dx := -1; dx <= 1; dx++ {
			s += " " + block.StateList[g.S.World.At(x+dx, y+dy, z)].ID()[10:]
		}
		s += "\n"
	}
	return s
}

// RemoveMobs takes every mob out of the world (a test's way to end a fight: it tells the bot they are gone).
func (g *Game) RemoveMobs() error {
	var ids []pk.VarInt
	for _, m := range g.mobs {
		ids = append(ids, pk.VarInt(m.id))
	}
	g.mobs = nil
	if len(ids) == 0 {
		return nil
	}
	return g.S.Send(play.RemoveEntities{EntityIds: ids})
}

// tellAttributes sends the player's attributes when what is held changes (and once, first): the server sends them
// at the join and again as an item with modifiers is taken in hand (a sword: its attack damage and speed). The
// base values are the player's own (the game's code: movement speed 0.1, attack damage 1, attack speed 4).
func (g *Game) tellAttributes() error {
	h := g.heldItem()
	key := ""
	if h.n > 0 {
		key = h.item
	}
	if g.toldAttr != nil && *g.toldAttr == key {
		return nil
	}
	g.toldAttr = &key
	snap := func(name string, base float64) types.UpdateAttributesAttributeSnapshot {
		s := types.UpdateAttributesAttributeSnapshot{Attribute: pk.VarInt(slices.Index(registryid.Attribute, name)), Base: pk.Double(base)}
		if h.n > 0 {
			if mods := itemdata.DefaultComponent[*component.AttributeModifiers](itemdata.ID(slices.Index(registryid.Item, h.item))); mods != nil {
				for _, e := range mods.Modifiers {
					if registryid.Attribute[e.Attribute] == name && (e.Slot == component.EquipmentSlotGroupMainhand || e.Slot == component.EquipmentSlotGroupAny) {
						s.Modifiers = append(s.Modifiers, types.AttributeModifier{ID: e.Modifier.ID, Amount: e.Modifier.Amount, Operation: types.AttributeModifierOperation(e.Modifier.Operation)})
					}
				}
			}
		}
		return s
	}
	return g.S.Send(play.UpdateAttributes{EntityID: 1, Values: []types.UpdateAttributesAttributeSnapshot{
		snap("minecraft:movement_speed", 0.1), snap("minecraft:attack_damage", 1), snap("minecraft:attack_speed", 4)}})
}
