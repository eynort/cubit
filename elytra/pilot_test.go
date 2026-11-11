package elytra

import (
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

var stone, air, water, lava = block.ToStateID[block.Stone{}], block.ToStateID[block.Air{}], block.ToStateID[block.Water{}], block.ToStateID[block.Lava{}]

// terrain es un mundo en memoria: piedra hasta height(x, z), aire encima;
// lo puesto en set manda.
type terrain struct {
	height func(x, z int) int
	set    map[world.BlockPos]block.StateID
	loaded float64 // radio cargado alrededor del centro (0: todo)
	cx, cz float64
}

func (t *terrain) BlockAt(p world.BlockPos) (block.StateID, bool) {
	if t.loaded > 0 && math.Hypot(float64(p.X)-t.cx, float64(p.Z)-t.cz) > t.loaded {
		return 0, false
	}
	if s, ok := t.set[p]; ok {
		return s, true
	}
	if p.Y <= t.height(p.X, p.Z) {
		return stone, true
	}
	return air, true
}

func flat(int, int) int { return 63 }

var trace = os.Getenv("TRACE")

// flight es el resultado de un vuelo simulado.
type flight struct {
	ok           bool
	why          string
	ticks        int
	end          physics.Vec3
	fallDamage   float64
	wallDamage   float64
	rocketsUsed  int
	maxY         float64
	minClearance float64
}

// fly simula un vuelo con la física del cliente: el cohete llega con lag
// ticks de retraso (lo que tarda el servidor en crearlo) y dura 20-31 ticks.
func fly(t *testing.T, w *terrain, start physics.Vec3, goal [2]float64, rockets, lag, maxTicks int) flight {
	t.Helper()
	return flyY(t, w, start, goal, math.NaN(), rockets, lag, maxTicks)
}

// flyY es fly con la altura de la meta (NaN: no se sabe).
func flyY(t *testing.T, w *terrain, start physics.Vec3, goal [2]float64, goalY float64, rockets, lag, maxTicks int) flight {
	t.Helper()
	p := physics.NewPlayer(start)
	pl := &Pilot{World: w, Goal: goal, GoalY: goalY, Logf: t.Logf}
	rng := rand.New(rand.NewPCG(1, 2))
	attr := physics.DefaultAttributes
	var res flight
	res.minClearance = math.Inf(1)
	var pending []int // cohetes en camino: ticks que les faltan
	boost := 0
	have := rockets
	for i := 0; i < 5 && !p.OnGround; i++ { // de pie
		p.Tick(w, nil, physics.Keys{}, attr)
	}
	for tick := 1; tick <= maxTicks; tick++ {
		w.cx, w.cz = p.Pos.X, p.Pos.Z // los chunks van llegando alrededor
		o := Obs{Pos: p.Pos, Vel: p.Vel, Yaw: p.Yaw, Pitch: p.Pitch, OnGround: p.OnGround, FallFlying: p.FallFlying,
			InWater: p.InWater, Boosting: boost > 0, Rockets: have, Tick: tick}
		c := pl.Step(o)
		p.Yaw, p.Pitch = c.Yaw, c.Pitch
		if c.Fire && have > 0 {
			have--
			res.rocketsUsed++
			pending = append(pending, lag)
		}
		for i := 0; i < len(pending); i++ {
			if pending[i] == 0 {
				// el cohete aparece enganchado a él y vive su vida entera: tira
				// mientras planee (si tocó suelo y vuelve a planear, otra vez)
				boost = max(boost, 20+rng.IntN(6)+rng.IntN(7))
				pending = append(pending[:i], pending[i+1:]...)
				i--
				continue
			}
			pending[i]--
		}
		p.CanGlide = true
		p.Rocket = boost > 0
		hBefore := math.Hypot(p.Vel.X, p.Vel.Z)
		fallBefore := p.FallDistance
		wasGliding := p.FallFlying
		p.Tick(w, nil, physics.Keys{Jump: c.Jump}, attr)
		if boost > 0 {
			boost--
		}
		if p.OnGround {
			fall := fallBefore + math.Max(0, -p.Vel.Y)
			if fall > 3 {
				res.fallDamage += math.Ceil(fall - 3)
			}
		}
		if wasGliding && p.HorizontalCollision {
			if dmg := (hBefore-math.Hypot(p.Vel.X, p.Vel.Z))*10 - 3; dmg > 0 {
				res.wallDamage += dmg
				t.Logf("CHOQUE t=%d en %.1f %.1f %.1f (%s): %.1f de daño", tick, p.Pos.X, p.Pos.Y, p.Pos.Z, pl.Status(), dmg)
			}
		}
		res.maxY = math.Max(res.maxY, p.Pos.Y)
		if p.FallFlying && !p.OnGround {
			if gy, ok := pl.groundAt(p.Pos.X, p.Pos.Z, p.Pos.Y); ok {
				res.minClearance = math.Min(res.minClearance, p.Pos.Y-gy)
			}
		}
		if trace != "" && t.Name() == trace {
			t.Logf("t=%d pos=%.1f %.2f %.1f vel=%.2f %.2f %.2f yaw=%.0f pitch=%.0f suelo=%v planea=%v cohete=%v J=%v F=%v %s",
				tick, p.Pos.X, p.Pos.Y, p.Pos.Z, p.Vel.X, p.Vel.Y, p.Vel.Z, p.Yaw, p.Pitch, p.OnGround, p.FallFlying, boost > 0, c.Jump, c.Fire, pl.Status())
		}
		if fin, why := pl.Done(); fin {
			res.ok, res.why, res.ticks, res.end = why == "", why, tick, p.Pos
			return res
		}
	}
	res.why, res.ticks, res.end = "sin terminar: "+pl.String(), maxTicks, p.Pos
	return res
}

func check(t *testing.T, r flight, goal [2]float64, near float64) {
	t.Helper()
	d := math.Hypot(r.end.X-goal[0], r.end.Z-goal[1])
	t.Logf("%v en %d ticks (%.0f s): a %.1f de la meta, en %.1f %.2f %.1f; cohetes %d; altura máx %.0f; daño caída %.0f, choque %.1f",
		r.ok, r.ticks, float64(r.ticks)/20, d, r.end.X, r.end.Y, r.end.Z, r.rocketsUsed, r.maxY, r.fallDamage, r.wallDamage)
	if !r.ok {
		t.Fatalf("no aterrizó: %s", r.why)
	}
	if d > near {
		t.Errorf("aterrizó a %.1f bloques de la meta (máx %.0f)", d, near)
	}
	// sin daño de caída; de rozar una ladera o el borde de una plataforma al
	// tocar suelo, menos de medio corazón (0.5-0.8 en ~3 de cada 800 vuelos al azar)
	if r.fallDamage > 0 || r.wallDamage >= 1 {
		t.Errorf("se hizo daño: caída %.0f, choque %.1f", r.fallDamage, r.wallDamage)
	}
}

func TestFlyFlat(t *testing.T) {
	w := &terrain{height: flat}
	goal := [2]float64{600, 200}
	r := fly(t, w, physics.Vec3{X: 0.5, Y: 64, Z: 0.5}, goal, 64, 2, 20*120)
	check(t, r, goal, 24)
}

func TestFlyMountains(t *testing.T) {
	// montañas: hasta 60 bloques por encima, y un pico alto en la línea recta
	h := func(x, z int) int {
		v := 63 + 30*math.Sin(float64(x)/37)*math.Cos(float64(z)/29) + 30
		if math.Hypot(float64(x)-300, float64(z)-100) < 20 {
			v = 170
		}
		return int(math.Max(63, v))
	}
	w := &terrain{height: h, loaded: 96}
	start := physics.Vec3{X: 0.5, Y: float64(h(0, 0)) + 1, Z: 0.5}
	goal := [2]float64{620, 210}
	r := fly(t, w, start, goal, 64, 2, 20*150)
	check(t, r, goal, 40)
}

func TestFlyFewRockets(t *testing.T) {
	// pocos cohetes: llega hasta donde llegue, y aterriza sin hacerse daño
	w := &terrain{height: flat}
	goal := [2]float64{900, 0}
	r := fly(t, w, physics.Vec3{X: 0.5, Y: 64, Z: 0.5}, goal, 4, 2, 20*120)
	t.Logf("con 4 cohetes: hasta x=%.0f", r.end.X)
	if !r.ok || r.fallDamage > 0 || r.wallDamage >= 0.5 {
		t.Fatalf("ok=%v %s, daño caída %.0f choque %.1f", r.ok, r.why, r.fallDamage, r.wallDamage)
	}
}

func TestFlyAvoidsLavaLanding(t *testing.T) {
	// la meta está en un lago de lava: aterriza al lado, en piedra
	w := &terrain{height: flat, set: map[world.BlockPos]block.StateID{}}
	for x := 290; x <= 302; x++ {
		for z := -6; z <= 6; z++ {
			w.set[world.BlockPos{X: x, Y: 63, Z: z}] = lava
		}
	}
	goal := [2]float64{296, 0}
	r := fly(t, w, physics.Vec3{X: 0.5, Y: 64, Z: 0.5}, goal, 64, 2, 20*90)
	check(t, r, goal, 30)
	for dy := -1; dy <= 0; dy++ {
		if s, _ := w.BlockAt(world.BlockPos{X: fl(r.end.X), Y: fl(r.end.Y) + dy, Z: fl(r.end.Z)}); s == lava {
			t.Fatalf("aterrizó en la lava")
		}
	}
}

func TestFlyLandsInWater(t *testing.T) {
	// la meta en el mar: cae al agua (sin daño), y lo demás es nadar
	w := &terrain{height: func(x, z int) int { return 50 }, set: map[world.BlockPos]block.StateID{}}
	for x := -20; x <= 420; x++ {
		for z := -40; z <= 40; z++ {
			for y := 51; y <= 62; y++ {
				if x > 150 {
					w.set[world.BlockPos{X: x, Y: y, Z: z}] = water
				} else {
					w.set[world.BlockPos{X: x, Y: y, Z: z}] = stone
				}
			}
		}
	}
	goal := [2]float64{400, 0}
	r := fly(t, w, physics.Vec3{X: 0.5, Y: 63, Z: 0.5}, goal, 64, 2, 20*90)
	if !r.ok || r.fallDamage > 0 || r.wallDamage >= 0.5 {
		t.Fatalf("ok=%v %s, daño caída %.0f choque %.1f", r.ok, r.why, r.fallDamage, r.wallDamage)
	}
}

func TestFlyCliff(t *testing.T) {
	// un muro de 100 bloques de alto cruzado en el camino: lo ve tarde (chunks a 48)
	h := func(x, z int) int {
		if x >= 300 && x <= 305 {
			return 165
		}
		return 63
	}
	w := &terrain{height: h, loaded: 48}
	goal := [2]float64{520, 30}
	r := fly(t, w, physics.Vec3{X: 0.5, Y: 64, Z: 0.5}, goal, 64, 3, 20*90)
	check(t, r, goal, 30)
}

// TestFlyRandom: muchos vuelos con terreno, meta, lag y distancia de chunks
// al azar: siempre aterriza, sin hacerse daño, cerca de la meta.
func TestFlyRandom(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 7))
	n := 40
	if os.Getenv("MANY") != "" {
		n = 400
	}
	for i := 0; i < n; i++ {
		a, b, c := rng.Float64()*40+15, rng.Float64()*40+15, rng.Float64()*70
		base := 63 + rng.IntN(20)
		h := func(x, z int) int {
			v := float64(base) + c*math.Sin(float64(x)/a)*math.Cos(float64(z)/b)
			return int(math.Max(float64(base)-10, v))
		}
		dist := 150 + rng.Float64()*900
		ang := rng.Float64() * 2 * math.Pi
		goal := [2]float64{math.Cos(ang) * dist, math.Sin(ang) * dist}
		lag := 1 + rng.IntN(6)
		loaded := []float64{0, 48, 64, 96}[rng.IntN(4)]
		w := &terrain{height: h, loaded: loaded}
		start := physics.Vec3{X: 0.5, Y: float64(h(0, 0)) + 1, Z: 0.5}
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			r := fly(t, w, start, goal, 64, lag, int(dist/1.2)+20*60)
			t.Logf("distancia %.0f, relieve %.0f, lag %d, cargado %.0f", dist, c, lag, loaded)
			check(t, r, goal, 40)
		})
	}
}

