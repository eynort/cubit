package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/automation"
	"minecraft-bot/combat"
)

// argKind es qué tipo de valor lleva un argumento: decide el autocompletado.
type argKind int

const (
	argText    argKind = iota // texto libre
	argPlayer                 // un jugador conectado: Tab recorre los nombres
	argChoice                 // una de varias opciones fijas
	argNumber                 // un número (y las opciones fijas que haya, como "all")
	argList                   // varias de las opciones, separadas por comas (diamond,iron)
	argCoord                  // una coordenada (~ = la del bot): en la primera se sugiere "~ ~ ~"
	argCommand                // otra orden entera (/queue add <orden>): se completa como ella
)

type arg struct {
	name       string
	kind       argKind
	choices    []string        // para argChoice (y opciones fijas de un argPlayer, como "in")
	choiceDesc text            // la explicación de esas opciones
	hint       text            // otra forma de rellenarlo (p.ej. coordenadas), como pista
	rest       bool            // se come el resto de la línea (texto con espacios)
	corner     bool            // empieza otro grupo de coordenadas (la segunda esquina): se sugiere "~ ~ ~" aparte
	items      bool            // sugiere también lo que lleva el bot en el inventario (/drop)
	amountOf   int             // >0: es la cantidad del objeto que va en el argumento amountOf-1 (sugiere el total)
	orPlayer   bool            // en su lugar puede ir un jugador (/drop dirt Alex): los sugiere y acepta "yo"
	desc       text            // qué es este argumento (sale al lado de la sugerencia)
	descs      map[string]text // la explicación de cada opción
	dyn        func() []string // opciones que se calculan al completar (los schematics…)
	opt        bool            // se puede no poner
}

// sub es un subcomando: una palabra fija tras la orden (/mine chest …) con sus argumentos.
type sub struct {
	name string
	desc text
	args []arg
}

type command struct {
	desc text
	args []arg
	run  func(b *Bot, args []string) error
	// subs: los subcomandos; la primera palabra es uno de ellos o, si no, el
	// primer argumento de args (/mine chest … o /mine diamond)
	subs []sub
}

// usages son formas de uso escritas a mano, para órdenes con variantes.
var usages = map[string]string{
	"fight":     "/fight <player> | in <x> <z> [<x2> <z2>]",
	"goto":      "/goto <player> | <x> <z>",
	"mine":      "/mine <ore[,ore…]> [<amount> | all] | chest [<x> <y> <z> | off | list] | list",
	"drop":      "/drop [<item> [<amount>] [<player>] | <player>]",
	"foodchest": "/foodchest [add [<x> <y> <z>] | remove [<x> <y> <z>] | list | clear | now]",
}

// usage es la forma de uso: "/fight <player>".
func (c command) usage(name string) string {
	if u, ok := usages[name]; ok {
		return u
	}
	var alts []string
	for _, s := range c.subs {
		alts = append(alts, s.name+subUsage(s))
	}
	var plain []string
	for _, a := range c.args {
		plain = append(plain, argUsage(a))
	}
	if len(plain) > 0 {
		alts = append([]string{strings.Join(plain, " ")}, alts...)
	}
	if len(alts) == 0 {
		return "/" + name
	}
	return "/" + name + " " + strings.Join(alts, " | ")
}

// commands son las órdenes del bot (se escriben con "/", como en Minecraft;
// siempre en inglés). Para añadir una, agrégala en init: el autocompletado y
// /help salen solos. Las explicaciones van en inglés y en español (/lang).
var commands map[string]command

