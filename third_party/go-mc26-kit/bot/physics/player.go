// Package physics moves a player the way the vanilla client does, one tick at
// a time: LocalPlayer.aiStep (the keys, sprinting, sneaking), LivingEntity.aiStep
// (jumping, travel in air, water and lava, climbing) and Entity.move (the
// collision with the block shapes, stepping up, the ground and collision
// flags, the speed factor of the block underfoot). The server checks every
// position the client sends against its own collision; a player moved by this
// package is one it has no reason to correct.
//
// Not yet: flying, elytra, riding, swimming pose, cobwebs and berry bushes,
// powder snow, bubble columns, honey, soul speed.
//
// PATCH (minecraft-bot): elytra gliding (LivingEntity.travelFallFlying, the
// pose's 0.6 box, LocalPlayer's tryToStartFallFlying on a jump pressed in the
// air) and a firework rocket's boost (FireworkRocketEntity.tick on the player
// it is attached to). Effects jump boost, slow falling and levitation, as
// LivingEntity does (getJumpBoostPower, getEffectiveGravity, travelInAir). Entities are collided with when the Blocks given is also
// an EntitySolids (a boat, a shulker).
package physics

import (
	"math"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/version"
	"github.com/mj41/go-mc26/level/block"
)

// Keys are the keys held this tick (net.minecraft.world.entity.player.Input).
type Keys struct {
	Forward, Backward, Left, Right, Jump, Shift, Sprint bool
}

// Attributes are the player's movement attributes, as the server sent them;
// the zero value is not usable, start from DefaultAttributes.
type Attributes struct {
	MovementSpeed           float64 // without the sprint modifier; the physics adds it
	JumpStrength            float64
	Gravity                 float64
	StepHeight              float64
	SneakingSpeed           float64
	MovementEfficiency      float64
	WaterMovementEfficiency float64
	FrictionModifier        float64 // 26.2+; 1 before
	AirDragModifier         float64 // 26.2+; 1 before
	Bounciness              float64 // 26.2+; 0 before
}

// DefaultAttributes are a player's base values (Player.createAttributes).
var DefaultAttributes = Attributes{
	MovementSpeed: 0.1, JumpStrength: 0.42, Gravity: 0.08, StepHeight: 0.6, SneakingSpeed: 0.3,
	FrictionModifier: 1, AirDragModifier: 1,
}

// sprintSpeedBoost is the sprinting modifier LivingEntity.setSprinting adds to
// the movement speed (ADD_MULTIPLIED_TOTAL).
const sprintSpeedBoost = 0.3

// Player is a simulated player.
type Player struct {
	Pos        Vec3
	Vel        Vec3 // the delta movement, blocks per tick
	Yaw, Pitch float32

	OnGround, HorizontalCollision, MinorHorizontalCollision, VerticalCollision bool

	Sprinting    bool
	Crouching    bool // the pose: sneaking (shift held) or forced by a low ceiling
	FallDistance float64
	FoodLevel    int // sprinting needs more than 6

	InWater, InLava         bool
	waterHeight, lavaHeight float64

	// PATCH: the effects that change the movement (Mover sets them each tick
	// from the effects the server told).
	JumpBoost   float32 // getJumpBoostPower: 0.1 × (amplifier+1), 0 without the effect
	SlowFalling bool
	Levitation  int // amplifier+1, 0 without the effect

	// PATCH: elytra. FallFlying is the shared flag 7 (isFallFlying): set by
	// the client as it starts, then by the server's word (Mover). CanGlide:
	// a glider worn that is not about to break (set before Tick). Rocket: a
	// firework rocket attached to the player boosts it this tick (set before
	// Tick). StartedFallFlying: this tick's Tick started the glide — the
	// client sends START_FALL_FLYING.
	FallFlying        bool
	CanGlide          bool
	Rocket            bool
	StartedFallFlying bool

	keys Keys // last tick's keys
	// UsingItem: an item in use (a shield up, a meal): the move is a fifth and no sprint (set before Tick)
	UsingItem   bool
	xxa, zza    float32 // the input vector after LocalPlayer.modifyInput
	jumping     bool
	noJumpDelay int
	speed       float32 // LivingEntity.speed: the movement speed attribute of the last tick
	supporting  *world.BlockPos
}

