// Package elytra vuela con élitros y cohetes: despega, sube a una altura
// por encima del terreno que viene, va en línea recta hacia la meta con
// cohetes cuando le falta velocidad o altura, esquiva lo que tenga delante
// subiendo, y aterriza planeando suave (sin caer más rápido de lo que
// duele) cerca de la meta. Lo que queda, se anda.
//
// Pilot es la decisión de cada tick (sin red: se prueba con la física sola);
// Flight lo conecta al bot (ponerse los élitros, los cohetes en la mano, el
// cohete enganchado, lo que dice el servidor).
package elytra

import (
	"fmt"
	"math"

	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"

	"minecraft-bot/pathing"
)

// Obs es lo que el piloto ve en un tick.
type Obs struct {
	Pos, Vel   physics.Vec3
	Yaw, Pitch float32
	OnGround   bool
	FallFlying bool // planeando (como lo tiene la física del cliente)
	InWater    bool
	Boosting   bool // un cohete tira de él ahora
	Rockets    int  // cohetes que le quedan
	Tick       int
}

// Cmd es lo que el piloto hace en un tick.
type Cmd struct {
	Yaw, Pitch float32
	Jump       bool
	Fire       bool // usar un cohete (con la mirada de este tick)
}

type phase int

const (
	takeoff phase = iota
	cruise
	land
	done
	failed
)

func (p phase) String() string {
	return [...]string{"despegando", "en vuelo", "aterrizando", "aterrizado", "fallo"}[p]
}

// Ajustes del vuelo (probados con la física: pilot_test.go).
const (
	cruiseMargin = 20.0 // altura de crucero sobre el terreno más alto de delante
	maxCruiseY   = 300.0
	lookAhead    = 112.0 // cuánto terreno de delante se mira para la altura
	refireTicks  = 25    // tras usar un cohete, sin otro hasta verlo tirar (o pasado esto: el servidor de los logs tarda ~12)
	safeSink     = 0.35  // al tocar suelo, caer más despacio que esto (0.5 es lo que duele)
	safeSpeed    = 0.35  // y de lado más despacio que esto (chocar a 0.3+ hace daño)
	maxYawStep   = 15.0  // grados por tick que gira (una persona con el ratón)
	maxPitchStep = 8.0
)

// Pilot vuela hasta Goal. World son los bloques que se conocen (los chunks
// cargados); lo que no está cargado no se ve.
type Pilot struct {
	World physics.Blocks
	Goal  [2]float64 // x, z
	// GoalY es la altura de la meta (NaN: no se sabe): se aterriza a esa
	// altura si se puede (lo alto de una torre), si no lo más cerca.
	GoalY float64
	// Logf, si está, cuenta lo que pasa (despegue, aterrizaje, sustos).
	Logf func(format string, args ...any)

	phase     phase
	why       string
	tries     int // intentos de despegue
	takeAt    int // tick en que empezó el intento de despegue
	lastFire  int
	cruiseY   float64
	cruiseAt  int
	landGoal  [2]float64
	landY     float64 // el suelo de la meta de aterrizaje
	landSet   bool
	grounded  int  // ticks seguidos en el suelo (aterrizando)
	climbOut  bool // recién despegado: subiendo en vertical hasta estar por encima de lo de alrededor
	tookOff   bool // ya dijo "despegué" (los reintentos del despegue no se repiten en el log)
	aboveSite bool // aterrizando: ya estuvo por encima de la pista
	outbound  bool // aterrizando: alejándose para volver de frente (estaba demasiado alto y cerca)
	pad       bool // la pista es una plataforma (lo alto de una torre): se entra de frente
	slow      bool // aterrizando: ya despacio (morro arriba), cerca de la pista
	started   int
	lastAlarm int
}

// Retarget cambia la meta en vuelo (un jugador que se mueve). Aterrizando,
// si se fue lejos de la pista elegida (o su altura cambió de capa), se
// vuelve al vuelo y se elige otra al llegar.
func (pl *Pilot) Retarget(x, y, z float64) {
	pl.Goal, pl.GoalY = [2]float64{x, z}, y
	if pl.phase != land || !pl.landSet {
		return
	}
	moved := math.Hypot(x-pl.landGoal[0], z-pl.landGoal[1]) > 16
	layer := !math.IsNaN(y) && math.Abs(y-pl.landY) > 4
	if moved || layer {
		pl.phase, pl.slow, pl.landSet = cruise, false, false
		pl.logf("élitros: se movió (a %.0f %.0f); voy tras él", x, z)
	}
}

// Status: "despegando", "en vuelo", "aterrizando", "aterrizado" o "fallo: …".
func (pl *Pilot) Status() string {
	if pl.phase == failed {
		return "fallo: " + pl.why
	}
	return pl.phase.String()
}

// Done: aterrizó (true, "") o falló (true, por qué).
func (pl *Pilot) Done() (bool, string) {
	switch pl.phase {
	case done:
		return true, ""
	case failed:
		return true, pl.why
	}
	return false, ""
}

func (pl *Pilot) logf(f string, a ...any) {
	if pl.Logf != nil {
		pl.Logf(f, a...)
	}
}

func (pl *Pilot) fail(why string) {
	pl.phase, pl.why = failed, why
	pl.logf("élitros: %s", why)
}

