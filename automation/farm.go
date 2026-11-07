package automation

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/entitydata"
	pk "github.com/mj41/go-mc26/net/packet"
)

// La granja: un campo de trigo y un corral de ovejas junto a la base, para
// hacer camas (3 de lana del mismo color y 3 tablones cada una).
//
//   - El trigo sirve para atraer ovejas (lo siguen si se lleva en la mano, a
//     menos de 10 bloques) y para que críen (uno a cada una de dos adultas).
//   - Las ovejas se esquilan (1 a 3 de lana cada vez) y la lana les vuelve a
//     crecer comiendo hierba: el corral es de hierba.

const (
	penSide   = 7 // el corral: 7x7 de vallas por fuera (5x5 por dentro)
	penSheep  = 8 // ovejas que quiere en el corral
	fieldSize = 12
)

// ---- ovejas (lo que dice el servidor de cada una) ----

type sheep struct {
	e             entities.Entity
	color         string
	sheared, baby bool
}

var dyeColors = []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
	"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"}

func sheepOf(e entities.Entity) sheep {
	s := sheep{e: e, color: "white"}
	if v, ok := e.Data[entitydata.SheepWool]; ok {
		if b, ok := v.Value.(*pk.Byte); ok {
			s.color = dyeColors[int(*b)&15]
			s.sheared = int(*b)&16 != 0
		}
	}
	if v, ok := e.Data[entitydata.AgeableMobBaby]; ok {
		if b, ok := v.Value.(*pk.Boolean); ok {
			s.baby = bool(*b)
		}
	}
	return s
}

// sheepNear son las ovejas a menos de r (las más cercanas primero).
func (j *Job) sheepNear(r float64) []sheep {
	p := j.Pos()
	var out []sheep
	for _, e := range j.r.d.Entities.Nearby(p.X, p.Y, p.Z, r) {
		if e.Type == "minecraft:sheep" {
			out = append(out, sheepOf(e))
		}
	}
	sort.Slice(out, func(a, b int) bool {
		return math.Hypot(out[a].e.X-p.X, out[a].e.Z-p.Z) < math.Hypot(out[b].e.X-p.X, out[b].e.Z-p.Z)
	})
	return out
}

// ---- el corral ----

// Pen es el corral: sus esquinas (las vallas) y la puerta.
type Pen struct {
	Min, Max world.BlockPos // las vallas de las esquinas (Y: la altura de las vallas)
	Gate     world.BlockPos
}

// inside: (x, z) está dentro del corral (entre las vallas).
func (p *Pen) inside(x, z float64) bool {
	return x > float64(p.Min.X)+1 && x < float64(p.Max.X) && z > float64(p.Min.Z)+1 && z < float64(p.Max.Z)
}

func (p *Pen) center() world.BlockPos {
	return world.BlockPos{X: (p.Min.X + p.Max.X) / 2, Y: p.Min.Y, Z: (p.Min.Z + p.Max.Z) / 2}
}

// outside es la celda de fuera, delante de la puerta.
func (p *Pen) outside() world.BlockPos {
	g := p.Gate
	switch {
	case g.Z == p.Min.Z:
		g.Z--
	case g.Z == p.Max.Z:
		g.Z++
	case g.X == p.Min.X:
		g.X--
	default:
		g.X++
	}
	return g
}

// penSpot busca, cerca de la base, un cuadrado de penSide x penSide llano y de hierba.
func (j *Job) penSpot() (world.BlockPos, bool) {
	base := j.Feet()
	if j.r.Store.Base != nil {
		base = *j.r.Store.Base
	}
	for r := 4; r <= 28; r += 2 {
		for _, d := range [][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}, {1, 1}, {-1, 1}, {1, -1}, {-1, -1}} {
			x0, z0 := base.X+d[0]*r, base.Z+d[1]*r
			g, ok := j.r.d.Walker.Ground(x0, z0, base.Y)
			if !ok {
				continue
			}
			flat, grass := true, 0
			for dx := 0; dx < penSide && flat; dx++ {
				for dz := 0; dz < penSide && flat; dz++ {
					q, ok := j.r.d.Walker.Ground(x0+dx, z0+dz, g.Y)
					if !ok || q.Y != g.Y || j.r.Store.taken(q) || j.liquid(world.BlockPos{X: q.X, Y: q.Y - 1, Z: q.Z}) {
						flat = false
						break
					}
					if j.Block(world.BlockPos{X: q.X, Y: q.Y - 1, Z: q.Z}) == "grass_block" {
						grass++
					}
				}
			}
			if flat && grass >= (penSide-2)*(penSide-2)/2 {
				return world.BlockPos{X: x0, Y: g.Y, Z: z0}, true
			}
		}
	}
	return world.BlockPos{}, false
}