// NewPlayer is a player standing at pos.
func NewPlayer(pos Vec3) *Player {
	return &Player{Pos: pos, FoodLevel: 20, speed: float32(DefaultAttributes.MovementSpeed)}
}

// PATCH: the player's dimensions are floats in the game (EntityDimensions:
// 0.6F wide, 1.8F high, 1.5F crouching), and its box is made from them
// (EntityDimensions.makeBoundingBox: float f = width / 2.0F): half a width is
// 0.30000001192092896, not 0.3, and the height 1.7999999523162842. With the
// exact decimals, a box stopped against a block sat 1.2e-8 inside it as the
// server (and an anticheat) sees it: every move that touched a block was
// turned back.
var (
	playerHalfWidth = float64(float32(0.6) / 2)
	standingHeight  = float64(float32(1.8))
	crouchingHeight = float64(float32(1.5))
	glidingHeight   = float64(float32(0.6))
)

func (p *Player) height() float64 {
	if p.FallFlying { // PATCH: Pose.FALL_FLYING is 0.6 × 0.6
		return glidingHeight
	}
	if p.Crouching {
		return crouchingHeight
	}
	return standingHeight
}

// Box is the player's bounding box.
func (p *Player) Box() AABB {
	w := playerHalfWidth
	return AABB{p.Pos.X - w, p.Pos.Y, p.Pos.Z - w, p.Pos.X + w, p.Pos.Y + p.height(), p.Pos.Z + w}
}

func (p *Player) blockPos() world.BlockPos {
	return world.BlockPos{X: floor(p.Pos.X), Y: floor(p.Pos.Y), Z: floor(p.Pos.Z)}
}

// Tick moves the player one tick with keys held.
func (p *Player) Tick(w Blocks, tags Tags, keys Keys, attr Attributes) {
	// Entity.baseTick: which fluids the player is in, from where it stands
	p.updateFluids(w, tags)

	// LocalPlayer.aiStep
	// the crouch is decided before the keys of this tick are read
	// (isShiftKeyDown is still last tick's)
	p.Crouching = p.keys.Shift || !p.fits(w, false)
	move := moveVector(keys)
	if p.UsingItem { // LocalPlayer.modifyInput: an item in use slows the move to a fifth
		move[0], move[1] = move[0]*0.2, move[1]*0.2
	}
	if !p.Sprinting && move[1] > 1e-5 && p.FoodLevel > 6 && !p.Crouching && keys.Sprint && !p.UsingItem {
		p.Sprinting = true
	}
	if p.Sprinting && (p.FoodLevel <= 6 || move[1] <= 1e-5 || p.UsingItem || p.HorizontalCollision && !p.MinorHorizontalCollision) {
		p.Sprinting = false
	}
	if p.InWater && keys.Shift {
		p.Vel.Y -= float64(float32(0.04)) // goDownInWater
	}
	// PATCH: LocalPlayer.aiStep: the jump key pressed (not held) in the air
	// starts the glide (Player.tryToStartFallFlying, canGlide); landed, or the
	// glider gone, it ends (the server says so a moment later: the client
	// takes the ground at once)
	p.StartedFallFlying = false
	if p.FallFlying && (p.OnGround || !p.CanGlide || p.Levitation > 0) {
		p.FallFlying = false
	}
	if keys.Jump && !p.keys.Jump && !p.OnGround && !p.FallFlying && p.CanGlide && !p.InWater && p.Levitation == 0 && !p.onClimbable(w, tags) {
		p.FallFlying, p.StartedFallFlying = true, true
	}
	p.keys = keys

	// LivingEntity.aiStep
	if p.noJumpDelay > 0 {
		p.noJumpDelay--
	}
	if p.Vel.HorizontalSqr() < 9.0e-6 {
		p.Vel.X, p.Vel.Z = 0, 0
	}
	if math.Abs(p.Vel.Y) < 0.003 {
		p.Vel.Y = 0
	}
	p.applyInput(move, attr)
	if p.jumping {
		p.jump(w, attr)
	} else {
		p.noJumpDelay = 0
	}
	p.travel(w, tags, attr)

	// Player.aiStep: the speed of the next tick
	speed := attr.MovementSpeed
	if p.Sprinting {
		speed *= 1 + sprintSpeedBoost
	}
	p.speed = float32(speed)
}

