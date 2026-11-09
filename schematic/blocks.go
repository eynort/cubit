package schematic

import (
	"strings"
	"sync"

	"github.com/mj41/go-mc26/data/registryid"
)

// Lo que hay que saber de cada bloque para construirlo: con qué objeto se
// pone, cuántos hacen falta, si se pone solo (la mitad de arriba de una
// puerta) y qué propiedades cuentan para darlo por bueno.

var (
	itemsOnce sync.Once
	itemSet   map[string]bool
)

// IsItem: existe el objeto (sin "minecraft:").
func IsItem(name string) bool {
	itemsOnce.Do(func() {
		itemSet = make(map[string]bool, len(registryid.Item))
		for _, it := range registryid.Item {
			itemSet[strings.TrimPrefix(it, "minecraft:")] = true
		}
	})
	return itemSet[name]
}

// skipBlocks no se ponen: aire, fluidos, portales, fuego, piezas que pone otro bloque.
var skipBlocks = map[string]bool{
	"air": true, "cave_air": true, "void_air": true, "water": true, "lava": true, "bubble_column": true,
	"fire": true, "soul_fire": true, "nether_portal": true, "end_portal": true, "end_gateway": true,
	"piston_head": true, "moving_piston": true, "frosted_ice": true, "tall_seagrass": true,
	"light": true, "barrier": true, "structure_void": true, "bedrock": true, "end_portal_frame": false,
	"spawner": true, "trial_spawner": true, "vault": true, "reinforced_deepslate": true, "budding_amethyst": true,
	"command_block": true, "chain_command_block": true, "repeating_command_block": true, "structure_block": true,
	"jigsaw": true, "petrified_oak_slab": true, "powder_snow": true, "infested_stone": true,
}

// blockItems son los bloques que se ponen con un objeto de otro nombre.
var blockItems = map[string]string{
	"redstone_wire": "redstone", "tripwire": "string", "wheat": "wheat_seeds", "carrots": "carrot",
	"potatoes": "potato", "beetroots": "beetroot_seeds", "melon_stem": "melon_seeds", "pumpkin_stem": "pumpkin_seeds",
	"attached_melon_stem": "melon_seeds", "attached_pumpkin_stem": "pumpkin_seeds", "cocoa": "cocoa_beans",
	"sweet_berry_bush": "sweet_berries", "cave_vines": "glow_berries", "cave_vines_plant": "glow_berries",
	"kelp_plant": "kelp", "bamboo_sapling": "bamboo", "weeping_vines_plant": "weeping_vines",
	"twisting_vines_plant": "twisting_vines", "big_dripleaf_stem": "big_dripleaf", "torchflower_crop": "torchflower_seeds",
	"pitcher_crop": "pitcher_pod", "water_cauldron": "cauldron", "lava_cauldron": "cauldron", "powder_snow_cauldron": "cauldron",
	"bamboo_shoot": "bamboo", "dirt_path": "", "farmland": "",
}

// Item es el objeto con el que se pone el bloque y cuántos (0: no hace falta
// ninguno, se pone solo con otro bloque; ok false: el bot no sabe ponerlo).
func Item(s State) (item string, n int, ok bool) {
	name := s.Name
	if skipBlocks[name] {
		return "", 0, false
	}
	// la otra mitad de un bloque de dos: la pone la primera
	if s.Props["half"] == "upper" || s.Props["part"] == "head" {
		return "", 0, true
	}
	if it, found := blockItems[name]; found {
		if it == "" {
			return "", 0, false
		}
		return it, 1, true
	}
	if strings.HasPrefix(name, "potted_") {
		return "flower_pot", 1, true // (la planta no: el bot no la mete)
	}
	n = 1
	switch {
	case s.Props["type"] == "double" && strings.HasSuffix(name, "_slab"):
		n = 2
	case s.Props["layers"] != "" && name == "snow":
		n = atoi(s.Props["layers"])
	case s.Props["candles"] != "":
		n = atoi(s.Props["candles"])
	case s.Props["pickles"] != "":
		n = atoi(s.Props["pickles"])
	case s.Props["eggs"] != "":
		n = atoi(s.Props["eggs"])
	case s.Props["flower_amount"] != "":
		n = atoi(s.Props["flower_amount"])
	case s.Props["segment_amount"] != "":
		n = atoi(s.Props["segment_amount"])
	}
	if n < 1 {
		n = 1
	}
	if IsItem(name) {
		return name, n, true
	}
	// los de pared: "oak_wall_sign" → "oak_sign", "wall_torch" → "torch"
	if strings.Contains(name, "wall_") {
		it := strings.Replace(name, "wall_", "", 1)
		if IsItem(it) {
			return it, n, true
		}
	}
	return "", 0, false
}