// Step decide el tick.
func (pl *Pilot) Step(o Obs) Cmd {
	if pl.started == 0 {
		pl.started = o.Tick
		pl.lastFire = -1000
		pl.lastAlarm = -1000
	}
	c := Cmd{Yaw: o.Yaw, Pitch: o.Pitch}
	switch pl.phase {
	case takeoff:
		pl.takeoff(o, &c)
	case cruise:
		pl.cruise(o, &c)
	case land:
		pl.land(o, &c)
	}
	// planeando sin cohete y despacio, el morro muy arriba es desplomarse
	// (el descenso lento del aterrizaje lo controla él mismo)
	// (solo cerca del suelo: arriba, esperar un cohete con el morro alto es
	// lo que se quiere; ponerlo horizontal hacía que el cohete, al llegar, lo
	// lanzara de lado contra la torre de al lado)
	if o.FallFlying && !o.Boosting && !(pl.phase == land && pl.slow) && math.Sqrt(o.Vel.LengthSqr()) < 0.5 && c.Pitch < -20 {
		if gy, ok := pl.groundAt(o.Pos.X, o.Pos.Z, o.Pos.Y); ok && o.Pos.Y-gy < 6 {
			c.Pitch = 0
		}
	}
	c.Yaw = approach(o.Yaw, c.Yaw, maxYawStep)
	c.Pitch = float32(math.Max(-90, math.Min(90, float64(approach(o.Pitch, c.Pitch, maxPitchStep)))))
	if c.Fire && (o.Rockets <= 0 || o.Boosting || o.Tick-pl.lastFire < refireTicks || !o.FallFlying) {
		c.Fire = false
	}
	if c.Fire {
		pl.lastFire = o.Tick
	}
	return c
}

// takeoff: saltar, soltar, y en el aire pulsar otra vez (empieza el planeo);
// planeando, mirando arriba y un cohete.
func (pl *Pilot) takeoff(o Obs, c *Cmd) {
	if o.FallFlying {
		pl.phase, pl.climbOut = cruise, true
		if !pl.tookOff {
			pl.tookOff = true
			pl.logf("élitros: despegué en %.0f %.0f %.0f", o.Pos.X, o.Pos.Y, o.Pos.Z)
		}
		c.Pitch = 0
		c.Fire = true // (si ya hay uno en camino, el guardián de refireTicks lo calla)
		return
	}
	if o.InWater {
		pl.fail("en el agua no se despega")
		return
	}
	if pl.takeAt == 0 || o.Tick-pl.takeAt > 30 {
		if pl.tries++; pl.tries > 6 {
			pl.fail("no consigo despegar")
			return
		}
		pl.takeAt = o.Tick
		if !pl.headroom(o.Pos) {
			pl.fail("no hay sitio encima para despegar")
			return
		}
	}
	c.Yaw = yawTo(o.Pos, pl.Goal[0], pl.Goal[1])
	c.Pitch = 0 // al abrir los élitros sin cohete aún: horizontal (es lo que más aguanta en el aire)
	c.Yaw = pl.openYaw(o.Pos)
	k := o.Tick - pl.takeAt
	switch {
	case o.OnGround && math.Abs(float64(o.Pitch)) > 5:
		c.Jump = false // primero levantar la vista
	case o.OnGround:
		c.Jump = k%4 == 0 // saltar (y soltar, para volver a pulsar en el aire)
	case o.Vel.Y < 0.1:
		c.Jump = k%2 == 0 // en lo alto del salto: pulsar (el planeo empieza al pulsar, no al mantener)
	}
}

// headroom: hay aire encima para saltar y empezar a subir.
func (pl *Pilot) headroom(p physics.Vec3) bool {
	for dy := 2; dy <= 6; dy++ {
		if pl.solid(world.BlockPos{X: fl(p.X), Y: fl(p.Y) + dy, Z: fl(p.Z)}) {
			return false
		}
	}
	return true
}

