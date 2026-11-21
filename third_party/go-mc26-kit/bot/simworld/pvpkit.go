package simworld

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26/data/entitydata"
	itemdata "github.com/mj41/go-mc26/data/item"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/component"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
	"github.com/mj41/go-mc26/wire"
)

// What a fight between players needs that mobs do not: armor (its points and toughness, CombatRules), the
// enchantments that change a blow (sharpness: 0.5 a level and 0.5; protection: 4 % a level, at most 80 %),
// the line of sight a blow needs, and an opponent that breaks a block between it and the player.

// Kit is a duelist's gear: its sword (or axe), its armor's material ("" none: leather, chainmail, iron,
// golden, diamond, netherite) and the enchantments on them.
type Kit struct {
	Weapon     string // "minecraft:iron_sword" when empty
	Sharpness  int
	Armor      string
	Protection int // on each piece
	// Still: it stands where it is (on a pillar, eating): it does not walk to the player
	Still bool
	// Eating: it stands eating a golden apple (in hand, the "using an item" flag on) and does not hit
	Eating bool
	// BreakTicks: with no sight of the player and near it, the ticks it takes to break the block between
	// (a pickaxe out, the block mined); 0: it never does
	BreakTicks int
	// PATCH (tricks.go): what it does beyond its blows
	Tricks Tricks
}

// enchants is a stack's enchantments as "name=level,…" (comparable, as a slotItem is).
func enchants(m map[string]int) string {
	var out []string
	for n, l := range m {
		if l > 0 {
			out = append(out, n+"="+strconv.Itoa(l))
		}
	}
	slices.Sort(out)
	return strings.Join(out, ",")
}

// level is the enchantment's level in the stack's list.
func level(list, name string) int {
	for _, e := range strings.Split(list, ",") {
		if n, l, ok := strings.Cut(e, "="); ok && (n == name || n == "minecraft:"+name) {
			v, _ := strconv.Atoi(l)
			return v
		}
	}
	return 0
}

// enchantComponent is the stack's enchantments as the component the client reads.
func enchantComponent(list string) component.Typed {
	var m wire.Map[pk.VarInt, *pk.VarInt, pk.VarInt, *pk.VarInt]
	for _, e := range strings.Split(list, ",") {
		n, l, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		if !strings.Contains(n, ":") {
			n = "minecraft:" + n
		}
		v, _ := strconv.Atoi(l)
		m = append(m, wire.Entry[pk.VarInt, pk.VarInt]{Key: pk.VarInt(slices.Index(fakeserver.Enchantments, n)), Val: pk.VarInt(v)})
	}
	return component.Typed{Type: pk.VarInt(slices.Index(registryid.DataComponentType, "minecraft:enchantments")),
		Value: &component.Enchantments{Enchantments: m}}
}

// GiveEnchanted puts one item with the enchantments ("sharpness": 5) in the inventory, as Give does.
func (g *Game) GiveEnchanted(item string, ench map[string]int) {
	g.T.Helper()
	g.add(item, 1)
	for i := range g.slots {
		if g.slots[i].item == item && g.slots[i].n == 1 && g.slots[i].ench == "" {
			g.slots[i].ench = enchants(ench)
			break
		}
	}
	g.broadcast()
}

// armorPoints is a piece's armor and toughness (its default attribute modifiers).
func armorPoints(item string) (armor, tough float64) {
	if item == "" {
		return
	}
	mods := itemdata.DefaultComponent[*component.AttributeModifiers](itemdata.ID(slices.Index(registryid.Item, item)))
	if mods == nil {
		return
	}
	for _, e := range mods.Modifiers {
		switch registryid.Attribute[e.Attribute] {
		case "minecraft:armor":
			armor += float64(e.Modifier.Amount)
		case "minecraft:armor_toughness":
			tough += float64(e.Modifier.Amount)
		}
	}
	return
}

// defense is what armor is worn: points, toughness and the protection levels (the enchantment's points).
type defense struct{ armor, tough, prot float64 }

// reduce is the damage after the armor and the protection (CombatRules.getDamageAfterAbsorb, then
// getDamageAfterMagicAbsorb).
func (d defense) reduce(dmg float64) float64 {
	f := math.Min(20, math.Max(d.armor/5, d.armor-4*dmg/(d.tough+8)))
	dmg *= 1 - f/25
	return dmg * (1 - math.Min(20, d.prot)/25)
}

// playerDefense is what the player wears.
func (g *Game) playerDefense() defense {
	var d defense
	for s := 5; s <= 8; s++ {
		if g.slots[s].n == 0 {
			continue
		}
		a, t := armorPoints(g.slots[s].item)
		d.armor += a
		d.tough += t
		d.prot += float64(level(g.slots[s].ench, "protection"))
	}
	return d
}