// EnsurePen construye el corral (si no lo hay): vallas alrededor y una puerta.
func (j *Job) EnsurePen() (*Pen, error) {
	if p := j.r.Store.Pen; p != nil {
		return p, nil
	}
	at, ok := j.penSpot()
	if !ok {
		return nil, errors.New("no encuentro sitio llano con hierba para el corral")
	}
	pen := &Pen{Min: at, Max: world.BlockPos{X: at.X + penSide - 1, Y: at.Y, Z: at.Z + penSide - 1}}
	pen.Gate = world.BlockPos{X: at.X + penSide/2, Y: at.Y, Z: at.Z}
	var ring []world.BlockPos
	for i := 0; i < penSide; i++ {
		ring = append(ring, world.BlockPos{X: at.X + i, Y: at.Y, Z: at.Z}, world.BlockPos{X: at.X + i, Y: at.Y, Z: pen.Max.Z})
		if i > 0 && i < penSide-1 {
			ring = append(ring, world.BlockPos{X: at.X, Y: at.Y, Z: at.Z + i}, world.BlockPos{X: pen.Max.X, Y: at.Y, Z: at.Z + i})
		}
	}
	// los materiales: vallas (4 tablones y 2 palos dan 3) y una puerta (2 tablones y 4 palos)
	fences := len(ring) - 1
	need := (fences+2)/3*4 + 2 + ((fences+2)/3*2+4+3)/4*2 // tablones: vallas, puerta y palos
	if err := j.ensureLogs((need+3)/4+1, ""); err != nil {
		return nil, err
	}
	// los palos primero (gastan tablones), luego los tablones de las vallas
	if err := j.EnsureSticks((fences+2)/3*2 + 4); err != nil {
		return nil, err
	}
	wood, err := j.EnsureSamePlanks((fences+2)/3*4 + 2)
	if err != nil {
		return nil, err
	}
	kind := strings.TrimSuffix(wood, "_planks")
	fence, gate := kind+"_fence", kind+"_fence_gate"
	if j.Count(fence) < fences {
		if err := j.CraftAtTable([]string{wood, "stick", wood, wood, "stick", wood}, 3, (fences-j.Count(fence)+2)/3, nil); err != nil {
			return nil, fmt.Errorf("vallas: %w", err)
		}
	}
	if j.Count(gate) == 0 {
		if err := j.CraftAtTable([]string{"stick", wood, "stick", "stick", wood, "stick"}, 3, 1, nil); err != nil {
			return nil, fmt.Errorf("puerta de valla: %w", err)
		}
	}
	j.Status("construyendo el corral")
	j.Logf("corral en %v (puerta en %v)", pen.Min, pen.Gate)
	for _, c := range ring {
		item := fence
		if c == pen.Gate {
			continue
		}
		if err := j.placeFence(item, c, pen); err != nil {
			return nil, err
		}
	}
	// la puerta, desde fuera (mirando hacia dentro: queda atravesada en el muro)
	if err := j.GoTo(pen.outside()); err != nil {
		return nil, err
	}
	if err := j.PlaceOn(gate, pen.Gate, world.BlockPos{X: pen.Gate.X, Y: pen.Gate.Y - 1, Z: pen.Gate.Z}); err != nil {
		return nil, fmt.Errorf("puerta: %w", err)
	}
	j.r.Store.mu.Lock()
	j.r.Store.Pen = pen
	j.r.Store.mu.Unlock()
	j.r.Store.save()
	return pen, nil
}