// cruise: a la altura de crucero, hacia la meta.
func (pl *Pilot) cruise(o Obs, c *Cmd) {
	if !pl.airborne(o, c) {
		return
	}
	d := math.Hypot(pl.Goal[0]-o.Pos.X, pl.Goal[1]-o.Pos.Z)
	if o.Tick-pl.cruiseAt >= 10 || pl.cruiseY == 0 {
		pl.cruiseY = pl.cruiseHeight(o)
		pl.cruiseAt = o.Tick
	}
	// la meta más alta que el terreno de delante (lo alto de una torre): el
	// crucero, por encima de ella
	if !math.IsNaN(pl.GoalY) && d < 160 {
		pl.cruiseY = math.Max(pl.cruiseY, math.Min(pl.GoalY+15, maxCruiseY))
	}
	// cerca: a aterrizar (cuando la altura sobre la meta deja bajar
	// planeando). Nunca recién despegado ni por debajo de la meta: se baja a
	// la pista, no se sube a ella (en los logs, con él 124 bloques encima y a
	// 22 de lado, "aterrizaba" nada más despegar)
	from := o.Pos.Y + 64
	if !math.IsNaN(pl.GoalY) {
		from = math.Max(from, pl.GoalY+12)
	}
	if gy, ok := pl.groundAt(pl.Goal[0], pl.Goal[1], math.Min(from, 319)); (ok || d < 48) && !pl.climbOut {
		if !ok {
			gy, _ = pl.groundAt(o.Pos.X, o.Pos.Z, o.Pos.Y)
		}
		if o.Pos.Y > gy+6 && d < (o.Pos.Y-gy)*1.6+24 {
			pl.startLanding(o)
			pl.land(o, c)
			return
		}
	}
	// recién despegado: en vertical (mirando arriba, el cohete tira hacia
	// arriba) hasta estar por encima de lo de alrededor; ir hacia la meta
	// desde una ladera era chocar con ella
	if pl.climbOut {
		top := pl.highestAround(o.Pos, 12)
		if o.Pos.Y < top+8 {
			c.Yaw = pl.clearYaw(o.Pos, yawTo(o.Pos, pl.Goal[0], pl.Goal[1])+180) // (levantando el morro, el cohete tira hacia donde mira)
			c.Pitch = -90
			c.Fire = true
			if !o.Boosting {
				// el cohete aún no llega (el servidor tarda: en los logs ~12 ticks):
				// mirando arriba del todo se cae a plomo y toca suelo antes; con
				// el morro horizontal se planea (cae ~0.9 bloques en 12 ticks:
				// menos que lo alto del salto), hacia el lado más despejado
				c.Pitch = 0
				c.Yaw = pl.openYaw(o.Pos)
			}
			return
		}
		pl.climbOut = false
	}
	// sin cohetes: lo que quede, planeando, y a aterrizar donde llegue
	if o.Rockets == 0 && !o.Boosting && o.Tick-pl.lastFire > refireTicks {
		pl.logf("élitros: sin cohetes; planeo hacia la meta y aterrizo donde llegue")
		pl.startLanding(o)
		pl.land(o, c)
		return
	}
	pl.flyLevel(o, c, pl.Goal[0], pl.Goal[1])
}

// flyLevel: hacia (x, z) a la altura de crucero, con cohetes cuando falta
// velocidad o altura, esquivando lo de delante.
func (pl *Pilot) flyLevel(o Obs, c *Cmd, x, z float64) {
	c.Yaw = yawTo(o.Pos, x, z)
	speed := math.Sqrt(o.Vel.LengthSqr())
	climb := pl.cruiseY - o.Pos.Y
	// mucho que subir para lo cerca que está (él en lo alto de una torre, al
	// lado): primero en vertical, luego de lado (subir en diagonal hacia la
	// torre era chocar con ella)
	// (y subiendo mucho, también: en diagonal, una ladera más empinada que la
	// subida es chocar con ella)
	if d := math.Hypot(x-o.Pos.X, z-o.Pos.Z); climb > 10 && d < climb*1.5+24 || climb > 12 {
		c.Pitch, c.Fire = -90, true
		c.Yaw = pl.clearYaw(o.Pos, yawTo(o.Pos, x, z)+180) // (mientras levanta el morro, el cohete tira hacia donde mira: no hacia la torre)
		if !o.Boosting {
			// esperando el cohete (mirando arriba sin él, se cae): pegado al
			// suelo, horizontal; arriba, morro a -50 (avanza poco)
			c.Pitch = -50
			if gy, ok := pl.groundAt(o.Pos.X, o.Pos.Z, o.Pos.Y); !ok || o.Pos.Y-gy < 4 {
				c.Pitch, c.Yaw = 0, pl.openYaw(o.Pos)
			}
		}
		return
	}
	if o.Boosting {
		c.Pitch = clampf(-climb*2, -45, 10)
	} else {
		c.Pitch = clampf(-climb*1.5, -25, 15)
		if climb > 6 && speed < 1.4 || speed < 0.9 {
			c.Fire = true
			if climb > 6 {
				c.Pitch = clampf(-climb*2, -45, 0)
			}
		}
	}
	pl.avoid(o, c, 40, false)
}

// airborne: sigue planeando; si se cortó el planeo en el aire (el servidor
// lo paró), volver a pulsar saltar. false: el tick ya está decidido.
func (pl *Pilot) airborne(o Obs, c *Cmd) bool {
	if o.InWater {
		pl.phase = done
		pl.logf("élitros: caí al agua en %.0f %.0f %.0f; sigo andando", o.Pos.X, o.Pos.Y, o.Pos.Z)
		return false
	}
	if o.OnGround && !o.FallFlying {
		if pl.phase == land {
			return true // tocar suelo aterrizando es aterrizar (land lo cuenta)
		}
		if o.Rockets == 0 {
			pl.phase = done
			pl.logf("élitros: toqué suelo en %.0f %.0f %.0f sin cohetes; sigo andando", o.Pos.X, o.Pos.Y, o.Pos.Z)
			return false
		}
		pl.phase, pl.takeAt = takeoff, 0
		pl.logf("élitros: toqué suelo en %.0f %.0f %.0f antes de tiempo; despego otra vez", o.Pos.X, o.Pos.Y, o.Pos.Z)
		return false
	}
	if !o.FallFlying {
		if gy, ok := pl.groundAt(o.Pos.X, o.Pos.Z, o.Pos.Y+0.5); ok && o.Pos.Y-gy < 2 {
			// pegado al suelo: es el aterrizaje (un tick sin "en el suelo" al
			// pararse); abrir los élitros otra vez lo hacía rebotar sin fin
			if pl.phase == land {
				return true
			}
			if o.Rockets > 0 {
				pl.phase, pl.takeAt = takeoff, 0
				return false
			}
			pl.phase = done
			return false
		}
		c.Jump = o.Tick%2 == 0 // cayendo sin planear: pulsar saltar lo vuelve a abrir
		return false
	}
	return true
}

