package combat

import (
	"testing"

	"cubit/internal/i18n"
)

// TestStatusEnglish: con el bot en inglés, el estado del combate (el de
// /status y el panel) no lleva nada en español, en ningún modo ni postura;
// en español sigue como antes. StatusEN (el de la IA) es inglés siempre.
func TestStatusEnglish(t *testing.T) {
	lang := "en"
	i18n.SetLang(func() string { return lang })
	defer i18n.SetLang(nil)
	f := &Fighter{aggressors: map[int32]*aggressor{}}
	for m := idle; m <= acting; m++ {
		for s := stanceEven; s <= stanceAvoid; s++ {
			f.plan.Store(&plan{mode: m, target: 7, stance: s})
			f.aggressors[7] = &aggressor{id: 7, name: "Steve"}
			f.zone.Store(&Zone{MinX: 0, MaxX: 10, MinZ: 0, MaxZ: 10})
			f.guard.Store(&guarded{name: "Alex"})
			lang = "en"
			st := f.Status()
			if w := i18n.SpanishWords(st); len(w) > 0 {
				t.Errorf("modo %d postura %d: %q lleva español %v", m, s, st, w)
			}
			if w := i18n.SpanishWords(f.StatusEN()); len(w) > 0 {
				t.Errorf("StatusEN: %q lleva español %v", f.StatusEN(), w)
			}
			lang = "es"
			if st := f.Status(); len(i18n.SpanishWords(st)) == 0 {
				t.Errorf("en español: %q", st)
			}
			if w := i18n.SpanishWords(f.StatusEN()); len(w) > 0 {
				t.Errorf("StatusEN con el bot en español: %q lleva español %v", f.StatusEN(), w)
			}
		}
	}
	lang = "en"
	f.plan.Store(&plan{mode: idle})
	f.aggressors = map[int32]*aggressor{}
	f.zone.Store(nil)
	f.guard.Store(nil)
	if st := f.Status(); st != "idle" {
		t.Errorf("tranquilo en inglés: %q", st)
	}
}
