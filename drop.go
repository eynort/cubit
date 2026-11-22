package main

// /drop: el bot tira al suelo todo lo que lleva (inventario, barra, mano
// izquierda y armadura). /drop <objeto> [<cantidad>]: solo ese objeto (todo
// lo que lleve de él, o esa cantidad), y con un jugador detrás se lo lleva
// a él. /drop <jugador>: va hasta él y se lo tira todo, mirándolo.

import (
	"context"
	"log"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/protocol/types"
)

// droppedAt es cuándo tiró sus cosas por última vez (UnixNano): un rato
// después no recoge lo que hay en el suelo (sería lo suyo) ni coge el arma.
var droppedAt atomic.Int64

// dropping: tiró sus cosas hace poco (o las está tirando).
func dropping() bool {
	return time.Since(time.Unix(0, droppedAt.Load())) < 30*time.Second
}

// dropSlots son los slots del menú del inventario que se vacían: la armadura
// (5-8), el inventario y la barra (9-44) y la mano izquierda (45).
var dropSlots = func() []int {
	var s []int
	for i := 5; i <= 45; i++ {
		s = append(s, i)
	}
	return s
}()

func cmdDrop(b *Bot, args []string) error {
	if len(args) > 3 {
		return errf("usage: /drop [<item> [<amount>] [<player>] | <player>]", "uso: /drop [<item> [<amount>] [<player>] | <player>]")
	}
	if len(args) == 0 {
		b.stopForDrop()
		go b.dropAll(b.ctx, "")
		return nil
	}
	// un objeto que lleva (o, con más argumentos, siempre un objeto); si no, un jugador
	item := normItem(args[0])
	have := b.itemTotals()[item]
	if have == 0 && len(args) == 1 {
		name := args[0]
		return b.dropTo(name, tr("everything", "todo"), func(ctx context.Context) { b.dropAll(ctx, name) })
	}
	if have == 0 {
		if !knownItem(item) {
			return errf("%s is not an item", "%s no es un objeto", args[0])
		}
		return errf("I carry no %s", "no llevo %s", item)
	}
	// lo que sigue: [<cantidad>] [<jugador>]
	n, to := have, ""
	rest := args[1:]
	if len(rest) > 0 && isDropAmount(rest[0]) {
		if !strings.EqualFold(rest[0], "all") {
			v, _ := strconv.Atoi(rest[0])
			if v <= 0 {
				return errf("the amount must be a number above 0: %s", "la cantidad tiene que ser un número mayor que 0: %s", rest[0])
			}
			if v > have {
				log.Printf(tr("drop: I only carry %d %s: I throw them all", "drop: solo llevo %d %s: los tiro todos"), have, item)
			}
			n = min(v, have)
		}
		rest = rest[1:]
	}
	switch len(rest) {
	case 0:
	case 1:
		to = rest[0]
	default:
		return errf("usage: /drop <item> [<amount>] [<player>]", "uso: /drop <item> [<amount>] [<player>]")
	}
	if to == "" {
		b.stopForDrop()
		go b.dropItem(b.ctx, item, n, "")
		return nil
	}
	return b.dropTo(to, strconv.Itoa(n)+" "+item, func(ctx context.Context) { b.dropItem(ctx, item, n, to) })
}

// isDropAmount: es una cantidad ("10", "all") y no un jugador.
func isDropAmount(s string) bool {
	if strings.EqualFold(s, "all") {
		return true
	}
	_, err := strconv.Atoi(s)
	return err == nil
}

