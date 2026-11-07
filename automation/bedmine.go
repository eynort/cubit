package automation

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/world"
)

// Minería con camas: en el Nether una cama que se usa explota (potencia 5,
// más que una TNT) y abre un cráter de unos 3-4 bloques de radio que deja a
// la vista los escombros ancestrales (resisten la explosión: 1200).
//
// La postura sale de la fórmula del juego (ServerExplosion.getSeenPercent y
// el daño ((i²+i)/2)·7·10+1, con i = (1-distancia/10)·lo que se ve de la
// caja del jugador): con el centro de la explosión tapado del todo, el daño
// es 1 (medio corazón, el mínimo del juego), haga lo que haga la explosión
// luego con los bloques. El centro es el de la CABECERA de la cama (aunque
// se pulse los pies), así que la cabecera va escondida y los pies a la vista:
//
//	vista de lado (n hacia la derecha)          vista desde arriba (y = -1, la cama)
//	  y=1  E . #  pasillo                         n →
//	  y=0  E O1 O2 P   (pasillo: z=2)             f ↓   E   O1  ·   P(pasillo)
//	  y=-1 # H  F                                       H   F
//
// Con E el sitio desde donde se pulsa, f la dirección del pasillo y n el
// lado del nicho (del pasillo hacia E es -n):
//
//	P  = E + f + 2n   (el final del pasillo: desde ahí se cava y se pone la cama)
//	O1 = E + n        (hueco de 1 de alto entre E y el pasillo: por él se ve)
//	O2 = E + f + n    (encima de los pies de la cama)
//	F  = E + f + n - y (los pies),  H = E + f - y (la cabecera, bajo un bloque y
//	                   junto al suelo de E: desde E no se ve ningún punto suyo)
//
// La cama se pone desde P mirando hacia -n (la cabecera queda hacia donde
// mira el jugador: en H) y se pulsa desde E, en el punto de los pies que se
// ve por O1/O2 (no en el centro de la cara: ese rayo roza una esquina).
// Probado con la fórmula: daño 1 con el cuerpo a ±0.2 del centro de E en
// la dirección f (a 0.3, pegado a la pared, ya entra algo: hasta 3.3).
//
// Lo que pasa después también es del juego: el suelo de E salta, el bot cae
// 2-3 bloques al cráter (sin daño) y en un tercio de las celdas despejadas
// aparece fuego; el reflejo (safety.Lava) lo saca de ahí antes de que prenda
// (20 ticks dentro del fuego). La lava que destape el cráter: el mismo reflejo.

// bedShot es la geometría de un disparo, en coordenadas del mundo.
type bedShot struct {
	f, n           [2]int // las direcciones (x, z)
	E, P, O1, O2   world.BlockPos
	F, H           world.BlockPos
	clickX, clickZ float64 // el punto de los pies que se pulsa (y: la parte de arriba de la cama)
}

func off(p world.BlockPos, a, up, b int, f, n [2]int) world.BlockPos {
	return world.BlockPos{X: p.X + a*f[0] + b*n[0], Y: p.Y + up, Z: p.Z + a*f[1] + b*n[1]}
}

// shotFrom es la geometría con P = la celda de los pies, el pasillo hacia f
// y el nicho hacia el lado -n.
func shotFrom(P world.BlockPos, f, n [2]int) bedShot {
	E := off(P, -1, 0, -2, f, n)
	s := bedShot{f: f, n: n, E: E, P: P,
		O1: off(E, 0, 0, 1, f, n), O2: off(E, 1, 0, 1, f, n),
		F: off(E, 1, -1, 1, f, n), H: off(E, 1, -1, 0, f, n)}
	// el punto: el centro de los pies, 0.2 hacia n (hacia el pasillo)
	s.clickX = float64(s.F.X) + 0.5 + 0.2*float64(n[0])
	s.clickZ = float64(s.F.Z) + 0.5 + 0.2*float64(n[1])
	return s
}

func isBed(n string) bool { return strings.HasSuffix(n, "_bed") }

// Beds es cuántas camas lleva.
func (j *Job) Beds() int { return j.CountWhere(isBed) }

// shotFits dice si alrededor de E todo es macizo (como lo supone la cuenta) y
// no hay lava cerca (el cráter la soltaría encima).
func (j *Job) shotFits(s bedShot) error {
	for a := -1; a <= 2; a++ {
		for up := -2; up <= 2; up++ {
			for b := -1; b <= 1; b++ {
				c := off(s.E, a, up, b, s.f, s.n)
				if !j.solid(c) || j.liquid(c) {
					return fmt.Errorf("hueco o líquido en %v", c)
				}
			}
		}
	}
	for dx := -5; dx <= 5; dx++ {
		for dy := -4; dy <= 4; dy++ {
			for dz := -5; dz <= 5; dz++ {
				c := world.BlockPos{X: s.H.X + dx, Y: s.H.Y + dy, Z: s.H.Z + dz}
				if strings.HasPrefix(j.Block(c), "lava") {
					return fmt.Errorf("lava cerca (%v)", c)
				}
			}
		}
	}
	return nil
}

