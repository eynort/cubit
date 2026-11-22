package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"testing"

	"cubit/automation"
	"cubit/internal/i18n"
	"cubit/pathing"
)

// withLang ejecuta f con el bot en el idioma l.
func withLang(l string, f func()) {
	old := curLang.Load()
	curLang.Store(&l)
	defer curLang.Store(old)
	f()
}

// TestEnglishTaskNames: con /lang en, los nombres de las tareas (los de
// /status, el panel y el chat) y los motivos de un camino fallido no llevan
// español; con /lang es, siguen en español.
func TestEnglishTaskNames(t *testing.T) {
	names := []string{"talar", "excavar", "minar", "recoger", "afk"}
	withLang("en", func() {
		if i18n.Lang() != "en" {
			t.Fatalf("i18n.Lang() = %q con el bot en inglés", i18n.Lang())
		}
		for _, n := range names {
			for _, s := range []string{automation.Label(n), taskName(n)} {
				if w := i18n.SpanishWords(s); len(w) > 0 {
					t.Errorf("%s: %q lleva español %v", n, s, w)
				}
			}
		}
		for _, st := range []string{"failed: no hay camino", "failed: no me puedo acercar más", "failed: no llego a 1 2 3", "failed: atascado antes de 1 2 3"} {
			if w := i18n.SpanishWords(pathing.StatusText(st)); len(w) > 0 {
				t.Errorf("%q → %q lleva español %v", st, pathing.StatusText(st), w)
			}
		}
		err := i18n.Errorf("can't reach %v", "no llego a %v", 3)
		if s := errText(err); s != "can't reach 3" {
			t.Errorf("errText en inglés: %q", s)
		}
	})
	withLang("es", func() {
		if automation.Label("talar") != "talar" || taskName("talar") != "talar" {
			t.Errorf("en español: %q, %q", automation.Label("talar"), taskName("talar"))
		}
		if pathing.StatusText("failed: no hay camino") != "failed: no hay camino" {
			t.Error("el estado del walker cambió en español")
		}
	})
}

// TestEnglishStatus: /status de un bot en inglés (con una tarea en marcha) no
// lleva español; en español, sí.
func TestEnglishStatus(t *testing.T) {
	b, _ := gotoBot(t, nil)
	b.Tasks = automation.New(automation.Deps{})
	release, set := make(chan struct{}), make(chan struct{})
	b.Tasks.Start(context.Background(), "minar", func(j *automation.Job) error {
		j.StatusT("mining %s (%d/%d)", "minando %s (%d/%d)", "iron", 1, 20)
		close(set)
		<-release
		return nil
	})
	defer b.Tasks.Stop()
	defer close(release)
	<-set
	status := func() string {
		var buf bytes.Buffer
		log.SetOutput(&buf)
		defer log.SetOutput(os.Stderr)
		if err := cmdStatus(b, nil); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	withLang("en", func() {
		out := status()
		if w := i18n.SpanishWords(out); len(w) > 0 {
			t.Errorf("/status en inglés lleva español %v:\n%s", w, out)
		}
		if !strings.Contains(out, "mine: mining iron (1/20)") || !strings.Contains(out, "idle") {
			t.Errorf("/status:\n%s", out)
		}
	})
	withLang("es", func() {
		if out := status(); !strings.Contains(out, "minar: minando iron (1/20)") || !strings.Contains(out, "tranquilo") {
			t.Errorf("/status en español:\n%s", out)
		}
	})
}

// TestOllamaDownLanguage: sin Ollama, la frase sale en el idioma de quien
// habla si se nota; si no, en el del bot.
func TestOllamaDownLanguage(t *testing.T) {
	down := &llmError{Kind: "down", Err: errors.New("x")}
	withLang("en", func() {
		if s := llmFriendly(down, speakerLang("¿puedes traerme hierro?")); s != llmFriendly(down, "es") {
			t.Errorf("a uno que habla español: %q", s)
		}
		if s := llmFriendly(down, speakerLang("ok")); s != llmFriendly(down, "en") {
			t.Errorf("sin saber el idioma, el del bot (inglés): %q", s)
		}
	})
	withLang("es", func() {
		if s := llmFriendly(down, speakerLang("can you bring me some iron?")); s != llmFriendly(down, "en") {
			t.Errorf("a uno que habla inglés: %q", s)
		}
	})
	for said, want := range map[string]string{
		"mina 20 de hierro y tráemelo": "es",
		"ven aquí":                     "es",
		"come here please":             "en",
		"bring me the iron":            "en",
		"ok":                           "",
	} {
		if got := speakerLang(said); got != want {
			t.Errorf("speakerLang(%q) = %q, quería %q", said, got, want)
		}
	}
}
