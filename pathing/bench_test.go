package pathing_test

import (
	"fmt"
	"math"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26-kit/bot/path"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"

	"minecraft-bot/pathing"
)

// course es un recorrido de prueba: los bloques, la meta y (para follow) la ruta del objetivo.
type course struct {
	name  string
	build func(w *fakeserver.World)
	goal  world.BlockPos
	// route, si no es nil, es por dónde camina el objetivo a seguir (centros de bloque, pies)
	route [][3]float64
	// sprint: el objetivo corre (5.6 b/s) en vez de andar
	sprint bool
}

func stairs(w *fakeserver.World) {
	for i := 0; i < 6; i++ { // seis escalones de un bloque hacia +x
		w.Fill(2+i, 64, -1, 2+i, 64+i, 1, "minecraft:stone")
	}
	w.Fill(8, 64, -1, 12, 69, 1, "minecraft:stone") // rellano arriba
}

func zigzag(w *fakeserver.World) {
	// escalones que cambian de dirección: +x, +z, +x, +z...
	x, z, y := 2, 0, 64
	for i := 0; i < 8; i++ {
		w.Fill(x-1, 64, z-1, x+1, y, z+1, "minecraft:stone")
		if i%2 == 0 {
			x += 2
		} else {
			z += 2
		}
		y++
	}
	w.Fill(x-1, 64, z-1, x+1, y, z+1, "minecraft:stone")
}

func gaps(w *fakeserver.World) {
	// plataformas a 3 de altura (no se vuelve a subir desde el suelo) con huecos de 1 y 2 bloques
	w.Fill(-1, 64, -1, 1, 64, 1, "minecraft:stone")
	w.Fill(2, 64, -1, 2, 65, 1, "minecraft:stone")
	w.Fill(3, 64, -1, 5, 66, 1, "minecraft:stone")
	w.Fill(7, 64, -1, 9, 66, 1, "minecraft:stone")   // hueco de 1 en x=6
	w.Fill(12, 64, -1, 14, 66, 1, "minecraft:stone") // hueco de 2 en x=10..11
}

// surface es la altura del terreno de colinas en (x, z): donde están los pies.
func surface(x, z int) int {
	if x >= -1 && x <= 1 && z >= -1 && z <= 1 {
		return 64 // donde aparece el bot: llano
	}
	h := 1.6*math.Sin(float64(x)/3.7) + 1.4*math.Cos(float64(z)/4.3) + 0.8*math.Sin(float64(x+z)/2.9)
	return 64 + max(0, int(math.Round(h+1)))
}

func hills(w *fakeserver.World) {
	for x := -4; x <= 26; x++ {
		for z := -6; z <= 26; z++ {
			if h := surface(x, z); h > 64 {
				w.Fill(x, 64, z, x, h-1, z, "minecraft:dirt")
			}
		}
	}
	// un acantilado de 2 que hay que rodear, vallas, losas y escalones
	w.Fill(8, 64, 2, 9, surface(8, 2)+1, 10, "minecraft:stone")
	for z := 12; z <= 18; z++ {
		w.Set(14, surface(14, z), z, "minecraft:oak_fence")
	}
	for _, c := range [][2]int{{4, 4}, {5, 9}, {17, 6}, {20, 15}} {
		w.Set(c[0], surface(c[0], c[1]), c[1], "minecraft:stone_slab")
	}
	for _, c := range [][2]int{{11, 3}, {6, 16}, {19, 20}} {
		w.Set(c[0], surface(c[0], c[1]), c[1], "minecraft:oak_stairs")
	}
}

func parkour(w *fakeserver.World) {
	// un foso de lava: caer es perder, hay que saltar
	w.Fill(-3, 63, -3, 22, 63, 3, "minecraft:lava")
	w.Fill(-1, 63, -1, 1, 63, 1, "minecraft:stone") // la salida
	w.Fill(2, 63, 0, 2, 64, 0, "minecraft:stone")   // escalón (pie a 65)
	w.Fill(3, 63, 0, 5, 65, 0, "minecraft:stone")   // pasillo de 1 de ancho (66)
	w.Fill(8, 63, 0, 9, 65, 0, "minecraft:stone")   // hueco de 2
	w.Fill(11, 63, 0, 13, 66, 0, "minecraft:stone") // hueco de 1 subiendo (67)
	w.Fill(17, 63, 0, 19, 66, 0, "minecraft:stone") // hueco de 3, con carrerilla
}

func ladder(w *fakeserver.World) {
	w.Fill(4, 64, -3, 4, 68, 3, "minecraft:stone") // pared de 5
	w.Fill(5, 64, -3, 9, 68, 3, "minecraft:stone") // meseta detrás
	// pegada a la pared (al este): mira al oeste. "minecraft:ladder" sin propiedades sería aire
	l := block.ToStateID[block.Ladder{Facing: block.DirectionWest}]
	for y := 64; y <= 68; y++ {
		w.SetState(3, y, 0, l)
	}
}

