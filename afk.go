package main

import (
	"context"
	"log"
	"math"
	"sync/atomic"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/clock"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"

	"cubit/automation"
)

// /afk: quedarse en un sitio las 24 horas (la tarea está en automation/afk.go).
// Aquí, lo que necesita del bot: la hora del juego y el tiempo (para saber
// cuándo se puede dormir) y la orden.

// worldState sigue la hora y las tormentas que manda el servidor.
type worldState struct {
	clk     *clock.Clock
	thunder atomic.Uint32 // float32: la fuerza de la tormenta (0 a 1)
}

// gameEventThunderLevel: el evento de ClientboundGameEventPacket con la fuerza de la tormenta.
const gameEventThunderLevel = 8

// setupAfk se engancha a los paquetes de la hora y del tiempo (al crear el bot).
func (b *Bot) setupAfk() {
	b.weather.clk = clock.New(b.Client)
	b.Client.Events.AddListener(bot.PacketHandler{Priority: 0, ID: packetid.ClientboundPlayGameEvent, F: func(p pk.Packet) error {
		var ge play.GameEvent
		if err := p.Scan(&ge); err != nil {
			return err
		}
		if ge.Event == gameEventThunderLevel {
			b.weather.thunder.Store(math.Float32bits(float32(ge.Param)))
		}
		return nil
	}})
}

// sleepTime: se puede dormir ahora (de noche, o con tormenta: como el juego,
// las camas valen de las 12542 a las 23459, o con tormenta a cualquier hora).
func (b *Bot) sleepTime() bool {
	if math.Float32frombits(b.weather.thunder.Load()) > 0.9 {
		return true
	}
	t, ok := b.weather.clk.TimeOfDay()
	return ok && t >= 12542 && t < 23460
}

func cmdAfk(b *Bot, args []string) error {
	p := b.Player.Position()
	spot := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
	switch {
	case len(args) == 0 || len(args) == 1 && args[0] == "here":
	case len(args) == 3:
		var v [3]int
		base := []float64{p.X, p.Y + 1e-6, p.Z}
		for i, a := range args {
			n, err := coord(a, base[i])
			if err != nil {
				return err
			}
			v[i] = int(math.Floor(n))
		}
		spot = world.BlockPos{X: v[0], Y: v[1], Z: v[2]}
	default:
		return errf("usage: /afk [here | <x> <y> <z>]", "uso: /afk [here | <x> <y> <z>]")
	}
	log.Printf(tr("afk: staying at %d %d %d (/stop to stop)", "afk: me quedo en %d %d %d (/stop para parar)"), spot.X, spot.Y, spot.Z)
	// por si muere o se reconecta: vuelve andando a este sitio (resumeWork)
	b.work.mu.Lock()
	b.work.pos, b.work.hasPos = spot, true
	b.work.mu.Unlock()
	b.startWork("afk", automation.Afk(automation.AfkOptions{
		Spot:  spot,
		Night: b.sleepTime,
		Travel: func(ctx context.Context, p world.BlockPos) bool {
			return b.gotoCell(ctx, float64(p.X)+0.5, float64(p.Y), float64(p.Z)+0.5, nil)
		},
	}))
	return nil
}