// TestFlyToTowerTop: el jugador está en lo alto de una torre: aterriza arriba, con él.
func TestFlyToTowerTop(t *testing.T) {
	h := func(x, z int) int {
		if x >= 398 && x <= 402 && z >= 28 && z <= 32 {
			return 140
		}
		return 63
	}
	w := &terrain{height: h}
	goal := [2]float64{400.5, 30.5}
	r := flyY(t, w, physics.Vec3{X: 0.5, Y: 64, Z: 0.5}, goal, 141, 64, 3, 20*120)
	check(t, r, goal, 6)
	if math.Abs(r.end.Y-141) > 0.5 {
		t.Fatalf("aterrizó en y=%.1f, él está en y=141 (lo alto de la torre)", r.end.Y)
	}
}

// TestFlyToLowerLayer: el jugador está abajo, al pie de una meseta 26 bloques
// más alta: aterriza abajo, a su altura, no encima de la meseta.
func TestFlyToLowerLayer(t *testing.T) {
	h := func(x, z int) int {
		if x >= 395 {
			return 89
		}
		return 63
	}
	w := &terrain{height: h}
	goal := [2]float64{392.5, 0.5}
	r := flyY(t, w, physics.Vec3{X: 0.5, Y: 64, Z: 0.5}, goal, 64, 64, 3, 20*120)
	check(t, r, goal, 20)
	if math.Abs(r.end.Y-64) > 0.5 {
		t.Fatalf("aterrizó en y=%.1f, él está en y=64 (abajo, no en la meseta)", r.end.Y)
	}
}