// clearYaw es prefer si por ahí (a 8 bloques) no hay nada a su altura; si
// no, el lado más despejado.
func (pl *Pilot) clearYaw(p physics.Vec3, prefer float32) float32 {
	a := float64(prefer) * math.Pi / 180
	for d := 1.0; d <= 8; d++ {
		x, z := p.X-d*math.Sin(a), p.Z+d*math.Cos(a)
		if gy, ok := pl.groundAt(x, z, p.Y+20); ok && gy > p.Y-2 {
			return pl.openYaw(p)
		}
	}
	return prefer
}

// openYaw es hacia dónde el terreno de alrededor (a 8 bloques) es más bajo:
// por donde planear despacio sin chocar.
func (pl *Pilot) openYaw(p physics.Vec3) float32 {
	best, bestTop := float32(0), math.Inf(1)
	for i := 0; i < 8; i++ {
		a := float64(i) * math.Pi / 4
		top := math.Inf(-1)
		for d := 1.0; d <= 8; d++ {
			x, z := p.X+d*math.Sin(-a), p.Z+d*math.Cos(a)
			if gy, ok := pl.groundAt(x, z, p.Y+20); ok && gy > top {
				top = gy
			}
		}
		if top < bestTop {
			best, bestTop = float32(a*180/math.Pi), top
		}
	}
	return best
}

// highestAround: el suelo más alto a r bloques alrededor de p.
func (pl *Pilot) highestAround(p physics.Vec3, r float64) float64 {
	top := math.Inf(-1)
	for dx := -r; dx <= r; dx += 3 {
		for dz := -r; dz <= r; dz += 3 {
			if gy, ok := pl.groundAt(p.X+dx, p.Z+dz, p.Y+40); ok && gy > top {
				top = gy
			}
		}
	}
	return top
}

// clearance es lo que le queda hasta el suelo, debajo y en los próximos
// ticks siguiendo así (una ladera que sube delante cuenta).
func (pl *Pilot) clearance(o Obs, ticks int) float64 {
	h := math.Inf(1)
	for k := 0; k <= ticks; k += 2 {
		q := o.Pos.Add(o.Vel.Scale(float64(k)))
		if gy, ok := pl.groundAt(q.X, q.Z, math.Max(q.Y, o.Pos.Y)+4); ok {
			h = math.Min(h, q.Y-gy)
		}
	}
	return h
}

// cruiseHeight: el terreno más alto de delante (y alrededor) más un margen.
func (pl *Pilot) cruiseHeight(o Obs) float64 {
	goal := pl.Goal
	if pl.phase == land {
		goal = pl.landGoal
	}
	dx, dz := goal[0]-o.Pos.X, goal[1]-o.Pos.Z
	d := math.Hypot(dx, dz)
	if d < 1e-6 {
		return o.Pos.Y
	}
	ux, uz := dx/d, dz/d
	top := math.Inf(-1)
	from := math.Min(o.Pos.Y+80, 319)
	for t := 0.0; t <= math.Min(lookAhead, d+16); t += 4 {
		for _, side := range []float64{-6, 0, 6} {
			x, z := o.Pos.X+ux*t-uz*side, o.Pos.Z+uz*t+ux*side
			if gy, ok := pl.groundAt(x, z, from); ok && gy > top {
				top = gy
			}
		}
	}
	if math.IsInf(top, -1) { // nada cargado delante: la altura de ahora
		return math.Min(o.Pos.Y, maxCruiseY)
	}
	return math.Min(top+cruiseMargin, maxCruiseY)
}

// avoid: si en la trayectoria de los próximos horizon ticks hay un bloque,
// subir y, muy cerca, girar a un lado. En crucero con un cohete; bajando
// (soft) con la velocidad que lleva, y cohete solo si ya no le queda (si
// no, cada colina de alrededor de la meta lo devolvía arriba: vueltas sin fin).
func (pl *Pilot) avoid(o Obs, c *Cmd, horizon int, soft bool) {
	k, hit := pl.collision(o.Pos, o.Vel, horizon, soft)
	if !hit {
		return
	}
	if o.Tick-pl.lastAlarm > 40 {
		pl.lastAlarm = o.Tick
		pl.logf("élitros: obstáculo delante (a %d ticks): subo", k)
	}
	if soft {
		// despacio (bajando con el morro arriba): un choque apenas hace daño, y
		// subir con un cohete era no bajar nunca; girar hacia otro lado
		c.Yaw = o.Yaw + 60
		return
	}
	c.Fire = true
	if !o.Boosting && math.Sqrt(o.Vel.LengthSqr()) < 0.8 {
		// sin cohete tirando y despacio, levantar el morro es desplomarse (en
		// las pruebas: cayendo a 0.8 por tick contra el suelo); girar, y que
		// el cohete que viene suba
		c.Yaw = o.Yaw + 60
		c.Pitch = 0
		return
	}
	c.Pitch = -50
	if k < 15 && o.Boosting {
		// cerca y con el cohete tirando: arriba del todo (el cohete frena lo de
		// lado a la mitad cada tick y sube en vertical; girar a medias contra
		// una ladera empinada era chocar con ella)
		c.Pitch = -90
		return
	}
	if k < 10 { // demasiado cerca para pasar por encima: girar también
		c.Yaw = o.Yaw + 60
	}
}

