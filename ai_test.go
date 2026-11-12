package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

// fakeOllama responde como /api/chat con las respuestas de reply (por orden) y
// guarda lo que le llegó.
func fakeOllama(t *testing.T, reply func(n int, req map[string]any) map[string]any) (*httptest.Server, *[]map[string]any) {
	var mu sync.Mutex
	var got []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("petición: %v", err)
		}
		mu.Lock()
		got = append(got, req)
		n := len(got)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(reply(n, req))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func msgResp(content string, calls ...map[string]any) map[string]any {
	m := map[string]any{"role": "assistant", "content": content}
	if len(calls) > 0 {
		m["tool_calls"] = calls
	}
	return map[string]any{"message": m, "prompt_eval_count": 100, "eval_count": 10, "done": true}
}

func TestAIToolRoundTrip(t *testing.T) {
	srv, got := fakeOllama(t, func(n int, req map[string]any) map[string]any {
		if n == 1 {
			return msgResp("", map[string]any{"function": map[string]any{"name": "get_status", "arguments": map[string]any{}}})
		}
		return msgResp("Estoy **bien**,\n- con la vida llena.")
	})
	b := &Bot{AI: newAI(srv.URL, "gemma4:e4b", 8192), ctx: context.Background()}
	reply, err := b.aiAsk(consoleWho, "¿cómo estás?")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Estoy bien, con la vida llena." {
		t.Errorf("respuesta %q", reply)
	}
	if len(*got) != 2 {
		t.Fatalf("%d peticiones, quería 2", len(*got))
	}
	second := (*got)[1]
	if second["options"].(map[string]any)["num_ctx"].(float64) != 8192 {
		t.Errorf("num_ctx no llegó: %v", second["options"])
	}
	msgs := second["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)
	if last["role"] != "tool" || last["tool_name"] != "get_status" {
		t.Errorf("el resultado de la herramienta no llegó: %v", last)
	}
	if sys := msgs[0].(map[string]any)["content"].(string); !strings.Contains(sys, "/goto") || !strings.Contains(sys, "/chop") {
		t.Errorf("el system no trae las órdenes del bot:\n%s", sys)
	}
}

func TestAINonOwnerHasNoTools(t *testing.T) {
	srv, got := fakeOllama(t, func(int, map[string]any) map[string]any { return msgResp("hola!") })
	b := &Bot{AI: newAI(srv.URL, "m", 8192), ctx: context.Background()}
	if _, err := b.aiAsk("UnDesconocido", "sígueme"); err != nil {
		t.Fatal(err)
	}
	req := (*got)[0]
	if _, ok := req["tools"]; ok {
		t.Error("un jugador que no es dueño no debe recibir herramientas")
	}
	// y aunque el modelo intente ejecutar algo, no se ejecuta
	if out := b.aiTool("UnDesconocido", false, aiFunc{Name: "run_command", Arguments: map[string]any{"command": "/quit"}}); !strings.HasPrefix(out, "error") {
		t.Errorf("ejecutó una orden de alguien sin permiso: %q", out)
	}
}

func TestAIAllowed(t *testing.T) {
	for cmd, ok := range map[string]bool{
		"/goto 1 2": true, "/chop oak 10": true, "/spawn": true, "/cmd home": true, "/tpa Alex": true,
		"/quit": false, "/ai hola": false, "/login x": false, "/cmd plugins": false, "/minecraft:op x": false, "/gamemode creative": false,
	} {
		if err := aiAllowed(cmd); (err == nil) != ok {
			t.Errorf("aiAllowed(%q) = %v, quería permitida=%v", cmd, err, ok)
		}
	}
}

