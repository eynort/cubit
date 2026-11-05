package combat

// Ardiendo en plena pelea (aspecto ígneo, una flecha en llamas, su lava o su
// mechero): un punto por segundo durante 4-8 s que la armadura no para (en
// los logs, 7 de vida así contra uno de aspecto ígneo). Con cubo de agua lo
// apaga el reflejo (safety.Fire). Sin cubo, si hay agua a unos pasos, se
// mete en ella: es lo mismo.

import (
	"math"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/world"
)

// waterSearch: hasta dónde (en horizontal) se busca agua ardiendo: a más,
// el camino quema más de lo que se ahorra.
const waterSearch = 8

// toWater: ardiendo, sin cubo de agua ni resistencia al fuego, agua a unos
// pasos: allí (el walker). Dice si va.
func (f *Fighter) toWater(st basic.Status, t *foe) bool {
	if f.Burning == nil || !f.Burning() {
		return false
	}
	if _, res := st.Effects["minecraft:fire_resistance"]; res || f.hasItem("minecraft:water_bucket") {
		return false
	}
	if phys := f.d.Mover.Player(); phys == nil || phys.InWater || phys.InLava {
		return false
	}
	// pegado a él con el golpe a punto no: correr de espaldas es comerse un golpe (más que el fuego)
	if t != nil && t.dist < 2.5 {
		return false
	}
	// al lado: recto adentro (el pathfinding se para en la orilla)
	p := f.d.Player.Position()
	if f.isWater(f.waterGoal) && f.steps-f.waterAt < 200 {
		cx, cz := float64(f.waterGoal.X)+0.5, float64(f.waterGoal.Z)+0.5
		if math.Hypot(cx-p.X, cz-p.Z) < 2 && math.Abs(float64(f.waterGoal.Y)-p.Y) < 1.5 {
			if f.following != 0 {
				f.d.Walker.Stop()
				f.following = 0
			}
			f.setPlan(plan{mode: acting, steer: true, steerX: cx, steerZ: cz})
			return true
		}
	}
	if f.steps-f.waterAt > 40 || !f.isWater(f.waterGoal) {
		c, ok := f.nearWater()
		if !ok {
			return false
		}
		if c != f.waterGoal {
			f.logf("ardiendo y sin cubo: al agua de %v", c)
		}
		f.waterGoal, f.waterAt = c, f.steps
		f.d.Walker.SetSprint(true)
		f.d.Walker.Goto(c)
		f.following = fleeing
	}
	f.setPlan(plan{mode: retreating})
	return true
}

// nearWater: el agua más cerca (de pie en ella: la celda de los pies), a
// waterSearch como mucho y con suelo o agua debajo.
func (f *Fighter) nearWater() (world.BlockPos, bool) {
	p := f.d.Player.Position()
	feet := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
	best, bestD := world.BlockPos{}, math.Inf(1)
	for dx := -waterSearch; dx <= waterSearch; dx++ {
		for dz := -waterSearch; dz <= waterSearch; dz++ {
			for dy := -3; dy <= 1; dy++ {
				c := world.BlockPos{X: feet.X + dx, Y: feet.Y + dy, Z: feet.Z + dz}
				if !f.isWater(c) {
					continue
				}
				d := math.Hypot(float64(dx), float64(dz)) + math.Abs(float64(dy))
				if d < bestD && d <= waterSearch {
					best, bestD = c, d
				}
			}
		}
	}
	return best, !math.IsInf(bestD, 1)
}

func (f *Fighter) isWater(c world.BlockPos) bool {
	s, ok := f.d.World.BlockAt(c)
	return ok && strings.HasPrefix(world.StateString(s), "minecraft:water")
}