// collision: el primer tick (hasta n) en que, siguiendo en línea recta con
// esta velocidad, el cuerpo (0.6 planeando) toca un bloque. Con wallsOnly
// (aterrizando) solo cuenta lo que está por encima de los pies: tocar el
// suelo bajando es aterrizar, no chocar (si no, rozaba el suelo dando
// vueltas, "esquivándolo").
func (pl *Pilot) collision(p, v physics.Vec3, n int, wallsOnly bool) (int, bool) {
	ys := []float64{0, 0.6}
	if wallsOnly {
		ys = []float64{0.55}
	}
	for k := 1; k <= n; k++ {
		q := p.Add(v.Scale(float64(k)))
		if wallsOnly && pl.solid(world.BlockPos{X: fl(q.X), Y: fl(q.Y + 0.55), Z: fl(q.Z)}) == false &&
			pl.solid(world.BlockPos{X: fl(q.X), Y: fl(q.Y), Z: fl(q.Z)}) {
			return 0, false // los pies tocan suelo antes que el cuerpo una pared: es aterrizar
		}
		for _, dy := range ys {
			for _, dx := range []float64{-0.3, 0.3} {
				for _, dz := range []float64{-0.3, 0.3} {
					if pl.solid(world.BlockPos{X: fl(q.X + dx), Y: fl(q.Y + dy), Z: fl(q.Z + dz)}) {
						return k, true
					}
				}
			}
		}
	}
	return 0, false
}

// Aterrizaje: como con paracaídas. Con el morro arriba (entre -25° y -50°)
// el élitro va despacio, ~0.35 bloques por tick de lado, y baja entre 0.09 y
// 0.3 por tick según lo levantado (medido con la física: glide_probe_test):
// tocar suelo así no hace daño ni de caída (duele a 0.5) ni contra una ladera.
// Planeando normal, en cambio, baja siempre unos 6-10° y rápido: no se puede
// apuntar a un sitio. Así que: hacia la pista planeando; a unos 45 bloques,
// morro arriba (frena, subiendo un poco); y despacio hacia ella, con la
// bajada que pida el ángulo; demasiado alto, en espiral cerrada encima.
const (
	slowFrom  = 45.0 // a cuánto de la pista se frena
	mushMin   = -25  // morro arriba: baja ~14° (0.09 por tick)
	mushMax   = -50  // baja ~40° (0.3 por tick): más, y cae deprisa
	mushAngLo = 14.3
	mushAngHi = 40.6
)

// startLanding elige la pista: segura, lo más cerca posible de la meta y,
// si se sabe a qué altura está (GoalY), a esa altura: desde el aire solo se
// aterriza en lo de arriba de cada columna, así que en lo alto de una torre
// sí, y en una cueva o bajo techo no (se aterriza lo más cerca, y se anda).
// Entre las que valen, mejor llana y lejos de peligros.
func (pl *Pilot) startLanding(o Obs) {
	pl.phase = land
	pl.landSet, pl.slow, pl.aboveSite = false, false, false
	hasY := !math.IsNaN(pl.GoalY)
	from := o.Pos.Y + 40
	if hasY {
		from = math.Max(from, pl.GoalY+12)
	}
	from = math.Min(from, 319)
	best := math.Inf(1)
	for r := 0.0; r <= 32; r += 2 {
		for a := 0.0; a < 2*math.Pi; a += math.Pi / 8 {
			x, z := pl.Goal[0]+r*math.Cos(a), pl.Goal[1]+r*math.Sin(a)
			gy, ok := pl.groundAt(x, z, from)
			if !ok || !pl.safeWithin(x, z, gy, 2) {
				continue
			}
			score := r
			if !pl.flat(x, z, gy) {
				score += 8
			}
			if !pl.safeWithin(x, z, gy, 4) {
				score += 4
			}
			if dy := math.Abs(gy - pl.GoalY); hasY && dy > 1.5 {
				score += 6 * dy // otra capa: andar de una a otra puede no tener camino
			}
			if score < best {
				best = score
				pl.landGoal, pl.landY, pl.landSet = [2]float64{x, z}, gy, true
			}
			if r == 0 {
				break
			}
		}
	}
	if !pl.landSet { // nada cargado allí: la meta, a la altura de aquí abajo
		pl.landGoal = pl.Goal
		pl.landY, _ = pl.groundAt(o.Pos.X, o.Pos.Z, o.Pos.Y)
	}
	// una plataforma (lo alto de una torre: alrededor, el suelo mucho más
	// abajo): ahí no se baja en espiral, se entra de frente
	pl.pad = false
	for _, dd := range [][2]float64{{6, 0}, {-6, 0}, {0, 6}, {0, -6}} {
		// (solo yendo a alguien cuya altura se sabe: si no, aterrizar en la
		// colina de al lado vale igual)
		if g, ok := pl.groundAt(pl.landGoal[0]+dd[0], pl.landGoal[1]+dd[1], pl.landY+2); ok && g < pl.landY-4 && hasY {
			pl.pad = true
		}
	}
	pl.logf("élitros: aterrizando en %.0f %.0f (y=%.0f)", pl.landGoal[0], pl.landGoal[1], pl.landY)
	if hasY && math.Abs(pl.landY-pl.GoalY) > 3 {
		pl.logf("élitros: la meta está en y=%.0f y desde el aire no se llega a esa altura (bajo techo o en una cueva): aterrizo lo más cerca y sigo andando", pl.GoalY)
	}
}