var courses = []course{
	{name: "escalera", build: stairs, goal: world.BlockPos{X: 11, Y: 70, Z: 0}},
	{name: "zigzag", build: zigzag, goal: world.BlockPos{X: 10, Y: 73, Z: 8}},
	{name: "huecos", build: gaps, goal: world.BlockPos{X: 13, Y: 67, Z: 0}},
	{name: "colinas", build: hills, goal: world.BlockPos{X: 22, Y: surface(22, 22), Z: 22}},
	{name: "parkour", build: parkour, goal: world.BlockPos{X: 18, Y: 67, Z: 0}},
	{name: "escalera-mano", build: ladder, goal: world.BlockPos{X: 7, Y: 69, Z: 0}},
	{name: "seguir-colinas", build: hills, goal: world.BlockPos{X: 22, Y: surface(22, 22), Z: 22}, sprint: true, route: func() [][3]float64 {
		var r [][3]float64
		for _, c := range [][2]int{{3, 0}, {6, 1}, {6, 12}, {12, 12}, {12, 20}, {18, 22}, {22, 22}} {
			r = append(r, [3]float64{float64(c[0]) + 0.5, float64(surface(c[0], c[1])), float64(c[1]) + 0.5})
		}
		return r
	}()},
	{name: "seguir-escalera", build: stairs, goal: world.BlockPos{X: 11, Y: 70, Z: 0}, route: [][3]float64{
		{3.5, 65, 0.5}, {4.5, 66, 0.5}, {5.5, 67, 0.5}, {6.5, 68, 0.5}, {7.5, 69, 0.5}, {8.5, 70, 0.5}, {11.5, 70, 0.5},
	}},
	{name: "seguir-zigzag", build: zigzag, goal: world.BlockPos{X: 10, Y: 73, Z: 8}, route: [][3]float64{
		{2.5, 65, 0.5}, {4.5, 66, 0.5}, {4.5, 67, 2.5}, {6.5, 68, 2.5}, {6.5, 69, 4.5}, {8.5, 70, 4.5}, {8.5, 71, 6.5}, {10.5, 72, 6.5}, {10.5, 73, 8.5},
	}},
}

// mover es lo que se compara: el walker de la librería o el nuestro.
type mover interface {
	Goto(world.BlockPos)
	Follow(func() (x, y, z float64, ok bool), float64)
	Think(*control.State, *basic.Player)
	Status() string
}

type result struct {
	ok    bool
	ticks int
	why   string
}

func run(t *testing.T, c course, mk func(b *fakeserver.Bot) mover) result {
	return runMode(t, c, mk, true)
}

// runMode: stepped=true, juego por pasos (determinista, búsqueda en línea);
// false, reloj real (búsqueda en otra goroutine, como en un servidor).
func runMode(t *testing.T, c course, mk func(b *fakeserver.Bot) mover, stepped bool) result {
	var r result
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		c.build(s.World)
		var b *fakeserver.Bot
		if stepped {
			b = fakeserver.JoinStepped(t, s, "Tester")
		} else {
			b = fakeserver.Join(t, s, "Tester")
		}
		if err := b.Loaded(3 * time.Second); err != nil {
			t.Fatal(err)
		}
		b.Mover.Food = func() int { return 20 } // el servidor falso no manda la comida: sin ella no se corre
		m := mk(b)
		b.Ctl.Think = m.Think
		const limit = 30 * 20 // 30 segundos de juego
		gx, gz := float64(c.goal.X)+0.5, float64(c.goal.Z)+0.5
		at := func() bool {
			p := b.Player.Position()
			return math.Hypot(p.X-gx, p.Z-gz) < 2.6 && math.Abs(p.Y-float64(c.goal.Y)) < 0.6
		}
		var tick atomic.Int64
		if c.route == nil {
			m.Goto(c.goal)
		} else {
			// el objetivo camina la ruta a 4.3 b/s (andar), empezando 2 bloques por delante
			m.Follow(func() (float64, float64, float64, bool) {
				v := 0.215
				if c.sprint {
					v = 0.28
				}
				d := float64(tick.Load()) * v
				pts := c.route
				for i := 1; i < len(pts); i++ {
					a, z := pts[i-1], pts[i]
					seg := math.Hypot(z[0]-a[0], z[2]-a[2])
					if d <= seg {
						f := d / seg
						return a[0] + (z[0]-a[0])*f, z[1], a[2] + (z[2]-a[2])*f, true
					}
					d -= seg
				}
				l := pts[len(pts)-1]
				return l[0], l[1], l[2], true
			}, 2)
		}
		n := 0
		check := func() bool {
			st := m.Status()
			if len(st) >= 6 && st[:6] == "failed" {
				r.why = st
				return true
			}
			return at()
		}
		var done bool
		if stepped {
			done = b.Until(limit, func() bool {
				n++
				tick.Add(1)
				return check()
			})
		} else {
			t0 := b.Ctl.State().Ticks
			for {
				time.Sleep(fakeserver.FastTick)
				n = int(b.Ctl.State().Ticks - t0)
				tick.Store(int64(n))
				if done = check(); done || n >= limit {
					break
				}
			}
		}
		r.ticks = n
		r.ok = done && at()
		if !r.ok && r.why == "" {
			p := b.Player.Position()
			r.why = fmt.Sprintf("se quedó en %.1f %.1f %.1f (%s)", p.X, p.Y, p.Z, m.Status())
		}
	})
	return r
}

func kitWalker(b *fakeserver.Bot) mover {
	w := &path.Walker{Graph: b.Walker.Graph, Sprint: true}
	return w
}

func ourWalker(b *fakeserver.Bot) mover {
	return pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
}

// TestCompare compara los dos walkers en cada recorrido (go test -v ./pathing -run Compare).
func TestCompare(t *testing.T) {
	for _, c := range courses {
		kit := run(t, c, kitWalker)
		our := run(t, c, ourWalker)
		real := runMode(t, c, ourWalker, false)
		t.Logf("%-16s librería: ok=%-5v %4d ticks %s", c.name, kit.ok, kit.ticks, kit.why)
		t.Logf("%-16s nuestro:  ok=%-5v %4d ticks %s", c.name, our.ok, our.ticks, our.why)
		t.Logf("%-16s nuestro (reloj real): ok=%-5v %4d ticks %s", c.name, real.ok, real.ticks, real.why)
		if !our.ok || !real.ok {
			t.Errorf("%s: nuestro walker no llegó: %s %s", c.name, our.why, real.why)
		}
	}
}
