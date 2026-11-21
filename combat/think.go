package combat

import (
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26/data/entitydata"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

// thinkState es lo que Think recuerda entre ticks (solo lo toca Think).
type thinkState struct {
	controlling bool // las teclas son nuestras (no del walker)
	blocking    bool // escudo levantado
	blockSince  uint64
	strafeDir   int // -1 izquierda, 0 nada, 1 derecha
	strafeTicks int
	backoff     int        // ticks que quedan retrocediendo (golpe y atrás)
	fromCreeper bool       // tras pegarle a un creeper: correr hasta estar a más de 7 (se le apaga la mecha)
	wtap        int        // ticks sin correr tras un golpe (reinicia el sprint)
	jumpedAt    uint64     // tick del último salto para crítico
	drawnAt     uint64     // tick en que se vio por última vez al tirador tensando (la flecha llega después)
	seenID      int32      // el objetivo cuyo movimiento se sigue (para apuntar adelantado)
	seenPos     [2]float64 // dónde estaba
	seenTick    uint64     // cuándo
	seenVel     [2]float64 // su velocidad (bloques por tick), suavizada
	myVel       [2]float64 // la del bot (para lo que se acercan)
	enemyHit    uint64     // tick del último golpe del objetivo (su brazo; la ventana de castigo)
	drawing     bool       // tensando el arco (bow.go)
	drawFrom    uint64     // desde qué tick
	blockFrom   uint64     // desde cuándo tiene el escudo arriba sin que pase nada
	charging    bool       // embistiendo con la lanza (el uso mantenido; arsenal.go)
	chargeFrom  uint64     // desde qué tick
	peakY       float64    // lo más alto desde que dejó el suelo (la caída de la maza)
	comboUntil  uint64     // encadenando empujones hasta este tick (cada golpe con sprint, sin saltar)
	waitFrom    uint64     // desde cuándo espera en el borde de su alcance a que entre él
	lastPos     basic.Position
}

// Think es el Think del controlador: cada tick decide teclas, mirada y golpes.
// Fuera del cuerpo a cuerpo le deja el control al walker.
func (f *Fighter) Think(s *control.State, bp *basic.Player) {
	if f.offhandSwapped.Swap(false) {
		f.t.blocking = false // (cambiar la mano izquierda baja el escudo)
	}
	// la física: con el escudo arriba o comiendo, despacio y sin correr
	defer func() {
		_, _, eating := f.d.Hands.Using()
		s.UsingItem = f.t.blocking || eating || f.t.drawing
	}()
	pl := f.plan.Load()
	if s.OnGround {
		f.t.peakY = bp.Position().Y
	} else {
		f.t.peakY = math.Max(f.t.peakY, bp.Position().Y)
	}
	if f.t.charging && (!pl.lunge || pl.mode != fighting) { // (la embestida acabó: se suelta la lanza)
		f.endLunge(s.Ticks)
	}
	if f.t.drawing && !pl.bow { // (otra cosa: el arco se suelta, si la flecha llega; si no, sin disparar)
		e, ok := f.d.Entities.Get(f.t.seenID)
		if ok && s.Ticks-f.t.drawFrom >= bowFull && f.arrowPathClear(bp.Position(), e, f.t.seenVel[0], f.t.seenVel[1], f.bowSpeed()) {
			f.stopDraw()
			f.shots.Add(1)
		} else {
			f.dropDraw()
		}
	}
	// todo lo que es ir a un sitio lo hace el pathfinding: perseguir de lejos y huir
	if pl.mode == idle || pl.mode == retreating || (pl.mode == fighting && !pl.closeIn) {
		if f.t.controlling { // soltar las teclas que dejamos puestas
			s.Input = control.Input{}
			f.t.controlling = false
		}
		f.d.Walker.Think(s, bp)
		// persiguiendo a un tirador por un camino con vueltas: el escudo arriba
		// mientras tensa el arco (el walker lleva la mirada: va hacia él)
		if e, ok := f.d.Entities.Get(pl.target); pl.mode == fighting && pl.shield && ok && f.shotAt(e, bp.Position(), s.Ticks) {
			f.raiseShield(s.Ticks, bp.Position())
		} else {
			f.lowerShield()
		}
		return
	}
	f.t.controlling = true
	switch pl.mode {
	case eating:
		f.lowerShield()
		s.Input = control.Input{}
	case fighting:
		if e, ok := f.d.Entities.Get(pl.target); ok && pl.bow {
			f.thinkBow(s, bp, pl, e)
			return
		}
		f.thinkFight(s, bp, pl)
	case wary:
		f.thinkWary(s, bp, pl)
	case sheltering:
		f.thinkShelter(s, bp, pl)
	case acting: // (las manos miran solas a lo suyo)
		if pl.fly > 0 { // con élitros (expert.go)
			f.thinkFly(s, bp, pl)
			return
		}
		f.lowerShield()
		s.Input = control.Input{}
		if pl.steer {
			p := bp.Position()
			f.turnTo(bp, float32(-math.Atan2(pl.steerX-p.X, pl.steerZ-p.Z)*180/math.Pi), p.Pitch, 60, 20)
			in := control.Input{Forward: true}
			if s.HorizontalCollision && s.OnGround {
				in.Jump = true
			}
			s.Input = in
			return
		}
		f.keepAt(s, bp, pl)
	}
}

func (f *Fighter) thinkFight(s *control.State, bp *basic.Player, pl *plan) {
	e, ok := f.d.Entities.Get(pl.target)
	if !ok {
		f.lowerShield()
		s.Input = control.Input{}
		return
	}
	pos := bp.Position()
	phys := f.d.Mover.Player()
	tick := s.Ticks
	if f.attackReset.Swap(false) {
		f.lastAttack.Store(tick)
	}
	delay := 20 / pl.weapon.speed
	charge := float64(tick-f.lastAttack.Load()) / delay
	reach := pl.weapon.hitReach() // (la lanza llega a 4,5)
	if time.Since(time.UnixMilli(f.lastHurt.Load())) < 300*time.Millisecond {
		f.t.comboUntil = 0 // (le pegó: el combo se rompió)
	}
	// dónde está de verdad: lo que se ve llegó con retraso, y el golpe tarda en
	// llegar al servidor, que lo mide con su posición. Se apunta y se mide el
	// alcance con la posición prevista (unos 3 ticks por delante)
	vx, vz := f.track(e, tick)
	if phys != nil {
		f.t.myVel = [2]float64{phys.Vel.X, phys.Vel.Z}
	}
	ahead := e
	ahead.X, ahead.Z = e.X+vx*aimLead, e.Z+vz*aimLead
	flat := math.Hypot(ahead.X-pos.X, ahead.Z-pos.Z)
	dist := boxDistance(pos, ahead)
	f.aim(bp, ahead.X, e.Y+1.1, ahead.Z)
	if pl.lunge || f.t.charging { // la embestida con la lanza (arsenal.go)
		f.thinkLunge(s, bp, pl, e, flat)
		return
	}
	// en el aire con la maza (arsenal.go): hacia él, y el golpe en cuanto esté
	// a tiro cayendo (la caída suma, no la carga)
	if pl.smash && phys != nil && !s.OnGround {
		f.lowerShield()
		in := control.Input{Forward: flat > 0.5}
		s.Input = in
		// (también si a tiro estará el tick que viene: cayendo a más de un
		// bloque por tick, la ventana es de uno o dos ticks; y el golpe va ya,
		// sin la cola de las manos)
		next := pos
		next.X, next.Y, next.Z = pos.X+phys.Vel.X, pos.Y+phys.Vel.Y, pos.Z+phys.Vel.Z
		if phys.Vel.Y < 0 && f.t.peakY-pos.Y > 1.5 && (dist <= reach || boxDistance(next, ahead) <= reach) && tick != f.lastAttack.Load() {
			a := play.Attack{EntityID: pk.VarInt(pl.target)}
			if f.d.Client.Conn.WritePacket(pk.Marshal(a.PacketID(), a)) == nil {
				sw := play.Swing{Hand: types.InteractionHandMainHand}
				_ = f.d.Client.Conn.WritePacket(pk.Marshal(sw.PacketID(), sw))
			}
			f.lastAttack.Store(tick + 1)
		}
		return
	}

	// un cristal suyo cerca: el escudo de cara a él (la explosión de frente
	// no entra) y un paso atrás, sin pegar (expert.go)
	if pl.guard && pl.shield && !f.shieldOff(tick) {
		f.aim(bp, pl.gx, pl.gy, pl.gz)
		f.raiseShield(tick, pos)
		in := control.Input{Backward: true}
		if pl.dodge { // (un mazazo que cae: de lado, fuera de donde cae, sin dejar de mirarle)
			in = control.Input{Left: f.t.strafeDir <= 0, Right: f.t.strafeDir > 0}
		}
		f.terrain(s, bp, &in)
		s.Input = in
		return
	}
	// un creeper (vanilla: mecha a menos de 3 bloques, 30 ticks; se apaga a
	// más de 7): con la mecha encendida, o cerca sin el golpe listo, correr
	// lejos de él; con la mecha apagada y el golpe cargado, entrar, pegar y
	// atrás (el empujón lo aleja)
	creeper := e.Type == "minecraft:creeper"
	if creeper && f.t.fromCreeper && flat > 7.5 {
		f.t.fromCreeper = false
	}
	// con escudo, al creeper encendido se le planta cara con el escudo
	// arriba: la explosión no hace nada (huir pocas veces llega: la mecha son
	// 30 ticks y con el retraso del servidor se ve tarde). Sin encender y sin
	// el golpe listo, un paso atrás (a menos de 3 se enciende)
	if creeper && pl.shield {
		lit := creeperLit(e)
		if lit && flat < 7 || f.t.fromCreeper && flat < 4 {
			f.raiseShield(tick, pos)
			in := control.Input{}
			if flat < 2 {
				in.Backward = true // (pegado: algo de sitio, sin darle la espalda)
			}
			f.terrain(s, bp, &in)
			s.Input = in
			return
		}
		f.lowerShield()
		if flat < 3.3 && charge < 0.9 {
			in := control.Input{Backward: true}
			f.terrain(s, bp, &in)
			s.Input = in
			return
		}
		f.t.fromCreeper = false
	} else if creeper && (f.t.fromCreeper || creeperLit(e) || flat < 3.3 && charge < 0.9) {
		f.lowerShield()
		away := float32(-math.Atan2(pos.X-e.X, pos.Z-e.Z) * 180 / math.Pi)
		f.turnTo(bp, away, 0, 70, 20)
		in := control.Input{Forward: true, Sprint: true}
		f.terrain(s, bp, &in)
		if s.HorizontalCollision && s.OnGround {
			in.Jump = true
		}
		s.Input = in
		return
	}
	if f.t.backoff > 0 {
		f.t.backoff--
		f.lowerShield()
		in := control.Input{Backward: true}
		f.terrain(s, bp, &in) // sin caer por un borde al retroceder
		s.Input = in
		return
	}

	in := control.Input{}
	melee := !rangedMobs[kind(e)]
	// bordes: el empujón de su golpe va en la dirección contraria a él. Con un
	// precipicio ahí (edgeBehind) no se retrocede ni se salta (en el aire el
	// empujón lleva más lejos): de lado y hacia él, hasta tener el vacío a un
	// lado. Con el precipicio detrás de él (edgeAhead), golpe con sprint, que
	// es el que más empuja
	edgeBehind, edgeAhead := false, false
	if flat > 0.1 {
		ax, az := (pos.X-ahead.X)/flat, (pos.Z-ahead.Z)/flat
		edgeBehind = f.edgeWithin(pos, ax, az, knockbackReach)
		edgeAhead = !f.d.Walker.Safe(basic.Position{X: ahead.X, Y: e.Y, Z: ahead.Z}, -ax, -az)
	}
	// la distancia (el "spacing" del PvP): con el golpe cargado, a por él; mientras
	// recarga, contra uno que pega de cerca, fuera de su alcance (sin escudo:
	// retrocediendo; con escudo, quieto tras él). Así el que pega primero es el bot
	// con escudo, a uno que pega de cerca y viene: no se le sale al paso; se le
	// espera con el escudo arriba (su golpe da en el escudo y le echa atrás) y
	// se le pega cuando entra a tiro con el golpe cargado
	// (escoger cuándo pegar: con escudo, o sin él con el golpe cargado, a uno
	// que viene no se le sale al paso: se le espera en el borde del alcance y
	// pega el bot al entrar él; como mucho 15 ticks, que no se quede esperando
	// a uno que también espera)
	closingIn := f.closingSpeed(pos, ahead, vx, vz)
	if closingIn <= 0.08 || dist <= reach {
		f.t.waitFrom = 0
	} else if f.t.waitFrom == 0 {
		f.t.waitFrom = tick
	}
	comes := melee && (pl.shield || charge >= 0.95 && tick-f.t.waitFrom < 15) && closingIn > 0.08 && flat < 8
	switch {
	case edgeBehind:
		in.Forward = flat > 1.6 // hacia él (lejos del borde), sin pegarse
		if s, ok := f.sideAwayFromEdge(bp, pos); ok {
			f.t.strafeDir, f.t.strafeTicks = s, 10 // y de lado, hacia donde hay suelo
		}
	case comes && dist > reach:
		// quieto, de cara
	case charge >= 0.9 && flat > 2.7:
		in.Forward, in.Sprint = true, true
	case charge < 0.9 && melee && !pl.shield && flat < spacing(pl.stance):
		in.Backward = true
	case charge < 0.9 && melee && pl.shield && flat > 4:
		in.Forward = true // (acercarse despacio, sin entrar a su alcance)
	case flat < 1.6:
		in.Backward = true
	}
	// W-tap: soltar W un par de ticks tras golpear para que el próximo golpe empuje más
	if f.t.wtap > 0 {
		f.t.wtap--
		in.Forward, in.Sprint = false, false
	}
	// su golpe al aire (fuera de alcance): le falta la recarga entera. Sin
	// escudo, a por él ya (con escudo lo hace la ventana de castigo)
	if !pl.shield && melee && !pl.weapon.stab && tick-f.t.enemyHit <= 8 && dist > reach && charge >= 0.8 && !edgeBehind {
		in.Forward, in.Sprint, in.Backward = true, true, false
	}
	// encadenando empujones: tras un golpe con sprint él sale despedido y
	// tarda en volver; a por él corriendo para el siguiente (también con
	// sprint) antes de que se recupere. Sin saltar (el crítico no empuja)
	combo := tick < f.t.comboUntil && melee && !pl.weapon.stab && !edgeBehind
	if combo && f.t.wtap == 0 {
		in.Forward, in.Sprint, in.Backward = flat > 1.2, flat > 1.2, false
	}
	// strafe: moverse de lado como una persona, cambiando a ratos
	if f.t.strafeTicks--; f.t.strafeTicks <= 0 {
		f.t.strafeDir = []int{-1, -1, 0, 1, 1}[rand.IntN(5)]
		f.t.strafeTicks = 8 + rand.IntN(18)
	}
	in.Left, in.Right = f.t.strafeDir < 0, f.t.strafeDir > 0
	// la lanza: se le pincha desde donde no llega (de 3,5 a 4,3) y mientras
	// recarga se sale de su alcance; más cerca de 2 la estocada no da
	if pl.weapon.stab && melee && !edgeBehind {
		in.Forward, in.Backward, in.Sprint = false, false, false
		in.Left, in.Right = false, false // (de lado se le da la vuelta y se entra en su alcance)
		switch {
		case dist < pl.weapon.minReach+0.3:
			in.Backward = true
		case charge >= 0.95 && dist > reach-0.3:
			in.Forward, in.Sprint = true, flat > 6
		case charge < 0.95 && flat < theirReach+0.6:
			in.Backward = true
		}
	}
	// contra su lanza: no da a menos de 2 bloques. Pegado a él (de lejos es
	// él el que pincha sin que se le llegue)
	if melee && !pl.weapon.stab && !edgeBehind && weaponOf(screen.Slot(e.Equipment[0])).stab {
		in.Backward = false
		in.Forward, in.Sprint = flat > 1.3, flat > 3
	}

	// barrido: con espada y dos o más pegados a él, el golpe en el suelo y sin
	// sprint les da a todos (y los aparta): contra un grupo vale más que el
	// crítico en uno solo
	sweep := false
	if inv, held := f.inventory(); strings.HasSuffix(nameOf(inv[slotHotbar+held]), "_sword") {
		others := 0
		for _, o := range f.d.Entities.Nearby(e.X, e.Y, e.Z, 2) {
			if o.ID != e.ID && hostileMobs[kind(o)] && !dead(o) {
				others++
			}
		}
		sweep = others >= 2
	}
	if sweep && charge >= 0.8 {
		in.Sprint = false
	}
	water := phys != nil && (phys.InWater || phys.InLava)
	// a la defensiva (pierde el intercambio, con escudo): sin saltar. En el
	// aire el escudo está abajo y el empujón lleva lejos: en los logs, los dos
	// golpes de 6 de un full diamante entraron así, saltando para el crítico.
	// Tapado siempre, y se pega en su ventana (tras su golpe), en el suelo
	defensive := pl.shield && !f.shieldOff(tick) && (pl.stance == stanceCautious || pl.stance == stanceAvoid) && !creeper
	canCrit := phys != nil && !water && !defensive && !pl.noCrit && !creeper && !edgeBehind && !edgeAhead && !sweep && !pl.weapon.stab && !combo // (creeper: golpe ya, la mecha corre; bordes: en el suelo y con sprint; barrido: en el suelo)
	if edgeAhead && charge >= 0.8 {
		in.Sprint = true // el golpe con sprint empuja un nivel más: al vacío
	}
	onGround := s.OnGround
	// crítico (×1,5: cargado, cayendo, sin correr): el salto sube 6 ticks y
	// cae otros 6; se salta 7 ticks antes de tener el golpe cargado, para ir
	// cayendo justo cuando lo esté (y él a tiro). Es lo que gana un intercambio
	// de golpes entre iguales: 9 contra 6. En el suelo, cargado y a tiro sin
	// haber saltado: se pega ya (esperar es comerse otro golpe)
	rising := false
	untilCharged := (1 - charge) * delay // ticks
	if onGround && charge >= 0.9 && dist <= reach {
		canCrit = false
	}
	// el salto, cuando caiga a la vez que se carga el golpe Y que él entre a
	// tiro: se cuenta lo que se acercan los dos (closing, bloques por tick)
	closing := math.Max(0, -((ahead.X-pos.X)*(vx-f.t.myVel[0])+(ahead.Z-pos.Z)*(vz-f.t.myVel[1]))/math.Max(flat, 0.1))
	inReachIn := math.Inf(1) // ticks hasta que esté a tiro
	if dist <= reach {
		inReachIn = 0
	} else if closing > 0.05 {
		inReachIn = (dist - reach) / closing
	}
	if when := math.Max(untilCharged, inReachIn); canCrit && onGround && when <= critLead && when >= critLead-3 {
		in.Jump = true
		f.t.jumpedAt = tick
	}
	f.terrain(s, bp, &in)

	// escudo: arriba todo el rato que el golpe recarga (el ritmo del PvP con
	// escudo: tapar, bajar, pegar, tapar) si el rival está cerca y mirando;
	// contra un tirador que tensa el arco, también acercándose (despacio). Un
	// rival con hacha lo desactiva 5 s de un golpe: contra él, mejor no
	shooting := pl.shield && f.shotAt(e, pos, tick)
	axe := holdsAxe(e)
	// la ventana de castigo: tras su golpe le faltan ~13 ticks para el
	// siguiente; pegar en los 6 primeros deja tiempo para volver a subir el
	// escudo (5 ticks). Fuera de ella, con escudo, se le espera tapado (si en
	// 25 ticks no pega, escudo contra escudo, se le pega igual)
	if id := f.enemySwing.Swap(0); id == pl.target {
		f.t.enemyHit = tick
	}
	// (perdiendo el intercambio, solo en su ventana: escudo contra escudo, se espera)
	punish := tick-f.t.enemyHit <= 6 || f.t.blocking && tick-f.t.blockFrom > 25 && pl.stance != stanceCautious && pl.stance != stanceAvoid
	if defensive {
		punish = f.safeOpening(e, pos, tick, pl)
	}
	wantBlock := pl.shield && !f.shieldOff(tick) && ((charge < 0.9 || comes && dist > reach || melee && dist <= reach && !punish || defensive && !punish) && dist < 7 && (facing(e, pos) || edgeBehind) && !axe || shooting && dist > reach)
	if wantBlock {
		f.raiseShield(tick, pos)
	} else {
		f.lowerShield()
	}
	if f.t.blocking { // con el escudo arriba: despacio, sin correr ni saltar
		in.Sprint, in.Jump = false, false
	}
	// jump reset: al recibir un golpe de cerca en el suelo, saltar ese mismo
	// tick (el empujón se lleva menos: el salto se come parte de él)
	// (esprintando hacia él: el impulso del salto con sprint se come el
	// empujón). Con el retraso del servidor el aviso llega tarde: solo si ese
	// salto sirve también para el crítico (caer cuando el golpe esté cargado);
	// si no, descuadra el crítico y no quita empujón
	if f.hurtMelee.Swap(false) && s.OnGround && !f.t.blocking && !edgeBehind && !defensive && untilCharged >= critLead-1 && untilCharged <= critLead+1 {
		in.Jump, in.Forward, in.Sprint, in.Backward = true, true, true, false
		f.t.jumpedAt = tick
	}
	// en el aire con el golpe a punto: sin sprint (con sprint no hay crítico)
	if !onGround && charge >= 0.8 && canCrit {
		in.Sprint, in.Forward = false, false
	}

	// golpe
	falling := phys != nil && !phys.OnGround && phys.Vel.Y < -0.08 && !phys.Sprinting
	jumpFailed := tick-f.t.jumpedAt > 12 // techo bajo o no pudo saltar
	// su invulnerabilidad: otra cosa (lava, una flecha, otro jugador) le dio
	// hace menos de 10 ticks: un golpe ahora solo cuenta lo que pase de aquel.
	// Se espera a que acabe (el golpe sigue cargado)
	iframes := f.enemyHurt.Load() == pl.target && time.Since(time.Unix(0, f.enemyHurtAt.Load())) < 420*time.Millisecond &&
		f.enemyHurtAt.Load()-f.ourHitAt.Load() > int64(350*time.Millisecond)
	if charge >= 1 && dist <= reach && !f.d.Hands.Busy() && !f.t.blocking && !pl.holdFire && !iframes &&
		(!canCrit || falling || jumpFailed || onGround || rising) && (!pl.weapon.stab || dist >= pl.weapon.minReach) {
		id := pl.target
		f.ourHitAt.Store(time.Now().UnixNano())
		// con sprint y sin ir ganando de calle: a encadenar (un golpe con
		// sprint lo aleja; el siguiente, antes de que vuelva)
		if (s.Sprinting || in.Sprint) && melee && !pl.weapon.stab && pl.stance != stancePress && !creeper {
			f.t.comboUntil = tick + 20
		}
		if pl.weapon.stab {
			f.stab() // (la lanza: la estocada, a todo lo que haya en la línea)
		} else {
			f.d.Hands.Attack(id, func() (entities.Entity, bool) { return f.d.Entities.Get(id) }, nil)
		}
		f.lastAttack.Store(tick + 1)
		if creeper {
			f.t.fromCreeper = true // pegado: lejos hasta que se le apague la mecha
		}
		if s.Sprinting || in.Sprint {
			f.t.wtap = 2
		}
	}
	if !f.d.Walker.SprintAllowed() {
		in.Sprint = false
	}
	if pl.stand { // en lo alto de una torre: ni un paso
		in = control.Input{}
	}
	s.Input = in
}

// thinkWary: en guardia. Mirándolo (adelantado a su movimiento), quieto, y
// con el escudo arriba si entra a menos de 7 bloques: su primer golpe no
// entra (en el simulador, sin guardia, el que pega primero gana el duelo).
func (f *Fighter) thinkWary(s *control.State, bp *basic.Player, pl *plan) {
	e, ok := f.d.Entities.Get(pl.target)
	if !ok {
		f.lowerShield()
		s.Input = control.Input{}
		return
	}
	pos := bp.Position()
	vx, vz := f.track(e, s.Ticks)
	ax, az := e.X+vx*aimLead, e.Z+vz*aimLead
	f.aim(bp, ax, e.Y+1.1, az)
	s.Input = control.Input{}
	// (con margen: el escudo tarda 5 ticks en parar y lo que se ve llega
	// tarde; corriendo, a 0,28 bloques por tick, son unos 3 bloques)
	if pl.shield && !f.shieldOff(s.Ticks) && math.Hypot(ax-pos.X, az-pos.Z) < 7 && !holdsAxe(e) {
		f.raiseShield(s.Ticks, pos)
	} else {
		f.lowerShield()
	}
}

// knockbackReach: hasta dónde puede llevar un empujón, en bloques. Safe mira
// unos 2 bloques (lo que lleva un golpe en el suelo), pero un golpe con sprint
// recibido en el aire (saltando para el crítico, o aún en el aire por el
// golpe anterior) lleva 3 o más: en la plataforma de 9×9 sin escudo, con el
// vacío a 2-3 bloques se retrocedía o se saltaba, y el siguiente golpe tiraba.
const knockbackReach = 3.5

// edgeWithin dice si hay una caída (o algo peligroso) a menos de reach
// bloques desde pos en la dirección (dx, dz), o en las diagonales cercanas
// (±30°: el empujón no va justo en la línea, el rival se mueve).
func (f *Fighter) edgeWithin(pos basic.Position, dx, dz, reach float64) bool {
	const spread = math.Pi / 6
	for _, a := range []float64{0, spread, -spread} {
		c, sn := math.Cos(a), math.Sin(a)
		ux, uz := dx*c-dz*sn, dx*sn+dz*c
		// Safe cubre ~2 bloques; desplazando el origen se cubre el resto
		for off := 0.0; ; off += 1.5 {
			p := pos
			p.X, p.Z = pos.X+ux*off, pos.Z+uz*off
			if !f.d.Walker.Safe(p, ux, uz) {
				return true
			}
			if off+2 >= reach {
				break
			}
		}
	}
	return false
}

// sideAwayFromEdge: hacia qué lado (-1 izquierda, 1 derecha, según la
// mirada) hay suelo seguro; ok false si por ninguno.
func (f *Fighter) sideAwayFromEdge(bp *basic.Player, pos basic.Position) (int, bool) {
	yaw := float64(bp.Position().Yaw) * math.Pi / 180
	lx, lz := math.Cos(yaw), math.Sin(yaw) // hacia la izquierda
	left, right := f.d.Walker.Safe(pos, lx, lz), f.d.Walker.Safe(pos, -lx, -lz)
	switch {
	case left && !right:
		return -1, true
	case right && !left:
		return 1, true
	case left && right:
		return f.t.strafeDir | 1, true // (los dos: el que llevaba)
	}
	return 0, false
}

// terrain ajusta las teclas del cuerpo a cuerpo al terreno con el
// pathfinding: nada de moverse hacia lava, fuego o una caída (se cambia de
// lado o se queda quieto en ese eje) y salto a tiempo para los escalones.
func (f *Fighter) terrain(s *control.State, bp *basic.Player, in *control.Input) {
	p := bp.Position()
	speed := math.Hypot(p.X-f.t.lastPos.X, p.Z-f.t.lastPos.Z)
	if speed > 1 {
		speed = 0
	}
	f.t.lastPos = p
	yaw := float64(p.Yaw) * math.Pi / 180
	fx, fz := -math.Sin(yaw), math.Cos(yaw) // hacia delante
	lx, lz := math.Cos(yaw), math.Sin(yaw)  // hacia la izquierda
	nav := f.d.Walker
	if in.Forward && !nav.Safe(p, fx, fz) {
		in.Forward, in.Sprint = false, false
	}
	if in.Backward && !nav.Safe(p, -fx, -fz) {
		in.Backward = false
	}
	if in.Left && !nav.Safe(p, lx, lz) {
		in.Left = false
		if nav.Safe(p, -lx, -lz) {
			in.Right, f.t.strafeDir = true, 1
		}
	} else if in.Right && !nav.Safe(p, -lx, -lz) {
		in.Right = false
		if nav.Safe(p, lx, lz) {
			in.Left, f.t.strafeDir = true, -1
		}
	}
	if s.OnGround && in.Forward && (nav.NeedJump(p, fx, fz, speed) || s.HorizontalCollision) {
		in.Jump = true
	}
}

// ReleaseHands baja el escudo y suelta el arco: un reflejo va a usar la mano
// (el cubo de agua). Solo desde el Think del controlador (como Think).
func (f *Fighter) ReleaseHands() {
	f.lowerShield()
	f.stopDraw()
	if _, _, eating := f.d.Hands.Using(); eating {
		f.d.Hands.CancelEat() // (comiendo, la mano no echa el agua: en el búnker, ardiendo y comiendo)
	}
}

// raiseShield levanta el escudo de la mano izquierda.
func (f *Fighter) raiseShield(tick uint64, pos basic.Position) {
	if f.t.blocking {
		return
	}
	f.t.blockFrom = tick
	u := play.UseItem{Hand: types.InteractionHandOffHand, YRot: pk.Float(pos.Yaw), XRot: pk.Float(pos.Pitch)}
	if f.d.Client.Conn.WritePacket(pk.Marshal(u.PacketID(), u)) == nil {
		f.t.blocking, f.t.blockSince = true, tick
	}
}

// lowerShield baja el escudo si está arriba.
func (f *Fighter) lowerShield() {
	if !f.t.blocking {
		return
	}
	a := play.PlayerAction{Action: types.PlayerActionActionReleaseUseItem}
	_ = f.d.Client.Conn.WritePacket(pk.Marshal(a.PacketID(), a))
	f.t.blocking = false
}

// aim gira la vista hacia un punto como lo haría un ratón: rápido pero no instantáneo.
func (f *Fighter) aim(bp *basic.Player, x, y, z float64) {
	p := bp.Position()
	dx, dy, dz := x-p.X, y-(p.Y+1.62), z-p.Z
	yaw := float32(-math.Atan2(dx, dz) * 180 / math.Pi)
	pitch := float32(-math.Atan2(dy, math.Hypot(dx, dz)) * 180 / math.Pi)
	f.turnTo(bp, yaw, pitch, 45, 25)
}

func (f *Fighter) turn(bp *basic.Player, yaw, pitch float32) { f.turnTo(bp, yaw, pitch, 30, 15) }

func (f *Fighter) turnTo(bp *basic.Player, yaw, pitch, maxYaw, maxPitch float32) {
	jitter := func() float32 { return (rand.Float32() - 0.5) * 1.2 }
	bp.UpdatePosition(func(p basic.Position) basic.Position {
		dy := wrap(yaw - p.Yaw)
		p.Yaw += clamp(dy, maxYaw) + jitter()
		p.Pitch = min(90, max(-90, p.Pitch+clamp(pitch-p.Pitch, maxPitch)+jitter()))
		return p
	})
}

func wrap(a float32) float32 {
	for a > 180 {
		a -= 360
	}
	for a < -180 {
		a += 360
	}
	return a
}

func clamp(v, m float32) float32 { return max(-m, min(m, v)) }

// facing dice si e mira más o menos hacia pos (un rival de espaldas no pega).
func facing(e entities.Entity, pos basic.Position) bool {
	want := float32(-math.Atan2(pos.X-e.X, pos.Z-e.Z) * 180 / math.Pi)
	yaw := e.HeadYaw
	if yaw == 0 && e.Yaw != 0 { // (la cabeza no siempre llega: el cuerpo)
		yaw = e.Yaw
	}
	return math.Abs(float64(wrap(want-yaw))) < 70
}

// boxDistance es la distancia de los ojos al punto más cercano de la caja del jugador e.
func boxDistance(p basic.Position, e entities.Entity) float64 {
	ex, ey, ez := act.Eye(p, false)
	cx := math.Max(e.X-0.3, math.Min(ex, e.X+0.3))
	cy := math.Max(e.Y, math.Min(ey, e.Y+1.8))
	cz := math.Max(e.Z-0.3, math.Min(ez, e.Z+0.3))
	return math.Sqrt((ex-cx)*(ex-cx) + (ey-cy)*(ey-cy) + (ez-cz)*(ez-cz))
}

// arrowFlight: los ticks que el escudo sigue arriba después de que el tirador
// suelte la cuerda (la flecha va a 1,6-3 bloques por tick: a 20 bloques, unos 10).
const arrowFlight = 15

// shotAt: e tensa el arco hacia el bot, o acaba de disparar (la flecha viene).
// Un mob tirador o un jugador con arco o ballesta.
func (f *Fighter) shotAt(e entities.Entity, pos basic.Position, tick uint64) bool {
	if aiming(e, pos) {
		f.t.drawnAt = tick
		return true
	}
	return (rangedMobs[kind(e)] || e.Type == "minecraft:player") && f.t.drawnAt > 0 && tick-f.t.drawnAt < arrowFlight
}

// shieldOff: el escudo está desactivado (un hachazo: onCooldown).
func (f *Fighter) shieldOff(uint64) bool { return time.Now().UnixNano() < f.shieldOffAt.Load() }

// netLag: los ticks de red en cada sentido (unos 200 ms en el servidor real,
// 4 en el simulador): su golpe se ve tarde y el del bot llega tarde.
const netLag = 4

// shieldDelay: los ticks que tarda el escudo en parar tras subirlo.
const shieldDelay = 5

// safeOpening: a la defensiva, si bajar el escudo para pegar no le da un
// golpe gratis. Tras su golpe le faltan 20/velocidad ticks para el
// siguiente; el bot lo ve netLag tarde, su golpe llega netLag después, y el
// escudo de nuevo arriba tarda shieldDelay más: con una espada (12.5 ticks)
// y 200 ms no da tiempo nunca (en el simulador: cada castigo, un golpe de 6
// recibido por 1.8 dado). Sí contra armas lentas (hacha, tridente), con él
// de espaldas (no pega) o comiendo.
func (f *Fighter) safeOpening(e entities.Entity, pos basic.Position, tick uint64, pl *plan) bool {
	if !facing(e, pos) || eatingNow(e) {
		return true
	}
	if f.t.enemyHit == 0 || pl.hisCooldown <= 0 {
		return false
	}
	return float64(tick-f.t.enemyHit+2*netLag+1+shieldDelay+2) <= pl.hisCooldown
}

// critLead: los ticks antes de tener el golpe cargado en que se salta para
// el crítico (a los 7 de un salto se va cayendo).
const critLead = 7

// aimLead: los ticks que se adelanta la puntería (el viaje de ida y vuelta al servidor).
const aimLead = 3

// track sigue el movimiento del objetivo: su velocidad en bloques por tick
// (suavizada), de sus posiciones de un tick a otro.
func (f *Fighter) track(e entities.Entity, tick uint64) (vx, vz float64) {
	if f.t.seenID != e.ID || tick-f.t.seenTick > 10 || tick == f.t.seenTick {
		if f.t.seenID != e.ID || tick-f.t.seenTick > 10 {
			f.t.seenVel = [2]float64{}
		}
	} else {
		dt := float64(tick - f.t.seenTick)
		nvx, nvz := (e.X-f.t.seenPos[0])/dt, (e.Z-f.t.seenPos[1])/dt
		if math.Hypot(nvx, nvz) < 1.5 { // (un teletransporte no es velocidad)
			f.t.seenVel = [2]float64{0.5*f.t.seenVel[0] + 0.5*nvx, 0.5*f.t.seenVel[1] + 0.5*nvz}
		}
	}
	f.t.seenID, f.t.seenPos, f.t.seenTick = e.ID, [2]float64{e.X, e.Z}, tick
	return f.t.seenVel[0], f.t.seenVel[1]
}

// closingSpeed: a qué velocidad se acerca e (bloques por tick; negativo si se aleja).
func (f *Fighter) closingSpeed(pos basic.Position, e entities.Entity, vx, vz float64) float64 {
	dx, dz := e.X-pos.X, e.Z-pos.Z
	n := math.Hypot(dx, dz)
	if n < 0.1 {
		return 0
	}
	return -((dx)*(vx-f.t.myVel[0]) + dz*(vz-f.t.myVel[1])) / n
}

// holdsAxe: lleva un hacha en la mano (un golpe suyo desactiva el escudo 5 s).
func holdsAxe(e entities.Entity) bool {
	it, ok := e.Equipment[0]
	return ok && it.Count > 0 && isAxe(itemName(int32(it.Item)))
}

// aiming: e es un tirador que tensa el arco (o la ballesta). Apunta a su
// objetivo (RangedBowAttackGoal lo mira): el del bot lo es. Un jugador, si
// tensa el arco de cara al bot (pos).
func aiming(e entities.Entity, pos basic.Position) bool {
	if e.Type == "minecraft:player" {
		return drawingBow(e) && facing(e, pos)
	}
	if !rangedMobs[kind(e)] {
		return false
	}
	v, ok := e.Data[entitydata.LivingEntityLivingEntityFlags]
	if !ok {
		return false
	}
	b, ok := v.Value.(*pk.Byte)
	return ok && *b&1 != 0 // (usando un objeto: el arco)
}

// drawingBow: un jugador tensa el arco o carga la ballesta (la mano en uso con ella).
func drawingBow(e entities.Entity) bool {
	n, ok := usingItem(e)
	return ok && (n == "minecraft:bow" || n == "minecraft:crossbow")
}

// creeperLit: el creeper tiene la mecha encendida (DATA_SWELL_DIR = 1).
func creeperLit(e entities.Entity) bool {
	v, ok := e.Data[entitydata.CreeperSwellDir]
	if !ok {
		return false
	}
	switch x := v.Value.(type) {
	case *pk.VarInt:
		return *x > 0
	case *pk.Int:
		return *x > 0
	}
	return false
}

// spacing: a qué distancia se le espera mientras el golpe recarga (sin
// escudo). Perdiendo el intercambio, más lejos: que entre él y se le castigue.
func spacing(s stance) float64 {
	if s == stanceCautious || s == stanceAvoid {
		return 4.5
	}
	return 3.8
}

// keepAt: maniobrando (rompiéndole la pared a uno armado), a pl.keepAt de
// él: fuera de su alcance (3) y con su pared al de la mano (4,5). Hacia
// delante o atrás, de cara; quieto mientras rompe (si no, se pierde el bloque).
func (f *Fighter) keepAt(s *control.State, bp *basic.Player, pl *plan) {
	if pl.keepAt <= 0 || f.d.Hands.Busy() {
		return
	}
	e, ok := f.d.Entities.Get(pl.target)
	if !ok {
		return
	}
	pos := bp.Position()
	d := math.Hypot(e.X-pos.X, e.Z-pos.Z)
	f.turn(bp, float32(-math.Atan2(e.X-pos.X, e.Z-pos.Z)*180/math.Pi), pos.Pitch)
	in := control.Input{}
	switch {
	case d < pl.keepAt-0.2:
		in.Backward = true
	case d > pl.keepAt+0.3:
		in.Forward = true
	}
	f.terrain(s, bp, &in)
	s.Input = in
}
