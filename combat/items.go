package combat

import (
	"context"
	"math"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/component"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/types"
)

// Índices del menú del inventario del jugador (InventoryMenu).
const (
	slotArmorHead = 5  // 5 cabeza, 6 pecho, 7 piernas, 8 pies
	slotMainStart = 9  // 9..35 inventario principal
	slotHotbar    = 36 // 36..44 barra rápida
	slotOffhand   = 45
	offhandButton = 40 // botón de un click "swap" que intercambia con la mano izquierda
)

func itemName(id int32) string {
	if id >= 0 && int(id) < len(registryid.Item) {
		return registryid.Item[id]
	}
	return ""
}

func nameOf(s screen.Slot) string {
	if s.Count <= 0 {
		return ""
	}
	return itemName(int32(s.Item))
}

func attrName(id pk.VarInt) string {
	if id >= 0 && int(id) < len(registryid.Attribute) {
		return registryid.Attribute[id]
	}
	return ""
}

// weapon es lo que hace un objeto en la mano: daño de un golpe cargado y golpes por segundo.
type weapon struct {
	damage, speed float64
}

func weaponOf(s screen.Slot) weapon {
	w := weapon{damage: 1, speed: 4} // el puño
	if s.Count <= 0 {
		return w
	}
	if mods := act.Component[*component.AttributeModifiers](s); mods != nil {
		for _, m := range mods.Modifiers {
			switch m.Slot {
			case component.EquipmentSlotGroupAny, component.EquipmentSlotGroupMainhand, component.EquipmentSlotGroupHand:
			default:
				continue
			}
			switch attrName(m.Attribute) {
			case "minecraft:attack_damage":
				w.damage += float64(m.Modifier.Amount)
			case "minecraft:attack_speed":
				w.speed += float64(m.Modifier.Amount)
			}
		}
	}
	w.speed = math.Max(w.speed, 0.1)
	return w
}

func isAxe(name string) bool { return strings.HasSuffix(name, "_axe") }

// armorOf suma armadura y dureza de lo que lleva puesto (equipo de una entidad).
func armorOf(equipment map[int]types.ItemStack) (armor, toughness float64) {
	for slot, s := range equipment {
		if slot < 2 || slot > 5 { // 2 pies, 3 piernas, 4 pecho, 5 cabeza
			continue
		}
		a, t := armorPiece(s)
		armor += a
		toughness += t
	}
	return
}

func armorPiece(s screen.Slot) (armor, toughness float64) {
	if s.Count <= 0 {
		return
	}
	mods := act.Component[*component.AttributeModifiers](s)
	if mods == nil {
		return
	}
	for _, m := range mods.Modifiers {
		switch m.Slot {
		case component.EquipmentSlotGroupMainhand, component.EquipmentSlotGroupOffhand, component.EquipmentSlotGroupHand:
			continue
		}
		switch attrName(m.Attribute) {
		case "minecraft:armor":
			armor += float64(m.Modifier.Amount)
		case "minecraft:armor_toughness":
			toughness += float64(m.Modifier.Amount)
		}
	}
	return
}

// afterArmor es el daño que llega tras la armadura (CombatRules.getDamageAfterAbsorb).
func afterArmor(dmg, armor, toughness float64) float64 {
	f := math.Min(20, math.Max(armor/5, armor-4*dmg/(toughness+8)))
	return dmg * (1 - f/25)
}

// food describe una comida del inventario.
type food struct {
	slot       int
	nutrition  int
	saturation float64
	golden     bool // manzana dorada (absorción + regeneración)
}

// badFood son comidas con efectos malos o raros: nunca en combate.
var badFood = map[string]bool{
	"minecraft:rotten_flesh": true, "minecraft:spider_eye": true, "minecraft:poisonous_potato": true,
	"minecraft:pufferfish": true, "minecraft:suspicious_stew": true, "minecraft:chorus_fruit": true,
}

func foodOf(slot int, s screen.Slot) (food, bool) {
	name := nameOf(s)
	if name == "" || badFood[name] {
		return food{}, false
	}
	f := act.Component[*component.Food](s)
	if f == nil {
		return food{}, false
	}
	return food{
		slot:       slot,
		nutrition:  int(f.Nutrition),
		saturation: float64(f.Saturation),
		golden:     name == "minecraft:golden_apple" || name == "minecraft:enchanted_golden_apple",
	}, true
}

// inventory copia los slots del inventario y el slot de la barra en la mano.
func (f *Fighter) inventory() ([slotOffhand + 1]screen.Slot, int) {
	f.d.Screens.Lock()
	defer f.d.Screens.Unlock()
	return f.d.Screens.Inventory.Slots, f.d.Screens.HeldSlot
}

