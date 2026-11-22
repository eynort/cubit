package automation

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"cubit/internal/i18n"
)

// /mine: minar minerales como un jugador (strip mining). Aquí, qué minerales
// hay y a qué capa se baja a por ellos.
//
// Las alturas son las de la generación de minerales de Java 1.18+ (la
// de 26.2 la conserva), de las páginas "Ore" y de cada mineral del Minecraft
// Wiki: Peak es la capa donde más hay; Lo..Hi, el rango donde aún sale en
// cantidad. Es la altura de los PIES: el túnel de 1×2 ocupa Y e Y+1.

// OreSpec es un mineral que /mine sabe buscar.
type OreSpec struct {
	Key     string   // el nombre en la orden ("diamond")
	Aliases []string // otros nombres que valen
	Blocks  []string // los bloques que se pican
	Items   []string // lo que sueltan (lo que cuenta y se deja en el cofre)
	Dim     string   // overworld o theNether
	Peak    int      // la capa donde más hay
	Lo, Hi  int      // el rango de capas donde sale en cantidad
	Weight  int      // lo difícil de encontrar (pesa más al elegir una capa común)
	Tier    int      // el pico mínimo: 0 madera/oro, 1 piedra/cobre, 2 hierro, 3 diamante, 4 netherita
	// Cap: la capa ideal está en las montañas (carbón 96, esmeralda 236): si el
	// terreno es más bajo, la mejor posible bajo la superficie
	Cap bool
	Src string // de dónde sale la altura
}

const (
	wikiOre  = "Minecraft Wiki, Ore (Java 1.18+ generation)"
	surfaceG = 8 // lo que se queda por debajo de la superficie cuando la capa ideal queda en el aire
)

// OreSpecs son los minerales conocidos.
var OreSpecs = []OreSpec{
	{Key: "diamond", Aliases: []string{"diamonds", "diamante", "diamantes"},
		Blocks: []string{"diamond_ore", "deepslate_diamond_ore"}, Items: []string{"diamond"},
		Dim: overworld, Peak: -59, Lo: -64, Hi: 16, Weight: 3, Tier: 2,
		Src: wikiOre + ": most common at y -58/-59 (range -64..16); the 2-high tunnel at -59 covers both"},
	{Key: "redstone", Aliases: []string{"piedra_roja"},
		Blocks: []string{"redstone_ore", "deepslate_redstone_ore"}, Items: []string{"redstone"},
		Dim: overworld, Peak: -58, Lo: -64, Hi: 15, Weight: 2, Tier: 2,
		Src: wikiOre + ": most common at y -58 (range -64..15)"},
	{Key: "gold", Aliases: []string{"oro"},
		Blocks: []string{"gold_ore", "deepslate_gold_ore"}, Items: []string{"raw_gold"},
		Dim: overworld, Peak: -16, Lo: -64, Hi: 32, Weight: 2, Tier: 2,
		Src: wikiOre + ": most common at y -16 (range -64..32)"},
	{Key: "lapis", Aliases: []string{"lapislazuli", "lapis_lazuli"},
		Blocks: []string{"lapis_ore", "deepslate_lapis_ore"}, Items: []string{"lapis_lazuli"},
		Dim: overworld, Peak: 0, Lo: -64, Hi: 64, Weight: 2, Tier: 1,
		Src: wikiOre + ": most common at y 0 (range -64..64)"},
	{Key: "iron", Aliases: []string{"hierro"},
		Blocks: []string{"iron_ore", "deepslate_iron_ore"}, Items: []string{"raw_iron"},
		Dim: overworld, Peak: 16, Lo: -24, Hi: 56, Weight: 1, Tier: 1,
		Src: wikiOre + ": most common at y 16 (range -24..56; also thin everywhere -64..72)"},
	{Key: "copper", Aliases: []string{"cobre"},
		Blocks: []string{"copper_ore", "deepslate_copper_ore"}, Items: []string{"raw_copper"},
		Dim: overworld, Peak: 48, Lo: -16, Hi: 112, Weight: 1, Tier: 1,
		Src: wikiOre + ": most common at y 48 (range -16..112)"},
	{Key: "coal", Aliases: []string{"carbon", "carbón"},
		Blocks: []string{"coal_ore", "deepslate_coal_ore"}, Items: []string{"coal"},
		Dim: overworld, Peak: 96, Lo: 0, Hi: 192, Weight: 1, Tier: 0, Cap: true,
		Src: wikiOre + ": most common at y 96 (range 0..192); where the ground is lower, just under the surface"},
	{Key: "emerald", Aliases: []string{"esmeralda"},
		Blocks: []string{"emerald_ore", "deepslate_emerald_ore"}, Items: []string{"emerald"},
		Dim: overworld, Peak: 236, Lo: -16, Hi: 320, Weight: 3, Tier: 2, Cap: true,
		Src: wikiOre + ": only in mountain biomes, most common around y 236 (range -16..320); with no mountain, as high as the ground allows"},
	{Key: "ancient_debris", Aliases: []string{"debris", "escombros", "netherite", "netherita"},
		Blocks: []string{"ancient_debris"}, Items: []string{"ancient_debris"},
		Dim: theNether, Peak: 15, Lo: 8, Hi: 22, Weight: 3, Tier: 3,
		Src: wikiOre + ": Nether, most common at y 15 (range 8..22), below the lava sea (y 31)"},
	{Key: "quartz", Aliases: []string{"cuarzo", "nether_quartz"},
		Blocks: []string{"nether_quartz_ore"}, Items: []string{"quartz"},
		Dim: theNether, Peak: 15, Lo: 10, Hi: 117, Weight: 1, Tier: 0,
		Src: wikiOre + ": Nether, uniform in y 10..117; y 15 shares the layer with ancient debris"},
}

