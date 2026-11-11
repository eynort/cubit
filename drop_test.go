package main

import (
	"context"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
)

// TestDropAll: /drop tira al suelo todo lo que lleva, y luego no lo recoge.
func TestDropAll(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{"give Tester dirt 100", "give Tester iron_sword 1", "give Tester cooked_beef 5"}})
		b := &Bot{Player: g.B.Player, Ctl: g.B.Ctl, Screens: g.B.Screens}
		g.Step(5)
		done := make(chan struct{})
		go func() { b.dropAll(context.Background(), ""); close(done) }()
		if !g.Until(5*20, func() bool {
			select {
			case <-done:
				return true
			default:
				return false
			}
		}) {
			t.Fatal("no terminó de tirar")
		}
		g.Step(5)
		b.Screens.Lock()
		for i, s := range b.Screens.Inventory.Slots {
			if s.Count > 0 {
				t.Errorf("slot %d sigue con %d", i, s.Count)
			}
		}
		b.Screens.Unlock()
		if g.Items() < 4 { // tierra (2 montones), espada, carne
			t.Errorf("en el suelo hay %d objetos, se esperaban 4", g.Items())
		}
		if !dropping() {
			t.Error("justo después de tirar, dropping() debería ser true (no recoger lo suyo)")
		}
	})
}

// TestDropItem: /drop dirt 70 tira 70 de tierra (un montón y 6 sueltos) y
// deja lo demás.
func TestDropItem(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{"give Tester dirt 100", "give Tester cooked_beef 5"}})
		b := &Bot{Player: g.B.Player, Ctl: g.B.Ctl, Screens: g.B.Screens}
		g.Step(5)
		done := make(chan struct{})
		go func() { b.dropItem(context.Background(), "dirt", 70, ""); close(done) }()
		if !g.Until(10*20, func() bool {
			select {
			case <-done:
				return true
			default:
				return false
			}
		}) {
			t.Fatal("no terminó de tirar")
		}
		g.Step(5)
		got := b.itemTotals()
		if got["dirt"] != 30 || got["cooked_beef"] != 5 {
			t.Errorf("le queda %v; se esperaba dirt 30 y cooked_beef 5", got)
		}
	})
}
