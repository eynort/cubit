package pathing

// El constructor del walker: pone bloques de construir (piedra, tierra,
// tablones…) para puentes y torres. Nunca usa bloques de valor ni los que
// caen (arena, grava) ni los que tienen uso (mesas, cofres).

import (
	"context"
	"log"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/block"
	"github.com/mj41/go-mc26/protocol/types"
)

// buildBlocks son los bloques que se gastan en construir, del más barato al
// más caro: la tierra y lo que no sirve para nada primero; los adoquines
// (herramientas, hornos) y la piedra al final.
var buildBlocks = []string{
	"minecraft:dirt", "minecraft:netherrack", "minecraft:andesite", "minecraft:diorite", "minecraft:granite",
	"minecraft:tuff", "minecraft:end_stone", "minecraft:sandstone", "minecraft:cobbled_deepslate", "minecraft:cobblestone",
	"minecraft:blackstone", "minecraft:stone", "minecraft:deepslate",
	"minecraft:oak_planks", "minecraft:spruce_planks", "minecraft:birch_planks", "minecraft:jungle_planks",
	"minecraft:acacia_planks", "minecraft:dark_oak_planks", "minecraft:mangrove_planks", "minecraft:cherry_planks",
	"minecraft:bamboo_planks", "minecraft:crimson_planks", "minecraft:warped_planks", "minecraft:pale_oak_planks",
}

// HandsBuilder es un Builder que pone los bloques con las manos del bot.
type HandsBuilder struct {
	Screens *screen.Manager
	Hands   *act.Hands
	Ctl     *control.Controller
	Ctx     context.Context

	placing atomic.Bool
	moving  atomic.Bool // pasando un bloque del inventario a la barra

	refused    atomic.Int32 // bloques seguidos que el servidor no puso
	placedMu   sync.Mutex
	placed     map[world.BlockPos]bool // los que puso (para quitarlos luego: una torre)
	pauseUntil atomic.Int64            // hasta cuándo no construir (UnixNano): zona protegida

	keep    atomic.Pointer[func(item string) bool]    // objetos que no se gastan en torres (SetKeep)
	noBreak atomic.Pointer[func(world.BlockPos) bool] // celdas que no se rompen para pasar (SetNoBreak)
}

// SetKeep: los objetos para los que keep dice true no se gastan en torres ni
// puentes (los materiales de una construcción); nil: se gasta lo de siempre.
func (bd *HandsBuilder) SetKeep(keep func(item string) bool) {
	if keep == nil {
		bd.keep.Store(nil)
		return
	}
	bd.keep.Store(&keep)
}

// SetNoBreak: las celdas para las que no dice true no se rompen para pasar
// (hojas, techos de una torre): lo ya construido. nil: cualquiera.
func (bd *HandsBuilder) SetNoBreak(no func(world.BlockPos) bool) {
	if no == nil {
		bd.noBreak.Store(nil)
		return
	}
	bd.noBreak.Store(&no)
}

// kept: el objeto (con "minecraft:") no se gasta en construir.
func (bd *HandsBuilder) kept(item string) bool {
	k := bd.keep.Load()
	return k != nil && (*k)(strings.TrimPrefix(item, "minecraft:"))
}

// CanBreak: la celda se puede romper (no es parte de lo que se construye).
func (bd *HandsBuilder) CanBreak(p world.BlockPos) bool { return !bd.unbreakable(p) }

func (bd *HandsBuilder) unbreakable(p world.BlockPos) bool {
	n := bd.noBreak.Load()
	return n != nil && (*n)(p)
}

func itemNameOf(id int32) string {
	if id >= 0 && int(id) < len(registryid.Item) {
		return registryid.Item[id]
	}
	return ""
}

// rank es la preferencia de un objeto como bloque de construir (-1: no lo es):
// primero los de buildBlocks, en su orden; después cualquier otro bloque
// entero y sólido que no sea de valor, no caiga y no tenga uso propio.
func rank(item string) int {
	for i, n := range buildBlocks {
		if n == item {
			return i
		}
	}
	if anySolid(item) {
		return len(buildBlocks)
	}
	return -1
}

