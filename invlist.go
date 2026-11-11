package main

// La vista de /inventory por defecto, en texto y ordenada como el juego:
// el equipo (las manos y la armadura), la barra casilla a casilla (la de la
// mano marcada) y el inventario de arriba, con lo de cada cosa sumado. Cada
// objeto en una línea: el nombre entero, la cantidad o lo que le queda (en
// %, con color) y nada más. Estrecha (cabe en el móvil).

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mj41/go-mc26/protocol/types"
)

// listNameW: el ancho de la columna del nombre (los más largos se cortan con …).
const listNameW = 20

// fitName ajusta un nombre a w caracteres (cortado con … si no cabe).
func fitName(n string, w int) string {
	if r := []rune(n); len(r) > w {
		return string(r[:w-1]) + "…"
	}
	return fmt.Sprintf("%-*s", w, n)
}

// wearText es lo que le queda a una herramienta: "84%", verde, naranja o rojo.
func wearText(left float64) string {
	color := lipgloss.Color("#55FF55")
	switch {
	case left < 0.25:
		color = "#FF5555"
	case left < 0.5:
		color = "#FFAA00"
	}
	return lipgloss.NewStyle().Foreground(color).Render(fmt.Sprintf("%5.0f%%", left*100))
}

// amount es la columna de la derecha de una casilla: el desgaste o "×N" (nada si es 1).
func amount(s types.ItemStack, count int) string {
	if left, ok := durability(s); ok {
		return wearText(left)
	}
	if count > 1 {
		return fmt.Sprintf("%6s", fmt.Sprintf("×%d", count))
	}
	return strings.Repeat(" ", 6)
}

// renderInventoryList dibuja la vista (slots del menú 0-45; held: la casilla
// de la barra en la mano, 0-8).
func renderInventoryList(title string, slots []types.ItemStack, held int, stats string) string {
	if len(slots) < 46 {
		s := make([]types.ItemStack, 46)
		copy(s, slots)
		slots = s
	}
	heldSlot := invHotbar + held
	item := func(n string) string {
		return lipgloss.NewStyle().Foreground(itemColor(n)).Render(fitName(n, listNameW))
	}
	empty := stEmpty.Render(fitName("·", listNameW))
	line := func(label string, i int) string {
		n := shortName(slots[i])
		if n == "" {
			return label + empty
		}
		return label + item(n) + " " + amount(slots[i], int(slots[i].Count))
	}
	head := func(s string, extra string) string { return stTitle.Render(s) + stLabel.Render(extra) }

	free := 0
	for i := invMain; i < invHotbar+9; i++ {
		if slots[i].Count <= 0 {
			free++
		}
	}
	out := []string{stTitle.Render(title), stLabel.Render(stats + fmt.Sprintf(tr(" · %d free slots", " · %d huecos libres"), free))}

	// el equipo
	out = append(out, "", head(tr("Equipment", "Equipo"), ""))
	labels := []string{tr("Hand", "Mano"), tr("Off hand", "Mano izq."), tr("Helmet", "Casco"),
		tr("Chestplate", "Peto"), tr("Leggings", "Pantalón"), tr("Boots", "Botas")}
	lw := 0
	for _, l := range labels {
		lw = max(lw, len([]rune(l)))
	}
	lab := func(l string) string { return "  " + stLabel.Render(l+strings.Repeat(" ", lw-len([]rune(l))+2)) }
	for k, i := range []int{heldSlot, invOffhand, invArmor, invArmor + 1, invArmor + 2, invArmor + 3} {
		out = append(out, line(lab(labels[k]), i))
	}

	// la barra, casilla a casilla
	used := 0
	for i := 0; i < 9; i++ {
		if slots[invHotbar+i].Count > 0 {
			used++
		}
	}
	out = append(out, "", head(tr("Hotbar", "Barra"), fmt.Sprintf(" (%d/9)", used)))
	for i := 0; i < 9; i++ {
		mark := "  "
		if i == held {
			mark = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF55")).Bold(true).Render("▶ ")
		}
		out = append(out, line(mark+stLabel.Render(fmt.Sprintf("%d ", i+1)), invHotbar+i))
	}

	// el inventario de arriba: cada cosa una vez, sumada (las que se gastan, cada una)
	used = 0
	type entry struct {
		n      string
		slot   int
		count  int
		stacks int
	}
	var es []*entry
	byName := map[string]*entry{}
	for i := invMain; i < invHotbar; i++ {
		n := shortName(slots[i])
		if n == "" {
			continue
		}
		used++
		if _, wears := durability(slots[i]); !wears {
			if e := byName[n]; e != nil {
				e.count += int(slots[i].Count)
				e.stacks++
				continue
			}
		}
		e := &entry{n: n, slot: i, count: int(slots[i].Count), stacks: 1}
		byName[n] = e
		es = append(es, e)
	}
	out = append(out, "", head(tr("Inventory", "Inventario"), fmt.Sprintf(" (%d/27)", used)))
	if len(es) == 0 {
		out = append(out, "    "+stEmpty.Render(tr("empty", "vacío")))
	}
	for _, e := range es {
		l := "    " + item(e.n) + " " + amount(slots[e.slot], e.count)
		if e.stacks > 1 {
			l += stLabel.Render(fmt.Sprintf(tr("  (%d stacks)", "  (%d montones)"), e.stacks))
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}