// BedShot hace un disparo desde donde está (al final de un pasillo que va
// hacia f): cava el nicho, pone la cama, se coloca y la pulsa. Devuelve
// errNoShot si el sitio no vale (hay que avanzar).
var errNoShot = errors.New("aquí no se puede poner la cama")

func (j *Job) BedShot(f [2]int) error {
	_, err := j.bedShot(f)
	return err
}

func (j *Job) bedShot(f [2]int) (bedShot, error) {
	P := j.Feet()
	var s bedShot
	var why error
	ok := false
	for _, n := range [][2]int{{-f[1], f[0]}, {f[1], -f[0]}} { // el nicho a un lado o al otro
		s = shotFrom(P, f, n)
		if why = j.shotFits(s); why == nil {
			ok = true
			break
		}
	}
	if !ok {
		return s, fmt.Errorf("%w: %v", errNoShot, why)
	}
	if j.Beds() == 0 {
		return s, errors.New("no me quedan camas")
	}
	j.Status("preparando la cama (cráter en %v)", s.H)
	// 1. cavar: el paso a E (dos de alto de momento), E, encima de los pies, los pies y la cabecera
	for _, c := range []world.BlockPos{off(s.P, -1, 1, 0, s.f, s.n), off(s.P, -1, 0, 0, s.f, s.n), off(s.O1, 0, 1, 0, s.f, s.n), s.O1, off(s.E, 0, 1, 0, s.f, s.n), s.E, s.O2, s.F, s.H} {
		if !j.safeToOpen(c) {
			return s, fmt.Errorf("%w: líquido junto a %v", errNoShot, c)
		}
		if err := j.clearCell(c); err != nil {
			return s, err
		}
	}
	// 2. la cama: desde P mirando hacia -n (la cabecera cae en H)
	if err := j.GoTo(s.P); err != nil {
		return s, err
	}
	bed := j.most(isBed)
	if err := j.PlaceOn(bed, s.F, world.BlockPos{X: s.F.X, Y: s.F.Y - 1, Z: s.F.Z}); err != nil {
		return s, fmt.Errorf("poner la cama en %v (cabecera %v: %s/%s, desde %v): %w", s.F, s.H, j.Block(s.F), j.Block(s.H), j.Pos(), err)
	}
	if st, _ := j.r.d.World.BlockAt(s.H); !isBed(blockName(st)) {
		// salió girada (la cabecera no está escondida): se quita, no se dispara así
		j.Logf("la cama no quedó con la cabecera en %v: la quito", s.H)
		_ = j.Dig(s.F)
		_ = j.Collect(4)
		return s, fmt.Errorf("%w: la cama salió girada", errNoShot)
	}
	// 3. a E, y tapar la cabeza del paso (O1 se queda de 1 de alto: por ahí se ve)
	if err := j.GoTo(s.E); err != nil {
		return s, err
	}
	if err := j.PlaceOn(j.fillBlock(), off(s.O1, 0, 1, 0, s.f, s.n), off(s.O1, 0, 2, 0, s.f, s.n)); err != nil {
		return s, fmt.Errorf("tapar el paso: %w", err)
	}
	// 4. bien en medio de E en la dirección f (pegado a la pared entraría algo de explosión)
	pos := j.Pos()
	along := (pos.X-float64(s.E.X)-0.5)*float64(s.f[0]) + (pos.Z-float64(s.E.Z)-0.5)*float64(s.f[1])
	if math.Abs(along) > 0.18 || j.Feet() != s.E {
		return s, fmt.Errorf("no estoy en medio del nicho (desvío %.2f): no pulso", along)
	}
	// 5. pulsar: con el pico en la mano (nada que poner), en el punto de los pies que se ve
	if p := j.best("pickaxe"); p != "" {
		_ = j.Hold(p)
	}
	j.Logf("¡cama! (cabecera en %v)", s.H)
	cursor := [3]float32{float32(s.clickX - float64(s.F.X)), 0.5625, float32(s.clickZ - float64(s.F.Z))}
	done := make(chan act.Use, 1)
	j.r.d.Hands.UseBlockAt(s.F, act.Up, cursor, func(u act.Use) { done <- u })
	select {
	case u := <-done:
		if u.Err != nil {
			return s, fmt.Errorf("pulsar la cama: %w", u.Err)
		}
	case <-time.After(10 * time.Second):
		return s, errors.New("pulsar la cama: el servidor no contesta")
	case <-j.ctx.Done():
		return s, j.ctx.Err()
	}
	// la caída y el fuego: el reflejo; aquí, esperar a estar quieto y a salvo
	for t := 0; t < 40; t++ {
		if err := j.Wait(2); err != nil {
			return s, err
		}
		if j.r.d.Ctl.State().OnGround && (j.r.d.Escaping == nil || !j.r.d.Escaping()) && t > 5 {
			break
		}
	}
	return s, nil
}

