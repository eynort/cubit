package main

// Control por lenguaje natural con un modelo local (Ollama), encendido por
// defecto y apagable por bot en el panel. Gratis y sin claves.
//
// Un dueño (allowedRemote) le escribe al bot algo que no es una orden ("minad
// 20 de hierro y traédmelo"), por privado, por el chat nombrándolo, o con
// /ask <texto>. El modelo (gemma, pequeño) contesta siempre un JSON
// {say, actions, done} (salida estructurada de Ollama); cada acción se valida
// contra el registro de órdenes (lista blanca, llm_tools.go), se ejecuta y su
// resultado vuelve al modelo hasta que acaba. Lo que dice sale por el chat, en
// el idioma del bot y breve, como un colega.
//
// El modelo va detrás de llmModel: Ollama de verdad en llm_ollama.go y los
// tests usan uno falso (o un servidor HTTP falso).

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	llmMaxSteps   = 6                // vueltas por pregunta
	llmAskTimeout = 10 * time.Minute // una pregunta entera (con las esperas)
	llmMemTurns   = 4                // turnos que recuerda por jugador
	llmMemTTL     = 10 * time.Minute // y cuánto
	llmHourlyCap  = 60               // preguntas por hora, en total
)

// ---- el modelo, detrás de una interfaz ----

// llmMsg: un mensaje de la conversación (solo texto).
type llmMsg struct {
	Role string // "user" | "assistant"
	Text string
}

// llmModel contesta con un JSON (llmSchema): Ollama en llm_ollama.go; los tests
// usan uno falso con respuestas guionizadas.
type llmModel interface {
	Chat(ctx context.Context, system string, msgs []llmMsg) (string, error)
}

// llmError: un fallo del modelo, con su clase (down, missing, other).
type llmError struct {
	Kind string
	Err  error
}

func (e *llmError) Error() string { return e.Err.Error() }
func (e *llmError) Unwrap() error { return e.Err }

// ---- el anfitrión: lo que el bucle necesita del bot ----

type llmHost interface {
	Run(from, line string) error                      // una orden de consola (como si la mandara from)
	Info(from string) string                          // el contexto: posición, vida, inventario, tarea, el jugador
	Data(tool string) string                          // status / inventory
	Wait(ctx context.Context, d time.Duration) string // hasta que acabe la tarea
	Task() string                                     // qué hace ahora
}

// ---- memoria corta por jugador ----

type llmConvo struct {
	msgs []llmMsg // solo texto: lo que dijo y lo que contestó
	last time.Time
}

type llmState struct {
	mu    sync.Mutex
	memo  map[string]*llmConvo
	asks  []time.Time // cuándo, para el tope por hora
	busy  sync.Mutex  // una pregunta a la vez
	model func() llmModel
}

var llm = &llmState{memo: map[string]*llmConvo{}, model: newOllamaModel}

func (s *llmState) recall(who string, now time.Time) []llmMsg {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.memo[strings.ToLower(who)]
	if c == nil || now.Sub(c.last) > llmMemTTL {
		delete(s.memo, strings.ToLower(who))
		return nil
	}
	return append([]llmMsg(nil), c.msgs...)
}

func (s *llmState) remember(who, said, reply string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := strings.ToLower(who)
	c := s.memo[k]
	if c == nil || now.Sub(c.last) > llmMemTTL {
		c = &llmConvo{}
		s.memo[k] = c
	}
	c.msgs = append(c.msgs, llmMsg{Role: "user", Text: said}, llmMsg{Role: "assistant", Text: reply})
	if n := llmMemTurns * 2; len(c.msgs) > n {
		c.msgs = c.msgs[len(c.msgs)-n:]
	}
	c.last = now
}

// underCap cuenta una pregunta contra el tope por hora.
func (s *llmState) underCap(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := s.asks[:0]
	for _, t := range s.asks {
		if now.Sub(t) < time.Hour {
			keep = append(keep, t)
		}
	}
	s.asks = keep
	if len(s.asks) >= llmHourlyCap {
		return false
	}
	s.asks = append(s.asks, now)
	return true
}

// ---- la configuración ----

// llmEnabled: la IA está encendida (por defecto sí; el panel la apaga por bot).
func llmEnabled() bool {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	return !cfg.LLM.Off
}

// ---- el bucle ----