// placeFence pone una valla en c, desde fuera del corral.
func (j *Job) placeFence(item string, c world.BlockPos, pen *Pen) error {
	if !j.placeable(c) {
		if !j.solid(c) {
			if err := j.clearCell(c); err != nil { // (hierba alta, flores)
				return err
			}
		} else {
			return nil // ya hay algo que hace de muro
		}
	}
	if !j.Reaches(c) {
		if err := j.GoNear(c); err != nil {
			return err
		}
	}
	if j.inBody(c) { // apartarse
		out := world.BlockPos{X: c.X + sign(c.X-pen.center().X), Y: c.Y, Z: c.Z + sign(c.Z-pen.center().Z)}
		if err := j.GoTo(out); err != nil {
			return err
		}
	}
	return j.PlaceOn(item, c, world.BlockPos{X: c.X, Y: c.Y - 1, Z: c.Z})
}

// gateOpen dice si la puerta está abierta.
func (j *Job) gateOpen(pen *Pen) bool {
	st, ok := j.r.d.World.BlockAt(pen.Gate)
	return ok && strings.Contains(world.StateString(st), "open=true")
}

// setGate abre o cierra la puerta del corral.
func (j *Job) setGate(pen *Pen, open bool) error {
	if j.gateOpen(pen) == open {
		return nil
	}
	if err := j.GoNear(pen.Gate); err != nil {
		return err
	}
	done := make(chan act.Use, 1)
	j.r.d.Hands.UseBlock(pen.Gate, func(u act.Use) { done <- u })
	select {
	case u := <-done:
		if u.Err != nil {
			return fmt.Errorf("puerta: %w", u.Err)
		}
	case <-time.After(5 * time.Second):
		return errors.New("puerta: no contesta")
	case <-j.ctx.Done():
		return j.ctx.Err()
	}
	return j.Wait(2)
}

// passGate cruza la puerta (hacia dentro o hacia fuera) y la deja cerrada.
func (j *Job) passGate(pen *Pen, in bool) error {
	from, to := pen.outside(), pen.Gate
	if !in {
		from = world.BlockPos{X: 2*pen.Gate.X - pen.outside().X, Y: pen.Gate.Y, Z: 2*pen.Gate.Z - pen.outside().Z}
		to = pen.outside()
	}
	if err := j.GoTo(from); err != nil {
		return err
	}
	if err := j.setGate(pen, true); err != nil {
		return err
	}
	if in {
		to = world.BlockPos{X: 2*pen.Gate.X - pen.outside().X, Y: pen.Gate.Y, Z: 2*pen.Gate.Z - pen.outside().Z}
	}
	if err := j.GoTo(to); err != nil {
		return err
	}
	return j.setGate(pen, false)
}

// sheepIn son las ovejas que hay dentro del corral.
func (j *Job) sheepIn(pen *Pen) []sheep {
	var out []sheep
	c := pen.center()
	for _, e := range j.r.d.Entities.Nearby(float64(c.X)+0.5, float64(c.Y), float64(c.Z)+0.5, 8) {
		if e.Type == "minecraft:sheep" && pen.inside(e.X, e.Z) {
			out = append(out, sheepOf(e))
		}
	}
	return out
}