// Floor dice si el bloque es la variante de suelo de un objeto que también
// tiene variante de pared (antorcha, cartel, estandarte, cabeza, abanico de
// coral): hay que ponerlo encima de algo, no contra una pared.
func Floor(name string) bool {
	if strings.Contains(name, "wall_") {
		return false
	}
	switch {
	case strings.HasSuffix(name, "torch"), strings.HasSuffix(name, "_sign") && !strings.Contains(name, "hanging"),
		strings.HasSuffix(name, "_banner"), strings.HasSuffix(name, "_head"), strings.HasSuffix(name, "_skull"),
		strings.HasSuffix(name, "coral_fan"):
		return true
	}
	return false
}

// Wall: la variante de pared ("wall_torch", "oak_wall_sign"): va contra el bloque de detrás.
func Wall(name string) bool {
	return strings.Contains(name, "wall_") && !strings.HasSuffix(name, "_wall")
}

// Hanging: un cartel colgante de techo (no el de pared).
func Hanging(name string) bool {
	return strings.HasSuffix(name, "_hanging_sign") && !strings.Contains(name, "wall_")
}

// Falls: el bloque cae si no tiene nada debajo (arena, grava, cemento en polvo, yunques…).
func Falls(name string) bool {
	return name == "sand" || name == "red_sand" || name == "gravel" || strings.HasSuffix(name, "concrete_powder") ||
		strings.HasSuffix(name, "anvil") || name == "dragon_egg" || name == "suspicious_sand" || name == "suspicious_gravel" ||
		name == "pointed_dripstone" || name == "scaffolding"
}

// Controlled: las propiedades que el bot puede dejar bien (y que por eso se
// comparan). Las que el juego recalcula al poner el bloque o con el tiempo
// (conexiones de vallas, formas de escaleras y raíles, agua dentro, edad,
// encendido, distancia de las hojas…) no cuentan.
func Controlled(name, key, value string) bool {
	switch key {
	case "facing", "axis", "rotation", "face", "hanging":
		return true
	case "half":
		return value == "top" || value == "bottom" // (lower/upper: la puerta y las plantas altas se ponen enteras)
	case "type":
		return value == "top" || value == "bottom" || value == "double" // losas (no cofres: single/left/right)
	case "open":
		return !strings.HasPrefix(name, "iron_") // las de hierro no se abren a mano
	case "delay", "mode", "note", "inverted":
		return true
	case "powered":
		return name == "lever"
	case "layers", "candles", "pickles", "eggs", "flower_amount", "segment_amount":
		return true
	}
	return false
}

// Matches: el bloque del mundo (have) vale por el que pide el schematic
// (want): el mismo bloque y las mismas propiedades que cuentan.
func Matches(want, have State) bool {
	if want.IsAir() {
		return have.IsAir()
	}
	if want.Name != have.Name {
		return false
	}
	for k, v := range want.Props {
		if !Controlled(want.Name, k, v) {
			continue
		}
		if hv, ok := have.Props[k]; ok && hv != v {
			return false
		}
	}
	return true
}

// ParseState lee un estado como lo escribe el juego ("minecraft:oak_stairs[facing=north]").
func ParseState(text string) State {
	name, props := parseStateText(text)
	return State{Name: strings.TrimPrefix(name, "minecraft:"), Props: props}
}
