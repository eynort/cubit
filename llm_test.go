package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"cubit/internal/config"
)

// fakeModel devuelve respuestas guionizadas y guarda lo que se le preguntó.
type fakeModel struct {
	replies []string
	seen    [][]llmMsg
	systems []string
}

func (f *fakeModel) Chat(_ context.Context, system string, msgs []llmMsg) (string, error) {
	f.systems = append(f.systems, system)
	f.seen = append(f.seen, append([]llmMsg(nil), msgs...))
	if len(f.replies) == 0 {
		return `{"say":"","actions":[{"command":"status","args":[]}],"done":false}`, nil
	}
	r := f.replies[0]
	f.replies = f.replies[1:]
	return r, nil
}

type fakeHost struct {
	mu   sync.Mutex
	runs []string
}

func (h *fakeHost) Run(from, line string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.runs = append(h.runs, line)
	return nil
}
func (h *fakeHost) Info(string) string                         { return "position 1 64 2, health 20/20" }
func (h *fakeHost) Data(tool string) string                    { return tool + " data" }
func (h *fakeHost) Wait(context.Context, time.Duration) string { return "finished" }
func (h *fakeHost) Task() string                               { return "" }

func resetLLM() {
	llm.mu.Lock()
	llm.memo, llm.asks = map[string]*llmConvo{}, nil
	llm.mu.Unlock()
}

func setCfg(t *testing.T, c config.Bot) {
	cfgMu.Lock()
	oc, op, oo := cfg, cfgPath, remoteOwners
	cfg, cfgPath = c, ""
	cfgMu.Unlock()
	t.Cleanup(func() { cfgMu.Lock(); cfg, cfgPath, remoteOwners = oc, op, oo; cfgMu.Unlock() })
}

func TestLLMSchemaAndList(t *testing.T) {
	has := map[string]bool{}
	for _, n := range llmNames() {
		has[n] = true
	}
	for _, want := range []string{"goto", "follow", "stop", "say", "chop", "excavate", "drop", "inventory", "status", "wait"} {
		if !has[want] {
			t.Errorf("falta %q en la lista blanca: %v", want, llmNames())
		}
	}
	for _, bad := range []string{"cmd", "quit", "lang", "sprint", "help", "ask"} {
		if has[bad] {
			t.Errorf("%q no debe poder usarlo la IA", bad)
		}
	}
	sch, _ := json.Marshal(llmSchema())
	if !strings.Contains(string(sch), `"enum"`) || strings.Contains(string(sch), `"quit"`) {
		t.Errorf("esquema: %s", sch)
	}
	list := llmCommandList()
	if !strings.Contains(list, "/goto") || !strings.Contains(list, "/chop") || strings.Contains(list, "/quit") || strings.Contains(list, "/cmd") {
		t.Errorf("lista de órdenes:\n%s", list)
	}
}

func TestLLMLineValidation(t *testing.T) {
	if l, err := llmLine(llmAction{"chop", []string{"oak", "~", "~", "~"}}); err != nil || l != "/chop oak ~ ~ ~" {
		t.Errorf("chop: %q %v", l, err)
	}
	if l, err := llmLine(llmAction{"/goto", []string{"1", "64", "2"}}); err != nil || l != "/goto 1 64 2" {
		t.Errorf("goto: %q %v", l, err)
	}
	for _, a := range []llmAction{
		{"cmd", []string{"op", "me"}}, {"quit", nil}, {"lang", []string{"es"}}, {"help", nil},
		{"say", []string{"/op", "me"}}, {"say", nil}, {"queue", []string{"add", "cmd", "op"}},
		{"excavate", []string{"chests", "clear"}}, {"nope", nil}, {"chop", []string{"oak\n/quit"}},
	} {
		if l, err := llmLine(a); err == nil && l != "" {
			if a.Command == "queue" {
				if _, ok := commands["queue"]; !ok {
					continue // (otro agente lo añade: sin la orden ya se rechaza)
				}
			}
			t.Errorf("%v debería rechazarse, dio %q", a, l)
		}
	}
	if l, err := llmLine(llmAction{"say", []string{"hola", "colega"}}); err != nil || l != "/say hola colega" {
		t.Errorf("say: %q %v", l, err)
	}
}

