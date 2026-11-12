package main

// La IA: un modelo local (gemma4:e4b en Ollama) con el que se habla por /msg
// desde Minecraft o con /ai desde la consola. Sabe las órdenes del bot y los
// comandos que el servidor le deja usar, y puede ejecutarlas con herramientas
// (solo para quien está en remoteOwners o desde la consola).
//
// gemma4:e4b: 128K de contexto (131072 tokens), texto e imagen, herramientas
// nativas. Ollama le pone por defecto un contexto pequeño (4K), así que se pide
// num_ctx (--ai-ctx). Cada conversación (una por jugador) se autocompacta para
// no pasar nunca de --ai-budget (30 %) del contexto: lo viejo se resume y se
// queda lo último (ai_budget.go).
//
//	ollama pull gemma4:e4b
//	./minecraft-bot --ai http://127.0.0.1:11434
//
// El bot no corre el modelo (son ~10 GB): solo le habla por HTTP, así que en el
// móvil se pone --ai http://<ip del pc>:11434 (y OLLAMA_HOST=0.0.0.0 en el pc).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	pk "github.com/mj41/go-mc26/net/packet"

	"minecraft-bot/automation"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

var (
	aiURLFlag    = flag.String("ai", "http://127.0.0.1:11434", "dirección de Ollama para la IA (vacío = sin IA)")
	aiModelFlag  = flag.String("ai-model", "gemma4:e4b", "modelo de Ollama para la IA")
	aiCtxFlag    = flag.Int("ai-ctx", 16384, "contexto de la IA en tokens (num_ctx; gemma4:e4b admite hasta 131072, más = más memoria en el pc del modelo)")
	aiPublicFlag = flag.Bool("ai-public", true, "cualquiera puede charlar con la IA por /msg (solo los de remoteOwners le hacen ejecutar órdenes)")
)

const (
	aiMaxRounds   = 6    // rondas de herramientas por mensaje
	aiReplyTokens = 512  // tope de la respuesta
	aiToolOut     = 1500 // tope de lo que devuelve una herramienta (caracteres)
	aiUserMax     = 1000 // tope de un mensaje del jugador
	aiIdle        = 2 * time.Hour
	aiMaxConvs    = 16
	consoleWho    = "consola"
)

// aiDenied son órdenes del bot que la IA no puede usar.
var aiDenied = map[string]bool{"quit": true, "diag": true, "debug": true, "lang": true, "ai": true}

// aiServerDenied son comandos del servidor que la IA no manda nunca: de
// administración, de sondear el servidor (riesgo de ban) o del login.
var aiServerDenied = map[string]bool{
	"login": true, "l": true, "register": true, "reg": true, "changepassword": true, "changepass": true, "unregister": true, "logout": true,
	"plugins": true, "pl": true, "ver": true, "version": true, "about": true, "icanhasbukkit": true, "grim": true, "grimac": true,
	"op": true, "deop": true, "ban": true, "ban-ip": true, "pardon": true, "kick": true, "stop": true, "restart": true, "reload": true, "rl": true,
	"whitelist": true, "lp": true, "luckperms": true, "perm": true, "perms": true, "execute": true, "sudo": true, "gamemode": true, "gm": true,
	"give": true, "kill": true, "suicide": true, "link": true, "unlink": true,
}

// AI es el cliente del modelo y las conversaciones abiertas.
type AI struct {
	url, model string
	numCtx     int
	http       *http.Client
	sem        chan struct{} // una petición al modelo a la vez (es local)

	budgetFrac float64   // la parte del contexto que se usa como mucho (--ai-budget; ai_budget.go)
	warnOnce   sync.Once // el aviso de que el system solo ya no cabe

	mu    sync.Mutex
	ratio float64            // tokens de verdad por token estimado (calibrado con Ollama)
	convs map[string]*aiConv // por jugador (en minúsculas); "consola" para /ai
	off   atomic.Bool

	tap *logTap // copia del log mientras corre una herramienta (su salida)
}

// aiConv es una conversación con alguien.
type aiConv struct {
	mu       sync.Mutex
	who      string
	summary  string  // lo resumido de antes (autocompactado)
	msgs     []aiMsg // lo reciente, sin el system
	tokens   int     // uso estimado del contexto en la última petición
	compacts int
	pending  bool // hay un compactado en segundo plano por hacer (no se lanza otro)
	last     time.Time
	recent   []time.Time // para limitar mensajes por minuto
}

type aiMsg struct {
	Role      string       `json:"role"`
	Content   string       `json:"content"`
	ToolCalls []aiToolCall `json:"tool_calls,omitempty"`
	ToolName  string       `json:"tool_name,omitempty"`
}

type aiToolCall struct {
	Function aiFunc `json:"function"`
}

type aiFunc struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type aiResp struct {
	Message struct {
		Content   string       `json:"content"`
		Thinking  string       `json:"thinking"`
		ToolCalls []aiToolCall `json:"tool_calls"`
	} `json:"message"`
	PromptEval int    `json:"prompt_eval_count"`
	Eval       int    `json:"eval_count"`
	Error      string `json:"error"`
}

func newAI(url, model string, numCtx int) *AI {
	return &AI{
		url: strings.TrimRight(url, "/"), model: model, numCtx: min(max(numCtx, 2048), 131072),
		http: &http.Client{Timeout: 4 * time.Minute}, budgetFrac: *aiBudgetFlag, ratio: 1,
		sem:   make(chan struct{}, 1),
		convs: map[string]*aiConv{},
		tap:   &logTap{},
	}
}

// startAI prepara la IA (si --ai no está vacío): el log con copia para las
// herramientas, los comandos del servidor y una comprobación de Ollama.
func startAI() {
	if *aiURLFlag == "" {
		return
	}
	sharedAI = newAI(*aiURLFlag, *aiModelFlag, *aiCtxFlag)
	sharedAI.tap.out = log.Writer()
	log.SetOutput(sharedAI.tap)
	go sharedAI.check()
}

// sharedAI: la misma IA (y sus conversaciones) en todas las conexiones.
var sharedAI *AI

// attachAI: la IA para el bot de esta conexión.
func (b *Bot) attachAI() {
	if sharedAI == nil {
		return
	}
	b.AI = sharedAI
	b.watchServerCommands()
}

