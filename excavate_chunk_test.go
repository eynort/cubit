package main

import (
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/world"

	"cubit/automation"
)

func TestChunkBounds(t *testing.T) {
	for _, c := range []struct{ v, chunk, lo, hi int }{{176, 11, 176, 191}, {191, 11, 176, 191}, {-16, -1, -16, -1}, {-17, -2, -32, -17}, {-31, -2, -32, -17}, {0, 0, 0, 15}, {-1, -1, -16, -1}} {
		ch := chunkOf(c.v)
		lo, hi := chunkBounds(ch)
		if ch != c.chunk || lo != c.lo || hi != c.hi {
			t.Errorf("%d: chunk %d (%d…%d), quería %d (%d…%d)", c.v, ch, lo, hi, c.chunk, c.lo, c.hi)
		}
	}
}

func TestSnapToChunks(t *testing.T) {
	box := func(x1, z1, x2, z2 int) automation.Box {
		return automation.NewBox(world.BlockPos{X: x1, Y: 0, Z: z1}, world.BlockPos{X: x2, Y: 90, Z: z2})
	}
	for _, c := range []struct {
		in, want automation.Box
	}{
		{box(176, -16, 191, -31), box(176, -17, 191, -32)}, // lo del usuario: chunk 11, -2
		{box(176, -48, 191, -63), box(176, -49, 191, -64)}, // y el otro: chunk 11, -4
		{box(176, -49, 191, -64), box(176, -49, 191, -64)}, // ya justo: igual
		{box(-15, 0, 0, 15), box(-16, 0, -1, 15)},          // X negativa, del otro lado
		{box(1, 1, 16, 16), box(0, 0, 15, 15)},             // positivos corridos +1
		{box(-63, -63, -32, -32), box(-64, -64, -33, -33)}, // 2×2 chunks
		{box(3, 3, 12, 30), box(3, 3, 12, 30)},             // una caja cualquiera: igual
		{box(-5, -5, 10, 10), box(-5, -5, 10, 10)},         // 16 de ancho pero no a un bloque del borde: igual
	} {
		got, note := snapToChunks(c.in)
		if got != c.want {
			t.Errorf("%s → %s, quería %s", c.in, got, c.want)
		}
		if (got != c.in) != (note != "") {
			t.Errorf("%s: aviso %q", c.in, note)
		}
		if note != "" {
			t.Log(note)
		}
	}
}

func TestExcavateChunkSuggestions(t *testing.T) {
	var ls []string
	for _, s := range suggestions("/excavate ") {
		ls = append(ls, s.label)
	}
	t.Log(ls)
	if !strings.Contains(strings.Join(ls, "|"), "chunk") {
		t.Errorf("sin chunk: %v", ls)
	}
}