// land: hacia la pista; cerca, despacio (morro arriba) hasta tocarla.
func (pl *Pilot) land(o Obs, c *Cmd) {
	if !pl.airborne(o, c) {
		return
	}
	if !o.FallFlying {
		if pl.grounded++; pl.grounded >= 3 {
			// en el suelo: si es la pista (o cerca, a su altura), aterrizado; si
			// no (rozó el suelo antes de llegar, esperando un cohete), otra vez
			// (en terreno normal, unos bloques al lado vale: lo que quede, se anda;
			// en otra capa que la suya, o lejos, no)
			dist := math.Hypot(pl.landGoal[0]-o.Pos.X, pl.landGoal[1]-o.Pos.Z)
			otherLayer := math.Abs(o.Pos.Y-pl.landY) > 3 && (pl.pad || !math.IsNaN(pl.GoalY))
			far := dist > 40 || dist > 12 && otherLayer
			if far && o.Rockets > 0 && pl.tries <= 3 {
				pl.logf("élitros: toqué suelo en %.0f %.0f %.0f, lejos de la pista (y=%.0f); despego otra vez", o.Pos.X, o.Pos.Y, o.Pos.Z, pl.landY)
				pl.phase, pl.takeAt, pl.grounded = takeoff, 0, 0
				return
			}
			pl.phase = done
			pl.logf("élitros: aterricé en %.0f %.0f %.0f (a %.0f bloques de la meta)", o.Pos.X, o.Pos.Y, o.Pos.Z,
				math.Hypot(pl.Goal[0]-o.Pos.X, pl.Goal[1]-o.Pos.Z))
		}
		return
	}
	pl.grounded = 0
	// por debajo de la pista (está en lo alto de algo) sin haber estado aún
	// por encima: subir antes, en vertical (cerca de una torre, ir de lado es
	// chocar con ella). (Bajando a ella desde arriba, estar cerca es aterrizar.)
	if o.Pos.Y > pl.landY+12 {
		pl.aboveSite = true
	}
	if pl.pad && pl.aboveSite && o.Pos.Y < pl.landY-1.5 && math.Hypot(pl.landGoal[0]-o.Pos.X, pl.landGoal[1]-o.Pos.Z) < 24 {
		// se pasó de la pista (en lo alto de algo) y quedó por debajo: subir
		// otra vez y volver a entrar
		pl.aboveSite = false
		pl.logf("élitros: me pasé de la pista (y=%.0f); subo y vuelvo a entrar", pl.landY)
	}
	if !pl.aboveSite && o.Pos.Y < pl.landY+8 {
		pl.slow = false
		c.Pitch, c.Fire = -90, true
		c.Yaw = pl.clearYaw(o.Pos, yawTo(o.Pos, pl.landGoal[0], pl.landGoal[1])+180)
		if !o.Boosting {
			// esperando el cohete: pegado al suelo, horizontal (aguanta en el
			// aire); arriba, morro a -50 (avanza poco: el cohete, al llegar, no
			// lo lanza de lado contra lo que tenga cerca)
			c.Pitch = -50
			if gy, ok := pl.groundAt(o.Pos.X, o.Pos.Z, o.Pos.Y); !ok || o.Pos.Y-gy < 4 {
				c.Pitch, c.Yaw = 0, pl.openYaw(o.Pos)
			}
		}
		return
	}
	gx, gz := pl.landGoal[0], pl.landGoal[1]
	d := math.Hypot(gx-o.Pos.X, gz-o.Pos.Z)
	h := o.Pos.Y - pl.landY
	below, okBelow := pl.groundAt(o.Pos.X, o.Pos.Z, o.Pos.Y)
	hb := o.Pos.Y - below
	if !okBelow {
		hb = h
	}
	bearing := yawTo(o.Pos, gx, gz)
	if !pl.slow {
		// lejos: a la altura de crucero hasta la pista (bajar antes, entre
		// colinas, era chocar con ellas); allí, frenar y bajar en espiral
		if o.Tick-pl.cruiseAt >= 10 {
			pl.cruiseY = pl.cruiseHeight(o)
			pl.cruiseAt = o.Tick
		}
		if d < slowFrom {
			pl.slow = true
		} else {
			if o.Rockets == 0 { // sin cohetes no se mantiene la altura: planeando hacia allí
				c.Yaw, c.Pitch = bearing, 5
				pl.avoid(o, c, 30, false)
				if d < slowFrom*2 || hb < 12 {
					pl.slow = true
				}
				return
			}
			pl.flyLevel(o, c, gx, gz)
			return
		}
	}
	// despacio: la bajada que pide el ángulo hasta la pista
	if ahead := pl.clearance(o, 10); ahead < hb {
		hb = ahead // una ladera que sube delante cuenta como suelo (para frenar a tiempo)
	}
	if pl.pad {
		h -= 1.5 // en una plataforma, apuntar por encima del borde (rozarlo es chocar)
	}
	ang := math.Atan2(h, math.Max(d, 0.5)) * 180 / math.Pi
	c.Yaw = bearing
	t := (ang - mushAngLo) / (mushAngHi - mushAngLo)
	c.Pitch = clampf(mushMin+t*(mushMax-mushMin), mushMax, mushMin)
	// demasiado alto para lo cerca: alejarse (bajando lo que se pueda) y
	// volver de frente, con una bajada que lleve a la pista. (Una espiral
	// encima tiene ~3.5 bloques de radio: en lo alto de una torre de 5x5
	// acababa bajando por el costado.)
	if pl.pad && ang > mushAngHi+2 && d < 40 {
		pl.outbound = true
	} else if ang < 30 || d > 40 {
		pl.outbound = false
	}
	switch {
	case pl.outbound:
		c.Yaw = bearing + 180
		c.Pitch = mushMax
	case ang > mushAngHi+2 && hb > 15 && d > 3:
		// en terreno normal, demasiado alto para lo cerca: en espiral encima
		// (más abajo, directo: la espiral se abre y pasa por encima de lo de alrededor)
		c.Yaw = bearing + 60
		c.Pitch = mushMax
	case ang > mushAngHi+2:
		c.Pitch = mushMax
	case ang < 10 && d > 8:
		// demasiado bajo para lo lejos: más deprisa (y un cohete si el suelo está cerca)
		c.Pitch = 0
		if hb < 6 && !o.Boosting {
			c.Fire, c.Pitch = true, -20
		}
	}
	// en una plataforma: si alguna forma de seguir (morro a tal ángulo, o
	// dejarse caer) acaba, según la física, dentro de ella y sin daño, esa
	if pl.pad && d < 16 {
		if pitch, ok := pl.padTouchdown(o); ok {
			c.Pitch = pitch
			c.Yaw = o.Yaw // (girar ahora cambiaría a dónde cae)
			return
		}
		// va a dar contra el costado (llega bajo): arriba en vertical, y otra vez
		if k, hit := pl.collision(o.Pos, o.Vel, 12, true); hit && k > 0 {
			c.Pitch, c.Fire = -90, true
			c.Yaw = bearing + 180
			pl.aboveSite = false
			return
		}
	}
	// la bajada nunca cerca de lo que duele (0.5): girando cerrado baja más
	if o.Vel.Y < -0.38 {
		c.Pitch = -30
	}
	switch {
	case hb < 1.5 && o.Vel.Y > -0.3:
		// el último bloque: morro muy arriba un momento, frena de lado
		// (tocar una ladera a más de 0.3 de lado hace daño); solo mientras
		// no caiga deprisa (frenar de más es caer: a 0.5 duele)
		c.Pitch = -60
	case hb < 3 || o.Vel.Y <= -0.3:
		c.Pitch = -30 // a ras: lo más suave
	}
	// donde va a tocar hay peligro (lava, fuego): hacia la pista bajando lo
	// menos posible; a punto de tocarlo, un cohete hacia ella
	if _, _, _, bad := pl.touchdown(o); bad && hb < 6 {
		c.Yaw, c.Pitch = bearing, -20
		if hb < 2.5 {
			c.Pitch, c.Fire = -40, true
		}
		if o.Tick-pl.lastAlarm > 40 {
			pl.lastAlarm = o.Tick
			pl.logf("élitros: abajo hay peligro; sigo hacia la pista")
		}
	}
	pl.avoid(o, c, 20, true)
}