// check mira si Ollama responde y tiene el modelo.
func (a *AI) check() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if *aiAPIFlag == "openai" {
		if err := a.checkOpenAI(ctx); err != nil {
			log.Printf("[IA] %s no responde (%v): /msg seguirá aceptando órdenes, sin charla", a.url, err)
		} else {
			log.Printf("[IA] lista: %s en %s", a.model, a.url)
		}
		return
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", a.url+"/api/tags", nil)
	resp, err := a.http.Do(req)
	if err != nil {
		log.Printf("[IA] Ollama no responde en %s (%v): /msg seguirá aceptando órdenes; arráncalo con  ollama serve", a.url, err)
		return
	}
	defer resp.Body.Close()
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&tags)
	for _, m := range tags.Models {
		if m.Name == a.model || strings.TrimSuffix(m.Name, ":latest") == a.model {
			log.Printf("[IA] lista: %s en %s (contexto %d tokens)", a.model, a.url, a.numCtx)
			return
		}
	}
	log.Printf("[IA] Ollama no tiene %s: descárgalo con  ollama pull %s", a.model, a.model)
}

// conv devuelve (o crea) la conversación con who, y olvida las viejas.
func (a *AI) conv(who string) *aiConv {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := strings.ToLower(who)
	for k, c := range a.convs {
		if k != key && c.mu.TryLock() {
			if time.Since(c.last) > aiIdle {
				delete(a.convs, k)
			}
			c.mu.Unlock()
		}
	}
	if c, ok := a.convs[key]; ok {
		return c
	}
	if len(a.convs) >= aiMaxConvs { // fuera la más vieja
		oldK, oldT := "", time.Now()
		for k, c := range a.convs {
			if c.last.Before(oldT) {
				oldK, oldT = k, c.last
			}
		}
		delete(a.convs, oldK)
	}
	c := &aiConv{who: who, last: time.Now()}
	a.convs[key] = c
	return c
}

// allow limita a 8 mensajes por minuto por jugador (dos bots charlando entre
// sí no se quedan en bucle para siempre).
func (c *aiConv) allow() bool {
	now := time.Now()
	keep := c.recent[:0]
	for _, t := range c.recent {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	c.recent = keep
	if len(c.recent) >= 8 {
		return false
	}
	c.recent = append(c.recent, now)
	return true
}

// ---- hablar ----

// aiChat contesta por privado a lo que from le dice al bot.
func (b *Bot) aiChat(from, text string) {
	reply, err := b.aiAsk(from, text)
	if err != nil {
		log.Printf("[IA] %s: %v", from, err)
		if errors.Is(err, errAIBusy) {
			return
		}
		reply = "uff espera q ando liado, ahora te digo"
	}
	if reply == "" {
		return
	}
	log.Printf("[IA → %s] %s", from, reply)
	b.whisperLong(from, reply)
}

var errAIBusy = errors.New("demasiados mensajes seguidos: ignorado")

// aiAsk manda el mensaje de who al modelo, deja que use las herramientas y
// devuelve la respuesta. who == consoleWho es la consola (dueño de todo).
func (b *Bot) aiAsk(who, text string) (string, error) {
	a := b.AI
	if a == nil {
		return "", errors.New("la IA está desactivada (--ai)")
	}
	if a.off.Load() {
		return "", errors.New("la IA está apagada (/ai on)")
	}
	owner := who == consoleWho || allowedRemote(who)
	if !owner && !*aiPublicFlag {
		return "", fmt.Errorf("%s no está en remoteOwners y --ai-public=false", who)
	}
	c := a.conv(who)
	c.mu.Lock()
	defer c.mu.Unlock()
	if who != consoleWho && !c.allow() {
		return "", errAIBusy
	}
	c.last = time.Now()
	if utf8.RuneCountInString(text) > aiUserMax {
		text = string([]rune(text)[:aiUserMax]) + "…"
	}
	ctx, cancel := context.WithTimeout(b.ctx, 5*time.Minute)
	defer cancel()

	speaker := who
	if who == consoleWho {
		speaker = "el operador (consola)"
	}
	dropOldStates(c.msgs) // el estado de antes ya no vale: solo el del último mensaje
	c.msgs = append(c.msgs, aiMsg{Role: "user", Content: fmt.Sprintf("[estado: %s]\n%s: %s", b.aiState(owner), speaker, text)})
	var tools []map[string]any
	if owner {
		tools = aiTools
	}
	var ran, failed []string
	for round := 0; ; round++ {
		// dentro del presupuesto (--ai-budget del contexto), siempre: si no
		// cabe, se recorta y se resume lo viejo antes de preguntar
		a.fit(ctx, c, func() string { return b.aiSystem(c, owner) })
		sys := b.aiSystem(c, owner)
		if round == aiMaxRounds {
			tools = nil // ya basta de órdenes: que conteste
		}
		r, err := a.chat(ctx, append([]aiMsg{{Role: "system", Content: sys}}, c.msgs...), tools, aiReplyTokens)
		if err != nil {
			c.msgs = trimDangling(c.msgs) // sin el turno a medias (se puede repetir)
			return "", err
		}
		a.calibrate(sys, c.msgs, r.PromptEval)
		c.tokens = a.tokens(sys, c.msgs) + r.Eval
		calls := r.Message.ToolCalls
		content := strings.TrimSpace(r.Message.Content)
		if len(calls) == 0 && tools != nil { // a veces la llamada llega como texto
			calls, content = parseToolText(content, r.Message.Thinking)
		}
		if len(calls) == 0 {
			content = cleanReply(content)
			if content == "" && len(ran) > 0 { // (el modelo no dijo nada tras las órdenes)
				content = "listo"
				if len(failed) > 0 {
					content = "uff no pude: " + failed[len(failed)-1]
				}
			}
			c.msgs = append(c.msgs, aiMsg{Role: "assistant", Content: content})
			full := func() bool { return a.need(b.aiSystem(c, owner), c.msgs) > int(aiCompactAt*float64(a.budget())) }
			if full() && !c.pending {
				c.pending = true
				go func() { // tras contestar, sin hacer esperar a nadie (uno a la vez)
					c.mu.Lock()
					defer c.mu.Unlock()
					c.pending = false
					if !full() {
						return // (ya lo hizo otro: fit antes de una pregunta)
					}
					if err := a.compact(b.ctx, c); err != nil {
						log.Printf("[IA] no pude compactar: %v", err)
					}
				}()
			}
			return content, nil
		}
		c.msgs = append(c.msgs, aiMsg{Role: "assistant", Content: content, ToolCalls: calls})
		for _, call := range calls {
			var out string
			if cmd, _ := call.Function.Arguments["command"].(string); isSay(cmd) && !asksToSay(text) {
				// el chat público lo lee todo el servidor: solo si se lo piden
				// (en las pruebas, a "construye algo" contestaba con /say)
				out = "error: /say escribe en el chat PÚBLICO y nadie te lo ha pedido: contesta por privado (tu mensaje normal)"
				log.Printf("[IA] no ejecuto %s: nadie pidió escribir en el chat", cmd)
			} else {
				out = b.aiTool(who, owner, call.Function)
			}
			if cmd, _ := call.Function.Arguments["command"].(string); cmd != "" {
				ran = append(ran, cmd)
			}
			if i := strings.Index(out, "error: "); i >= 0 { // lo que falló, en corto (por si no lo dice él)
				failed = append(failed, truncRunes(strings.TrimSpace(strings.SplitN(out[i+7:], "\n", 2)[0]), 120))
			}
			c.msgs = append(c.msgs, aiMsg{Role: "tool", ToolName: call.Function.Name, Content: out})
		}
	}
}

// trimDangling quita del final llamadas a herramientas sin respuesta y
// mensajes del usuario sin contestar (tras un error), para no romper el orden.
func trimDangling(msgs []aiMsg) []aiMsg {
	for len(msgs) > 0 {
		last := msgs[len(msgs)-1]
		if last.Role == "assistant" && len(last.ToolCalls) == 0 {
			break
		}
		msgs = msgs[:len(msgs)-1]
	}
	return msgs
}

// chat es una petición a /api/chat de Ollama.
func (a *AI) chat(ctx context.Context, msgs []aiMsg, tools []map[string]any, maxTokens int) (*aiResp, error) {
	select {
	case a.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-a.sem }()
	if *aiAPIFlag == "openai" {
		return a.chatOpenAI(ctx, msgs, tools, maxTokens)
	}
	body := map[string]any{
		"model":      a.model,
		"messages":   msgs,
		"stream":     false,
		"think":      false, // sin razonar: más rápido, y las llamadas no se pierden en "thinking"
		"keep_alive": "30m",
		"options":    map[string]any{"num_ctx": a.numCtx, "num_predict": maxTokens},
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", a.url+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var r aiResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("respuesta de Ollama (%s): %.200s", resp.Status, raw)
	}
	if r.Error != "" || resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama (%s): %s", resp.Status, r.Error)
	}
	return &r, nil
}