func init() {
	player := []arg{{name: "player", kind: argPlayer}}
	xyz := func(opt bool, alt ...string) []arg { // <x> <y> <z>, o en su lugar una de alt
		return []arg{{name: "x", kind: argCoord, opt: opt, choices: alt}, {name: "y", kind: argCoord, opt: opt}, {name: "z", kind: argCoord, opt: opt}}
	}
	nearest := text{"<x> <y> <z> of the chest; without them, the nearest one (5 blocks)", "<x> <y> <z> del cofre; sin ellas, el más cercano (5 bloques)"}
	withDesc := func(as []arg, d text) []arg {
		for i := range as {
			as[i].desc = d
		}
		return as
	}
	oreDescs := map[string]text{}
	for k, v := range automation.OreBrief() {
		oreDescs[k] = text{v, v}
	}
	commands = map[string]command{
		"help": {desc: text{"this help", "esta ayuda"}, run: cmdHelp},
		"lang": {desc: text{"language of the explanations (commands are always in English)", "idioma de las explicaciones (las órdenes siempre en inglés)"},
			args: []arg{{name: "language", kind: argChoice, choices: []string{"en", "es"},
				descs: map[string]text{"en": {"English", "inglés"}, "es": {"Spanish", "español"}}}}, run: cmdLang},
		"fight": {
			desc: text{"fight a player, or every player in an area", "pelear con un jugador, o con todos los de una zona"},
			args: []arg{{name: "player", kind: argPlayer, desc: text{"the player to fight", "el jugador con el que pelear"}}},
			subs: []sub{{name: "in", desc: text{"every player inside an area: a point (radius 8) or two corners", "todos los jugadores dentro de una zona: un punto (radio 8) o dos esquinas"},
				args: []arg{{name: "x", kind: argCoord}, {name: "z", kind: argCoord}, {name: "x2", kind: argCoord, opt: true}, {name: "z2", kind: argCoord, opt: true}}}},
			run: cmdFight,
		},
		"goto": {
			desc: text{"go to a player or to some coordinates", "ir hasta un jugador o hasta unas coordenadas"},
			args: []arg{{name: "player", kind: argPlayer, hint: text{"<x> <z>: coordinates (~ = where the bot is)", "<x> <z>: unas coordenadas (~ = donde está el bot)"}},
				{name: "z", kind: argCoord, desc: text{"the z coordinate", "la coordenada z"}}},
			run: cmdGoto,
		},
		"follow": {desc: text{"follow a player non-stop (until /stop)", "seguir a un jugador sin parar (hasta /stop)"}, args: player, run: cmdFollow},
		"stop":   {desc: text{"stop fighting and walking (and pause the queue)", "dejar de pelear y de caminar (y pausar la cola)"}, run: cmdStop},
		"guard": {desc: text{"bodyguard: follows a player and attacks whoever hits them (until /stop, or for the given minutes)", "guardaespaldas: sigue a un jugador y ataca a quien le pegue (hasta /stop, o los minutos que se digan)"},
			args: []arg{{name: "player", kind: argPlayer}, {name: "minutes", kind: argNumber, opt: true, desc: text{"for how many minutes (without it, until /stop)", "cuántos minutos (sin ello, hasta /stop)"}}}, run: cmdGuard},
		"cmd": {desc: text{"run a server command as a player (even one named like a bot command: /cmd help, /cmd tpa Alex)", "ejecutar un comando del servidor como jugador (aunque se llame igual que una orden del bot: /cmd help, /cmd tpa Alex)"},
			args: []arg{{name: "command", rest: true, desc: text{"the server command, without /", "el comando del servidor, sin /"}}}, run: cmdServer},
		"say": {desc: text{"write in the chat (or type without /)", "escribir en el chat (o escribe sin /)"},
			args: []arg{{name: "text", rest: true, desc: text{"what to write", "lo que escribir"}}}, run: cmdSay},
		"status": {desc: text{"health, food, position and combat", "vida, comida, posición y combate"}, run: cmdStatus},
		"limits": {desc: text{"what the bot never does: blocks it never breaks, the area it may dig in, PvP (saved in bot.json)", "lo que el bot no hace nunca: bloques que no rompe, la zona donde puede picar, PvP (se guarda en bot.json)"},
			args: []arg{{name: "what", kind: argChoice, opt: true, choices: []string{"area", "pvp", "deny", "allow", "reset"},
				descs: map[string]text{"area": {"area <x1> <z1> <x2> <z2> | area off", "area <x1> <z1> <x2> <z2> | area off"}, "pvp": {"pvp on|off", "pvp on|off"},
					"deny": {"deny <block>: never break it", "deny <bloque>: no romperlo nunca"}, "allow": {"allow <block>: it may break it", "allow <bloque>: puede romperlo"}, "reset": {"the default limits", "los límites de siempre"}}},
				{name: "value", rest: true, opt: true}}, run: cmdLimits},
		"mem":    {desc: text{"memory, GC, loaded chunks and entities (the same as --monitor)", "memoria, GC, chunks cargados y entidades (lo mismo que --monitor)"}, run: cmdMem},
		"sprint": {desc: text{"allow or forbid sprinting", "permitir o prohibir correr"},
			args: []arg{{name: "mode", kind: argChoice, choices: []string{"on", "off"},
				descs: map[string]text{"on": {"it may sprint", "puede correr"}, "off": {"it always walks", "siempre camina"}}}}, run: cmdSprint},
		"elytra": {desc: text{"on/off: let /goto fly with elytra and rockets when far (on by default); wear: put the elytra on now", "on/off: dejar que /goto vuele con élitros y cohetes cuando está lejos (sí por defecto); wear: ponerse los élitros ya"},
			args: []arg{{name: "mode", kind: argChoice, choices: []string{"on", "off", "wear"},
				descs: map[string]text{"on": {"/goto may fly when far", "/goto puede volar si está lejos"}, "off": {"/goto always walks", "/goto siempre a pie"}, "wear": {"put the elytra on now", "ponerse los élitros ya"}}}}, run: cmdElytra},
		"chop": {
			desc: text{"chop trees: with the best axe it carries; without one, it makes a stone axe first; then the base (table and double chest)",
				"talar: con la mejor hacha que lleve; sin ninguna, primero se hace una de piedra; luego la base (mesa y cofre doble)"},
			args: []arg{{name: "wood", kind: argChoice, choices: automation.WoodKinds(), desc: text{"which wood", "qué madera"}},
				{name: "amount", kind: argNumber, opt: true, desc: text{"how many logs (without it, until /stop)", "cuántos troncos (sin ello, hasta /stop)"}}},
			run: cmdChop,
		},
		"mine": {
			desc: text{"mine ores by strip mining, each with its own strategy (Y level, tunnel spacing and length): /mine diamond, /mine iron,copper 200, /mine debris. /mine list shows the strategies. /mine chest marks the chest of its dimension (one per dimension): it takes pickaxes (without breaking any), food and armor from it and leaves what it mines there; if the ore's dimension has none it goes to the other one. With no usable pickaxe it makes its own",
				"minar por strip mining, cada mineral con su estrategia (altura, separación y largo de las galerías): /mine diamond, /mine iron,copper 200, /mine debris. /mine list enseña las estrategias. /mine chest marca el cofre de su dimensión (uno por dimensión): de ahí coge picos (sin romper ninguno), comida y armadura y ahí deja lo minado; si en la dimensión del mineral no hay, va al de la otra. Sin pico que valga se hace uno"},
			args: []arg{{name: "ores", kind: argList, choices: automation.OreKeys(), descs: oreDescs},
				{name: "amount", kind: argNumber, opt: true, choices: []string{"all"}, desc: text{"how many (without it, or all: until /stop)", "cuántos (sin ello, o all: hasta /stop)"},
					descs: map[string]text{"all": {"until /stop", "hasta /stop"}}}},
			subs: []sub{
				{name: "chest", desc: text{"mark the mining chest of this dimension (the nearest one, or <x> <y> <z>); off forgets it; list shows them", "marcar el cofre de minar de esta dimensión (el más cercano, o <x> <y> <z>); off lo olvida; list los enseña"},
					args: func() []arg {
						as := withDesc(xyz(true, "off", "list"), nearest)
						as[0].descs = map[string]text{"off": {"forget the chest of this dimension", "olvidar el cofre de esta dimensión"},
							"list": {"show the mining chests", "ver los cofres de minar"}}
						return as
					}()},
				{name: "list", desc: text{"the strategy of every ore (Y level, spacing, why)", "la estrategia de cada mineral (altura, separación, por qué)"}},
			},
			run: cmdMine,
		},
		"foodchest": {
			desc: text{"chests the bot may take food from (besides its storage and the chests by the base): add marks the nearest one (or <x> <y> <z>), remove/clear forget, list shows them, now goes for food now. With no food it goes there before hunting, and takes only what it needs",
				"cofres de los que el bot puede coger comida (además de su almacén y los de junto a la base): add marca el más cercano (o <x> <y> <z>), remove/clear los olvida, list los muestra, now va ya a por comida. Sin comida va allí antes de cazar, y coge solo lo que necesita"},
			subs: []sub{
				{name: "add", desc: text{"mark a food chest", "marcar un cofre de comida"}, args: withDesc(xyz(true), nearest)},
				{name: "remove", desc: text{"forget a food chest", "olvidar un cofre de comida"}, args: withDesc(xyz(true), nearest)},
				{name: "list", desc: text{"show them", "enseñarlos"}},
				{name: "clear", desc: text{"forget them all", "olvidarlos todos"}},
				{name: "now", desc: text{"go for food now", "ir ya a por comida"}},
			},
			run: cmdFoodChest,
		},
		"excavate": {
			desc: text{"dig out a whole box of blocks between two corners (/excavate 10 70 10 30 50 30), 4 blocks deep at a time: each 4-block pass through the whole box before going down to the next (the ground goes down evenly); in each pass it stands on the bottom and digs the 4 blocks high it reaches around it, then moves on, and picks up what fell when the pass is done. It uses the pickaxes it carries and the ones in the mining chest (/mine chest) or the storage, never breaking one, and when full it takes what it dug to the chests",
				"vaciar una caja de bloques entre dos esquinas (/excavate 10 70 10 30 50 30), de 4 en 4 bloques de alto: cada pasada de 4 en toda la caja antes de bajar a la siguiente (el terreno baja parejo); en cada pasada se pone en el fondo y pica los 4 de alto que alcanza alrededor, y sigue, y al acabarla recoge lo que cayó. Usa los picos que lleve y los del cofre de minar (/mine chest) o del almacén, sin romper ninguno, y cuando se llena lleva lo cavado a los cofres"},
			args: append(withDesc(xyz(false), text{"one corner (~ = where the bot is)", "una esquina (~ = donde está el bot)"}),
				withDesc([]arg{{name: "x2", kind: argCoord, corner: true}, {name: "y2", kind: argCoord}, {name: "z2", kind: argCoord}}, text{"the opposite corner", "la esquina opuesta"})...),
			run: cmdExcavate,
		},
		"protected": {
			desc: text{"places the server said \"you don't have permission\" (claims, spawn): the bot does not dig, build or look for trees within 16 blocks of them. list shows them, here forgets the ones around the bot, clear forgets them all",
				"sitios donde el servidor dijo \"no tienes permiso\" (terrenos reclamados, el spawn): a 16 bloques de ellos el bot no cava, no construye ni busca árboles. list los muestra, here olvida los de alrededor del bot, clear los olvida todos"},
			subs: []sub{
				{name: "list", desc: text{"show them", "enseñarlos"}},
				{name: "here", desc: text{"forget the ones around the bot", "olvidar los de alrededor del bot"}},
				{name: "clear", desc: text{"forget them all", "olvidarlos todos"}},
			},
			run: cmdProtected,
		},
		"farm": {
			desc: text{"farm by the base: wheat field and sheep pen (brings sheep in, breeds and shears them) — until /stop",
				"granja junto a la base: campo de trigo y corral de ovejas (las mete, las cría y las esquila) — hasta /stop"},
			run: cmdFarm,
		},
		"beds": {
			desc: text{"make beds (wool from sheep: shears the ones around and in the pen; tends the farm if short)",
				"hacer camas (lana de las ovejas: esquila las de alrededor y las del corral; cuida la granja si falta)"},
			args: []arg{{name: "amount", kind: argNumber, opt: true, desc: text{"how many beds (8 if not given)", "cuántas camas (8 si no se dice)"}}},
			run:  cmdBeds,
		},
		"base": {
			desc: text{"the storage: \"here\" sets the chests' base where the bot stands; \"info\" shows it",
				"el almacén: \"here\" pone la base de los cofres donde está el bot; \"info\" la muestra"},
			args: []arg{{name: "action", kind: argChoice, choices: []string{"here", "info"},
				descs: map[string]text{"here": {"the base where the bot stands", "la base donde está el bot"}, "info": {"where it is and its chests", "dónde está y sus cofres"}}}},
			run: cmdBase,
		},
		"queue": {
			desc: text{"the command queue (goto, guard, fight, chop, mine, beds, farm, build): list, add, remove, reorder, skip, pause",
				"la cola de órdenes (goto, guard, fight, chop, mine, beds, farm, build): ver, añadir, quitar, reordenar, saltar, pausar"},
			subs: []sub{
				{name: "list", desc: text{"show the queue", "ver la cola"}},
				{name: "add", desc: text{"add a command at the end", "añadir una orden al final"}, args: []arg{{name: "command", kind: argCommand, rest: true}}},
				{name: "insert", desc: text{"put a command at a position", "poner una orden en una posición"},
					args: []arg{{name: "position", kind: argNumber, desc: text{"where (1 = next)", "dónde (1 = la siguiente)"}}, {name: "command", kind: argCommand, rest: true}}},
				{name: "remove", desc: text{"remove one", "quitar una"}, args: []arg{{name: "n", kind: argNumber, desc: text{"its position (see /queue)", "su posición (mira /queue)"}}}},
				{name: "move", desc: text{"move one", "mover una"}, args: []arg{{name: "from", kind: argNumber, desc: text{"its position", "su posición"}}, {name: "to", kind: argNumber, desc: text{"the new position", "la posición nueva"}}}},
				{name: "up", desc: text{"one place earlier", "un puesto antes"}, args: []arg{{name: "n", kind: argNumber, desc: text{"its position", "su posición"}}}},
				{name: "down", desc: text{"one place later", "un puesto después"}, args: []arg{{name: "n", kind: argNumber, desc: text{"its position", "su posición"}}}},
				{name: "skip", desc: text{"stop the running one and go to the next", "parar la que va y pasar a la siguiente"}},
				{name: "clear", desc: text{"remove all the pending ones", "quitar todas las pendientes"}},
				{name: "pause", desc: text{"do not start the next ones", "no empezar las siguientes"}},
				{name: "resume", desc: text{"go on", "seguir"}},
			},
			run: cmdQueue,
		},
		"inventory": {desc: text{"what the bot carries: equipment, hotbar slot by slot and the inventory; grid: like the game screen",
			"lo que lleva el bot: el equipo, la barra casilla a casilla y el inventario; grid: como la pantalla del juego"},
			args: []arg{{name: "view", kind: argChoice, opt: true, choices: []string{"grid"},
				descs: map[string]text{"grid": {"like the game screen (wide)", "como la pantalla del juego (ancha)"}}}}, run: cmdInventory},
		"around": {desc: text{"write the blocks around the bot (layer by layer) to the log", "escribir en el log los bloques alrededor del bot (capa a capa)"}, run: cmdAround},
		"debug":  {desc: text{"write where every part of the bot is (goroutine stacks) to the session log (--logs)", "escribir en el log de sesión (--logs) dónde está cada parte del bot (las pilas de las goroutines)"}, run: cmdDebug},
		"diag":   {desc: text{"tests jumps and moves and counts the server's corrections", "prueba saltos y movimientos y cuenta correcciones del servidor"}, run: cmdDiag},
		"drop": {desc: text{"throw on the ground everything it carries, or one item (all of it, or the amount given: /drop dirt 10); with a player, it goes to them and throws it to them (/drop Alex, /drop dirt 10 Alex)",
			"tirar al suelo todo lo que lleva, o un objeto (todo, o la cantidad que se diga: /drop dirt 10); con un jugador, va hasta él y se lo tira (/drop Alex, /drop dirt 10 Alex)"},
			args: []arg{{name: "item", kind: argPlayer, items: true}, {name: "amount", amountOf: 1, orPlayer: true}, {name: "player", kind: argPlayer}}, run: cmdDrop},
		"quit": {desc: text{"disconnect and exit", "desconectar y salir"}, run: cmdQuit},
		"ai": {desc: text{"talk to the AI (gemma4 in Ollama), or: info, reset [player], compact [player], on, off",
			"hablar con la IA (gemma4 en Ollama), o: info, reset [jugador], compact [jugador], on, off"},
			args: []arg{{name: "message", rest: true, desc: text{"what to tell it", "lo que decirle"}}},
			subs: []sub{
				{name: "info", desc: text{"model, context and conversations", "modelo, contexto y conversaciones"}},
				{name: "reset", desc: text{"forget a conversation (yours without a player)", "olvidar una conversación (la tuya sin jugador)"}, args: []arg{{name: "player", kind: argPlayer, opt: true}}},
				{name: "compact", desc: text{"summarise a conversation now", "resumir una conversación ya"}, args: []arg{{name: "player", kind: argPlayer, opt: true}}},
				{name: "on", desc: text{"answer /msg with the AI", "contestar los /msg con la IA"}},
				{name: "off", desc: text{"/msg are only commands", "los /msg son solo órdenes"}},
			},
			run: cmdAI},
	}
	registerBuildCommand()
}

