package i18n

import (
	"net/http/httptest"
	"testing"
)

func TestPanelLanguage(t *testing.T) {
	err := Errorf("invalid name %q", "nombre %q inválido", "a b")
	for lang, want := range map[string]string{"": `invalid name "a b"`, "en": `invalid name "a b"`, "es": `nombre "a b" inválido`, "fr": `invalid name "a b"`} {
		r := httptest.NewRequest("POST", "/settings", nil)
		if lang != "" {
			r.Header.Set("X-Lang", lang)
		}
		if got := ErrText(r, err); got != want {
			t.Errorf("X-Lang %q: %q, quería %q", lang, got, want)
		}
	}
}