// padTouchdown prueba, con la física del cliente, a seguir con el morro a
// cada ángulo (o mirando abajo del todo sin cohete: se cae recto, sin
// sustentación ni giro) hasta tocar algo: la que acaba en la plataforma (a
// su altura, cerca del centro), sin daño de caída ni de choque.
func (pl *Pilot) padTouchdown(o Obs) (float32, bool) {
	best, bestD := float32(0), math.Inf(1)
	for _, pitch := range []float32{90, -25, -30, -40, -50, -60} {
		p := physics.NewPlayer(o.Pos)
		p.Vel, p.Yaw, p.Pitch = o.Vel, o.Yaw, pitch
		p.FallFlying, p.CanGlide = true, true
		ok := false
		for k := 0; k < 80; k++ {
			hb := math.Hypot(p.Vel.X, p.Vel.Z)
			fall := p.FallDistance
			gliding := p.FallFlying
			p.Tick(pl.World, nil, physics.Keys{}, physics.DefaultAttributes)
			if gliding && p.HorizontalCollision && (hb-math.Hypot(p.Vel.X, p.Vel.Z))*10-3 > 0 {
				break // choca
			}
			if p.OnGround {
				ok = fall+math.Max(0, -p.Vel.Y) <= 3 && math.Abs(p.Pos.Y-pl.landY) < 0.6
				break
			}
		}
		if !ok {
			continue
		}
		if d := math.Hypot(p.Pos.X-pl.landGoal[0], p.Pos.Z-pl.landGoal[1]); d < bestD {
			best, bestD = pitch, d
		}
	}
	return best, bestD < 1.5
}

// touchdown: si sigue así, cerca del suelo (a menos de 8), pasa o toca
// suelo peligroso (con margen): dónde (ok true).
func (pl *Pilot) touchdown(o Obs) (x, z, gy float64, ok bool) {
	gy, ok = pl.groundAt(o.Pos.X, o.Pos.Z, o.Pos.Y)
	if !ok || o.Pos.Y-gy > 8 {
		return 0, 0, 0, false
	}
	t := 40.0
	if o.Vel.Y < -0.01 {
		t = math.Min(t, (o.Pos.Y-gy)/-o.Vel.Y)
	}
	for k := 0.0; k <= t+4; k += 2 { // (+4: lo que resbala al tocar)
		x, z = o.Pos.X+o.Vel.X*k, o.Pos.Z+o.Vel.Z*k
		if g, ok := pl.groundAt(x, z, o.Pos.Y); ok && !pl.safeAround(x, z, g) {
			return x, z, g, true // pasa por encima de peligro a ras de suelo (con margen)
		}
	}
	return 0, 0, 0, false
}

