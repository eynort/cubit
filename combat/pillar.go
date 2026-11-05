package combat

// La torre: contra mobs que pegan de cerca y no trepan (zombis y los suyos),
// retirándose y sin sitio al que huir (un callejón, una cueva), 3 bloques de
// torre en el sitio. Arriba no llegan (su golpe se mide con su caja: un zombi que salta
// contra la torre llega a quien está a 2, no a 3). Se come hasta tener vida
// (quieto: un paso y se cae) y se baja a pelear con ella (de 3 bloques la
// caída no hace daño). Ni con arañas (trepan), ni creepers (explotan igual),
// ni tiradores, ni jugadores (suben también).

import (
	"context"
	"math"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/world"
)

// pillarHeight: los bloques de la torre.
const pillarHeight = 3

type pillarState struct {
	climbing, on bool
	top          world.BlockPos
	best         float64 // la altura más alta subiendo
	since        uint64  // el paso en que la alcanzó
}

// pillarSafe: todos los enemigos son mobs que pegan de cerca y no suben.
func pillarSafe(foes []foe) bool {
	for _, fo := range foes {
		k := kind(fo.e)
		if fo.e.Type == "minecraft:player" || rangedMobs[k] || k == "creeper" || k == "spider" || k == "cave_spider" ||
			k == "phantom" || k == "vex" || k == "blaze" || k == "ghast" || k == "enderman" || k == "slime" || k == "magma_cube" {
			return false
		}
	}
	return len(foes) > 0
}

// pillarStep lleva la torre (subir, comer arriba, bajar). Mira él mismo los
// mobs que vienen (8 bloques), no solo los que ya pelean: hay que decidir
// antes de tenerlos encima. Dice si este paso se fue en eso (mientras sube o
// está arriba, manda ella).
func (f *Fighter) pillarStep(ctx context.Context, st basic.Status, foods []food) bool {
	p := f.d.Player.Position()
	feet := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
	var near []foe // los hostiles a menos de 8 (con su distancia)
	for _, e := range f.d.Entities.Nearby(p.X, p.Y, p.Z, 8) {
		if hostileMobs[kind(e)] && !dead(e) {
			near = append(near, foe{e: e, dist: math.Sqrt(e.DistanceSqr(p.X, p.Y, p.Z))})
		}
	}
	switch {
	case f.pil.on:
		if feet != f.pil.top || !pillarSafe(near) && len(near) > 0 {
			if feet != f.pil.top {
				f.logf("bajo de la torre")
			}
			f.pil = pillarState{}
			return false
		}
		// arriba: comer hasta tener vida; entonces, abajo a pelear (desde 3
		// bloques la caída no hace daño; desde arriba no se les pega: la
		// propia torre tapa a los que están pegados a ella)
		if st.Health >= 18 || len(near) == 0 || st.Health >= 14 && len(foods) == 0 && st.Food < 18 {
			f.logf("curado en la torre (vida %.1f): abajo a pelear", st.Health)
			f.pil = pillarState{}
			return false
		}
		if !f.d.Hands.Busy() {
			if fd, ok := pickFood(foods, 20-st.Food, true); ok {
				f.eat(ctx, fd, p.X, p.Z)
				return true
			}
			if fd, ok := pickFood(foods, 20-st.Food, false); ok && st.Food < 20 {
				f.eat(ctx, fd, p.X, p.Z)
				return true
			}
		}
		f.setPlan(plan{mode: eating}) // quieto (con el hambre llena se cura solo)
		return true
	case f.pil.climbing:
		// sin ganar altura en 20 ticks (un bloque que el servidor no pone: un
		// mob en medio): se deja, y a pelear
		if p.Y > f.pil.best+0.5 {
			f.pil.best, f.pil.since = p.Y, f.steps
		} else if f.steps-f.pil.since > 20 {
			f.logf("la torre no sube (algo en medio): a pelear")
			f.d.Walker.Stop()
			f.pil = pillarState{}
			return false
		}
		switch ws := f.d.Walker.Status(); {
		case ws == "arrived":
			f.pil.climbing, f.pil.on, f.pil.top = false, true, feet
			f.logf("en la torre: aquí no me llegan")
			return true
		case ws == "walking":
			f.setPlan(plan{mode: retreating}) // (la torre la sube el walker)
			return true
		}
		f.pil = pillarState{} // no se pudo
		return false
	}
	// ¿subir? cuesta ~30 ticks en los que le pegan si los tiene encima: con
	// tres o más viniendo y la vida justa, o con poca vida y nadie pegado aún.
	// Contra los que no suben ni disparan; en el suelo y con bloques
	if len(near) == 0 || !pillarSafe(near) || f.d.Walker.BuildBlocks() < pillarHeight || !f.d.Ctl.State().OnGround {
		return false
	}
	closest := math.Inf(1)
	for _, fo := range near {
		closest = math.Min(closest, fo.dist)
	}
	// solo sin salida: en campo abierto, retirarse a comer funciona mejor (los
	// zombis son lentos) y al bajar de la torre se está rodeado (medido: con
	// tierra y torre, peor que sin ella)
	if f.noWayOut == 0 || f.steps-f.noWayOut > 40 || closest < 2.5 {
		return false
	}
	// el bloque no se pone donde hay alguien: con su caja en la columna, no
	for _, fo := range near {
		if math.Abs(fo.e.X-(float64(feet.X)+0.5)) < 0.8 && math.Abs(fo.e.Z-(float64(feet.Z)+0.5)) < 0.8 && math.Abs(fo.e.Y-p.Y) < 2 {
			return false
		}
	}
	if !f.d.Walker.PillarUp(p, pillarHeight) {
		return false
	}
	f.following = 0
	f.pil = pillarState{climbing: true, best: p.Y, since: f.steps}
	f.logf("vida %.1f y %d viniendo: torre de %d (no me alcanzan)", st.Health, len(near), pillarHeight)
	f.setPlan(plan{mode: retreating})
	return true
}
