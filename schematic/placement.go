package schematic

import (
	"fmt"
	"strconv"
	"strings"
)

// Rotation es el giro de Litematica, en cuartos de vuelta en el sentido de
// las agujas del reloj visto desde arriba: 0 none, 1 CW 90, 2 CW 180, 3 CCW 90.
type Rotation int

// Mirror es el espejo de Litematica (el de Minecraft): LeftRight cambia z
// por -z; FrontBack cambia x por -x.
type Mirror int

const (
	MirrorNone Mirror = iota
	MirrorLeftRight
	MirrorFrontBack
)

func (r Rotation) String() string {
	return [...]string{"none", "cw_90", "cw_180", "ccw_90"}[r&3]
}

func (m Mirror) String() string {
	return [...]string{"none", "left_right", "front_back"}[m]
}

// ParseRotation lee una rotación como la escribe la gente o Litematica:
// "none", "0", "90", "cw90", "cw_90", "clockwise_90", "180", "270",
// "ccw90", "-90", "counterclockwise_90"…
func ParseRotation(s string) (Rotation, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	t = strings.NewReplacer("_", "", "-", "-", "°", "", " ", "", "rotate", "", "deg", "").Replace(t)
	switch t {
	case "none", "0", "no", "ninguna", "360":
		return 0, nil
	case "90", "cw90", "clockwise90", "r90", "+90", "derecha":
		return 1, nil
	case "180", "cw180", "clockwise180", "ccw180", "-180", "r180":
		return 2, nil
	case "270", "ccw90", "counterclockwise90", "-90", "cw270", "r270", "izquierda":
		return 3, nil
	}
	return 0, fmt.Errorf("rotación %q: none, 90, 180 o 270 (o cw_90, ccw_90…)", s)
}

// ParseMirror lee un espejo: "none", "left_right"/"lr", "front_back"/"fb".
func ParseMirror(s string) (Mirror, error) {
	t := strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(s))
	switch t {
	case "none", "no", "0", "ninguno":
		return MirrorNone, nil
	case "leftright", "lr", "z":
		return MirrorLeftRight, nil
	case "frontback", "fb", "x":
		return MirrorFrontBack, nil
	}
	return 0, fmt.Errorf("espejo %q: none, left_right (lr) o front_back (fb)", s)
}

// Placement es dónde y cómo va el schematic en el mundo (lo que el jugador
// ve en Litematica).
type Placement struct {
	Origin   Pos
	Rotation Rotation
	Mirror   Mirror
}

func (p Placement) String() string {
	return fmt.Sprintf("origen %v, rotación %s, espejo %s", p.Origin, p.Rotation, p.Mirror)
}

// Transform lleva una posición relativa al origen del schematic a su sitio
// relativo en el mundo (PositionUtils.getTransformedBlockPos de Litematica:
// el espejo y luego la rotación).
func (p Placement) Transform(rel Pos) Pos {
	x, y, z := rel.X, rel.Y, rel.Z
	switch p.Mirror {
	case MirrorLeftRight:
		z = -z
	case MirrorFrontBack:
		x = -x
	}
	switch p.Rotation & 3 {
	case 1:
		x, z = -z, x
	case 2:
		x, z = -x, -z
	case 3:
		x, z = z, -x
	}
	return Pos{x, y, z}
}

// World es la posición en el mundo de un bloque del schematic.
func (p Placement) World(rel Pos) Pos { return p.Origin.Add(p.Transform(rel)) }

// ---- los estados: mirror(..).rotate(..) ----

var horizontal = []string{"north", "east", "south", "west"} // en orden horario

func dirIndex(d string) int {
	for i, h := range horizontal {
		if h == d {
			return i
		}
	}
	return -1
}

func rotateDir(d string, r Rotation) string {
	if i := dirIndex(d); i >= 0 {
		return horizontal[(i+int(r&3))&3]
	}
	return d
}

func mirrorDir(d string, m Mirror) string {
	switch {
	case m == MirrorLeftRight && d == "north":
		return "south"
	case m == MirrorLeftRight && d == "south":
		return "north"
	case m == MirrorFrontBack && d == "east":
		return "west"
	case m == MirrorFrontBack && d == "west":
		return "east"
	}
	return d
}

// TransformState es el estado del bloque tal como queda colocado (espejo y
// rotación de la colocación). Las propiedades que el juego recalcula al poner
// el bloque (formas de escaleras, conexiones de vallas) también se giran,
// aunque luego no se comparan.
func (p Placement) TransformState(s State) State {
	if len(s.Props) == 0 || p.Rotation&3 == 0 && p.Mirror == MirrorNone {
		return s
	}
	out := State{Name: s.Name, Props: make(map[string]string, len(s.Props))}
	for k, v := range s.Props {
		out.Props[k] = v
	}
	if p.Mirror != MirrorNone {
		mirrorProps(out.Props, p.Mirror)
	}
	if p.Rotation&3 != 0 {
		rotateProps(out.Props, p.Rotation)
	}
	return out
}