// moveVector is KeyboardInput.tick's: (left, forward), normalized.
func moveVector(k Keys) [2]float32 {
	impulse := func(pos, neg bool) float32 {
		switch {
		case pos == neg:
			return 0
		case pos:
			return 1
		}
		return -1
	}
	v := [2]float32{impulse(k.Left, k.Right), impulse(k.Forward, k.Backward)}
	l := float32(math.Sqrt(float64(v[0]*v[0] + v[1]*v[1])))
	if l < 1.0e-4 {
		return [2]float32{}
	}
	return [2]float32{v[0] / l, v[1] / l}
}

// applyInput is LocalPlayer.applyInput and modifyInput.
func (p *Player) applyInput(move [2]float32, attr Attributes) {
	p.jumping = p.keys.Jump
	if move[0] == 0 && move[1] == 0 {
		p.xxa, p.zza = 0, 0
		return
	}
	x, z := move[0]*0.98, move[1]*0.98
	if p.Crouching {
		f := float32(attr.SneakingSpeed)
		x, z = x*f, z*f
	}
	// modifyInputSpeedForSquareMovement
	length := float32(math.Sqrt(float64(x*x + z*z)))
	if length > 0 {
		dx, dz := x/length, z/length
		ax, az := float32(math.Abs(float64(dx))), float32(math.Abs(float64(dz)))
		tan := az / ax
		if az > ax {
			tan = ax / az
		}
		toSquare := float32(math.Sqrt(float64(1 + tan*tan)))
		l := min(length*toSquare, 1)
		x, z = dx*l, dz*l
	}
	p.xxa, p.zza = x, z
}

// jump is LivingEntity.aiStep's jump: from the ground, in water, in lava.
func (p *Player) jump(w Blocks, attr Attributes) {
	inWaterDeep := p.InWater && p.waterHeight > 0
	const threshold = 0.4 // getFluidJumpThreshold: eye height ≥ 0.4
	switch {
	case inWaterDeep && !(p.OnGround && p.waterHeight <= threshold):
		p.Vel.Y += float64(float32(0.04)) // jumpInLiquid
	case p.InLava && !(p.OnGround && p.lavaHeight <= threshold):
		p.Vel.Y += float64(float32(0.04))
	case (p.OnGround || inWaterDeep && p.waterHeight <= threshold) && p.noJumpDelay == 0:
		p.jumpFromGround(w, attr)
		p.noJumpDelay = 10
	}
}

// jumpFromGround is LivingEntity.jumpFromGround.
func (p *Player) jumpFromGround(w Blocks, attr Attributes) {
	power := float32(attr.JumpStrength)*p.blockJumpFactor(w) + p.JumpBoost // PATCH: + getJumpBoostPower
	if power <= 1.0e-5 {
		return
	}
	p.Vel.Y = math.Max(float64(power), p.Vel.Y)
	if p.Sprinting {
		a := float64(p.Yaw * degToRad)
		p.Vel.X += float64(-sin(a)) * 0.2
		p.Vel.Z += float64(cos(a)) * 0.2
	}
}

// travel is LivingEntity.travel for a player that does not fly (creative).
func (p *Player) travel(w Blocks, tags Tags, attr Attributes) {
	// PATCH: FireworkRocketEntity.tick: a rocket attached to a gliding player
	// pulls it along its look, toward 1.5 blocks a tick
	if p.Rocket && p.FallFlying {
		l := p.LookAngle()
		v := p.Vel
		p.Vel = v.Add(Vec3{l.X*0.1 + (l.X*1.5-v.X)*0.5, l.Y*0.1 + (l.Y*1.5-v.Y)*0.5, l.Z*0.1 + (l.Z*1.5-v.Z)*0.5})
	}
	if p.InWater || p.InLava {
		p.travelInFluid(w, tags, attr)
		return
	}
	if p.FallFlying {
		p.travelFallFlying(w, tags, attr)
		return
	}
	p.travelInAir(w, tags, attr)
}