// groundAt es la altura del suelo (la cara de arriba de lo primero que hay,
// bajando desde fromY; el agua cuenta como suelo). ok false si no está cargado.
func (pl *Pilot) groundAt(x, z, fromY float64) (float64, bool) {
	bx, bz := fl(x), fl(z)
	for y := fl(fromY); y > fl(fromY)-200 && y >= -64; y-- {
		s, ok := pl.World.BlockAt(world.BlockPos{X: bx, Y: y, Z: bz})
		if !ok {
			return 0, false
		}
		if block.IsAir(s) {
			continue
		}
		top := 0.0
		for _, b := range block.CollisionShape(s) {
			top = math.Max(top, b.MaxY)
		}
		if top > 0 {
			return float64(y) + top, true
		}
		if n := world.StateString(s); pathing.IsWaterName(n) || hasPrefix(n, "minecraft:lava") {
			return float64(y) + 1, true
		}
	}
	return 0, false
}

// safeGround: se puede aterrizar en (x, z) con el suelo a gy: sin lava,
// fuego, cactus, magma ni bayas.
func (pl *Pilot) safeGround(x, z, gy float64) bool {
	for _, dy := range []int{-1, 0} {
		s, ok := pl.World.BlockAt(world.BlockPos{X: fl(x), Y: fl(gy-0.01) + dy + 1, Z: fl(z)})
		if !ok {
			continue
		}
		n := world.StateString(s)
		for _, bad := range []string{"minecraft:lava", "minecraft:fire", "minecraft:soul_fire", "minecraft:cactus", "minecraft:magma_block", "minecraft:sweet_berry_bush", "minecraft:campfire", "minecraft:powder_snow"} {
			if hasPrefix(n, bad) {
				return false
			}
		}
	}
	s, ok := pl.World.BlockAt(world.BlockPos{X: fl(x), Y: fl(gy - 0.01), Z: fl(z)})
	if ok {
		n := world.StateString(s)
		for _, bad := range []string{"minecraft:lava", "minecraft:magma_block", "minecraft:cactus", "minecraft:campfire"} {
			if hasPrefix(n, bad) {
				return false
			}
		}
	}
	return true
}

// flat: alrededor de (x, z) el suelo está a la altura de gy (±1) hasta 4
// bloques: se aterriza resbalando, y en una ladera un escalón es una pared.
func (pl *Pilot) flat(x, z, gy float64) bool {
	for _, d := range [][2]float64{{4, 0}, {-4, 0}, {0, 4}, {0, -4}, {3, 3}, {-3, 3}, {3, -3}, {-3, -3}} {
		g, ok := pl.groundAt(x+d[0], z+d[1], gy+6)
		if !ok || math.Abs(g-gy) > 1.01 {
			return false
		}
	}
	return true
}

// safeAround: suelo seguro en (x, z) y a dos bloques alrededor (al
// aterrizar se resbala un poco).
func (pl *Pilot) safeAround(x, z, gy float64) bool { return pl.safeWithin(x, z, gy, 2) }

// safeWithin: suelo seguro en (x, z) y a r bloques alrededor.
func (pl *Pilot) safeWithin(x, z, gy, r float64) bool {
	for _, d := range [][2]float64{{0, 0}, {r, 0}, {-r, 0}, {0, r}, {0, -r}, {r * 0.7, r * 0.7}, {-r * 0.7, r * 0.7}, {r * 0.7, -r * 0.7}, {-r * 0.7, -r * 0.7}} {
		g, ok := pl.groundAt(x+d[0], z+d[1], gy+3)
		if ok && !pl.safeGround(x+d[0], z+d[1], g) {
			return false
		}
	}
	return true
}

func (pl *Pilot) solid(p world.BlockPos) bool {
	s, ok := pl.World.BlockAt(p)
	return ok && len(block.CollisionShape(s)) > 0
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

func fl(v float64) int { return int(math.Floor(v)) }

func clampf(v, lo, hi float64) float32 { return float32(math.Max(lo, math.Min(hi, v))) }

// yawTo es el yaw de mirar desde p hacia (x, z) (el del juego: 0 al sur, +z).
func yawTo(p physics.Vec3, x, z float64) float32 {
	return float32(-math.Atan2(x-p.X, z-p.Z) * 180 / math.Pi)
}

// approach gira de from hacia to a lo sumo step grados (por el lado corto).
func approach(from, to float32, step float64) float32 {
	d := math.Mod(float64(to-from), 360)
	if d > 180 {
		d -= 360
	} else if d < -180 {
		d += 360
	}
	d = math.Max(-step, math.Min(step, d))
	return from + float32(d)
}

// String describe el vuelo (para /status y los logs).
func (pl *Pilot) String() string {
	return fmt.Sprintf("%s hacia %.0f %.0f (crucero a y=%.0f)", pl.Status(), pl.Goal[0], pl.Goal[1], pl.cruiseY)
}
