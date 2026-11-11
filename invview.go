package main

import (
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/component"
	"github.com/mj41/go-mc26/protocol/types"
)

// /inventory: el inventario del bot dibujado como la pantalla del juego —
// la armadura a la izquierda, el crafteo 2x2, las tres filas del inventario
// y la barra rápida (con la casilla en la mano resaltada) y la mano
// izquierda. Cada casilla: el objeto abreviado en dos líneas (con el color
// de su material) y abajo la cantidad o la barra de durabilidad.

const (
	slotW = 6 // ancho de dentro de una casilla
	// slots del menú del inventario
	invCraftOut = 0
	invCraft    = 1
	invArmor    = 5
	invMain     = 9
	invHotbar   = 36
	invOffhand  = 45
)

var (
	stSlot     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#555555")).Width(slotW)
	stSlotHeld = lipgloss.NewStyle().Border(lipgloss.ThickBorder()).BorderForeground(lipgloss.Color("#FFFF55")).Width(slotW)
	stEmpty    = lipgloss.NewStyle().Foreground(lipgloss.Color("#444444"))
	stCount    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true)
	stTitle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA00")).Bold(true)
	stLabel    = lipgloss.NewStyle().Foreground(lipgloss.Color("#8A8A8A"))
)

// itemColor es el color de un objeto por su material (como lo reconoce la vista).
func itemColor(name string) lipgloss.Color {
	switch {
	case strings.Contains(name, "netherite"), strings.Contains(name, "ancient_debris"):
		return "#B48EAD"
	case strings.Contains(name, "diamond"):
		return "#55FFFF"
	case strings.Contains(name, "emerald"):
		return "#55FF55"
	case strings.Contains(name, "gold"):
		return "#FFD700"
	case strings.Contains(name, "iron"), strings.Contains(name, "chainmail"), name == "shears", name == "bucket":
		return "#E8E8E8"
	case strings.Contains(name, "redstone"):
		return "#FF5555"
	case strings.Contains(name, "lapis"):
		return "#5577FF"
	case strings.Contains(name, "obsidian"), strings.Contains(name, "ender"):
		return "#9B6FD6"
	case strings.Contains(name, "_log"), strings.Contains(name, "_planks"), strings.Contains(name, "wooden"),
		strings.Contains(name, "stick"), strings.Contains(name, "_wood"), strings.Contains(name, "chest"), strings.Contains(name, "crafting"):
		return "#C69C6D"
	case strings.Contains(name, "stone"), strings.Contains(name, "deepslate"), strings.Contains(name, "andesite"),
		strings.Contains(name, "diorite"), strings.Contains(name, "granite"), strings.Contains(name, "tuff"), name == "furnace":
		return "#A0A0A0"
	case strings.Contains(name, "dirt"), strings.Contains(name, "grass"), strings.Contains(name, "netherrack"):
		return "#9C7A54"
	case strings.Contains(name, "wool"), strings.Contains(name, "_bed"):
		return "#F5F5F5"
	}
	if _, ok := foodNames[name]; ok {
		return "#FF9966"
	}
	return "#DDDDDD"
}

// foodNames: comidas (se pintan de otro color).
var foodNames = map[string]bool{
	"cooked_beef": true, "cooked_porkchop": true, "cooked_mutton": true, "cooked_chicken": true, "cooked_rabbit": true,
	"bread": true, "apple": true, "golden_apple": true, "enchanted_golden_apple": true, "carrot": true, "golden_carrot": true,
	"beef": true, "porkchop": true, "mutton": true, "chicken": true, "rabbit": true, "baked_potato": true, "potato": true,
	"cooked_cod": true, "cooked_salmon": true, "cod": true, "salmon": true, "melon_slice": true, "sweet_berries": true,
	"pumpkin_pie": true, "cookie": true, "mushroom_stew": true, "rabbit_stew": true, "beetroot": true, "rotten_flesh": true,
}

// shortName: el objeto sin "minecraft:".
func shortName(s types.ItemStack) string {
	if s.Count <= 0 || int(s.Item) < 0 || int(s.Item) >= len(registryid.Item) {
		return ""
	}
	return strings.TrimPrefix(registryid.Item[s.Item], "minecraft:")
}

// abbrev parte el nombre en dos líneas de slotW: "diamond_pickaxe" → "Diamon", "pickax".
func abbrev(name string) (string, string) {
	words := strings.Split(name, "_")
	cut := func(s string) string {
		if utf8.RuneCountInString(s) > slotW {
			return string([]rune(s)[:slotW])
		}
		return s
	}
	first := cut(strings.ToUpper(words[0][:1]) + words[0][1:])
	rest := strings.Join(words[1:], " ")
	if rest == "" && len(words[0]) > slotW { // una sola palabra larga: sigue en la segunda línea
		rest = string([]rune(words[0])[slotW:])
	}
	return first, cut(rest)
}

