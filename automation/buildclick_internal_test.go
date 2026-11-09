package automation

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"

	"minecraft-bot/schematic"
)

type gridWorld map[world.BlockPos]block.StateID

func (g gridWorld) BlockAt(p world.BlockPos) (block.StateID, bool) { return g[p], true }

func stoneState() block.StateID {
	return block.ToStateID[block.FromID["minecraft:stone"]]
}

// floorWorld: un suelo de piedra en y=63 de -8 a 8.
func floorWorld() gridWorld {
	g := gridWorld{}
	s := stoneState()
	for x := -8; x <= 8; x++ {
		for z := -8; z <= 8; z++ {
			g[world.BlockPos{X: x, Y: 63, Z: z}] = s
		}
	}
	return g
}

func eyeAt(x, y, z float64) vec3 { return vec3{x, y + 1.62, z} }

func TestPlanStairs(t *testing.T) {
	g := floorWorld()
	tp := world.BlockPos{X: 0, Y: 64, Z: 0}
	want := schematic.State{Name: "oak_stairs", Props: map[string]string{"facing": "north", "half": "bottom"}}
	n := clickNeeds{want: want, rule: ruleLook, hasRule: true, solidT: true}
	// mirando al norte hace falta estar al sur
	plan, ok := planClick(g, tp, n, eyeAt(0.5, 64, 3.5), buildReach)
	if !ok {
		t.Fatal("desde el sur debería poder")
	}
	if plan.look4 != act.North || plan.face != act.Up {
		t.Errorf("plan %+v", plan)
	}
	if _, ok := planClick(g, tp, n, eyeAt(0.5, 64, -2.5), buildReach); ok {
		t.Error("desde el norte no puede mirar al norte hacia la celda")
	}
	// la mitad de arriba: no vale pinchar el suelo (cara de arriba); sin
	// vecinos de lado ni techo no hay manera
	n.want = schematic.State{Name: "oak_stairs", Props: map[string]string{"facing": "north", "half": "top"}}
	if _, ok := planClick(g, tp, n, eyeAt(0.5, 64, 3.5), buildReach); ok {
		t.Error("una escalera de arriba encima del suelo, sin nada al lado")
	}
	// con un bloque al este: su cara oeste, por arriba de la mitad
	g[world.BlockPos{X: 1, Y: 64, Z: 0}] = stoneState()
	plan, ok = planClick(g, tp, n, eyeAt(0.2, 64, 3.5), buildReach)
	if !ok {
		t.Fatal("con un bloque al lado debería poder")
	}
	if plan.face != act.West || plan.cursor[1] <= 0.5 || plan.against != (world.BlockPos{X: 1, Y: 64, Z: 0}) {
		t.Errorf("plan %+v", plan)
	}
}

func TestPlanLogAndTorch(t *testing.T) {
	g := floorWorld()
	tp := world.BlockPos{X: 0, Y: 64, Z: 0}
	// un tronco tumbado en x: solo vale pinchar un vecino del eje x
	n := clickNeeds{want: schematic.State{Name: "oak_log", Props: map[string]string{"axis": "x"}}, solidT: true}
	if _, ok := planClick(g, tp, n, eyeAt(0.5, 64, 3.5), buildReach); ok {
		t.Error("tronco en x sin vecino en x")
	}
	g[world.BlockPos{X: -1, Y: 64, Z: 0}] = stoneState()
	plan, ok := planClick(g, tp, n, eyeAt(0.5, 64, 3.5), buildReach)
	if !ok || plan.face != act.East {
		t.Fatalf("tronco: %+v %v", plan, ok)
	}
	// antorcha de pared mirando al este: contra el bloque del oeste, su cara este
	n = clickNeeds{want: schematic.State{Name: "wall_torch", Props: map[string]string{"facing": "east"}}, rule: ruleFace, hasRule: true}
	plan, ok = planClick(g, tp, n, eyeAt(2.5, 64, 0.5), buildReach)
	if !ok || plan.face != act.East || plan.against != (world.BlockPos{X: -1, Y: 64, Z: 0}) {
		t.Fatalf("antorcha de pared: %+v %v", plan, ok)
	}
	// antorcha de suelo: solo encima
	n = clickNeeds{want: schematic.State{Name: "torch"}}
	plan, ok = planClick(g, tp, n, eyeAt(2.5, 64, 0.5), buildReach)
	if !ok || plan.face != act.Up {
		t.Fatalf("antorcha: %+v %v", plan, ok)
	}
}

func TestPlanFurnaceAndSign(t *testing.T) {
	g := floorWorld()
	tp := world.BlockPos{X: 0, Y: 64, Z: 0}
	// un horno mirando al este mira hacia el jugador: el jugador al este
	n := clickNeeds{want: schematic.State{Name: "furnace", Props: map[string]string{"facing": "east"}}, rule: ruleOpp, hasRule: true, solidT: true}
	if _, ok := planClick(g, tp, n, eyeAt(-2.5, 64, 0.5), buildReach); ok {
		t.Error("horno al este desde el oeste")
	}
	if plan, ok := planClick(g, tp, n, eyeAt(3.5, 64, 0.5), buildReach); !ok || plan.look4 != act.West {
		t.Errorf("horno desde el este: %+v %v", plan, ok)
	}
	// un cartel con rotación 0 (mira al sur): yaw del norte (180) + 8 → 0: el jugador al sur mirando al norte
	n = clickNeeds{want: schematic.State{Name: "oak_sign", Props: map[string]string{"rotation": "0"}}, rotOff: 8}
	if plan, ok := planClick(g, tp, n, eyeAt(0.5, 64, 3.5), buildReach); !ok || plan.look4 != act.North {
		t.Errorf("cartel: %+v %v", plan, ok)
	}
	if _, ok := planClick(g, tp, n, eyeAt(3.5, 64, 0.5), buildReach); ok {
		t.Error("cartel con rotación 0 desde el este")
	}
}

func TestPredictLearn(t *testing.T) {
	// lo que da cada regla mirando al norte y pinchando la cara de arriba
	if predictFacing(ruleOpp, act.North, act.North, act.Up) != act.South {
		t.Error("opp")
	}
	if predictFacing(ruleCW, act.North, act.North, act.Up) != act.East {
		t.Error("cw")
	}
	if predictFacing(ruleTrapdoor, act.North, act.North, act.East) != act.East {
		t.Error("trampilla de lado")
	}
	if predictFacing(ruleTrapdoor, act.North, act.Down, act.Up) != act.South {
		t.Error("trampilla de arriba")
	}
}
