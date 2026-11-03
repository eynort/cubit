package pathing_test

import (
	"os"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
)

type traced struct {
	mover
	t *testing.T
}

func (tr traced) Think(s *control.State, bp *basic.Player) {
	tr.mover.Think(s, bp)
	p := bp.Position()
	tr.t.Logf("t=%d pos=%.2f %.2f %.2f yaw=%.0f ground=%v coll=%v J=%v F=%v st=%s %s", s.Ticks, p.X, p.Y, p.Z, p.Yaw, s.OnGround, s.HorizontalCollision, s.Input.Jump, s.Input.Forward, tr.mover.Status(), dbg(tr.mover))
}

func TestTrace(t *testing.T) {
	name := os.Getenv("COURSE")
	if name == "" {
		t.Skip("COURSE=nombre")
	}
	for _, c := range courses {
		if c.name == name {
			r := runMode(t, c, func(b *fakeserver.Bot) mover { return traced{ourWalker(b), t} }, os.Getenv("MODE") != "real")
			t.Logf("%+v", r)
		}
	}
}

func dbg(m mover) string {
	if d, ok := m.(interface{ Debug() string }); ok {
		return d.Debug()
	}
	return ""
}