// exec ejecuta una línea de la consola:
//
//	/orden args   una orden del bot
//	/otra cosa    un comando del servidor (si no es del bot)
//	texto         un mensaje al chat
func (b *Bot) exec(line string) {
	if err := b.run(line); err != nil {
		log.Print(err)
	}
}

// run es exec devolviendo el error (para contestar a quien mandó la orden por /msg).
func (b *Bot) run(line string) error {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	if !strings.HasPrefix(line, "/") {
		if err := b.Chat.SendMessage(line); err != nil {
			return fmt.Errorf("chat: %w", err)
		}
		return nil
	}
	fields := strings.Fields(strings.TrimPrefix(line, "/"))
	if len(fields) == 0 {
		return nil
	}
	c, ok := commands[strings.ToLower(fields[0])]
	if !ok {
		if secret(line) { // /login, /register…: la contraseña, ni en el log
			log.Printf("→ servidor: /%s ***", fields[0])
			b.noteManualAuth(line) // (y se recuerda para este servidor)
		} else {
			log.Printf("→ servidor: %s", line)
		}
		if err := b.Chat.SendCommand(strings.TrimPrefix(line, "/")); err != nil {
			return fmt.Errorf("comando: %w", err)
		}
		return nil
	}
	// una orden directa que mueve al bot le quita el control a la cola: la de
	// la cola en marcha se retoma después
	if name := strings.ToLower(fields[0]); b.Queue != nil && (queueKinds[name] || name == "follow" || name == "diag") {
		b.Queue.Interrupt()
	}
	return b.runCommand(c, fields[0], fields[1:])
}

// runCommand ejecuta una orden del bot con sus argumentos; un "until …" al
// final son las condiciones de parada del trabajo que empiece (startWork se
// las lleva). La usan la consola, los /msg y la cola.
func (b *Bot) runCommand(c command, name string, rawArgs []string) error {
	args, conds, err := splitConds(c, rawArgs)
	if err != nil {
		return fmt.Errorf("/%s: %w", name, err)
	}
	if conds == nil {
		if err := c.run(b, args); err != nil {
			return fmt.Errorf("/%s: %w", name, err)
		}
		return nil
	}
	untilState.Lock()
	untilState.conds, untilState.used = conds, false
	untilState.Unlock()
	err = c.run(b, args)
	untilState.Lock()
	used := untilState.used
	untilState.conds, untilState.used = nil, false
	untilState.Unlock()
	if err != nil {
		return fmt.Errorf("/%s: %w", name, err)
	}
	if !used {
		return errf("/%s: done, but without the until (it only works with commands that start a task: chop, mine, beds, farm…)",
			"/%s: hecho, pero sin el until (solo vale para órdenes que empiezan una tarea: chop, mine, beds, farm…)", name)
	}
	return nil
}

