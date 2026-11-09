package main

// /build: construir un schematic. La colocación la manda el jugador que lo
// tiene en Litematica (el bot no ve su pantalla): el origen, la rotación y el
// espejo que salen en "Placement settings". Lo más fácil, por privado:
//
//	/msg Bot /build load casa.litematic yo          (el origen: donde está el jugador)
//	/msg Bot /build load casa.litematic 120 64 -30 cw_90 none
//	/msg Bot /build check                           (cuánto encaja con lo ya puesto)
//	/msg Bot /build start

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/automation"
	"minecraft-bot/schematic"
)

// schematicsDir es donde se buscan los schematics por nombre.
const schematicsDir = "schematics"

// registerBuildCommand añade /build a las órdenes (lo llama el init de console.go,
// que es quien crea el mapa).
func registerBuildCommand() {
	rot := arg{name: "rotation", kind: argChoice, opt: true, choices: []string{"none", "90", "180", "270"},
		descs: map[string]text{"none": {"as it is", "tal cual"}, "90": {"a quarter turn clockwise", "un cuarto de vuelta a la derecha"},
			"180": {"half a turn", "media vuelta"}, "270": {"a quarter turn counterclockwise", "un cuarto de vuelta a la izquierda"}}}
	mir := arg{name: "mirror", kind: argChoice, opt: true, choices: []string{"none", "left_right", "front_back"},
		descs: map[string]text{"none": {"no mirror", "sin espejo"}, "left_right": {"mirrored left to right", "de izquierda a derecha"}, "front_back": {"mirrored front to back", "de delante a atrás"}}}
	coords := func(opt bool, alt ...string) []arg {
		return []arg{{name: "x", kind: argCoord, opt: opt, choices: alt}, {name: "y", kind: argCoord, opt: opt}, {name: "z", kind: argCoord, opt: opt}}
	}
	file := arg{name: "file", kind: argChoice, dyn: listSchematics, desc: text{"a schematic of schematics/", "un schematic de schematics/"}}
	commands["build"] = command{
		desc: text{"build a schematic (Litematica .litematic, WorldEdit .schem, structure .nbt) with the materials of some chests: load <file> [<x> <y> <z> | here | <player>] [rotation] [mirror], origin, rotate, mirror, import <litematica placements .json>, info, check, materials, chest add|remove|list|clear, start [noclear], list",
			"construir un schematic (Litematica .litematic, WorldEdit .schem, estructura .nbt) con los materiales de unos cofres: load <archivo> [<x> <y> <z> | here | <jugador>] [rotación] [espejo], origin, rotate, mirror, import <.json de colocaciones de Litematica>, info, check, materials, chest add|remove|list|clear, start [noclear], list"},
		subs: []sub{
			{name: "load", desc: text{"load a schematic (and where it goes)", "cargar un schematic (y dónde va)"},
				args: append([]arg{file}, append(coords(true, "here"), rot, mir)...)},
			{name: "origin", desc: text{"where it goes: <x> <y> <z>, here or a player", "dónde va: <x> <y> <z>, here o un jugador"}, args: coords(false, "here")},
			{name: "rotate", desc: text{"turn it", "girarlo"}, args: []arg{func() arg { r := rot; r.opt = false; return r }()}},
			{name: "mirror", desc: text{"mirror it", "reflejarlo"}, args: []arg{func() arg { m := mir; m.opt = false; return m }()}},
			{name: "import", desc: text{"the placements of a Litematica .json", "las colocaciones de un .json de Litematica"},
				args: []arg{{name: "file.json", desc: text{"the .json file", "el archivo .json"}}, {name: "name", opt: true, desc: text{"which placement", "qué colocación"}}}},
			{name: "info", desc: text{"what is loaded and where", "qué está cargado y dónde"}},
			{name: "check", desc: text{"what is already built and what is missing", "qué está hecho y qué falta"}},
			{name: "materials", desc: text{"the materials it needs and those in the chests", "los materiales que necesita y los de los cofres"}},
			{name: "chest", desc: text{"the chests with the materials", "los cofres de los materiales"},
				args: []arg{{name: "action", kind: argChoice, choices: []string{"add", "remove", "list", "clear"},
					descs: map[string]text{"add": {"mark the nearest one (or <x> <y> <z>)", "marcar el más cercano (o <x> <y> <z>)"}, "remove": {"forget one", "olvidar uno"},
						"list": {"show them", "enseñarlos"}, "clear": {"forget them all", "olvidarlos todos"}}},
					{name: "x", kind: argCoord, opt: true}, {name: "y", kind: argCoord, opt: true}, {name: "z", kind: argCoord, opt: true}}},
			{name: "start", desc: text{"start building", "empezar a construir"},
				args: []arg{{name: "noclear", kind: argChoice, opt: true, choices: []string{"noclear"}, descs: map[string]text{"noclear": {"without clearing the area first", "sin despejar la zona antes"}}}}},
			{name: "list", desc: text{"the schematics in schematics/", "los schematics de schematics/"}},
		},
		run: cmdBuild,
	}
	usages["build"] = "/build load <file> [<x> <y> <z> | me | <player>] [none|cw_90|180|ccw_90] [none|left_right|front_back] | origin <x> <y> <z> | rotate <r> | mirror <m> | import <placements.json> | info | check | materials | chest [add|remove [<x> <y> <z>] | list | clear] | start [noclear] | list"
	queueKinds["build"] = true
}

