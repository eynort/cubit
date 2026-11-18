package automation_test

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
	"github.com/mj41/go-mc26/level/component"

	"minecraft-bot/automation"
)

// TestExcavate: una caja de 5×5×5 en el suelo (hierba, tierra y piedra). El
// bot no lleva pico: coge el de hierro del cofre de minar, vacía la caja de
// arriba abajo sin cavar bajo sus pies; no usa cofres (el de al lado se
// queda vacío).
func TestExcavate(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
		}})
		r, _ := safeRunner(t, g)
		g.Give("minecraft:iron_pickaxe", 1)
		g.Step(5)
		box := automation.NewBox(world.BlockPos{X: 3, Y: 63, Z: 3}, world.BlockPos{X: 7, Y: 59, Z: 7})
		var err error
		r.Start(context.Background(), "excavar", func(j *automation.Job) error {
			err = automation.Excavate(box)(j)
			return err
		})
		g.Until(1500*20, func() bool { return r.Status() == "" })
		left, cols := 0, map[[2]int]bool{}
		for x := 3; x <= 7; x++ {
			for y := 59; y <= 63; y++ {
				for z := 3; z <= 7; z++ {
					if !block.IsAir(g.S.World.At(x, y, z)) {
						left++
						cols[[2]int{x, z}] = true
						t.Logf("queda %s en %d %d %d", block.StateList[g.S.World.At(x, y, z)].ID(), x, y, z)
					}
				}
			}
		}
		p := g.B.Player.Position()
		t.Logf("error %v; el bot en %.1f %.1f %.1f; quedan %d; en el cofre: dirt %d, cobblestone %d; lleva cobblestone %d, dirt %d, pico %d",
			err, p.X, p.Y, p.Z, left, g.ChestCount(-3, 64, 0, "minecraft:dirt"), g.ChestCount(-3, 64, 0, "minecraft:cobblestone"),
			g.Count("minecraft:cobblestone"), g.Count("minecraft:dirt"), g.Count("minecraft:iron_pickaxe"))
		if err != nil {
			t.Fatalf("excavar: %v", err)
		}
		// (de un hoyo se sale con una torre: lo que de ella no se alcanza desde el borde se queda)
		// (a veces el camino tantea una torre y sale por la de al lado: dos columnas vecinas)
		if left > 4 || len(cols) > 2 {
			t.Errorf("quedan %d bloques en %d columnas: solo debería quedar parte de la torre para salir", left, len(cols))
		}
		if g.Count("minecraft:iron_pickaxe") != 1 {
			t.Errorf("rompió el pico")
		}
		if n := g.ChestCount(-3, 64, 0, "minecraft:dirt") + g.ChestCount(-3, 64, 0, "minecraft:cobblestone"); n != 0 {
			t.Errorf("sin cofres: no tenía que guardar nada, y en el cofre hay %d", n)
		}
	})
}

// TestExcavateRoundTrip: una caja de 7×8×7 con el inventario casi lleno
// (antorchas: se quedan): a mitad se llena, tira la basura y acaba sin
// tocar las antorchas.
func TestExcavateRoundTrip(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"give Tester torch 1664", "give Tester diamond_pickaxe 1",
		}})
		r, _ := safeRunner(t, g)
		g.Step(5)
		box := automation.NewBox(world.BlockPos{X: 3, Y: 63, Z: 3}, world.BlockPos{X: 9, Y: 56, Z: 9})
		var err error
		r.Start(context.Background(), "excavar", func(j *automation.Job) error {
			err = automation.Excavate(box)(j)
			return err
		})
		g.Until(4000*20, func() bool { return r.Status() == "" })
		left := 0
		for x := 3; x <= 9; x++ {
			for y := 56; y <= 63; y++ {
				for z := 3; z <= 9; z++ {
					if !block.IsAir(g.S.World.At(x, y, z)) {
						left++
					}
				}
			}
		}
		stored := g.ChestCount(-3, 64, 0, "minecraft:dirt") + g.ChestCount(-3, 64, 0, "minecraft:cobblestone")
		t.Logf("error %v; quedan %d; en el cofre %d; antorchas %d; libres %d", err, left, stored, g.Count("minecraft:torch"), g.Count("minecraft:dirt"))
		if err != nil {
			t.Fatalf("excavar: %v", err)
		}
		if left > 10 { // (la torre por la que sale a guardar desde el fondo de una pasada, y la del final)
			t.Errorf("quedan %d bloques en la caja", left)
		}
		if stored != 0 {
			t.Errorf("sin cofres: no tenía que guardar nada, y en el cofre hay %d", stored)
		}
		if g.Count("minecraft:torch") != 1664 {
			t.Errorf("las antorchas no se guardan: lleva %d", g.Count("minecraft:torch"))
		}
	})
}