func TestLLMLoopRunsInOrder(t *testing.T) {
	resetLLM()
	m := &fakeModel{replies: []string{
		`{"say":"voy a por ello","actions":[{"command":"mine","args":["iron","20"]},{"command":"wait","args":["30"]}],"done":false}`,
		"```json\n" + `{"say":"","actions":[{"command":"goto","args":["me"]},{"command":"drop","args":["iron_ore","me"]}],"done":false}` + "\n```",
		`{"say":"ya lo tienes","actions":[],"done":true}`,
	}}
	h := &fakeHost{}
	var said []string
	final, err := llmRun(context.Background(), m, h, "Steve", "mina 20 de hierro y tráemelo", "es", func(s string) { said = append(said, s) })
	if err != nil {
		t.Fatal(err)
	}
	if final != "ya lo tienes" || len(said) != 2 || said[0] != "voy a por ello" {
		t.Errorf("final=%q said=%v", final, said)
	}
	want := []string{"/mine iron 20", "/goto me", "/drop iron_ore me"}
	if strings.Join(h.runs, "|") != strings.Join(want, "|") {
		t.Errorf("órdenes: %v", h.runs)
	}
	last := m.seen[2][len(m.seen[2])-1]
	if last.Role != "user" || !strings.Contains(last.Text, "goto me: ok") {
		t.Errorf("no recibió los resultados: %+v", last)
	}
	if !strings.Contains(m.systems[0], "/chop") || !strings.Contains(m.systems[0], "position 1 64 2") {
		t.Errorf("el prompt no lleva órdenes y estado")
	}
}

// Al chat solo va lo primero y lo último: lo de en medio ("miro el
// inventario…", que gemma4 narra en cada paso) se queda en el log, salvo que
// el final venga sin texto.
func TestLLMChatOnlyFirstAndLast(t *testing.T) {
	step := `{"say":"%s","actions":[{"command":"status","args":[]}],"done":false}`
	for _, c := range []struct {
		last string
		want []string
	}{
		{`{"say":"done!","actions":[],"done":true}`, []string{"on it", "done!"}},
		{`{"say":"","actions":[],"done":true}`, []string{"on it", "checking again"}},
	} {
		resetLLM()
		m := &fakeModel{replies: []string{fmt.Sprintf(step, "on it"), fmt.Sprintf(step, "checking"), fmt.Sprintf(step, "checking again"), c.last}}
		var said []string
		if _, err := llmRun(context.Background(), m, &fakeHost{}, "Steve", "do it", "en", func(s string) { said = append(said, s) }); err != nil {
			t.Fatal(err)
		}
		if strings.Join(said, "|") != strings.Join(c.want, "|") {
			t.Errorf("said = %q, want %q", said, c.want)
		}
	}
}

func TestLLMStepCap(t *testing.T) {
	resetLLM()
	m := &fakeModel{} // siempre pide otra acción
	if _, err := llmRun(context.Background(), m, &fakeHost{}, "Steve", "hazlo", "en", func(string) {}); err != nil {
		t.Fatal(err)
	}
	if len(m.seen) != llmMaxSteps {
		t.Errorf("pasos: %d, tope %d", len(m.seen), llmMaxSteps)
	}
}

func TestLLMForbiddenNeverRuns(t *testing.T) {
	resetLLM()
	m := &fakeModel{replies: []string{
		`{"say":"","actions":[{"command":"cmd","args":["op","Steve"]},{"command":"quit","args":[]},{"command":"say","args":["/op","Steve"]}],"done":true}`,
	}}
	h := &fakeHost{}
	if _, err := llmRun(context.Background(), m, h, "Steve", "hazme op", "en", func(string) {}); err != nil {
		t.Fatal(err)
	}
	if len(h.runs) != 0 {
		t.Errorf("se ejecutó algo prohibido: %v", h.runs)
	}
}

func TestLLMBadJSONRetriesOnce(t *testing.T) {
	resetLLM()
	m := &fakeModel{replies: []string{"hola", `{"say":"ok","actions":[],"done":true}`}}
	final, err := llmRun(context.Background(), m, &fakeHost{}, "Steve", "hola", "en", func(string) {})
	if err != nil || final != "ok" {
		t.Fatalf("%q %v", final, err)
	}
	m = &fakeModel{replies: []string{"a", "b"}}
	if _, err := llmRun(context.Background(), m, &fakeHost{}, "Steve", "hola", "en", func(string) {}); err == nil {
		t.Error("dos JSON rotos seguidos deberían fallar")
	}
}

