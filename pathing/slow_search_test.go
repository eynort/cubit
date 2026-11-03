package pathing

import (
	"context"
	"testing"
	"time"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// valley: un valle hondo entre el bot y una meta encerrada (inalcanzable): la
// búsqueda mira todos los nodos que puede, la peor para el tiempo.
func valley() mapWorld {
	w := mapWorld{}
	air := block.ToStateID[block.Air{}]
	w.fill(3, 20, -40, 60, 63, 40, air)
	w.fill(78, 64, -2, 82, 70, 2, block.ToStateID[block.Bedrock{}])
	w.fill(79, 64, -1, 81, 69, 1, air)
	return w
}

// TestSearchBudget: una búsqueda lenta (en los logs, más de un minuto quieto
// esperándola siguiendo a alguien lejos) acaba a su tiempo con el camino
// hacia lo más cercano que encontró, para ir acercándose.
func TestSearchBudget(t *testing.T) {
	g := newGraph(valley(), nil)
	g.build, g.breakSoft = true, true
	g.jump, g.health, g.fallBudget = maxJump, 20, 12
	g.budget = 50 * time.Millisecond
	start, _ := g.start(0.5, 64, 0.5)
	t0 := time.Now()
	r, err := g.find(context.Background(), start, world.BlockPos{X: 80, Y: 64, Z: 0}, 1.5, 1<<30)
	d := time.Since(t0)
	t.Logf("%v: %d nodos mirados, %d de camino, sin tiempo=%v, err=%v", d, r.searched, len(r.path), r.timedOut, err)
	if err != nil || !r.timedOut || len(r.path) == 0 {
		t.Fatalf("se esperaba un camino parcial por tiempo: err=%v timedOut=%v camino=%d", err, r.timedOut, len(r.path))
	}
	if d > 500*time.Millisecond {
		t.Errorf("tardó %v con un presupuesto de 50 ms", d)
	}
	if end := r.path[len(r.path)-1].Pos; end.X < 3 {
		t.Errorf("el camino parcial no se acerca: acaba en %v", end)
	}
}