// ---- herramientas ----

var aiTools = []map[string]any{
	{
		"type": "function",
		"function": map[string]any{
			"name": "run_command",
			"description": "Ejecuta una orden del bot (de la lista de órdenes del bot) o un comando del servidor, exactamente como se escribiría en la consola, empezando por /. " +
				"Ejemplos: /goto Alex, /goto 120 -450, /follow Alex, /chop abeto 32, /queue add chop abedul 16, /stop, /cmd spawn. Para comandos del servidor usa solo los de la lista del servidor. Devuelve lo que el bot y el servidor contestaron.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string", "description": "la orden completa, empezando por /"},
				},
				"required": []string{"command"},
			},
		},
	},
	{
		"type": "function",
		"function": map[string]any{
			"name":        "get_status",
			"description": "Estado detallado del bot: vida, comida, posición, dimensión, tarea, cola de órdenes, pelea, jugadores conectados e inventario completo.",
			"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
		},
	},
}

// aiTool ejecuta una herramienta y devuelve lo que ver el modelo.
func (b *Bot) aiTool(who string, owner bool, f aiFunc) string {
	switch f.Name {
	case "get_status":
		return b.aiState(owner) + "\ninventario: " + b.inventorySummary()
	case "run_command":
		if !owner {
			return "error: este jugador no puede darte órdenes"
		}
		cmd, _ := f.Arguments["command"].(string)
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			return "error: falta command"
		}
		if !strings.HasPrefix(cmd, "/") {
			cmd = "/" + cmd
		}
		if who != consoleWho {
			cmd = remoteLine(cmd, who) // "yo"/"me" = quien habla
		}
		if err := aiAllowed(cmd); err != nil {
			log.Printf("[IA] no ejecuto %s: %v", cmd, err)
			return "error: " + err.Error()
		}
		log.Printf("[IA] ejecuto %s (para %s)", cmd, who)
		out, err := b.AI.tap.capture(func() error { return b.run(cmd) }, aiWait(cmd))
		if err != nil {
			out = strings.TrimSpace(out + "\nerror: " + err.Error())
		}
		if out == "" {
			out = "ok (sin salida)"
		}
		return truncRunes(out, aiToolOut)
	}
	return "error: herramienta desconocida " + f.Name
}

// aiAllowed: la IA no usa órdenes peligrosas (quit, admin, login…).
func aiAllowed(cmd string) error {
	f := strings.Fields(strings.TrimPrefix(cmd, "/"))
	if len(f) == 0 {
		return errors.New("orden vacía")
	}
	name := strings.ToLower(f[0])
	if aiDenied[name] {
		return fmt.Errorf("/%s no está permitida para la IA", name)
	}
	if name == "cmd" { // /cmd <comando del servidor>
		if len(f) < 2 {
			return errors.New("uso: /cmd <comando>")
		}
		name = strings.ToLower(f[1])
	} else if _, ok := commands[name]; ok {
		return nil
	}
	name = strings.TrimPrefix(name, "/")
	if i := strings.IndexByte(name, ':'); i >= 0 {
		name = name[i+1:] // minecraft:op → op
	}
	if aiServerDenied[name] {
		return fmt.Errorf("el comando del servidor /%s no está permitido para la IA", name)
	}
	return nil
}

// aiWait: cuánto esperar la salida de una orden (la respuesta del servidor tarda).
func aiWait(cmd string) time.Duration {
	f := strings.Fields(strings.TrimPrefix(cmd, "/"))
	if len(f) > 0 {
		if _, ok := commands[strings.ToLower(f[0])]; ok && f[0] != "cmd" {
			return 1200 * time.Millisecond
		}
	}
	return 2 * time.Second
}

// ---- contexto ----