// splitConds separa "until …" del final de los argumentos (las condiciones de
// parada de cualquier tarea). Las órdenes que se comen el resto de la línea
// (say, cmd, queue, ai) lo dejan tal cual: es su texto (la cola lo guarda y
// lo aplica al ejecutar la orden).
func splitConds(c command, args []string) ([]string, []automation.Cond, error) {
	if hasRest(c) { // texto libre (say, cmd, queue, ai): el until es parte del texto
		return args, nil, nil
	}
	args, until := automation.SplitUntil(args)
	if until == nil {
		return args, nil, nil
	}
	conds, err := automation.ParseUntil(until)
	if err != nil {
		return nil, nil, fmt.Errorf("until: %w", err)
	}
	return args, conds, nil
}

// hasRest: algún argumento de la orden (o de un subcomando) se come el resto de la línea.
func hasRest(c command) bool {
	for _, a := range c.args {
		if a.rest {
			return true
		}
	}
	for _, s := range c.subs {
		for _, a := range s.args {
			if a.rest {
				return true
			}
		}
	}
	return false
}

// console lee órdenes línea a línea: la consola simple, para cuando la
// salida no es una terminal (si lo es, se usa la interfaz de tui.go).
func console(ctx context.Context) {
	log.Print(`consola lista: escribe "/help" para ver las órdenes`)
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		if ctx.Err() != nil {
			return
		}
		execLine(in.Text())
	}
}

// execLine: una línea de la consola, para el bot de la conexión de ahora.
func execLine(line string) {
	if b := current.Load(); b != nil {
		b.exec(line)
	} else {
		log.Print("aún no estoy conectado")
	}
}

// appQuit sale del programa (lo pone main).
var appQuit = func() {}

func cmdHelp(*Bot, []string) error {
	var lines []string
	for name, c := range commands {
		lines = append(lines, fmt.Sprintf("  %-26s %s", c.usage(name), c.desc))
	}
	sort.Strings(lines)
	lines = append(lines, "  /<other command>           "+tr("is sent to the server (e.g. /spawn)", "se manda al servidor (ej: /spawn)"),
		"  text without /             "+tr("is written in the chat", "se escribe en el chat"),
		"",
		"  "+tr("Any task (chop, mine, beds, farm) can end on its own with:", "Cualquier tarea (chop, mine, beds, farm) puede acabar sola con:"),
		"    "+automation.UntilUsage,
		"    "+tr("groups: ", "grupos: ")+strings.Join(automation.ItemGroups(), ", ")+tr("; or an item (oak_log) or a pattern (*_log)", "; o un objeto (oak_log) o un patrón (*_log)"),
		"    "+tr("e.g. /chop abeto until got logs 64 or time 20m", "ej: /chop abeto until got logs 64 or time 20m"))
	log.Print(tr("commands:", "órdenes:") + "\n" + strings.Join(lines, "\n"))
	return nil
}

// findPlayer busca a un jugador por nombre entre los que el bot ve.
func (b *Bot) findPlayer(name string) (entities.Entity, error) {
	if strings.EqualFold(name, botName) {
		return entities.Entity{}, errf("that is me", "ese soy yo")
	}
	id, ok := b.Players.ByName(name)
	if !ok {
		return entities.Entity{}, errf("%s is not online (names are case-sensitive)", "%s no está conectado (el nombre distingue mayúsculas)", name)
	}
	e, ok := b.Entities.ByUUID(id)
	if !ok {
		return entities.Entity{}, errf("%s is online but I cannot see them (far away or in another world)", "%s está conectado pero no lo veo (lejos o en otro mundo)", name)
	}
	return e, nil
}

func cmdFight(b *Bot, args []string) error {
	if len(args) >= 1 && strings.EqualFold(args[0], "in") {
		return cmdFightIn(b, args[1:])
	}
	if len(args) != 1 {
		return errf("usage: /fight <player>  or  /fight in <x> <z> [<x2> <z2>]", "uso: /fight <player>  o  /fight in <x> <z> [<x2> <z2>]")
	}
	e, err := b.findPlayer(args[0])
	if err != nil {
		return err
	}
	b.Combat.Engage(e.ID, args[0]) // un goto se corta solo; un follow sigue tras la pelea
	return nil
}

// moveTask es la orden de movimiento en curso (goto o follow; una a la vez).
var moveTask struct {
	sync.Mutex
	cancel context.CancelFunc
}

// startMove cancela la orden de movimiento anterior y empieza una nueva.
func (b *Bot) startMove(ctx context.Context) (context.Context, context.CancelFunc) {
	b.cancelMove()
	if b.Tasks != nil {
		b.Tasks.Stop() // una orden de movimiento para la tarea en curso (talar…)
	}
	ctx, cancel := context.WithCancel(ctx)
	moveTask.Lock()
	moveTask.cancel = cancel
	moveTask.Unlock()
	return ctx, cancel
}

func (b *Bot) cancelMove() {
	setMoveKind("")
	moveTask.Lock()
	defer moveTask.Unlock()
	if moveTask.cancel != nil {
		moveTask.cancel()
		moveTask.cancel = nil
	}
}

func cmdGoto(b *Bot, args []string) error {
	if len(args) == 2 {
		return cmdGotoCoords(b, args)
	}
	if len(args) != 1 {
		return errf("usage: /goto <player>  or  /goto <x> <z>", "uso: /goto <player>  o  /goto <x> <z>")
	}
	name := args[0]
	if strings.EqualFold(name, botName) {
		return errf("that is me", "ese soy yo")
	}
	uid, ok := b.Players.ByName(name)
	if !ok {
		return errf("%s is not online (names are case-sensitive)", "%s no está conectado (el nombre distingue mayúsculas)", name)
	}
	ctx, cancel := b.startMove(b.ctx)
	gen := setMoveKind("goto")
	go func() {
		defer cancel()
		defer clearMove(gen)
		b.gotoPlayer(ctx, name, uid)
	}()
	return nil
}

func (b *Bot) distanceTo(e entities.Entity) float64 {
	p := b.Player.Position()
	return math.Sqrt(e.DistanceSqr(p.X, p.Y, p.Z))
}

func cmdStop(b *Bot, _ []string) error {
	if b.Queue != nil {
		b.Queue.Stop()
	}
	b.cancelMove()
	b.Tasks.Stop()
	b.Combat.Reset()
	b.Walker.Stop()
	log.Print("parado")
	return nil
}

func cmdSay(b *Bot, args []string) error {
	if len(args) == 0 {
		return errf("usage: /say <text>", "uso: /say <text>")
	}
	return b.Chat.SendMessage(strings.Join(args, " "))
}

func cmdStatus(b *Bot, _ []string) error {
	st, p := b.Player.Status(), b.Player.Position()
	log.Printf("vida %.1f, comida %d, bloques de construir %d, en %.1f %.1f %.1f, %s", st.Health, st.Food, b.Walker.BuildBlocks(), p.X, p.Y, p.Z, b.Combat.Status())
	if t := b.Tasks.Status(); t != "" {
		log.Printf("tarea: %s", t)
	}
	if f := b.Flight.Status(); f != "" {
		log.Printf("élitros: %s", f)
	}
	return nil
}

func cmdQuit(b *Bot, _ []string) error {
	b.quit()
	return nil
}

func cmdFollow(b *Bot, args []string) error {
	if len(args) != 1 {
		return errf("usage: /follow <player>", "uso: /follow <player>")
	}
	name := args[0]
	if strings.EqualFold(name, botName) {
		return errf("that is me", "ese soy yo")
	}
	uid, ok := b.Players.ByName(name)
	if !ok {
		return errf("%s is not online (names are case-sensitive)", "%s no está conectado (el nombre distingue mayúsculas)", name)
	}
	ctx, _ := b.startMove(b.ctx)
	setMoveKind("follow")
	log.Printf("siguiendo a %s (\"stop\" para parar)", name)
	go b.follow(ctx, name, uid)
	return nil
}

