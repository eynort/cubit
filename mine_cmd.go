package main

import (
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/world"

	"cubit/automation"
)

// /mine <ores> <x> <y> <z> [every <n>] [total <n>]: minar minerales como un
// jugador (automation/oremine.go). La orden se registra aquí, en un archivo
// aparte, para no tocar console.go ni docs_examples.go.
func init() {
	oreArg := arg{name: "ores", kind: argList, choices: automation.OreKeys(),
		desc: text{"what to mine: one, or several separated by commas (diamond,iron); in the Nether, ancient_debris and quartz",
			"qué minar: uno, o varios separados por comas (diamond,iron); en el Nether, ancient_debris y quartz"}}
	chestDesc := text{"the chest (or a wall of chests: any one of them) where it takes pickaxes from and leaves the ores (~ = where the bot is)",
		"el cofre (o una pared de cofres: vale uno cualquiera) de donde coge picos y donde deja los minerales (~ = donde está el bot)"}
	commands["mine"] = command{
		desc: text{"mine ores like a real player (strip mining, no xray): from where it stands, it goes down a staircase to the ore's best layer, digs a 1×2 main tunnel with a branch to each side every 3 blocks, and only mines what a player would see (ore touching the air of the tunnel, and the rest of a vein once it shows). It never breaks a pickaxe: it takes them from the chest and, with none left, goes back for more (or asks in chat). Every 8 ores (every <n>) it walks back to the chest, leaves the ores and valuables (never tools or food), and returns to the mine. /stop stops it; /status says what it is doing. When it ends it prints a summary",
			"minar minerales como un jugador de verdad (strip mining, sin xray): desde donde está baja por una escalera a la mejor capa del mineral, cava un túnel principal de 1×2 con una rama a cada lado cada 3 bloques, y solo pica lo que vería un jugador (mineral que toca el aire del túnel, y el resto de una veta cuando se ve). Nunca rompe un pico: los coge del cofre y, si no le quedan, vuelve a por más (o los pide por el chat). Cada 8 minerales (every <n>) vuelve andando al cofre, deja los minerales y lo valioso (nunca herramientas ni comida) y regresa a la mina. /stop lo para; /status dice qué hace. Al acabar da un resumen"},
		args: []arg{oreArg,
			{name: "x", kind: argCoord, desc: chestDesc}, {name: "y", kind: argCoord, desc: chestDesc}, {name: "z", kind: argCoord, desc: chestDesc},
			{name: "option", kind: argChoice, opt: true, choices: []string{"every", "total"},
				descs: map[string]text{
					"every": {"leave the ores in the chest every <n> (default 8)", "dejar los minerales en el cofre cada <n> (por defecto 8)"},
					"total": {"stop after <n> ores (default: until /stop)", "acabar al juntar <n> minerales (por defecto: hasta /stop)"}}},
			{name: "n", kind: argNumber, opt: true, desc: text{"how many", "cuántos"}}},
		run: cmdMine,
	}
	usages["mine"] = "/mine <ores> <x> <y> <z> [every <n>] [total <n>]"
	commandExamples["mine"] = []example{
		{"/mine diamond 104 103 -224", text{"mine diamonds, using the chest there (leaves them every 8)", "minar diamantes, con el cofre de ahí (los deja cada 8)"}},
		{"/mine iron,coal 104 103 -224 every 16", text{"iron and coal at once, leaving them every 16", "hierro y carbón a la vez, dejándolos cada 16"}},
		{"/mine diamond ~ ~ ~ total 12", text{"the chest is where it stands; stops at 12 diamonds", "el cofre está donde está él; acaba al juntar 12 diamantes"}},
		{"/mine ancient_debris 104 103 -224", text{"in the Nether: ancient debris at y=15", "en el Nether: escombros ancestrales en y=15"}},
	}
}

// cmdMine: /mine <ores> <x> <y> <z> [every <n>] [total <n>].
func cmdMine(b *Bot, args []string) error {
	usage := errf("usage: /mine <ores> <x> <y> <z> [every <n>] [total <n>] (ores: %s)",
		"uso: /mine <ores> <x> <y> <z> [every <n>] [total <n>] (ores: %s)", strings.Join(automation.OreKeys(), ", "))
	if len(args) < 4 {
		return usage
	}
	ores, err := automation.ParseOres(args[0])
	if err != nil {
		return errf("%v", "%v", err)
	}
	p := b.Player.Position()
	base := []float64{p.X, p.Y + 1e-6, p.Z}
	var v [3]int
	for i, s := range args[1:4] {
		n, err := coord(s, base[i])
		if err != nil {
			return err
		}
		v[i] = int(math.Floor(n))
	}
	chest := world.BlockPos{X: v[0], Y: v[1], Z: v[2]}
	cfg := automation.MineConfig{Ores: ores, Chest: chest}
	rest := args[4:]
	for len(rest) > 0 {
		if len(rest) < 2 {
			return usage
		}
		n, err := strconv.Atoi(rest[1])
		if err != nil || n < 1 {
			return errf("%q is not a number of 1 or more", "%q no es un número de 1 o más", rest[1])
		}
		switch strings.ToLower(rest[0]) {
		case "every", "cada":
			cfg.Every = n
		case "total":
			cfg.Total = n
		default:
			return usage
		}
		rest = rest[2:]
	}
	// el cofre: si se ve desde aquí, que sea de verdad uno (o que haya uno pegado, un bloque de error);
	// si no está cargado, se va y se mira allí
	if s, ok := b.World.BlockAt(chest); ok && !automation.IsContainer(world.StateString(s)) {
		found := false
		for _, d := range [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, -1, 0}, {0, 1, 0}} {
			n := world.BlockPos{X: chest.X + d[0], Y: chest.Y + d[1], Z: chest.Z + d[2]}
			if s2, ok := b.World.BlockAt(n); ok && automation.IsContainer(world.StateString(s2)) {
				log.Printf(tr("there is no chest at %v, but there is one right next to it: %v (I use that one)",
					"en %v no hay cofre, pero hay uno pegado: %v (uso ese)"), chest, n)
				cfg.Chest, found = n, true
				break
			}
		}
		if !found {
			return errf("there is no chest at %d %d %d (there is %s)", "en %d %d %d no hay un cofre (hay %s)", chest.X, chest.Y, chest.Z,
				strings.TrimPrefix(world.StateString(s), "minecraft:"))
		}
	}
	names := make([]string, len(ores))
	for i, o := range ores {
		names[i] = o.Key
	}
	log.Print(fmt.Sprintf(tr("mining %s: chest at %v (/stop to stop, /status to see how it goes)", "minando %s: cofre en %v (/stop para parar, /status para ver cómo va)"),
		strings.Join(names, ","), cfg.Chest))
	b.startWork("minar", automation.MineOres(cfg))
	return nil
}
