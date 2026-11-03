package pathing

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/path"
	"github.com/mj41/go-mc26-kit/bot/world"
)

func bp(x, y, z int) world.BlockPos { return world.BlockPos{X: x, Y: y, Z: z} }

func line(x0, x1, z int) []world.BlockPos {
	var out []world.BlockPos
	for x := x0; x <= x1; x++ {
		out = append(out, bp(x, 64, z))
	}
	return out
}

func TestLoopFree(t *testing.T) {
	in := []world.BlockPos{bp(0, 64, 0), bp(1, 64, 0), bp(2, 64, 0), bp(2, 64, 1), bp(1, 64, 0), bp(1, 64, 1)}
	got := loopFree(in)
	want := []world.BlockPos{bp(0, 64, 0), bp(1, 64, 0), bp(1, 64, 1)}
	if len(got) != len(want) {
		t.Fatalf("%v, se esperaba %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%v, se esperaba %v", got, want)
		}
	}
}

// TestMemoryInvalid: una ruta recordada que ya no se puede andar (un muro
// nuevo en medio) no se usa; la de un sitio de en medio a otro, sí.
func TestMemoryInvalid(t *testing.T) {
	w := mapWorld{}
	m := NewMemory()
	m.remember(line(0, 12, 0))
	g := newGraph(w, nil)
	start := Node{Pos: bp(2, 64, 0), Floor: 64, Kind: path.Walk}
	res, ok := m.reuse(g, start, bp(10, 64, 0), 0)
	if !ok || len(res.path) != 8 || res.path[7].Pos != bp(10, 64, 0) {
		t.Fatalf("tramo de en medio: ok=%v %v", ok, res.path)
	}
	if _, ok := m.reuse(g, Node{Pos: bp(10, 64, 0), Floor: 64}, bp(1, 64, 0), 0); !ok {
		t.Error("al revés también vale")
	}
	w.fill(6, 64, 0, 6, 65, 0, stoneID)
	if _, ok := m.reuse(newGraph(w, nil), start, bp(10, 64, 0), 0); ok {
		t.Error("con un muro en medio no vale")
	}
	if _, ok := m.reuse(newGraph(w, nil), start, bp(5, 70, 9), 0); ok {
		t.Error("una meta que no está en ninguna ruta")
	}
}

// TestMemorySnagDetour: una traba apuntada en el camino recto hace que la
// búsqueda dé la vuelta por otro lado.
func TestMemorySnagDetour(t *testing.T) {
	w := mapWorld{}
	start := Node{Pos: bp(0, 64, 0), Floor: 64, Kind: path.Walk}
	find := func(m *Memory) []Node {
		g := newGraph(w, nil)
		g.penalty = m.penalties()
		r, err := g.find(context.Background(), start, bp(8, 64, 0), 0, 20000)
		if err != nil {
			t.Fatal(err)
		}
		return r.path
	}
	on := func(p []Node, c world.BlockPos) bool {
		for _, n := range p {
			if n.Pos == c {
				return true
			}
		}
		return false
	}
	m := NewMemory()
	if !on(find(m), bp(4, 64, 0)) {
		t.Fatal("sin trabas va recto")
	}
	for z := -1; z <= 1; z++ {
		m.snag(bp(4, 64, z))
		m.snag(bp(4, 64, z))
	}
	if p := find(m); on(p, bp(4, 64, 0)) {
		t.Errorf("con trabas sigue pasando por ellas: %v", p)
	}
}

func TestMemorySaveLoad(t *testing.T) {
	m := NewMemory()
	m.remember(line(0, 10, 0))
	m.snag(bp(3, 64, 3))
	f := filepath.Join(t.TempDir(), "caminos.json")
	if err := m.Save(f); err != nil {
		t.Fatal(err)
	}
	n := NewMemory()
	if err := n.Load(f); err != nil {
		t.Fatal(err)
	}
	if n.Routes() != 1 || n.Snags() != 1 {
		t.Errorf("cargado: %d rutas, %d trabas", n.Routes(), n.Snags())
	}
	if err := NewMemory().Load(filepath.Join(t.TempDir(), "no")); err != nil {
		t.Errorf("sin archivo: %v", err)
	}
}
