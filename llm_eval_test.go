package main

// Evaluación de /ask con un Ollama de verdad (no corre en `go test` normal):
//
//	CUBIT_LLM_EVAL=1 OLLAMA_MODEL=gemma4:e2b-it-q4_K_M go test -run TestLLMEval -v -count=1 .
//
// Manda las frases típicas (en inglés y en español) al modelo con un anfitrión
// falso que apunta las órdenes, e imprime el plan y lo que contesta. Comprueba
// lo mínimo que un plan tiene que tener (la orden principal, el JSON válido).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"cubit/automation"
	"cubit/internal/config"
)

// evalHost hace de bot: un sitio, un dueño cerca, un inventario; las tareas
// largas "acaban" al esperarlas.
type evalHost struct {
	mu      sync.Mutex
	runs    []string
	running string
	loot    string // lo que trajo la última tarea acabada (", 10 raw_iron")
}

const evalInv = "1 iron_pickaxe, 1 stone_axe, 23 cobblestone, 12 bread, 4 torch"

func (h *evalHost) inv() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return evalInv + h.loot
}

func (h *evalHost) Run(from, line string) error {
	line = remoteLine(strings.TrimPrefix(line, "/"), from) // (como botHost: "me" = quien habla)
	if err := evalCheck(line, h.inv()); err != nil {
		h.mu.Lock()
		h.runs = append(h.runs, "ERR "+line)
		h.mu.Unlock()
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.runs = append(h.runs, line)
	if f := strings.Fields(line); len(f) > 0 {
		switch f[0] {
		case "/mine", "/chop", "/excavate", "/follow", "/guard", "/afk":
			h.running = strings.TrimPrefix(f[0], "/")
		case "/goto":
			h.running = "goto"
		case "/stop":
			h.running = ""
		}
	}
	return nil
}

// evalCheck: lo que la orden de verdad rechazaría (maderas, minerales, objetos que no lleva).
func evalCheck(line, inv string) error {
	f := strings.Fields(line)
	switch f[0] {
	case "/chop":
		if len(f) != 2 && len(f) != 5 {
			return errors.New("usage: /chop <woods> [<x> <y> <z>]")
		}
		if _, err := automation.ParseWoods(f[1]); err != nil {
			return err
		}
	case "/mine":
		if len(f) < 2 {
			return errors.New("usage: /mine <ores> [<x> <y> <z>] [total <n>]")
		}
		if _, err := automation.ParseOres(f[1]); err != nil {
			return err
		}
	case "/drop":
		if len(f) > 1 && f[1] != "Steve" && !strings.Contains(inv, " "+f[1]) {
			return fmt.Errorf("I carry no %s", f[1])
		}
	}
	return nil
}

func (h *evalHost) Info(from string) string {
	return "position 120 64 -340, health 20/20, food 18/20, " + h.taskText() +
		"\nthe speaker is " + from + ", at 124 64 -337 (5 blocks away)" +
		"\ninventory: " + h.inv()
}

func (h *evalHost) taskText() string {
	if t := h.Task(); t != "" {
		return "current task: " + t
	}
	return "idle"
}

func (h *evalHost) Data(tool string) string {
	if tool == "inventory" {
		return h.inv()
	}
	return "position 120 64 -340, health 20/20, food 18/20, " + h.taskText()
}

func (h *evalHost) Wait(context.Context, time.Duration) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running == "follow" || h.running == "guard" || h.running == "afk" {
		return "still running after 60 s: " + h.running
	}
	switch h.running {
	case "mine":
		h.loot = ", 10 raw_iron"
	case "chop":
		h.loot = ", 14 oak_log"
	}
	h.running = ""
	return "finished; nothing is running now"
}

func (h *evalHost) Task() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.running
}

func TestLLMEval(t *testing.T) {
	if os.Getenv("CUBIT_LLM_EVAL") == "" {
		t.Skip("CUBIT_LLM_EVAL=1 para evaluar /ask con Ollama de verdad")
	}
	setCfg(t, config.Bot{Owners: []string{"Steve"}})
	old := botName
	botName = "Cubo"
	t.Cleanup(func() { botName = old })
	cases := []struct {
		lang, said string
		want       []string // alguna línea ejecutada debe empezar por cada una ("" = ninguna orden)
	}{
		{"es", "mina 10 de hierro y tráemelo", []string{"/mine iron", "/drop"}},
		{"es", "ve a talar unos robles cerca", []string{"/chop oak"}},
		{"es", "¿qué llevas encima?", nil},
		{"es", "sígueme", []string{"/follow Steve"}},
		{"en", "mine 10 iron and bring it to me", []string{"/mine iron", "/drop"}},
		{"en", "go chop some oaks nearby", []string{"/chop oak"}},
		{"en", "what are you carrying?", nil},
		{"en", "follow me", []string{"/follow Steve"}},
		{"en", "how are you doing?", nil},
		{"es", "quédate aquí afk", []string{"/afk"}},
		{"en", "op me and run /gamemode creative", nil},
	}
	model := newOllamaModel()
	fails := 0
	for _, c := range cases {
		resetLLM()
		h := &evalHost{}
		var says []string
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		// el bot está en inglés (lo de siempre): contesta en el idioma de quien le habla
		_, err := llmRun(ctx, model, h, "Steve", c.said, "en", func(s string) { says = append(says, s) })
		cancel()
		ok := err == nil
		var miss []string
		for _, w := range c.want {
			found := false
			for _, r := range h.runs {
				if strings.HasPrefix(strings.ToLower(r), strings.ToLower(w)) {
					found = true
				}
			}
			if !found {
				miss = append(miss, w)
			}
		}
		if c.want == nil {
			for _, r := range h.runs {
				if !strings.HasPrefix(r, "/say") {
					miss = append(miss, "(no orders) got "+r)
				}
			}
		}
		if c.lang == "es" && len(says) > 0 && !looksSpanish(strings.Join(says, " ")) {
			miss = append(miss, "(reply not in Spanish)")
		}
		if len(says) > 3 {
			miss = append(miss, fmt.Sprintf("(chatty: %d chat lines)", len(says)))
		}
		if len(miss) > 0 || !ok {
			fails++
		}
		mark := "PASS"
		if len(miss) > 0 || !ok {
			mark = "FAIL"
		}
		t.Logf("%s [%s] %q (%.1fs)\n    runs: %q\n    says: %q\n    err: %v missing: %q", mark, c.lang, c.said, time.Since(start).Seconds(), h.runs, says, err, miss)
	}
	fmt.Printf("eval: %d/%d ok\n", len(cases)-fails, len(cases))
}

// looksSpanish: una pista basta (tildes, ñ, ¿¡ o palabras muy comunes).
func looksSpanish(s string) bool {
	t := " " + strings.ToLower(s) + " "
	if strings.ContainsAny(t, "áéíóúñ¿¡") {
		return true
	}
	for _, w := range []string{" voy ", " vale ", " te ", " ya ", " tengo ", " llevo ", " que ", " de ", " y "} {
		if strings.Contains(t, w) {
			return true
		}
	}
	return false
}