func cmdBuild(b *Bot, args []string) error {
	if len(args) == 0 {
		return errf("usage: "+usages["build"], "uso: "+usages["build"])
	}
	st := b.Tasks.Build
	action, rest := strings.ToLower(args[0]), args[1:]
	switch action {
	case "list":
		files := listSchematics()
		if len(files) == 0 {
			log.Printf("construir: no hay schematics en %s/ (copia ahí los .litematic, .schem o .nbt)", schematicsDir)
			return nil
		}
		log.Printf("construir: schematics en %s/: %s", schematicsDir, strings.Join(files, ", "))
		return nil
	case "load":
		if len(rest) == 0 {
			return errf("which file? /build load <file> [...] (/build list)", "¿qué archivo? /build load <archivo> [...] (/build list)")
		}
		path, err := findSchematic(rest[0])
		if err != nil {
			return err
		}
		s, err := schematic.Load(path)
		if err != nil {
			return err
		}
		st.Update(func(s *automation.BuildState) { s.Schematic = path })
		size := s.Size()
		log.Printf("construir: %s cargado (%s%d×%d×%d, %d bloques, %d regiones)", filepath.Base(path), authorOf(s), size.X, size.Y, size.Z, s.Blocks(), len(s.Regions))
		if len(rest) > 1 {
			if err := buildPlacementArgs(b, rest[1:]); err != nil {
				return err
			}
		}
		return buildInfo(b)
	case "origin":
		if err := buildOrigin(b, rest); err != nil {
			return err
		}
		return buildInfo(b)
	case "rotate", "rotation":
		if len(rest) != 1 {
			return errf("/build rotate <none|cw_90|180|ccw_90>", "/build rotate <none|cw_90|180|ccw_90>")
		}
		r, err := schematic.ParseRotation(rest[0])
		if err != nil {
			return err
		}
		st.Update(func(s *automation.BuildState) { s.Rotation = int(r) })
		return buildInfo(b)
	case "mirror":
		if len(rest) != 1 {
			return errf("/build mirror <none|left_right|front_back>", "/build mirror <none|left_right|front_back>")
		}
		m, err := schematic.ParseMirror(rest[0])
		if err != nil {
			return err
		}
		st.Update(func(s *automation.BuildState) { s.Mirror = int(m) })
		return buildInfo(b)
	case "import":
		if len(rest) == 0 {
			return errf("/build import <file.json> [name]", "/build import <archivo.json> [nombre]")
		}
		return buildImport(b, rest[0], strings.Join(rest[1:], " "))
	case "info":
		return buildInfo(b)
	case "check", "materials":
		return buildCheck(b, action == "materials")
	case "chest", "chests", "cofre":
		return buildChests(b, rest)
	case "start":
		return buildStart(b, rest)
	}
	return errf("usage: "+usages["build"], "uso: "+usages["build"])
}

func authorOf(s *schematic.Schematic) string {
	if s.Author == "" {
		return ""
	}
	return "de " + s.Author + ", "
}