// TestFlyToCave: el jugador está en una cueva (bajo tierra): no se llega
// volando; aterriza en la superficie, encima, y lo dice.
func TestFlyToCave(t *testing.T) {
	w := &terrain{height: flat}
	var logs []string
	goal := [2]float64{300.5, 0.5}
	p := physics.NewPlayer(physics.Vec3{X: 0.5, Y: 64, Z: 0.5})
	_ = p
	pl := &Pilot{World: w, Goal: goal, GoalY: 30, Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }}
	pl.startLanding(Obs{Pos: physics.Vec3{X: 280, Y: 90, Z: 0}})
	if pl.landY != 64 || math.Hypot(pl.landGoal[0]-goal[0], pl.landGoal[1]-goal[1]) > 2 {
		t.Fatalf("pista en %.0f %.0f y=%.0f; se esperaba encima, en la superficie", pl.landGoal[0], pl.landGoal[1], pl.landY)
	}
	found := false
	for _, l := range logs {
		found = found || strings.Contains(l, "no se llega a esa altura")
	}
	if !found {
		t.Errorf("no avisó de que no llega a la altura de la cueva: %q", logs)
	}
}

// TestTakeoffSlowRocket: el cohete tarda 12 ticks en llegar (el servidor de
// los logs): despega a la primera, sin volver a tocar suelo.
func TestTakeoffSlowRocket(t *testing.T) {
	w := &terrain{height: flat}
	var retakes int
	goal := [2]float64{300, 0}
	p := physics.NewPlayer(physics.Vec3{X: 0.5, Y: 64, Z: 0.5})
	takeoffs := 0
	pl := &Pilot{World: w, Goal: goal, GoalY: math.NaN(), Logf: func(f string, a ...any) {
		if strings.Contains(f, "antes de tiempo") || strings.Contains(f, "despegué") {
			takeoffs++
		}
	}}
	attr := physics.DefaultAttributes
	for i := 0; i < 5 && !p.OnGround; i++ {
		p.Tick(w, nil, physics.Keys{}, attr)
	}
	pending, boost := -1, 0
	for tick := 1; tick < 200; tick++ {
		c := pl.Step(Obs{Pos: p.Pos, Vel: p.Vel, Yaw: p.Yaw, Pitch: p.Pitch, OnGround: p.OnGround, FallFlying: p.FallFlying, Boosting: boost > 0, Rockets: 10, Tick: tick})
		p.Yaw, p.Pitch = c.Yaw, c.Pitch
		if c.Fire {
			pending = 12
		}
		if pending == 0 {
			boost = 25
		}
		if pending >= 0 {
			pending--
		}
		p.CanGlide, p.Rocket = true, boost > 0
		wasGliding := p.FallFlying
		p.Tick(w, nil, physics.Keys{Jump: c.Jump}, attr)
		if wasGliding && !p.FallFlying && p.Pos.Y < 70 {
			retakes++ // se le cerró el planeo tocando suelo: un despegue fallido
		}
		if boost > 0 {
			boost--
		}
	}
	_ = takeoffs
	t.Logf("tras 10 s: en y=%.0f, despegues repetidos %d", p.Pos.Y, retakes)
	if retakes > 0 || p.Pos.Y < 75 {
		t.Fatalf("con el cohete lento: %d despegues repetidos, y=%.0f", retakes, p.Pos.Y)
	}
}

