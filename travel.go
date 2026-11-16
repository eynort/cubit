package main

// Ir hasta un jugador esté donde esté. El servidor solo manda la posición de
// los jugadores cercanos (su distancia de seguimiento, ~48-128 bloques); más
// lejos el bot no la recibe. Para llegar igual:
//
//  1. si lo ve, va directo;
//  2. si lo vio alguna vez, va a donde lo vio por última vez (por tramos, sin
//     límite de distancia) y, en cuanto lo vuelve a ver, sigue directo;
//  3. si no sabe dónde está, se lo pregunta por privado (/msg) y espera sus
//     coordenadas ("120 -450" o "120 64 -450").

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"minecraft-bot/elytra"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/mj41/go-mc26-kit/bot/entities"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// seenPos es dónde se vio a un jugador por última vez.
type seenPos struct {
	x, y, z float64
	at      time.Time
}

// coordsMsg son unas coordenadas que un jugador escribió.
type coordsMsg struct {
	x, z float64
	at   time.Time
}

var travel struct {
	sync.Mutex
	seen   map[uuid.UUID]seenPos   // última posición vista de cada jugador
	coords map[uuid.UUID]coordsMsg // últimas coordenadas que escribió cada jugador
	asked  map[string]uuid.UUID    // nombre (minúsculas) → a quién se le preguntó
}

// watchSeen apunta la posición de cada jugador que el bot ve, cada vez que se mueve.
func (b *Bot) watchSeen() {
	travel.Lock()
	travel.seen = map[uuid.UUID]seenPos{}
	travel.coords = map[uuid.UUID]coordsMsg{}
	travel.asked = map[string]uuid.UUID{}
	travel.Unlock()
	note := func(e entities.Entity) {
		if e.Type != "minecraft:player" {
			return
		}
		travel.Lock()
		travel.seen[e.UUID] = seenPos{e.X, e.Y, e.Z, time.Now()}
		travel.Unlock()
	}
	b.Entities.Watch(entities.Events{
		Added:   note,
		Removed: note, // al salir de la vista: su último sitio
		Moved:   func(e entities.Entity, _ []entities.Step) { note(e) },
	})
}

func lastSeen(id uuid.UUID) (seenPos, bool) {
	travel.Lock()
	defer travel.Unlock()
	p, ok := travel.seen[id]
	return p, ok
}

// numbers son los números de un texto (coordenadas: "120 64 -450", "x: 120, z: -450").
var numbers = regexp.MustCompile(`-?\d+(?:\.\d+)?`)

// parseCoords saca x y z de un mensaje con 2 números (x z) o 3 (x y z).
func parseCoords(text string) (x, z float64, ok bool) {
	if len(text) > 80 {
		return 0, 0, false // un mensaje largo no es una respuesta de coordenadas
	}
	ns := numbers.FindAllString(text, -1)
	var v []float64
	for _, n := range ns {
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, 0, false
		}
		v = append(v, f)
	}
	switch len(v) {
	case 2:
		return v[0], v[1], true
	case 3:
		return v[0], v[2], true
	}
	return 0, 0, false
}

// noteCoords guarda las coordenadas que escribió un jugador (chat o privado).
func noteCoords(id uuid.UUID, text string) {
	x, z, ok := parseCoords(text)
	if !ok || id == uuid.Nil {
		return
	}
	travel.Lock()
	travel.coords[id] = coordsMsg{x, z, time.Now()}
	travel.Unlock()
}

func coordsSince(id uuid.UUID, t time.Time) (coordsMsg, bool) {
	travel.Lock()
	defer travel.Unlock()
	c, ok := travel.coords[id]
	return c, ok && c.at.After(t)
}

