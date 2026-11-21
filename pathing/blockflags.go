package pathing

import (
	"strings"
	"sync/atomic"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// Lo que la búsqueda pregunta de un bloque (agua, lava, fuego…), visto una
// vez por estado: antes cada pregunta comparaba el nombre del bloque con
// varios textos, y una búsqueda hace millones (en el móvil, segundos).
const (
	flagWater uint32 = 1 << iota
	flagLava
	flagFire
	flagDeadly
	flagClimbable
	flagKnown uint32 = 1 << 31
)

var stateFlags = make([]atomic.Uint32, len(block.StateList))

func flagsOf(s block.StateID) uint32 {
	if int(s) < 0 || int(s) >= len(stateFlags) {
		return 0
	}
	if f := stateFlags[s].Load(); f&flagKnown != 0 {
		return f
	}
	n := world.StateString(s)
	f := flagKnown
	if IsWaterName(n) {
		f |= flagWater
	}
	if strings.HasPrefix(n, "minecraft:lava") {
		f |= flagLava
	}
	if strings.HasPrefix(n, "minecraft:fire") || strings.HasPrefix(n, "minecraft:soul_fire") {
		f |= flagFire
	}
	for _, c := range []string{"minecraft:lava", "minecraft:fire", "minecraft:soul_fire", "minecraft:magma_block", "minecraft:cactus", "minecraft:sweet_berry_bush", "minecraft:powder_snow", "minecraft:cobweb", "minecraft:wither_rose", "minecraft:campfire", "minecraft:soul_campfire"} {
		if strings.HasPrefix(n, c) {
			f |= flagDeadly
		}
	}
	for _, c := range []string{"minecraft:ladder", "minecraft:vine", "minecraft:scaffolding", "minecraft:twisting_vines", "minecraft:weeping_vines", "minecraft:cave_vines"} {
		if strings.HasPrefix(n, c) {
			f |= flagClimbable
		}
	}
	stateFlags[s].Store(f)
	return f
}

// is dice si el bloque de p tiene la marca f (sin cargar: no).
func (g *graph) is(p world.BlockPos, f uint32) bool {
	s, ok := g.state(p)
	return ok && flagsOf(s)&f != 0
}

// IsWaterName: el estado n (world.StateString) tiene agua dentro: el agua,
// lo anegado (waterlogged=true) y las plantas de agua, que no tienen esa
// propiedad pero siempre están en el agua y la física las trata como agua (en
// los logs, de pie en un estanque con algas el camino las tomaba por suelo
// seco: saltaba como en tierra, no subía, y celda tras celda de la orilla).
func IsWaterName(n string) bool {
	if strings.HasPrefix(n, "minecraft:water") || strings.Contains(n, "waterlogged=true") {
		return true
	}
	for _, p := range waterPlants {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

var waterPlants = []string{"minecraft:seagrass", "minecraft:tall_seagrass", "minecraft:kelp", "minecraft:bubble_column"}