// TestFlyUpToPlayerAbove: el caso de los logs: él en lo alto de una torre a
// y=191, el bot abajo a 22 bloques de lado, y el cohete tarda 12 ticks:
// sube y aterriza arriba, con él (antes "aterrizaba" nada más despegar).
func TestFlyUpToPlayerAbove(t *testing.T) {
	h := func(x, z int) int {
		if x >= -3 && x <= 1 && z >= -11 && z <= -7 {
			return 190
		}
		return 64
	}
	w := &terrain{height: h}
	goal := [2]float64{-0.5, -8.5}
	r := flyY(t, w, physics.Vec3{X: -24.5, Y: 65, Z: -4.5}, goal, 191, 32, 12, 20*90)
	check(t, r, goal, 6)
	if math.Abs(r.end.Y-191) > 0.5 {
		t.Fatalf("terminó en y=%.1f; él está arriba, en y=191", r.end.Y)
	}
}

// TestFlyRandomTowers: él en lo alto de torres al azar (tamaño, altura,
// distancia, lag del cohete): el bot acaba arriba, con él, sin daño.
func TestFlyRandomTowers(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 9))
	n := 30
	if os.Getenv("MANY") != "" {
		n = 200
	}
	for i := 0; i < n; i++ {
		size := 3 + rng.IntN(5)
		top := 63 + 30 + rng.IntN(100)
		dist := 10 + rng.Float64()*190
		ang := rng.Float64() * 2 * math.Pi
		cx, cz := int(math.Cos(ang)*dist), int(math.Sin(ang)*dist)
		lag := 1 + rng.IntN(12)
		h := func(x, z int) int {
			if x >= cx && x < cx+size && z >= cz && z < cz+size {
				return top
			}
			return 63
		}
		goal := [2]float64{float64(cx) + float64(size)/2, float64(cz) + float64(size)/2}
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			r := flyY(t, &terrain{height: h}, physics.Vec3{X: 0.5, Y: 64, Z: 0.5}, goal, float64(top+1), 64, lag, 20*120)
			t.Logf("torre %dx%d hasta y=%d a %.0f bloques, lag %d", size, size, top+1, dist, lag)
			check(t, r, goal, 6)
			if math.Abs(r.end.Y-float64(top+1)) > 0.5 {
				t.Fatalf("terminó en y=%.1f; él está arriba, en y=%d", r.end.Y, top+1)
			}
		})
	}
}

