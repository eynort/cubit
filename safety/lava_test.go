package safety

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/world"
)

// TestThinkBeforePhysics: al conectar, el controlador piensa antes del primer
// paso de la física (su jugador simulado aún es nil) y sin chunks cargados:
// el reflejo no hace nada, y sobre todo no se rompe.
func TestThinkBeforePhysics(t *testing.T) {
	c := bot.NewClient()
	p := basic.NewPlayer(c, basic.DefaultSettings, basic.EventsListener{})
	w := world.NewWorld(c, p, world.EventsListener{})
	l := &Lava{World: w, Mover: &physics.Mover{World: w}}
	var s control.State
	if l.Think(&s, p) {
		t.Errorf("tomó el control sin lava")
	}
}