// bestWeapon elige el arma que mata antes a un objetivo con hp vida y esa armadura.
// Con axeOnly (el objetivo bloquea con escudo) solo cuenta hachas.
func (f *Fighter) bestWeapon(hp, armor, toughness float64, axeOnly bool) (slot int, w weapon, ttk float64) {
	inv, held := f.inventory()
	slot, ttk = slotHotbar+held, math.Inf(1)
	for i := slotMainStart; i <= slotHotbar+8; i++ {
		s := inv[i]
		if axeOnly && !isAxe(nameOf(s)) {
			continue
		}
		cw := weaponOf(s)
		t := timeToKill(hp, afterArmor(cw.damage*critFactor, armor, toughness), cw.speed)
		if i != slotHotbar+held && cw.speed > 0 {
			// cambiar de arma reinicia la carga: el primer golpe espera una
			// recarga entera (sin esto, contra el mismo zombi: espada, hacha,
			// espada… y cada cambio, un golpe perdido)
			t += 1 / cw.speed
		}
		// en la mano gana el empate: cambiar cuesta tiempo
		if t < ttk-1e-6 || (math.Abs(t-ttk) < 1e-6 && i == slotHotbar+held) {
			slot, w, ttk = i, cw, t
		}
	}
	if math.IsInf(ttk, 1) { // sin hacha: lo que haya en la mano
		w = weaponOf(inv[slot])
		ttk = timeToKill(hp, afterArmor(w.damage*critFactor, armor, toughness), w.speed)
	}
	return
}

// timeToKill son los segundos para quitar hp vida con golpes de hit, speed golpes por segundo.
func timeToKill(hp, hit, speed float64) float64 {
	if hit <= 0 {
		return math.Inf(1)
	}
	return (math.Ceil(hp/hit) - 1) / speed
}

// foods lista las comidas que lleva.
func (f *Fighter) foods() []food {
	inv, _ := f.inventory()
	var out []food
	for i := slotMainStart; i <= slotOffhand; i++ {
		if fd, ok := foodOf(i, inv[i]); ok {
			out = append(out, fd)
		}
	}
	return out
}

// pickFood elige qué comer: con golden la manzana dorada (mejor la encantada);
// si no, la de más saturación que no se desperdicie mucho.
func pickFood(foods []food, hunger int, golden bool) (food, bool) {
	best, found := food{}, false
	score := func(fd food) float64 {
		waste := math.Max(0, float64(fd.nutrition-hunger))
		return fd.saturation + float64(fd.nutrition) - 1.5*waste
	}
	for _, fd := range foods {
		if fd.golden != golden {
			continue
		}
		if !found || score(fd) > score(best) {
			best, found = fd, true
		}
	}
	return best, found
}

// hold pone en la mano el objeto del slot i del menú.
func (f *Fighter) hold(ctx context.Context, i int) error {
	if i >= slotHotbar && i <= slotHotbar+8 {
		return f.selectHotbar(i - slotHotbar)
	}
	h := f.spareHotbar()
	if err := f.d.Screens.Click(0, i, h, types.ContainerInputSwap); err != nil {
		return err
	}
	if err := f.d.Ctl.WaitTicks(ctx, 2); err != nil {
		return err
	}
	return f.selectHotbar(h)
}

func (f *Fighter) selectHotbar(h int) error {
	_, held := f.inventory()
	if held == h {
		return nil
	}
	f.attackReset.Store(true) // cambiar de objeto reinicia la carga del ataque
	return f.d.Screens.SelectHotbar(h)
}

// spareHotbar es un slot de la barra que se puede ocupar: uno vacío, o uno
// sin arma, comida, tótem ni escudo (la 8, la del constructor, la última); la 8 si no hay otro.
func (f *Fighter) spareHotbar() int {
	inv, held := f.inventory()
	for h := 0; h < 9; h++ {
		if inv[slotHotbar+h].Count <= 0 {
			return h
		}
	}
	// la 8, la última: el constructor del pathfinding pone ahí sus bloques
	// (si no, se la quitan el uno al otro: la comida sale y entra la tierra)
	for _, h := range []int{7, 6, 5, 4, 3, 2, 1, 0, 8} {
		s := inv[slotHotbar+h]
		n := nameOf(s)
		if h == held || strings.HasSuffix(n, "_sword") || isAxe(n) || n == "minecraft:totem_of_undying" || n == "minecraft:shield" {
			continue
		}
		if _, ok := foodOf(0, s); ok {
			continue
		}
		return h
	}
	return 8
}

// armorUpgrade es una pieza del inventario mejor que la puesta.
type armorUpgrade struct {
	from, to int // slots del menú: la pieza nueva y donde va
	name     string
}