// usesLeft: los usos que le quedan a la herramienta item que lleva el bot (-1 si no la lleva).
func usesLeft(g *simworld.Game, item string) int {
	s := g.B.Screens
	s.Lock()
	defer s.Unlock()
	for _, sl := range s.Inventory.Slots {
		if sl.Count > 0 && itemName(int32(sl.Item)) == "minecraft:"+item {
			m := act.Component[*component.MaxDamage](sl)
			d := act.Component[*component.Damage](sl)
			if m == nil {
				return -1
			}
			if d == nil {
				return int(m.Value)
			}
			return int(m.Value) - int(d.Value)
		}
	}
	return -1
}

// TestExcavateTools: tierra con un tronco dentro. Con pala y hacha, la
// tierra con la pala y el tronco con el hacha; sin ellas, con la mano: el
// pico (que la tierra y la madera también gastan) no pierde ni un uso.
func TestExcavateTools(t *testing.T) {
	for _, withTools := range []bool{true, false} {
		t.Run(map[bool]string{true: "pala y hacha", false: "solo pico"}[withTools], func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				setup := []string{
					"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
					"setblock 4 62 4 minecraft:oak_log",
					"give Tester iron_pickaxe 1",
				}
				if withTools {
					setup = append(setup, "give Tester stone_shovel 1", "give Tester stone_axe 1")
				}
				g := simworld.Start(t, simworld.Options{Setup: setup})
				r, _ := safeRunner(t, g)
				g.Step(5)
				pick := usesLeft(g, "iron_pickaxe")
				box := automation.NewBox(world.BlockPos{X: 3, Y: 63, Z: 3}, world.BlockPos{X: 5, Y: 61, Z: 5})
				var err error
				r.Start(context.Background(), "excavar", func(j *automation.Job) error {
					err = automation.Excavate(box)(j)
					return err
				})
				g.Until(1500*20, func() bool { return r.Status() == "" })
				shovel, axe := usesLeft(g, "stone_shovel"), usesLeft(g, "stone_axe")
				t.Logf("error %v; pico %d → %d; pala %d; hacha %d", err, pick, usesLeft(g, "iron_pickaxe"), shovel, axe)
				if err != nil {
					t.Fatal(err)
				}
				if usesLeft(g, "iron_pickaxe") != pick {
					t.Errorf("el pico se gastó en tierra o madera: %d → %d", pick, usesLeft(g, "iron_pickaxe"))
				}
				if withTools && (shovel >= 131 || axe != 130) {
					t.Errorf("la tierra va con pala y el tronco con hacha: pala %d usos, hacha %d", shovel, axe)
				}
			})
		})
	}
}