// OreKeys: los nombres de /mine (para el autocompletado).
func OreKeys() []string {
	var out []string
	for _, o := range OreSpecs {
		out = append(out, o.Key)
	}
	return out
}

// ParseOres lee "diamond,iron" (sin repetir; también en español).
func ParseOres(list string) ([]OreSpec, error) {
	var out []OreSpec
	for _, w := range strings.Split(strings.ToLower(strings.TrimSpace(list)), ",") {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		found := false
		for _, o := range OreSpecs {
			if o.Key == w || slices.Contains(o.Aliases, w) || o.Key == strings.TrimSuffix(w, "s") { // (irons, emeralds)
				if !slices.ContainsFunc(out, func(x OreSpec) bool { return x.Key == o.Key }) {
					out = append(out, o)
				}
				found = true
				break
			}
		}
		if !found {
			return nil, i18n.Errorf("unknown ore %q (options: %s)", "mineral desconocido %q (hay: %s)", w, strings.Join(OreKeys(), ", "))
		}
	}
	if len(out) == 0 {
		return nil, i18n.Errorf("no ore given (options: %s)", "no hay ningún mineral (hay: %s)", strings.Join(OreKeys(), ", "))
	}
	return out, nil
}

// density es cuánto hay de este mineral en la capa y, de 0 a 1 (un triángulo
// con la punta en Peak, como lo genera el juego).
func (o OreSpec) density(y int) float64 {
	if y < o.Lo || y > o.Hi {
		return 0
	}
	hw := float64(max(o.Peak-o.Lo, o.Hi-o.Peak, 1))
	d := 1 - float64(abs(y-o.Peak))/hw
	return max(d, 0.02)
}

// ChooseLayer elige la capa (la altura de los pies) donde hacer el túnel para
// todos estos minerales: entre las capas que valen para todos (la
// intersección de sus rangos), aquella donde el que peor sale sale lo mejor
// posible (a igualdad, la que más suma, pesando más los raros). surface es la altura del suelo
// donde se empieza: los minerales de montaña (carbón, esmeralda) no pueden
// quedar por encima. Si ninguna capa vale para todos, error: se minan por
// separado. why explica la elección.
func ChooseLayer(os []OreSpec, surface int) (y int, why string, err error) {
	if len(os) == 0 {
		return 0, "", i18n.Errorf("no ores", "sin minerales")
	}
	lo, hi := -1<<30, 1<<30
	eff := make([]OreSpec, len(os))
	for i, o := range os {
		if o.Dim != os[0].Dim {
			return 0, "", &i18n.Error{EN: fmt.Sprintf("%s is from %s and %s from %s: they can't be mined together", os[0].Key, dimNameEN(os[0].Dim), o.Key, dimNameEN(o.Dim)),
				ES: fmt.Sprintf("%s es de %s y %s de %s: no se minan juntos", os[0].Key, dimName(os[0].Dim), o.Key, dimName(o.Dim))}
		}
		if o.Cap && o.Hi > surface-surfaceG { // la capa ideal queda en el aire: la mejor bajo la superficie
			o.Hi = max(surface-surfaceG, o.Lo)
			o.Peak = min(o.Peak, o.Hi)
		}
		eff[i] = o
		lo, hi = max(lo, o.Lo), min(hi, o.Hi)
	}
	if lo > hi {
		var parts []string
		for _, o := range eff {
			parts = append(parts, fmt.Sprintf("%s y %d..%d", o.Key, o.Lo, o.Hi))
		}
		return 0, "", i18n.Errorf("they share no layer (%s): mine them separately", "no comparten capa (%s): mínalos por separado", strings.Join(parts, ", "))
	}
	if os[0].Dim == overworld {
		lo = max(lo, -59) // (por debajo, el lecho de roca)
	}
	// la capa donde el que peor sale sale lo mejor posible (con varios, ninguno
	// queda a cero); a igualdad, la que más suma (según lo raro que es cada uno)
	best, bestMin, bestSum := lo, -1.0, -1.0
	for c := lo; c <= hi; c++ {
		mn, sum := 2.0, 0.0
		for _, o := range eff {
			d := o.density(c)
			mn = min(mn, d)
			sum += float64(o.Weight) * d
		}
		if mn > bestMin+1e-9 || mn > bestMin-1e-9 && sum > bestSum+1e-9 {
			best, bestMin, bestSum = c, mn, sum
		}
	}
	keys := make([]string, len(eff))
	for i, o := range eff {
		keys[i] = o.Key
	}
	sort.Strings(keys)
	if len(eff) == 1 {
		why = fmt.Sprintf("%s: layer y=%d (%s)", eff[0].Key, best, eff[0].Src)
	} else {
		why = fmt.Sprintf("%s: layers shared by all are y %d..%d; y=%d is where the scarcest of them is best (none drops to zero)", strings.Join(keys, "+"), lo, hi, best)
	}
	return best, why, nil
}

// pickTierOf: el nivel de un pico (-1: no es un pico).
func pickTierOf(name string) int {
	switch name {
	case "wooden_pickaxe", "golden_pickaxe":
		return 0
	case "stone_pickaxe", "copper_pickaxe":
		return 1
	case "iron_pickaxe":
		return 2
	case "diamond_pickaxe":
		return 3
	case "netherite_pickaxe":
		return 4
	}
	return -1
}
