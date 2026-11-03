package pathing

import (
	"math"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// mapWorld es un mundo en memoria: piedra hasta y=63, aire encima, salvo lo puesto.
type mapWorld map[world.BlockPos]block.StateID

var stoneID, airID = block.ToStateID[block.Stone{}], block.ToStateID[block.Air{}]

func (m mapWorld) BlockAt(p world.BlockPos) (block.StateID, bool) {
	if s, ok := m[p]; ok {
		return s, true
	}
	if p.Y <= 63 {
		return stoneID, true
	}
	return airID, true
}

func (m mapWorld) fill(x1, y1, z1, x2, y2, z2 int, s block.StateID) {
	for x := x1; x <= x2; x++ {
		for y := y1; y <= y2; y++ {
			for z := z1; z <= z2; z++ {
				m[world.BlockPos{X: x, Y: y, Z: z}] = s
			}
		}
	}
}

func at(x, y, z float64) basic.Position { return basic.Position{X: x, Y: y, Z: z} }

func TestCombatHelpers(t *testing.T) {
	w := mapWorld{}
	stone, air := block.ToStateID[block.Stone{}], block.ToStateID[block.Air{}]
	lava := block.ToStateID[block.Lava{}]
	w.fill(3, 63, -1, 3, 63, 1, lava)  // lava a 2.5 bloques al este (+x)
	w.fill(-3, 61, -1, -3, 63, 1, air) // un pozo de 3 al oeste (-x)
	w.fill(-1, 64, 3, 1, 64, 3, stone) // un escalón al sur (+z)
	wk := New(w, nil, nil)
	p := at(1.5, 64, 0.5)

	if wk.Safe(p, 1, 0) {
		t.Error("hacia la lava (+x) no es seguro")
	}
	if !wk.Safe(at(0.5, 64, 0.5), 0, -1) {
		t.Error("hacia el norte, llano, es seguro")
	}
	if wk.Safe(at(-1.5, 64, 0.5), -1, 0) {
		t.Error("hacia el pozo (-x) no es seguro")
	}
	if !wk.NeedJump(at(0.5, 64, 2.0), 0, 1, 0.2) {
		t.Error("el escalón al sur pide salto")
	}
	if wk.NeedJump(at(0.5, 64, 0.5), 0, -1, 0.2) {
		t.Error("en llano no hay que saltar")
	}
	if !wk.Direct(at(0.5, 64, 0.5), 0.5, 5.5) {
		t.Error("se llega directo subiendo un escalón")
	}
	if wk.Direct(at(1.5, 64, 0.5), 5.5, 0.5) {
		t.Error("no se va directo cruzando lava")
	}
	// huir de un enemigo al este: el sitio está al oeste, lejos
	g, ok := wk.FleeGoal(at(0.5, 64, 0.5), 5, 0.5)
	if !ok || g.X > -5 {
		t.Errorf("huida: %v %v, se esperaba al oeste", g, ok)
	}
	if d := math.Hypot(float64(g.X), float64(g.Z)); d < 7 {
		t.Errorf("huida demasiado cerca: %.1f", d)
	}
}

func TestJumpHeight(t *testing.T) {
	if h := jumpHeight(0); math.Abs(h-1.2522) > 0.001 {
		t.Errorf("sin efecto: %.4f, se esperaba 1.2522", h)
	}
	if h := jumpHeight(0.2); h < 2.45 || h > 2.6 { // la wiki: ~2.5 bloques
		t.Errorf("Salto II: %.4f, se esperaba ~2.5", h)
	}
}

func TestBuildBlocks(t *testing.T) {
	for item, want := range map[string]bool{
		"minecraft:cobblestone": true, "minecraft:oak_log": false, "minecraft:stone_bricks": true,
		"minecraft:glass": true, "minecraft:white_wool": true, "minecraft:deepslate_bricks": true,
		"minecraft:diamond_block": false, "minecraft:iron_ore": false, "minecraft:sand": false,
		"minecraft:gravel": false, "minecraft:chest": false, "minecraft:crafting_table": false,
		"minecraft:oak_slab": false, "minecraft:torch": false, "minecraft:diamond_sword": false,
		"minecraft:tnt": false, "minecraft:white_concrete_powder": false,
	} {
		if got := rank(item) >= 0; got != want {
			t.Errorf("%s: para construir=%v, se esperaba %v", item, got, want)
		}
	}
}
