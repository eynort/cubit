package main

import (
	"net/http/httptest"
	"testing"
)

func TestPanelLanguage(t *testing.T) {
	err := bi("invalid name %q", "nombre %q inválido", "a b")
	for lang, want := range map[string]string{"": `invalid name "a b"`, "en": `invalid name "a b"`, "es": `nombre "a b" inválido`, "fr": `invalid name "a b"`} {
		r := httptest.NewRequest("POST", "/settings", nil)
		if lang != "" {
			r.Header.Set("X-Lang", lang)
		}
		if got := errText(r, err); got != want {
			t.Errorf("X-Lang %q: %q, quería %q", lang, got, want)
		}
	}
	for _, d := range serverProps {
		if d.LabelEn == "" {
			t.Errorf("%s sin etiqueta en inglés", d.Key)
		}
	}
	for _, g := range gamerules {
		if g.LabelEn == "" {
			t.Errorf("%s sin etiqueta en inglés", g.Name)
		}
	}
}
