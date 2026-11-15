package automation_test

import (
	"context"
	"testing"
	"time"

	"github.com/mj41/go-mc26-kit/bot/simworld"

	"minecraft-bot/automation"
)

// TestStorageStuckOnce: sin dónde guardar, lo dice por el chat una sola vez
// (aunque falle muchas veces) y sigue trabajando sin guardar, sin esperar a
// que nadie conteste.
func TestStorageStuckOnce(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		defer automation.SetStoreWait(3 * time.Second)()
		g := simworld.Start(t, simworld.Options{})
		r := runner(t, g)
		var said []string
		r.SetSay(func(s string) { said = append(said, s) })
		g.Step(5)
		var first, second, noStore bool
		r.Start(context.Background(), "excavar", func(j *automation.Job) error {
			first = j.StorageStuckForTest("el almacén no me deja guardar cobblestone (almacén: cofre: mesa de trabajo: faltan 4 tablones iguales (y troncos de una misma madera))")
			for i := 0; i < 5; i++ { // vuelve a fallar al guardar
				second = j.StorageStuckForTest("el inventario está lleno")
			}
			noStore = j.NoStore()
			return nil
		})
		g.Until(60*20, func() bool { return r.Status() == "" })
		t.Logf("mensajes %q; reintentar %v %v; sin guardar %v", said, first, second, noStore)
		if len(said) != 1 {
			t.Fatalf("%d mensajes en el chat: tiene que ser uno solo", len(said))
		}
		if len(said[0]) > 256 {
			t.Errorf("el mensaje tiene %d bytes: el servidor lo rechaza (más de 256)", len(said[0]))
		}
		if first || second || !noStore {
			t.Fatalf("sin respuesta tiene que seguir sin guardar")
		}

		// sin esperar: la tarea sigue en el mismo tick
		said = nil
		var retry bool
		took := 0
		r.Start(context.Background(), "excavar", func(j *automation.Job) error {
			retry = j.StorageStuckForTest("el inventario está lleno")
			return nil
		})
		for took = 0; r.Status() != "" && took < 60*20; took++ {
			g.Step(1)
		}
		if len(said) != 1 || retry || took > 20 {
			t.Fatalf("sigue sin esperar: mensajes %d, reintentar %v, %d ticks", len(said), retry, took)
		}

		// cofres desactivados: lo dice una vez y no guarda
		said = nil
		r.SetChests(false)
		defer r.SetChests(true)
		var off bool
		r.Start(context.Background(), "excavar", func(j *automation.Job) error {
			off = j.StoringOffForTest() && j.StoringOffForTest()
			return nil
		})
		g.Until(60*20, func() bool { return r.Status() == "" })
		if !off || len(said) != 1 {
			t.Fatalf("cofres desactivados: sin guardar %v, mensajes %q", off, said)
		}
	})
}
