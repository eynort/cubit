package pathing_test

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/packetid"
)

// TestPacketsPerTick sube la escalera y cuenta lo que el bot manda en cada
// tick: el cliente vanilla manda como mucho, por tick, una entrada de teclas,
// un cambio de sprint, un paquete de movimiento y el fin de tick.
func TestPacketsPerTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := fakeserver.StartMem(t)
		ids := map[packetid.ServerboundPacketID]string{
			packetid.ServerboundPlayMovePlayerPos:        "pos",
			packetid.ServerboundPlayMovePlayerPosRot:     "posrot",
			packetid.ServerboundPlayMovePlayerRot:        "rot",
			packetid.ServerboundPlayMovePlayerStatusOnly: "status",
			packetid.ServerboundPlayPlayerInput:          "input",
			packetid.ServerboundPlayPlayerCommand:        "sprint",
			packetid.ServerboundPlayClientTickEnd:        "fin",
		}
		type ev struct{ kind string }
		var (
			mu     sync.Mutex
			all    []ev
			record bool
		)
		// en el orden en que llegan (no por la hora: dos pueden llegar a la vez)
		s.Hook = func(p fakeserver.Packet) {
			mu.Lock()
			defer mu.Unlock()
			if name, ok := ids[p.ID]; ok && record {
				all = append(all, ev{name})
			}
		}
		stairs(s.World)
		b := fakeserver.JoinStepped(t, s, "Tester")
		if err := b.Loaded(3 * time.Second); err != nil {
			t.Fatal(err)
		}
		b.Mover.Food = func() int { return 20 }
		w := ourWalker(b)
		b.Ctl.Think = w.Think
		b.Step(1)
		mu.Lock()
		record = true // desde un fin de tick: los grupos empiezan alineados
		mu.Unlock()
		w.Goto(world.BlockPos{X: 11, Y: 70, Z: 0})
		if !b.Until(600, func() bool { return w.Status() == "arrived" }) {
			t.Fatalf("no llegó: %s", w.Status())
		}
		b.Step(5)
		mu.Lock()
		defer mu.Unlock()
		ticks, maxMove, maxTotal, total := 0, 0, 0, map[string]int{}
		cur := map[string]int{}
		n := 0
		for _, e := range all {
			total[e.kind]++
			if e.kind == "fin" {
				ticks++
				m := cur["pos"] + cur["posrot"] + cur["rot"] + cur["status"]
				maxMove, maxTotal = max(maxMove, m), max(maxTotal, n)
				if cur["input"] > 1 || cur["sprint"] > 1 || m > 1 {
					t.Errorf("tick %d: %v", ticks, cur)
				}
				cur, n = map[string]int{}, 0
				continue
			}
			cur[e.kind]++
			n++
		}
		t.Logf("%d ticks; por tick como mucho: %d de movimiento, %d paquetes en total (sin el fin de tick)", ticks, maxMove, maxTotal)
		t.Logf("totales: %v", total)
	})
}