// TestFlyRandomWithY: los vuelos al azar, sabiendo a qué altura está él (de
// pie en el suelo de la meta): aterriza a su altura, cerca, sin daño.
func TestFlyRandomWithY(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 5))
	n := 30
	if os.Getenv("MANY") != "" {
		n = 200
	}
	for i := 0; i < n; i++ {
		a, b, c := rng.Float64()*40+15, rng.Float64()*40+15, rng.Float64()*70
		base := 63 + rng.IntN(20)
		h := func(x, z int) int {
			v := float64(base) + c*math.Sin(float64(x)/a)*math.Cos(float64(z)/b)
			return int(math.Max(float64(base)-10, v))
		}
		dist := 150 + rng.Float64()*900
		ang := rng.Float64() * 2 * math.Pi
		goal := [2]float64{math.Floor(math.Cos(ang)*dist) + 0.5, math.Floor(math.Sin(ang)*dist) + 0.5}
		gy := float64(h(int(math.Floor(goal[0])), int(math.Floor(goal[1]))) + 1)
		lag := 1 + rng.IntN(12)
		w := &terrain{height: h, loaded: []float64{0, 64, 96}[rng.IntN(3)]}
		start := physics.Vec3{X: 0.5, Y: float64(h(0, 0)) + 1, Z: 0.5}
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			r := flyY(t, w, start, goal, gy, 64, lag, int(dist/1.2)+20*90)
			t.Logf("distancia %.0f, relieve %.0f, él en y=%.0f, lag %d", dist, c, gy, lag)
			check(t, r, goal, 40)
			// a su altura, o tan cerca que lo que queda se anda (una ladera)
			if math.Abs(r.end.Y-gy) > 6 && math.Hypot(r.end.X-goal[0], r.end.Z-goal[1]) > 10 {
				t.Errorf("terminó en y=%.1f, él está en y=%.0f", r.end.Y, gy)
			}
		})
	}
}