// LookAngle is Entity.getLookAngle (calculateViewVector, with Mth's sine).
func (p *Player) LookAngle() Vec3 {
	f := float64(p.Pitch * degToRad)
	g := float64(-p.Yaw * degToRad)
	h, i, j, k := cos(g), sin(g), cos(f), sin(f)
	return Vec3{float64(i * j), float64(-k), float64(h * j)}
}

// travelFallFlying is LivingEntity.travelFallFlying (PATCH): on a ladder the
// glide ends; else updateFallFlyingMovement, the move, and
// checkFallDistanceAccumulation (gliding level, the fall is forgotten).
func (p *Player) travelFallFlying(w Blocks, tags Tags, attr Attributes) {
	if p.onClimbable(w, tags) {
		p.travelInAir(w, tags, attr)
		p.FallFlying = false
		return
	}
	p.Vel = p.updateFallFlyingMovement(p.Vel, attr)
	p.move(w, attr, p.Vel)
	if p.Vel.Y > -0.5 && p.FallDistance > 1 {
		p.FallDistance = 1
	}
}

// updateFallFlyingMovement is LivingEntity.updateFallFlyingMovement.
func (p *Player) updateFallFlyingMovement(v Vec3, attr Attributes) Vec3 {
	look := p.LookAngle()
	pitch := p.Pitch * degToRad
	lh := math.Sqrt(look.X*look.X + look.Z*look.Z)
	vh := math.Sqrt(v.X*v.X + v.Z*v.Z)
	g := p.effectiveGravity(attr, v.Y)
	c := math.Cos(float64(pitch))
	lift := c * c
	v.Y += g * (-1.0 + lift*0.75)
	if v.Y < 0 && lh > 0 {
		d := v.Y * -0.1 * lift
		v = v.Add(Vec3{look.X * d / lh, d, look.Z * d / lh})
	}
	if pitch < 0 && lh > 0 {
		d := vh * float64(-sin(float64(pitch))) * 0.04
		v = v.Add(Vec3{-look.X * d / lh, d * 3.2, -look.Z * d / lh})
	}
	if lh > 0 {
		v = v.Add(Vec3{(look.X/lh*vh - v.X) * 0.1, 0, (look.Z/lh*vh - v.Z) * 0.1})
	}
	return Vec3{v.X * float64(float32(0.99)), v.Y * float64(float32(0.98)), v.Z * float64(float32(0.99))}
}

func modifiedFriction(f, modifier float32) float32 {
	return float32(clamp(float64(1-(1-f)*modifier), 0, 1))
}

// travelInAir is LivingEntity.travelInAir.
func (p *Player) travelInAir(w Blocks, tags Tags, attr Attributes) {
	friction := float32(1)
	if p.OnGround {
		below := p.stateAt(w, p.blockBelowAffectingMovement())
		friction = modifiedFriction(block.BehaviourOf(below).Friction, float32(attr.FrictionModifier))
	}
	movement := p.moveWithFriction(w, tags, attr, friction)
	// PATCH: levitation instead of gravity; slow falling caps the gravity while falling
	my := movement.Y
	if p.Levitation > 0 {
		my += (0.05*float64(p.Levitation) - movement.Y) * 0.2
	} else {
		my -= p.effectiveGravity(attr, movement.Y)
	}
	airDrag := modifiedFriction(0.91, float32(attr.AirDragModifier))
	f := friction * airDrag
	vertical := modifiedFriction(0.98, float32(attr.AirDragModifier))
	p.Vel = Vec3{movement.X * float64(f), my * float64(vertical), movement.Z * float64(f)}
}

