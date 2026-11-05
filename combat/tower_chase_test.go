package combat_test

import (
	"math"
	"os"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"

	"github.com/mj41/go-mc26-kit/bot/simworld"
)

// TestChaseUpTower: el rival (un jugador) se sube a unos bloques. Como en el
// goto, el bot pone bloques y sube detrás, sin pararse a pegar desde media
// torre ni rendirse mientras construye (antes, peleando con un jugador, no
// construía nada: se quedaba abajo, y a los 8 s lo dejaba). Allí arriba el
// rival le pega y le tira (como haría uno de verdad): se mira que suba y que
// siga, no que gane.
func TestChaseUpTower(t *testing.T) {
	t.Run("libre", func(t *testing.T) { chaseUpTower(t, false) })
	// el rival le pone un techo de tierra encima mientras sube: lo rompe (o
	// sube por otra columna) y sigue; no se queda saltando debajo
	t.Run("techo", func(t *testing.T) { chaseUpTower(t, true) })
}

func chaseUpTower(t *testing.T, capIt bool) {
	simworld.Run(t, func(t *testing.T) {
		g, f := simFighter(t, append([]string{
			"fill 6 64 -1 8 69 1 stone", // una torre de 3×3 y 6 de alto, con un hueco
			"setblock 7 69 0 air",       // arriba: el rival se queda en él (no baja)
			"give Tester cobblestone 32",
		}, kit...)...)
		g.Teleport(0.5, 64, 0.5)
		g.Step(5 * 20)
		if err := g.Summon("minecraft:player", 7.5, 0.5); err != nil {
			t.Fatal(err)
		}
		id := g.LastMob()
		if err := g.S.SetBlock(7, 71, 0, "minecraft:stone"); err != nil { // (con techo: no salta fuera)
			t.Fatal(err)
		}
		g.Step(lag + 2)
		f.Engage(id, "en la torre")
		if os.Getenv("TRACE") != "" {
			think := g.B.Ctl.Think
			g.B.Ctl.Think = func(s *control.State, bp *basic.Player) {
				think(s, bp)
				p := bp.Position()
				t.Logf("t=%d %.2f %.2f %.2f suelo=%v choque=%v in=%+v slot=%d", s.Ticks, p.X, p.Y, p.Z, s.OnGround, s.HorizontalCollision, s.Input, g.B.Screens.HeldSlot)
			}
		}
		top, capped := 0.0, false
		g.Until(40*20, func() bool {
			top = max(top, g.Pos()[1])
			if p := g.B.Player.Position(); capIt && !capped && g.B.Ctl.State().OnGround && p.Y >= 66 && p.Y < 68 && p.X < 6 {
				capped = true
				if err := g.S.SetBlock(int(math.Floor(p.X)), int(math.Floor(p.Y))+2, int(math.Floor(p.Z)), "minecraft:dirt"); err != nil {
					t.Fatal(err)
				}
			}
			_, alive := g.MobHealth(id)
			return !alive || g.Deaths() > 0
		})
		_, alive := g.MobHealth(id)
		mx, my, mz, _ := g.MobAt(id)
		t.Logf("lo más alto: %.1f; peleando %v; rival vivo %v, en %.1f %.1f %.1f", top, f.Fighting(), alive, mx, my, mz)
		if capIt && !capped {
			t.Errorf("no llegó a ponerle el techo (la prueba no probó nada)")
		}
		if top < 67.9 {
			t.Errorf("no subió a la torre (y más alta %.1f)", top)
		}
		if alive && !f.Fighting() {
			t.Errorf("lo dejó estando vivo (y con bloques para subir)")
		}
	})
}