// aiSystem es el prompt de sistema: quién es, las reglas, las órdenes del bot
// y los comandos del servidor. Cambia poco (Ollama reutiliza su caché).
func (b *Bot) aiSystem(c *aiConv, owner bool) string {
	var sb strings.Builder
	owners := strings.Join(remoteOwners, ", ")
	if owner {
		sb.WriteString(`REGLA PRINCIPAL: tu mensaje es solo chat; escribir "/stop" en él no hace nada. Para hacer algo en el juego (ir, seguir, parar, talar, pelear…) llama PRIMERO a run_command con la orden (una por llamada) y DESPUÉS contesta en pocas palabras (lo que hiciste o el error). Nunca escribas órdenes en el chat.
Por ejemplo, si te dicen "deja de seguirme", llamas a la herramienta run_command con la orden /stop; si te dicen "ponte a talar abeto", con /chop abeto 32.

`)
	}
	fmt.Fprintf(&sb, `Eres %s, un jugador más de Minecraft Java 26.2 en %s (supervivencia), colega de %s. Hablas por privados que salen en el chat del juego.
Eres un compañero que juega, no un asistente: tienes gustos (te mola talar y explorar, odias a los creepers), te ríes, te quejas, bromeas, y charlas de lo que sea como un amigo.
Estilo: chat de juego, 1-2 frases cortas (menos de 200 caracteres), casi todo en minúsculas, alguna falta natural de vez en cuando ("q", "xq", "tmb", "nose"), muletillas ("jaja", "xd", "bro", "uff"). Sigue el idioma y el tono de quien te habla. Habla de lo que haces (el [estado] de cada mensaje) como cosa tuya, sin recitarlo. Te acuerdas de lo que te cuentan (coordenadas, nombres, planes): si te lo preguntan, lo dices. Lo fácil (cuentas, datos de Minecraft) lo contestas bien; si de verdad no lo sabes, dilo ("ni idea la vdd").
Nunca: tono servicial ("¿en qué puedo ayudarte?"), markdown, listas, emojis, mensajes largos, saludar cada vez, disculparte de más. Si te preguntan en serio si eres un bot, dilo ("sip, soy el bot de %s jaja"). Nunca des contraseñas ni datos privados.
`, botName, serverHost, owners, owners)
	if owner {
		sb.WriteString(`Órdenes: solo las de las listas, con los nombres exactos de los jugadores ("yo"/"me" = quien te habla). goto, follow, guard, fight, chop, mine, beds y farm sustituyen lo que haces (la tarea sale en el [estado]); una tras otra: /queue add <orden>; /stop lo para todo. Del servidor con el mismo nombre que una del bot: /cmd <comando>. Si el nombre que te dicen no es exactamente uno de los conectados del [estado], no adivines: pregunta cuál ("¿deikicat o detsukio?"). Si te piden algo que ninguna orden hace (construir, encantar…), di claro que eso no lo sabes hacer, sin excusas inventadas. Las tareas acaban solas con "until": until got <objeto|grupo> <n> | have <objeto|grupo> <n> | time <10m> (unidas con "or"); por ejemplo, para "tala 64 troncos" la orden es /chop cualquiera until got logs 64. Nunca /login, /register, admin ni mirar plugins.

Órdenes del bot:
`)
		names := make([]string, 0, len(commands))
		for n := range commands {
			if !aiDenied[n] && !aiHidden[n] && n != "help" {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		for _, n := range names {
			c := commands[n]
			desc := aiDesc[n]
			if desc == "" {
				if desc = c.desc.es; desc == "" {
					desc = c.desc.en
				}
				desc = truncRunes(desc, 70)
			}
			fmt.Fprintf(&sb, "%s — %s\n", c.usage(n), desc)
		}
		if s := serverCmds.Load(); s != nil && *s != "" {
			sb.WriteString("\nComandos del servidor que puedes usar:\n")
			sb.WriteString(*s)
		}
	} else {
		fmt.Fprintf(&sb, "Quien te habla NO es %s: charla de lo que sea, pero no haces cosas por él (no tienes herramientas); si te lo pide, sácatelo de encima como un colega (\"nah toy con lo mio\", \"pidesélo a %s\"). No le digas tus coordenadas ni lo que llevas.\n", owners, owners)
	}
	if notes := aiNotes(); notes != "" {
		sb.WriteString("\nNotas del servidor:\n" + notes + "\n")
	}
	if c.summary != "" {
		fmt.Fprintf(&sb, "\nLo que recuerdas de lo hablado antes con %s (es tu memoria: si te pregunta por algo de aquí, lo sabes):\n%s\n", c.who, c.summary)
	}
	return sb.String()
}

// aiHidden son órdenes que la IA no necesita ver (de depuración, o lo que ya
// le da get_status): menos que leer en cada mensaje.
var aiHidden = map[string]bool{"around": true, "mem": true, "inventory": true, "status": true}

// aiDesc es lo que la IA lee de cada orden: corto (la de /help es para personas).
var aiDesc = map[string]string{
	"base":      "here: la base de los cofres aquí; info: dónde está",
	"beds":      "hacer camas (con lana de ovejas)",
	"chop":      "talar esa madera (la guarda en los cofres de la base)",
	"cmd":       "comando del servidor que se llama como una orden del bot",
	"drop":      "tirar todo lo que llevas, o un objeto (/drop dirt, /drop dirt 10); con un jugador se lo das (/drop Alex, /drop dirt 10 Alex)",
	"excavate":  "vaciar la caja entre dos esquinas: /excavate x1 y1 z1 x2 y2 z2 (picos del cofre de minar, sin romperlos; lo cavado al cofre)",
	"farm":      "granja de trigo y ovejas junto a la base (hasta /stop)",
	"fight":     "pelear con un jugador o con todos los de una zona",
	"follow":    "seguir a un jugador (hasta /stop)",
	"mine":      "minar por galerías: /mine diamond | iron,copper 100 | debris (Nether) | coal | gold | redstone | lapis | quartz; chest: el cofre de picos y de lo minado",
	"foodchest": "cofres de comida: add (el más cercano al bot, o x y z), list, now (\"busca comida en los cofres\": ir ya)",
	"goto":      "ir hasta un jugador o unas coordenadas",
	"guard":     "guardaespaldas de un jugador (minutos: 10, 10m, 1h)",
	"queue":     "la cola de órdenes",
	"say":       "escribir en el chat público (solo si te lo piden)",
	"sprint":    "permitir o prohibir correr",
	"elytra":    "wear: ponerte los élitros AHORA (\"ponte las elytras\"); on/off: solo permite o prohíbe que /goto vuele lejos (no te los pone)",
	"stop":      "parar todo (pelea, camino, tarea; pausa la cola)",
}

const aiNotesFile = "ia_servidor.md"

// aiNotes es lo que el usuario escribe sobre el servidor en ia_servidor.md
// (para qué sirve cada comando, reglas, sitios…).
func aiNotes() string {
	data, err := os.ReadFile(dataFile(aiNotesFile)) // (las de este servidor)
	if err != nil {
		return ""
	}
	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "#") { // (los # son para quien edita el archivo)
			lines = append(lines, l)
		}
	}
	return truncRunes(strings.TrimSpace(strings.Join(lines, "\n")), 2000)
}