// TestExcavateHaste: netherrack con pico de diamante. Con Haste V se rompe al
// instante: el barrido va corriendo (filas de 1) y tarda bastante menos que
// sin Haste (andando, filas de 3). Los dos lo vacían todo.
func TestExcavateHaste(t *testing.T) {
	ticks := map[bool]int{}
	for _, haste := range []bool{false, true} {
		t.Run(map[bool]string{true: "con Haste V", false: "sin Haste"}[haste], func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				setup := []string{
					"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
					"fill 3 60 3 14 63 5 minecraft:netherrack",
					"give Tester diamond_pickaxe 1",
				}
				if haste {
					setup = append(setup, "effect give Tester haste 5")
				}
				g := simworld.Start(t, simworld.Options{Setup: setup})
				r, _ := safeRunner(t, g)
				g.Step(5)
				box := automation.NewBox(world.BlockPos{X: 3, Y: 63, Z: 3}, world.BlockPos{X: 14, Y: 60, Z: 5})
				var err error
				start := g.Tick
				r.Start(context.Background(), "excavar", func(j *automation.Job) error {
					err = automation.Excavate(box)(j)
					return err
				})
				g.Until(3000*20, func() bool { return r.Status() == "" })
				ticks[haste] = int(g.Tick - start)
				left := 0
				for x := 3; x <= 14; x++ {
					for y := 60; y <= 63; y++ {
						for z := 3; z <= 5; z++ {
							if !block.IsAir(g.S.World.At(x, y, z)) {
								left++
							}
						}
					}
				}
				t.Logf("error %v; quedan %d; %d ticks", err, left, ticks[haste])
				if err != nil {
					t.Fatal(err)
				}
				if left > 4 {
					t.Errorf("quedan %d bloques", left)
				}
			})
		})
	}
	if ticks[true] == 0 || ticks[true]*10 > ticks[false]*8 {
		t.Errorf("con Haste (al instante, corriendo) tendría que tardar mucho menos: %d ticks contra %d", ticks[true], ticks[false])
	}
}

// TestExcavateHill: la caja llega muy por encima del suelo (como en el
// servidor: y 61 a 75) y dentro hay una colina pequeña. Las capas de arriba
// son casi todo aire: no hay que barrerlas (sería meterse en el aire, con
// torres), sino cavar la colina bloque a bloque; el suelo de abajo, barrido.
func TestExcavateHill(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"fill 6 64 6 8 66 8 minecraft:dirt", // la colina: 3×3×3
			"setblock 7 67 7 minecraft:mycelium",
			"give Tester diamond_pickaxe 1", "give Tester cobblestone 64",
		}})
		r, _ := safeRunner(t, g)
		g.Step(5)
		box := automation.NewBox(world.BlockPos{X: 3, Y: 75, Z: 3}, world.BlockPos{X: 11, Y: 61, Z: 11})
		var err error
		start := g.Tick
		r.Start(context.Background(), "excavar", func(j *automation.Job) error {
			err = automation.Excavate(box)(j)
			return err
		})
		finished := g.Until(3000*20, func() bool { return r.Status() == "" })
		left := 0
		for x := 3; x <= 11; x++ {
			for y := 61; y <= 75; y++ {
				for z := 3; z <= 11; z++ {
					if !block.IsAir(g.S.World.At(x, y, z)) {
						left++
					}
				}
			}
		}
		t.Logf("error %v; quedan %d; %d ticks", err, left, g.Tick-start)
		if !finished {
			t.Fatalf("no terminó: %s", r.Status())
		}
		if err != nil {
			t.Fatal(err)
		}
		if left > 6 {
			t.Errorf("quedan %d bloques", left)
		}
	})
}