func isFire(n string) bool { return n == "fire" || n == "soul_fire" }

// putOutFires apaga a golpes el fuego al alcance (se rompe al instante): el
// que deja la explosión en el cráter, que en el netherrack no se apaga solo.
func (j *Job) putOutFires() error {
	skip := map[world.BlockPos]bool{}
	for n := 0; n < 40; n++ {
		p, ok := j.findBlock(5, 4, 4, isFire, skip)
		if !ok {
			return nil
		}
		skip[p] = true
		if !j.Reaches(p) {
			continue
		}
		if err := j.Dig(p); err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
		}
	}
	return nil
}

// fillBlock es un bloque de relleno que lleva (netherrack, adoquines…).
func (j *Job) fillBlock() string {
	if b := j.most(func(n string) bool { return junk(n) && n != "gravel" && n != "magma_block" && n != "soul_sand" }); b != "" {
		return b
	}
	return "netherrack"
}

// BedMine mina escombros con camas a la altura en la que esté: avanza un
// pasillo hacia f, dispara una cama donde quepa, recoge los escombros que
// se vean y sigue, hasta tener want o quedarse sin camas.
func (j *Job) BedMine(have func() int, want int) error {
	f := [2]int{1, 0}
	shots, fails := 0, 0
	var last *bedShot
	for have() < want && j.Beds() > 0 && fails < 12 {
		if err := j.Wait(1); err != nil {
			return err
		}
		j.makeRoom()
		j.Status("camas: %d escombros de %d (quedan %d camas)", have(), want, j.Beds())
		// a roca sin tocar: tras un disparo, un túnel desde el cráter hasta 10
		// más allá de la cabecera; luego un pasillo de 1x2 hacia delante
		if last != nil {
			to := off(last.H, 10, 1, 0, f, last.n)
			j.Status("saliendo del cráter hacia %v", to)
			if err := j.putOutFires(); err != nil {
				return err
			}
			if err := j.TunnelTo(to); err != nil {
				if j.Fatal() != nil {
					return j.Fatal()
				}
				j.Logf("salir del cráter: %v", err)
			}
			last = nil
		}
		if err := j.stripRun(f[0], f[1], 2, func(string) bool { return false }, nil); err != nil {
			j.Logf("pasillo: %v (giro)", err)
			f = [2]int{-f[1], f[0]}
			fails++
			continue
		}
		shot, err := j.bedShot(f)
		switch {
		case errors.Is(err, errNoShot):
			j.Logf("%v: sigo adelante", err)
			fails++
			continue
		case err != nil:
			if j.Fatal() != nil {
				return j.Fatal()
			}
			j.Logf("disparo: %v", err)
			fails++
			continue
		}
		shots, fails, last = shots+1, 0, &shot
		// los escombros a la vista del cráter (apagando antes el fuego que haya cerca: rozarlo quema)
		skip := map[world.BlockPos]bool{}
		for k := 0; k < 8 && have() < want; k++ {
			if err := j.putOutFires(); err != nil {
				return err
			}
			p, found := j.findBlock(8, 6, 6, isDebris, skip)
			if !found {
				break
			}
			if err := j.TunnelTo(p); err != nil {
				if j.Fatal() != nil {
					return j.Fatal()
				}
				skip[p] = true
				continue
			}
			j.digAround(isDebris, skip, func() bool { return have() >= want })
			if isDebris(j.Block(p)) {
				skip[p] = true
			}
		}
		if err := j.putOutFires(); err != nil {
			return err
		}
		if err := j.Collect(8); err != nil {
			return err
		}
		if err := j.putOutFires(); err != nil {
			return err
		}
		j.Logf("disparo %d: llevo %d escombros", shots, have())
	}
	if have() < want {
		return fmt.Errorf("con camas: %d de %d (camas que quedan: %d)", have(), want, j.Beds())
	}
	return nil
}