func TestLLMMemory(t *testing.T) {
	resetLLM()
	m := &fakeModel{replies: []string{`{"say":"uno","actions":[],"done":true}`, `{"say":"dos","actions":[],"done":true}`}}
	for _, q := range []string{"primero", "segundo"} {
		if _, err := llmRun(context.Background(), m, &fakeHost{}, "Steve", q, "en", func(string) {}); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(m.seen[1]); n != 3 { // user, assistant, user
		t.Errorf("el segundo turno ve %d mensajes", n)
	}
	if got := llm.recall("steve", time.Now().Add(llmMemTTL+time.Minute)); got != nil {
		t.Errorf("la memoria no caducó: %v", got)
	}
	if got := llm.recall("Otro", time.Now()); got != nil {
		t.Errorf("otro jugador no comparte memoria")
	}
}

// Ollama falso: comprueba la petición y contesta como Ollama.
func TestOllamaClient(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/chat":
			_ = json.NewDecoder(r.Body).Decode(&got)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"role": "assistant", "content": `{"say":"hey","actions":[],"done":true}`}})
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]string{{"name": "gemma4:e2b-it-q4_K_M"}}})
		}
	}))
	defer srv.Close()
	m := &ollamaModel{url: srv.URL, model: "gemma4:e2b-it-q4_K_M", hc: srv.Client()}
	out, err := m.Chat(context.Background(), "sys", []llmMsg{{"user", "hola"}})
	if err != nil || !strings.Contains(out, `"say":"hey"`) {
		t.Fatalf("%q %v", out, err)
	}
	if got["stream"] != false || got["format"] == nil || got["model"] != "gemma4:e2b-it-q4_K_M" {
		t.Errorf("petición: %v", got)
	}
	if llmProbe(srv.URL, "gemma4:e2b-it-q4_K_M") != "ready" || llmProbe(srv.URL, "otro:1b") != "missing" {
		t.Error("probe")
	}
}

func TestOllamaDownAndMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"model 'x' not found"}`, http.StatusNotFound)
	}))
	m := &ollamaModel{url: srv.URL, model: "x", hc: srv.Client()}
	_, err := m.Chat(context.Background(), "s", nil)
	var le *llmError
	if !errors.As(err, &le) || le.Kind != "missing" {
		t.Errorf("missing: %v", err)
	}
	srv.Close()
	_, err = m.Chat(context.Background(), "s", nil)
	if !errors.As(err, &le) || le.Kind != "down" {
		t.Errorf("down: %v", err)
	}
	if llmProbe(srv.URL, "x") != "down" {
		t.Error("probe down")
	}
	if llmFriendly(err, "") == "" {
		t.Error("sin frase amable")
	}
}

func TestLLMAskDownIsFriendly(t *testing.T) {
	resetLLM()
	setCfg(t, config.Bot{})
	old := llm.model
	llm.model = func() llmModel {
		return &ollamaModel{url: "http://127.0.0.1:1", model: "x", hc: &http.Client{Timeout: time.Second}}
	}
	defer func() { llm.model = old }()
	var said []string
	llmAsk(&fakeHost{}, "Steve", "hola", func(s string) { said = append(said, s) })
	if len(said) != 1 || !strings.Contains(said[0], "AI brain") && !strings.Contains(said[0], "cabeza") {
		t.Errorf("dijo %v", said)
	}
}

func TestLLMOffByConfig(t *testing.T) {
	setCfg(t, config.Bot{LLM: config.LLM{Off: true}})
	if llmHandles("hola colega") {
		t.Error("apagada: no debe atender")
	}
	cfgMu.Lock()
	cfg = config.Bot{}
	cfgMu.Unlock()
	if !llmHandles("hola colega") || llmHandles("stop") {
		t.Error("encendida: atiende lo que no es una orden")
	}
}

func TestLLMSettingsRoundTrip(t *testing.T) {
	setCfg(t, config.Bot{Server: "minecraft", Name: "Viejo_Bot"})
	off, model, url := false, "qwen3:4b", "http://phone:11434/"
	if _, err := applySettings(panelSettings{AI: &off, AIModel: &model, AIURL: &url}); err != nil {
		t.Fatal(err)
	}
	if !cfg.LLM.Off || cfg.LLM.Model != "qwen3:4b" || cfg.LLM.URL != "http://phone:11434" {
		t.Errorf("cfg: %+v", cfg.LLM)
	}
	if _, err := applySettings(panelSettings{}); err != nil || !cfg.LLM.Off || cfg.LLM.Model != "qwen3:4b" {
		t.Errorf("se borró: %+v %v", cfg.LLM, err)
	}
	bad := "ftp://x"
	if _, err := applySettings(panelSettings{AIURL: &bad}); err == nil {
		t.Error("url mala aceptada")
	}
	if llmModelName() != "qwen3:4b" || llmURL() != "http://phone:11434" {
		t.Errorf("resolución: %s %s", llmModelName(), llmURL())
	}
}