// moveWithFriction is handleRelativeFrictionAndCalculateMovement.
func (p *Player) moveWithFriction(w Blocks, tags Tags, attr Attributes, friction float32) Vec3 {
	speed := p.flyingSpeed()
	if p.OnGround {
		speed = p.speed
		if friction > 0.6 {
			speed = p.speed * (0.21600002 / (friction * friction * friction))
		}
	}
	p.moveRelative(speed)
	climbing := p.onClimbable(w, tags)
	if climbing {
		p.Vel.X = clamp(p.Vel.X, -0.15, 0.15)
		p.Vel.Z = clamp(p.Vel.Z, -0.15, 0.15)
		p.Vel.Y = math.Max(p.Vel.Y, float64(float32(-0.15)))
		if p.Vel.Y < 0 && p.keys.Shift { // isSuppressingSlidingDownLadder, not on scaffolding
			p.Vel.Y = 0
		}
		p.FallDistance = 0
	}
	p.move(w, attr, p.Vel)
	movement := p.Vel
	if (p.HorizontalCollision || p.jumping) && climbing {
		movement.Y = 0.2
	}
	return movement
}

// flyingSpeed is Player.getFlyingSpeed when not flying.
func (p *Player) flyingSpeed() float32 {
	if p.Sprinting {
		return 0.025999999
	}
	return 0.02
}

// moveRelative is Entity.moveRelative with getInputVector.
func (p *Player) moveRelative(speed float32) {
	in := Vec3{float64(p.xxa), 0, float64(p.zza)}
	l := in.LengthSqr()
	if l < 1.0e-7 {
		return
	}
	if l > 1 {
		in = in.Scale(1 / math.Sqrt(l))
	}
	in = in.Scale(float64(speed))
	a := float64(p.Yaw * degToRad)
	s, c := float64(sin(a)), float64(cos(a))
	p.Vel = p.Vel.Add(Vec3{in.X*c - in.Z*s, in.Y, in.Z*c + in.X*s})
}

// travelInFluid is LivingEntity.travelInFluid: water, else lava.
func (p *Player) travelInFluid(w Blocks, tags Tags, attr Attributes) {
	falling := p.Vel.Y <= 0
	oldY := p.Pos.Y
	gravity := p.effectiveGravity(attr, p.Vel.Y) // PATCH: slow falling
	if p.InWater {
		slowDown := float32(0.8)
		if p.Sprinting {
			slowDown = 0.9
		}
		speed := float32(0.02)
		walker := float32(attr.WaterMovementEfficiency)
		if !p.OnGround {
			walker *= 0.5
		}
		if walker > 0 {
			slowDown += (0.54600006 - slowDown) * walker
			speed += (p.speed - speed) * walker
		}
		p.moveRelative(speed)
		p.move(w, attr, p.Vel)
		m := p.Vel
		if p.HorizontalCollision && p.onClimbable(w, tags) {
			m.Y = 0.2
		}
		m = Vec3{m.X * float64(slowDown), m.Y * float64(float32(0.8)), m.Z * float64(slowDown)}
		p.Vel = p.fluidFalling(gravity, falling, m)
	} else {
		p.moveRelative(0.02)
		p.move(w, attr, p.Vel)
		if p.lavaHeight <= 0.4 { // isInShallowFluid
			p.Vel = Vec3{p.Vel.X * 0.5, p.Vel.Y * float64(float32(0.8)), p.Vel.Z * 0.5}
			p.Vel = p.fluidFalling(gravity, falling, p.Vel)
		} else {
			p.Vel = p.Vel.Scale(0.5)
		}
		if gravity != 0 {
			p.Vel.Y -= gravity / 4
		}
	}
	// jumpOutOfFluid
	if p.HorizontalCollision && p.isFree(w, Vec3{p.Vel.X, p.Vel.Y + float64(float32(0.6)) - p.Pos.Y + oldY, p.Vel.Z}) {
		p.Vel.Y = float64(float32(0.3))
	}
}