// durability: lo que le queda (0-1) a una herramienta o pieza; ok false si no se gasta.
func durability(s types.ItemStack) (float64, bool) {
	max := act.Component[*component.MaxDamage](s)
	if max == nil || max.Value <= 0 {
		return 0, false
	}
	dmg := 0
	if d := act.Component[*component.Damage](s); d != nil {
		dmg = int(d.Value)
	}
	return 1 - float64(dmg)/float64(max.Value), true
}

// cell dibuja una casilla (label: lo que va en una vacía, p.ej. "casco").
func cell(s types.ItemStack, held bool, label string) string {
	st := stSlot
	if held {
		st = stSlotHeld
	}
	name := shortName(s)
	if name == "" {
		return st.Render(lipgloss.JoinVertical(lipgloss.Left,
			stEmpty.Render(fmt.Sprintf("%-*s", slotW, label)), strings.Repeat(" ", slotW), strings.Repeat(" ", slotW)))
	}
	l1, l2 := abbrev(name)
	c := lipgloss.NewStyle().Foreground(itemColor(name))
	bottom := strings.Repeat(" ", slotW)
	if left, ok := durability(s); ok {
		n := int(left*slotW + 0.5)
		color := lipgloss.Color("#55FF55")
		switch {
		case left < 0.25:
			color = "#FF5555"
		case left < 0.5:
			color = "#FFAA00"
		}
		bottom = lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("▰", n)) + stEmpty.Render(strings.Repeat("▱", slotW-n))
	} else if s.Count > 1 {
		bottom = stCount.Render(fmt.Sprintf("%*d", slotW, s.Count))
	}
	return st.Render(lipgloss.JoinVertical(lipgloss.Left,
		c.Render(fmt.Sprintf("%-*s", slotW, l1)), c.Render(fmt.Sprintf("%-*s", slotW, l2)), bottom))
}

func row(slots []types.ItemStack, from, n, held int) string {
	cells := make([]string, n)
	for i := 0; i < n; i++ {
		cells[i] = cell(slots[from+i], from+i == held, "")
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cells...)
}

// renderInventory dibuja el inventario (slots del menú 0-45; held: la casilla
// de la barra en la mano, 0-8). stats es una línea de estado (vida, comida…).
func renderInventory(title string, slots []types.ItemStack, held int, stats string) string {
	if len(slots) < 46 {
		s := make([]types.ItemStack, 46)
		copy(s, slots)
		slots = s
	}
	heldSlot := invHotbar + held
	// arriba: la armadura en columna; al lado el crafteo 2x2 → resultado, y el estado
	armorLabels := []string{tr("head", "cabeza"), tr("chest", "pecho"), tr("legs", "piernas"), tr("feet", "pies")}
	var armor []string
	for i := 0; i < 4; i++ {
		armor = append(armor, cell(slots[invArmor+i], false, armorLabels[i]))
	}
	craft := lipgloss.JoinVertical(lipgloss.Left,
		row(slots, invCraft, 2, -1), row(slots, invCraft+2, 2, -1))
	crafting := lipgloss.JoinVertical(lipgloss.Left, stLabel.Render(tr("Crafting", "Crafteo")),
		lipgloss.JoinHorizontal(lipgloss.Center, craft, "  →  ", cell(slots[invCraftOut], false, "")))
	offhand := lipgloss.JoinHorizontal(lipgloss.Center, cell(slots[invOffhand], false, ""), stLabel.Render(" "+tr("off hand", "mano izquierda")))
	side := lipgloss.JoinVertical(lipgloss.Left, stats, crafting, offhand)
	top := lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.JoinVertical(lipgloss.Left, armor...), "    ", side)

	main := lipgloss.JoinVertical(lipgloss.Left,
		row(slots, invMain, 9, -1), row(slots, invMain+9, 9, -1), row(slots, invMain+18, 9, -1))
	hotbar := row(slots, invHotbar, 9, heldSlot)

	// la leyenda: lo que lleva, entero (las casillas abrevian)
	total := map[string]int{}
	for i := invArmor; i <= invOffhand; i++ {
		if n := shortName(slots[i]); n != "" {
			total[n] += int(slots[i].Count)
		}
	}
	names := make([]string, 0, len(total))
	for n := range total {
		names = append(names, n)
	}
	sort.Strings(names)
	var legend []string
	for _, n := range names {
		legend = append(legend, lipgloss.NewStyle().Foreground(itemColor(n)).Render(n)+fmt.Sprintf(" ×%d", total[n]))
	}
	inHand := shortName(slots[heldSlot])
	if inHand == "" {
		inHand = tr("nothing", "nada")
	}
	free := 0
	for i := invMain; i <= invHotbar+8; i++ {
		if slots[i].Count <= 0 {
			free++
		}
	}
	foot := stLabel.Render(fmt.Sprintf(tr("in hand: %s (slot %d) · %d free slots", "en la mano: %s (casilla %d) · %d huecos libres"), inHand, held+1, free))
	return lipgloss.JoinVertical(lipgloss.Left,
		stTitle.Render(title), top, "", main, "", hotbar, foot, wrapJoin(legend, " · ", 9*(slotW+2)))
}