// follow sigue al jugador uid hasta que ctx termine. Durante una pelea se
// aparta y luego sigue; si lo pierde de vista va a donde lo vio por última
// vez y espera a que vuelva a aparecer (aunque se desconecte y reconecte).
func (b *Bot) follow(ctx context.Context, name string, uid uuid.UUID) {
	defer b.Walker.Stop()
	var (
		following int32 = -1 // entidad que el walker está siguiendo
		last      world.BlockPos
		known     bool // last es válido y aún no llegó allí
		walking   bool // este follow mandó al walker a last
		lost      bool
	)
	for {
		if err := b.Ctl.WaitTicks(ctx, 5); err != nil {
			return
		}
		if b.Combat.Fighting() || detour.Load() {
			following, walking = -1, false // la pelea (o recoger algo) manda; luego se vuelve a seguir
			continue
		}
		e, ok := b.Entities.ByUUID(uid)
		if ok {
			if lost {
				log.Printf("follow: vuelvo a ver a %s", name)
				lost = false
			}
			last = world.BlockPos{X: int(math.Floor(e.X)), Y: int(math.Floor(e.Y + 1e-6)), Z: int(math.Floor(e.Z))}
			known, walking = true, false
			// lejos o muy por encima (se fue volando): volar tras él
			if b.flyToPlayer(ctx, e.ID) {
				following = -1
				continue
			}
			if following != e.ID || b.Walker.Status() != "following" {
				id := e.ID
				b.Walker.SetSprint(true)
				b.Walker.Follow(func() (float64, float64, float64, bool) {
					e, ok := b.Entities.Get(id)
					return e.X, e.Y, e.Z, ok
				}, 2)
				following = id
			}
			continue
		}
		following = -1
		if !lost {
			lost = true
			if known {
				log.Printf("follow: perdí de vista a %s, voy a donde lo vi (%v)", name, last)
			} else {
				log.Printf("follow: no veo a %s, espero a que aparezca", name)
			}
		}
		if !known {
			continue
		}
		switch st := b.Walker.Status(); {
		case walking && st == "walking":
		case walking && (st == "arrived" || strings.HasPrefix(st, "failed")):
			known, walking = false, false // allí ya no está: esperar a verlo
			log.Printf("follow: %s no está donde lo vi; espero a que aparezca", name)
		default:
			// lejos (se fue volando): volar hasta donde lo vio
			if b.flyNear(ctx, float64(last.X)+0.5, float64(last.Y), float64(last.Z)+0.5) {
				continue
			}
			b.Walker.SetSprint(true)
			b.Walker.Goto(last)
			walking = true
		}
	}
}

// keys escribe las teclas pulsadas (para los logs).
func keys(in control.Input) string {
	var k []string
	for _, x := range []struct {
		on   bool
		name string
	}{{in.Forward, "W"}, {in.Backward, "S"}, {in.Left, "A"}, {in.Right, "D"}, {in.Jump, "salto"}, {in.Sprint, "correr"}, {in.Shift, "shift"}} {
		if x.on {
			k = append(k, x.name)
		}
	}
	if len(k) == 0 {
		return "ninguna"
	}
	return strings.Join(k, "+")
}

func cmdDiag(b *Bot, _ []string) error {
	if b.Combat.Fighting() {
		return errf("I am fighting", "estoy peleando")
	}
	b.cancelMove()
	b.Walker.Stop()
	go b.diag()
	return nil
}

// diag muestra los bloques que el bot cree tener alrededor y prueba, mirando
// hacia donde mira, saltar, andar, andar saltando y correr saltando, contando
// las correcciones del servidor de cada prueba.
func (b *Bot) diag() {
	ctx := b.ctx
	p := b.Player.Position()
	yaw := float64(p.Yaw) * math.Pi / 180
	fx, fz := -math.Sin(yaw), math.Cos(yaw)
	name := func(x, y, z int) string {
		s, ok := b.World.BlockAt(world.BlockPos{X: x, Y: y, Z: z})
		if !ok {
			return "(sin cargar)"
		}
		return strings.TrimPrefix(world.StateString(s), "minecraft:")
	}
	fxb, fyb, fzb := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	ax, az := int(math.Floor(p.X+fx)), int(math.Floor(p.Z+fz))
	log.Printf("diag: en %.2f %.2f %.2f mirando %.0f°, suelo=%v", p.X, p.Y, p.Z, p.Yaw, b.Ctl.State().OnGround)
	for dy := 2; dy >= -1; dy-- {
		log.Printf("diag:   y%+d  aquí: %-28s  delante: %s", dy, name(fxb, fyb+dy, fzb), name(ax, fyb+dy, az))
	}
	tests := []struct {
		name  string
		in    control.Input
		ticks int
	}{
		{"saltar en el sitio", control.Input{Jump: true}, 1},
		{"andar", control.Input{Forward: true}, 10},
		{"andar saltando (sin correr)", control.Input{Forward: true, Jump: true}, 14},
		{"correr saltando", control.Input{Forward: true, Sprint: true, Jump: true}, 14},
	}
	// primero: andar por llano en una dirección despejada (sin tocar ningún bloque)
	for _, deg := range []float64{0, 90, 180, 270, 45, 135, 225, 315} {
		a := deg * math.Pi / 180
		dx, dz := -math.Sin(a), math.Cos(a)
		if !b.Walker.Safe(p, dx, dz) || b.Walker.NeedJump(p, dx, dz, 0.3) || !b.Walker.Direct(p, p.X+dx*4, p.Z+dz*4) {
			continue
		}
		b.Player.UpdatePosition(func(q basic.Position) basic.Position { q.Yaw, q.Pitch = float32(deg), 0; return q })
		_ = b.Ctl.WaitTicks(ctx, 2)
		before, start := b.corrections.Load(), b.Player.Position()
		b.Ctl.SetInput(control.Input{Forward: true})
		_ = b.Ctl.WaitTicks(ctx, 8)
		b.Ctl.SetInput(control.Input{})
		_ = b.Ctl.WaitTicks(ctx, 20)
		end := b.Player.Position()
		log.Printf("diag: andar por llano (%3.0f°)          se movió %.2f, correcciones del servidor: %d", deg, math.Hypot(end.X-start.X, end.Z-start.Z), b.corrections.Load()-before)
		// y volver mirando como estaba
		b.Ctl.SetInput(control.Input{Backward: true})
		_ = b.Ctl.WaitTicks(ctx, 8)
		b.Ctl.SetInput(control.Input{})
		_ = b.Ctl.WaitTicks(ctx, 10)
		b.Player.UpdatePosition(func(q basic.Position) basic.Position { q.Yaw, q.Pitch = p.Yaw, p.Pitch; return q })
		_ = b.Ctl.WaitTicks(ctx, 2)
		break
	}
	// primero: andar por llano en una dirección despejada (sin tocar ningún bloque)
	for _, deg := range []float64{0, 90, 180, 270, 45, 135, 225, 315} {
		a := deg * math.Pi / 180
		dx, dz := -math.Sin(a), math.Cos(a)
		if !b.Walker.Safe(p, dx, dz) || b.Walker.NeedJump(p, dx, dz, 0.3) || !b.Walker.Direct(p, p.X+dx*4, p.Z+dz*4) {
			continue
		}
		b.Player.UpdatePosition(func(q basic.Position) basic.Position { q.Yaw, q.Pitch = float32(deg), 0; return q })
		_ = b.Ctl.WaitTicks(ctx, 2)
		for _, run := range []struct {
			name string
			in   control.Input
		}{{"andar por llano", control.Input{Forward: true}}, {"correr por llano", control.Input{Forward: true, Sprint: true}}} {
			before, start := b.corrections.Load(), b.Player.Position()
			b.Ctl.SetInput(run.in)
			_ = b.Ctl.WaitTicks(ctx, 8)
			b.Ctl.SetInput(control.Input{})
			_ = b.Ctl.WaitTicks(ctx, 20)
			end := b.Player.Position()
			log.Printf("diag: %-16s (%3.0f°)    se movió %.2f, correcciones del servidor: %d", run.name, deg, math.Hypot(end.X-start.X, end.Z-start.Z), b.corrections.Load()-before)
			// volver andando hacia atrás
			b.Ctl.SetInput(control.Input{Backward: true})
			_ = b.Ctl.WaitTicks(ctx, 10)
			b.Ctl.SetInput(control.Input{})
			_ = b.Ctl.WaitTicks(ctx, 10)
		}
		b.Player.UpdatePosition(func(q basic.Position) basic.Position { q.Yaw, q.Pitch = p.Yaw, p.Pitch; return q })
		_ = b.Ctl.WaitTicks(ctx, 2)
		break
	}
	for _, t := range tests {
		before, start := b.corrections.Load(), b.Player.Position()
		maxY := start.Y
		b.Ctl.SetInput(t.in)
		for i := 0; i < t.ticks+20; i++ {
			if i == t.ticks {
				b.Ctl.SetInput(control.Input{})
			}
			if b.Ctl.WaitTicks(ctx, 1) != nil {
				return
			}
			maxY = math.Max(maxY, b.Player.Position().Y)
		}
		end := b.Player.Position()
		log.Printf("diag: %-28s se movió %.2f, subió hasta +%.2f, correcciones del servidor: %d",
			t.name, math.Hypot(end.X-start.X, end.Z-start.Z), maxY-start.Y, b.corrections.Load()-before)
	}
	log.Print("diag: fin")
}

