package automation

import (
	"fmt"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// El Nether: saber en qué dimensión está, usar un portal (el que recuerda,
// uno que vea, o uno que construye) y volver por el mismo.

const (
	overworld = "minecraft:overworld"
	theNether = "minecraft:the_nether"
)

// Dimension es la dimensión en la que está ("minecraft:the_nether").
func (j *Job) Dimension() string { return string(j.r.d.Player.Spawn.Dimension) }

func (j *Job) inNether() bool { return j.Dimension() == theNether }

func isPortal(n string) bool { return n == "nether_portal" }

// portalFoot es la celda de abajo del portal que contiene p (donde se pone uno para cruzarlo).
func (j *Job) portalFoot(p world.BlockPos) world.BlockPos {
	for i := 0; i < 24 && isPortal(j.Block(world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z})); i++ {
		p.Y--
	}
	return p
}

// findPortal es un portal: el que usó en esta dimensión (si sigue ahí) o uno a la vista.
func (j *Job) findPortal() (world.BlockPos, bool) {
	if p, ok := j.r.Store.portal(j.Dimension()); ok {
		if n := j.Block(p); isPortal(n) {
			return p, true
		} else if n == "" { // lejos, sin cargar: ir a ver
			if err := j.GoNear(p); err == nil && isPortal(j.Block(p)) {
				return p, true
			}
		}
	}
	if p, ok := j.findBlock(48, 24, 24, isPortal, nil); ok {
		return j.portalFoot(p), true
	}
	return world.BlockPos{}, false
}

// UsePortal cruza el portal p: se mete, espera a cambiar de dimensión y a
// que cargue el mundo, apunta los dos portales y sale del de llegada.
func (j *Job) UsePortal(p world.BlockPos) error {
	from := j.Dimension()
	p = j.portalFoot(p)
	j.Status("entrando al portal de %v", p)
	if err := j.GoTo(p); err != nil {
		return fmt.Errorf("portal: %w", err)
	}
	j.r.Store.setPortal(from, p)
	w := j.r.d.Walker
	w.Stop()
	for t := 0; j.Dimension() == from; t++ {
		if t > 20*20 {
			return fmt.Errorf("el portal de %v no me lleva", p)
		}
		if err := j.Wait(1); err != nil {
			return err
		}
	}
	j.trail = nil
	j.Logf("crucé el portal: ahora en %s", strings.TrimPrefix(j.Dimension(), "minecraft:"))
	for t := 0; t < 20*20; t++ { // que llegue el mundo de alrededor
		if _, ok := j.r.d.World.BlockAt(world.BlockPos{X: j.Feet().X, Y: j.Feet().Y - 1, Z: j.Feet().Z}); ok && t > 20 {
			break
		}
		if err := j.r.d.Ctl.WaitTicks(j.ctx, 1); err != nil {
			return err
		}
	}
	if q, ok := j.findBlock(2, 2, 3, isPortal, nil); ok {
		j.r.Store.setPortal(j.Dimension(), j.portalFoot(q))
	} else {
		j.r.Store.setPortal(j.Dimension(), j.Feet())
	}
	return j.stepOut()
}

// stepOut sale del portal en el que está (a un par de bloques, a un sitio con suelo).
func (j *Job) stepOut() error {
	f := j.Feet()
	for _, d := range [][2]int{{0, 2}, {0, -2}, {2, 0}, {-2, 0}, {0, 3}, {0, -3}, {3, 0}, {-3, 0}} {
		g, ok := j.r.d.Walker.Ground(f.X+d[0], f.Z+d[1], f.Y)
		if !ok || isPortal(j.Block(g)) || abs(g.Y-f.Y) > 2 {
			continue
		}
		if err := j.GoTo(g); err == nil {
			return nil
		}
	}
	j.Logf("no encuentro por dónde salir del portal: me quedo junto a él")
	return nil
}

// fillerOK: bloques para las esquinas del marco (no se caen, no se usan para nada).
func fillerOK(n string) bool {
	return junk(n) && n != "gravel" && n != "soul_sand" && n != "magma_block" || n == "stone" || n == "deepslate"
}