// TestDigLostHands: mientras el bot pica un tronco a mano (3 s), otra cosa
// usa las manos (aquí, otra picada, como haría la seguridad del fuego o la
// pelea). Dig tiene que darse cuenta enseguida, no esperar al plazo de 30 s
// con el bot quieto (como pasó en el servidor: dos veces 30 s parado).
func TestDigLostHands(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock 1 64 0 minecraft:oak_log", "setblock 0 64 1 minecraft:dirt",
		}})
		r, _ := safeRunner(t, g)
		g.Step(5)
		var err error
		finished := false
		r.Start(context.Background(), "cavar", func(j *automation.Job) error {
			err = j.Dig(world.BlockPos{X: 1, Y: 64, Z: 0})
			finished = true
			return nil
		})
		g.Step(10)
		g.B.Hands.Dig(world.BlockPos{X: 0, Y: 64, Z: 1}, nil) // le quitan las manos
		start := g.Tick
		g.Until(40*20, func() bool { return finished })
		t.Logf("Dig: %v, %d ticks después de perder las manos", err, g.Tick-start)
		if !finished || g.Tick-start > 20 {
			t.Fatalf("Dig siguió esperando con las manos en otra cosa: %d ticks", g.Tick-start)
		}
		if err == nil {
			t.Errorf("Dig dijo que rompió el tronco, pero las manos estaban en otra cosa")
		}
	})
}

// TestExcavateFast: lo de un jugador con buen equipo — pico y pala de diamante
// con Efficiency V y Haste II: la piedra y la tierra se rompen al instante.
// Una caja de 16×8×10 (1280 bloques, 4 capas de tierra y 4 de piedra): tiene
// que ir corriendo y quedar vacía, a buen ritmo (bloques por segundo).
func TestExcavateFast(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"fill 3 59 3 18 60 12 minecraft:stone", // (el suelo plano: piedra hasta 60, tierra 61-62, hierba 63)
			"effect give Tester haste 2",
		}})
		g.GiveEnchanted("minecraft:diamond_pickaxe", map[string]int{"efficiency": 5})
		g.GiveEnchanted("minecraft:diamond_shovel", map[string]int{"efficiency": 5})
		r, _ := safeRunner(t, g)
		g.Step(5)
		box := automation.NewBox(world.BlockPos{X: 3, Y: 63, Z: 3}, world.BlockPos{X: 18, Y: 56, Z: 12})
		var err error
		start := g.Tick
		r.Start(context.Background(), "excavar", func(j *automation.Job) error {
			err = automation.Excavate(box)(j)
			return err
		})
		busy, moving, idle := 0, 0, 0
		idleBy, movingBy := map[string]int{}, map[string]int{}
		var lastPos [3]float64
		g.Until(6000*20, func() bool {
			p := g.B.Player.Position()
			moved := math.Abs(p.X-lastPos[0])+math.Abs(p.Y-lastPos[1])+math.Abs(p.Z-lastPos[2]) > 0.01
			lastPos = [3]float64{p.X, p.Y, p.Z}
			switch {
			case g.B.Hands.Busy():
				busy++
			case moved:
				moving++
				st := r.Status()
				if i := strings.Index(st, "("); i > 0 {
					st = st[:i]
				}
				movingBy[st]++
			default:
				idle++
				st := r.Status()
				if i := strings.Index(st, "("); i > 0 {
					st = st[:i]
				}
				idleBy[st]++
			}
			return r.Status() == ""
		})
		ticks := g.Tick - start
		t.Logf("ticks: picando %d, moviéndose %d, quieto sin picar %d", busy, moving, idle)
		for st, n := range idleBy {
			if n > 20 {
				t.Logf("  quieto %4d ticks en: %s", n, st)
			}
		}
		for st, n := range movingBy {
			if n > 40 {
				t.Logf("  moviéndose %4d ticks en: %s", n, st)
			}
		}
		left := 0
		for x := 3; x <= 18; x++ {
			for y := 56; y <= 63; y++ {
				for z := 3; z <= 12; z++ {
					if !block.IsAir(g.S.World.At(x, y, z)) {
						left++
					}
				}
			}
		}
		dug := box.Volume() - left
		t.Logf("error %v; quedan %d; %d bloques en %d ticks (%.1f s): %.1f bloques/s", err, left, dug, ticks, float64(ticks)/20, float64(dug)*20/float64(ticks))
		if err != nil {
			t.Fatal(err)
		}
		if left > 8 {
			t.Errorf("quedan %d bloques", left)
		}
	})
}