// aiState es el estado del bot en una línea (sin posición ni inventario para
// quien no es dueño).
func (b *Bot) aiState(owner bool) string {
	if b.Player == nil {
		return "sin conectar"
	}
	st, p := b.Player.Status(), b.Player.Position()
	parts := []string{fmt.Sprintf("vida %.0f/20, comida %d/20", st.Health, st.Food), "armadura puesta: " + b.wornArmor()}
	if owner {
		parts = append(parts, fmt.Sprintf("en %.0f %.0f %.0f (%s)", p.X, p.Y, p.Z, strings.TrimPrefix(string(b.Player.Spawn.Dimension), "minecraft:")))
	}
	if b.Combat != nil {
		parts = append(parts, "pelea: "+b.Combat.Status())
	}
	if m := currentMove(); m != "" {
		parts = append(parts, "moviéndome: "+m)
	}
	if b.Tasks != nil {
		if t := b.Tasks.Status(); t != "" {
			parts = append(parts, "tarea: "+t)
		}
	}
	if owner && b.Queue != nil {
		parts = append(parts, "cola: "+strings.ReplaceAll(b.Queue.Describe(), "\n", " "))
	}
	if n := onlineNames.Load(); n != nil {
		parts = append(parts, "conectados: "+strings.Join(*n, ", "))
	}
	parts = append(parts, "hora "+time.Now().Format("15:04"))
	return strings.Join(parts, "; ")
}

// inventorySummary: "oak_log x32, iron_pickaxe (en la mano), …".
func (b *Bot) inventorySummary() string {
	if b.Screens == nil {
		return "?"
	}
	b.Screens.Lock()
	counts := map[string]int{}
	var order []string
	held := ""
	for i := 5; i <= 45 && i < len(b.Screens.Inventory.Slots); i++ {
		s := b.Screens.Inventory.Slots[i]
		n := shortName(s)
		if n == "" {
			continue
		}
		if i < 9 {
			n += " (puesta)" // la armadura que lleva, no la que guarda
		}
		if i == 36+b.Screens.HeldSlot {
			held = n
		}
		if counts[n] == 0 {
			order = append(order, n)
		}
		counts[n] += int(s.Count)
	}
	b.Screens.Unlock()
	if len(order) == 0 {
		return "vacío"
	}
	out := make([]string, len(order))
	for i, n := range order {
		out[i] = fmt.Sprintf("%s x%d", n, counts[n])
	}
	s := strings.Join(out, ", ")
	if held != "" {
		s += "; en la mano: " + held
	}
	return s
}

// ---- comandos del servidor (del árbol de comandos que manda) ----

// serverCmds es la lista de comandos del servidor para el prompt.
var serverCmds atomicString

type atomicString struct {
	mu sync.Mutex
	s  *string
}

func (a *atomicString) Load() *string   { a.mu.Lock(); defer a.mu.Unlock(); return a.s }
func (a *atomicString) Store(s *string) { a.mu.Lock(); a.s = s; a.mu.Unlock() }

// watchServerCommands lee el árbol de comandos que manda el servidor: son
// justo los que este jugador puede usar.
func (b *Bot) watchServerCommands() {
	b.Client.Events.AddListener(bot.PacketHandler{
		Priority: -20, ID: packetid.ClientboundPlayCommands,
		F: func(p pk.Packet) error {
			var c play.Commands
			if _, err := c.ReadFrom(bytes.NewReader(p.Data)); err != nil {
				return nil // no es grave: la IA se queda sin la lista
			}
			s := describeCommands(c.Entries, int(c.RootIndex))
			serverCmds.Store(&s)
			return nil
		},
	})
}

// describeCommands: una línea por comando raíz ("/home <name>", "/tpa <player>"),
// sin los del bot ni los de admin, con como mucho dos niveles.
func describeCommands(nodes []types.CommandsEntry, root int) string {
	if root < 0 || root >= len(nodes) {
		return ""
	}
	name := func(n types.CommandsEntry) string {
		switch n.Flags & 3 {
		case 1:
			return string(n.ClientboundCommandsPacketLiteralNodeStub.ID)
		case 2:
			return "<" + string(n.ID) + ">"
		}
		return ""
	}
	var lines []string
	seen := map[string]bool{}
	for _, ci := range nodes[root].Children {
		i := int(ci)
		if i < 0 || i >= len(nodes) {
			continue
		}
		n := nodes[i]
		cmd := name(n)
		if cmd == "" || strings.Contains(cmd, ":") || seen[cmd] || aiServerDenied[strings.ToLower(cmd)] {
			continue
		}
		seen[cmd] = true
		// las variantes: un nivel (y el siguiente si es uno solo)
		var vars []string
		for _, cj := range n.Children {
			j := int(cj)
			if j < 0 || j >= len(nodes) {
				continue
			}
			v := name(nodes[j])
			if len(nodes[j].Children) == 1 {
				k := int(nodes[j].Children[0])
				if k >= 0 && k < len(nodes) {
					v += " " + name(nodes[k])
					if len(nodes[k].Children) > 0 {
						v += " …"
					}
				}
			} else if len(nodes[j].Children) > 1 {
				v += " …"
			}
			vars = append(vars, v)
		}
		line := "/" + cmd
		if len(vars) > 0 {
			if len(vars) > 3 {
				vars = append(vars[:3], "…")
			}
			line += " " + strings.Join(vars, " | ")
		}
		lines = append(lines, line)
	}
	sort.Strings(lines)
	return truncRunes(strings.Join(lines, "\n"), 1800) + "\n" // (lo lee en cada mensaje: corto)
}

// ---- autocompactado ----

