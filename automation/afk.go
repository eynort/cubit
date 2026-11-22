package automation

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
)

// /afk: el bot se queda en un sitio (todo el tiempo que haga falta) sin que
// el servidor lo eche por inactivo: de vez en cuando, un gesto pequeño (mirar
// alrededor, saltar, un paso y volver, un manotazo); si le empujan, vuelve;
// de noche (o con tormenta) duerme si hay una cama cerca. Comer y pelear con
// lo que se acerque lo hace el módulo de pelea (come fuera de combate con
// hambre y se defiende de los mobs hostiles a 5 bloques).

// Variables para los tests (los tiempos son en ticks: el juego por pasos no
// tiene reloj de pared).
var (
	afkIdleTicks   = [2]int{60 * 20, 240 * 20} // cada cuánto un gesto: de 1 a 4 minutos, al azar
	afkSleepRetry  = 3 * 60 * 20               // si no pudo dormir, otra vez en 3 minutos
	afkSleepMax    = 13000                     // lo más que espera en la cama (una noche entera)
	afkLeash       = 2.0                       // más lejos que esto de su sitio: vuelve
	afkBedRange    = 16                        // a qué distancia busca cama
	afkReturnRetry = 10 * 20                   // si no consigue volver, lo reintenta cada 10 s
	afkHungerTell  = 15 * 60 * 20              // sin comida y con hambre: lo dice como mucho cada 15 minutos
)

// AfkOptions configura la tarea Afk.
type AfkOptions struct {
	// Spot: la celda de los pies donde se queda.
	Spot world.BlockPos
	// Night dice si toca dormir (de noche o con tormenta; nil: nunca duerme).
	// Solo se mira en el mundo normal: una cama en el Nether o el End explota.
	Night func() bool
	// Travel lleva al bot hasta Spot cuando está lejos (nil: andando con GoTo).
	// Se llama aparte de la tarea: esta no deja de dar señales de vida.
	Travel func(ctx context.Context, p world.BlockPos) bool
	// Seed: la semilla de los gestos al azar (0: una cualquiera).
	Seed int64
}

// Afk es la tarea: se queda en o.Spot hasta que la paren. Si la reempiezan
// (el bot murió y volvió, o se reconectó), lo dice como un colega que vuelve.
func Afk(o AfkOptions) func(j *Job) error {
	var mu sync.Mutex
	runs := 0
	return func(j *Job) error {
		mu.Lock()
		runs++
		n := runs
		mu.Unlock()
		return j.afk(o, n)
	}
}

func (j *Job) afk(o AfkOptions, run int) error {
	seed := o.Seed
	if seed == 0 {
		seed = rand.Int63()
	}
	rng := rand.New(rand.NewSource(seed))
	j.Status("quieto en %d %d %d", o.Spot.X, o.Spot.Y, o.Spot.Z)

	if err := j.afkBack(o); err != nil {
		if j.ctx.Err() != nil {
			return j.ctx.Err()
		}
		if run == 1 {
			return fmt.Errorf("no llego a %d %d %d: %v", o.Spot.X, o.Spot.Y, o.Spot.Z, err)
		}
		j.sayf("I can't get back to my spot, I'll keep trying", "no consigo volver a mi sitio, lo sigo intentando")
	} else if run == 1 {
		j.sayf("I'll hang around here", "me quedo por aquí, a lo mío")
	} else {
		j.sayf("I'm back at my spot", "ya estoy de vuelta en mi sitio")
	}

	var (
		tick       int
		nextIdle   = tick + afkBetween(rng, afkIdleTicks)
		nextSleep  int
		nextBack   int
		nextHunger int
		toldFail   bool
		bad        int // esperas seguidas que fallaron
	)
	for {
		if err := j.afkWait(20, &bad); err != nil {
			return err
		}
		tick += 20

		if j.afkAway(o) && tick >= nextBack {
			if err := j.afkBack(o); err != nil {
				if j.ctx.Err() != nil {
					return j.ctx.Err()
				}
				j.Logf("afk: no vuelvo a mi sitio: %v", err)
				nextBack = tick + afkReturnRetry
			}
		}

		if o.Night != nil && tick >= nextSleep && j.Dimension() == "minecraft:overworld" && o.Night() {
			slept, err := j.afkSleep(o, &toldFail)
			if err != nil {
				return err
			}
			if slept {
				toldFail = false
			}
			nextSleep = tick + afkSleepRetry
			nextIdle = tick + afkBetween(rng, afkIdleTicks)
		} else if o.Night != nil && !o.Night() {
			toldFail = false
		}

		if tick >= nextIdle {
			if err := j.afkFidget(rng, o); err != nil {
				return err
			}
			nextIdle = tick + afkBetween(rng, afkIdleTicks)
		}

		if st := j.r.d.Player.Status(); st.Food <= 6 && st.Health > 0 && tick >= nextHunger && (j.r.d.HasFood == nil || !j.r.d.HasFood()) {
			j.sayf("I'm starving and I have nothing to eat, got any food?", "tengo hambre y no me queda nada de comer, ¿me pasáis algo?")
			nextHunger = tick + afkHungerTell
		}
	}
}