// flyMinDist: a menos de esto, /goto anda (despegar y aterrizar no compensa).
const flyMinDist = 64

// elytraAllowed: /goto puede volar con élitros (orden "elytra on|off").
var elytraAllowed = func() *atomic.Bool { var b atomic.Bool; b.Store(true); return &b }()

// cmdElytra: /elytra on|off — si /goto vuela con élitros cuando puede;
// /elytra wear — ponérselos ya.
func cmdElytra(b *Bot, args []string) error {
	ready := func() string {
		if _, why := b.Flight.Ready(flyMinDist); why != "" {
			return why
		}
		return "listo para volar"
	}
	if len(args) != 1 || (args[0] != "on" && args[0] != "off" && args[0] != "wear") {
		return errf("usage: /elytra on|off|wear (now: %v; %s)", "uso: /elytra on|off|wear (ahora: %v; %s)", map[bool]string{true: "on", false: "off"}[elytraAllowed.Load()], ready())
	}
	if args[0] == "wear" {
		ctx, cancel := context.WithTimeout(b.ctx, 10*time.Second)
		defer cancel()
		if err := b.Flight.Wear(ctx); err != nil {
			return errf("elytra: %v", "élitros: %v", err)
		}
		return nil
	}
	elytraAllowed.Store(args[0] == "on")
	log.Printf("élitros: %s (/goto vuela a más de %d bloques); %s", args[0], flyMinDist, ready())
	return nil
}

func cmdSprint(b *Bot, args []string) error {
	if len(args) != 1 || (args[0] != "on" && args[0] != "off") {
		return errf("usage: /sprint on|off (now: %v)", "uso: /sprint on|off (ahora: %v)", map[bool]string{true: "on", false: "off"}[b.Walker.SprintAllowed()])
	}
	b.Walker.AllowSprint(args[0] == "on")
	log.Printf("correr: %s", args[0])
	return nil
}

// zoneRadius es el radio de "/fight in <x> <z>" (con un solo punto).
const zoneRadius = 8

// cmdFightIn: /fight in <x> <z> (radio 8) o /fight in <x1> <z1> <x2> <z2>
// (rectángulo). "~" es la posición del bot (como en Minecraft), "~5" relativo.
func cmdFightIn(b *Bot, args []string) error {
	if len(args) != 2 && len(args) != 4 {
		return errf("usage: /fight in <x> <z> (radius %d)  or  /fight in <x1> <z1> <x2> <z2>", "uso: /fight in <x> <z> (radio %d)  o  /fight in <x1> <z1> <x2> <z2>", zoneRadius)
	}
	p := b.Player.Position()
	var v [4]float64
	for i, a := range args {
		base := p.X
		if i%2 == 1 {
			base = p.Z
		}
		n, err := coord(a, base)
		if err != nil {
			return err
		}
		v[i] = n
	}
	var z combat.Zone
	if len(args) == 2 {
		z = combat.ZoneAround(v[0], v[1], zoneRadius)
	} else {
		z = combat.ZoneBetween(v[0], v[1], v[2], v[3])
	}
	b.cancelMove()
	b.Combat.SetZone(&z)
	return nil
}

// coord lee una coordenada: un número, "~" (la del bot) o "~n" (relativa).
func coord(s string, base float64) (float64, error) {
	if strings.HasPrefix(s, "~") {
		if s == "~" {
			return base, nil
		}
		var d float64
		if _, err := fmt.Sscan(s[1:], &d); err != nil {
			return 0, errf("invalid coordinate %q", "coordenada %q no válida", s)
		}
		return base + d, nil
	}
	var n float64
	if _, err := fmt.Sscan(s, &n); err != nil {
		return 0, errf("invalid coordinate %q (a number or ~)", "coordenada %q no válida (un número o ~)", s)
	}
	return n, nil
}

// cmdServer manda el resto de la línea al servidor como un comando del
// jugador (la respuesta sale en el log como [sistema]). Sirve también para los
// comandos del servidor que se llaman igual que una orden del bot (/help, /stop…).
func cmdServer(b *Bot, args []string) error {
	line := strings.TrimPrefix(strings.Join(args, " "), "/")
	if line == "" {
		return errf("usage: /cmd <command> (e.g. /cmd spawn)", "uso: /cmd <command> (ej: /cmd spawn)")
	}
	log.Printf("→ servidor: /%s", line)
	return b.Chat.SendCommand(line)
}

// cmdGuard: el bot sigue al jugador y, si alguien le pega, lo defiende.
func cmdGuard(b *Bot, args []string) error {
	if len(args) != 1 && len(args) != 2 {
		return errf("usage: /guard <player> [minutes]", "uso: /guard <player> [minutes]")
	}
	name := args[0]
	if strings.EqualFold(name, botName) {
		return errf("that is me", "ese soy yo")
	}
	uid, ok := b.Players.ByName(name)
	if !ok {
		return errf("%s is not online (names are case-sensitive)", "%s no está conectado (el nombre distingue mayúsculas)", name)
	}
	var minutes float64
	if len(args) == 2 {
		m, err := strconv.ParseFloat(args[1], 64)
		if err != nil { // también una duración: 10m, 90s, 1h (la IA lo escribe así)
			if d, derr := time.ParseDuration(strings.ToLower(args[1])); derr == nil {
				m, err = d.Minutes(), nil
			}
		}
		if err != nil || m <= 0 {
			return errf("minutes: a number or a duration (/guard <player> [10 | 10m | 1h])", "minutes: un número o una duración (/guard <player> [10 | 10m | 1h])")
		}
		minutes = m
	}
	b.Combat.SetGuard(uid, name)
	ctx, cancel := b.startMove(b.ctx)
	gen := setMoveKind("guard")
	go b.follow(ctx, name, uid) // a su lado; durante una pelea se aparta y luego vuelve
	if minutes > 0 {
		log.Printf("protejo a %s durante %.0f min", name, minutes)
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(minutes * float64(time.Minute))):
			}
			if currentMove() == "guard" && b.Combat.Guarding() == name {
				log.Printf("guard: pasaron los %.0f min: dejo de proteger a %s", minutes, name)
				b.Combat.SetGuard(uuid.Nil, "")
				clearMove(gen)
				cancel()
				b.Walker.Stop()
			}
		}()
	}
	return nil
}

// cmdGotoCoords: /goto <x> <z> ("~" es la posición del bot, "~n" relativa).
func cmdGotoCoords(b *Bot, args []string) error {
	p := b.Player.Position()
	x, err := coord(args[0], p.X)
	if err != nil {
		return err
	}
	z, err := coord(args[1], p.Z)
	if err != nil {
		return err
	}
	ctx, cancel := b.startMove(b.ctx)
	gen := setMoveKind("goto")
	log.Printf("yendo a %.0f %.0f (a %.0f bloques)", x, z, math.Hypot(x-p.X, z-p.Z))
	go func() {
		defer cancel()
		defer clearMove(gen)
		if b.gotoCoords(ctx, x, z, nil) {
			log.Printf("goto: llegué a %.0f %.0f", x, z)
		}
	}()
	return nil
}