// ensureFlintAndSteel hace un mechero: un lingote de hierro y un pedernal (de la grava).
func (j *Job) ensureFlintAndSteel() error {
	if j.Count("flint_and_steel") > 0 {
		return nil
	}
	if err := j.ensureIronIngots(1, ""); err != nil {
		return fmt.Errorf("mechero: %w", err)
	}
	if j.Count("flint") == 0 {
		j.Logf("busco grava para sacar pedernal")
		err := j.Mine("grava (pedernal)", func(n string) bool { return n == "gravel" }, func() int { return j.Count("flint") }, 1)
		if serr := j.Surface(); serr != nil {
			j.Logf("%v", serr)
		}
		if err != nil {
			return fmt.Errorf("pedernal: %w", err)
		}
	}
	j.CloseScreen()
	if err := j.craftHere([]string{"iron_ingot", "flint"}, 2, 1); err != nil {
		return fmt.Errorf("mechero: %w", err)
	}
	if j.Count("flint_and_steel") == 0 {
		return fmt.Errorf("mechero: no salió")
	}
	j.Logf("hice un mechero")
	return nil
}

// ensureObsidian junta n de obsidiana (con pico de diamante: abajo, junto a la lava).
func (j *Job) ensureObsidian(n int) error {
	if j.Count("obsidian") >= n {
		return nil
	}
	j.Logf("busco obsidiana (%d)", n-j.Count("obsidian"))
	have := func() int { return j.Count("obsidian") }
	err := j.MineAt("obsidiana", func(b string) bool { return b == "obsidian" }, have, n, diamondLevel, 40)
	if serr := j.Surface(); serr != nil {
		j.Logf("%v", serr)
	}
	return err
}

// BuildPortal construye un portal al Nether junto al bot y lo enciende:
// marco de obsidiana de 4x5 (10 bloques; las 4 esquinas de otra cosa) sobre
// suelo llano, puesto bloque a bloque desde delante. Devuelve una celda del portal.
func (j *Job) BuildPortal() (world.BlockPos, error) {
	if j.Count("obsidian") < 10 || j.Count("flint_and_steel") == 0 {
		return world.BlockPos{}, fmt.Errorf("para el portal hacen falta 10 de obsidiana y un mechero")
	}
	filler := j.most(fillerOK)
	if filler == "" || j.Count(filler) < 4 {
		return world.BlockPos{}, fmt.Errorf("para las esquinas del portal hacen falta 4 bloques (piedra, tierra…)")
	}
	x0, g, z0, stand, ok := j.portalSpot()
	if !ok {
		return world.BlockPos{}, fmt.Errorf("no encuentro un sitio llano para el portal")
	}
	j.Status("construyendo un portal en %d %d %d", x0, g, z0)
	if err := j.GoTo(stand); err != nil {
		return world.BlockPos{}, fmt.Errorf("portal: %w", err)
	}
	at := func(dx, dy int) world.BlockPos { return world.BlockPos{X: x0 + dx, Y: g + dy, Z: z0} }
	type piece struct {
		item         string
		pos, against world.BlockPos
	}
	var plan []piece
	under := func(p world.BlockPos) world.BlockPos { return world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z} }
	for _, dx := range []int{0, 3} {
		plan = append(plan, piece{filler, at(dx, 0), under(at(dx, 0))})
	}
	for _, dx := range []int{1, 2} {
		plan = append(plan, piece{"obsidian", at(dx, 0), under(at(dx, 0))})
	}
	for dy := 1; dy <= 3; dy++ {
		for _, dx := range []int{0, 3} {
			plan = append(plan, piece{"obsidian", at(dx, dy), under(at(dx, dy))})
		}
	}
	for _, dx := range []int{0, 3} {
		plan = append(plan, piece{filler, at(dx, 4), under(at(dx, 4))})
	}
	plan = append(plan, piece{"obsidian", at(1, 4), at(0, 4)}, piece{"obsidian", at(2, 4), at(3, 4)})
	for _, pc := range plan {
		if j.Block(pc.pos) == pc.item {
			continue
		}
		if err := j.PlaceOn(pc.item, pc.pos, pc.against); err != nil {
			return world.BlockPos{}, fmt.Errorf("portal: poner %s en %v: %w", pc.item, pc.pos, err)
		}
	}
	inside := at(1, 1)
	if err := j.PlaceOn("flint_and_steel", inside, at(1, 0)); err != nil && !isPortal(j.Block(inside)) {
		return world.BlockPos{}, fmt.Errorf("encender el portal: %w", err)
	}
	if err := j.Wait(5); err != nil {
		return world.BlockPos{}, err
	}
	if !isPortal(j.Block(inside)) {
		return world.BlockPos{}, fmt.Errorf("el portal no se encendió")
	}
	j.Logf("portal al Nether construido en %v", inside)
	return inside, nil
}

