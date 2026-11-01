package physics

import (
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
)

// Mover moves a controller's player with this package's physics: set
// controller.Step to its Step. The position it starts from, and any teleport
// later, come from the basic.Player.
type Mover struct {
	World Blocks
	Tags  Tags
	// Food, when set, says the player's food level (sprinting needs more than 6).
	Food func() int
	// Trace, when set, sees the player after every tick.
	Trace func(*Player)

	// PATCH (elytra): CanGlide says a glider is worn that is not about to
	// break; Rocket, that a firework rocket is attached to the player now;
	// FallFlying, the shared flag 7 as the server last told it, and a count
	// that changes each time it tells (nil: never told).
	CanGlide   func() bool
	Rocket     func() bool
	FallFlying func() (on bool, told uint64)
	toldGlide  uint64

	p    *Player
	last basic.Position // what the last Step wrote
}

// Player returns the simulated player, nil before the first step.
func (m *Mover) Player() *Player { return m.p }

// Step is a control.Controller's Step.
func (m *Mover) Step(s *control.State, bp *basic.Player) {
	pos := bp.Position()
	if m.p == nil {
		m.p = NewPlayer(Vec3{pos.X, pos.Y, pos.Z})
	}
	p := m.p
	if pos.X != m.last.X || pos.Y != m.last.Y || pos.Z != m.last.Z {
		// a teleport (or the spawn) put the player elsewhere
		p.Pos = Vec3{pos.X, pos.Y, pos.Z}
		p.Vel = Vec3{pos.VX, pos.VY, pos.VZ}
		p.FallDistance = 0
	} else if pos.VX != m.last.VX || pos.VY != m.last.VY || pos.VZ != m.last.VZ {
		// the server set the velocity (knockback)
		p.Vel = Vec3{pos.VX, pos.VY, pos.VZ}
	}
	p.Yaw, p.Pitch = pos.Yaw, pos.Pitch
	if m.Food != nil {
		p.FoodLevel = m.Food()
	}
	// PATCH: the effects that change the movement, as the server told them
	eff := bp.Status().Effects
	p.JumpBoost, p.SlowFalling, p.Levitation = 0, false, 0
	if e, ok := eff["minecraft:jump_boost"]; ok {
		p.JumpBoost = 0.1 * float32(e.Amplifier+1)
	}
	if _, ok := eff["minecraft:slow_falling"]; ok {
		p.SlowFalling = true
	}
	if e, ok := eff["minecraft:levitation"]; ok {
		p.Levitation = e.Amplifier + 1
	}

	// PATCH (elytra): the server's word on the glide wins over the client's
	if m.FallFlying != nil {
		if on, told := m.FallFlying(); told != m.toldGlide {
			m.toldGlide, p.FallFlying = told, on
		}
	}
	p.CanGlide = m.CanGlide != nil && m.CanGlide()
	p.Rocket = m.Rocket != nil && m.Rocket()

	p.UsingItem = s.UsingItem
	p.Tick(m.World, m.Tags, Keys(s.Input), attributesOf(bp))
	if p.StartedFallFlying {
		s.StartFallFlying = true // sent with this tick's changes
	}
	s.FallFlying = p.FallFlying
	if m.Trace != nil {
		m.Trace(p)
	}

	s.OnGround, s.HorizontalCollision, s.Sprinting = p.OnGround, p.HorizontalCollision, p.Sprinting
	next := basic.Position{X: p.Pos.X, Y: p.Pos.Y, Z: p.Pos.Z, Yaw: pos.Yaw, Pitch: pos.Pitch, VX: p.Vel.X, VY: p.Vel.Y, VZ: p.Vel.Z}
	bp.UpdatePosition(func(cur basic.Position) basic.Position {
		if cur.X != pos.X || cur.Y != pos.Y || cur.Z != pos.Z {
			return cur // a teleport arrived while this tick ran: it wins
		}
		next.Yaw, next.Pitch = cur.Yaw, cur.Pitch // a look while this tick ran
		m.last = next
		return next
	})
}

// attributesOf reads the movement attributes the server sent, the defaults
// for any it did not (a version without the attribute, or before the first
// update). The sprint modifier is the physics' own to apply.
func attributesOf(bp *basic.Player) Attributes {
	a := DefaultAttributes
	for name, f := range map[string]*float64{
		"minecraft:movement_speed":            &a.MovementSpeed,
		"minecraft:jump_strength":             &a.JumpStrength,
		"minecraft:gravity":                   &a.Gravity,
		"minecraft:step_height":               &a.StepHeight,
		"minecraft:sneaking_speed":            &a.SneakingSpeed,
		"minecraft:movement_efficiency":       &a.MovementEfficiency,
		"minecraft:water_movement_efficiency": &a.WaterMovementEfficiency,
		"minecraft:friction_modifier":         &a.FrictionModifier,
		"minecraft:air_drag_modifier":         &a.AirDragModifier,
		"minecraft:bounciness":                &a.Bounciness,
	} {
		if v, ok := bp.Attribute(name); ok {
			*f = v.Value("minecraft:sprinting")
		}
	}
	return a
}