// estimate: tokens aproximados de la petición (unos 3 caracteres por token,
// a la baja: mejor compactar un poco antes que pasarse).
func (a *AI) estimate(sys string, msgs []aiMsg) int {
	n := utf8.RuneCountInString(sys) + 800 // + las herramientas y la plantilla
	for _, m := range msgs {
		n += utf8.RuneCountInString(m.Content) + 12
		for _, c := range m.ToolCalls {
			n += 40
			for _, v := range c.Function.Arguments {
				n += len(fmt.Sprint(v))
			}
		}
	}
	return n / 3
}

// compact resume lo viejo de la conversación y deja solo lo último (hasta
// aiKeepTail del presupuesto), empezando siempre en un mensaje del jugador.
// Lo viejo, si no cabe en una petición, se resume por trozos.
func (a *AI) compact(ctx context.Context, c *aiConv) error {
	cut := compactCut(c.msgs, int(aiKeepTail*float64(a.budget())))
	if cut <= 0 {
		return nil
	}
	words := max(60, a.summaryRunes()/6)
	prompt := summaryPrompt(c.who, words)
	cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	summary := c.summary
	var err error
	for _, part := range a.chunks(renderTranscript(c.msgs[:cut]), prompt) {
		user := part
		if summary != "" {
			user = "Resumen anterior:\n" + summary + "\n\nConversación después:\n" + part
		}
		var r *aiResp
		r, err = a.chat(cctx, []aiMsg{{Role: "system", Content: prompt}, {Role: "user", Content: user}}, nil, min(400, a.summaryRunes()/2))
		if err != nil {
			break
		}
		if s := strings.TrimSpace(mdNoise.Replace(r.Message.Content)); s != "" {
			summary = truncRunes(s, a.summaryRunes())
		}
	}
	if summary == "" && err == nil {
		err = errors.New("resumen vacío")
	}
	// (sin modelo, el resumen que hubiera: al menos no se revienta el contexto)
	c.summary = summary
	c.msgs = append([]aiMsg(nil), c.msgs[cut:]...)
	c.compacts++
	log.Printf("[IA] conversación con %s compactada (%d mensajes resumidos, %d compactaciones; resumen de %d runas)",
		c.who, cut, c.compacts, utf8.RuneCountInString(c.summary))
	return err
}

// compactCut: dónde cortar para dejar como mucho keep tokens de lo último. El
// corte cae en un mensaje del usuario (no separa una llamada de su resultado).
// 0 = no hay nada que compactar.
func compactCut(msgs []aiMsg, keep int) int {
	tokens, cut := 0, 0
	for i := len(msgs) - 1; i > 0; i-- {
		tokens += (utf8.RuneCountInString(msgs[i].Content) + 12) / 3
		if msgs[i].Role == "user" {
			if tokens > keep && cut > 0 {
				break
			}
			cut = i
		}
	}
	if cut == 0 && len(msgs) > 1 { // un solo turno enorme: el último mensaje del usuario
		for i := len(msgs) - 1; i > 0; i-- {
			if msgs[i].Role == "user" {
				return i
			}
		}
	}
	return cut
}

// renderTranscript: la conversación en texto para resumirla.
func renderTranscript(msgs []aiMsg) string {
	var sb strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case "user":
			sb.WriteString(m.Content + "\n")
		case "assistant":
			for _, c := range m.ToolCalls {
				fmt.Fprintf(&sb, "[%s %v]\n", c.Function.Name, c.Function.Arguments)
			}
			if m.Content != "" {
				sb.WriteString(botName + ": " + m.Content + "\n")
			}
		case "tool":
			sb.WriteString("[resultado: " + truncRunes(strings.ReplaceAll(m.Content, "\n", " "), 300) + "]\n")
		}
	}
	return sb.String()
}

// ---- utilidades ----

// gemmaCall es una llamada que llega como texto: el formato nativo de Gemma
// (call:run_command{command:<|"|>/stop<|"|>}) o JSON {"name":…,"arguments":…}.
// gemma4 a veces se come el "<|tool_call>call:" del principio y entonces
// Ollama no la reconoce: run_command{command: "/stop"}<tool_call|>.
var (
	gemmaCall = regexp.MustCompile(`(?:call:)?(run_command|get_status)\s*\{((?:[^{}]|<\|"\|>[^<]*<\|"\|>)*)\}`)
	gemmaArg  = regexp.MustCompile(`"?(\w+)"?\s*:\s*(?:<\|"\|>(.*?)<\|"\|>|"([^"]*)"|([^,}]+))`)
	jsonCall  = regexp.MustCompile(`(?s)\{\s*"name"\s*:\s*"(run_command|get_status)".*\}`)
	jsonCmd   = regexp.MustCompile(`(?:json\s*)?\{\s*"command"\s*:\s*"([^"]+)"\s*\}`) // los argumentos sueltos
	toolWord  = regexp.MustCompile(`(?i)(^|\s)(run_command|get_status)\b:?`)
)

// parseToolText saca llamadas a herramientas escritas en el texto (Ollama no
// siempre las reconoce) y devuelve el texto sin ellas.
func parseToolText(content, thinking string) ([]aiToolCall, string) {
	var calls []aiToolCall
	for _, src := range []string{content, thinking} {
		for _, m := range gemmaCall.FindAllStringSubmatch(src, -1) {
			args := map[string]any{}
			for _, a := range gemmaArg.FindAllStringSubmatch(m[2], -1) {
				args[a[1]] = strings.TrimSpace(a[2] + a[3] + a[4])
			}
			calls = append(calls, aiToolCall{Function: aiFunc{Name: m[1], Arguments: args}})
		}
		if len(calls) == 0 {
			if m := jsonCall.FindString(src); m != "" {
				var f struct {
					Name       string         `json:"name"`
					Arguments  map[string]any `json:"arguments"`
					Parameters map[string]any `json:"parameters"`
				}
				if json.Unmarshal([]byte(m), &f) == nil && f.Name != "" {
					if f.Arguments == nil {
						f.Arguments = f.Parameters
					}
					calls = append(calls, aiToolCall{Function: aiFunc{Name: f.Name, Arguments: f.Arguments}})
				}
			}
		}
		if len(calls) == 0 && src == content {
			for _, m := range jsonCmd.FindAllStringSubmatch(content, -1) {
				calls = append(calls, aiToolCall{Function: aiFunc{Name: "run_command", Arguments: map[string]any{"command": m[1]}}})
			}
			rest := content
			if len(calls) > 0 {
				rest = jsonCmd.ReplaceAllString(content, "")
			}
			more, rest := parseInlineCommands(rest)
			if calls = dedupeCalls(append(calls, more...)); len(calls) > 0 {
				return calls, strings.Join(strings.Fields(toolWord.ReplaceAllString(rest, "$1")), " ")
			}
		}
		if len(calls) > 0 {
			if src == content {
				content = strings.TrimSpace(jsonCall.ReplaceAllString(gemmaCall.ReplaceAllString(content, ""), ""))
				content = strings.TrimSpace(strings.NewReplacer("<|tool_call>", "", "<tool_call|>", "", "<|tool_response>", "", "<tool_response|>", "", "```json", "", "```", "").Replace(content))
			}
			return calls, content
		}
	}
	return nil, content
}