// gotoCoords camina hasta (x, z). El bot solo conoce los bloques de los chunks
// cargados, así que va por tramos de hasta 40 bloques hacia el destino: en
// cada uno busca dónde pisar y deja el camino al pathfinding; si un tramo no
// se puede, prueba uno más corto o desviado.
//
// Devuelve si llegó. stop (puede ser nil) se mira a cada momento: si dice que
// sí, para y devuelve false (p.ej. ya se ve al jugador al que se iba).
func (b *Bot) gotoCoords(ctx context.Context, x, z float64, stop func() bool) bool {
	return b.gotoCoordsY(ctx, x, math.NaN(), z, stop)
}

// gotoCoordsY es gotoCoords sabiendo (si y no es NaN) a qué altura está la
// meta: volando, se aterriza lo más cerca posible de esa altura.
func (b *Bot) gotoCoordsY(ctx context.Context, x, y, z float64, stop func() bool) bool {
	defer b.Walker.Stop()
	const done = 1.5
	fails := 0
	// lejos, con élitros y cohetes: volando (lo que quede, andando)
	if b.flyNear(ctx, x, y, z) && ctx.Err() != nil {
		return false
	}
	for {
		p := b.Player.Position()
		d := math.Hypot(x-p.X, z-p.Z)
		if d <= done {
			return true
		}
		if stop != nil && stop() {
			return false
		}
		goal, ok := b.segment(p, x, z, d, fails)
		if !ok {
			log.Printf("goto: no encuentro por dónde seguir hacia %.0f %.0f (a %.0f bloques)", x, z, d)
			return false
		}
		b.Walker.SetSprint(true)
		b.Walker.Goto(goal)
		for {
			if err := b.Ctl.WaitTicks(ctx, 5); err != nil {
				return false // cancelado: otra orden tomó el control
			}
			if b.Combat.Fighting() { // sobrevivir primero; al terminar, se sigue
				if !b.waitFight(ctx) {
					return false
				}
				b.Walker.SetSprint(true)
				b.Walker.Goto(goal) // la pelea usó el walker
				continue
			}
			if stop != nil && stop() {
				return false
			}
			if q := b.Player.Position(); math.Hypot(x-q.X, z-q.Z) <= done {
				break
			}
			if st := b.Walker.Status(); st != "walking" {
				if strings.HasPrefix(st, "failed") {
					if fails++; fails > 6 {
						log.Printf("goto: me rindo a %.0f bloques de %.0f %.0f (%s)", d, x, z, st)
						return false
					}
				} else {
					fails = 0
				}
				break
			}
		}
	}
}

// segment elige el siguiente tramo hacia (x, z): un sitio donde pisar a lo
// sumo 40 bloques más allá; tras fallos, más corto y desviado a los lados.
func (b *Bot) segment(p basic.Position, x, z, d float64, fails int) (world.BlockPos, bool) {
	dx, dz := (x-p.X)/d, (z-p.Z)/d
	for _, step := range []float64{40, 24, 12, 6, 3} {
		step = math.Min(step, d) / float64(1+fails/3)
		for _, off := range []float64{0, 0.35, -0.35, 0.7, -0.7} {
			if fails == 0 && off != 0 && step == math.Min(40, d) {
				continue // primero recto
			}
			c, s := math.Cos(off), math.Sin(off)
			tx := p.X + (dx*c-dz*s)*step
			tz := p.Z + (dx*s+dz*c)*step
			if g, ok := b.Walker.Ground(int(math.Floor(tx)), int(math.Floor(tz)), int(math.Floor(p.Y))); ok {
				return g, true
			}
		}
	}
	return world.BlockPos{}, false
}

// cmdMine: /mine <minerales> [<cantidad> | all] | chest [...] | list
func cmdMine(b *Bot, args []string) error {
	if len(args) == 0 {
		return errf("which ore? "+usages["mine"], "¿qué mineral? "+usages["mine"])
	}
	switch strings.ToLower(args[0]) {
	case "chest", "cofre":
		return cmdMineChest(b, args[1:])
	case "list", "lista":
		for _, l := range automation.OreHelp() {
			log.Print(l)
		}
		return nil
	}
	// "diamond, iron 50": los minerales pueden venir separados por coma y espacio
	words, target := args, 0
	if n := len(args); n > 1 {
		last := strings.ToLower(args[n-1])
		if v, err := strconv.Atoi(last); err == nil {
			if v <= 0 {
				return errf("amount: a positive number or all", "cantidad: un número mayor que 0 o all")
			}
			words, target = args[:n-1], v
		} else if last == "all" || last == "todo" {
			words = args[:n-1]
		}
	}
	for _, w := range words { // un subcomando fuera de sitio: decir dónde va
		switch lw := strings.ToLower(strings.Trim(w, ",")); lw {
		case "chest", "cofre", "list", "lista":
			return errf("\"%s\" goes right after /mine, on its own: /mine %s (the chest is set once, then /mine <ore> uses it)",
				"\"%s\" va justo después de /mine, solo: /mine %s (el cofre se marca una vez y luego /mine <mineral> lo usa)", lw, lw)
		case "all", "todo":
			return errf("\"all\" goes at the end: /mine <ore> all", "\"all\" va al final: /mine <mineral> all")
		}
	}
	os, err := automation.ParseOres(strings.Join(words, ","))
	if err != nil {
		return err
	}
	keys := make([]string, len(os))
	for i, o := range os {
		keys[i] = o.Key
	}
	what := fmt.Sprint(target)
	if target == 0 {
		what = "hasta /stop"
	}
	where := "sin cofre de minar: lo minado al almacén y los picos me los hago (/mine chest para marcar uno)"
	if l := b.Tasks.Store.MineChestList(); len(l) > 0 {
		var cs []string
		for d, c := range l {
			cs = append(cs, fmt.Sprintf("%v en %s", c, dimShort(d)))
		}
		sort.Strings(cs)
		where = "con el cofre de minar " + strings.Join(cs, " y ")
	}
	log.Printf("minar %s: %s, %s (/stop para parar)", strings.Join(keys, ","), what, where)
	b.startWork("minar", automation.MineOres(os, target))
	return nil
}

// dimShort: "minecraft:the_nether" → "el Nether".
func dimShort(d string) string {
	switch d {
	case "minecraft:the_nether":
		return "el Nether"
	case "minecraft:overworld":
		return "el mundo normal"
	}
	return strings.TrimPrefix(d, "minecraft:")
}

// cmdMineChest: /mine chest [<x> <y> <z> | off | list] — el cofre de minar de
// la dimensión donde está el bot (sin coordenadas: el más cercano).
func cmdMineChest(b *Bot, args []string) error {
	st := b.Tasks.Store
	dim := string(b.Player.Spawn.Dimension)
	p := b.Player.Position()
	here := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y)), Z: int(math.Floor(p.Z))}
	var c world.BlockPos
	switch {
	case len(args) == 1 && strings.EqualFold(args[0], "list"):
		l := st.MineChestList()
		if len(l) == 0 {
			log.Print("cofres de minar: ninguno (/mine chest junto a uno lo marca)")
			return nil
		}
		for d, c := range l {
			log.Printf("cofre de minar en %s: %v", dimShort(d), c)
		}
		return nil
	case len(args) == 1 && strings.EqualFold(args[0], "off"):
		st.SetMineChest(dim, nil)
		log.Printf("cofre de minar de %s: olvidado", dimShort(dim))
		return nil
	case len(args) == 0:
		var ok bool
		if c, ok = b.Tasks.NearestChest(here, 5); !ok {
			return errf("no chest within 5 blocks: stand by it or give /mine chest <x> <y> <z>",
				"no hay un cofre a menos de 5 bloques: ponme al lado o dime /mine chest <x> <y> <z>")
		}
	case len(args) == 3:
		var v [3]int
		for i, a := range args {
			n, err := coord(a, []float64{p.X, p.Y, p.Z}[i])
			if err != nil {
				return err
			}
			v[i] = int(math.Floor(n))
		}
		c = world.BlockPos{X: v[0], Y: v[1], Z: v[2]}
		if s, ok := b.World.BlockAt(c); ok && !strings.Contains(world.StateString(s), "chest") && !strings.Contains(world.StateString(s), "barrel") {
			// un bloque desviado (con negativas, -739.6 es el bloque -740): el cofre más cercano
			if near, found := b.Tasks.NearestChest(c, 3); found {
				log.Printf("cofre de minar: en %v no hay cofre, uso el de %v", c, near)
				c = near
			} else {
				log.Printf("cofre de minar: ojo, en %v no veo ningún cofre (ni a 3 bloques); lo apunto igual", c)
			}
		}
	default:
		return errf("usage: /mine chest [<x> <y> <z> | off | list]", "uso: /mine chest [<x> <y> <z> | off | list]")
	}
	st.SetMineChest(dim, &c)
	log.Printf("cofre de minar de %s: %v. Deja ahí picos, comida y armadura (y oro para el Nether); lo minado lo dejo ahí. Hay uno por dimensión: si para un mineral no hay en la suya, voy al de la otra",
		dimShort(dim), c)
	return nil
}

