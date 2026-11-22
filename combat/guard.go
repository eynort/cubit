package combat

import (
	"github.com/google/uuid"
	"github.com/mj41/go-mc26-kit/bot/entities"
)

// guarded es el jugador al que el bot protege (la orden "guard").
type guarded struct {
	id   uuid.UUID
	name string
}

// SetGuard hace al bot guardaespaldas de un jugador: quien le pegue pasa a ser
// enemigo. Con uuid.Nil deja de protegerlo.
func (f *Fighter) SetGuard(id uuid.UUID, name string) {
	if id == uuid.Nil {
		if g := f.guard.Swap(nil); g != nil {
			f.logT("no longer guarding %s", "ya no protejo a %s", g.name)
		}
		return
	}
	f.guard.Store(&guarded{id: id, name: name})
	f.logT("guarding %s: whoever hits them deals with me", "protegiendo a %s: quien le pegue, se las ve conmigo", name)
}

// Guarding es a quién protege ("" si a nadie).
func (f *Fighter) Guarding() string {
	if g := f.guard.Load(); g != nil {
		return g.name
	}
	return ""
}

// isGuarded dice si la entidad es el jugador protegido.
func (f *Fighter) isGuarded(e entities.Entity) bool {
	g := f.guard.Load()
	return g != nil && e.UUID == g.id
}

// defend: alguien (cause) le hizo daño al protegido (victim). Si es un
// jugador (y no el protegido ni el bot), pasa a ser enemigo.
func (f *Fighter) defend(victim, cause int32) {
	g := f.guard.Load()
	if g == nil || cause < 0 || cause == int32(f.d.Player.Login.PlayerID) {
		return
	}
	v, ok := f.d.Entities.Get(victim)
	if !ok || v.UUID != g.id {
		return
	}
	e, ok := f.d.Entities.Get(cause)
	if !ok || e.UUID == g.id {
		return
	}
	// jugador o mob: quien le pegue al protegido
	f.engage(e, "le pegó a "+g.name+": lo defiendo", false)
}
