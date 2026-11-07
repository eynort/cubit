package automation

import (
	"fmt"
	"slices"
	"strings"
)

// Ore es la estrategia de minado de un mineral: en qué dimensión y a qué
// altura se hacen las galerías, cada cuánto (Spacing: la distancia entre dos
// galerías paralelas; 3 deja 2 bloques en medio, y como cada galería ve el
// bloque de cada lado, se ve todo), lo largas que son y cuánto se aparta para
// ir a por uno que vea. Lo que cambia de uno a otro sale de cómo los genera
// el juego (1.18+): la altura donde más hay, lo grandes que son las vetas y
// si se ven desde las cuevas (los diamantes y los escombros casi nunca tocan
// el aire: hay que pasar junto a cada bloque).
type Ore struct {
	Key    string   // el nombre en /mine ("diamond")
	Names  []string // otros nombres que valen ("diamante", "diamonds")
	What   string   // para el log ("diamantes")
	Blocks []string // los bloques que se pican
	Items  []string // lo que sale (y cuenta, y se guarda en el cofre)
	Dim    string   // overworld o theNether
	Y      int      // la altura de las galerías
	// Spacing: de una galería a la siguiente (3: no se escapa nada; más: más
	// rápido, para vetas grandes que se ven aunque queden a 2 o 3 bloques)
	Spacing int
	Branch  int  // lo largas que son las galerías
	Detour  int  // a cuánto (en horizontal) va a por uno que vea
	Pick    int  // el pico mínimo (tierWood…tierDiamond)
	Gold    bool // hace falta oro puesto (los piglins)
	Batch   int  // cada tantos vuelve al cofre (lo que lleva encima se pierde al morir)
	Why     string
}

// ores son las estrategias conocidas (para añadir un mineral: otra entrada).
var ores = []Ore{
	{Key: "diamond", Names: []string{"diamonds", "diamante", "diamantes"}, What: "diamantes",
		Blocks: []string{"diamond_ore", "deepslate_diamond_ore"}, Items: []string{"diamond", "diamond_ore", "deepslate_diamond_ore"},
		Dim: overworld, Y: diamondLevel, Spacing: 3, Branch: 48, Detour: 12, Pick: tierIron, Batch: 12,
		Why: "casi nunca tocan el aire (no se ven desde las cuevas): galerías largas cada 3 para pasar junto a cada bloque, justo por encima de los lagos de lava"},
	{Key: "redstone", Names: []string{"piedra_roja"}, What: "redstone",
		Blocks: []string{"redstone_ore", "deepslate_redstone_ore"}, Items: []string{"redstone", "redstone_ore", "deepslate_redstone_ore"},
		Dim: overworld, Y: diamondLevel, Spacing: 3, Branch: 40, Detour: 10, Pick: tierIron, Batch: 128,
		Why: "abunda abajo del todo, como los diamantes: misma capa, galerías cada 3"},
	{Key: "gold", Names: []string{"oro"}, What: "oro",
		Blocks: []string{"gold_ore", "deepslate_gold_ore"}, Items: []string{"raw_gold", "gold_ore", "deepslate_gold_ore"},
		Dim: overworld, Y: goldLevel, Spacing: 4, Branch: 40, Detour: 10, Pick: tierIron, Batch: 48,
		Why: "pico de abundancia en y=-16, vetas medianas: cada 4"},
	{Key: "lapis", Names: []string{"lapislazuli", "lapis_lazuli"}, What: "lapislázuli",
		Blocks: []string{"lapis_ore", "deepslate_lapis_ore"}, Items: []string{"lapis_lazuli", "lapis_ore", "deepslate_lapis_ore"},
		Dim: overworld, Y: 0, Spacing: 4, Branch: 40, Detour: 10, Pick: tierStone, Batch: 128,
		Why: "pico en y=0; la mitad no toca el aire: cada 4"},
	{Key: "iron", Names: []string{"hierro"}, What: "hierro",
		Blocks: []string{"iron_ore", "deepslate_iron_ore", "raw_iron_block"}, Items: []string{"raw_iron", "iron_ore", "deepslate_iron_ore", "raw_iron_block"},
		Dim: overworld, Y: ironLevel, Spacing: 5, Branch: 32, Detour: 14, Pick: tierStone, Batch: 96,
		Why: "pico en y=16, vetas grandes y muy comunes: galerías cada 5, y a por lo que se vea en las cuevas"},
	{Key: "copper", Names: []string{"cobre"}, What: "cobre",
		Blocks: []string{"copper_ore", "deepslate_copper_ore", "raw_copper_block"}, Items: []string{"raw_copper", "copper_ore", "deepslate_copper_ore", "raw_copper_block"},
		Dim: overworld, Y: 48, Spacing: 6, Branch: 32, Detour: 14, Pick: tierStone, Batch: 192,
		Why: "pico en y=48, vetas enormes: galerías cada 6"},
	{Key: "coal", Names: []string{"carbon", "carbón"}, What: "carbón",
		Blocks: []string{"coal_ore", "deepslate_coal_ore"}, Items: []string{"coal", "coal_ore", "deepslate_coal_ore"},
		Dim: overworld, Y: 44, Spacing: 6, Branch: 32, Detour: 14, Pick: tierWood, Batch: 192,
		Why: "por todas partes por encima de 0 y vetas grandes: cada 6, a por lo que se vea"},
	{Key: "debris", Names: []string{"ancient_debris", "escombros", "netherite", "netherita"}, What: "escombros ancestrales",
		Blocks: []string{"ancient_debris"}, Items: []string{"ancient_debris"},
		Dim: theNether, Y: debrisLevel, Spacing: 3, Branch: 48, Detour: 6, Pick: tierDiamond, Gold: true, Batch: 8,
		Why: "nunca tocan el aire y salen de 1 en 1 o de 2 en 2: galerías largas cada 3 en y=15, bajo el mar de lava, tapando la lava antes de abrir"},
	{Key: "quartz", Names: []string{"cuarzo", "nether_quartz"}, What: "cuarzo",
		Blocks: []string{"nether_quartz_ore"}, Items: []string{"quartz", "nether_quartz_ore"},
		Dim: theNether, Y: debrisLevel, Spacing: 5, Branch: 32, Detour: 12, Pick: tierWood, Gold: true, Batch: 192,
		Why: "por todo el Nether y en vetas grandes: cada 5, en la capa de los escombros (de paso salen)"},
}