// LureSheep trae ovejas de fuera al corral con trigo en la mano (lo siguen a
// menos de 10 bloques): va a por ellas, vuelve despacio (esperándolas),
// entra, suelta el trigo y sale cerrando la puerta.
func (j *Job) LureSheep(pen *Pen, want int) error {
	for tries := 0; len(j.sheepIn(pen)) < want && tries < 6; tries++ {
		if j.Count("wheat") == 0 {
			return errors.New("para atraer ovejas hace falta trigo")
		}
		var wild []sheep
		for _, s := range j.sheepNear(64) {
			if !pen.inside(s.e.X, s.e.Z) {
				wild = append(wild, s)
			}
		}
		if len(wild) == 0 {
			return errors.New("no veo ovejas fuera del corral")
		}
		t := wild[0]
		j.Status("atrayendo ovejas al corral (%d dentro)", len(j.sheepIn(pen)))
		if err := j.Hold("wheat"); err != nil {
			return err
		}
		if err := j.goNearEntity(t.e.ID, 4); err != nil {
			j.Logf("oveja: %v", err)
			continue
		}
		// las que me siguen: las de menos de 8 bloques
		followers := func() []int32 {
			var ids []int32
			for _, s := range j.sheepNear(8) {
				if !pen.inside(s.e.X, s.e.Z) {
					ids = append(ids, s.e.ID)
				}
			}
			return ids
		}
		if err := j.lead(pen.outside(), followers); err != nil {
			j.Logf("llevarlas: %v", err)
			continue
		}
		if err := j.setGate(pen, true); err != nil {
			return err
		}
		if err := j.lead(pen.center(), followers); err != nil {
			j.Logf("meterlas: %v", err)
		}
		// esperar a que entren, soltar el trigo y salir
		for t := 0; t < 30; t++ {
			if err := j.Wait(10); err != nil {
				return err
			}
			if len(followers()) == 0 {
				break
			}
		}
		j.dropWheatFromHand()
		if err := j.passGate(pen, false); err != nil {
			return err
		}
		j.Logf("ovejas en el corral: %d", len(j.sheepIn(pen)))
	}
	return nil
}

// dropWheatFromHand: otra cosa en la mano (si no, las ovejas le siguen).
func (j *Job) dropWheatFromHand() {
	for _, it := range []string{j.best("pickaxe"), j.best("axe"), "shears", j.fillBlock()} {
		if it != "" && j.Count(it) > 0 && j.Hold(it) == nil {
			return
		}
	}
}

// goNearEntity camina hasta tener la entidad a menos de r.
func (j *Job) goNearEntity(id int32, r float64) error {
	w := j.r.d.Walker
	defer w.Stop()
	for t := 0; t < 300; t++ {
		e, ok := j.r.d.Entities.Get(id)
		if !ok {
			return errors.New("ya no está")
		}
		p := j.Pos()
		if math.Hypot(e.X-p.X, e.Z-p.Z) <= r && math.Abs(e.Y-p.Y) < 2 {
			return nil
		}
		if t%10 == 0 { // se mueve: recalcular
			w.SetSprint(false)
			w.Goto(world.BlockPos{X: int(math.Floor(e.X)), Y: int(math.Floor(e.Y + 0.2)), Z: int(math.Floor(e.Z))})
		}
		if err := j.Wait(2); err != nil {
			return err
		}
		if st := w.Status(); strings.HasPrefix(st, "failed") {
			return fmt.Errorf("no llego: %s", st)
		}
	}
	return errors.New("no la alcanzo")
}

// lead camina hasta to esperando a las que le siguen (que no se queden atrás).
func (j *Job) lead(to world.BlockPos, followers func() []int32) error {
	w := j.r.d.Walker
	defer w.Stop()
	w.SetSprint(false)
	w.Goto(to)
	for t := 0; t < 900; t++ {
		if err := j.Wait(2); err != nil {
			return err
		}
		// la más lejana de las que siguen: si se queda a más de 6, esperarla
		p := j.Pos()
		far := 0.0
		for _, id := range followers() {
			if e, ok := j.r.d.Entities.Get(id); ok {
				far = math.Max(far, math.Hypot(e.X-p.X, e.Z-p.Z))
			}
		}
		if far > 6 && w.Status() != "arrived" {
			w.Stop()
			for k := 0; k < 20 && far > 3.5; k++ {
				if err := j.Wait(5); err != nil {
					return err
				}
				far = 0
				p = j.Pos()
				for _, id := range followers() {
					if e, ok := j.r.d.Entities.Get(id); ok && math.Hypot(e.X-p.X, e.Z-p.Z) < 10 {
						far = math.Max(far, math.Hypot(e.X-p.X, e.Z-p.Z))
					}
				}
			}
			w.Goto(to)
		}
		switch st := w.Status(); {
		case st == "arrived":
			return nil
		case strings.HasPrefix(st, "failed"):
			return fmt.Errorf("no llego a %v: %s", to, st)
		case st == "stopped":
			w.Goto(to)
		}
	}
	return fmt.Errorf("tardo demasiado en llegar a %v", to)
}