// cmdFarm: /farm — monta y cuida la granja de trigo y ovejas hasta /stop.
func cmdFarm(b *Bot, args []string) error {
	log.Printf("granja: trigo y ovejas junto a la base (/stop para parar)")
	b.startWork("farm", automation.Farm())
	return nil
}

// cmdBeds: /beds [n] — hace n camas (8 si no se dice).
func cmdBeds(b *Bot, args []string) error {
	n := 8
	if len(args) > 0 {
		v, err := strconv.Atoi(args[0])
		if err != nil || v <= 0 {
			return errf("amount: a number of beds", "amount: un número de camas")
		}
		n = v
	}
	log.Printf("camas: %d (/stop para parar)", n)
	b.startWork("beds", automation.Beds(n))
	return nil
}

func cmdChop(b *Bot, args []string) error {
	kind, target := "", 0
	for _, a := range args {
		if n, err := strconv.Atoi(a); err == nil {
			target = n
			continue
		}
		k, err := automation.ParseWood(a)
		if err != nil {
			return err
		}
		kind = k
	}
	what := "cualquier madera"
	if kind != "" {
		what = kind
	}
	if target > 0 {
		log.Printf("talar: %d troncos de %s (/stop para parar)", target, what)
	} else {
		log.Printf("talar: %s, sin parar (/stop para parar)", what)
	}
	b.startWork("talar", automation.Woodcut(kind, target))
	return nil
}

// cmdBase: /base here | info — el sitio del almacén (los cofres).
func cmdBase(b *Bot, args []string) error {
	if len(args) == 0 || args[0] == "info" {
		log.Printf("almacén: %s", b.Tasks.Store.Describe())
		return nil
	}
	if args[0] != "here" {
		return errf("usage: /base here | info", "uso: /base here | info")
	}
	p := b.Player.Position()
	pos := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + 1e-6)), Z: int(math.Floor(p.Z))}
	b.Tasks.Store.SetBase(pos)
	log.Printf("almacén: base en %v (los cofres irán en fila desde aquí)", pos)
	return nil
}

// cmdDebug: /debug — las pilas de todas las goroutines al log de la sesión.
func cmdDebug(b *Bot, _ []string) error {
	buf := make([]byte, 4<<20)
	buf = buf[:runtime.Stack(buf, true)]
	session.Trace("DEBUG pilas:\n%s", buf)
	log.Print(tr("debug: goroutine stacks written to the session log", "debug: pilas escritas en el log de la sesión"))
	return nil
}

// cmdAround: /around — los bloques alrededor del bot, de 2 abajo a 3 arriba,
// 7x7 (el bot en el centro, "@"), con la leyenda de abreviaturas.
func cmdAround(b *Bot, _ []string) error {
	p := b.Player.Position()
	fx, fy, fz := int(math.Floor(p.X)), int(math.Floor(p.Y+1e-6)), int(math.Floor(p.Z))
	short := map[string]string{}
	next := 'a'
	var sb strings.Builder
	fmt.Fprintf(&sb, "alrededor de %d %d %d (x hacia la derecha, z hacia abajo):\n", fx, fy, fz)
	for y := fy + 3; y >= fy-2; y-- {
		fmt.Fprintf(&sb, "y=%d\n", y)
		for z := fz - 3; z <= fz+3; z++ {
			for x := fx - 3; x <= fx+3; x++ {
				st, ok := b.World.BlockAt(world.BlockPos{X: x, Y: y, Z: z})
				n := "?"
				if ok {
					n = strings.TrimPrefix(world.StateString(st), "minecraft:")
				}
				c := "."
				switch {
				case x == fx && z == fz && (y == fy || y == fy+1):
					c = "@"
				case n == "air" || n == "cave_air":
				default:
					if _, seen := short[n]; !seen {
						short[n] = string(next)
						next++
					}
					c = short[n]
				}
				sb.WriteString(c)
			}
			sb.WriteString("\n")
		}
	}
	for n, c := range short {
		fmt.Fprintf(&sb, "  %s = %s\n", c, n)
	}
	log.Print(sb.String())
	return nil
}

// cmdFoodChest: /foodchest [add|remove [<x> <y> <z>] | list | clear | now].
func cmdFoodChest(b *Bot, args []string) error {
	st := b.Tasks.Store
	p := b.Player.Position()
	here := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y)), Z: int(math.Floor(p.Z))}
	action := "list"
	if len(args) > 0 {
		action = strings.ToLower(args[0])
		args = args[1:]
	}
	where := func() (world.BlockPos, error) {
		if len(args) == 3 {
			var v [3]int
			for i, a := range args {
				n, err := coord(a, []float64{p.X, p.Y, p.Z}[i])
				if err != nil {
					return world.BlockPos{}, err
				}
				v[i] = int(math.Floor(n))
			}
			return world.BlockPos{X: v[0], Y: v[1], Z: v[2]}, nil
		}
		if len(args) != 0 {
			return world.BlockPos{}, errf("give <x> <y> <z>, or nothing for the nearest chest", "dime <x> <y> <z>, o nada para el cofre más cercano")
		}
		c, ok := b.Tasks.NearestChest(here, 5)
		if !ok {
			return world.BlockPos{}, errf("no chest within 5 blocks: stand by it or give <x> <y> <z>",
				"no hay un cofre a menos de 5 bloques: ponme al lado o dime <x> <y> <z>")
		}
		return c, nil
	}
	switch action {
	case "add":
		c, err := where()
		if err != nil {
			return err
		}
		if st.AddFoodChest(c) {
			log.Printf("cofre de comida: %v (van %d). Sin comida, irá a él antes de cazar", c, len(st.FoodChestList()))
		} else {
			log.Printf("cofre de comida: %v ya estaba", c)
		}
	case "remove":
		c, err := where()
		if err != nil {
			return err
		}
		if st.RemoveFoodChest(c) {
			log.Printf("cofre de comida: %v olvidado", c)
		} else {
			log.Printf("cofre de comida: %v no estaba", c)
		}
	case "clear":
		st.ClearFoodChests()
		log.Print("cofres de comida: olvidados todos (sigue usando el almacén y los de junto a la base)")
	case "now":
		log.Print("comida: voy a los cofres")
		b.startWork("comida", automation.FoodRestock())
	case "list":
		l := st.FoodChestList()
		base := "sin base (/base here)"
		if st.Base != nil {
			base = fmt.Sprintf("y los cofres a menos de 16 bloques de la base (%v)", *st.Base)
		}
		if len(l) == 0 {
			log.Printf("cofres de comida: ninguno marcado (/foodchest add); uso el almacén %s", base)
		} else {
			log.Printf("cofres de comida: %v; además, el almacén %s", l, base)
		}
	default:
		return errf("usage: "+usages["foodchest"], "uso: "+usages["foodchest"])
	}
	return nil
}
