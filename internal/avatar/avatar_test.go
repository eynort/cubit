package avatar

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

// TestAvatarMatchesGame: sin cuentas premium la cabeza es la generada (la
// skin de Mojang de ese nombre no es la que se ve en el juego); con premium,
// la URL pide la de verdad.
func TestAvatarMatchesGame(t *testing.T) {
	if u := URL("Vegetta777", false); u != "/avatar/Vegetta777" {
		t.Errorf("sin premium: %s", u)
	}
	if u := URL("Vegetta777", true); u != "/avatar/Vegetta777?skin=real" {
		t.Errorf("premium: %s", u)
	}
	rec := httptest.NewRecorder()
	Serve(rec, httptest.NewRequest("GET", "/avatar/Vegetta777", nil), "Vegetta777")
	if !bytes.Equal(rec.Body.Bytes(), encodeFace(madeUpFace("Vegetta777"))) {
		t.Error("sin ?skin=real tenía que dar la cabeza generada")
	}
}
