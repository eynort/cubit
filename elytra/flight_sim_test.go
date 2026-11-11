package elytra_test

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/registryid"

	"minecraft-bot/elytra"
	"minecraft-bot/pathing"
)

// TestFlySim: el bot entero (cliente, física, manos, inventario) vuela con
// élitros contra el servidor simulado, que comprueba cada movimiento (lo que
// mete al jugador en un bloque lo devuelve atrás), crea los cohetes y cuenta
// la caída: se pone los élitros (el peto a la mano), despega, vuela 400
// bloques, aterriza sin daño, y se vuelve a poner el peto.
func TestFlySim(t *testing.T) {
	for _, lag := range []int{0, 3} {
		t.Run(fmt.Sprintf("lag%d", lag), func(t *testing.T) { flySim(t, lag) })
	}
}

func rockets(b *fakeserver.Bot) int {
	b.Screens.Lock()
	defer b.Screens.Unlock()
	n := 0
	for _, s := range b.Screens.Inventory.Slots {
		if s.Count > 0 && registryid.Item[s.Item] == "minecraft:firework_rocket" {
			n += int(s.Count)
		}
	}
	return n
}

func flySim(t *testing.T, lag int) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Lag: lag, Setup: []string{
			"give @p iron_chestplate", "give @p elytra", "give @p firework_rocket 32",
			"fill 180 64 -20 184 110 20 stone", // un muro en el camino
		}})
		b := g.B
		b.Mover.Food = func() int { return 20 }
		f := elytra.New(elytra.Deps{Client: b.C, Player: b.Player, World: b.World, Entities: b.Entities,
			Screens: b.Screens, Hands: b.Hands, Ctl: b.Ctl, Mover: b.Mover, Logf: t.Logf})
		b.Ctl.Think = func(s *control.State, bp *basic.Player) {
			if f.Active() {
				f.Think(s, bp)
			}
		}
		g.Step(20)
		// el peto puesto (como lo lleva el bot normalmente)
		if err := b.Screens.SelectHotbar(0); err != nil {
			t.Fatal(err)
		}
		b.Hands.UseItemToward(0, 64, 10, nil)
		g.Step(10)
		if w := g.Worn(6); w != "minecraft:iron_chestplate" {
			t.Fatalf("el peto no está puesto: %q", w)
		}
		r0 := rockets(b)
		goal := [2]float64{400, 30}
		done := make(chan error, 1)
		go func() { done <- f.FlyTo(context.Background(), goal[0], math.NaN(), goal[1]) }()
		t0 := b.Ctl.State().Teleports
		var err error
		finished := g.Until(20*120, func() bool {
			select {
			case err = <-done:
				return true
			default:
				return false
			}
		})
		g.Step(40) // (volver a ponerse el peto)
		p := b.Player.Position()
		d := math.Hypot(p.X-goal[0], p.Z-goal[1])
		tps := b.Ctl.State().Teleports - t0
		t.Logf("terminó=%v err=%v en %d ticks: en %.1f %.1f %.1f (a %.1f de la meta), vida %.1f, correcciones %d, pecho %q, planeando=%v",
			finished, err, g.Tick, p.X, p.Y, p.Z, d, g.Health, tps, g.Worn(6), g.Gliding())
		if !finished || err != nil {
			t.Fatalf("no aterrizó: %v", err)
		}
		if d > 40 {
			t.Errorf("aterrizó a %.0f bloques de la meta", d)
		}
		if g.Health < 20 {
			t.Errorf("se hizo daño: vida %.1f", g.Health)
		}
		if tps > 0 {
			t.Errorf("el servidor lo devolvió atrás %d veces", tps)
		}
		if g.Gliding() {
			t.Errorf("el servidor aún lo tiene planeando")
		}
		if w := g.Worn(6); w != "minecraft:iron_chestplate" {
			t.Errorf("no se volvió a poner el peto: lleva %q", w)
		}
		used := r0 - rockets(b)
		t.Logf("cohetes usados: %d", used)
		if used == 0 {
			t.Errorf("no usó ningún cohete")
		}
	})
}

// TestFlyIntoLake: el vuelo acaba en un lago: los élitros siguen abiertos en
// el agua (solo los cierra tocar suelo) y con ellos la cabeza va a 0.4 de los
// pies (en los logs: flotando quieto así, se ahogó). Baja al fondo a
// cerrarlos, y luego, quieto, mantiene los ojos fuera del agua.
func TestFlyIntoLake(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give @p elytra", "give @p firework_rocket 32",
			"fill 360 56 -10 440 63 70 water",
		}})
		b := g.B
		b.Mover.Food = func() int { return 20 }
		f := elytra.New(elytra.Deps{Client: b.C, Player: b.Player, World: b.World, Entities: b.Entities,
			Screens: b.Screens, Hands: b.Hands, Ctl: b.Ctl, Mover: b.Mover, Logf: t.Logf})
		w := pathing.New(b.World, b.Player, physics.ClientTags{T: &b.C.Tags})
		b.Ctl.Think = func(s *control.State, bp *basic.Player) {
			if f.Active() {
				f.Think(s, bp)
				return
			}
			w.Think(s, bp) // quieto: el walker solo saca la cabeza del agua
		}
		g.Step(20)
		done := make(chan error, 1)
		go func() { done <- f.FlyTo(context.Background(), 400, math.NaN(), 30) }()
		var err error
		if !g.Until(20*120, func() bool {
			select {
			case err = <-done:
				return true
			default:
				return false
			}
		}) || err != nil {
			t.Fatalf("el vuelo no terminó: %v", err)
		}
		t.Logf("tras el vuelo: planeando según el servidor=%v", g.Gliding())
		if g.Gliding() {
			t.Errorf("acabó el vuelo con los élitros abiertos en el agua")
		}
		under := 0
		for i := 0; i < 20*20; i++ {
			g.Step(1)
			p := b.Player.Position()
			eye := 1.62
			if g.Gliding() {
				eye = 0.4
			}
			if s, ok := b.World.BlockAt(world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + eye)), Z: int(math.Floor(p.Z))}); ok && strings.HasPrefix(world.StateString(s), "minecraft:water") {
				under++
			}
		}
		t.Logf("de 400 ticks quieto en el lago, con los ojos bajo el agua: %d", under)
		if under > 100 {
			t.Errorf("con los ojos bajo el agua %d de 400 ticks: se ahoga", under)
		}
	})
}