// dropTo va hasta el jugador name y, si llega, le tira lo suyo con drop
// (what es lo que le lleva, para el log).
func (b *Bot) dropTo(name, what string, drop func(ctx context.Context)) error {
	if strings.EqualFold(name, botName) {
		return errf("that is me", "ese soy yo")
	}
	uid, ok := b.Players.ByName(name)
	if !ok {
		return errf("%s is not online (names are case-sensitive)", "%s no está conectado (el nombre distingue mayúsculas)", name)
	}
	ctx, cancel := b.startMove(b.ctx)
	gen := setMoveKind("goto")
	log.Printf(tr("drop: going to %s to give them %s", "drop: voy hasta %s para darle %s"), name, what)
	go func() {
		defer cancel()
		defer clearMove(gen)
		b.gotoPlayer(ctx, name, uid)
		if ctx.Err() != nil {
			return
		}
		e, ok := b.Entities.ByUUID(uid)
		if !ok || b.distanceTo(e) > 4 {
			log.Printf(tr("drop: I did not reach %s: I throw nothing", "drop: no llegué hasta %s: no tiro nada"), name)
			return
		}
		drop(ctx)
	}()
	return nil
}

// lookAtPlayer mira al pecho del jugador to (que lo que tire le caiga delante).
func (b *Bot) lookAtPlayer(to string) {
	if to == "" || b.Players == nil || b.Entities == nil {
		return
	}
	id, ok := b.Players.ByName(to)
	if !ok {
		return
	}
	if e, ok := b.Entities.ByUUID(id); ok {
		act.LookAt(b.Player, false, e.X, e.Y+1, e.Z)
	}
}

// stopForDrop para lo que esté haciendo: tirar mueve el inventario.
func (b *Bot) stopForDrop() {
	b.cancelMove()
	b.Tasks.Stop()
	b.Walker.Stop()
}

// dropItem tira n unidades de item: montones enteros mientras quepan y el
// resto de uno en uno (como Q sin Ctrl). Primero el inventario, luego la
// barra, la otra mano y lo puesto.
func (b *Bot) dropItem(ctx context.Context, item string, n int, to string) {
	droppedAt.Store(time.Now().UnixNano())
	defer droppedAt.Store(time.Now().UnixNano())
	if to != "" {
		b.lookAtPlayer(to)
		if b.Ctl.WaitTicks(ctx, 2) != nil { // que el servidor sepa hacia dónde mira
			return
		}
	}
	b.resyncInventory(ctx)
	left := n
	for _, i := range itemDropOrder {
		if left <= 0 {
			break
		}
		b.Screens.Lock()
		s := b.Screens.Inventory.Slots[i]
		b.Screens.Unlock()
		if s.Count <= 0 || shortName(s) != item {
			continue
		}
		b.lookAtPlayer(to)
		if int(s.Count) <= left { // el montón entero (botón 1, como Ctrl+Q)
			if err := b.Screens.Click(0, i, 1, types.ContainerInputThrow); err != nil {
				log.Printf("drop: slot %d: %v", i, err)
				continue
			}
			left -= int(s.Count)
			if b.Ctl.WaitTicks(ctx, 1) != nil {
				return
			}
			continue
		}
		for left > 0 { // de uno en uno (botón 0, como Q)
			if err := b.Screens.Click(0, i, 0, types.ContainerInputThrow); err != nil {
				log.Printf("drop: slot %d: %v", i, err)
				break
			}
			left--
			if b.Ctl.WaitTicks(ctx, 1) != nil {
				return
			}
		}
	}
	if left == n {
		log.Printf(tr("drop: I carry no %s", "drop: no llevo %s"), item)
		return
	}
	if to != "" {
		log.Printf(tr("drop: thrown %d %s to %s", "drop: le he tirado %d %s a %s"), n-left, item, to)
		return
	}
	log.Printf(tr("drop: thrown %d %s", "drop: he tirado %d %s"), n-left, item)
}

// itemDropOrder: el orden en que /drop <objeto> vacía los slots.
var itemDropOrder = func() []int {
	var s []int
	for i := invMain; i <= invOffhand; i++ {
		s = append(s, i)
	}
	for i := invArmor; i < invMain; i++ {
		s = append(s, i)
	}
	return s
}()