// sides son las propiedades con el nombre de un lado (vallas, muros, cristales, redstone, enredaderas…).
func sideKeys(props map[string]string) bool {
	for _, k := range horizontal {
		if _, ok := props[k]; ok {
			return true
		}
	}
	return false
}

func mirrorProps(props map[string]string, m Mirror) {
	for _, k := range []string{"facing", "horizontal_facing"} {
		if v, ok := props[k]; ok {
			props[k] = mirrorDir(v, m)
		}
	}
	if v, ok := props["rotation"]; ok {
		r := atoi(v)
		if m == MirrorLeftRight {
			r = (16 - r) % 16
		} else {
			r = (8 - r + 16) % 16
		}
		props["rotation"] = strconv.Itoa(r)
	}
	if v, ok := props["orientation"]; ok { // crafter, jigsaw: "up_east", "north_up"
		parts := strings.Split(v, "_")
		for i := range parts {
			parts[i] = mirrorDir(parts[i], m)
		}
		props["orientation"] = strings.Join(parts, "_")
	}
	if sideKeys(props) {
		a, b := "north", "south"
		if m == MirrorFrontBack {
			a, b = "east", "west"
		}
		va, oka := props[a]
		vb, okb := props[b]
		if oka && okb {
			props[a], props[b] = vb, va
		}
	}
	if v, ok := props["hinge"]; ok { // las puertas cambian de bisagra (DoorBlock.mirror)
		if v == "left" {
			props["hinge"] = "right"
		} else if v == "right" {
			props["hinge"] = "left"
		}
	}
	if v, ok := props["shape"]; ok {
		props["shape"] = mirrorShape(v, m)
	}
	if v, ok := props["type"]; ok && (v == "left" || v == "right") { // cofres dobles
		if v == "left" {
			props["type"] = "right"
		} else {
			props["type"] = "left"
		}
	}
}

func rotateProps(props map[string]string, r Rotation) {
	r &= 3
	for _, k := range []string{"facing", "horizontal_facing"} {
		if v, ok := props[k]; ok {
			props[k] = rotateDir(v, r)
		}
	}
	if v, ok := props["axis"]; ok && r%2 == 1 {
		if v == "x" {
			props["axis"] = "z"
		} else if v == "z" {
			props["axis"] = "x"
		}
	}
	if v, ok := props["rotation"]; ok {
		props["rotation"] = strconv.Itoa((atoi(v) + 4*int(r)) % 16)
	}
	if v, ok := props["orientation"]; ok {
		parts := strings.Split(v, "_")
		for i := range parts {
			parts[i] = rotateDir(parts[i], r)
		}
		props["orientation"] = strings.Join(parts, "_")
	}
	if sideKeys(props) {
		old := map[string]string{}
		for _, k := range horizontal {
			if v, ok := props[k]; ok {
				old[k] = v
			}
		}
		if len(old) == 4 {
			for k, v := range old {
				props[rotateDir(k, r)] = v
			}
		}
	}
	if v, ok := props["shape"]; ok {
		props["shape"] = rotateShape(v, r)
	}
}

// rotateShape gira la forma de un raíl ("north_east", "ascending_south",
// "east_west"); la de una escalera (inner_left…) no cambia al girar.
func rotateShape(v string, r Rotation) string {
	parts := strings.Split(v, "_")
	changed := false
	for i, p := range parts {
		if dirIndex(p) >= 0 {
			parts[i] = rotateDir(p, r)
			changed = true
		}
	}
	if !changed {
		return v
	}
	return normalizeRail(parts)
}

func mirrorShape(v string, m Mirror) string {
	switch v { // escaleras
	case "inner_left":
		return "inner_right"
	case "inner_right":
		return "inner_left"
	case "outer_left":
		return "outer_right"
	case "outer_right":
		return "outer_left"
	}
	parts := strings.Split(v, "_")
	changed := false
	for i, p := range parts {
		if dirIndex(p) >= 0 {
			parts[i] = mirrorDir(p, m)
			changed = true
		}
	}
	if !changed {
		return v
	}
	return normalizeRail(parts)
}

// normalizeRail escribe una forma de raíl como el juego: north_south,
// east_west, ascending_X, y las curvas con north/south primero.
func normalizeRail(parts []string) string {
	if len(parts) == 2 && parts[0] == "ascending" {
		return "ascending_" + parts[1]
	}
	if len(parts) != 2 {
		return strings.Join(parts, "_")
	}
	a, b := parts[0], parts[1]
	ns := func(d string) bool { return d == "north" || d == "south" }
	switch {
	case ns(a) && ns(b):
		return "north_south"
	case !ns(a) && !ns(b):
		return "east_west"
	case ns(b):
		a, b = b, a
	}
	return a + "_" + b
}