// debrisLevel es la altura a la que más escombros ancestrales hay en el Nether
// (y por debajo del mar de lava, que está en la 31).
const debrisLevel = 15

func isDebris(n string) bool { return n == "ancient_debris" }

// OreKeys son los minerales que se pueden pedir.
func OreKeys() []string {
	var k []string
	for _, o := range ores {
		k = append(k, o.Key)
	}
	return k
}

// OreHelp es una línea por mineral: altura, patrón y por qué.
func OreHelp() []string {
	var out []string
	for _, o := range ores {
		dim := "mundo normal"
		if o.Dim == theNether {
			dim = "Nether"
		}
		out = append(out, fmt.Sprintf("%-8s %s, y=%d, galerías de %d cada %d: %s", o.Key, dim, o.Y, o.Branch, o.Spacing, o.Why))
	}
	return out
}

// ParseOres lee "diamond,iron" (o "diamante, hierro").
func ParseOres(s string) ([]Ore, error) {
	var out []Ore
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return r == ',' || r == ' ' || r == '+' }) {
		w = strings.TrimPrefix(w, "minecraft:")
		i := slices.IndexFunc(ores, func(o Ore) bool { return o.Key == w || slices.Contains(o.Names, w) })
		if i < 0 {
			return nil, fmt.Errorf("no sé minar %q (sí: %s)", w, strings.Join(OreKeys(), ", "))
		}
		if !slices.ContainsFunc(out, func(o Ore) bool { return o.Key == ores[i].Key }) {
			out = append(out, ores[i])
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("¿qué mineral? (%s)", strings.Join(OreKeys(), ", "))
	}
	return out, nil
}

// oreBlockOf: un bloque de alguno de esos minerales.
func oreBlockOf(os []Ore) func(string) bool {
	return func(n string) bool {
		for _, o := range os {
			if slices.Contains(o.Blocks, n) {
				return true
			}
		}
		return false
	}
}

// oreItemOf: lo que sale de alguno de esos minerales.
func oreItemOf(os []Ore) func(string) bool {
	return func(n string) bool {
		for _, o := range os {
			if slices.Contains(o.Items, n) {
				return true
			}
		}
		return false
	}
}

// pickTierOf es el nivel de un pico por su nombre (0 si no es un pico).
func pickTierOf(name string) int {
	for i, t := range tiers {
		if name == t+"_pickaxe" {
			return len(tiers) - i
		}
	}
	return 0
}

// OreBrief es una línea corta por mineral (para el autocompletado):
// "mundo normal, y=-53, galerías cada 3".
func OreBrief() map[string]string {
	out := map[string]string{}
	for _, o := range ores {
		dim := "mundo normal"
		if o.Dim == theNether {
			dim = "Nether"
		}
		out[o.Key] = fmt.Sprintf("%s: %s, y=%d, galerías cada %d", o.What, dim, o.Y, o.Spacing)
	}
	return out
}