// fluidFalling is LivingEntity.getFluidFallingAdjustedMovement.
func (p *Player) fluidFalling(gravity float64, falling bool, m Vec3) Vec3 {
	if gravity == 0 || p.Sprinting {
		return m
	}
	if falling && math.Abs(m.Y-0.005) >= 0.003 && math.Abs(m.Y-gravity/16) < 0.003 {
		m.Y = -0.003
	} else {
		m.Y -= gravity / 16
	}
	return m
}

// isFree is Entity.isFree: no block in the moved box, and no liquid.
func (p *Player) isFree(w Blocks, d Vec3) bool {
	box := p.Box().Move(d)
	return noCollision(w, box) && !containsLiquid(w, box)
}

// move is Entity.move for MoverType.SELF.
func (p *Player) move(w Blocks, attr Attributes, delta Vec3) {
	delta = p.backOffFromEdge(w, attr, delta)
	box := p.Box()
	movement := collide(w, box, delta, p.OnGround, float32(attr.StepHeight))
	if l := movement.LengthSqr(); l > 1e-7 || delta.LengthSqr()-l < 1e-7 {
		p.Pos = p.Pos.Add(movement)
	}
	xCollision, zCollision := !equal(delta.X, movement.X), !equal(delta.Z, movement.Z)
	p.HorizontalCollision = xCollision || zCollision
	p.VerticalCollision = delta.Y != movement.Y
	below := p.VerticalCollision && delta.Y < 0
	p.OnGround = below
	p.checkSupportingBlock(w, movement)
	p.MinorHorizontalCollision = p.HorizontalCollision && p.horizontalCollisionMinor(movement)

	// Entity.checkFallDamage, the distance only
	if p.OnGround {
		p.FallDistance = 0
	} else if movement.Y < 0 {
		p.FallDistance -= movement.Y
	}

	effect := p.stateAt(w, p.onPos(0.2))
	if delta.Y != 0 && p.VerticalCollision || p.HorizontalCollision {
		p.restitute(effect, xCollision, zCollision, movement, attr)
	}
	f := p.blockSpeedFactor(w, attr)
	p.Vel.X *= float64(f)
	p.Vel.Z *= float64(f)
}

// restitute is Entity.restituteMovementAfterCollisions: what a collision
// leaves of the velocity — nothing, unless the block or the player bounces.
func (p *Player) restitute(effect block.StateID, xCollision, zCollision bool, movement Vec3, attr Attributes) {
	suppress := p.keys.Shift // Player.isSuppressingBounce
	restitution := attr.Bounciness
	if suppress {
		restitution = 0
	}
	cur := p.Vel
	after := cur
	if xCollision {
		after.X = -cur.X * restitution
	}
	if zCollision {
		after.Z = -cur.Z * restitution
	}
	if p.VerticalCollision {
		if p.OnGround {
			if -cur.Y <= attr.Gravity || suppress {
				restitution = 0
			} else {
				restitution = math.Max(restitution, float64(block.BehaviourOf(effect).BounceRestitution))
			}
			if version.ProtocolVersion < 776 && !suppress && isSlime(effect) {
				restitution = 1 // 26.1: SlimeBlock.bounceUp, -y for a living entity
			}
		}
		gravityCompensation, drag := 0.0, 1.0
		if restitution > 0 && cur.Y != 0 {
			portion := movement.Y / cur.Y
			gravityCompensation = portion * attr.Gravity
			drag = 1 + (float64(modifiedFriction(0.98, float32(attr.AirDragModifier)))-1)*portion
		}
		after.Y = (gravityCompensation - cur.Y) * drag * restitution
		if version.ProtocolVersion < 776 {
			after.Y = 0
			if restitution > 0 && cur.Y < 0 {
				after.Y = -cur.Y
			}
		}
	}
	p.Vel = after
}

func isSlime(s block.StateID) bool {
	return int(s) >= 0 && int(s) < len(block.StateList) && block.StateList[s].ID() == "minecraft:slime_block"
}