func llmSystem(botName, language, info string) string {
	lang := "English"
	if language == "es" {
		lang = "Spanish"
	}
	return fmt.Sprintf(`You are %s, a Minecraft bot playing with a friend (the owner). Talk like a friendly mate: short, warm, natural, no emojis, no markdown. Write "say" in the language the owner writes in (if unsure, %s), max 150 characters.
Reply ONLY with JSON: {"say": text, "actions": [{"command": name, "args": [strings]}], "done": bool}.
Use actions to do what the owner asks (max %d per reply); you will get their results and can continue. "say" goes to the game chat: say what you will do at the start and the result at the end; leave it "" while you only check or wait. Set done=true when finished or when there is nothing to do. Long tasks run in the background: use the "wait" action before a step that needs them finished (e.g. "bring me 10 iron" = mine iron total 10, wait, inventory, drop raw_iron 10 me). Never invent coordinates or results: use status/inventory. Use stop only when the owner asks you to stop (it cancels what you are doing, a drop walk too). Politely refuse destructive or abusive requests, server commands, op and quitting (no actions, explain in say).
Commands (command = name without /, args = its words in order; "me" = the owner):
%s
State: %s`, botName, lang, llmMaxActions, llmCommandList(), info)
}

// llmRun es el bucle: pregunta, valida y ejecuta las acciones, devuelve los
// resultados, hasta que el modelo acaba (done / sin acciones) o se acaban los
// pasos. reply se llama con cada frase para el jugador. Devuelve la última.
func llmRun(ctx context.Context, model llmModel, host llmHost, from, said, language string, reply func(string)) (string, error) {
	msgs := append(llm.recall(from, time.Now()), llmMsg{Role: "user", Text: said})
	var final, pending string
	spoke := false
	bad := 0
	for step := 0; step < llmMaxSteps; step++ {
		raw, err := model.Chat(ctx, llmSystem(botName, language, host.Info(from)), msgs)
		if err != nil {
			return final, err
		}
		r, perr := parseLLMReply(raw)
		if perr != nil { // un JSON roto: se le pide otra vez (una vez)
			if bad++; bad > 1 {
				return final, &llmError{Kind: "other", Err: fmt.Errorf("the model did not return valid JSON: %w", perr)}
			}
			msgs = append(msgs, llmMsg{Role: "assistant", Text: raw}, llmMsg{Role: "user", Text: "That was not valid JSON. Reply only with the JSON object."})
			continue
		}
		// acciones sin orden ({"command":""}, a veces de gemma4): ni se ejecutan ni se cuentan
		kept := r.Actions[:0]
		for _, a := range r.Actions {
			if strings.TrimSpace(a.Command) != "" {
				kept = append(kept, a)
			}
		}
		r.Actions = kept
		// al chat, solo lo primero (qué va a hacer) y lo último (cómo ha ido):
		// los modelos pequeños narran cada paso ("voy a mirar el inventario…")
		terminal := len(r.Actions) == 0 || r.Done
		if t := strings.TrimSpace(r.Say); t != "" {
			final = t
			if terminal || !spoke {
				reply(t)
				spoke, pending = true, ""
			} else {
				pending = t // (si acaba sin decir nada, se dice esto)
				log.Printf("[ia] (solo en el log) %s", t)
			}
		}
		if terminal && pending != "" {
			reply(pending)
			pending = ""
		}
		if len(r.Actions) == 0 {
			llm.remember(from, said, final, time.Now())
			return final, nil
		}
		if len(r.Actions) > llmMaxActions {
			r.Actions = r.Actions[:llmMaxActions]
		}
		var results []string
		for _, a := range r.Actions {
			out := llmExec(ctx, host, from, a)
			log.Printf("[ia] %s %v → %s", a.Command, a.Args, clip(out, 120))
			results = append(results, fmt.Sprintf("%s %s: %s", a.Command, strings.Join(a.Args, " "), out))
		}
		if r.Done { // pidió acciones y acabar: se ejecutan y listo
			llm.remember(from, said, final, time.Now())
			return final, nil
		}
		msgs = append(msgs, llmMsg{Role: "assistant", Text: raw}, llmMsg{Role: "user", Text: "Results:\n" + strings.Join(results, "\n")})
	}
	// se acabaron los pasos: lo que haya hecho ya está hecho
	if pending != "" {
		reply(pending)
	}
	if final == "" {
		final = tr("I got a bit tangled up, I'll stop here", "me he liado un poco, lo dejo aquí")
		reply(final)
	}
	llm.remember(from, said, final, time.Now())
	return final, nil
}