// SummonDuelistKit is SummonDuelist with its gear: the blow is the weapon's (and its sharpness), every blow
// on it goes through its armor, and it shows all of it, enchanted, as a player's gear shows.
func (g *Game) SummonDuelistKit(dx, dz float64, k Kit) error {
	if err := g.SummonNear("minecraft:player", dx, dz); err != nil {
		return err
	}
	m := g.mobs[len(g.mobs)-1]
	weapon := k.Weapon
	if weapon == "" {
		weapon = "minecraft:iron_sword"
	}
	held := slotItem{item: weapon, n: 1, ench: enchants(map[string]int{"sharpness": k.Sharpness})}
	dmg, speed := attackOf(weapon)
	if k.Sharpness > 0 {
		dmg += 0.5*float64(k.Sharpness) + 0.5
	}
	m.k.damage = float32(dmg)
	m.k.interval = int(math.Ceil(20 / speed))
	m.breakTicks = k.BreakTicks
	if k.Tricks != (Tricks{}) {
		m.tricks = &tricks{mace: k.Tricks.Mace, lava: k.Tricks.Lava, trap: k.Tricks.Trap}
	}
	if k.Still || k.Eating {
		m.k.speed = 0
	}
	if k.Eating {
		m.k.damage = 0
		held = slotItem{item: "minecraft:golden_apple", n: 1}
	}
	slots := types.SetEquipmentEntryList{{SlotID: 0, ItemStack: itemStack(held)}}
	if k.Armor != "" {
		prot := enchants(map[string]int{"protection": k.Protection})
		for i, part := range []string{"boots", "leggings", "chestplate", "helmet"} {
			item := "minecraft:" + k.Armor + "_" + part
			a, t := armorPoints(item)
			m.def.armor += a
			m.def.tough += t
			m.def.prot += float64(k.Protection)
			slots = append(slots, types.SetEquipmentEntry{SlotID: pk.Byte(2 + i), ItemStack: itemStack(slotItem{item: item, n: 1, ench: prot})})
		}
	}
	slots[0].ItemStack = itemStack(held)
	if err := g.S.Send(play.SetEquipment{Entity: pk.VarInt(m.id), Slots: slots}); err != nil {
		return err
	}
	if k.Eating { // (LivingEntity's flags: 0x01 using an item, in the main hand)
		using := pk.Byte(1)
		return g.S.Send(play.SetEntityData{ID: pk.VarInt(m.id), PackedItems: types.EntityData{
			{Index: entitydata.LivingEntityLivingEntityFlags, Serializer: 0, Value: &using}}})
	}
	return nil
}

// attackOf is an item's blow and blows a second (its default attribute modifiers on a player's 1 and 4).
func attackOf(item string) (damage, speed float64) {
	damage, speed = 1, 4
	mods := itemdata.DefaultComponent[*component.AttributeModifiers](itemdata.ID(slices.Index(registryid.Item, item)))
	if mods == nil {
		return
	}
	for _, e := range mods.Modifiers {
		if e.Slot != component.EquipmentSlotGroupMainhand && e.Slot != component.EquipmentSlotGroupAny {
			continue
		}
		switch registryid.Attribute[e.Attribute] {
		case "minecraft:attack_damage":
			damage += float64(e.Modifier.Amount)
		case "minecraft:attack_speed":
			speed += float64(e.Modifier.Amount)
		}
	}
	return
}

// seesPlayer: the mob sees some of the player's box from its eyes (a blow needs it: a player's pick ray
// stops at the first block).
func (g *Game) seesPlayer(m *mob) bool {
	eye := [3]float64{m.x, m.y + m.high*0.9, m.z}
	for _, h := range []float64{playerEye, 1.0, 0.3} {
		if g.sees(eye, [3]float64{g.pos[0], g.pos[1] + h, g.pos[2]}) {
			return true
		}
	}
	return false
}

// breakToward: a duelist that has not seen the player for its break ticks, near it, breaks the first block
// between them (from its eyes and its feet' height to the player's).
func (g *Game) breakToward(m *mob) error {
	if m.breakTicks <= 0 {
		return nil
	}
	if m.blind++; m.blind < m.breakTicks {
		return nil
	}
	m.blind = 0
	for _, h := range []float64{1.0, 0.3, playerEye} {
		a := [3]float64{m.x, m.y + h, m.z}
		b := [3]float64{g.pos[0], g.pos[1] + h, g.pos[2]}
		t, hit := g.clipBlocks(a, b)
		if !hit {
			continue
		}
		// the block met: a hair past the point along the ray
		p := [3]float64{a[0] + (b[0]-a[0])*(t+1e-4), a[1] + (b[1]-a[1])*(t+1e-4), a[2] + (b[2]-a[2])*(t+1e-4)}
		x, y, z := int(math.Floor(p[0])), int(math.Floor(p[1])), int(math.Floor(p[2]))
		g.S.World.Set(x, y, z, "minecraft:air")
		g.broken++
		return g.S.Send(play.BlockUpdate{Pos: pk.Position{X: x, Y: y, Z: z}, BlockState: 0})
	}
	return nil
}

// Broken is how many blocks duelists broke to get at the player.
func (g *Game) Broken() int { return g.broken }

// MobHits is how many blows mobs landed on the player (shields not counted).
func (g *Game) MobHits() int { return g.landed }

// SetHealth puts the player's health at h (as a fight left it) and tells it.
func (g *Game) SetHealth(h float32) error {
	before := [3]float32{g.Health, float32(g.Food), 0}
	g.Health = h
	return g.tellHealth(before)
}