// TestExcavateOldProtected: una zona apuntada como protegida antes (un "no
// tienes permiso" a 16 bloques, de otra vez) tapa toda la caja. Como la caja
// la elige el jugador, se excava igual (antes: "terminado", 0 bloques).
func TestExcavateOldProtected(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"give Tester diamond_shovel 1", "give Tester diamond_pickaxe 1",
		}})
		r, _ := safeRunner(t, g)
		r.Store.NoteProtected(world.BlockPos{X: 12, Y: 64, Z: 12}) // tapa de x/z -4 a 28
		g.Step(5)
		box := automation.NewBox(world.BlockPos{X: 3, Y: 63, Z: 3}, world.BlockPos{X: 7, Y: 61, Z: 7})
		var err error
		r.Start(context.Background(), "excavar", func(j *automation.Job) error {
			err = automation.Excavate(box)(j)
			return err
		})
		g.Until(1500*20, func() bool { return r.Status() == "" })
		left := 0
		for x := 3; x <= 7; x++ {
			for y := 61; y <= 63; y++ {
				for z := 3; z <= 7; z++ {
					if !block.IsAir(g.S.World.At(x, y, z)) {
						left++
					}
				}
			}
		}
		t.Logf("error %v; quedan %d", err, left)
		if err != nil {
			t.Fatal(err)
		}
		if left > 4 {
			t.Errorf("quedan %d bloques: la zona protegida de antes no debería bloquear la caja", left)
		}
		if !r.Store.IsProtected(world.BlockPos{X: 12, Y: 64, Z: 12}) {
			t.Errorf("la zona apuntada no se tiene que borrar sola")
		}
	})
}

// TestExcavateLeaves: una copa de hojas (azalea, como en el servidor) sobre
// la caja: se cavan como lo demás. Antes, "waterlogged=false" contaba como
// agua y junto a las hojas no se cavaba nada ("agua o lava junto a…").
func TestExcavateLeaves(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"fill 4 64 4 8 65 8 minecraft:azalea_leaves[distance=1,persistent=true,waterlogged=false]",
			"fill 5 64 5 7 65 7 minecraft:flowering_azalea_leaves[distance=1,persistent=true,waterlogged=false]",
			"give Tester diamond_shovel 1", "give Tester diamond_pickaxe 1", "give Tester diamond_hoe 1",
		}})
		r, _ := safeRunner(t, g)
		g.Step(5)
		box := automation.NewBox(world.BlockPos{X: 3, Y: 65, Z: 3}, world.BlockPos{X: 9, Y: 62, Z: 9})
		var err error
		r.Start(context.Background(), "excavar", func(j *automation.Job) error {
			err = automation.Excavate(box)(j)
			return err
		})
		g.Until(1500*20, func() bool { return r.Status() == "" })
		left := 0
		for x := 3; x <= 9; x++ {
			for y := 62; y <= 65; y++ {
				for z := 3; z <= 9; z++ {
					if !block.IsAir(g.S.World.At(x, y, z)) {
						left++
					}
				}
			}
		}
		t.Logf("error %v; quedan %d", err, left)
		if err != nil {
			t.Fatal(err)
		}
		if left > 4 {
			t.Errorf("quedan %d bloques (las hojas no se cavaron)", left)
		}
	})
}