// llmExec ejecuta una acción ya validada; el texto vuelve al modelo.
func llmExec(ctx context.Context, host llmHost, from string, a llmAction) string {
	name := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(a.Command), "/"))
	switch {
	case name == llmWait:
		d := 60 * time.Second
		if len(a.Args) > 0 {
			if n, err := strconv.ParseFloat(strings.TrimSpace(a.Args[0]), 64); err == nil && n > 0 {
				d = time.Duration(math.Min(n, 300)) * time.Second
			}
		}
		return host.Wait(ctx, d)
	case llmOwnTools[name] && allowedLLM(name):
		if _, ok := commands[name]; !ok {
			return "error: unknown command"
		}
		return host.Data(name)
	}
	line, err := llmLine(a)
	if err != nil {
		return "error: " + err.Error()
	}
	if err := host.Run(from, line); err != nil {
		return "error: " + errText(err)
	}
	out := "ok"
	if name == "stop" {
		return out + ", stopped"
	}
	if t := host.Task(); t != "" {
		out += "; running: " + t + " (continues in the background; use wait if you need it finished)"
	}
	return out
}

// llmFriendly: la frase para el jugador cuando el modelo falla (el detalle va al log).
func llmFriendly(err error) string {
	var le *llmError
	if errors.As(err, &le) {
		switch le.Kind {
		case "down":
			return tr("ugh, my AI brain isn't answering right now, but commands still work", "uf, no me llega la cabeza ahora mismo, pero las órdenes de siempre me valen")
		case "missing":
			return tr("hang on, I'm still downloading my brain, ask me again in a few minutes", "espera, que aún me estoy bajando el cerebro, pídemelo en unos minutos")
		}
	}
	return tr("ugh, I got confused there, try again in a bit", "uf, me he liado, prueba otra vez en un rato")
}

// llmAsk atiende una frase de from: la manda al modelo y contesta con reply.
// Se llama en su propia goroutine. Una pregunta a la vez.
func llmAsk(host llmHost, from, said string, reply func(string)) {
	if !llmEnabled() {
		return
	}
	if !llm.busy.TryLock() {
		reply(tr("hold on, I'm in the middle of something, one sec", "espera, que estoy a lo mío, un segundín"))
		return
	}
	defer llm.busy.Unlock()
	if !llm.underCap(time.Now()) {
		reply(tr("I'm a bit overloaded, ask me again later", "voy un poco saturado, pídemelo más tarde"))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), llmAskTimeout)
	defer cancel()
	log.Printf("[ia] %s: %s", from, clip(said, 200))
	_, err := llmRun(ctx, llm.model(), host, from, said, lang(), reply)
	if err != nil {
		log.Printf("[ia] el modelo falló: %v", err)
		var le *llmError
		if errors.As(err, &le) && le.Kind == "missing" {
			llmPull() // (se baja solo)
		}
		reply(llmFriendly(err))
	}
}

// ---- el anfitrión de verdad: el bot ----

type botHost struct{ b *Bot }

func (h botHost) Run(from, line string) error {
	line = remoteLine(strings.TrimPrefix(line, "/"), from)
	return asOrderFrom(from, func() error { return h.b.run(line) })
}

func (h botHost) Task() string {
	var parts []string
	if t := h.b.Tasks.Status(); t != "" {
		parts = append(parts, t)
	}
	if m := currentMove(); m != "" {
		parts = append(parts, m)
	}
	return strings.Join(parts, ", ")
}

func (h botHost) busy() bool {
	return h.b.Tasks.Status() != "" || currentMove() == "goto" || h.b.workResuming()
}

func (h botHost) Wait(ctx context.Context, d time.Duration) string {
	end := time.Now().Add(d)
	for h.busy() {
		if ctx.Err() != nil || time.Now().After(end) {
			return fmt.Sprintf("still running after %d s: %s", int(d.Seconds()), h.Task())
		}
		time.Sleep(500 * time.Millisecond)
	}
	return "finished; nothing is running now"
}

// inventoryText: lo que lleva, contado ("12 iron_ingot, 3 bread…").
func (h botHost) inventoryText() string {
	b := h.b
	if b.Screens == nil {
		return "unknown"
	}
	b.resyncInventory(b.ctx)
	counts := map[string]int{}
	b.Screens.Lock()
	for i := invArmor; i <= invHotbar+8 && i < len(b.Screens.Inventory.Slots); i++ {
		s := b.Screens.Inventory.Slots[i]
		if n := shortName(s); n != "" {
			if i < invArmor+4 {
				n += " (worn)"
			}
			counts[n] += int(s.Count)
		}
	}
	b.Screens.Unlock()
	if len(counts) == 0 {
		return "empty"
	}
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Strings(names)
	for i, n := range names {
		names[i] = fmt.Sprintf("%d %s", counts[n], n)
	}
	return strings.Join(names, ", ")
}

func (h botHost) Data(tool string) string {
	if tool == "inventory" {
		return h.inventoryText()
	}
	return h.stateText("")
}

func (h botHost) Info(from string) string {
	return h.stateText(from) + "\ninventory: " + h.inventoryTextCached()
}