// TestFlyFollowMoving: seguir a alguien que se va volando (su altura en el
// aire no cuenta) y aterriza en lo alto de una torre: el bot va tras él y
// aterriza arriba, a su lado.
func TestFlyFollowMoving(t *testing.T) {
	h := func(x, z int) int {
		if x >= 598 && x <= 602 && z >= 198 && z <= 202 {
			return 130
		}
		return 63
	}
	w := &terrain{height: h}
	p := physics.NewPlayer(physics.Vec3{X: 0.5, Y: 64, Z: 0.5})
	pl := &Pilot{World: w, Goal: [2]float64{300, 0}, GoalY: math.NaN(), Logf: t.Logf}
	attr := physics.DefaultAttributes
	for i := 0; i < 5 && !p.OnGround; i++ {
		p.Tick(w, nil, physics.Keys{}, attr)
	}
	var pending []int
	boost := 0
	// él: de (300, 0) a (600.5, 200.5) volando (1 bloque por tick, a y=150), y aterriza en la torre (y=131)
	him := func(tick int) (x, y, z float64, onGround bool) {
		f := math.Min(1, float64(tick)/360)
		if f >= 1 {
			return 600.5, 131, 200.5, true
		}
		return 300 + f*300.5, 150, f * 200.5, false
	}
	for tick := 1; tick <= 20*120; tick++ {
		if tick%5 == 0 {
			x, y, z, on := him(tick)
			if !on {
				y = math.NaN()
			}
			pl.Retarget(x, y, z)
		}
		c := pl.Step(Obs{Pos: p.Pos, Vel: p.Vel, Yaw: p.Yaw, Pitch: p.Pitch, OnGround: p.OnGround, FallFlying: p.FallFlying, Boosting: boost > 0, Rockets: 40, Tick: tick})
		p.Yaw, p.Pitch = c.Yaw, c.Pitch
		if c.Fire {
			pending = append(pending, 6)
		}
		for i := 0; i < len(pending); i++ {
			if pending[i] == 0 {
				boost = 25
				pending = append(pending[:i], pending[i+1:]...)
				i--
				continue
			}
			pending[i]--
		}
		p.CanGlide, p.Rocket = true, boost > 0
		p.Tick(w, nil, physics.Keys{Jump: c.Jump}, attr)
		if boost > 0 {
			boost--
		}
		if fin, why := pl.Done(); fin {
			t.Logf("terminó (%q) en %d ticks en %.1f %.1f %.1f", why, tick, p.Pos.X, p.Pos.Y, p.Pos.Z)
			if why != "" || math.Abs(p.Pos.Y-131) > 0.5 || math.Hypot(p.Pos.X-600.5, p.Pos.Z-200.5) > 5 {
				t.Fatalf("no aterrizó con él en lo alto de la torre (600 131 200)")
			}
			return
		}
	}
	t.Fatalf("no terminó: %s en %.1f %.1f %.1f", pl.String(), p.Pos.X, p.Pos.Y, p.Pos.Z)
}
