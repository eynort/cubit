package main

// Recoger lo que le tiran: un objeto que aparece cerca del bot y junto a un
// jugador (se lo han tirado) se recoge, si el bot está libre o solo
// siguiendo/protegiendo a alguien (un desvío corto); no en una pelea, una
// tarea (/chop) ni un /goto. Lo que sea mejor que lo que lleva puesto se lo
// pone el módulo de pelea al momento.

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/world"
)

// moveKind es la orden de movimiento en curso ("goto", "follow", "guard"; ""
// ninguna), con un número por orden: una orden vieja que acaba no borra la nueva.
var moveKind struct {
	sync.Mutex
	kind string
	gen  int
}

// detour: el bot se ha desviado a recoger algo (follow/guard esperan).
var detour atomic.Bool

// setMoveKind marca la orden en curso y devuelve su número (para clearMove).
func setMoveKind(k string) int {
	moveKind.Lock()
	defer moveKind.Unlock()
	moveKind.kind = k
	moveKind.gen++
	return moveKind.gen
}

// clearMove quita la marca de la orden gen, si sigue siendo la actual.
func clearMove(gen int) {
	moveKind.Lock()
	defer moveKind.Unlock()
	if moveKind.gen == gen {
		moveKind.kind = ""
	}
}

func currentMove() string {
	moveKind.Lock()
	defer moveKind.Unlock()
	return moveKind.kind
}

// watchThrown recoge los objetos que le tiran.
func (b *Bot) watchThrown() {
	thrown := make(chan int32, 32)
	b.Entities.Watch(entities.Events{Added: func(e entities.Entity) {
		if e.Type == "minecraft:item" {
			select {
			case thrown <- e.ID:
			default:
			}
		}
	}})
	go func() {
		for {
			var id int32
			select {
			case <-b.ctx.Done():
				return
			case id = <-thrown:
			}
			if b.Ctl.WaitTicks(b.ctx, 10) != nil { // que caiga y se pare
				return
			}
			if !b.canDetour() || dropping() { // (lo que acaba de tirar él no se recoge)
				continue
			}
			e, ok := b.Entities.Get(id)
			if !ok || b.distanceTo(e) > 6 || b.distanceTo(e) < 1 || !nearPlayer(b, e) {
				continue
			}
			b.pickUp(e)
		}
	}()
}

// canDetour: libre, o solo siguiendo/protegiendo a alguien.
func (b *Bot) canDetour() bool {
	if b.Combat.Fighting() || b.Tasks.Status() != "" {
		return false
	}
	k := currentMove()
	return k == "" || k == "follow" || k == "guard"
}

// nearPlayer: hay un jugador (no el bot) a menos de 4 bloques del objeto: se lo tiraron.
func nearPlayer(b *Bot, item entities.Entity) bool {
	for _, e := range b.Entities.Nearby(item.X, item.Y, item.Z, 4) {
		if e.Type == "minecraft:player" {
			return true
		}
	}
	return false
}

// pickUp va hasta el objeto y espera a cogerlo (3 s como mucho).
func (b *Bot) pickUp(e entities.Entity) {
	detour.Store(true)
	defer detour.Store(false)
	cell := world.BlockPos{X: int(math.Floor(e.X)), Y: int(math.Floor(e.Y + 0.2)), Z: int(math.Floor(e.Z))}
	b.Walker.SetSprint(false)
	b.Walker.Goto(cell)
	defer b.Walker.Stop()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b.Ctl.WaitTicks(b.ctx, 2) != nil || b.Combat.Fighting() {
			return
		}
		if _, ok := b.Entities.Get(e.ID); !ok {
			return // cogido
		}
	}
}