// horizontalCollisionMinor is LocalPlayer.isHorizontalCollisionMinor: the
// player brushed a wall at less than 8° from where it meant to go.
func (p *Player) horizontalCollisionMinor(movement Vec3) bool {
	a := float64(p.Yaw * degToRad)
	s, c := float64(sin(a)), float64(cos(a))
	gx := float64(p.xxa)*c - float64(p.zza)*s
	gz := float64(p.zza)*c + float64(p.xxa)*s
	al := gx*gx + gz*gz
	ml := movement.X*movement.X + movement.Z*movement.Z
	if al < float64(float32(1.0e-5)) || ml < float64(float32(1.0e-5)) {
		return false
	}
	return math.Acos((gx*movement.X+gz*movement.Z)/math.Sqrt(al*ml)) < float64(float32(0.13962634))
}

// backOffFromEdge is Player.maybeBackOffFromEdge: sneaking on the ground, the
// player does not walk off an edge higher than a step.
func (p *Player) backOffFromEdge(w Blocks, attr Attributes, d Vec3) Vec3 {
	maxDown := float64(float32(attr.StepHeight))
	if d.Y > 0 || !p.keys.Shift || !p.aboveGround(w, maxDown) {
		return d
	}
	dx, dz := d.X, d.Z
	sx, sz := math.Copysign(0.05, dx), math.Copysign(0.05, dz)
	if dx == 0 {
		sx = 0
	}
	if dz == 0 {
		sz = 0
	}
	for dx != 0 && p.canFall(w, dx, 0, maxDown) {
		if math.Abs(dx) <= 0.05 {
			dx = 0
			break
		}
		dx -= sx
	}
	for dz != 0 && p.canFall(w, 0, dz, maxDown) {
		if math.Abs(dz) <= 0.05 {
			dz = 0
			break
		}
		dz -= sz
	}
	for dx != 0 && dz != 0 && p.canFall(w, dx, dz, maxDown) {
		if math.Abs(dx) <= 0.05 {
			dx = 0
		} else {
			dx -= sx
		}
		if math.Abs(dz) <= 0.05 {
			dz = 0
		} else {
			dz -= sz
		}
	}
	return Vec3{dx, d.Y, dz}
}

func (p *Player) aboveGround(w Blocks, maxDown float64) bool {
	return p.OnGround || p.FallDistance < maxDown && !p.canFall(w, 0, 0, maxDown-p.FallDistance)
}

func (p *Player) canFall(w Blocks, dx, dz, h float64) bool {
	b := p.Box()
	return noCollision(w, AABB{b.MinX + 1e-7 + dx, b.MinY - h - 1e-7, b.MinZ + 1e-7 + dz, b.MaxX - 1e-7 + dx, b.MinY, b.MaxZ - 1e-7 + dz})
}

// fits reports whether the player fits standing (or, with crouching, crouched).
func (p *Player) fits(w Blocks, crouching bool) bool {
	h := standingHeight
	if crouching {
		h = crouchingHeight
	}
	b := p.Box()
	return noCollision(w, AABB{b.MinX, b.MinY, b.MinZ, b.MaxX, b.MinY + h, b.MaxZ}.Deflate(1e-7))
}

// checkSupportingBlock is Entity.checkSupportingBlock: the block the player
// stands on, the closest one under its feet.
func (p *Player) checkSupportingBlock(w Blocks, movement Vec3) {
	if !p.OnGround {
		p.supporting = nil
		return
	}
	b := p.Box()
	area := AABB{b.MinX, b.MinY - 1e-6, b.MinZ, b.MaxX, b.MinY, b.MaxZ}
	if s := supportingBlock(w, area, p.Pos); s != nil {
		p.supporting = s
		return
	}
	p.supporting = supportingBlock(w, area.Move(Vec3{-movement.X, 0, -movement.Z}), p.Pos)
}

