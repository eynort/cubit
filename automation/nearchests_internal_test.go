package automation

import "testing"

func TestChestDeniedMsg(t *testing.T) {
	for _, m := range []string{"No tienes permiso para abrir este cofre", "This chest is protected by Alex", "You don't have permission to access this container", "[Lands] No puedes acceder a eso aquí", "Hey! Sorry, but you can't open that chest here."} {
		if !chestDeniedMsg.MatchString(m) {
			t.Errorf("no reconoce %q", m)
		}
	}
	for _, m := range []string{"Bienvenido al servidor", "Alex se ha unido", "Hogar establecido."} {
		if chestDeniedMsg.MatchString(m) {
			t.Errorf("reconoce %q", m)
		}
	}
}