// interactSheep usa lo que tiene en la mano (tijeras, trigo) con la oveja id.
func (j *Job) interactSheep(id int32, item string) error {
	if err := j.Hold(item); err != nil {
		return err
	}
	if err := j.goNearEntity(id, 2.5); err != nil {
		return err
	}
	j.r.d.Walker.Stop()
	done := make(chan error, 1)
	j.r.d.Hands.InteractEntity(func() (entities.Entity, bool) { return j.r.d.Entities.Get(id) }, func(err error) { done <- err })
	select {
	case err := <-done:
		if err != nil {
			return err
		}
	case <-time.After(5 * time.Second):
		return errors.New("no contesta")
	case <-j.ctx.Done():
		return j.ctx.Err()
	}
	return j.Wait(4)
}

// ensureShears hace unas tijeras (2 lingotes de hierro) si no tiene.
func (j *Job) ensureShears() error {
	if j.Count("shears") > 0 {
		return nil
	}
	if err := j.ensureIronIngots(2, ""); err != nil {
		return fmt.Errorf("tijeras: %w", err)
	}
	j.CloseScreen()
	if err := j.craftHere([]string{"", "iron_ingot", "iron_ingot", ""}, 2, 1); err != nil {
		return fmt.Errorf("tijeras: %w", err)
	}
	if j.Count("shears") == 0 {
		return errors.New("tijeras: no salieron")
	}
	return nil
}

// Shear esquila las ovejas adultas con lana que haya a menos de r (las del
// corral primero, entrando en él) y recoge la lana. Devuelve cuántas esquiló.
func (j *Job) Shear(r float64) (int, error) {
	if err := j.ensureShears(); err != nil {
		return 0, err
	}
	n := 0
	pen := j.r.Store.Pen
	if pen != nil {
		var woolly []sheep
		for _, s := range j.sheepIn(pen) {
			if !s.sheared && !s.baby {
				woolly = append(woolly, s)
			}
		}
		if len(woolly) > 0 {
			j.dropWheatFromHand()
			if err := j.passGate(pen, true); err != nil {
				return n, err
			}
			for _, s := range woolly {
				if err := j.interactSheep(s.e.ID, "shears"); err != nil {
					j.Logf("esquilar: %v", err)
					continue
				}
				n++
			}
			if err := j.Collect(6); err != nil {
				return n, err
			}
			if err := j.passGate(pen, false); err != nil {
				return n, err
			}
		}
	}
	for _, s := range j.sheepNear(r) {
		if s.sheared || s.baby || (pen != nil && pen.inside(s.e.X, s.e.Z)) {
			continue
		}
		if err := j.interactSheep(s.e.ID, "shears"); err != nil {
			j.Logf("esquilar: %v", err)
			continue
		}
		n++
		if err := j.Collect(5); err != nil {
			return n, err
		}
	}
	return n, nil
}

// Breed da trigo a las parejas de adultas del corral (salen corderos);
// cada una puede volver a criar a los 5 minutos.
func (j *Job) Breed(pen *Pen) (int, error) {
	var adults []sheep
	now := time.Now()
	for _, s := range j.sheepIn(pen) {
		if !s.baby && now.Sub(j.r.bred[s.e.UUID.String()]) > 5*time.Minute {
			adults = append(adults, s)
		}
	}
	pairs := min(len(adults)/2, j.Count("wheat")/2)
	if pairs == 0 || len(j.sheepIn(pen)) >= 2*penSheep {
		return 0, nil
	}
	if err := j.passGate(pen, true); err != nil {
		return 0, err
	}
	fed := 0
	for i := 0; i < 2*pairs; i++ {
		if err := j.interactSheep(adults[i].e.ID, "wheat"); err != nil {
			j.Logf("criar: %v", err)
			continue
		}
		j.r.bred[adults[i].e.UUID.String()] = now
		fed++
	}
	j.dropWheatFromHand()
	if err := j.passGate(pen, false); err != nil {
		return fed / 2, err
	}
	j.Logf("di trigo a %d ovejas (%d parejas)", fed, fed/2)
	return fed / 2, nil
}

