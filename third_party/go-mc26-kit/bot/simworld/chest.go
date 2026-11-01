package simworld

// PATCH: chests. A chest (or a barrel) is a block with 27 slots
// (ChestBlockEntity, a single chest: a double one is not modelled); its menu is
// the generic 9×3 (ChestMenu): its slots, then the pack and the hotbar. A
// shift-click takes a chest's stack into the player's inventory from its end,
// or puts the player's into the chest. What a test puts in one comes from
// PutInChest; ChestCount reads it.

import (
	"slices"

	"github.com/mj41/go-mc26/chat"
	"github.com/mj41/go-mc26/data/registryid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

const chestSlots = 27

// chest is the chest's slots at pos (made empty the first time).
func (g *Game) chest(pos [3]int) *[chestSlots]slotItem {
	if g.chests == nil {
		g.chests = map[[3]int]*[chestSlots]slotItem{}
	}
	c, ok := g.chests[pos]
	if !ok {
		c = &[chestSlots]slotItem{}
		g.chests[pos] = c
	}
	return c
}

func chestMenu(id int, pos [3]int) *menu {
	return &menu{id: id, chest: true, pos: pos, pack0: chestSlots, hotbar0: chestSlots + 27, end: chestSlots + 36}
}

// openChest opens the chest at pos: the screen, then its contents.
func (g *Game) openChest(pos [3]int) error {
	g.containerID = g.containerID%100 + 1
	m := chestMenu(g.containerID, pos)
	g.open = m
	idx := slices.Index(registryid.Menu, "minecraft:generic_9x3")
	if err := g.S.Send(play.OpenScreen{ContainerID: pk.VarInt(m.id), Type: pk.VarInt(idx), Title: types.Text(chat.Text("Chest"))}); err != nil {
		return err
	}
	g.stateID++
	view := g.view(m)
	items := make([]types.ItemStack, m.end)
	for i, s := range view {
		items[i] = itemStack(s)
	}
	copy(m.snap[:], view)
	return g.S.Send(play.ContainerSetContent{ContainerID: pk.VarInt(m.id), StateID: pk.VarInt(g.stateID), Items: items, CarriedItem: itemStack(g.cursor)})
}

// PutInChest puts n of item ("minecraft:bread") into the chest at x, y, z: onto
// its stacks of the item, then into empty slots. It says how many did not fit.
func (g *Game) PutInChest(x, y, z int, item string, n int) int {
	c := g.chest([3]int{x, y, z})
	limit := stackLimit(item)
	for i := range c {
		if n > 0 && c[i].n > 0 && c[i].item == item && c[i].n < limit {
			d := min(n, limit-c[i].n)
			c[i].n += d
			n -= d
		}
	}
	for i := range c {
		if n > 0 && c[i].n == 0 {
			d := min(n, limit)
			c[i] = slotItem{item: item, n: d}
			n -= d
		}
	}
	return n
}

// ChestCount is how many of item the chest at x, y, z holds.
func (g *Game) ChestCount(x, y, z int, item string) int {
	n := 0
	for _, s := range g.chest([3]int{x, y, z}) {
		if s.n > 0 && s.item == item {
			n += s.n
		}
	}
	return n
}
