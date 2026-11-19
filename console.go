package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/world"

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

// sub es un subcomando: una palabra fija tras la orden (/excavate chunk …) con sus argumentos.
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
	// primer argumento de args (/excavate chunk … o /excavate 1 2 3 …)
	subs []sub
}

// usages son formas de uso escritas a mano, para órdenes con variantes.
var usages = map[string]string{
	"fight":     "/fight <player> | in <x> <z> [<x2> <z2>]",
	"goto":      "/goto <player> | <x> <z>",
	"drop":      "/drop [<item> [<amount>] [<player>] | <player>]",
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
	withDesc := func(as []arg, d text) []arg {
		for i := range as {
			as[i].desc = d
		}
		return as
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
		"cmd": {desc: text{"run a server command as a player (even one named like a bot command: /cmd help, /cmd time set day)", "ejecutar un comando del servidor como jugador (aunque se llame igual que una orden del bot: /cmd help, /cmd time set day)"},
			args: []arg{{name: "command", rest: true, desc: text{"the server command, without /", "el comando del servidor, sin /"}}}, run: cmdServer},
		"say": {desc: text{"write in the chat (or type without /)", "escribir en el chat (o escribe sin /)"},
			args: []arg{{name: "text", rest: true, desc: text{"what to write", "lo que escribir"}}}, run: cmdSay},
		"status": {desc: text{"health, food, position and combat", "vida, comida, posición y combate"}, run: cmdStatus},
		"sprint": {desc: text{"allow or forbid sprinting", "permitir o prohibir correr"},
			args: []arg{{name: "mode", kind: argChoice, choices: []string{"on", "off"},
				descs: map[string]text{"on": {"it may sprint", "puede correr"}, "off": {"it always walks", "siempre camina"}}}}, run: cmdSprint},
		"excavate": {
			desc: text{"dig out a whole box of blocks between two corners (/excavate 10 70 10 30 50 30), 4 blocks deep at a time: each 4-block pass through the whole box before going down to the next (the ground goes down evenly); in each pass it stands on the bottom and digs the 4 blocks high it reaches around it, then moves on, and picks up what fell when the pass is done. It uses the pickaxes it carries, never breaking one (with none left, it asks for one in chat); it uses no chests: when full it throws away the junk (dirt, stone, gravel…) and keeps what is valuable",
				"vaciar una caja de bloques entre dos esquinas (/excavate 10 70 10 30 50 30), de 4 en 4 bloques de alto: cada pasada de 4 en toda la caja antes de bajar a la siguiente (el terreno baja parejo); en cada pasada se pone en el fondo y pica los 4 de alto que alcanza alrededor, y sigue, y al acabarla recoge lo que cayó. Usa los picos que lleve, sin romper ninguno (si no le queda ninguno, lo pide por el chat); no usa cofres: cuando se llena tira la basura (tierra, piedra, grava…) y se queda lo valioso"},
			args: append(withDesc(xyz(false), text{"one corner (~ = where the bot is)", "una esquina (~ = donde está el bot)"}),
				append(withDesc([]arg{{name: "x2", kind: argCoord, corner: true}, {name: "y2", kind: argCoord}, {name: "z2", kind: argCoord}}, text{"the opposite corner", "la esquina opuesta"}),
					arg{name: "exact", kind: argChoice, opt: true, choices: []string{"exact"},
						descs: map[string]text{"exact": {"exactly those blocks (without it, a box of whole chunks written with the chunk edges is fitted to the chunks)", "justo esos bloques (sin ello, una caja de chunks enteros escrita con los bordes se ajusta a los chunks)"}}})...),
			subs: []sub{{name: "chunk", desc: text{"the whole 16×16 chunk the bot is in (or the one with block x z), from y1 to y2: the chunk edges are worked out for you",
				"el chunk entero de 16×16 donde está el bot (o el del bloque x z), de y1 a y2: los bordes del chunk los calcula él"},
				args: []arg{{name: "y1", kind: argCoord, desc: text{"from this height", "desde esta altura"}}, {name: "y2", kind: argCoord, desc: text{"to this height", "hasta esta altura"}}}}},
			run: cmdExcavate,
		},
		"inventory": {desc: text{"what the bot carries: equipment, hotbar slot by slot and the inventory; grid: like the game screen",
			"lo que lleva el bot: el equipo, la barra casilla a casilla y el inventario; grid: como la pantalla del juego"},
			args: []arg{{name: "view", kind: argChoice, opt: true, choices: []string{"grid"},
				descs: map[string]text{"grid": {"like the game screen (wide)", "como la pantalla del juego (ancha)"}}}}, run: cmdInventory},
		"drop": {desc: text{"throw on the ground everything it carries, or one item (all of it, or the amount given: /drop dirt 10); with a player, it goes to them and throws it to them (/drop Alex, /drop dirt 10 Alex)",
			"tirar al suelo todo lo que lleva, o un objeto (todo, o la cantidad que se diga: /drop dirt 10); con un jugador, va hasta él y se lo tira (/drop Alex, /drop dirt 10 Alex)"},
			args: []arg{{name: "item", kind: argPlayer, items: true}, {name: "amount", amountOf: 1, orPlayer: true}, {name: "player", kind: argPlayer}}, run: cmdDrop},
		"quit": {desc: text{"disconnect and exit", "desconectar y salir"}, run: cmdQuit},
	}
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
		log.Printf("→ servidor: %s", line)
		if err := b.Chat.SendCommand(strings.TrimPrefix(line, "/")); err != nil {
			return fmt.Errorf("comando: %w", err)
		}
		return nil
	}
	return b.runCommand(c, fields[0], fields[1:])
}

// runCommand ejecuta una orden del bot con sus argumentos (la consola, los /msg).
func (b *Bot) runCommand(c command, name string, args []string) error {
	if err := c.run(b, args); err != nil {
		return fmt.Errorf("/%s: %w", name, err)
	}
	return nil
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
	lines = append(lines, "  /<other command>           "+tr("is sent to the server (e.g. /time set day)", "se manda al servidor (ej: /time set day)"),
		"  text without /             "+tr("is written in the chat", "se escribe en el chat"),
)
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










