package main

// Lo que ve el modelo (llm.go): la lista de órdenes y el esquema JSON de su
// respuesta, sacados del registro de órdenes. Los modelos pequeños no son
// fiables con las herramientas nativas, así que contestan siempre un JSON
//
//	{"say": "...", "actions": [{"command": "chop", "args": ["oak","~","~","~"]}], "done": false}
//
// que se valida contra la lista blanca llmAllowed antes de ejecutar nada:
// jamás /cmd (comandos del servidor) ni /quit.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// llmAllowed son las órdenes que puede usar la IA (las que no existan se saltan:
// la cola, /afk… las añaden otros). Nada de cmd, quit, lang, sprint ni help.
var llmAllowed = []string{"goto", "follow", "stop", "guard", "say", "fight", "chop", "excavate", "mine", "drop", "inventory", "status", "afk", "queue"}

// llmOwnTools: órdenes cuyo resultado son datos (la orden solo escribe en el log):
// las contesta el anfitrión.
var llmOwnTools = map[string]bool{"status": true, "inventory": true}

// llmWait es la pseudo-orden que espera a que acabe la tarea ("wait" ["60"]).
const llmWait = "wait"

const llmMaxActions = 4 // por respuesta

func allowedLLM(name string) bool {
	for _, n := range llmAllowed {
		if n == name {
			return true
		}
	}
	return false
}

// llmNames: las órdenes que existen de verdad y se le dejan, más wait.
func llmNames() []string {
	var out []string
	for _, n := range llmAllowed {
		if _, ok := commands[n]; ok {
			out = append(out, n)
		}
	}
	return append(out, llmWait)
}

// llmSchema: el formato de la respuesta (el campo "format" de Ollama).
func llmSchema() map[string]any {
	names := make([]any, 0, 16)
	for _, n := range llmNames() {
		names = append(names, n)
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"say": map[string]any{"type": "string"},
			"actions": map[string]any{
				"type":     "array",
				"maxItems": llmMaxActions,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command": map[string]any{"type": "string", "enum": names},
						"args":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					},
					"required": []any{"command", "args"},
				},
			},
			"done": map[string]any{"type": "boolean"},
		},
		"required": []any{"say", "actions", "done"},
	}
}

// firstSentence: la primera frase de una explicación, recortada.
func firstSentence(s string, n int) string {
	if i := strings.Index(s, ". "); i > 0 {
		s = s[:i]
	}
	if i := strings.Index(s, ":"); i > 0 && i < n {
		s = s[:i]
	}
	return clip(strings.TrimSpace(s), n)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// llmHints: lo que el modelo necesita saber de algunas órdenes y que la
// primera frase de la ayuda no dice (probado con gemma4: sin esto inventa
// coordenadas para /mine, se olvida del /drop o dice "oaks").
var llmHints = map[string]string{
	"chop":  "chop the trees of those woods (oak, birch, spruce, jungle, acacia, dark_oak, cherry…; singular, comma-separated) within 24 blocks of the point; no coordinates = where it stands",
	"mine":  "strip-mine ores (iron, coal, copper, gold, diamond, redstone, lapis, emerald); x y z = the chest it takes pickaxes from and leaves ores in (no coordinates = the chest where it stands); total <n> = stop after n ores",
	"drop":  "throw an item (its exact name from the inventory, e.g. raw_iron, oak_log) and how many; with a player (me = the owner) it walks to them and throws it to them",
	"guard": "bodyguard: follow a player and attack whoever hits them",
}

// llmCommandList: una línea por orden, "/chop <woods> <x> <y> <z> - chop trees…".
func llmCommandList() string {
	var sb strings.Builder
	for _, n := range llmAllowed {
		c, ok := commands[n]
		if !ok {
			continue
		}
		u := c.usage(n)
		if n == "excavate" {
			u = "/excavate <x> <y> <z> <x2> <y2> <z2> | chunk <y1> <y2>"
		}
		d := firstSentence(c.desc.en, 90)
		if h, ok := llmHints[n]; ok {
			d = h
		}
		fmt.Fprintf(&sb, "%s - %s\n", u, d)
	}
	sb.WriteString("wait [seconds] - wait until the current task ends (not for follow/guard)\n")
	return sb.String()
}

// llmAction es una acción tal como la devuelve el modelo.
type llmAction struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// llmReply es la respuesta del modelo.
type llmReply struct {
	Say     string      `json:"say"`
	Actions []llmAction `json:"actions"`
	Done    bool        `json:"done"`
}

// parseLLMReply lee el JSON del modelo (tolera una cerca de ``` alrededor).
func parseLLMReply(s string) (llmReply, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "{"); i > 0 {
		s = s[i:]
	}
	if j := strings.LastIndex(s, "}"); j >= 0 && j < len(s)-1 {
		s = s[:j+1]
	}
	var r llmReply
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return r, err
	}
	return r, nil
}

// llmLine valida una acción y la convierte en una línea de consola
// ("/chop oak ~ ~ ~"). Rechaza lo que no está en la lista blanca, otra orden
// metida en un argumento y un "/" al principio del /say.
func llmLine(a llmAction) (string, error) {
	name := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(a.Command), "/"))
	if !allowedLLM(name) {
		return "", fmt.Errorf("unknown command %q", a.Command)
	}
	if _, ok := commands[name]; !ok {
		return "", fmt.Errorf("unknown command %q", a.Command)
	}
	var args []string
	for _, v := range a.Args {
		if v = strings.TrimSpace(v); v != "" {
			if name != "say" && strings.ContainsAny(v, "\r\n") {
				return "", fmt.Errorf("bad argument")
			}
			args = append(args, v)
		}
	}
	if name == "say" {
		t := strings.Join(args, " ")
		if t == "" || strings.HasPrefix(t, "/") {
			return "", fmt.Errorf("say needs plain chat text, never a /command")
		}
		return "/say " + strings.NewReplacer("\r", " ", "\n", " ").Replace(t), nil
	}
	if name == "excavate" && len(args) > 0 && strings.EqualFold(args[0], "chests") {
		return "", fmt.Errorf("not allowed")
	}
	for _, v := range args {
		if strings.ContainsAny(v, ";\r\n") { // (" ; " encadena órdenes: /stop ; /op alguien)
			return "", fmt.Errorf("not allowed")
		}
		w := strings.ToLower(strings.TrimLeft(strings.Fields(v)[0], "/"))
		if w == "cmd" || w == "quit" {
			return "", fmt.Errorf("not allowed")
		}
	}
	return strings.TrimSpace("/" + name + " " + strings.Join(args, " ")), nil
}