// supportingBlock is CollisionGetter.findSupportingBlock: of the blocks that
// collide with area, the one closest to pos, the lowest coordinates on a tie.
func supportingBlock(w Blocks, area AABB, pos Vec3) *world.BlockPos {
	var best *world.BlockPos
	bestDist := math.MaxFloat64
	for x := floor(area.MinX - 1e-7); x <= floor(area.MaxX+1e-7); x++ {
		for y := floor(area.MinY-1e-7) - 1; y <= floor(area.MaxY+1e-7); y++ {
			for z := floor(area.MinZ - 1e-7); z <= floor(area.MaxZ+1e-7); z++ {
				bp := world.BlockPos{X: x, Y: y, Z: z}
				state, ok := w.BlockAt(bp)
				if !ok {
					continue
				}
				hit := false
				for _, s := range block.CollisionShape(state) {
					if (AABB{s.MinX + float64(x), s.MinY + float64(y), s.MinZ + float64(z), s.MaxX + float64(x), s.MaxY + float64(y), s.MaxZ + float64(z)}).Intersects(area) {
						hit = true
						break
					}
				}
				if !hit {
					continue
				}
				c := Vec3{float64(x) + 0.5, float64(y) + 0.5, float64(z) + 0.5}.Sub(pos)
				if d := c.LengthSqr(); d < bestDist || d == bestDist && best != nil && lessPos(bp, *best) {
					bp := bp
					best, bestDist = &bp, d
				}
			}
		}
	}
	return best
}

// lessPos is Vec3i.compareTo: y, then z, then x.
func lessPos(a, b world.BlockPos) bool {
	if a.Y != b.Y {
		return a.Y < b.Y
	}
	if a.Z != b.Z {
		return a.Z < b.Z
	}
	return a.X < b.X
}

// onPos is Entity.getOnPos: the supporting block, or the block offset below the feet.
func (p *Player) onPos(offset float32) world.BlockPos {
	if p.supporting != nil {
		if offset <= 1.0e-5 {
			return *p.supporting
		}
		// (fences, walls and gates keep the supporting block; the robot's
		// worlds seldom need it)
		return world.BlockPos{X: p.supporting.X, Y: floor(p.Pos.Y - float64(offset)), Z: p.supporting.Z}
	}
	return world.BlockPos{X: floor(p.Pos.X), Y: floor(p.Pos.Y - float64(offset)), Z: floor(p.Pos.Z)}
}

func (p *Player) blockBelowAffectingMovement() world.BlockPos { return p.onPos(0.500001) }

func (p *Player) stateAt(w Blocks, pos world.BlockPos) block.StateID {
	s, _ := w.BlockAt(pos)
	return s
}

// blockSpeedFactor is LivingEntity.getBlockSpeedFactor.
func (p *Player) blockSpeedFactor(w Blocks, attr Attributes) float32 {
	here := p.stateAt(w, p.blockPos())
	f := block.BehaviourOf(here).SpeedFactor
	name := block.StateList[here].ID()
	if name != "minecraft:water" && name != "minecraft:bubble_column" && f == 1 {
		f = block.BehaviourOf(p.stateAt(w, p.blockBelowAffectingMovement())).SpeedFactor
	}
	e := float32(attr.MovementEfficiency)
	return f + (1-f)*e // Mth.lerp(efficiency, f, 1)
}

// blockJumpFactor is Entity.getBlockJumpFactor.
func (p *Player) blockJumpFactor(w Blocks) float32 {
	here := block.BehaviourOf(p.stateAt(w, p.blockPos())).JumpFactor
	if here == 1 {
		return block.BehaviourOf(p.stateAt(w, p.blockBelowAffectingMovement())).JumpFactor
	}
	return here
}

// onClimbable is LivingEntity.onClimbable for the block tag: ladders, vines,
// scaffolding (not the open trapdoor above a ladder).
func (p *Player) onClimbable(w Blocks, tags Tags) bool {
	return tags != nil && tags.BlockIn(p.stateAt(w, p.blockPos()), "minecraft:climbable")
}

// effectiveGravity is LivingEntity.getEffectiveGravity (PATCH): with slow
// falling and not going up, at most 0.01.
func (p *Player) effectiveGravity(attr Attributes, vy float64) float64 {
	if p.SlowFalling && vy <= 0 {
		return math.Min(attr.Gravity, 0.01)
	}
	return attr.Gravity
}
