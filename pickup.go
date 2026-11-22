package main

// Recoger lo que le tiran: un objeto que aparece cerca del bot y junto a un
// jugador (se lo han tirado) se recoge, si el bot está libre o solo
// siguiendo/protegiendo a alguien (un desvío corto); no en una pelea, una
// tarea (/chop) ni un /goto. Lo que sea mejor que lo que lleva puesto se lo
// pone el módulo de pelea al momento.
//
// Si lo que le tiran es útil (un pico, comida…), le cae cerca y no tiene
// hueco, tira un montón de basura (tierra, piedra, grava…) para que entre,
// también en mitad de una tarea: así puede seguir picando.

import (
	"log"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/protocol/types"

	"cubit/automation"
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
			e, ok := b.Entities.Get(id)
			if !ok || dropping() || !nearPlayer(b, e) {
				continue
			}
			b.makeRoomFor(e) // (aunque esté en una tarea: si le cae cerca, entra solo)
			if !b.canDetour() || b.distanceTo(e) > 6 || b.distanceTo(e) < 1 || automation.IsJunk(entityItemName(e)) {
				continue // (la basura no se va a buscar: puede ser la que acaba de tirar)
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

// makeRoomFor: lo que le han tirado cerca es útil y no le cabe: tira el
// montón más grande de basura para hacerle sitio (lo tirado no se puede
// recoger en 2 s: para entonces ya ha cogido lo otro).
func (b *Bot) makeRoomFor(e entities.Entity) {
	if b.distanceTo(e) <= 3 {
		b.makeRoomForItem(entityItemName(e))
	}
}

// makeRoomForItem: hace sitio para name si no le cabe y es útil (dice si tiró algo).
func (b *Bot) makeRoomForItem(name string) bool {
	if name == "" || automation.IsJunk(name) {
		return false
	}
	if _, open := b.Screens.Open(); open {
		return false // (con un cofre abierto, no se toca el inventario)
	}
	s := b.Screens
	s.Lock()
	junkSlot, junkCount := -1, 0
	for i := 9; i <= 44; i++ {
		sl := s.Inventory.Slots[i]
		n := slotItemName(sl)
		switch {
		case sl.Count <= 0:
			s.Unlock()
			return false // (hay hueco)
		case n == name && !unstackable(n):
			s.Unlock()
			return false // (se apila con lo que ya lleva)
		case automation.IsJunk(n) && int(sl.Count) > junkCount:
			junkSlot, junkCount = i, int(sl.Count)
		}
	}
	junkName := ""
	if junkSlot >= 0 {
		junkName = slotItemName(s.Inventory.Slots[junkSlot])
	}
	s.Unlock()
	if junkSlot < 0 {
		log.Printf(tr("someone threw me %s and my inventory is full: no junk to drop", "me tiran %s y no me cabe: no llevo basura que tirar"), name)
		return false
	}
	if err := s.Click(0, junkSlot, 1, types.ContainerInputThrow); err != nil {
		log.Printf("hacer sitio: %v", err)
		return false
	}
	log.Printf(tr("someone threw me %s and my inventory is full: dropping %d %s to make room", "me tiran %s y no me cabe: tiro %d de %s para hacerle sitio"), name, junkCount, junkName)
	return true
}

// entityItemName: lo que es un objeto en el suelo ("diamond_pickaxe"; "" si no se sabe).
func entityItemName(e entities.Entity) string {
	v, ok := e.Data[entitydata.ItemEntityItem]
	if !ok {
		return ""
	}
	st, ok := v.Value.(*types.ItemStack)
	if !ok || st == nil {
		return ""
	}
	return slotItemName(*st)
}

// slotItemName: el nombre (sin "minecraft:") de lo que hay en un hueco.
func slotItemName(st types.ItemStack) string {
	if st.Count <= 0 || int(st.Item) < 0 || int(st.Item) >= len(registryid.Item) {
		return ""
	}
	return strings.TrimPrefix(registryid.Item[st.Item], "minecraft:")
}

// unstackable: herramientas, armas y armadura (de uno en uno).
func unstackable(n string) bool {
	for _, suf := range []string{"_pickaxe", "_axe", "_shovel", "_hoe", "_sword", "_helmet", "_chestplate", "_leggings", "_boots", "bow", "crossbow", "trident", "shield", "elytra", "mace", "_spear", "fishing_rod", "shears", "flint_and_steel"} {
		if strings.HasSuffix(n, suf) {
			return true
		}
	}
	return false
}
