package screen

import (
	"slices"
	"testing"

	"github.com/mj41/go-mc26/data/registryid"

	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// TestOpenReplacesTheOldMenu: a crafting table left open (never closed by
// the bot), then the server opens a furnace: only the furnace is open — Open
// never gives the old menu's slots for the new one.
func TestOpenReplacesTheOldMenu(t *testing.T) {
	m := &Manager{Screens: map[int]Container{}}
	m.Screens[0] = &m.Inventory
	open := func(id int, typ int32) {
		t.Helper()
		p := pk.Marshal(play.OpenScreen{}.PacketID(), play.OpenScreen{ContainerID: pk.VarInt(id), Type: pk.VarInt(typ)})
		if err := m.onOpenScreen(p); err != nil {
			t.Fatal(err)
		}
	}
	open(1, int32(slices.Index(registryid.Menu, "minecraft:crafting")))
	open(2, int32(slices.Index(registryid.Menu, "minecraft:furnace")))
	if _, ok := m.Screens[1]; ok {
		t.Fatalf("the crafting table still open beside the furnace: %v", m.Screens)
	}
	for i := 0; i < 20; i++ { // the map's order is random: every time
		if got, ok := m.Open(); !ok || got.ID != 2 {
			t.Fatalf("Open: %v %v; want the furnace (2)", got.ID, ok)
		}
	}
}

// TestLateSlotOfClosedMenu: a chest open, the bot closes it and only then
// the server's slot for an item picked up a moment before comes, under the
// chest's id. The server counts it as sent (the inventory menu takes the
// chest's remote slots): dropped, the inventory would stay a stack behind.
func TestLateSlotOfClosedMenu(t *testing.T) {
	m := &Manager{Screens: map[int]Container{}}
	m.Screens[0] = &m.Inventory
	chest := int32(slices.Index(registryid.Menu, "minecraft:generic_9x3"))
	p := pk.Marshal(play.OpenScreen{}.PacketID(), play.OpenScreen{ContainerID: 3, Type: pk.VarInt(chest)})
	if err := m.onOpenScreen(p); err != nil {
		t.Fatal(err)
	}
	content := play.ContainerSetContent{ContainerID: 3, Items: make([]Slot, 27+36)}
	if err := m.onSetContentPacket(pk.Marshal(content.PacketID(), content)); err != nil {
		t.Fatal(err)
	}
	// the server closes it (as the bot's Close does on its side)
	cl := play.ClientboundContainerClose{ContainerID: 3}
	if err := m.onCloseScreen(pk.Marshal(cl.PacketID(), cl)); err != nil {
		t.Fatal(err)
	}
	dirt := pk.VarInt(slices.Index(registryid.Item, "minecraft:dirt"))
	// the first slot of the player's part (27): slot 9 of the inventory menu
	ss := play.ContainerSetSlot{ContainerID: 3, Slot: 27, ItemStack: func() Slot { var s Slot; s.Count, s.Item = 40, dirt; return s }()}
	if err := m.onSetSlot(pk.Marshal(ss.PacketID(), ss)); err != nil {
		t.Fatal(err)
	}
	if got := m.Inventory.Slots[9]; got.Count != 40 || got.Item != dirt {
		t.Fatalf("inventory slot 9: %v; want 40 dirt", got)
	}
	// a new menu with the same id: the old one no longer counts
	if err := m.onOpenScreen(p); err != nil {
		t.Fatal(err)
	}
	if m.closedMenu(3) != nil {
		t.Fatalf("the closed chest still kept after a new menu 3 opened")
	}
}