// gotoPlayer va hasta el jugador uid, esté donde esté (ver arriba).
func (b *Bot) gotoPlayer(ctx context.Context, name string, uid uuid.UUID) {
	defer b.Walker.Stop()
	visible := func() bool { _, ok := b.Entities.ByUUID(uid); return ok }
	var askedAt time.Time // cuándo se le preguntó (cero: aún no)
	triedSeen := false
	for ctx.Err() == nil {
		if b.Combat.Fighting() { // sobrevivir primero; al terminar, se sigue
			if !b.waitFight(ctx) {
				return
			}
			continue
		}
		if !isOnline(name) {
			log.Printf("goto: %s se desconectó", name)
			return
		}
		// 1. a la vista: directo (lejos, volando si lleva élitros y cohetes)
		if e, ok := b.Entities.ByUUID(uid); ok {
			if b.flyToPlayer(ctx, e.ID) && ctx.Err() != nil {
				return
			}
			if b.chase(ctx, name, e.ID) {
				log.Printf("goto: llegué hasta %s", name)
				return
			}
			continue // lo perdió de vista o se cortó: otra vuelta
		}
		// 2. coordenadas que nos dio (después de preguntarle) o 3. donde lo vimos
		var tx, tz float64
		ty := math.NaN() // su altura, si se sabe (para volar a su capa)
		var src string
		if c, ok := coordsSince(uid, askedAt); ok && !askedAt.IsZero() {
			tx, tz, src = c.x, c.z, "las coordenadas que me dio"
			askedAt = time.Now() // las siguientes que mande, si vuelve a moverse
		} else if p, ok := lastSeen(uid); ok && !triedSeen {
			tx, ty, tz, src = p.x, p.y, p.z, fmt.Sprintf("donde lo vi hace %s", time.Since(p.at).Round(time.Second))
			triedSeen = true
		} else {
			// no se sabe dónde está: preguntarle (una vez) y esperar
			if askedAt.IsZero() || time.Since(askedAt) > 60*time.Second {
				if !askedAt.IsZero() {
					log.Printf("goto: %s no me dijo dónde está; usa /goto <x> <z>", name)
					return
				}
				b.askWhere(name, uid)
				askedAt = time.Now()
			}
			if b.Ctl.WaitTicks(ctx, 10) != nil {
				return
			}
			continue
		}
		p := b.Player.Position()
		log.Printf("goto: %s no está a la vista; voy a %s: %.0f %.0f (a %.0f bloques)", name, src, tx, tz, math.Hypot(tx-p.X, tz-p.Z))
		// volando si se puede (y si no, se le dice por qué); lo que quede, andando
		if b.flyTargetTold(ctx, func() (float64, float64, float64, bool, bool) { return tx, ty, tz, true, true }, name) && ctx.Err() != nil {
			return
		}
		if b.gotoCoordsY(ctx, tx, ty, tz, visible) && !visible() {
			log.Printf("goto: %s no está aquí", name)
			// allí no está: preguntar (si no se hizo ya)
		}
	}
}

// flyNear vuela hasta cerca de (x, y, z) si está lejos y se puede (élitros,
// cohetes, /elytra on); dice si lo intentó. Lo que quede, se anda. y es la
// altura de la meta (NaN: no se sabe): se aterriza lo más cerca posible de
// ella (en lo alto de una torre, sí; en una cueva no se entra volando).
func (b *Bot) flyNear(ctx context.Context, x, y, z float64) bool {
	return b.flyTarget(ctx, func() (float64, float64, float64, bool, bool) { return x, y, z, true, true })
}

// flyToPlayer vuela tras el jugador id (que puede moverse, o volar) si está
// lejos o muy por encima; dice si lo intentó. Si no puede volar (sin
// cohetes, sin élitros que aguanten), se lo dice a él por privado.
func (b *Bot) flyToPlayer(ctx context.Context, id int32) bool {
	name := ""
	if e, ok := b.Entities.Get(id); ok {
		name = nameOf(e.UUID)
	}
	return b.flyTargetTold(ctx, func() (float64, float64, float64, bool, bool) {
		e, ok := b.Entities.Get(id)
		return e.X, e.Y, e.Z, e.OnGround, ok
	}, name)
}

// flyTold: lo último que se le dijo a cada jugador de por qué no se vuela, y cuándo.
var flyTold struct {
	sync.Mutex
	last map[string]flyNote
}

type flyNote struct {
	why string
	at  time.Time
}

// tellNoFly le dice a name por qué no vuela con él (una vez cada 2 minutos
// por motivo: en los logs, no voló sin decir nada y él no sabía por qué).
func (b *Bot) tellNoFly(name, why string) {
	if name == "" {
		return
	}
	flyTold.Lock()
	if flyTold.last == nil {
		flyTold.last = map[string]flyNote{}
	}
	n := flyTold.last[name]
	if n.why == why && time.Since(n.at) < 2*time.Minute {
		flyTold.Unlock()
		return
	}
	flyTold.last[name] = flyNote{why, time.Now()}
	flyTold.Unlock()
	b.whisper(name, "no puedo volar contigo: "+why)
}

// noFlyUntil: tras un vuelo que no se pudo (sin cohetes, sin élitros), no
// se vuelve a intentar (ni a decirlo) hasta entonces.
var noFlyUntil atomic.Int64

// flyTarget vuela hacia target si está lejos (o muy por encima: subir
// andando 100 bloques no se puede) y se puede; dice si lo intentó.
func (b *Bot) flyTarget(ctx context.Context, target func() (x, y, z float64, onGround, ok bool)) bool {
	return b.flyTargetTold(ctx, target, "")
}