// ---- el trigo ----

func (j *Job) hoeTier() string {
	for _, t := range tiers {
		if j.Count(t+"_hoe") > 0 {
			return t + "_hoe"
		}
	}
	return ""
}

// ensureHoe hace una azada (de piedra si lleva adoquines, si no de madera).
func (j *Job) ensureHoe() (string, error) {
	if h := j.hoeTier(); h != "" {
		return h, nil
	}
	if err := j.EnsureSticks(2); err != nil {
		return "", err
	}
	mat, hoe := j.most(stoneMaterial), "stone_hoe"
	if mat == "" || j.Count(mat) < 2 {
		var err error
		if mat, err = j.EnsureSamePlanks(2); err != nil {
			return "", err
		}
		hoe = "wooden_hoe"
	}
	if err := j.CraftAtTable([]string{mat, mat, "", "stick", "", "stick"}, 2, 1, nil); err != nil {
		return "", fmt.Errorf("azada: %w", err)
	}
	if j.Count(hoe) == 0 {
		return "", errors.New("azada: no salió")
	}
	return hoe, nil
}

func isGrassPlant(n string) bool { return n == "short_grass" || n == "tall_grass" || n == "grass" }

// ensureSeeds rompe hierba (una de cada ocho da semillas) hasta tener n.
func (j *Job) ensureSeeds(n int) error {
	skip := map[world.BlockPos]bool{}
	for tries := 0; j.Count("wheat_seeds") < n && tries < 80; tries++ {
		j.Status("buscando semillas (%d/%d)", j.Count("wheat_seeds"), n)
		p, ok := j.findBlock(24, 6, 6, isGrassPlant, skip)
		if !ok {
			break
		}
		skip[p] = true
		if err := j.GoNear(p); err != nil {
			continue
		}
		if err := j.Dig(p); err != nil {
			continue
		}
		if tries%4 == 3 {
			if err := j.Collect(6); err != nil {
				return err
			}
		}
	}
	if err := j.Collect(8); err != nil {
		return err
	}
	if j.Count("wheat_seeds") == 0 {
		return errors.New("no consigo semillas (no hay hierba alta cerca)")
	}
	return nil
}

// fieldCells elige dónde sembrar: tierra o hierba con aire encima cerca de la
// base, al lado del agua si la hay (se riega y crece el doble de rápido).
func (j *Job) fieldCells() []world.BlockPos {
	center := j.Feet()
	if j.r.Store.Base != nil {
		center = *j.r.Store.Base
	}
	if w, ok := j.nearTo(center, 20, func(n string) bool { return n == "water" }); ok {
		center = w
	}
	type cand struct {
		p world.BlockPos
		d float64
	}
	var cs []cand
	for dx := -8; dx <= 8; dx++ {
		for dz := -8; dz <= 8; dz++ {
			for dy := -2; dy <= 2; dy++ {
				p := world.BlockPos{X: center.X + dx, Y: center.Y + dy, Z: center.Z + dz}
				n := j.Block(p)
				above := world.BlockPos{X: p.X, Y: p.Y + 1, Z: p.Z}
				if (n == "grass_block" || n == "dirt" || n == "farmland") && (j.Block(above) == "air" || isGrassPlant(j.Block(above)) || j.Block(above) == "wheat") &&
					!j.r.Store.taken(above) && (j.r.Store.Pen == nil || !j.r.Store.Pen.inside(float64(p.X)+0.5, float64(p.Z)+0.5) && !j.nearPen(p)) {
					cs = append(cs, cand{p, math.Hypot(float64(dx), float64(dz)) + 2*math.Abs(float64(dy))})
				}
			}
		}
	}
	sort.Slice(cs, func(a, b int) bool { return cs[a].d < cs[b].d })
	var out []world.BlockPos
	for _, c := range cs {
		if len(out) >= fieldSize {
			break
		}
		out = append(out, c.p)
	}
	return out
}

func (j *Job) nearPen(p world.BlockPos) bool {
	pen := j.r.Store.Pen
	return pen != nil && p.X >= pen.Min.X-1 && p.X <= pen.Max.X+1 && p.Z >= pen.Min.Z-1 && p.Z <= pen.Max.Z+1
}