// TestExcavateEven: la caja baja de 4 en 4, pareja: mientras quede algo en
// una pasada de 4, no se cava nada dos pasadas más abajo (la siguiente sí
// puede empezar: la escalera para bajar a ella). Antes, o se hacían zanjas
// (zona por zona) o se quitaba 1 de alto en toda la caja cada vez.
func TestExcavateEven(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"effect give Tester haste 2",
		}})
		g.GiveEnchanted("minecraft:diamond_pickaxe", map[string]int{"efficiency": 5})
		g.GiveEnchanted("minecraft:diamond_shovel", map[string]int{"efficiency": 5})
		r, _ := safeRunner(t, g)
		g.Step(5)
		box := automation.NewBox(world.BlockPos{X: 3, Y: 63, Z: 3}, world.BlockPos{X: 12, Y: 52, Z: 12})
		passes := [][2]int{{60, 63}, {56, 59}, {52, 55}}
		solidIn := func(lo, hi int) (solid, air int) {
			for x := 3; x <= 12; x++ {
				for y := lo; y <= hi; y++ {
					for z := 3; z <= 12; z++ {
						if block.IsAir(g.S.World.At(x, y, z)) {
							air++
						} else {
							solid++
						}
					}
				}
			}
			return
		}
		var err error
		r.Start(context.Background(), "excavar", func(j *automation.Job) error {
			err = automation.Excavate(box)(j)
			return err
		})
		bad := 0
		g.Until(3000*20, func() bool {
			if g.Tick%10 == 0 {
				for k, p := range passes {
					if s, _ := solidIn(p[0], p[1]); s > 4 { // (más que la torre de salida)
						for _, q := range passes[min(k+2, len(passes)):] {
							if _, a := solidIn(q[0], q[1]); a > 0 && bad < 3 {
								bad++
								t.Errorf("tick %d: quedan %d bloques en las capas %d-%d y ya cavó %d en las %d-%d (no baja parejo)", g.Tick, s, p[0], p[1], a, q[0], q[1])
							}
						}
						break
					}
				}
			}
			return r.Status() == ""
		})
		s, _ := solidIn(52, 63)
		t.Logf("error %v; quedan %d", err, s)
		if err != nil {
			t.Fatal(err)
		}
		if s > 12 { // (la torre para salir de un hoyo de 12: la deja)
			t.Errorf("quedan %d bloques", s)
		}
	})
}

// TestExcavateProtectedSkip: parte de la caja es un terreno reclamado: al
// picar ahí el servidor devuelve el bloque (como un plugin de protección). El
// bot tiene que darse cuenta, dejar esos bloques y seguir con lo demás (antes:
// en bucle picando lo mismo, sin avanzar).
func TestExcavateProtectedSkip(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"setblock -3 64 0 minecraft:chest[facing=north,type=single,waterlogged=false]",
			"effect give Tester haste 2",
		}})
		g.GiveEnchanted("minecraft:diamond_pickaxe", map[string]int{"efficiency": 5})
		g.GiveEnchanted("minecraft:diamond_shovel", map[string]int{"efficiency": 5})
		g.Protect(9, 3, 12, 12) // la franja de x 9 a 12 es de otro
		r, _ := safeRunner(t, g)
		g.Step(5)
		box := automation.NewBox(world.BlockPos{X: 3, Y: 63, Z: 3}, world.BlockPos{X: 12, Y: 60, Z: 12})
		var err error
		start := g.Tick
		r.Start(context.Background(), "excavar", func(j *automation.Job) error {
			err = automation.Excavate(box)(j)
			return err
		})
		finished := g.Until(1500*20, func() bool { return r.Status() == "" })
		free, claimed := 0, 0
		for x := 3; x <= 12; x++ {
			for y := 60; y <= 63; y++ {
				for z := 3; z <= 12; z++ {
					if !block.IsAir(g.S.World.At(x, y, z)) {
						if x >= 9 {
							claimed++
						} else {
							free++
						}
					}
				}
			}
		}
		t.Logf("terminó %v (%d ticks); error %v; quedan %d fuera del terreno reclamado y %d dentro; picadas devueltas %d",
			finished, g.Tick-start, err, free, claimed, g.ProtectedTries())
		if !finished {
			t.Fatalf("no terminó: en bucle (%s)", r.Status())
		}
		if free > 4 {
			t.Errorf("quedan %d bloques fuera del terreno reclamado", free)
		}
		if g.ProtectedTries() > 4*160 {
			t.Errorf("%d picadas en el terreno reclamado: insiste demasiado", g.ProtectedTries())
		}
	})
}