func TestAICompact(t *testing.T) {
	srv, got := fakeOllama(t, func(int, map[string]any) map[string]any { return msgResp("Alex pidió madera; se taló roble.") })
	a := newAI(srv.URL, "m", 2048)
	c := &aiConv{who: "Alex"}
	long := strings.Repeat("palabra ", 200)
	for i := 0; i < 10; i++ {
		c.msgs = append(c.msgs,
			aiMsg{Role: "user", Content: "Alex: " + long},
			aiMsg{Role: "assistant", ToolCalls: []aiToolCall{{Function: aiFunc{Name: "run_command", Arguments: map[string]any{"command": "/chop oak"}}}}},
			aiMsg{Role: "tool", ToolName: "run_command", Content: "ok"},
			aiMsg{Role: "assistant", Content: "hecho"})
	}
	before := a.estimate("", c.msgs)
	if err := a.compact(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if c.summary == "" || c.compacts != 1 {
		t.Errorf("sin resumen: %+v", c)
	}
	if c.msgs[0].Role != "user" {
		t.Errorf("lo que queda debe empezar por el usuario, empieza por %s", c.msgs[0].Role)
	}
	after := a.estimate("", c.msgs)
	if after >= before || len(c.msgs) != 4 { // cada turno ya pasa de aiKeepTail: queda solo el último
		t.Errorf("no compactó bastante: %d → %d tokens, %d mensajes", before, after, len(c.msgs))
	}
	// el resumen va al system en la siguiente petición
	b := &Bot{AI: a}
	if !strings.Contains(b.aiSystem(c, true), "se taló roble") {
		t.Error("el resumen no está en el system")
	}
	if len(*got) == 0 {
		t.Error("no se pidió el resumen")
	}
	for _, req := range *got {
		if req["tools"] != nil {
			t.Error("el resumen se pide sin herramientas")
		}
	}
}

// sentTokens es lo que ocupa una petición a Ollama: sus mensajes (el system
// incluido) estimados como lo estima la IA, más la respuesta que pide.
func sentTokens(a *AI, req map[string]any) int {
	var sys string
	var msgs []aiMsg
	raw, _ := json.Marshal(req["messages"])
	var all []aiMsg
	_ = json.Unmarshal(raw, &all)
	for _, m := range all {
		if m.Role == "system" {
			sys = m.Content
		} else {
			msgs = append(msgs, m)
		}
	}
	opts, _ := req["options"].(map[string]any)
	predict, _ := opts["num_predict"].(float64)
	return a.estimate(sys, msgs) + int(predict)
}

// TestAIBudgetNeverExceeded: una charla larga (mensajes largos, respuestas
// largas, resultados de herramientas enormes en el historial): ninguna
// petición al modelo, ni las de resumir, pasa del 30 % del contexto, y la
// conversación se compacta (como el autocompact de Claude Code).
func TestAIBudgetNeverExceeded(t *testing.T) {
	var a *AI
	srv, got := fakeOllama(t, func(n int, req map[string]any) map[string]any {
		r := msgResp(strings.Repeat("vale bro te cuento ", 30))
		r["prompt_eval_count"] = sentTokens(a, req) * 88 / 100 // (como el de verdad: la estimación se pasa un poco)
		return r
	})
	a = newAI(srv.URL, "m", 16384)
	b := &Bot{AI: a, ctx: context.Background()}
	for i := 0; i < 40; i++ {
		if i%5 == 0 { // un resultado de herramienta enorme de un turno anterior
			c := a.conv(consoleWho)
			c.mu.Lock()
			c.msgs = append(c.msgs,
				aiMsg{Role: "user", Content: "dame el estado"},
				aiMsg{Role: "assistant", ToolCalls: []aiToolCall{{Function: aiFunc{Name: "get_status", Arguments: map[string]any{}}}}},
				aiMsg{Role: "tool", ToolName: "get_status", Content: strings.Repeat("inventario: tierra x64, ", 60)},
				aiMsg{Role: "assistant", Content: "toy bien"})
			c.mu.Unlock()
		}
		if _, err := b.aiAsk(consoleWho, strings.Repeat("oye q tal todo por ahi ", 40)); err != nil {
			t.Fatal(err)
		}
	}
	budget := a.budget()
	worst := 0
	for i, req := range *got {
		n := sentTokens(a, req) * 88 / 100 // (lo que cuenta el modelo, como dice el falso Ollama)
		worst = max(worst, n)
		if n > budget {
			t.Errorf("petición %d: %d tokens, el presupuesto es %d (30%% de %d)", i+1, n, budget, a.numCtx)
		}
	}
	c := a.conv(consoleWho)
	t.Logf("%d peticiones; la mayor %d tokens (%d%% del contexto); %d compactaciones; calibrado ×%.2f",
		len(*got), worst, 100*worst/a.numCtx, c.compacts, a.ratio)
	if c.compacts == 0 {
		t.Error("no compactó")
	}
	if c.summary == "" {
		t.Error("sin resumen")
	}
}

func TestParseToolText(t *testing.T) {
	calls, rest := parseToolText(`<|tool_call>call:run_command{command:<|"|>/goto Alex<|"|>}<tool_call|>`, "")
	if len(calls) != 1 || calls[0].Function.Arguments["command"] != "/goto Alex" || rest != "" {
		t.Errorf("formato Gemma: %+v %q", calls, rest)
	}
	calls, _ = parseToolText("```json\n{\"name\": \"run_command\", \"arguments\": {\"command\": \"/stop\"}}\n```", "")
	if len(calls) != 1 || calls[0].Function.Arguments["command"] != "/stop" {
		t.Errorf("JSON: %+v", calls)
	}
	// sin "<|tool_call>call:" (gemma4 a veces se lo come)
	for _, in := range []string{
		`run_command{command:<|"|>/stop<|"|>}<tool_call|><|tool_response>`,
		`run_command{command: "/stop"}`,
		`run_command {"command": "/stop"}`,
	} {
		calls, rest = parseToolText(in+"\nya voy bro", "")
		if len(calls) != 1 || calls[0].Function.Arguments["command"] != "/stop" || rest != "ya voy bro" {
			t.Errorf("%s: %+v %q", in, calls, rest)
		}
	}
	// lo que gemma4 escribía con el prompt corto: restos de la llamada en el texto
	for in, want := range map[string]string{
		`run_command uff ok /stop ya voy`:                                                   "/stop|uff ok ya voy",
		`run_command 10m json { "command": "/guard Alex 10m" } dale`:                       "/guard Alex 10m|10m dale",
		`run_command dale /queue add chop roble 64 run_command /queue add chop roble 64 va`: "/queue add chop roble 64|dale va",
	} {
		calls, rest := parseToolText(in, "")
		var got []string
		for _, c := range calls {
			got = append(got, c.Function.Arguments["command"].(string))
		}
		if strings.Join(got, ",")+"|"+rest != want {
			t.Errorf("%q: %q %q", in, got, rest)
		}
	}
	if calls, rest := parseToolText("hola, ¿qué tal?", ""); calls != nil || rest != "hola, ¿qué tal?" {
		t.Errorf("texto normal: %+v %q", calls, rest)
	}
}

func TestSplitChat(t *testing.T) {
	s := strings.Repeat("árbol ", 200)
	parts := splitChat(strings.TrimSpace(s), 190, 4)
	if len(parts) != 4 {
		t.Fatalf("%d trozos", len(parts))
	}
	for _, p := range parts {
		if len(p) > 190 || !utf8.ValidString(p) {
			t.Errorf("trozo malo (%d bytes): %q", len(p), p)
		}
	}
	if !strings.HasSuffix(parts[3], "…") {
		t.Error("el último trozo debería avisar de que se cortó")
	}
}

func TestLooksLikeCommand(t *testing.T) {
	for text, want := range map[string]bool{
		"stop": true, "follow yo": true, "goto 100 200": true, "/lo que sea": true, "say hola a todos": true,
		"follow me please now": false, "¿cómo estás?": false, "help me find wood": false, "ai hola": false,
	} {
		if got := looksLikeCommand(text); got != want {
			t.Errorf("looksLikeCommand(%q) = %v", text, got)
		}
	}
}

func TestParseInlineCommands(t *testing.T) {
	cases := []struct {
		in   string
		cmds []string
		rest string
	}{
		{"yo: jaja ok bro /stop ya me pongo a talar xd /queue add chop abeto 32", []string{"/stop", "/queue add chop abeto 32"}, "yo: jaja ok bro ya me pongo a talar xd"},
		{"yo: ok espera q llego xq tamos cerca xd /goto Alex", []string{"/goto Alex"}, "yo: ok espera q llego xq tamos cerca xd"},
		{"voy /goto 120 -450 ya", []string{"/goto 120 -450"}, "voy ya"},
		{"/follow yo.", []string{"/follow yo"}, ""},
		{"/chop abeto pq hay muchos", []string{"/chop abeto"}, "pq hay muchos"},
		{"/queue pause y luego vemos", []string{"/queue pause"}, "y luego vemos"},
		{"usa /cmd spawn o /quit", nil, "usa /cmd spawn o /quit"},
		{"vale /chop abeto until got logs 64 or time 20m y ya", []string{"/chop abeto until got logs 64 or time 20m"}, "vale y ya"},
		{"/chop abeto until got logs 64 or sea", []string{"/chop abeto until got logs 64"}, "or sea"},
		{"/chop abeto until mañana", []string{"/chop abeto"}, "until mañana"},
		{"/guard Alex 10m dale", []string{"/guard Alex 10m"}, "dale"},
		{"mira 1/2 de vida y/o", nil, "mira 1/2 de vida y/o"},
	}
	for _, c := range cases {
		calls, rest := parseInlineCommands(c.in)
		var got []string
		for _, call := range calls {
			got = append(got, call.Function.Arguments["command"].(string))
		}
		if strings.Join(got, "|") != strings.Join(c.cmds, "|") || rest != c.rest {
			t.Errorf("%q: %q %q", c.in, got, rest)
		}
	}
}

func TestCleanReply(t *testing.T) {
	for in, want := range map[string]string{
		"yo: hola bro": "hola bro",
		"oye q onda [estado: vida 17/20, comida 20/20] xd": "oye q onda  xd",
		"ok yo: ya voy": "ok ya voy",
		"yo tmb quiero": "yo tmb quiero",
	} {
		if got := cleanReply(in); got != want {
			t.Errorf("%q: %q, quería %q", in, got, want)
		}
	}
}

func TestDropOldStates(t *testing.T) {
	msgs := []aiMsg{{Role: "user", Content: "[estado: vida 20/20, en 1 2 3]\nAlex: hola"}, {Role: "assistant", Content: "[estado: no se toca]"}}
	dropOldStates(msgs)
	if msgs[0].Content != "Alex: hola" || msgs[1].Content != "[estado: no se toca]" {
		t.Errorf("%q", msgs)
	}
}

func TestAsksToSay(t *testing.T) {
	for text, want := range map[string]bool{
		"Construye algo": false, "pelea con detsukio": false, "pon en el chat que llego tarde": true,
		"di a todos q hay evento": true, "Pone \"Lau Catboy\" en el chat general": true, "escribe hola": true, "dime algo": false,
	} {
		if asksToSay(text) != want {
			t.Errorf("%q: %v", text, !want)
		}
	}
	if !isSay("/say hola") || !isSay("say hola") || isSay("/stop") {
		t.Error("isSay")
	}
}