// listSchematics son los archivos de schematics/.
func listSchematics() []string {
	ents, err := os.ReadDir(schematicsDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".litematic", ".schem", ".schematic", ".nbt":
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// findSchematic busca el archivo: la ruta tal cual, o en schematics/ (sin
// extensión o sin distinguir mayúsculas).
func findSchematic(name string) (string, error) {
	if _, err := os.Stat(name); err == nil {
		return name, nil
	}
	p := filepath.Join(schematicsDir, name)
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	base := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
	for _, f := range listSchematics() {
		if strings.ToLower(strings.TrimSuffix(f, filepath.Ext(f))) == base || strings.EqualFold(f, name) {
			return filepath.Join(schematicsDir, f), nil
		}
	}
	return "", errf("no schematic %q (put it in %s/; /build list)", "no encuentro el schematic %q (ponlo en %s/; /build list)", name, schematicsDir)
}

// buildPlacementArgs lee "[<x> <y> <z> | jugador] [rotación] [espejo]".
func buildPlacementArgs(b *Bot, args []string) error {
	n := 1
	if len(args) >= 3 && isNumArg(args[0]) {
		n = 3
	}
	if err := buildOrigin(b, args[:n]); err != nil {
		return err
	}
	args = args[n:]
	if len(args) > 0 {
		r, err := schematic.ParseRotation(args[0])
		if err != nil {
			return err
		}
		b.Tasks.Build.Update(func(s *automation.BuildState) { s.Rotation = int(r) })
		args = args[1:]
	}
	if len(args) > 0 {
		m, err := schematic.ParseMirror(args[0])
		if err != nil {
			return err
		}
		b.Tasks.Build.Update(func(s *automation.BuildState) { s.Mirror = int(m) })
	}
	return nil
}

// buildOrigin: "<x> <y> <z>" (con ~ relativo al bot), "here"/"aquí" (donde
// está el bot) o el nombre de un jugador (donde están sus pies: el origen que
// pone Litematica al cargar el schematic estando ahí).
func buildOrigin(b *Bot, args []string) error {
	p := b.Player.Position()
	var o [3]int
	switch {
	case len(args) == 3:
		for i, a := range args {
			v, err := coord(a, []float64{p.X, p.Y, p.Z}[i])
			if err != nil {
				return err
			}
			o[i] = int(math.Floor(v))
		}
	case len(args) == 1 && (strings.EqualFold(args[0], "here") || strings.EqualFold(args[0], "aqui") || strings.EqualFold(args[0], "aquí")):
		o = [3]int{int(math.Floor(p.X)), int(math.Floor(p.Y + 1e-6)), int(math.Floor(p.Z))}
	case len(args) == 1:
		e, err := b.findPlayer(args[0])
		if err != nil {
			return err
		}
		o = [3]int{int(math.Floor(e.X)), int(math.Floor(e.Y + 1e-6)), int(math.Floor(e.Z))}
		log.Printf("construir: el origen, donde está %s: %d %d %d", args[0], o[0], o[1], o[2])
	default:
		return errf("origin: <x> <y> <z>, here, or a player (me)", "origen: <x> <y> <z>, here, o un jugador (yo)")
	}
	dim := string(b.Player.Spawn.Dimension)
	b.Tasks.Build.Update(func(s *automation.BuildState) { s.Origin, s.Dim = &o, dim })
	return nil
}

// buildImport lee las colocaciones que guarda Litematica (un .json con
// "origin", "rotation", "mirror" y "schematic") y usa la de ese nombre (o la
// única, o la primera activa).
func buildImport(b *Bot, file, want string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		if data, err = os.ReadFile(filepath.Join(schematicsDir, file)); err != nil {
			return err
		}
	}
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	type found struct {
		schem, name, rot, mir string
		origin                [3]int
		enabled               bool
	}
	var all []found
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if o, ok := x["origin"].([]any); ok && len(o) == 3 {
				f := found{enabled: true}
				for i := range o {
					if n, ok := o[i].(float64); ok {
						f.origin[i] = int(n)
					}
				}
				f.schem, _ = x["schematic"].(string)
				f.name, _ = x["name"].(string)
				f.rot, _ = x["rotation"].(string)
				f.mir, _ = x["mirror"].(string)
				if e, ok := x["enabled"].(bool); ok {
					f.enabled = e
				}
				all = append(all, f)
			}
			for _, c := range x {
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(root)
	if len(all) == 0 {
		return errf("%s has no placements (no \"origin\")", "%s no tiene colocaciones (ningún \"origin\")", file)
	}
	pick := -1
	for i, f := range all {
		if want != "" && (strings.EqualFold(f.name, want) || strings.Contains(strings.ToLower(f.schem), strings.ToLower(want))) {
			pick = i
			break
		}
	}
	if pick < 0 && want == "" {
		for i, f := range all {
			if f.enabled {
				pick = i
				break
			}
		}
	}
	if pick < 0 {
		var names []string
		for _, f := range all {
			names = append(names, fmt.Sprintf("%q (%s)", f.name, filepath.Base(f.schem)))
		}
		return errf("which one? %s", "¿cuál? %s", strings.Join(names, ", "))
	}
	f := all[pick]
	rot, mir := schematic.Rotation(0), schematic.MirrorNone
	if f.rot != "" {
		if rot, err = schematic.ParseRotation(f.rot); err != nil {
			return err
		}
	}
	if f.mir != "" {
		if mir, err = schematic.ParseMirror(f.mir); err != nil {
			return err
		}
	}
	// el schematic: el de la colocación, si lo tenemos (por su nombre)
	if f.schem != "" {
		// (la ruta es la del ordenador del jugador: solo vale el nombre)
		name := filepath.Base(strings.ReplaceAll(f.schem, "\\", "/"))
		if path, err := findSchematic(name); err == nil {
			b.Tasks.Build.Update(func(s *automation.BuildState) { s.Schematic = path })
		} else {
			log.Printf("construir: la colocación es de %s, que no tengo: cópialo a %s/", name, schematicsDir)
		}
	}
	o := f.origin
	dim := string(b.Player.Spawn.Dimension)
	b.Tasks.Build.Update(func(s *automation.BuildState) {
		s.Origin, s.Rotation, s.Mirror, s.Dim = &o, int(rot), int(mir), dim
	})
	log.Printf("construir: colocación %q importada", f.name)
	return buildInfo(b)
}

// loadPlaced carga el schematic apuntado y lo coloca.
func loadPlaced(b *Bot) (*schematic.Schematic, *schematic.Placed, error) {
	st := b.Tasks.Build
	var file string
	st.View(func(s *automation.BuildState) { file = s.Schematic })
	if file == "" {
		return nil, nil, errf("no schematic: /build load <file>", "no hay schematic: /build load <archivo>")
	}
	pl, ok := st.Placement()
	if !ok {
		return nil, nil, errf("no origin: /build origin <x> <y> <z> | me", "falta el origen: /build origin <x> <y> <z> | yo")
	}
	s, err := schematic.Load(file)
	if err != nil {
		return nil, nil, err
	}
	p, err := s.Place(pl)
	if err != nil {
		return nil, nil, err
	}
	return s, p, nil
}

func buildInfo(b *Bot) error {
	st := b.Tasks.Build
	var file, dim string
	var chests []world.BlockPos
	st.View(func(s *automation.BuildState) { file, dim, chests = s.Schematic, s.Dim, append(chests, s.Chests...) })
	if file == "" {
		log.Print("construir: sin schematic (/build load <archivo>; /build list)")
		return nil
	}
	pl, ok := st.Placement()
	if !ok {
		log.Printf("construir: %s, sin origen todavía: /build origin <x> <y> <z> (o \"yo\" por privado, de pie en el origen de Litematica)", filepath.Base(file))
		return nil
	}
	_, p, err := loadPlaced(b)
	if err != nil {
		return err
	}
	cs := "los de alrededor del bot al empezar (a 12 bloques)"
	if len(chests) > 0 {
		cs = fmt.Sprint(chests)
	}
	where := ""
	if dim != "" && dim != string(b.Player.Spawn.Dimension) {
		where = fmt.Sprintf(" (ojo: en %s, y estoy en %s)", dim, b.Player.Spawn.Dimension)
	}
	log.Printf("construir: %s; %s; ocupa de %v a %v (%v)%s; cofres de materiales: %s",
		filepath.Base(file), pl, p.Min, p.Max, p.Size(), where, cs)
	return nil
}

// buildCheck compara el schematic con lo cargado: lo que está bien, lo que
// falta y los materiales; si ya hay algo puesto y otra rotación o espejo
// encaja mejor, lo dice (un dato mal copiado de Litematica).
func buildCheck(b *Bot, materials bool) error {
	s, p, err := loadPlaced(b)
	if err != nil {
		return err
	}
	c := automation.CheckBuild(b.World, p)
	pct := 0.0
	if c.Total > 0 {
		pct = 100 * float64(c.Good) / float64(c.Total)
	}
	log.Printf("construir: %d/%d bloques bien (%.1f %%), %d por poner, %d equivocados, %d que sobran, %d sin cargar",
		c.Good, c.Total, pct, c.Missing, c.Wrong, c.Extra, c.Unloaded)
	if len(c.Unsupported) > 0 {
		log.Printf("construir: no sé poner: %s", automation.FormatNeeds(c.Unsupported))
	}
	if materials || c.Good < c.Total {
		log.Printf("construir: materiales que faltan por poner: %s", automation.FormatNeeds(c.Needs))
	}
	if c.Good > 0 && c.Good < c.Total {
		pl := p.Placement
		best, bestPl := c.Good, pl
		for r := 0; r < 4; r++ {
			for _, m := range []schematic.Mirror{schematic.MirrorNone, schematic.MirrorLeftRight} {
				alt := schematic.Placement{Origin: pl.Origin, Rotation: schematic.Rotation(r), Mirror: m}
				if alt == pl {
					continue
				}
				ap, err := s.Place(alt)
				if err != nil {
					continue
				}
				if ac := automation.CheckBuild(b.World, ap); ac.Good > best+best/4+3 {
					best, bestPl = ac.Good, alt
				}
			}
		}
		if bestPl != pl {
			log.Printf("construir: ojo, con rotación %s y espejo %s encajan %d bloques (ahora %d): ¿está bien copiada la colocación de Litematica?",
				bestPl.Rotation, bestPl.Mirror, best, c.Good)
		}
	}
	return nil
}

func buildChests(b *Bot, args []string) error {
	st := b.Tasks.Build
	p := b.Player.Position()
	here := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y)), Z: int(math.Floor(p.Z))}
	action := "list"
	if len(args) > 0 {
		action, args = strings.ToLower(args[0]), args[1:]
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
		added := false
		st.Update(func(s *automation.BuildState) {
			for _, q := range s.Chests {
				if q == c {
					return
				}
			}
			s.Chests = append(s.Chests, c)
			added = true
		})
		if added {
			log.Printf("construir: cofre de materiales en %v", c)
		} else {
			log.Printf("construir: el cofre de %v ya estaba", c)
		}
	case "remove":
		c, err := where()
		if err != nil {
			return err
		}
		st.Update(func(s *automation.BuildState) {
			for i, q := range s.Chests {
				if q == c {
					s.Chests = append(s.Chests[:i], s.Chests[i+1:]...)
					return
				}
			}
		})
		log.Printf("construir: cofre de %v olvidado", c)
	case "clear":
		st.Update(func(s *automation.BuildState) { s.Chests = nil })
		log.Print("construir: cofres de materiales olvidados (al empezar uso los de alrededor)")
	case "list":
		var cs []world.BlockPos
		st.View(func(s *automation.BuildState) { cs = append(cs, s.Chests...) })
		if len(cs) == 0 {
			log.Print("construir: sin cofres apuntados (al empezar uso los que haya a 12 bloques del bot)")
		} else {
			log.Printf("construir: cofres de materiales: %v", cs)
		}
	default:
		return errf("/build chest add|remove [<x> <y> <z>] | list | clear", "/build chest add|remove [<x> <y> <z>] | list | clear")
	}
	return nil
}

func buildStart(b *Bot, args []string) error {
	_, p, err := loadPlaced(b)
	if err != nil {
		return err
	}
	st := b.Tasks.Build
	var file, dim string
	var chests []world.BlockPos
	noClear := false
	for _, a := range args {
		if strings.EqualFold(a, "noclear") {
			noClear = true
		}
	}
	st.View(func(s *automation.BuildState) { file, dim, chests = s.Schematic, s.Dim, append(chests, s.Chests...) })
	if dim != "" && dim != string(b.Player.Spawn.Dimension) {
		return errf("the build is in %s and I am in %s", "la construcción es en %s y estoy en %s", dim, b.Player.Spawn.Dimension)
	}
	name := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	log.Printf("construir %s: %s (/stop para parar)", name, p.Placement)
	b.startWork("construir", automation.Build(p, automation.BuildOptions{Name: name, NoClear: noClear, Chests: chests}))
	return nil
}