// flyTargetTold es flyTarget diciéndole a name (si no es "") por qué no vuela.
func (b *Bot) flyTargetTold(ctx context.Context, target func() (x, y, z float64, onGround, ok bool), name string) bool {
	x, y, z, _, ok := target()
	if !ok || !elytraAllowed.Load() {
		return false
	}
	p := b.Player.Position()
	d := math.Hypot(x-p.X, z-p.Z)
	if up := y - p.Y; !math.IsNaN(y) && up > 0 {
		d = math.Hypot(d, up)
	}
	if d < flyMinDist {
		return false
	}
	if ok, why := b.Flight.Ready(d); !ok {
		if time.Now().UnixNano() >= noFlyUntil.Load() {
			if why != "no llevo élitros" {
				log.Printf("goto: no vuelo: %s; voy andando", why)
			}
			noFlyUntil.Store(time.Now().Add(30 * time.Second).UnixNano())
		}
		b.tellNoFly(name, why)
		return false
	}
	if time.Now().UnixNano() < noFlyUntil.Load() {
		return false // (un vuelo que acaba de fallar: no otra vez en seguida)
	}
	log.Printf("goto: voy volando con élitros (a %.0f bloques)", d)
	b.Walker.Stop()
	err := b.Flight.FlyFollow(ctx, target)
	if errors.Is(err, elytra.ErrNoHeadroom) && b.walkToSky(ctx) {
		err = b.Flight.FlyFollow(ctx, target)
	}
	if err != nil && ctx.Err() == nil {
		log.Printf("goto: el vuelo no salió (%v); sigo andando", err)
		noFlyUntil.Store(time.Now().Add(30 * time.Second).UnixNano())
		b.tellNoFly(name, err.Error())
	}
	return true
}

// walkToSky anda a un sitio cercano (a 12 bloques) con aire encima para
// despegar (debajo de un árbol, de un techo, no se puede).
func (b *Bot) walkToSky(ctx context.Context) bool {
	p := b.Player.Position()
	for r := 1; r <= 12; r++ {
		for k := 0; k < 8*r; k++ {
			a := float64(k) * 2 * math.Pi / float64(8*r)
			x, z := int(math.Floor(p.X+float64(r)*math.Cos(a))), int(math.Floor(p.Z+float64(r)*math.Sin(a)))
			g, ok := b.Walker.Ground(x, z, int(math.Floor(p.Y)))
			if !ok || !elytra.Headroom(b.World, float64(x)+0.5, float64(g.Y), float64(z)+0.5) {
				continue
			}
			log.Printf("élitros: aquí no hay sitio encima para despegar; voy a %d %d %d (a cielo abierto)", g.X, g.Y, g.Z)
			b.Walker.SetSprint(true)
			b.Walker.Goto(g)
			for i := 0; i < 20*20/5; i++ {
				if b.Ctl.WaitTicks(ctx, 5) != nil {
					return false
				}
				if st := b.Walker.Status(); st != "walking" {
					return st == "arrived"
				}
			}
			b.Walker.Stop()
			return false
		}
	}
	log.Printf("élitros: no hay sitio a cielo abierto cerca para despegar")
	return false
}

// askWhere le pregunta por privado a un jugador dónde está.
func (b *Bot) askWhere(name string, uid uuid.UUID) {
	travel.Lock()
	travel.asked[strings.ToLower(name)] = uid
	travel.Unlock()
	log.Printf("goto: no sé dónde está %s; se lo pregunto por privado", name)
	c := play.ChatCommand{Command: pk.String(fmt.Sprintf("msg %s ¿Dónde estás? Respóndeme con tus coordenadas: x z", name))}
	if err := b.Client.Conn.WritePacket(pk.Marshal(c.PacketID(), c)); err != nil {
		log.Print("goto: ", err)
	}
}

// chase persigue a un jugador a la vista hasta estar a su lado (true), o hasta
// perderlo de vista o que el camino falle (false).
func (b *Bot) chase(ctx context.Context, name string, id int32) bool {
	b.Walker.SetSprint(true)
	b.Walker.Follow(func() (float64, float64, float64, bool) {
		e, ok := b.Entities.Get(id)
		return e.X, e.Y, e.Z, ok
	}, 2)
	defer b.Walker.Stop()
	if e, ok := b.Entities.Get(id); ok {
		log.Printf("goto: veo a %s (a %.0f bloques): voy directo", name, b.distanceTo(e))
	}
	for {
		if b.Ctl.WaitTicks(ctx, 5) != nil || b.Combat.Fighting() {
			return false
		}
		e, ok := b.Entities.Get(id)
		if !ok {
			log.Printf("goto: perdí de vista a %s", name)
			return false
		}
		if b.distanceTo(e) <= 2.5 {
			return true
		}
	}
}

// waitFight espera a que termine la pelea en curso (pelear, o huir y comer,
// lo decide el módulo de pelea); false si cancelaron la orden mientras.
func (b *Bot) waitFight(ctx context.Context) bool {
	log.Print("pelea: la orden espera a que termine")
	for b.Combat.Fighting() {
		if b.Ctl.WaitTicks(ctx, 10) != nil {
			return false
		}
	}
	log.Print("pelea terminada: sigo con la orden")
	return true
}