func afkBetween(rng *rand.Rand, r [2]int) int {
	if r[1] <= r[0] {
		return r[0]
	}
	return r[0] + rng.Intn(r[1]-r[0])
}

// afkWait espera n ticks quieta a propósito (el vigilante no la toma por un
// atasco). Si Wait se rinde porque está herida y sin comida, aquí no se para:
// se espera a curarse sola.
func (j *Job) afkWait(n int, bad *int) error {
	j.r.resting.Store(true)
	defer j.r.resting.Store(false)
	err := j.Wait(n)
	if err == nil {
		*bad = 0
		return nil
	}
	if j.ctx.Err() != nil {
		return err
	}
	*bad++
	if *bad > 30 {
		return err
	}
	j.Logf("afk: %v", err)
	j.fatal = nil
	return j.r.d.Ctl.WaitTicks(j.ctx, 100)
}

// afkAway: está más lejos de su sitio de lo que se permite.
func (j *Job) afkAway(o AfkOptions) bool {
	p := j.Pos()
	return math.Hypot(p.X-(float64(o.Spot.X)+0.5), p.Z-(float64(o.Spot.Z)+0.5)) > afkLeash || math.Abs(p.Y-float64(o.Spot.Y)) > 2.5
}

// afkBack vuelve a su sitio (con Travel si está lejos).
func (j *Job) afkBack(o AfkOptions) error {
	if j.Feet() == o.Spot {
		return nil
	}
	p := j.Pos()
	if d := math.Hypot(p.X-float64(o.Spot.X), p.Z-float64(o.Spot.Z)); d > 12 && o.Travel != nil {
		if !j.afkTravel(o) {
			if err := j.ctx.Err(); err != nil {
				return err
			}
			return fmt.Errorf("no encuentro el camino")
		}
	}
	return j.GoTo(o.Spot)
}

// afkTravel: Travel en otra goroutine, mientras la tarea sigue dando señales de vida.
func (j *Job) afkTravel(o AfkOptions) bool {
	ctx, cancel := context.WithCancel(j.ctx)
	defer cancel()
	done := make(chan bool, 1)
	go func() { done <- o.Travel(ctx, o.Spot) }()
	for {
		select {
		case ok := <-done:
			return ok
		default:
		}
		if err := j.Wait(10); err != nil {
			cancel()
			<-done
			return false
		}
	}
}

// afkFidget: un gesto pequeño, para que el servidor no lo eche por inactivo.
func (j *Job) afkFidget(rng *rand.Rand, o AfkOptions) error {
	ctl := j.r.d.Ctl
	switch rng.Intn(4) {
	case 0: // mirar alrededor
		for i := 0; i < 2+rng.Intn(3); i++ {
			p := j.Pos()
			ctl.Look(p.Yaw+float32(rng.Intn(120)-60), float32(rng.Intn(50)-20))
			if err := j.Wait(6 + rng.Intn(14)); err != nil {
				return err
			}
		}
	case 1: // saltar
		ctl.SetInput(control.Input{Jump: true})
		err := j.Wait(2)
		ctl.SetInput(control.Input{})
		if err != nil {
			return err
		}
		return j.Wait(12)
	case 2: // un paso a un lado y volver
		in, ok := j.afkStepKey(rng)
		if !ok {
			return j.Wait(10)
		}
		ctl.SetInput(in)
		err := j.Wait(4)
		ctl.SetInput(control.Input{})
		if err != nil {
			return err
		}
		if err := j.Wait(10); err != nil {
			return err
		}
		if j.Feet() != o.Spot {
			if err := j.GoTo(o.Spot); err != nil && j.ctx.Err() != nil {
				return err
			}
		}
	default: // un manotazo
		for i := 0; i < 1+rng.Intn(2); i++ {
			j.afkSwing()
			if err := j.Wait(6); err != nil {
				return err
			}
		}
	}
	return nil
}

// afkStepKey: una tecla de dirección para dar un paso, hacia donde haya suelo
// firme y sitio libre (nunca al borde de un precipicio, al agua o a la lava).
func (j *Job) afkStepKey(rng *rand.Rand) (control.Input, bool) {
	yaw := float64(j.Pos().Yaw) * math.Pi / 180
	// hacia dónde mira: -sin(yaw), +cos(yaw) (el yaw de Minecraft)
	fx, fz := -math.Sin(yaw), math.Cos(yaw)
	type dir struct {
		in     control.Input
		dx, dz float64
	}
	dirs := []dir{
		{control.Input{Forward: true}, fx, fz},
		{control.Input{Backward: true}, -fx, -fz},
		{control.Input{Left: true}, fz, -fx},
		{control.Input{Right: true}, -fz, fx},
	}
	rng.Shuffle(len(dirs), func(a, b int) { dirs[a], dirs[b] = dirs[b], dirs[a] })
	p := j.Pos()
	feet := j.Feet()
	for _, d := range dirs {
		// la celda a la que llegaría (un poco más de un bloque, por si acaso)
		c := world.BlockPos{X: int(math.Floor(p.X + d.dx*1.3)), Y: feet.Y, Z: int(math.Floor(p.Z + d.dz*1.3))}
		if j.afkSafeCell(c) {
			return d.in, true
		}
	}
	return control.Input{}, false
}

