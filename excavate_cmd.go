package main

import (
	"fmt"
	"log"
	"math"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/world"

	"cubit/automation"
)

// cmdExcavate: /excavate <x1> <y1> <z1> <x2> <y2> <z2> [exact] — vaciar la
// caja entre esas dos esquinas (con ~ relativo al bot). Una caja de chunks
// enteros escrita con los bordes (Z -48 a -63: el chunk -4, que en bloques va
// de -49 a -64) se ajusta a los chunks; con exact, justo esos bloques.
// /excavate chunk [<x> <z>] <y1> <y2> — el chunk donde está el bot (o el de
// ese bloque).
func cmdExcavate(b *Bot, args []string) error {
	if len(args) > 0 && strings.EqualFold(args[0], "chunk") {
		return cmdExcavateChunk(b, args[1:])
	}
	if len(args) > 0 && strings.EqualFold(args[0], "chests") {
		return cmdExcavateChests(b, args[1:])
	}
	exact := len(args) == 7 && strings.EqualFold(args[6], "exact")
	if exact {
		args = args[:6]
	}
	if len(args) != 6 {
		return errf("usage: /excavate <x1> <y1> <z1> <x2> <y2> <z2> [exact] (two opposite corners)",
			"uso: /excavate <x1> <y1> <z1> <x2> <y2> <z2> [exact] (dos esquinas opuestas)")
	}
	p := b.Player.Position()
	base := []float64{p.X, p.Y + 1e-6, p.Z}
	var v [6]int
	for i, a := range args {
		n, err := coord(a, base[i%3])
		if err != nil {
			return err
		}
		v[i] = int(math.Floor(n))
	}
	box := automation.NewBox(world.BlockPos{X: v[0], Y: v[1], Z: v[2]}, world.BlockPos{X: v[3], Y: v[4], Z: v[5]})
	if !exact {
		if fixed, note := snapToChunks(box); note != "" {
			log.Print(note)
			box = fixed
		}
	}
	return startExcavate(b, box)
}

// startExcavate comprueba el tamaño y empieza.
func startExcavate(b *Bot, box automation.Box) error {
	if n := box.Volume(); n > automation.ExcavateMax {
		return errf("that box has %d blocks: too many (at most %d); check the coordinates",
			"esa caja tiene %d bloques: demasiados (como mucho %d); revisa las coordenadas", n, automation.ExcavateMax)
	}
	where := "lo valioso me lo quedo y la basura la tiro"
	if n := len(b.Tasks.Store.ExcavateChestList()); n > 0 {
		where = fmt.Sprintf("la basura la tiro y lo valioso lo guardo en los cofres (%d)", n)
	}
	log.Printf(tr("excavate: %s (%d blocks at most): %s (/stop to stop)", "excavar %s (%d bloques como mucho): %s (/stop para parar)"), box, box.Volume(), where)
	b.startWork("excavar", automation.Excavate(box))
	return nil
}

// cmdProtected: /protected [list | here | clear] — los sitios donde el
// servidor dijo "no tienes permiso".
func cmdProtected(b *Bot, args []string) error {
	st := b.Tasks.Store
	action := "list"
	if len(args) > 0 {
		action = strings.ToLower(args[0])
	}
	switch action {
	case "list":
		l := st.ProtectedList()
		if len(l) == 0 {
			log.Print(tr("protected places: none", "sitios protegidos: ninguno"))
			return nil
		}
		for _, p := range l {
			log.Printf(tr("protected: %v (x %d…%d, z %d…%d)", "protegido: %v (x %d…%d, z %d…%d)"), p,
				p.X-automation.ProtectRadius, p.X+automation.ProtectRadius, p.Z-automation.ProtectRadius, p.Z+automation.ProtectRadius)
		}
		return nil
	case "here":
		p := b.Player.Position()
		here := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y)), Z: int(math.Floor(p.Z))}
		log.Printf(tr("protected: forgot %d around %v", "protegido: olvido %d alrededor de %v"), st.ClearProtected(&here), here)
		return nil
	case "clear":
		log.Printf(tr("protected: forgot all (%d)", "protegido: los olvido todos (%d)"), st.ClearProtected(nil))
		return nil
	}
	return errf("usage: /protected [list | here | clear]", "uso: /protected [list | here | clear]")
}

