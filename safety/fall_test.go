package safety_test

import (
	"fmt"
	"testing"

	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/simworld"

	"minecraft-bot/safety"
)

// TestWaterClutch: cayendo de lo alto con un cubo de agua en la barra
// rápida, lo echa al llegar al suelo y no se hace daño; luego recoge el agua
// (vuelve a tener el cubo de agua). Sin cubo, la caída hace daño (el
// simulador lo cuenta como el juego).
func TestWaterClutch(t *testing.T) {
	for _, c := range []struct {
		name   string
		height float64
		bucket bool
	}{{"10-con-cubo", 10, true}, {"25-con-cubo", 25, true}, {"40-con-cubo", 40, true}, {"25-sin-cubo", 25, false}} {
		t.Run(c.name, func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				var setup []string
				if c.bucket {
					setup = []string{"give Tester stone_pickaxe 1", "give Tester water_bucket 1"}
				}
				g := simworld.Start(t, simworld.Options{Setup: setup, Lag: 3})
				b := g.B
				f := &safety.Fall{World: b.World, Mover: b.Mover, Screens: b.Screens, Hands: b.Hands, Logf: t.Logf}
				b.Ctl.Think = func(s *control.State, bp *basic.Player) { f.Think(s, bp) }
				g.Step(10)
				ground := b.Player.Position().Y
				g.Teleport(5.5, ground+c.height, 5.5)
				g.Step(int(c.height)*2 + 60) // (la caída, el agua y recogerla)
				t.Logf("vida %.0f; cubos de agua %d, vacíos %d; caídas salvadas %d", g.Health, g.Count("minecraft:water_bucket"), g.Count("minecraft:bucket"), f.Saves())
				if !c.bucket {
					if g.Health >= 20 {
						t.Errorf("sin cubo no se hizo daño: el simulador no cuenta la caída")
					}
					return
				}
				if g.Health < 20 {
					t.Errorf("se hizo daño: vida %.0f", g.Health)
				}
				if g.Count("minecraft:water_bucket") != 1 {
					t.Errorf("no recogió el agua (cubos de agua %d)", g.Count("minecraft:water_bucket"))
				}
			})
		})
	}
}

// TestWaterDrop: el water drop en más casos. Alturas de 4 (un corazón) a 100
// (velocidad máxima, casi 4 bloques por tick: el alcance del cubo se cruza en
// un tick); el cubo en el inventario (lo pasa a la barra cayendo); y cayendo
// con impulso de lado (un empujón, salir corriendo de un borde: cae en otra
// columna, no en la de debajo). En todos: sin daño y con el cubo lleno al final.
func TestWaterDrop(t *testing.T) {
	type tc struct {
		name   string
		height float64
		inv    bool    // el cubo en el inventario, no en la barra
		push   float64 // impulso de lado (bloques por tick)
		none   bool    // sin cubo: el control (la caída sí hace daño)
	}
	var cases []tc
	for _, h := range []float64{4, 5, 7, 15, 30, 60, 100} {
		cases = append(cases, tc{name: fmt.Sprintf("%.0f", h), height: h})
	}
	cases = append(cases, tc{"inventario-20", 20, true, 0, false}, tc{"inventario-8", 8, true, 0, false},
		tc{"empujon-12", 12, false, 0.5, false}, tc{"empujon-30", 30, false, 0.8, false}, tc{"empujon-inventario-20", 20, true, 0.6, false},
		tc{"sin-cubo-5", 5, false, 0, true}, tc{"sin-cubo-empujon-12", 12, false, 0.5, true})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				setup := []string{"give Tester water_bucket 1"}
				if c.none {
					setup = nil
				}
				if c.inv { // la barra llena: el cubo, al inventario
					setup = []string{}
					for i := 0; i < 9; i++ {
						setup = append(setup, "give Tester cobblestone 64")
					}
					setup = append(setup, "give Tester water_bucket 1")
				}
				g := simworld.Start(t, simworld.Options{Setup: setup, Lag: 4})
				b := g.B
				f := &safety.Fall{World: b.World, Mover: b.Mover, Screens: b.Screens, Hands: b.Hands, Logf: t.Logf}
				b.Ctl.Think = func(s *control.State, bp *basic.Player) { f.Think(s, bp) }
				g.Step(10)
				ground := b.Player.Position().Y
				g.Teleport(0.5, ground+c.height, 0.5)
				if c.push > 0 {
					g.Step(1)
					v := c.push
					if err := g.S.Send(play.SetEntityMotion{ID: 1, Movement: types.LpVec3{X: v, Y: 0, Z: v / 2}}); err != nil {
						t.Fatal(err)
					}
				}
				low := g.Health // (la más baja: con el hambre llena se cura enseguida)
				g.Until(int(c.height)*2+80, func() bool { low = min(low, g.Health); return false })
				p := g.Pos()
				t.Logf("vida mínima %.0f; cubos de agua %d, vacíos %d; cayó en %.1f %.1f", low, g.Count("minecraft:water_bucket"), g.Count("minecraft:bucket"), p[0], p[2])
				if c.none {
					if low >= 20 {
						t.Errorf("sin cubo no se hizo daño: la prueba no prueba nada")
					}
					return
				}
				if low < 20 {
					t.Errorf("se hizo daño: vida mínima %.0f", low)
				}
				if g.Count("minecraft:water_bucket") != 1 {
					t.Errorf("no acabó con el cubo lleno")
				}
			})
		})
	}
}