// TendField cuida el campo: siega el trigo maduro, ara lo que falte y siembra.
// Lo crea la primera vez. Devuelve cuánto trigo cosechó.
func (j *Job) TendField() (int, error) {
	before := j.Count("wheat")
	if len(j.r.Store.Field) == 0 {
		cells := j.fieldCells() // (fuera del candado: mira los cofres del almacén)
		j.r.Store.mu.Lock()
		j.r.Store.Field = cells
		j.r.Store.mu.Unlock()
		j.r.Store.save()
		if len(j.r.Store.Field) == 0 {
			return 0, errors.New("no encuentro tierra para sembrar trigo")
		}
		j.Logf("campo de trigo: %d celdas", len(j.r.Store.Field))
	}
	hoe, err := j.ensureHoe()
	if err != nil {
		return 0, err
	}
	for _, soil := range j.r.Store.Field {
		if err := j.Wait(1); err != nil {
			return 0, err
		}
		crop := world.BlockPos{X: soil.X, Y: soil.Y + 1, Z: soil.Z}
		st, ok := j.r.d.World.BlockAt(crop)
		if !ok {
			continue
		}
		cs := world.StateString(st)
		switch {
		case strings.HasPrefix(cs, "minecraft:wheat") && strings.Contains(cs, "age=7"):
			j.Status("segando trigo")
			if err := j.GoNear(crop); err != nil {
				continue
			}
			if err := j.Dig(crop); err != nil {
				continue
			}
		case strings.HasPrefix(cs, "minecraft:wheat"):
			continue // creciendo
		}
		if isGrassPlant(j.Block(crop)) {
			if err := j.GoNear(crop); err == nil {
				_ = j.Dig(crop)
			}
		}
		if n := j.Block(soil); n == "grass_block" || n == "dirt" {
			if err := j.GoNear(soil); err != nil {
				continue
			}
			if err := j.useOn(hoe, soil); err != nil {
				j.Logf("arar %v: %v", soil, err)
				continue
			}
		}
		if j.Block(soil) == "farmland" && j.Block(crop) == "air" {
			if j.Count("wheat_seeds") == 0 {
				if err := j.Collect(6); err != nil {
					return 0, err
				}
				if j.Count("wheat_seeds") == 0 {
					if err := j.ensureSeeds(4); err != nil {
						if j.Fatal() != nil {
							return 0, j.Fatal()
						}
						j.Logf("trigo: %v (siembro lo demás cuando tenga)", err)
						break
					}
				}
			}
			if err := j.GoNear(soil); err != nil {
				continue
			}
			if err := j.PlaceOn("wheat_seeds", crop, soil); err != nil {
				j.Logf("sembrar %v: %v", crop, err)
			}
		}
	}
	if err := j.Collect(8); err != nil {
		return 0, err
	}
	return j.Count("wheat") - before, nil
}

// useOn usa item sobre la cara de arriba del bloque p (una azada en la tierra).
func (j *Job) useOn(item string, p world.BlockPos) error {
	if err := j.Hold(item); err != nil {
		return err
	}
	j.r.d.Walker.Stop()
	done := make(chan act.Use, 1)
	j.r.d.Hands.UseBlockAt(p, act.Up, [3]float32{0.5, 1, 0.5}, func(u act.Use) { done <- u })
	select {
	case u := <-done:
		if u.Err != nil {
			return u.Err
		}
	case <-time.After(5 * time.Second):
		return errors.New("no contesta")
	case <-j.ctx.Done():
		return j.ctx.Err()
	}
	return j.Wait(2)
}

// ---- las camas ----

// woolColors: cuánta lana lleva de cada color.
func (j *Job) woolColors() map[string]int {
	out := map[string]int{}
	for it, c := range j.Items() {
		if strings.HasSuffix(it, "_wool") {
			out[strings.TrimSuffix(it, "_wool")] += c
		}
	}
	return out
}