// chunkOf: el chunk de la coordenada de bloque v (también con negativos:
// el -1 va de -16 a -1).
func chunkOf(v int) int { return v >> 4 }

// chunkBounds: el primer y el último bloque del chunk c.
func chunkBounds(c int) (int, int) { return c * 16, c*16 + 15 }

// cmdExcavateChunk: /excavate chunk [<x> <z>] <y1> <y2>.
func cmdExcavateChunk(b *Bot, args []string) error {
	p := b.Player.Position()
	x, z := p.X, p.Z
	switch len(args) {
	case 2:
	case 4:
		var err error
		if x, err = coord(args[0], p.X); err != nil {
			return err
		}
		if z, err = coord(args[1], p.Z); err != nil {
			return err
		}
		args = args[2:]
	default:
		return errf("usage: /excavate chunk [<x> <z>] <y1> <y2> (the chunk the bot is in, or the one with that block)",
			"uso: /excavate chunk [<x> <z>] <y1> <y2> (el chunk donde está el bot, o el de ese bloque)")
	}
	var ys [2]int
	for i, a := range args {
		n, err := coord(a, p.Y+1e-6)
		if err != nil {
			return err
		}
		ys[i] = int(math.Floor(n))
	}
	cx, cz := chunkOf(int(math.Floor(x))), chunkOf(int(math.Floor(z)))
	x1, x2 := chunkBounds(cx)
	z1, z2 := chunkBounds(cz)
	box := automation.NewBox(world.BlockPos{X: x1, Y: ys[0], Z: z1}, world.BlockPos{X: x2, Y: ys[1], Z: z2})
	log.Printf("chunk %d, %d: de %d %d a %d %d (16×16)", cx, cz, x1, z1, x2, z2)
	return startExcavate(b, box)
}

// snapToChunks: una caja de chunks enteros (16, 32… de ancho) que se sale
// justo un bloque de los bordes de chunk es casi seguro un chunk escrito con
// sus bordes: con negativos el chunk -4 se ve entre las líneas -48 y -64, pero
// en bloques va de -49 a -64 (-48 ya es del -3). Se corre ese bloque para que
// sea el chunk entero; note lo explica ("" si no hacía falta).
func snapToChunks(box automation.Box) (automation.Box, string) {
	shift := func(lo, hi int) int {
		if (hi-lo+1)%16 != 0 {
			return 0
		}
		switch lo & 15 {
		case 1:
			return -1
		case 15:
			return 1
		}
		return 0
	}
	var notes []string
	fixed := box
	if d := shift(box.Min.X, box.Max.X); d != 0 {
		fixed.Min.X, fixed.Max.X = box.Min.X+d, box.Max.X+d
		notes = append(notes, fmt.Sprintf("X %d…%d → %d…%d", box.Min.X, box.Max.X, fixed.Min.X, fixed.Max.X))
	}
	if d := shift(box.Min.Z, box.Max.Z); d != 0 {
		fixed.Min.Z, fixed.Max.Z = box.Min.Z+d, box.Max.Z+d
		notes = append(notes, fmt.Sprintf("Z %d…%d → %d…%d", box.Min.Z, box.Max.Z, fixed.Min.Z, fixed.Max.Z))
	}
	if len(notes) == 0 {
		return box, ""
	}
	cx1, cx2 := chunkOf(fixed.Min.X), chunkOf(fixed.Max.X)
	cz1, cz2 := chunkOf(fixed.Min.Z), chunkOf(fixed.Max.Z)
	which := fmt.Sprintf("al chunk %d, %d", cx1, cz1)
	if cx1 != cx2 || cz1 != cz2 {
		which = fmt.Sprintf("a los chunks %d…%d, %d…%d", cx1, cx2, cz1, cz2)
	}
	return fixed, fmt.Sprintf("ajusto la caja %s (%s): lo escrito se salía un bloque del chunk. Para cavar justo esas coordenadas, añade exact al final",
		which, strings.Join(notes, ", "))
}

