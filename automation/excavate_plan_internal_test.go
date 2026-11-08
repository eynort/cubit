package automation

import (
	"fmt"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/world"
)

// TestPassPlan: las pasadas salen del alto de la caja: las menos posibles de
// hasta 4 capas, repartidas, de arriba abajo y sin huecos ni solapes.
func TestPassPlan(t *testing.T) {
	for _, c := range []struct {
		h    int
		want string
	}{{1, "[1]"}, {3, "[3]"}, {4, "[4]"}, {5, "[3 2]"}, {6, "[3 3]"}, {7, "[4 3]"}, {8, "[4 4]"}, {9, "[3 3 3]"}, {10, "[4 3 3]"}, {16, "[4 4 4 4]"}, {19, "[4 4 4 4 3]"}} {
		box := NewBox(world.BlockPos{X: 0, Y: 70, Z: 0}, world.BlockPos{X: 4, Y: 70 - c.h + 1, Z: 4})
		plan := passPlan(box)
		var sizes []int
		top := box.Max.Y
		for _, ps := range plan {
			sizes = append(sizes, ps[1])
			if ps[0]+ps[1]-1 != top {
				t.Errorf("alto %d: la pasada %v no empieza justo debajo de la anterior (%d)", c.h, ps, top)
			}
			top = ps[0] - 1
		}
		if top != box.Min.Y-1 {
			t.Errorf("alto %d: las pasadas no llegan al fondo (%v)", c.h, plan)
		}
		if got := fmt.Sprint(sizes); got != c.want {
			t.Errorf("alto %d: %s, quería %s", c.h, got, c.want)
		}
	}
}