// craftBeds hace hasta k camas con la lana que lleva (3 del mismo color cada una).
func (j *Job) craftBeds(k int) int {
	made := 0
	for color, c := range j.woolColors() {
		times := min(k-made, c/3)
		if times <= 0 {
			continue
		}
		wood, err := j.EnsureSamePlanks(3 * times)
		if err != nil {
			if err := j.ensureLogs((3*times+3)/4+1, ""); err != nil {
				j.Logf("camas: %v", err)
				return made
			}
			if wood, err = j.EnsureSamePlanks(3 * times); err != nil {
				j.Logf("camas: %v", err)
				return made
			}
		}
		w := color + "_wool"
		before := j.Beds()
		if err := j.CraftAtTable([]string{w, w, w, wood, wood, wood}, 3, times, nil); err != nil {
			j.Logf("camas: %v", err)
			continue
		}
		made += j.Beds() - before
	}
	return made
}

// EnsureBeds consigue n camas: con la lana que lleve, esquilando ovejas (las
// del corral y las de alrededor) y, si hace falta, cuidando la granja (trigo
// para que críen) y esperando a que les vuelva a crecer la lana.
func (j *Job) EnsureBeds(n int) error {
	start := time.Now()
	for j.Beds() < n {
		if err := j.Wait(1); err != nil {
			return err
		}
		if j.craftBeds(n-j.Beds()) > 0 {
			j.Logf("camas: %d de %d", j.Beds(), n)
			continue
		}
		j.Status("camas: %d de %d (esquilando)", j.Beds(), n)
		sheared, err := j.Shear(48)
		if err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			return err
		}
		if sheared > 0 {
			continue
		}
		// sin lana a mano: la granja (trigo, ovejas al corral, crías) y esperar a que crezca
		if time.Since(start) > 40*time.Minute {
			return fmt.Errorf("solo tengo %d camas de %d (las ovejas no dan más lana de momento)", j.Beds(), n)
		}
		if err := j.farmRound(); err != nil {
			j.Logf("granja: %v", err)
		}
		j.Status("camas: %d de %d (esperando a que crezca la lana)", j.Beds(), n)
		if err := j.Rest(20 * 30); err != nil {
			return err
		}
	}
	return nil
}

// farmRound es una vuelta de granjero: el trigo, el corral, ovejas nuevas y crías.
func (j *Job) farmRound() error {
	if _, err := j.TendField(); err != nil {
		j.Logf("trigo: %v", err)
	}
	pen, err := j.EnsurePen()
	if err != nil {
		return err
	}
	if len(j.sheepIn(pen)) < penSheep && j.Count("wheat") > 0 {
		if err := j.LureSheep(pen, penSheep); err != nil {
			j.Logf("ovejas: %v", err)
		}
	}
	if _, err := j.Breed(pen); err != nil {
		j.Logf("criar: %v", err)
	}
	return nil
}

// Farm es la tarea de la granja: monta el campo y el corral y los cuida
// (siega, siembra, mete ovejas, las cría y las esquila), guardando la lana.
func Farm() func(j *Job) error {
	return func(j *Job) error {
		if j.r.Store.Base == nil {
			f := j.Feet()
			j.r.Store.SetBase(f)
			j.Logf("almacén: base aquí, en %v", f)
		}
		for round := 1; ; round++ {
			j.Status("granja: vuelta %d", round)
			if err := j.farmRound(); err != nil {
				return err
			}
			if _, err := j.Shear(24); err != nil {
				j.Logf("esquilar: %v", err)
			}
			if err := j.Rest(20 * 60); err != nil { // un minuto entre vueltas
				return err
			}
		}
	}
}

// Beds es la tarea de hacer n camas (y quedárselas).
func Beds(n int) func(j *Job) error {
	if n <= 0 {
		n = 8
	}
	return func(j *Job) error {
		if j.r.Store.Base == nil {
			j.r.Store.SetBase(j.Feet())
		}
		if err := j.EnsureBeds(n); err != nil {
			return err
		}
		j.Logf("tengo %d camas", j.Beds())
		return nil
	}
}

// SheepInPen es cuántas ovejas hay dentro del corral.
func (j *Job) SheepInPen(pen *Pen) int { return len(j.sheepIn(pen)) }
