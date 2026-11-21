package pathing_test

import (
	"context"
	"math"
	"os"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"cubit/pathing"
)

// TestPillarCap: el bot sube en torre hacia lo alto de una torre de 6 y
// alguien, desde arriba, le pone un techo encima de la cabeza (el truco del
// PvP para que no suba: saltando contra el techo el bloque de debajo no se
// pone). Antes se quedaba saltando en el sitio. Ahora:
//   - un techo que rompe enseguida (tierra a mano, piedra con pico): lo
//     rompe con la mejor herramienta de la barra y sigue;
//   - uno que no (piedra sin pico: 10 s): sube por la columna de al lado;
//   - si le tapan una y otra vez la misma columna: tras 3, cambia de columna.
func TestPillarCap(t *testing.T) {
	cases := []struct {
		name  string
		give  []string
		cap   string
		caps  int // techos que pone el de arriba (uno cada vez que el bot tiene sitio)
		broke bool
	}{
		{"tierra-a-mano", nil, "minecraft:dirt", 1, true},
		{"piedra-sin-pico", nil, "minecraft:cobblestone", 1, false},
		{"piedra-con-pico", []string{"give Tester stone_pickaxe 1"}, "minecraft:cobblestone", 1, true},
		{"tapa-sin-parar", nil, "minecraft:dirt", 8, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			simworld.Run(t, func(t *testing.T) {
				setup := append([]string{"fill 6 64 -1 8 69 1 stone", "give Tester dirt 48"}, c.give...)
				g := simworld.Start(t, simworld.Options{Setup: setup})
				b := g.B
				b.Mover.Food = func() int { return 20 }
				w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
				w.SetBuilder(&pathing.HandsBuilder{Screens: b.Screens, Hands: b.Hands, Ctl: b.Ctl, Ctx: context.Background()})
				b.Ctl.Think = w.Think
				if os.Getenv("TRACE") == c.name {
					b.Ctl.Think = func(s *control.State, bp *basic.Player) {
						w.Think(s, bp)
						p := bp.Position()
						t.Logf("t=%d %.2f %.2f %.2f suelo=%v J=%v %s", s.Ticks, p.X, p.Y, p.Z, s.OnGround, s.Input.Jump, w.Debug())
					}
				}
				g.Teleport(0.5, 64, 0.5)
				g.Step(20)
				goal := world.BlockPos{X: 7, Y: 70, Z: 0}
				w.Goto(goal)
				placed, broken := 0, false
				var capAt world.BlockPos
				ok := g.Until(90*20, func() bool {
					if w.Status() == "arrived" {
						return true
					}
					// el de arriba: con el bot en su torre (de pie, a 65 o más) y
					// sitio encima de la cabeza, un techo
					if placed > 0 && !broken {
						if st, ok := b.World.BlockAt(capAt); ok && world.StateString(st) == "minecraft:air" {
							broken = true // (luego la torre del bot puede ocupar la celda)
						}
					}
					p := b.Player.Position()
					if placed < c.caps && b.Ctl.State().OnGround && p.Y >= 65 && p.Y < 69 && p.X < 6 {
						cell := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y)) + 2, Z: int(math.Floor(p.Z))}
						if st, ok := b.World.BlockAt(cell); ok && world.StateString(st) == "minecraft:air" {
							if err := g.S.SetBlock(cell.X, cell.Y, cell.Z, c.cap); err != nil {
								t.Fatal(err)
							}
							placed++
							capAt = cell
						}
					}
					return false
				})
				p := b.Player.Position()
				t.Logf("llegó=%v en %d ticks (%s), en %.1f %.1f %.1f; techos %d, alguno roto %v", ok, g.Tick, w.Status(), p.X, p.Y, p.Z, placed, broken)
				if !ok {
					t.Errorf("no llegó arriba: %s", w.Debug())
				}
				if placed == 0 {
					t.Errorf("no hubo techo (la prueba no probó nada)")
				}
				if c.caps == 1 && broken != c.broke {
					t.Errorf("techo roto=%v, quería %v", broken, c.broke)
				}
			})
		})
	}
}