// cmdExcavateChests: /excavate chests [list | clear | <x> <y> <z> | remove <x> <y> <z>]
// — los cofres donde /excavate guarda lo valioso.
func cmdExcavateChests(b *Bot, args []string) error {
	st := b.Tasks.Store
	usage := func() error {
		return errf("usage: /excavate chests [list | clear | <x> <y> <z> | remove <x> <y> <z>]",
			"uso: /excavate chests [list | clear | <x> <y> <z> | remove <x> <y> <z>]")
	}
	action := "list"
	if len(args) > 0 {
		action = strings.ToLower(args[0])
	}
	pos := func(a []string) (world.BlockPos, error) {
		if len(a) != 3 {
			return world.BlockPos{}, usage()
		}
		p := b.Player.Position()
		base := []float64{p.X, p.Y + 1e-6, p.Z}
		var v [3]int
		for i, s := range a {
			n, err := coord(s, base[i])
			if err != nil {
				return world.BlockPos{}, err
			}
			v[i] = int(math.Floor(n))
		}
		return world.BlockPos{X: v[0], Y: v[1], Z: v[2]}, nil
	}
	switch action {
	case "list":
		l := st.ExcavateChestList()
		if len(l) == 0 {
			log.Print(tr("excavate chests: none (it throws the junk and keeps the valuables)", "cofres de excavate: ninguno (tira la basura y se queda lo valioso)"))
			return nil
		}
		for _, c := range l {
			what := tr("not loaded from here", "no está cargado desde aquí")
			if s, ok := b.World.BlockAt(c); ok {
				what = strings.TrimPrefix(world.StateString(s), "minecraft:")
				if i := strings.IndexByte(what, '['); i >= 0 {
					what = what[:i]
				}
			}
			log.Printf(tr("excavate chest: %d %d %d (%s)", "cofre de excavate: %d %d %d (%s)"), c.X, c.Y, c.Z, what)
		}
		return nil
	case "clear":
		log.Printf(tr("excavate chests: forgot all (%d)", "cofres de excavate: los olvido todos (%d)"), st.RemoveExcavateChest(nil))
		return nil
	case "remove":
		c, err := pos(args[1:])
		if err != nil {
			return err
		}
		if st.RemoveExcavateChest(&c) == 0 {
			return errf("%d %d %d is not one of the excavate chests", "%d %d %d no es uno de los cofres de excavate", c.X, c.Y, c.Z)
		}
		log.Printf(tr("excavate chest forgotten: %d %d %d", "cofre de excavate olvidado: %d %d %d"), c.X, c.Y, c.Z)
		return nil
	}
	c, err := pos(args)
	if err != nil {
		return err
	}
	// (si se ve desde aquí, que sea de verdad un cofre: si no lo es pero hay
	// uno pegado, un bloque de error al dar las coordenadas, ese; si no está
	// cargado, se apunta igual)
	if s, ok := b.World.BlockAt(c); ok && !automation.IsContainer(world.StateString(s)) {
		fixed := false
		for _, d := range [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}, {0, -1, 0}, {0, 1, 0}} {
			n := world.BlockPos{X: c.X + d[0], Y: c.Y + d[1], Z: c.Z + d[2]}
			if s2, ok := b.World.BlockAt(n); ok && automation.IsContainer(world.StateString(s2)) {
				log.Printf(tr("there is no chest at %d %d %d, but there is one right next to it: %d %d %d (I use that one)",
					"en %d %d %d no hay cofre, pero hay uno pegado: %d %d %d (uso ese)"), c.X, c.Y, c.Z, n.X, n.Y, n.Z)
				c, fixed = n, true
				break
			}
		}
		if !fixed {
			return errf("there is no chest at %d %d %d (there is %s)", "en %d %d %d no hay un cofre (hay %s)", c.X, c.Y, c.Z,
				strings.TrimPrefix(world.StateString(s), "minecraft:"))
		}
	}
	added, err := st.AddExcavateChest(c)
	if err != nil {
		return errf("%v", "%v", err)
	}
	n := len(st.ExcavateChestList())
	if !added {
		log.Printf(tr("that chest was already there (%d in all)", "ese cofre ya estaba (%d en total)"), n)
		return nil
	}
	log.Printf(tr("excavate chest: %d %d %d (%d in all): /excavate keeps the valuables there and takes pickaxes from it; the chests touching it count too",
		"cofre de excavate: %d %d %d (%d en total): /excavate guarda ahí lo valioso y coge de ahí los picos; los cofres pegados a él cuentan también"), c.X, c.Y, c.Z, n)
	return nil
}