// armorUpgrades lista las piezas que mejoran la armadura puesta (una por hueco).
func (f *Fighter) armorUpgrades() []armorUpgrade {
	// slot del menú de cada pieza, por EquipmentSlot del componente equippable
	menuSlot := map[component.EquipmentSlot]int{
		component.EquipmentSlotHead: slotArmorHead, component.EquipmentSlotChest: slotArmorHead + 1,
		component.EquipmentSlotLegs: slotArmorHead + 2, component.EquipmentSlotFeet: slotArmorHead + 3,
	}
	score := func(s screen.Slot) float64 { a, t := armorPiece(s); return a + t/2 }
	inv, _ := f.inventory()
	var out []armorUpgrade
	keepGold := f.d.KeepGold != nil && f.d.KeepGold()
	for eq, ms := range menuSlot {
		if keepGold && strings.HasPrefix(nameOf(inv[ms]), "minecraft:golden_") {
			continue // el oro se queda puesto
		}
		if nameOf(inv[ms]) == "minecraft:elytra" {
			// unos élitros puestos son para volar (/elytra wear, o un vuelo de
			// /goto): no "mejoran" por un peto (en los logs, se los quitaba al
			// momento de ponérselos). El vuelo se vuelve a poner el peto al aterrizar.
			continue
		}
		best, bestScore := -1, score(inv[ms])
		for i := slotMainStart; i <= slotHotbar+8; i++ {
			s := inv[i]
			e := act.Component[*component.Equippable](s)
			if e == nil || e.Slot != eq {
				continue
			}
			if sc := score(s); sc > bestScore+1e-6 {
				best, bestScore = i, sc
			}
		}
		if best >= 0 {
			out = append(out, armorUpgrade{from: best, to: ms, name: nameOf(inv[best])})
		}
	}
	return out
}

// equipArmor se pone la mejor armadura que lleve en el inventario.
func (f *Fighter) equipArmor(ctx context.Context) {
	for _, u := range f.armorUpgrades() {
		inv, _ := f.inventory()
		var err error
		if inv[u.to].Count <= 0 {
			err = f.d.Screens.Click(0, u.from, 0, types.ContainerInputQuickMove) // shift-click la equipa
		} else { // tomar la nueva, cambiarla por la puesta y dejar la vieja donde estaba la nueva
			for _, s := range []int{u.from, u.to, u.from} {
				if err = f.d.Screens.Click(0, s, 0, types.ContainerInputPickup); err != nil {
					break
				}
				_ = f.d.Ctl.WaitTicks(ctx, 1)
			}
		}
		if err != nil {
			f.logf("armadura: %v", err)
			return
		}
		f.logf("armadura: me pongo %s", strings.TrimPrefix(u.name, "minecraft:"))
		_ = f.d.Ctl.WaitTicks(ctx, 2)
	}
}

// fingerprint resume el inventario: cambia cuando recoge, pierde o rompe algo.
func (f *Fighter) fingerprint() uint64 {
	inv, _ := f.inventory()
	var h uint64 = 14695981039346656037
	for i, s := range inv {
		for _, v := range []uint64{uint64(i), uint64(s.Item), uint64(s.Count)} {
			h ^= v
			h *= 1099511628211
		}
	}
	return h
}

// HasFood: lleva algo de comer.
func (f *Fighter) HasFood() bool { return len(f.foods()) > 0 }

// isWeapon: un objeto hecho para pegar (no una herramienta que también pega).
func isWeapon(name string) bool {
	return strings.HasSuffix(name, "_sword") || isAxe(name) || strings.HasSuffix(name, "_spear") ||
		name == "minecraft:trident" || name == "minecraft:mace"
}

// bestArm es el slot del menú de la mejor arma que lleva, contra un enemigo
// cualquiera (20 de vida, sin armadura); a igualdad, la de la barra rápida y
// mejor la de la mano. ok es false si no lleva ninguna.
func bestArm(inv [slotOffhand + 1]screen.Slot, held int) (slot int, ok bool) {
	best := math.Inf(1)
	for i := slotMainStart; i <= slotHotbar+8; i++ {
		if !isWeapon(nameOf(inv[i])) {
			continue
		}
		w := weaponOf(inv[i])
		t := timeToKill(20, w.damage*critFactor, w.speed)
		bonus := 0.0
		if i >= slotHotbar {
			bonus = 1e-3
		}
		if i == slotHotbar+held {
			bonus = 2e-3
		}
		if t-bonus < best {
			slot, best, ok = i, t-bonus, true
		}
	}
	return
}

// keepArmed tiene siempre la mejor arma a mano: en la barra rápida (sacarla del
// inventario en plena pelea cuesta golpes) y, si el bot está libre, en la mano.
func (f *Fighter) keepArmed(ctx context.Context) {
	if f.lastArm != 0 && f.steps-f.lastArm < 40 { // cada 2 s
		return
	}
	f.lastArm = f.steps
	inv, held := f.inventory()
	slot, ok := bestArm(inv, held)
	if !ok {
		return
	}
	idle := f.d.Idle != nil && f.d.Idle()
	if slot < slotHotbar {
		if f.d.Walker.Building() {
			return // no mover la barra mientras pone bloques
		}
		h := f.spareHotbar()
		if h == held && !idle {
			return // la barra está llena y la mano la usa una tarea: luego
		}
		if err := f.d.Screens.Click(0, slot, h, types.ContainerInputSwap); err != nil {
			f.logf("arma: %v", err)
			return
		}
		f.logf("arma: %s a la barra rápida", strings.TrimPrefix(nameOf(inv[slot]), "minecraft:"))
		_ = f.d.Ctl.WaitTicks(ctx, 2)
		slot = slotHotbar + h
	}
	if idle && slot != slotHotbar+held && !f.d.Walker.Building() { // (en una torre la mano es del constructor)
		if err := f.selectHotbar(slot - slotHotbar); err != nil {
			f.logf("arma: %v", err)
		}
	}
}