// IsBuildBlock: el objeto (sin "minecraft:") vale para torres y puentes.
func IsBuildBlock(item string) bool {
	if !strings.Contains(item, ":") {
		item = "minecraft:" + item
	}
	return rank(item) >= 0
}

// notToBuild: bloques enteros que no se gastan en un puente (de valor, que
// caen, con uso propio o raros).
var notToBuild = []string{
	"_log", "_wood", "_stem", "_hyphae", // la madera se recoge, no se gasta en torres
	"_ore", "diamond", "emerald", "gold_block", "iron_block", "netherite", "lapis_block", "redstone_block",
	"copper_block", "amethyst", "beacon", "spawner", "tnt", "shulker", "bedrock", "barrier", "command_block",
	"structure", "jigsaw", "sand", "gravel", "concrete_powder", "anvil", "dragon_egg", "sponge", "ancient_debris",
	"crying_obsidian", "respawn_anchor", "lodestone", "chest", "barrel", "furnace", "smoker", "crafting_table",
	"table", "dispenser", "dropper", "hopper", "observer", "piston", "note_block", "jukebox", "bookshelf",
	"_bed", "slime_block", "honey_block", "magma_block", "ice", "snow_block", "scaffolding", "suspicious",
	"budding", "head", "skull", "melon", "pumpkin", "hay_block", "beehive", "bee_nest", "target", "lectern",
	"loom", "stonecutter", "grindstone", "smithing", "cartography", "fletching", "composter", "cauldron",
	"enchanting", "brewing", "conduit", "vault", "trial_spawner", "crafter", "creaking", "sculk",
}

// anySolid: el objeto pone un bloque entero y sólido apto para construir.
func anySolid(item string) bool {
	for _, bad := range notToBuild {
		if strings.Contains(item, bad) {
			return false
		}
	}
	b, ok := block.FromID[item]
	if !ok {
		return false
	}
	st, ok := block.ToStateID[b]
	if !ok {
		return false
	}
	shape := block.CollisionShape(st)
	if len(shape) != 1 {
		return false
	}
	c := shape[0]
	return c.MinX <= 1e-9 && c.MinY <= 1e-9 && c.MinZ <= 1e-9 && c.MaxX >= 1-1e-9 && c.MaxY >= 1-1e-9 && c.MaxZ >= 1-1e-9
}

// best es el slot del menú con el mejor bloque de construir (-1: ninguno) y cuántos hay en total.
func (bd *HandsBuilder) best() (slot, total int) {
	bd.Screens.Lock()
	defer bd.Screens.Unlock()
	slot, bestRank := -1, len(buildBlocks)
	for i := 9; i <= 44; i++ {
		s := bd.Screens.Inventory.Slots[i]
		if s.Count <= 0 {
			continue
		}
		name := itemNameOf(int32(s.Item))
		r := rank(name)
		if r < 0 || bd.kept(name) {
			continue
		}
		total += int(s.Count)
		// en la barra gana el empate: no hay que moverlo
		if r < bestRank || r == bestRank && i >= 36 && slot < 36 {
			slot, bestRank = i, r
		}
	}
	return slot, total
}

// Blocks son los bloques de construir; 0 mientras está en pausa (el servidor
// rechazó varios seguidos: una zona protegida), para que se busque otro camino.
func (bd *HandsBuilder) Blocks() int {
	if time.Now().UnixNano() < bd.pauseUntil.Load() {
		return 0
	}
	_, n := bd.best()
	return n
}

// freeHotbar es el slot de la barra al que pasar los bloques: uno vacío, o el
// 8 (el combate deja la 8 para lo último: ahí no tiene la comida ni el arma).
func (bd *HandsBuilder) freeHotbar() int {
	bd.Screens.Lock()
	defer bd.Screens.Unlock()
	for h := 8; h >= 0; h-- {
		if bd.Screens.Inventory.Slots[36+h].Count <= 0 {
			return h
		}
	}
	return 8
}

func (bd *HandsBuilder) Busy() bool { return bd.placing.Load() || bd.moving.Load() }