// portalSpot busca dónde cabe el marco (4 de ancho en x, 5 de alto, en el
// plano z0) sobre suelo firme, con sitio delante (z0-1) para ponerlo.
func (j *Job) portalSpot() (x0, g, z0 int, stand world.BlockPos, ok bool) {
	f := j.Feet()
	free := func(p world.BlockPos) bool {
		st, ok := j.r.d.World.BlockAt(p)
		return ok && block.Replaceable(st) && !j.liquid(p)
	}
	for r := 1; r <= 8; r++ {
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				if max(abs(dx), abs(dz)) != r {
					continue
				}
				gr, okg := j.r.d.Walker.Ground(f.X+dx, f.Z+dz, f.Y)
				if !okg {
					continue
				}
				x, y, z := f.X+dx, gr.Y, f.Z+dz
				fits := true
				for i := 0; i < 4 && fits; i++ {
					if !j.solid(world.BlockPos{X: x + i, Y: y - 1, Z: z}) || j.liquid(world.BlockPos{X: x + i, Y: y - 1, Z: z}) {
						fits = false
					}
					for h := 0; h <= 4 && fits; h++ {
						if !free(world.BlockPos{X: x + i, Y: y + h, Z: z}) {
							fits = false
						}
					}
				}
				if !fits {
					continue
				}
				st, oks := j.r.d.Walker.Ground(x+1, z-1, y)
				if !oks || st.Y != y {
					continue
				}
				return x, y, z, st, true
			}
		}
	}
	return 0, 0, 0, world.BlockPos{}, false
}

// goToNether llega al Nether: por un portal conocido o a la vista; si no hay,
// construye uno (obsidiana, mechero) junto a la base.
func (j *Job) goToNether() error {
	if j.inNether() {
		return nil
	}
	p, ok := j.findPortal()
	if !ok {
		j.Logf("no conozco ningún portal al Nether: construyo uno")
		if err := j.ensureObsidian(10); err != nil {
			return fmt.Errorf("portal: %w (dame 10 de obsidiana, o llévame junto a un portal)", err)
		}
		if err := j.ensureFlintAndSteel(); err != nil {
			return err
		}
		if base := j.r.Store.Base; base != nil {
			if err := j.GoNear(*base); err != nil {
				j.Logf("base: %v (lo construyo aquí)", err)
			}
		}
		var err error
		if p, err = j.BuildPortal(); err != nil {
			return err
		}
	}
	return j.UsePortal(p)
}

// leaveNether vuelve al mundo normal por el portal por el que llegó (o uno a la vista).
func (j *Job) leaveNether() error {
	if !j.inNether() {
		return nil
	}
	p, ok := j.r.Store.portal(theNether)
	if !ok {
		if q, found := j.findBlock(48, 24, 24, isPortal, nil); found {
			p, ok = j.portalFoot(q), true
		}
	}
	if !ok {
		return fmt.Errorf("no sé dónde está el portal para volver")
	}
	j.Status("volviendo al portal de %v", p)
	if err := j.GoNear(p); err != nil {
		j.Problem("al portal: %v (vuelvo por el túnel)", err)
		if serr := j.Surface(); serr != nil {
			j.Logf("%v", serr)
		}
		if err := j.GoNear(p); err != nil {
			return fmt.Errorf("no llego al portal de %v: %w", p, err)
		}
	}
	return j.UsePortal(p)
}
