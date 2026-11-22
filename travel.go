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
	"fmt"
	"log"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
			log.Printf(tr("goto: %s disconnected", "goto: %s se desconectó"), name)
			return
		}
		// 1. a la vista: directo
		if e, ok := b.Entities.ByUUID(uid); ok {
			if b.chase(ctx, name, e.ID) {
				log.Printf(tr("goto: reached %s", "goto: llegué hasta %s"), name)
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
					log.Printf(tr("goto: %s didn't tell me where they are; use /goto <x> <y> <z>", "goto: %s no me dijo dónde está; usa /goto <x> <y> <z>"), name)
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
		log.Printf(tr("goto: %s isn't in sight; going to %s: %.0f %.0f (%.0f blocks away)", "goto: %s no está a la vista; voy a %s: %.0f %.0f (a %.0f bloques)"), name, src, tx, tz, math.Hypot(tx-p.X, tz-p.Z))
		if b.gotoCoordsY(ctx, tx, ty, tz, visible) && !visible() {
			log.Printf(tr("goto: %s isn't here", "goto: %s no está aquí"), name)
			// allí no está: preguntar (si no se hizo ya)
		}
	}
}

// askWhere le pregunta por privado a un jugador dónde está.
func (b *Bot) askWhere(name string, uid uuid.UUID) {
	travel.Lock()
	travel.asked[strings.ToLower(name)] = uid
	travel.Unlock()
	log.Printf(tr("goto: I don't know where %s is; asking them by whisper", "goto: no sé dónde está %s; se lo pregunto por privado"), name)
	c := play.ChatCommand{Command: pk.String(fmt.Sprintf("msg %s %s", name, tr("where are you? send me your coords (x z)", "¿dónde estás? pásame tus coords (x z)")))}
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
		log.Printf(tr("goto: I see %s (%.0f blocks away): heading straight there", "goto: veo a %s (a %.0f bloques): voy directo"), name, b.distanceTo(e))
	}
	for {
		if b.Ctl.WaitTicks(ctx, 5) != nil || b.Combat.Fighting() {
			return false
		}
		e, ok := b.Entities.Get(id)
		if !ok {
			log.Printf(tr("goto: lost sight of %s", "goto: perdí de vista a %s"), name)
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
	log.Print(tr("fight: the order waits until it's over", "pelea: la orden espera a que termine"))
	for b.Combat.Fighting() {
		if b.Ctl.WaitTicks(ctx, 10) != nil {
			return false
		}
	}
	log.Print(tr("fight over: back to the order", "pelea terminada: sigo con la orden"))
	return true
}