// Place pone el mejor bloque de construir en pos contra against. Si no está en
// la barra, primero lo pasa a ella (en otra goroutine) y devuelve false: el
// walker lo vuelve a pedir.
func (bd *HandsBuilder) Place(pos, against world.BlockPos) bool { return bd.place(pos, against, false) }

// PlaceNow es Place sin esperar la respuesta del servidor (como el cliente:
// el bloque se da por puesto y, si el servidor no lo puso, su respuesta lo
// quita): un bloque cada dos ticks en vez de uno por ida y vuelta. Para
// encerrarse en una pelea, donde cada tick cuenta.
func (bd *HandsBuilder) PlaceNow(pos, against world.BlockPos) bool {
	return bd.place(pos, against, true)
}

func (bd *HandsBuilder) place(pos, against world.BlockPos, now bool) bool {
	if bd.Busy() {
		return false
	}
	if _, _, eating := bd.Hands.Using(); eating {
		return false // comiendo: la mano no se toca (luego se pone)
	}
	slot, _ := bd.best()
	if slot < 0 {
		return false
	}
	if slot < 36 { // en el inventario: a la barra primero (a un hueco; si no hay, a la 8)
		bd.moving.Store(true)
		to := bd.freeHotbar()
		go func() {
			defer bd.moving.Store(false)
			if err := bd.Screens.Click(0, slot, to, types.ContainerInputSwap); err != nil {
				log.Print("construir: ", err)
			}
			_ = bd.Ctl.WaitTicks(bd.Ctx, 2)
		}()
		return false
	}
	bd.Screens.Lock()
	held := bd.Screens.HeldSlot
	bd.Screens.Unlock()
	if held != slot-36 {
		if err := bd.Screens.SelectHotbar(slot - 36); err != nil {
			log.Print("construir: ", err)
			return false
		}
		return false // en la mano el siguiente tick (el servidor ya lo sabe): entonces se pone
	}
	// lo que hay en la mano tiene que ser un bloque de construir: si otra cosa
	// (una tarea) cambió la mano entre medias, no se pone un palo
	inHand := bd.heldName()
	if rank(inHand) < 0 || bd.kept(inHand) {
		log.Printf("construir en %v: en la mano tengo %s, no un bloque: espero", pos, short(inHand))
		return false
	}
	log.Printf("construir: pongo %s en %v (en la mano: %s)", short(inHand), pos, short(inHand))
	bd.placing.Store(true)
	placeOn := bd.Hands.PlaceOn
	if now {
		placeOn = bd.Hands.PlaceOnNow
	}
	placeOn(pos, against, func(u act.Use) {
		bd.placing.Store(false)
		if u.Err == nil {
			bd.refused.Store(0)
			bd.placedMu.Lock()
			if bd.placed == nil {
				bd.placed = map[world.BlockPos]bool{}
			}
			bd.placed[pos] = true
			bd.placedMu.Unlock()
			return
		}
		if strings.Contains(u.Err.Error(), "in the way") {
			return
		}
		log.Printf("construir en %v: %v (en la mano: %s)", pos, u.Err, short(bd.heldName()))
		if strings.Contains(u.Err.Error(), "did not change") && bd.refused.Add(1) >= 4 {
			bd.refused.Store(0)
			bd.pauseUntil.Store(time.Now().Add(time.Minute).UnixNano())
			log.Print("construir: el servidor no me deja poner bloques aquí (¿zona protegida?): sin construir 1 minuto")
		}
	})
	return true
}

// heldName es lo que tiene en la mano ("" nada).
func (bd *HandsBuilder) heldName() string {
	bd.Screens.Lock()
	defer bd.Screens.Unlock()
	sl := bd.Screens.Inventory.Slots[36+bd.Screens.HeldSlot]
	if sl.Count <= 0 {
		return ""
	}
	return itemNameOf(int32(sl.Item))
}

func short(item string) string {
	if item == "" {
		return "nada"
	}
	return strings.TrimPrefix(item, "minecraft:")
}

// Placed son los bloques que puso y siguen apuntados (para quitarlos).
func (bd *HandsBuilder) Placed() []world.BlockPos {
	bd.placedMu.Lock()
	defer bd.placedMu.Unlock()
	var out []world.BlockPos
	for p := range bd.placed {
		out = append(out, p)
	}
	return out
}