// inventoryTextCached: como inventoryText pero sin pedir el inventario al servidor.
func (h botHost) inventoryTextCached() string {
	b := h.b
	if b.Screens == nil {
		return "unknown"
	}
	counts := map[string]int{}
	b.Screens.Lock()
	for i := invArmor; i <= invHotbar+8 && i < len(b.Screens.Inventory.Slots); i++ {
		if n := shortName(b.Screens.Inventory.Slots[i]); n != "" {
			counts[n] += int(b.Screens.Inventory.Slots[i].Count)
		}
	}
	b.Screens.Unlock()
	if len(counts) == 0 {
		return "empty"
	}
	type kv struct {
		n string
		c int
	}
	var all []kv
	for n, c := range counts {
		all = append(all, kv{n, c})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].c > all[j].c || all[i].c == all[j].c && all[i].n < all[j].n })
	var out []string
	for i, e := range all {
		if i == 12 {
			out = append(out, fmt.Sprintf("and %d more kinds (use the inventory tool)", len(all)-i))
			break
		}
		out = append(out, fmt.Sprintf("%d %s", e.c, e.n))
	}
	return strings.Join(out, ", ")
}

func (h botHost) stateText(from string) string {
	b := h.b
	st, p := b.Player.Status(), b.Player.Position()
	s := fmt.Sprintf("position %.0f %.0f %.0f, health %.0f/20, food %d/20", p.X, p.Y, p.Z, st.Health, st.Food)
	if t := h.Task(); t != "" {
		s += ", current task: " + t
	} else {
		s += ", idle"
	}
	if c := b.Combat.Status(); c != "" {
		s += ", combat: " + c
	}
	if from != "" {
		s += "\nthe speaker is " + from
		if id, ok := b.Players.ByName(from); ok {
			if e, ok := b.Entities.ByUUID(id); ok {
				s += fmt.Sprintf(", at %.0f %.0f %.0f (%.0f blocks away)", e.X, e.Y, e.Z, b.distanceTo(e))
			} else {
				s += ", online but out of sight"
			}
		} else {
			s += ", not seen online"
		}
	}
	return s
}

// ---- puntos de entrada ----

// llmHandles: un mensaje de un dueño que no es una orden y que la IA atiende.
func llmHandles(text string) bool {
	return llmEnabled() && !looksLikeCommand(text)
}

// onOwnerWhisper: un privado de un dueño que no es una orden.
func (b *Bot) llmWhisper(from, text string) {
	go llmAsk(botHost{b}, from, text, func(s string) { b.whisper(from, s) })
}

// llmPublic: alguien dijo algo en el chat nombrando al bot.
func (b *Bot) llmPublic(from, text string) {
	if !allowedRemote(from) || !llmHandles(text) {
		return
	}
	go llmAsk(botHost{b}, from, text, func(s string) {
		if err := b.Chat.SendMessage(shortChat(s, 240)); err != nil {
			log.Print("chat: ", err)
		}
	})
}

func init() {
	commands["ask"] = command{
		desc: text{"ask the bot in plain words and it does it, using its commands as tools (needs the AI assistant on in the panel settings, and Ollama running): /ask mine 20 iron and bring it to me",
			"pídele al bot en lenguaje normal y lo hace, usando sus órdenes como herramientas (hace falta el asistente IA activado en los ajustes del panel, y Ollama en marcha): /ask mina 20 de hierro y tráemelo"},
		args: []arg{{name: "request", rest: true, desc: text{"what you want, in your own words", "lo que quieres, con tus palabras"}}},
		run:  cmdAsk,
	}
}

func cmdAsk(b *Bot, args []string) error {
	said := strings.TrimSpace(strings.Join(args, " "))
	if said == "" {
		return errf("usage: /ask <what you want>", "uso: /ask <lo que quieres>")
	}
	if !llmEnabled() {
		return errf("the AI assistant is off: turn it on in the panel settings", "el asistente IA está apagado: actívalo en los ajustes del panel")
	}
	orderFrom.Lock()
	who := orderFrom.who
	orderFrom.Unlock()
	reply := func(s string) { log.Printf("[ia] → %s", s) }
	if who != "" { // llegó por /msg: se contesta por privado a quien lo pidió
		reply = func(s string) { b.whisper(who, s) }
	}
	if who == "" {
		cfgMu.Lock()
		if len(cfg.Owners) > 0 && cfg.Owners[0] != "*" {
			who = cfg.Owners[0] // desde la consola: habla el primer dueño
		}
		cfgMu.Unlock()
		if who == "" {
			who = "console"
		}
	}
	go llmAsk(botHost{b}, who, said, reply)
	return nil
}
