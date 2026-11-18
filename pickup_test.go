package main

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
)

// TestMakeRoomForThrown: con el inventario lleno de piedra, si le tiran un
// pico tira un montón de basura para que entre; con hueco, o si lo que le
// tiran es basura, no tira nada; sin basura, tampoco (no tira lo importante).
func TestMakeRoomForThrown(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{"give Tester cobblestone 2240", "give Tester cooked_beef 64"}}) // 35 montones + comida: lleno
		b := &Bot{Player: g.B.Player, Ctl: g.B.Ctl, Screens: g.B.Screens}
		g.Step(5)
		if b.makeRoomForItem("cobblestone") {
			t.Fatal("por basura no se hace sitio")
		}
		if !b.makeRoomForItem("diamond_pickaxe") {
			t.Fatal("con el inventario lleno de basura, tenía que tirar un montón")
		}
		g.Step(5)
		got := b.itemTotals()
		if got["cobblestone"] != 2240-64 || got["cooked_beef"] != 64 {
			t.Fatalf("le queda %v: tenía que tirar solo un montón de piedra", got)
		}
		if b.makeRoomForItem("diamond_pickaxe") {
			t.Fatal("ya hay hueco: no tenía que tirar más")
		}
		if b.makeRoomForItem("cooked_beef") {
			t.Fatal("la comida se apila con la que lleva: no hace falta sitio")
		}
	})
}

// TestMakeRoomNeverThrowsValuables: sin basura, no tira nada aunque esté lleno.
func TestMakeRoomNeverThrowsValuables(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{"give Tester diamond 2304"}}) // 36 montones de diamantes
		b := &Bot{Player: g.B.Player, Ctl: g.B.Ctl, Screens: g.B.Screens}
		g.Step(5)
		if b.makeRoomForItem("iron_pickaxe") {
			t.Fatal("tiró algo sin tener basura")
		}
		if b.itemTotals()["diamond"] != 2304 {
			t.Fatal("ha tirado diamantes")
		}
	})
}