// Paused: el servidor no deja construir aquí (zona protegida) de momento.
func (bd *HandsBuilder) Paused() bool { return time.Now().UnixNano() < bd.pauseUntil.Load() }

// Break rompe el bloque de pos (hojas que tapan el paso) con lo que tenga en
// la mano; false si está ocupado.
func (bd *HandsBuilder) Break(pos world.BlockPos) bool {
	if bd.Busy() || bd.unbreakable(pos) {
		return false
	}
	bd.placing.Store(true)
	bd.Hands.Dig(pos, func(d act.Dig) {
		bd.placing.Store(false)
		if d.Err != nil {
			log.Printf("romper %v para pasar: %v", pos, d.Err)
		}
	})
	return true
}

// Protected avisa de que el servidor dijo que aquí no se puede construir
// (un mensaje de zona protegida): sin construir 1 minuto.
func (bd *HandsBuilder) Protected() {
	bd.refused.Store(0)
	// (el servidor avisa una vez por cada bloque que no deja poner, varios
	// por segundo: se dice solo al empezar la pausa, no en cada aviso)
	paused := time.Now().UnixNano() < bd.pauseUntil.Load()
	bd.pauseUntil.Store(time.Now().Add(time.Minute).UnixNano())
	if !paused {
		log.Print("construir: el servidor dice que aquí no se construye: sin construir 1 minuto")
	}
}

// WasPlaced: el bloque en p lo puso el bot (una torre, un puente).
func (bd *HandsBuilder) WasPlaced(p world.BlockPos) bool {
	bd.placedMu.Lock()
	defer bd.placedMu.Unlock()
	return bd.placed[p]
}

// Forget olvida un bloque puesto (ya quitado).
func (bd *HandsBuilder) Forget(p world.BlockPos) {
	bd.placedMu.Lock()
	defer bd.placedMu.Unlock()
	delete(bd.placed, p)
}

// BreakTicks es lo que tarda en romper un bloque de estado st, de pie, con lo
// mejor que lleva en la barra (o la mano vacía), y en qué slot de la barra
// está; ticks < 0: no se rompe (roca madre, barrera). Sin los efectos (prisa,
// fatiga): solo para decidir si merece la pena.
func (bd *HandsBuilder) BreakTicks(st block.StateID) (ticks, hotbar int) {
	hardness := block.DestroySpeed(st)
	if hardness < 0 {
		return -1, -1
	}
	if hardness == 0 {
		return 1, -1
	}
	bd.Screens.Lock()
	slots := make([]types.ItemStack, 9)
	for h := 0; h < 9; h++ {
		slots[h] = bd.Screens.Inventory.Slots[36+h]
	}
	bd.Screens.Unlock()
	best, bestSlot := float32(0), -1
	for h, s := range slots {
		var speed float32 = 1
		correct := !block.RequiresCorrectTool(st)
		if s.Count > 0 {
			speed, correct = act.ToolSpeed(&bd.Hands.C.Tags, s, st)
		}
		mod := float32(100)
		if correct {
			mod = 30
		}
		if p := speed / hardness / mod; p > best {
			best, bestSlot = p, h
		}
	}
	if best <= 0 {
		return -1, -1
	}
	return int(math.Ceil(float64(1 / best))), bestSlot
}

// BreakBest rompe el bloque de pos (estado st) con lo mejor que lleva en la
// barra: si no lo tiene en la mano, lo coge y devuelve false (el walker lo
// vuelve a pedir el tick siguiente, como Place).
func (bd *HandsBuilder) BreakBest(pos world.BlockPos, st block.StateID) bool {
	if bd.Busy() {
		return false
	}
	if _, _, eating := bd.Hands.Using(); eating {
		return false
	}
	if _, h := bd.BreakTicks(st); h >= 0 {
		bd.Screens.Lock()
		held := bd.Screens.HeldSlot
		bd.Screens.Unlock()
		if held != h {
			if err := bd.Screens.SelectHotbar(h); err != nil {
				log.Print("romper: ", err)
			}
			return false
		}
	}
	return bd.Break(pos)
}
