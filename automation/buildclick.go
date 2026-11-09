package automation

// Cómo poner un bloque del schematic para que quede como pide: contra qué
// bloque de al lado, en qué cara, en qué punto de ella, y desde dónde mirar.
// El servidor decide el estado del bloque puesto con lo que haría con un
// jugador (getStateForPlacement): la cara pinchada (troncos, antorchas de
// pared, escaleras de pared), la altura del clic (losas y escaleras de
// arriba) y hacia dónde mira el jugador (escaleras, hornos, carteles). Como
// el bot mira justo al punto que pincha (el anticheat lo exige), mirar hacia
// un lado se consigue poniéndose en el sitio desde el que ese punto se ve en
// esa dirección.

import (
	"math"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"

	"minecraft-bot/schematic"
)

// facingRule es cómo sale el "facing" de un bloque al ponerlo.
type facingRule int

const (
	ruleLook     facingRule = iota // hacia donde mira el jugador (escaleras, puertas, puertas de valla, camas)
	ruleOpp                        // hacia el jugador (hornos, cofres, calabazas, la mayoría)
	ruleCW                         // a la derecha de donde mira (yunques)
	ruleCCW                        // a la izquierda
	ruleLook6                      // hacia donde mira, también arriba/abajo (observadores)
	ruleOpp6                       // hacia el jugador, también arriba/abajo (pistones, dispensadores, barriles)
	ruleFace                       // la cara pinchada (antorchas de pared, escaleras de mano, varas del End)
	ruleOppFace                    // contra la cara pinchada (tolvas)
	ruleTrapdoor                   // trampillas: la cara pinchada si es de lado; si no, hacia el jugador
	nRules
)

var ruleNames = [...]string{"mirar", "hacia mí", "derecha", "izquierda", "mirar6", "hacia mí6", "cara", "contra la cara", "trampilla"}

func (r facingRule) String() string {
	if r >= 0 && int(r) < len(ruleNames) {
		return ruleNames[r]
	}
	return "?"
}

