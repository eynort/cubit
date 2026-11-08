package automation_test

import (
	"context"
	"testing"
	"time"

	"github.com/mj41/go-mc26-kit/bot/simworld"

	"minecraft-bot/automation"
)

// TestStorageStuckOnce: sin dónde guardar, lo dice por el chat una sola vez
// (aunque falle muchas veces) y, sin respuesta, sigue sin guardar ni recoger;
// si alguien contesta, vuelve a intentarlo.
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

		// con respuesta: vuelve a intentarlo
		said = nil
		var retry bool
		r.Start(context.Background(), "excavar", func(j *automation.Job) error {
			retry = j.StorageStuckForTest("el inventario está lleno")
			return nil
		})
		g.Step(20)
		r.NoteChat() // alguien contesta
		g.Until(60*20, func() bool { return r.Status() == "" })
		if len(said) != 1 || !retry {
			t.Fatalf("con respuesta: mensajes %d, reintentar %v", len(said), retry)
		}
	})
}
