package automation_test

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"cubit/automation"
)

func afkDist(g *simworld.Game, spot world.BlockPos) float64 {
	p := g.B.Player.Position()
	return math.Hypot(p.X-float64(spot.X)-0.5, p.Z-float64(spot.Z)-0.5)
}

// TestAfkStaysAndFidgets: pasa mucho tiempo quieto en su sitio: hace gestos
// (se nota en lo que manda al servidor) y nunca se aleja más de 2 bloques.
func TestAfkStaysAndFidgets(t *testing.T) {
	defer automation.SetAfkTimes(40, 120)()
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{})
		r := runner(t, g)
		var said []string
		r.SetSay(func(s string) { said = append(said, s) })
		g.Step(5)
		spot := world.BlockPos{X: int(math.Floor(g.B.Player.Position().X)), Y: 64, Z: int(math.Floor(g.B.Player.Position().Z))}
		r.Start(context.Background(), "afk", automation.Afk(automation.AfkOptions{Spot: spot, Seed: 7}))
		far, swings0, jumps := 0.0, g.B.Hands.Swings(), 0
		for i := 0; i < 600; i++ { // 10 minutos del juego
			g.Step(20)
			far = math.Max(far, afkDist(g, spot))
			if !g.B.Ctl.State().OnGround {
				jumps++
			}
		}
		t.Logf("lo más lejos %.2f, saltando %d s, manotazos %d, dijo %q, estado %q", far, jumps, g.B.Hands.Swings()-swings0, said, r.Status())
		if far > 2.5 {
			t.Errorf("se alejó %.2f bloques de su sitio", far)
		}
		if r.Status() == "" {
			t.Error("la tarea acabó sola: el afk no acaba hasta /stop")
		}
		if len(said) == 0 || !strings.Contains(said[0], "aquí") && !strings.Contains(said[0], "here") {
			t.Errorf("tenía que avisar de que se queda: %q", said)
		}
		if jumps == 0 && g.B.Hands.Swings() == swings0 {
			t.Error("en 10 minutos no hizo ningún gesto (saltar o manotazo)")
		}
		r.Stop()
	})
}

// TestAfkReturnsAfterPush: si lo empujan lejos, vuelve a su sitio.
func TestAfkReturnsAfterPush(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{})
		r := runner(t, g)
		g.Step(5)
		p := g.B.Player.Position()
		spot := world.BlockPos{X: int(math.Floor(p.X)), Y: 64, Z: int(math.Floor(p.Z))}
		r.Start(context.Background(), "afk", automation.Afk(automation.AfkOptions{Spot: spot, Seed: 3}))
		g.Step(60)
		g.Teleport(float64(spot.X)+6.5, 64, float64(spot.Z)+0.5)
		back := g.Until(40*20, func() bool { return afkDist(g, spot) < 1.2 })
		t.Logf("volvió=%v, a %.2f", back, afkDist(g, spot))
		if !back {
			t.Error("no volvió a su sitio tras el empujón")
		}
		if r.Status() == "" {
			t.Error("la tarea acabó")
		}
		r.Stop()
	})
}

// TestAfkGoesToBed: de noche va a la cama cercana, la usa (el simulador no
// acuesta a nadie: se da por no dormido) y vuelve a su sitio.
func TestAfkGoesToBed(t *testing.T) {
	defer automation.SetAfkTimes(100000, 200000)()
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{"setblock 5 64 0 minecraft:red_bed[facing=east,occupied=false,part=foot]", "setblock 6 64 0 minecraft:red_bed[facing=east,occupied=false,part=head]"}})
		r := runner(t, g)
		var said []string
		r.SetSay(func(s string) { said = append(said, s) })
		g.Step(5)
		p := g.B.Player.Position()
		spot := world.BlockPos{X: int(math.Floor(p.X)), Y: 64, Z: int(math.Floor(p.Z))}
		night := true
		r.Start(context.Background(), "afk", automation.Afk(automation.AfkOptions{Spot: spot, Seed: 1, Night: func() bool { return night }}))
		nearBed := false
		g.Until(60*20, func() bool {
			if math.Hypot(g.B.Player.Position().X-5.5, g.B.Player.Position().Z-0.5) < 4.5 {
				nearBed = true
			}
			return r.Status() != "" && len(said) >= 3
		})
		night = false
		g.Step(20 * 30)
		t.Logf("junto a la cama=%v, dijo %q, a %.2f del sitio", nearBed, said, afkDist(g, spot))
		if !nearBed {
			t.Error("no fue a la cama")
		}
		if afkDist(g, spot) > 1.5 {
			t.Errorf("no volvió a su sitio (a %.2f)", afkDist(g, spot))
		}
		joined := strings.Join(said, "|")
		if !strings.Contains(joined, "bed") {
			t.Errorf("no avisó de que se iba a dormir: %q", said)
		}
		r.Stop()
	})
}