// wrapJoin une las piezas con sep cortando en líneas de unos width caracteres visibles.
func wrapJoin(parts []string, sep string, width int) string {
	var lines []string
	cur, curW := "", 0
	for _, p := range parts {
		w := lipgloss.Width(p)
		if curW > 0 && curW+len(sep)+w > width {
			lines = append(lines, cur)
			cur, curW = "", 0
		}
		if curW > 0 {
			cur += sep
			curW += len(sep)
		}
		cur += p
		curW += w
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return strings.Join(lines, "\n")
}

// cmdInventory: /inventory [grid] — lo que lleva el bot: una lista por
// grupos, estrecha (cabe en el móvil); grid: la pantalla del juego.
func cmdInventory(b *Bot, args []string) error {
	s := b.Screens
	b.resyncInventory(b.ctx)
	s.Lock()
	slots := make([]types.ItemStack, 46)
	for i := 0; i < 46 && i < len(s.Inventory.Slots); i++ {
		slots[i] = s.Inventory.Slots[i]
	}
	held := s.HeldSlot
	s.Unlock()
	st := b.Player.Status()
	title := tr("Inventory of ", "Inventario de ") + botName
	if len(args) > 0 && strings.EqualFold(args[0], "grid") {
		stats := fmt.Sprintf("♥ %.0f/20   🍗 %d/20", st.Health, st.Food)
		log.Print("\n" + renderInventory(title, slots, held, stats))
		return nil
	}
	stats := fmt.Sprintf(tr("health %.0f/20 · food %d/20", "vida %.0f/20 · comida %d/20"), st.Health, st.Food)
	log.Print("\n" + renderInventoryList(title, slots, held, stats))
	return nil
}

// ansi son los códigos de color de la terminal (fuera del log en archivo).
var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansi.ReplaceAllString(s, "") }

// invCounts cuenta lo que lleva: "iron_chestplate (puesta)" para la armadura
// y el escudo de la otra mano, el resto por nombre.
func (b *Bot) invCounts() map[string]int {
	out := map[string]int{}
	if b.Screens == nil {
		return out
	}
	b.Screens.Lock()
	defer b.Screens.Unlock()
	for i := invArmor; i <= invOffhand && i < len(b.Screens.Inventory.Slots); i++ {
		s := b.Screens.Inventory.Slots[i]
		n := shortName(s)
		if n == "" {
			continue
		}
		if i < invArmor+4 {
			n += " (puesta)"
		} else if i == invOffhand {
			n += " (otra mano)"
		}
		out[n] += int(s.Count)
	}
	return out
}

// wornArmor: "iron_helmet, iron_chestplate" o "nada".
func (b *Bot) wornArmor() string {
	if b.Screens == nil {
		return "?"
	}
	b.Screens.Lock()
	defer b.Screens.Unlock()
	var worn []string
	for i := invArmor; i < invArmor+4 && i < len(b.Screens.Inventory.Slots); i++ {
		if n := shortName(b.Screens.Inventory.Slots[i]); n != "" {
			worn = append(worn, n)
		}
	}
	if len(worn) == 0 {
		return "nada"
	}
	return strings.Join(worn, ", ")
}

// traceInventory escribe en el archivo de --logs lo que entra y sale del
// inventario (lo que le tiran, lo que pierde al morir, la armadura que se pone):
// sin esto no se sabía si un regalo llegó a recogerse.
func (b *Bot) traceInventory() {
	now := b.invCounts()
	if b.lastInv == nil {
		b.lastInv = now
		session.Trace("INV al empezar: %s", fmtCounts(now, ""))
		return
	}
	diff := map[string]int{}
	for k, v := range now {
		if d := v - b.lastInv[k]; d != 0 {
			diff[k] = d
		}
	}
	for k, v := range b.lastInv {
		if _, ok := now[k]; !ok {
			diff[k] = -v
		}
	}
	if len(diff) == 0 {
		return
	}
	b.lastInv = now
	session.Trace("INV %s", fmtCounts(diff, "+"))
}

func fmtCounts(m map[string]int, sign string) string {
	if len(m) == 0 {
		return "vacío"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		if sign != "" && m[k] > 0 {
			parts[i] = fmt.Sprintf("+%d %s", m[k], k)
		} else {
			parts[i] = fmt.Sprintf("%d %s", m[k], k)
		}
	}
	return strings.Join(parts, ", ")
}