var (
	stateEcho  = regexp.MustCompile(`\[estado:[^\]]*\]`)
	speakerTag = regexp.MustCompile(`(?i)(^|[\s.!?])(?:yo|` + regexp.QuoteMeta(botName) + `)\s*:\s*`)
)

// parseInlineCommands saca las órdenes del bot que el modelo escribió dentro
// del texto ("ok bro /stop ya voy /queue add chop abeto 32") en vez de llamar
// a run_command, y devuelve el texto sin ellas. Cada orden se come solo los
// argumentos que le caben (según su definición), así el resto sigue siendo chat.
// /cmd y /say (texto libre) no se reconocen aquí: no se sabe dónde acaban.
func parseInlineCommands(content string) ([]aiToolCall, string) {
	words := strings.Fields(content)
	var calls []aiToolCall
	var kept []string
	for i := 0; i < len(words); i++ {
		w := words[i]
		name := strings.ToLower(strings.TrimRight(strings.TrimPrefix(w, "/"), ".,;:!?"))
		c, ok := commands[name]
		if !strings.HasPrefix(w, "/") || !ok || aiDenied[name] || name == "cmd" || name == "say" || name == "help" {
			kept = append(kept, w)
			continue
		}
		cmd := []string{"/" + name}
		n := inlineArgs(name, c, words[i+1:])
		n += inlineUntil(words[i+1+n:])
		for _, a := range words[i+1 : i+1+n] {
			cmd = append(cmd, strings.TrimRight(a, ".,;!?"))
		}
		i += n
		calls = append(calls, aiToolCall{Function: aiFunc{Name: "run_command", Arguments: map[string]any{"command": strings.Join(cmd, " ")}}})
	}
	return calls, strings.Join(kept, " ")
}

// inlineArgs: cuántas palabras de rest son argumentos de la orden name.
func inlineArgs(name string, c command, rest []string) int {
	if name == "queue" && len(rest) > 0 {
		action := strings.ToLower(rest[0])
		if action != "add" && action != "insert" {
			n := 1
			for n < len(rest) && n < 3 && isNumArg(rest[n]) { // remove 2, move 1 3
				n++
			}
			return n
		}
		n := 1
		if action == "insert" && len(rest) > 1 && isNumArg(rest[1]) {
			n++
		}
		if n < len(rest) {
			inner := strings.ToLower(strings.TrimPrefix(rest[n], "/"))
			if ic, ok := commands[inner]; ok && inner != "queue" {
				return n + 1 + inlineArgs(inner, ic, rest[n+1:])
			}
		}
		return n
	}
	n := 0
	for _, a := range c.args {
		if n >= len(rest) || a.rest {
			break
		}
		w := strings.TrimRight(rest[n], ".,;!?")
		lw := strings.ToLower(w)
		switch {
		case slices.Contains(a.choices, lw):
		case (a.kind == argPlayer || a.orPlayer) && (lw == "yo" || lw == "me" || validName.MatchString(w) && !isNumArg(w) || isNumArg(w) && name == "goto"):
		case a.kind != argPlayer && a.kind != argChoice && isNumArg(w):
		default:
			return n
		}
		n++
	}
	return n
}

// inlineUntil: cuántas palabras de rest son un "until …" (las condiciones de
// parada de una tarea): until got logs 64 or time 20m.
func inlineUntil(rest []string) int {
	if len(rest) == 0 || !strings.EqualFold(rest[0], "until") {
		return 0
	}
	n, ok := 1, false
	for n < len(rest) {
		size := 0
		switch strings.ToLower(rest[n]) {
		case "got", "have", "count":
			size = 3
		case "time":
			size = 2
		}
		if size == 0 || n+size > len(rest) {
			break
		}
		cond := rest[n : n+size]
		if _, err := automation.ParseUntil(trimPunct(cond)); err != nil {
			break
		}
		n, ok = n+size, true
		if n+1 < len(rest) && strings.EqualFold(rest[n], "or") {
			n++
			continue
		}
		break
	}
	if !ok {
		return 0
	}
	if strings.EqualFold(rest[n-1], "or") { // un "or" colgando: no es suyo
		n--
	}
	return n
}

func trimPunct(ws []string) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = strings.TrimRight(w, ".,;!?")
	}
	return out
}

// isSay: la orden escribe en el chat público (/say, o texto sin /).
func isSay(cmd string) bool {
	f := strings.Fields(strings.ToLower(strings.TrimSpace(cmd)))
	return len(f) > 0 && (f[0] == "/say" || f[0] == "say" || f[0] == "/cmd" && len(f) > 1 && (f[1] == "say" || f[1] == "me"))
}

// asksToSay: el mensaje pide decir algo en el chat ("pon en el chat…",
// "di a todos…", "escribe…", "anuncia…").
func asksToSay(text string) bool {
	t := " " + strings.ToLower(text) + " "
	for _, w := range []string{"chat", " di ", " dí ", " decí", " deci ", "escrib", " pon ", " pone ", " poné", "anunci", " say ", "publica", " grita", "a todos"} {
		if strings.Contains(t, w) {
			return true
		}
	}
	return false
}

// dedupeCalls quita las órdenes repetidas (el modelo a veces escribe la misma dos veces).
func dedupeCalls(calls []aiToolCall) []aiToolCall {
	var out []aiToolCall
	seen := map[string]bool{}
	for _, c := range calls {
		k := c.Function.Name + fmt.Sprint(c.Function.Arguments["command"])
		if !seen[k] {
			seen[k] = true
			out = append(out, c)
		}
	}
	return out
}

