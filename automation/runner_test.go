package automation_test

import (
	"context"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/automation"
	"minecraft-bot/pathing"
	"minecraft-bot/safety"
)

// runner: el ejecutor de tareas sobre el bot del mundo simulado, con caminos.
func runner(t *testing.T, g *simworld.Game) *automation.Runner {
	b := g.B
	b.Mover.Food = func() int { return 20 }
	w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
	bd := &pathing.HandsBuilder{Screens: b.Screens, Hands: b.Hands, Ctl: b.Ctl, Ctx: context.Background()}
	w.SetBuilder(bd)
	b.Ctl.Think = w.Think
	return automation.New(automation.Deps{
		Client: b.C, Player: b.Player, World: b.World, Entities: b.Entities, Screens: b.Screens,
		Hands: b.Hands, Ctl: b.Ctl, Walker: w, Builder: bd, Fighting: func() bool { return false },
		HasFood: func() bool { return true },
	})
}

// safeRunner es runner con el reflejo de la lava (y el fuego) delante del walker.
func safeRunner(t *testing.T, g *simworld.Game) (*automation.Runner, *safety.Lava) {
	b := g.B
	b.Mover.Food = func() int { return 20 }
	w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
	bd := &pathing.HandsBuilder{Screens: b.Screens, Hands: b.Hands, Ctl: b.Ctl, Ctx: context.Background()}
	w.SetBuilder(bd)
	lava := &safety.Lava{World: b.World, Mover: b.Mover, Stop: w.Stop, Logf: t.Logf,
		Extinguish: func(p world.BlockPos) { b.Hands.Dig(p, nil) }}
	b.Ctl.Think = func(s *control.State, bp *basic.Player) {
		if lava.Think(s, bp) {
			return
		}
		w.Think(s, bp)
	}
	return automation.New(automation.Deps{
		Client: b.C, Player: b.Player, World: b.World, Entities: b.Entities, Screens: b.Screens,
		Hands: b.Hands, Ctl: b.Ctl, Walker: w, Builder: bd, Fighting: func() bool { return false },
		HasFood: func() bool { return true }, Escaping: lava.Escaping,
	}), lava
}