// itemTotals: cuánto lleva de cada objeto (inventario, barra, otra mano y puesto).
func (b *Bot) itemTotals() map[string]int {
	out := map[string]int{}
	if b == nil || b.Screens == nil {
		return out
	}
	b.Screens.Lock()
	defer b.Screens.Unlock()
	for _, i := range dropSlots {
		if i < len(b.Screens.Inventory.Slots) {
			if n := shortName(b.Screens.Inventory.Slots[i]); n != "" {
				out[n] += int(b.Screens.Inventory.Slots[i].Count)
			}
		}
	}
	return out
}

// carriedSource da lo que lleva el bot (para el autocompletado); lo pone setupModules.
var carriedSource atomic.Pointer[func() map[string]int]

type carried struct {
	name  string
	count int
}

// carriedItems: lo que lleva cuyo nombre empieza por prefix, por orden alfabético.
func carriedItems(prefix string) []carried {
	f := carriedSource.Load()
	if f == nil {
		return nil
	}
	prefix = normItem(prefix)
	var out []carried
	for n, c := range (*f)() {
		if strings.HasPrefix(n, prefix) {
			out = append(out, carried{n, c})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// carriedCount: cuánto lleva de item (0 si no se sabe).
func carriedCount(item string) int {
	if f := carriedSource.Load(); f != nil {
		return (*f)()[item]
	}
	return 0
}

// normItem: "Minecraft:Dirt" → "dirt".
func normItem(s string) string {
	return strings.TrimPrefix(strings.ToLower(s), "minecraft:")
}

// knownItem: es un objeto del juego.
func knownItem(name string) bool { return slices.Contains(registryid.Item, "minecraft:"+name) }

// dropAll tira todo lo que lleva; con to, mirando a ese jugador (se lo da).
func (b *Bot) dropAll(ctx context.Context, to string) {
	droppedAt.Store(time.Now().UnixNano())
	defer droppedAt.Store(time.Now().UnixNano()) // los 30 s cuentan desde que acaba
	look := func() { b.lookAtPlayer(to) }
	look()
	if b.Ctl.WaitTicks(ctx, 2) != nil { // que el servidor sepa hacia dónde mira
		return
	}
	// lo que lleva de verdad (el servidor), no lo que el bot cree: un montón
	// que no supiera que tiene no se tiraría
	b.resyncInventory(ctx)
	n := 0
	for _, i := range dropSlots {
		b.Screens.Lock()
		s := b.Screens.Inventory.Slots[i]
		b.Screens.Unlock()
		if s.Count <= 0 {
			continue
		}
		look()
		// tirar (botón 1: todo el montón, como Ctrl+Q)
		if err := b.Screens.Click(0, i, 1, types.ContainerInputThrow); err != nil {
			log.Printf("drop: slot %d: %v", i, err)
			continue
		}
		n += int(s.Count)
		if b.Ctl.WaitTicks(ctx, 1) != nil {
			return
		}
	}
	switch {
	case n == 0:
		log.Print(tr("drop: my inventory is empty", "drop: no llevo nada"))
	case to != "":
		log.Printf(tr("drop: gave %s everything I had (%d items)", "drop: le he tirado a %s todo lo que llevaba (%d objetos)"), to, n)
	default:
		log.Printf(tr("drop: dropped everything I had (%d items)", "drop: he tirado todo lo que llevaba (%d objetos)"), n)
	}
}

// resyncInventory pide al servidor el inventario entero y espera a que llegue
// (unos ticks): lo que se ve o se tira es lo que hay de verdad. Con un cofre
// abierto el servidor no contesta (y no hace falta: el cofre trae el inventario).
func (b *Bot) resyncInventory(ctx context.Context) {
	if _, open := b.Screens.Open(); open {
		return
	}
	if err := b.Screens.Resync(); err != nil {
		log.Printf(tr("inventory: couldn't get it from the server: %v", "inventario: no pude pedirlo al servidor: %v"), err)
		return
	}
	_ = b.Ctl.WaitTicks(ctx, 6)
}