// faceDir es el vector de cada cara (en el orden de act.Face).
var faceDir = [6][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

var faceNames = [6]string{"down", "up", "north", "south", "west", "east"}

func faceOf(name string) (act.Face, bool) {
	for i, n := range faceNames {
		if n == name {
			return act.Face(i), true
		}
	}
	return 0, false
}

func oppFace(f act.Face) act.Face { return f ^ 1 }

// cw es la cara a la derecha (horario visto desde arriba) de una horizontal.
func cwFace(f act.Face) act.Face {
	switch f {
	case act.North:
		return act.East
	case act.East:
		return act.South
	case act.South:
		return act.West
	case act.West:
		return act.North
	}
	return f
}

func ccwFace(f act.Face) act.Face { return cwFace(cwFace(cwFace(f))) }

func horizontalFace(f act.Face) bool { return f >= act.North }

// defaultRules son las reglas a probar para un bloque, la más probable primero
// (lo que se aprende al ponerlos las va descartando).
func defaultRules(name string, props map[string]string) []facingRule {
	f := props["facing"]
	switch {
	case hasSuffix(name, "_trapdoor"):
		return []facingRule{ruleTrapdoor}
	case schematic.Wall(name), name == "ladder", hasSuffix(name, "_wall_fan"),
		name == "end_rod", name == "lightning_rod", hasSuffix(name, "amethyst_bud"), name == "amethyst_cluster",
		hasSuffix(name, "shulker_box"), name == "tripwire_hook":
		return []facingRule{ruleFace, ruleOppFace, ruleOpp, ruleLook}
	case (hasSuffix(name, "_button") || name == "lever" || name == "grindstone") && props["face"] == "wall":
		return []facingRule{ruleFace, ruleOpp}
	case hasSuffix(name, "_button") || name == "lever" || name == "grindstone":
		return []facingRule{ruleLook, ruleOpp}
	case name == "hopper":
		return []facingRule{ruleOppFace, ruleFace}
	case name == "cocoa":
		return []facingRule{ruleOppFace, ruleFace}
	case name == "piston" || name == "sticky_piston" || name == "dispenser" || name == "dropper" || name == "barrel" || name == "crafter":
		return []facingRule{ruleOpp6, ruleLook6, ruleOpp, ruleLook}
	case name == "observer":
		return []facingRule{ruleLook6, ruleOpp6, ruleLook, ruleOpp}
	case hasSuffix(name, "anvil"):
		return []facingRule{ruleCW, ruleCCW, ruleLook, ruleOpp}
	case hasSuffix(name, "_stairs"), hasSuffix(name, "_door"), hasSuffix(name, "_fence_gate"), hasSuffix(name, "_bed"),
		name == "campfire", name == "soul_campfire", name == "decorated_pot", hasSuffix(name, "_shelf"):
		return []facingRule{ruleLook, ruleOpp, ruleCW, ruleCCW}
	case f == "up" || f == "down":
		return []facingRule{ruleOpp6, ruleLook6, ruleFace, ruleOppFace}
	}
	return []facingRule{ruleOpp, ruleLook, ruleCW, ruleCCW, ruleOpp6, ruleLook6, ruleFace, ruleOppFace}
}

func hasSuffix(s, suf string) bool { return len(s) >= len(suf) && s[len(s)-len(suf):] == suf }

// lookOf es hacia dónde mira un vector: en horizontal (4) y en las 6 direcciones,
// y si está claro (no en el borde entre dos, donde un redondeo cambia el resultado).
func lookOf(d [3]float64) (h4, h6 act.Face, clear4, clear6 bool) {
	ax, ay, az := math.Abs(d[0]), math.Abs(d[1]), math.Abs(d[2])
	if ax > az {
		h4 = act.West
		if d[0] > 0 {
			h4 = act.East
		}
	} else {
		h4 = act.North
		if d[2] > 0 {
			h4 = act.South
		}
	}
	hor := math.Max(ax, az)
	clear4 = math.Abs(ax-az) > 0.12*math.Hypot(ax, az)
	h6 = h4
	if ay > hor {
		h6 = act.Down
		if d[1] > 0 {
			h6 = act.Up
		}
	}
	clear6 = math.Abs(ay-hor) > 0.12*math.Sqrt(ax*ax+ay*ay+az*az) && (ay > hor || clear4)
	return
}

// yawSegment es la rotación de 16 pasos de un vector (la de los carteles:
// RotationSegment.convertToSegment del yaw) y si está lejos del borde.
func yawSegment(d [3]float64) (int, bool) {
	yaw := -math.Atan2(d[0], d[2]) * 180 / math.Pi
	x := yaw / 22.5
	seg := int(math.Floor(x+0.5)) & 15
	frac := x + 0.5 - math.Floor(x+0.5)
	return seg, frac > 0.15 && frac < 0.85
}

// predictFacing es el facing que daría una regla (para aprender de lo que pasó).
func predictFacing(r facingRule, look4, look6, face act.Face) act.Face {
	switch r {
	case ruleLook:
		return look4
	case ruleOpp:
		return oppFace(look4)
	case ruleCW:
		return cwFace(look4)
	case ruleCCW:
		return ccwFace(look4)
	case ruleLook6:
		return look6
	case ruleOpp6:
		return oppFace(look6)
	case ruleFace:
		return face
	case ruleOppFace:
		return oppFace(face)
	case ruleTrapdoor:
		if horizontalFace(face) {
			return face
		}
		return oppFace(look4)
	}
	return face
}

// blockSource es el mundo para planear (World.BlockAt).
type blockSource interface {
	BlockAt(world.BlockPos) (block.StateID, bool)
}

// clickPlan es un clic para poner un bloque.
type clickPlan struct {
	against world.BlockPos // el bloque pinchado (el propio bloque si onSelf)
	face    act.Face       // su cara
	cursor  [3]float32     // el punto, en coordenadas del bloque pinchado
	onSelf  bool           // pinchar el propio bloque (losa doble, más velas)
	look4   act.Face
	look6   act.Face
	score   float64 // menor es mejor
}

// clickNeeds es lo que el estado pedido exige del clic.
type clickNeeds struct {
	want    schematic.State
	rule    facingRule
	hasRule bool
	rotOff  int  // rotación = segmento del yaw + rotOff (carteles: 8)
	solidT  bool // el bloque tiene colisión: el cuerpo no puede estar en la celda
	// onSelf: el bloque ya está a medias (losa de abajo, velas): se pincha él mismo
	onSelf bool
	// forceFace: una cara que hay que pinchar sí o sí (aprendido: antorcha de pared en vez de suelo)
	forceFace *act.Face
}

// eye es la posición de los ojos.
type vec3 = [3]float64

// cursorsFor son los puntos de una cara que se prueban: el centro y alrededor
// (lo justo para que la altura y la dirección de la mirada cambien).
func cursorsFor(f act.Face) [][3]float32 {
	vals := []float32{0.5, 0.2, 0.8}
	var out [][3]float32
	for _, a := range vals {
		for _, b := range vals {
			c := [3]float32{0.5, 0.5, 0.5}
			switch f {
			case act.Up, act.Down:
				c = [3]float32{a, 1, b}
				if f == act.Down {
					c[1] = 0
				}
			case act.North, act.South:
				c = [3]float32{a, b, 1}
				if f == act.North {
					c[2] = 0
				}
			default:
				c = [3]float32{1, b, a}
				if f == act.West {
					c[0] = 0
				}
			}
			out = append(out, c)
		}
	}
	return out
}

// planClick busca el mejor clic desde los ojos en eye para poner want en t.
// reach es el alcance (desde los ojos al punto pinchado).
func planClick(w blockSource, t world.BlockPos, n clickNeeds, eye vec3, reach float64) (clickPlan, bool) {
	best, found := clickPlan{score: math.MaxFloat64}, false
	props := n.want.Props
	name := n.want.Name
	wantFacing, hasFacing := faceOf(props["facing"])
	if !n.hasRule {
		hasFacing = false
	}
	try := func(against world.BlockPos, f act.Face, onSelf bool) {
		if n.forceFace != nil && f != *n.forceFace && !onSelf {
			return
		}
		// la cara tiene que verse: los ojos al lado de fuera de su plano
		switch f {
		case act.Up:
			if eye[1] <= float64(against.Y)+1+0.05 && !onSelf {
				return
			}
		case act.Down:
			if eye[1] >= float64(against.Y)-0.05 && !onSelf {
				return
			}
		case act.North:
			if eye[2] >= float64(against.Z)-0.05 {
				return
			}
		case act.South:
			if eye[2] <= float64(against.Z)+1+0.05 {
				return
			}
		case act.West:
			if eye[0] >= float64(against.X)-0.05 {
				return
			}
		case act.East:
			if eye[0] <= float64(against.X)+1+0.05 {
				return
			}
		}
		cursors := cursorsFor(f)
		if onSelf {
			cursors = [][3]float32{{0.5, 0.5, 0.5}}
		}
		for ci, c := range cursors {
			hit := vec3{float64(against.X) + float64(c[0]), float64(against.Y) + float64(c[1]), float64(against.Z) + float64(c[2])}
			d := vec3{hit[0] - eye[0], hit[1] - eye[1], hit[2] - eye[2]}
			dist := math.Sqrt(d[0]*d[0] + d[1]*d[1] + d[2]*d[2])
			if dist > reach || dist < 0.3 {
				continue
			}
			look4, look6, clear4, clear6 := lookOf(d)
			if !onSelf && !halfOK(name, props, f, c) {
				continue
			}
			if hasFacing {
				needs4 := n.rule == ruleLook || n.rule == ruleOpp || n.rule == ruleCW || n.rule == ruleCCW ||
					n.rule == ruleTrapdoor && !horizontalFace(f)
				needs6 := n.rule == ruleLook6 || n.rule == ruleOpp6
				if needs4 && !clear4 || needs6 && !clear6 {
					continue
				}
				if predictFacing(n.rule, look4, look6, f) != wantFacing {
					continue
				}
				if n.rule == ruleTrapdoor && !horizontalFace(f) && trapdoorHalfFromVertical(f) != props["half"] {
					continue
				}
			}
			if r, ok := props["rotation"]; ok && !onSelf {
				seg, clear := yawSegment(d)
				if !clear || (seg+n.rotOff)&15 != atoiDef(r, -1) {
					continue
				}
			}
			if blockedRay(w, eye, hit, t, against) {
				continue
			}
			score := dist + float64(ci)*0.05
			if onSelf {
				score -= 1
			}
			if score < best.score {
				best = clickPlan{against: against, face: f, cursor: c, onSelf: onSelf, look4: look4, look6: look6, score: score}
				found = true
			}
		}
	}
	if n.onSelf {
		// una losa de abajo se pincha por arriba; la de arriba, por abajo; velas y demás, por arriba
		f := act.Up
		if st, ok := w.BlockAt(t); ok && hasStateProp(st, "type=top") {
			f = act.Down
		}
		try(t, f, true)
		return best, found
	}
	for fi := 0; fi < 6; fi++ {
		f := act.Face(fi)
		if !faceAllowed(name, props, f) {
			continue
		}
		o := faceDir[f]
		against := world.BlockPos{X: t.X - o[0], Y: t.Y - o[1], Z: t.Z - o[2]}
		st, ok := w.BlockAt(against)
		if !ok || len(block.CollisionShape(st)) == 0 || block.Replaceable(st) {
			continue
		}
		try(against, f, false)
	}
	return best, found
}

// faceAllowed: la cara pinchada f (la del vecino que toca la celda) sirve para el bloque.
func faceAllowed(name string, props map[string]string, f act.Face) bool {
	if a, ok := props["axis"]; ok {
		switch a {
		case "y":
			if f != act.Up && f != act.Down {
				return false
			}
		case "x":
			if f != act.East && f != act.West {
				return false
			}
		case "z":
			if f != act.North && f != act.South {
				return false
			}
		}
	}
	switch props["face"] {
	case "floor":
		return f == act.Up
	case "ceiling":
		return f == act.Down
	case "wall":
		return horizontalFace(f)
	}
	switch props["hanging"] {
	case "true":
		return f == act.Down
	case "false":
		if name == "lantern" || name == "soul_lantern" || name == "copper_lantern" || hasSuffix(name, "_lantern") {
			return f == act.Up
		}
	}
	if schematic.Floor(name) {
		return f == act.Up
	}
	if schematic.Hanging(name) {
		return f == act.Down
	}
	if schematic.Wall(name) {
		return horizontalFace(f)
	}
	return true
}

// halfOK: el clic deja la mitad pedida (losas, escaleras, trampillas de lado).
func halfOK(name string, props map[string]string, f act.Face, c [3]float32) bool {
	want := props["half"]
	if want != "top" && want != "bottom" {
		want = ""
	}
	if t := props["type"]; t == "top" || t == "bottom" {
		want = t
	} else if t == "double" {
		want = "bottom" // la primera, abajo; la segunda pinchándola
	}
	if want == "" {
		return true
	}
	if hasSuffix(name, "_trapdoor") && !horizontalFace(f) {
		return true // la mitad la da la cara (trapdoorHalfFromVertical)
	}
	switch f {
	case act.Up:
		return want == "bottom"
	case act.Down:
		return want == "top"
	}
	if want == "top" {
		return c[1] > 0.55
	}
	return c[1] < 0.45
}

// trapdoorHalfFromVertical: una trampilla puesta pinchando arriba queda abajo, y al revés.
func trapdoorHalfFromVertical(f act.Face) string {
	if f == act.Up {
		return "bottom"
	}
	return "top"
}

// blockedRay: algo sólido tapa la línea de los ojos al punto (salvo la celda
// del bloque y la pinchada).
func blockedRay(w blockSource, eye, hit vec3, t, against world.BlockPos) bool {
	d := vec3{hit[0] - eye[0], hit[1] - eye[1], hit[2] - eye[2]}
	l := math.Sqrt(d[0]*d[0] + d[1]*d[1] + d[2]*d[2])
	steps := int(l / 0.1)
	for i := 1; i < steps; i++ {
		k := float64(i) / float64(steps)
		c := world.BlockPos{X: int(math.Floor(eye[0] + d[0]*k)), Y: int(math.Floor(eye[1] + d[1]*k)), Z: int(math.Floor(eye[2] + d[2]*k))}
		if c == t || c == against {
			continue
		}
		st, ok := w.BlockAt(c)
		if !ok {
			return true
		}
		if len(block.CollisionShape(st)) > 0 && !block.Replaceable(st) {
			return true
		}
	}
	return false
}

func hasStateProp(st block.StateID, kv string) bool {
	s := world.StateString(st)
	for i := 0; i+len(kv) <= len(s); i++ {
		if s[i:i+len(kv)] == kv {
			return true
		}
	}
	return false
}

func atoiDef(s string, def int) int {
	n, neg := 0, false
	if s == "" {
		return def
	}
	for i, r := range s {
		if i == 0 && r == '-' {
			neg = true
			continue
		}
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
	}
	if neg {
		n = -n
	}
	return n
}