var afkUnsafeGround = []string{"air", "lava", "water", "fire", "cactus", "magma", "campfire", "berry", "powder_snow", "web", "kelp", "seagrass", "bubble"}

func (j *Job) afkSafeCell(c world.BlockPos) bool {
	for _, p := range []world.BlockPos{c, {X: c.X, Y: c.Y + 1, Z: c.Z}} {
		n := j.Block(p)
		if n == "" || !(strings.HasSuffix(n, "air")) {
			return false
		}
	}
	below := j.Block(world.BlockPos{X: c.X, Y: c.Y - 1, Z: c.Z})
	if below == "" {
		return false
	}
	for _, bad := range afkUnsafeGround {
		if strings.Contains(below, bad) {
			return false
		}
	}
	return true
}

// afkSwing: manotazo al aire (ServerboundSwing, o ServerboundPunch desde la 26.3).
func (j *Job) afkSwing() {
	if j.r.d.Client == nil || j.r.d.Client.Conn == nil {
		return
	}
	var id packetid.ServerboundPacketID
	found, punch := false, false
	for i := packetid.ServerboundPacketID(0); i < 512; i++ {
		switch i.String() {
		case "ServerboundPunch":
			id, found, punch = i, true, true
		case "ServerboundSwing":
			if !punch {
				id, found = i, true
			}
		}
	}
	if !found {
		return
	}
	if punch {
		_ = j.r.d.Client.Conn.WritePacket(pk.Marshal(id))
		return
	}
	_ = j.r.d.Client.Conn.WritePacket(pk.Marshal(id, pk.VarInt(0))) // InteractionHand.MAIN_HAND
}

// afkSleep: va a la cama más cercana (a afkBedRange), la usa y espera a que
// amanezca. Dice si durmió. Si no hay cama o no se puede dormir ("solo se
// puede dormir de noche", monstruos cerca…), false: se reintenta más tarde.
func (j *Job) afkSleep(o AfkOptions, toldFail *bool) (bool, error) {
	bed, ok := j.findBed(afkBedRange)
	if !ok {
		return false, nil
	}
	j.sayf("it's getting late, I'm off to bed", "ya es de noche, me voy a dormir")
	if err := j.GoNear(bed); err != nil {
		j.Logf("afk: no llego a la cama: %v", err)
		if err := j.ctx.Err(); err != nil {
			return false, err
		}
		return false, j.afkBack(o)
	}
	done := make(chan struct{}, 1)
	j.r.d.Hands.Interact(bed, func(act.Use) { done <- struct{}{} })
	for i := 0; i < 50; i++ {
		select {
		case <-done:
			i = 50
			continue
		default:
		}
		if err := j.Wait(2); err != nil {
			return false, err
		}
	}
	var bad int
	for i := 0; i < 5; i++ { // 5 s para que el servidor lo acueste
		if err := j.afkWait(20, &bad); err != nil {
			return false, err
		}
	}
	if !j.inBed(bed) {
		j.Logf("afk: no he podido dormir en la cama de %v", bed)
		if !*toldFail {
			j.sayf("couldn't sleep, I'll try again in a bit", "no he podido dormir, luego lo vuelvo a intentar")
			*toldFail = true
		}
		return false, j.afkBack(o)
	}
	j.Logf("afk: durmiendo en %v", bed)
	for t := 0; t < afkSleepMax; t += 20 {
		if err := j.afkWait(20, &bad); err != nil {
			return false, err
		}
		if !j.inBed(bed) || (o.Night != nil && !o.Night()) {
			break
		}
	}
	j.sayf("good morning!", "buenos días")
	return true, j.afkBack(o)
}

// inBed: el servidor lo tiene acostado en esa cama (la deja en su centro).
func (j *Job) inBed(bed world.BlockPos) bool {
	p := j.Pos()
	return math.Hypot(p.X-(float64(bed.X)+0.5), p.Z-(float64(bed.Z)+0.5)) < 0.9 && math.Abs(p.Y-float64(bed.Y)) < 1.2
}

// findBed: la cama libre más cercana a r bloques (la mitad que sea).
func (j *Job) findBed(r int) (world.BlockPos, bool) {
	c := j.Feet()
	var best world.BlockPos
	bestD, found := math.MaxFloat64, false
	for x := c.X - r; x <= c.X+r; x++ {
		for z := c.Z - r; z <= c.Z+r; z++ {
			for y := c.Y - 4; y <= c.Y+4; y++ {
				p := world.BlockPos{X: x, Y: y, Z: z}
				st, ok := j.r.d.World.BlockAt(p)
				if !ok {
					continue
				}
				s := world.StateString(st)
				if !strings.Contains(s, "_bed[") || strings.Contains(s, "occupied=true") {
					continue
				}
				d := float64((x-c.X)*(x-c.X) + (y-c.Y)*(y-c.Y) + (z-c.Z)*(z-c.Z))
				if d <= float64(r*r) && d < bestD {
					best, bestD, found = p, d, true
				}
			}
		}
	}
	return best, found
}