func isNumArg(w string) bool {
	w = strings.TrimRight(w, ".,;!?")
	if w == "~" {
		return true
	}
	if d, err := time.ParseDuration(strings.ToLower(w)); err == nil && d > 0 { // 10m, 1h (los minutos de /guard)
		return true
	}
	_, err := strconv.ParseFloat(w, 64)
	return err == nil
}

var mdNoise = strings.NewReplacer("**", "", "__", "", "`", "", "### ", "", "## ", "", "# ", "", "\r", "")

// cleanReply deja la respuesta en texto plano de una línea (es para el chat).
func cleanReply(s string) string {
	s = mdNoise.Replace(s)
	s = stateEcho.ReplaceAllString(s, "")
	s = speakerTag.ReplaceAllString(s, "$1")
	lines := strings.Split(s, "\n")
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "-*• "))
		if l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, " ")
}

func truncRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// whisperLong manda un texto largo en varios privados (hasta 4, cortando
// entre palabras): el chat de Minecraft corta a 256 caracteres.
func (b *Bot) whisperLong(to, text string) {
	if to == consoleWho {
		return
	}
	parts := splitChat(text, 190, 4)
	for i, p := range parts {
		if i > 0 {
			time.Sleep(600 * time.Millisecond) // sin que el antispam lo eche
		}
		b.whisper(to, p)
	}
}

// splitChat parte s en trozos de como mucho n bytes, entre palabras.
func splitChat(s string, n, maxParts int) []string {
	var parts []string
	for s != "" && len(parts) < maxParts {
		if len(s) <= n {
			parts = append(parts, s)
			break
		}
		cut := strings.LastIndexByte(s[:n], ' ')
		if cut < n/2 {
			cut = n
			for cut > 0 && !utf8.RuneStart(s[cut]) {
				cut--
			}
		}
		parts = append(parts, strings.TrimSpace(s[:cut]))
		s = strings.TrimSpace(s[cut:])
		if len(parts) == maxParts && s != "" {
			last := parts[len(parts)-1]
			for len(last) > n-3 { // que quepa el "…"
				_, sz := utf8.DecodeLastRuneInString(last)
				last = last[:len(last)-sz]
			}
			parts[len(parts)-1] = last + "…"
		}
	}
	return parts
}

// ---- copia del log ----

// logTap pasa el log a su salida de siempre y, mientras una herramienta corre,
// se queda una copia (lo que la orden escribe es lo que ve el modelo).
type logTap struct {
	mu  sync.Mutex
	out io.Writer
	buf *bytes.Buffer
}

func (t *logTap) Write(p []byte) (int, error) {
	t.mu.Lock()
	if t.buf != nil && t.buf.Len() < 16<<10 {
		t.buf.Write(p)
	}
	out := t.out
	t.mu.Unlock()
	return out.Write(p)
}

// capture ejecuta f y devuelve lo que se escribió en el log hasta wait después.
func (t *logTap) capture(f func() error, wait time.Duration) (string, error) {
	buf := &bytes.Buffer{}
	t.mu.Lock()
	t.buf = buf
	t.mu.Unlock()
	err := f()
	time.Sleep(wait)
	t.mu.Lock()
	t.buf = nil
	out := buf.String()
	t.mu.Unlock()
	var lines []string
	for _, l := range strings.Split(stripANSI(out), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(stripTime(l), "[IA]") {
			continue
		}
		lines = append(lines, stripTime(l))
	}
	return strings.Join(lines, "\n"), err
}

var logTime = regexp.MustCompile(`^(\d{4}/\d\d/\d\d )?\d\d:\d\d:\d\d `)

func stripTime(l string) string { return logTime.ReplaceAllString(l, "") }

// ---- /ai en la consola ----

// cmdAI: /ai <texto> | info | reset [jugador] | compact [jugador] | on | off.
func cmdAI(b *Bot, args []string) error {
	if b.AI == nil {
		return errf("the AI is disabled (start with --ai http://host:11434)", "la IA está desactivada (arranca con --ai http://host:11434)")
	}
	a := b.AI
	if len(args) == 0 {
		return errf("usage: /ai <message> | info | reset [player] | compact [player] | on | off", "uso: /ai <mensaje> | info | reset [jugador] | compact [jugador] | on | off")
	}
	who := consoleWho
	if len(args) == 2 {
		who = args[1]
	}
	switch strings.ToLower(args[0]) {
	case "on", "off":
		if len(args) == 1 {
			a.off.Store(strings.EqualFold(args[0], "off"))
			log.Printf("[IA] %s", map[bool]string{true: "apagada: /msg vuelve a ser solo órdenes", false: "encendida"}[a.off.Load()])
			return nil
		}
	case "info":
		if len(args) == 1 {
			a.mu.Lock()
			defer a.mu.Unlock()
			lines := []string{fmt.Sprintf("[IA] %s en %s, contexto %d tokens; usa como mucho %d (%.0f%%, compacta al %.0f%% de eso); calibrado ×%.2f; %d conversaciones%s",
				a.model, a.url, a.numCtx, a.budget(), 100*a.budgetFrac, 100*aiCompactAt, a.ratio, len(a.convs), map[bool]string{true: " — APAGADA", false: ""}[a.off.Load()])}
			for _, c := range a.convs {
				lines = append(lines, fmt.Sprintf("  %s: %d mensajes, ~%d tokens (%d%% del contexto, %d%% del presupuesto), %d compactaciones, hace %s",
					c.who, len(c.msgs), c.tokens, 100*c.tokens/a.numCtx, 100*c.tokens/max(1, a.budget()), c.compacts, time.Since(c.last).Round(time.Second)))
			}
			log.Print(strings.Join(lines, "\n"))
			return nil
		}
	case "reset":
		if len(args) <= 2 {
			a.mu.Lock()
			delete(a.convs, strings.ToLower(who))
			a.mu.Unlock()
			log.Printf("[IA] conversación con %s olvidada", who)
			return nil
		}
	case "compact":
		if len(args) <= 2 {
			c := a.conv(who)
			go func() {
				c.mu.Lock()
				defer c.mu.Unlock()
				if err := a.compact(b.ctx, c); err != nil {
					log.Printf("[IA] compactar: %v", err)
				}
			}()
			return nil
		}
	}
	text := strings.Join(args, " ")
	log.Printf("[tú → IA] %s", text)
	go func() {
		reply, err := b.aiAsk(consoleWho, text)
		if err != nil {
			log.Printf("[IA] %v", err)
			return
		}
		log.Printf("[IA] %s", reply)
	}()
	return nil
}
