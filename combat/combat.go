// Package combat es el módulo de pelea PvP del bot.
//
// Solo pelea contra jugadores que le hacen daño. Con varios atacantes elige
// el que puede matar antes (vida, armadura y distancia). Usa la mejor arma
// que tenga contra cada uno, hace críticos, W-tap y strafe, bloquea con
// escudo, se pone la mejor armadura, se retira para comer cuando le queda
// poca vida y vuelve a la pelea cuando se recupera.
package combat

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/playerlist"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/data/entitydata"
	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

// Ajustes del combate.
const (
	reach      = 2.9 // distancia de golpe (el servidor permite 3)
	critFactor = 1.5 // multiplicador de un crítico: el bot pelea a críticos
	chaseMax   = 48  // más lejos que esto, deja de perseguir
	forgetIdle = 45 * time.Second
	sprintBPS  = 5.6 // bloques por segundo corriendo
)

// Navigator es el movimiento con pathfinding que usa el combate (pathing.Walker).
type Navigator interface {
	Follow(target func() (x, y, z float64, ok bool), near float64)
	Goto(goal world.BlockPos)
	Stop()
	Status() string
	Think(s *control.State, bp *basic.Player)
	SetSprint(bool)
	SprintAllowed() bool
	// para el cuerpo a cuerpo, que maneja las teclas él mismo:
	Direct(pos basic.Position, x, z float64) bool                             // se llega en línea recta
	Safe(pos basic.Position, dx, dz float64) bool                             // moverse hacia ahí es seguro
	NeedJump(pos basic.Position, dx, dz, speed float64) bool                  // escalón delante: saltar ya
	FleeGoal(pos basic.Position, fromX, fromZ float64) (world.BlockPos, bool) // dónde huir
	Ground(x, z, nearY int) (world.BlockPos, bool)                            // dónde pisar en (x, z)
	Building() bool                                                           // pone bloques (torre, puente): la mano es suya
	PillarUp(pos basic.Position, n int) bool                                  // sube n bloques en el sitio (una torre)
	SetBuild(bool)                                                            // permite o prohíbe poner bloques en los caminos
	SetBridges(bool)                                                          // permite o prohíbe solo los puentes (las torres siguen)
	BuildBlocks() int                                                         // los bloques de construir que lleva
	Climbing() bool                                                           // el camino aún tiene torre por delante
}

// Deps es lo que el módulo necesita del bot.
type Deps struct {
	Client   *bot.Client
	Player   *basic.Player
	Entities *entities.Manager
	Players  *playerlist.PlayerList
	Screens  *screen.Manager
	World    *world.World
	Hands    *act.Hands
	Ctl      *control.Controller
	Walker   Navigator
	Mover    *physics.Mover
	// NameOf da el nombre de un jugador por su uuid (desde cualquier goroutine).
	NameOf func(uuid.UUID) string
	// KeepGold: la armadura de oro puesta no se cambia por otra (en el Nether los piglins atacan si no se lleva oro).
	KeepGold func() bool
	// Idle: el bot no hace nada (ni tarea ni orden): lleva el arma en la mano (nil: nunca).
	Idle func() bool
	// Ordered: otra orden mueve al bot (goto, follow, una tarea…): no se vuelve a la zona mientras (nil: nunca).
	Ordered func() bool
	// Flying: vuela con élitros: no se pelea ni se toca la mano (nil: nunca).
	Flying func() bool
	// Friend: un jugador de los nuestros (los dueños): no se le planta cara aunque venga armado (nil: nadie).
	Friend func(uuid.UUID) bool
	// Builder pone bloques (el búnker, bunker.go); nil: no se encierra.
	Builder Builder
	// Ally: otro bot del mismo panel: nunca se pelea con él (nil: ninguno).
	Ally func(uuid.UUID) bool
	// Nether: el bot está en el Nether (las anclas no explotan allí); nil: nunca.
	Nether func() bool
}

type mode int

const (
	idle mode = iota
	fighting
	retreating
	eating
	wary       // en guardia: un jugador armado viene a por él (arma en mano, escudo listo, mirándolo; no ataca)
	sheltering // encerrándose con bloques para curarse (bunker.go)
	acting     // las manos en algo (romperle el suelo, quitarle el agua, encerrarle: tactics.go): quieto
)

func (m mode) String() string {
	return [...]string{"tranquilo", "peleando", "retirándose", "comiendo", "en guardia", "en el búnker", "maniobrando"}[m]
}

// plan es lo que la goroutine de decisiones le dice a Think (cada tick).
type plan struct {
	mode         mode
	target       int32
	closeIn      bool           // cuerpo a cuerpo: Think maneja teclas y golpes
	weapon       weapon         // el arma en la mano
	shield       bool           // tiene escudo en la mano izquierda
	noCrit       bool           // golpe directo, sin saltar (hacha contra escudo)
	stand        bool           // quieto (en lo alto de una torre: un paso y se cae)
	bow          bool           // con el arco (bow.go)
	stance       stance         // cómo va el intercambio con este rival (matchup.go)
	shelter      world.BlockPos // la celda del búnker (bunker.go)
	jump         bool           // en el búnker: saltar (para poner sobre la pared)
	holdFire     bool           // acercarse sin pegar (para encerrarle antes: tactics.go)
	keepAt       float64        // maniobrando: a esta distancia de él (0: quieto)
	hisCooldown  float64        // los ticks entre dos golpes suyos (su arma; 0: no se sabe)
	steer        bool           // maniobrando: andar recto a (steerX, steerZ) (meterse en el agua: fire.go)
	steerX       float64
	steerZ       float64
	fleeX, fleeZ float64 // de dónde huir al retirarse
	finish       bool    // le queda poca vida: a por él, sin retroceder (defense.go)
	lunge        bool    // la embestida con la lanza (arsenal.go)
	smash        bool    // en el aire con la maza: pegar en cuanto esté a tiro (arsenal.go)
	fly          int     // con élitros: 1 subiendo, 2 en picado (expert.go)
	guard        bool    // un cristal cerca: el escudo de cara a él (expert.go)
	dodge        bool    // con guard: de lado en vez de atrás (un mazazo que cae: defense.go)
	gx, gy, gz   float64 // dónde está
}

type aggressor struct {
	fromZone bool // está aquí por estar en la zona, no por pegarle al bot
	avoid    bool // no se pelea: se huye de él (un warden)
	id       int32
	name     string
	lastHit  time.Time
	hits     int
}

type foe struct {
	aggressor
	e    entities.Entity
	dist float64
}

// Fighter es el módulo de pelea. Créalo con New, pon su Think como Think del
// controlador y lanza Run en una goroutine.
type Fighter struct {
	lastEndLog   time.Time // el último "pelea terminada" (para no repetirlo a cada paso)
	lastStandLog time.Time // el último "no huyo" (lo mismo)
	d            Deps
	// Log recibe los mensajes del módulo (nil: log.Printf).
	Log func(format string, args ...any)
	// EatWhenIdle hace que coma fuera de combate cuando tiene hambre.
	EatWhenIdle bool
	// Reflex: un reflejo (apagarse con el cubo) tiene las manos: no se cambia
	// lo de la mano ni se decide nada mientras (nil: nunca).
	Reflex func() bool
	// Burning: el bot está ardiendo (safety.Fire); nil: no se sabe (fire.go).
	Burning func() bool

	mu         sync.Mutex
	aggressors map[int32]*aggressor
	zone       atomic.Pointer[Zone]    // "fight in": pelear con todo el que esté dentro
	guard      atomic.Pointer[guarded] // "guard": el jugador al que protege

	plan           atomic.Pointer[plan]
	lastAttack     atomic.Uint64 // tick del último golpe
	attackReset    atomic.Bool   // cambió de objeto: la carga empieza de cero
	offhandSwapped atomic.Bool   // cambió la mano izquierda: un escudo levantado ya no lo está (Think)
	hurtMelee      atomic.Bool   // le acaban de pegar de cerca (Think: jump reset)
	shieldHit      atomic.Bool   // un golpe le dio en el escudo (evento 29): eso también es atacarle
	enemySwing     atomic.Int32  // quién acaba de dar un golpe (su brazo, su golpe en el bot o en el escudo): Think abre la ventana de castigo
	shieldOffAt    atomic.Int64  // hasta cuándo (UnixNano) tiene el escudo desactivado (un hachazo)
	lastHurt       atomic.Int64
	shots          atomic.Int64 // flechas tiradas
	shelterBlocks  atomic.Int64 // bloques puestos en búnkeres
	dropShelter    atomic.Bool  // olvidar el búnker (ForgetFights: una orden "stop", morir)
	handsBusy      atomic.Bool  // una táctica usa las manos (el cubo): hasta que acabe, nada de cambiar de objeto
	lunging        atomic.Bool  // Think está embistiendo con la lanza (arsenal.go)
	popped         atomic.Bool  // se gastó un tótem del bot (expert.go)
	enemyPop       atomic.Int32 // a quién se le acaba de gastar uno
	glideOn        atomic.Bool  // planeando, según el servidor
	glideTold      atomic.Uint64
	enemyHurtAt    atomic.Int64 // cuándo (UnixNano) se le hizo daño a enemyHurt
	ourHitAt       atomic.Int64 // cuándo pegó el bot por última vez
	enemyHurt      atomic.Int32 // a quién se le acaba de hacer daño (la embestida: le dio)

	// solo la goroutine de Run
	following    int32          // a quién sigue el walker (fleeing: huyendo con Goto; pearlChase: a donde cae una perla)
	pearlID      int32          // la perla del objetivo que se persigue (pearl.go)
	pearlGoal    world.BlockPos // a dónde se va por ella
	pearlOwner   int32          // quién lanzó la última perla perseguida
	pearlStep    uint64         // y cuándo se la vio volar por última vez
	fleeAt       time.Time      // cuándo eligió el último sitio al que huir
	standUntil   time.Time
	retreatSince time.Time
	finishSince  time.Time         // desde cuándo cree que lo mata del siguiente golpe
	lastUpkeep   uint64            // el paso de la última comida fuera de pelea (upkeep)
	lastArm      uint64            // el del último repaso del arma (keepArmed)
	zoneGoal     bool              // va de vuelta a la zona
	geared       bool              // ya revisó el equipo con este inventario
	gearPrint    uint64            // huella del inventario en la última revisión
	gearCheck    uint64            // ticks hasta la próxima revisión
	steps        uint64            // pasos (ticks) de Run: para esperas que no dependan del reloj
	offhandAt    uint64            // el paso del último cambio de la mano izquierda
	offhandWant  string            // lo que se pidió con el último cambio (hasta verlo en la mano)
	offhandFails int               // intentos seguidos sin que llegara
	offhandRest  uint64            // tras 3 fallos, hasta este paso no se intenta
	chaseID      int32             // a quién se persigue (para ver si se avanza)
	chaseBest    float64           // lo más cerca que se ha estado de él
	chaseAt      uint64            // el paso en que se acercó por última vez
	potionAt     uint64            // el paso de la última poción (no dos seguidas)
	pil          pillarState       // la torre (pillar.go)
	noWayOut     uint64            // el paso en que no encontró adónde huir (la torre: pillar.go)
	fightStep    uint64            // el último paso en que se peleó (fight)
	ignored      map[int32]uint64  // a quién no se llega: no se le busca pelea hasta ese paso (si pega, sí)
	waryDist     map[int32]float64 // a qué distancia estaba cada jugador armado (beWary: si viene)
	waryFrom     uint64            // desde qué paso está en guardia contra el de ahora
	waryIgnore   map[int32]uint64  // a quién no se vigila hasta ese paso (estuvo cerca sin atacar)
	stagedAt     uint64            // el de la última vez que dejó escudo o tótem en la barra

	ars         arsenalState            // lanza, maza, lava, telarañas… (arsenal.go)
	ex          expertState             // cristales, anclas, caña, perla, élitros… (expert.go)
	bk          bunkerState             // el búnker (bunker.go)
	tac         tacticsState            // romperle el suelo, quitarle el agua, encerrarle (tactics.go)
	myBlocks    map[world.BlockPos]bool // los bloques que puso en búnkeres (para salir si se queda encerrado)
	stanceNow   stance                  // la postura contra el objetivo de este paso
	bunkerRetry uint64                  // hasta este paso no se intenta otro búnker (uno falló)
	match       matchup                 // la cuenta contra el objetivo (matchup.go)
	arms        map[int32]armsSeen      // el arma más fuerte vista a cada jugador (matchup.go)
	waterGoal   world.BlockPos          // el agua a la que va ardiendo sin cubo (fire.go)
	waterAt     uint64                  // cuándo la eligió
	matchID     int32                   // de quién
	matchAt     uint64                  // y de cuándo (se rehace cada segundo: la vida cambia)

	t thinkState // solo Think
}

// New crea el módulo y se suscribe a los eventos de daño.
func New(d Deps) *Fighter {
	f := &Fighter{d: d, aggressors: map[int32]*aggressor{}, EatWhenIdle: true, waryDist: map[int32]float64{}}
	f.plan.Store(&plan{})
	d.Client.Events.AddListener(bot.PacketHandler{
		Priority: 0, ID: packetid.ClientboundPlayDamageEvent, F: f.onDamage,
	}, bot.PacketHandler{
		Priority: 0, ID: packetid.ClientboundPlayCooldown, F: f.onCooldown,
	}, bot.PacketHandler{
		Priority: 0, ID: packetid.ClientboundPlayEntityEvent, F: f.onEntityEvent,
	}, bot.PacketHandler{
		Priority: 0, ID: packetid.ClientboundPlayAnimate, F: f.onAnimate,
	})
	f.initExpert()
	return f
}

func (f *Fighter) logf(format string, args ...any) {
	if f.Log != nil {
		f.Log(format, args...)
		return
	}
	log.Printf("[pelea] "+format, args...)
}

// Fighting dice si está en combate (peleando, retirándose o comiendo en pelea).
func (f *Fighter) Fighting() bool { return f.plan.Load().mode != idle }

// Reset olvida todas las peleas (al morir, o la orden "stop"). Se puede
// llamar desde cualquier goroutine: Run ve que no quedan enemigos y para.
func (f *Fighter) Reset() {
	f.zone.Store(nil)
	f.SetGuard(uuid.Nil, "")
	f.ForgetFights()
}

// ForgetFights olvida las peleas en curso pero mantiene la zona y a quién
// protege (al morir: al reaparecer sigue con lo suyo).
func (f *Fighter) ForgetFights() {
	f.mu.Lock()
	clear(f.aggressors)
	f.mu.Unlock()
	f.dropShelter.Store(true) // (y el búnker: si queda cerrado, unbox abre una salida)
}

// Engage empieza a pelear contra la entidad id como si le hubiera atacado
// (la orden "fight").
func (f *Fighter) Engage(id int32, name string) {
	f.mu.Lock()
	a, ok := f.aggressors[id]
	if !ok {
		a = &aggressor{id: id, name: name}
		f.aggressors[id] = a
	}
	a.lastHit = time.Now()
	f.mu.Unlock()
	f.logf("a por %s", name)
}

// Status resume el estado del combate para la consola.
func (f *Fighter) Status() string {
	f.mu.Lock()
	var names []string
	for _, a := range f.aggressors {
		names = append(names, a.name)
	}
	f.mu.Unlock()
	pl := f.plan.Load()
	st := pl.mode.String()
	if len(names) > 0 {
		st = fmt.Sprintf("%s contra %v", pl.mode, names)
	}
	if z := f.zone.Load(); z != nil {
		st += " (zona " + z.String() + ")"
	}
	if g := f.guard.Load(); g != nil {
		st += " (protegiendo a " + g.name + ")"
	}
	if pl.target != 0 && pl.mode != idle && pl.stance != stanceEven {
		st += " [" + pl.stance.String() + "]"
	}
	return st
}

func (f *Fighter) nameOf(id uuid.UUID, eid int32) string {
	if f.d.NameOf != nil {
		if n := f.d.NameOf(id); n != "" {
			return n
		}
	}
	return fmt.Sprintf("jugador#%d", eid)
}

// onDamage anota quién le hizo daño (ClientboundDamageEvent).
func (f *Fighter) onDamage(p pk.Packet) error {
	var ev play.DamageEvent
	if err := p.Scan(&ev); err != nil {
		return err
	}
	if int32(ev.EntityID) != int32(f.d.Player.Login.PlayerID) {
		f.enemyHurt.Store(int32(ev.EntityID))
		f.enemyHurtAt.Store(time.Now().UnixNano())
		f.defend(int32(ev.EntityID), int32(ev.SourceCauseID)-1) // ¿le pegaron al protegido?
		return nil
	}
	f.lastHurt.Store(time.Now().UnixMilli())
	cause := int32(ev.SourceCauseID) - 1 // el protocolo manda id+1; 0 es "nadie"
	if cause < 0 {
		return nil
	}
	if ev.SourceDirectID == ev.SourceCauseID { // de cerca: lo que pega es él (no una flecha)
		f.hurtMelee.Store(true)
		f.enemySwing.Store(cause)
	}
	e, ok := f.d.Entities.Get(cause)
	if !ok {
		return nil
	}
	// quien le pegue, jugador o mob (el protegido nunca: engage lo descarta)
	f.engage(e, "me atacó", true)
	return nil
}

// onAnimate: alguien movió el brazo (Animate 0: el golpe de la mano
// derecha). El del rival es su golpe, dé o no: se apunta para la ventana de
// castigo (Think).
func (f *Fighter) onAnimate(p pk.Packet) error {
	var a play.Animate
	if err := p.Scan(&a); err != nil {
		return err
	}
	if a.Action == 0 {
		f.enemySwing.Store(int32(a.ID))
	}
	return nil
}

// onEntityEvent: el 29 sobre el bot es un golpe que paró el escudo (el
// sonido del escudo): no hay daño, pero alguien le está atacando.
func (f *Fighter) onEntityEvent(p pk.Packet) error {
	var ev play.EntityEvent
	if err := p.Scan(&ev); err != nil {
		return err
	}
	if int32(ev.EntityID) == int32(f.d.Player.Login.PlayerID) && ev.EventID == 29 {
		f.shieldHit.Store(true)
	}
	if ev.EventID == 35 { // un tótem gastado (expert.go)
		f.onPop(int32(ev.EntityID))
	}
	return nil
}

// onCooldown: el servidor pone en espera un objeto. El del escudo es un
// hachazo con el escudo arriba: 5 s (100 ticks) sin poder levantarlo.
func (f *Fighter) onCooldown(p pk.Packet) error {
	var c play.Cooldown
	if err := p.Scan(&c); err != nil {
		return err
	}
	if strings.HasSuffix(string(c.CooldownGroup), "shield") && c.Duration > 0 {
		f.shieldOffAt.Store(time.Now().Add(time.Duration(c.Duration) * 50 * time.Millisecond).UnixNano())
		f.logf("escudo desactivado %.1f s (un hachazo)", float64(c.Duration)/20)
	}
	return nil
}

// Run toma las decisiones del combate, una vez por tick, hasta que ctx termine.
func (f *Fighter) Run(ctx context.Context) error {
	for {
		if err := f.d.Ctl.WaitTicks(ctx, 1); err != nil {
			return err
		}
		f.step(ctx)
	}
}

func (f *Fighter) setPlan(p plan) { f.plan.Store(&p) }

func (f *Fighter) step(ctx context.Context) {
	f.steps++
	st := f.d.Player.Status()
	if st.Health <= 0 {
		return
	}
	if f.Reflex != nil && f.Reflex() {
		return // (volver a la espada aquí es quitarle el cubo de la mano)
	}
	if f.retotem() { // (gastado un tótem: otro a la mano antes que nada)
		return
	}
	cur := *f.plan.Load()
	if f.d.Flying != nil && f.d.Flying() {
		if cur.mode != idle {
			f.setPlan(plan{})
		}
		return
	}
	if f.dropShelter.Swap(false) && f.bk.on && len(f.bk.opening) == 0 {
		f.bk = bunkerState{}
		f.bunkerRetry = f.steps + 100
		f.setPlan(plan{})
	}
	f.scanZone()
	f.scanThreats()
	foes := f.foes()
	// de los que se evitan (un warden) se huye, pase lo que pase
	var fight []foe
	for _, fo := range foes {
		if fo.avoid {
			if fo.dist < safeFrom([]foe{fo})+4 {
				f.retreatSince = time.Now()
				f.retreat(ctx, st, nil, fo.dist, fo.e.X, fo.e.Z)
				return
			}
			continue
		}
		fight = append(fight, fo)
	}
	foes = fight
	// ardiendo sin cubo y con agua cerca: antes que nada (antes que el
	// búnker: dentro se sigue quemando; en los logs, de 14 a 9 encerrado
	// ardiendo con agua a unos pasos)
	if !f.bk.on {
		var near *foe
		if len(foes) > 0 {
			near = &foes[0]
		}
		if f.toWater(st, near) { // (también sin pelea: en guardia, o una lava que pisó)
			return
		}
	}
	// saliendo del búnker y le esperan: a pelear (defense.go)
	f.exitAmbush(foes)
	// encerrado por él: romper su pared y salir a pelear (defense.go)
	for i := range foes {
		if foes[i].e.Type == "minecraft:player" && f.trapped(&foes[i]) {
			break
		}
	}
	// el búnker (bunker.go): mientras dura, manda él
	if f.bunkerStep(ctx, st, foes, f.match.stance) {
		return
	}
	// encerrado entre sus bloques sin búnker en marcha (se canceló, le
	// empujaron dentro, reconectó): salir, también en plena pelea (el
	// pathfinding no rompe bloques: antes se quedaba dentro hasta "no llego")
	if f.unbox() {
		f.bunkerStep(ctx, st, foes, f.match.stance)
		return
	}
	// la torre (pillar.go): antes que nada, que decide con los que vienen
	if f.pillarStep(ctx, st, f.foods()) {
		return
	}
	mobsOnly := len(foes) > 0
	for _, fo := range foes {
		if fo.e.Type == "minecraft:player" {
			mobsOnly = false
		}
	}
	// peleando con un jugador, el pathfinding sin puentes: con sus empujones,
	// un puente al borde es caerse (medido en una plataforma: 2 de 4 caídas
	// con, 0 sin). Las torres sí, como en el goto: si se sube a unos bloques,
	// se sube detrás (antes se quedaba abajo mirándolo)
	f.d.Walker.SetBuild(true)
	f.d.Walker.SetBridges(mobsOnly || len(foes) == 0)

	// un golpe en el escudo estando en guardia: es él, y empieza la pelea
	if f.shieldHit.Swap(false) && cur.mode == wary {
		if e, ok := f.d.Entities.Get(cur.target); ok {
			f.engage(e, "me pegó en el escudo", true)
			foes = f.foes()
		}
	}
	if len(foes) == 0 && f.beWary(ctx, st) {
		return // un jugador armado viene: en guardia (aún no es pelea)
	}
	if len(foes) == 0 {
		if cur.mode != idle {
			if time.Since(f.lastEndLog) > 5*time.Second { // (huyendo, entra y sale de la pelea a cada paso)
				f.logf("pelea terminada (vida %.1f, comida %d)", st.Health, st.Food)
				f.lastEndLog = time.Now()
			}
			f.stopMoving()
			f.setPlan(plan{})
			f.geared = false
		}
		f.upkeep(ctx, st)
		if f.fetchTrident() || f.reclaimLava(ctx) { // (el tridente lanzado, la lava echada: a recogerlos)
			return
		}
		// fuera de pelea también: equipo mejor recién recogido (lo que le tiran), puesto ya;
		// no con una pantalla abierta (un cofre, una mesa: lo usa una tarea)
		if _, open := f.d.Screens.Open(); !open && !f.d.Hands.Busy() {
			if f.unbox() { // encerrado en un búnker que quedó a medias: salir
				return
			}
			f.gear(ctx, math.Inf(1))
			f.manageOffhand(ctx, nil, st.Health, math.Inf(1))
			f.stageOffhand(ctx)
			f.keepArmed(ctx)
		}
		f.toZone()
		return
	}
	f.zoneGoal = false

	inv, _ := f.inventory()
	shield := nameOf(inv[slotOffhand]) == "minecraft:shield"
	foods := f.foods()
	near := foes[0].dist
	cx, cz, close8 := 0.0, 0.0, 0
	for _, fo := range foes {
		near = math.Min(near, fo.dist)
		if fo.dist <= 8 {
			cx += fo.e.X
			cz += fo.e.Z
			close8++
		}
	}
	if close8 == 0 {
		cx, cz, close8 = foes[0].e.X, foes[0].e.Z, 1
	}
	cx, cz = cx/float64(close8), cz/float64(close8)

	// umbral de retirada: más enemigos cerca, antes se retira
	low := float32(7)
	switch {
	case close8 >= 3:
		low = 14
	case close8 == 2:
		low = 11
	}
	_, hasFood := pickFood(foods, 20-st.Food, false)
	gap, hasGap := pickFood(foods, 20-st.Food, true)
	target, w, slot, ttk := f.pickTarget(foes, cur.target)
	// lo mata de un golpe: no se retira por eso... pero solo un momento (si el
	// golpe no entra, una araña que salta, no se juega la vida esperándolo) y
	// nunca con tan poca vida que un golpe más lo mate
	finishing := target != nil && target.dist <= 4 && ttk <= 0.01
	if !finishing {
		f.finishSince = time.Time{}
	} else if f.finishSince.IsZero() {
		f.finishSince = time.Now()
	}
	finishing = finishing && st.Health > 4 && time.Since(f.finishSince) < 1500*time.Millisecond

	// contra un jugador: su equipo contra el del bot (matchup.go) decide cómo
	f.stanceNow = stanceEven
	isPlayer := target != nil && target.e.Type == "minecraft:player"
	if isPlayer {
		m := f.matchupFor(target, st)
		f.stanceNow = m.stance
		switch m.stance {
		case stancePress:
			low = min(low, 5) // gana el intercambio: retirarse es regalarle la pelea
		case stanceCautious:
			low += 4 // pierde el intercambio: a curarse antes (al búnker)
		case stanceAvoid:
			low += 6
		}
		// a dos golpes suyos de morir (con su arma y la armadura del bot), a
		// curarse aunque se gane el intercambio: el cálculo es de media, y dos
		// golpes con suerte (un crítico) acaban la pelea (en el simulador, con 6
		// de vida y "le gano", muerto en 1,3 s)
		armor, tough := f.myArmor()
		if two := 2 * afterArmor(theirHit(target.e), armor, tough); float64(low) < two {
			low = float32(min(two, 12))
		}
	}

	// la torre: contra zombis y los suyos, arriba no llegan (pillar.go)
	// pociones: curarse con poca vida, prepararse contra un jugador
	if f.usePotions(ctx, st, target, near) {
		return
	}
	// manzana dorada en plena pelea si no hay nadie encima. Comer son 1,6 s
	// quieto: a uno que viene corriendo (a 0,28 por tick llega en 32 ticks
	// desde 9) le da tiempo a llegar y pegar dos veces. Contra un jugador que
	// viene, antes el búnker (dentro se come); si no, solo con él lejos
	_, regen := st.Effects["minecraft:regeneration"]
	if hasGap && st.Health <= 12 && !regen && near >= 3.5 {
		coming := false
		if target != nil && target.e.Type == "minecraft:player" {
			p := f.d.Player.Position()
			coming = target.dist-math.Max(0, f.closingSpeed(p, target.e, f.t.seenVel[0], f.t.seenVel[1]))*34 < 3
		}
		// (y con un precipicio a la espalda no: comiendo se está quieto, y su
		// golpe tira por el borde; en los logs, bot contra bot: 68 bloques)
		edge := false
		if target != nil {
			p := f.d.Player.Position()
			if fl := math.Hypot(p.X-target.e.X, p.Z-target.e.Z); fl > 0.1 {
				edge = f.edgeWithin(p, (p.X-target.e.X)/fl, (p.Z-target.e.Z)/fl, knockbackReach)
			}
		}
		switch {
		case edge:
			// (sin comer: a pelear, y el borde lo lleva thinkFight)
		case !coming:
			f.logf("como una dorada (vida %.1f)", st.Health)
			f.eat(ctx, gap, cx, cz)
			return
		case f.wantBunker(target) && time.Now().After(f.standUntil):
			f.startBunker(target, fmt.Sprintf("vida %.1f: dentro me como la dorada", st.Health))
			return
		}
	}

	switch cur.mode {
	case retreating, eating:
		cornered := time.Since(f.retreatSince) > 3*time.Second && near < 2.5
		safe := mobsOnly && near > safeFrom(foes) // los mobs se quedaron atrás
		// a salvo vuelve a pelear solo con algo de vida: con la de huir, al
		// acercarse se volvería a retirar, y así sin parar
		recovered := st.Health >= low+6 || (!hasFood && !hasGap && st.Food >= 18 && st.Health >= low+2) ||
			safe && st.Health > low
		if cornered && time.Now().After(f.standUntil) {
			f.logf("acorralado (vida %.1f): sigo peleando", st.Health)
			f.standUntil = time.Now().Add(8 * time.Second)
		} else if safe && !recovered {
			// lejos de ellos pero sin vida: quieto, comiendo si puede, a que se cure
			if near > safeFrom(foes)+8 && time.Since(time.UnixMilli(f.lastHurt.Load())) > 5*time.Second {
				f.logf("a salvo de los mobs (vida %.1f): dejo la pelea", st.Health)
				f.ForgetFights()
				f.stopMoving()
				f.setPlan(plan{})
				return
			}
			if fd, ok := pickFood(foods, 20-st.Food, false); ok && st.Food < 20 {
				f.eat(ctx, fd, cx, cz)
				return
			}
			f.stopMoving()
			return
		} else if !recovered && time.Now().After(f.standUntil) {
			if f.gear(ctx, near) { // lejos de ellos: buen momento para equiparse
				return
			}
			if !mobsOnly && f.wantBunker(target) { // encerrarse con bloques y curarse
				f.startBunker(target, fmt.Sprintf("vida %.1f", st.Health))
				return
			}
			if !mobsOnly { // de un jugador no se huye: o se esconde (el búnker) o se pelea
				f.standFirm(target, st)
			} else {
				f.retreat(ctx, st, foods, near, cx, cz)
				return
			}
		} else if recovered && mobsOnly && st.Health < low+2 && near > safeFrom(foes)+8 && time.Since(time.UnixMilli(f.lastHurt.Load())) > 5*time.Second {
			// los mobs se quedaron atrás pero sigo sin vida: no se vuelve a por
			// ellos (si vienen, que vengan: entonces es otra pelea)
			f.logf("a salvo de los mobs (vida %.1f): dejo la pelea", st.Health)
			f.ForgetFights()
			f.stopMoving()
			f.setPlan(plan{})
			return
		} else if recovered {
			f.logf("recuperado (vida %.1f): vuelvo a pelear", st.Health)
		}
	default:
		mustEat := st.Food <= 6 && hasFood // sin comida no puede correr
		// huir sirve para comer y curarse; y de los mobs, siempre: se les deja atrás corriendo
		tooLow := st.Health <= low && (hasFood || hasGap || st.Food >= 18 || mobsOnly) && !finishing
		// a un jugador no se le deja atrás corriendo (corre igual): huir sin con
		// qué (perla, poción de velocidad) es que te peguen por la espalda. Se
		// sigue si se gana el intercambio de golpes o si no hay escapatoria
		// contra un jugador nunca se huye: ganar, o esconderse mientras se
		// cura (el búnker) y volver. Huir corriendo es que te peguen por la
		// espalda, y la perla solo alarga la pelea
		if (tooLow || mustEat) && !mobsOnly && target != nil {
			win, why := f.trade(target, st)
			if !win && f.wantBunker(target) && time.Now().After(f.standUntil) {
				f.logf("vida %.1f contra %s, y pierdo el intercambio (%s)", st.Health, target.name, why)
				f.startBunker(target, "a curarme")
				return
			}
			if time.Since(f.lastStandLog) > 5*time.Second {
				f.logf("vida %.1f pero no huyo de %s: %s", st.Health, target.name, why)
				f.lastStandLog = time.Now()
			}
			tooLow, mustEat = false, false
		}
		if (mustEat || tooLow) && time.Now().After(f.standUntil) {
			f.logf("vida %.1f, comida %d, %d enemigos cerca: me retiro a comer", st.Health, st.Food, close8)
			f.retreatSince = time.Now()
			f.stopMoving()
			f.setPlan(plan{mode: retreating, fleeX: cx, fleeZ: cz})
			return
		}
	}

	if target == nil {
		return
	}
	f.manageOffhand(ctx, foes, st.Health, near) // escudo o tótem, según contra qué
	// comer en plena pelea con la comida por debajo de 20: con ella llena (y
	// saturación) la vida vuelve muy deprisa, y por debajo de 18 no vuelve
	// nada. Contra un jugador, en vanilla es lo que más cura (en los logs,
	// contra una persona: el bot a 15-16 de comida toda la pelea, sin curarse;
	// ella de 1 a 10 en 13 s). Si da tiempo a acabar antes de que llegue
	// (1,6 s) y sin precipicio a la espalda
	if isPlayer && st.Food < 20 && f.snack(ctx, st, target, foods, cx, cz) {
		return
	}
	// a uno que no se le gana: encerrarse y que se canse, o pelear con todo
	// (nunca huir: corre igual y pega por la espalda)
	if isPlayer && f.stanceNow == stanceAvoid {
		if target.dist < 9 && f.wantBunker(target) {
			f.startBunker(target, "no le gano: que se canse")
			return
		}
	}
	// ardiendo sin cubo de agua: al agua que haya cerca (fire.go)
	if f.toWater(st, target) {
		return
	}
	// en una telaraña: romperla (expert.go)
	if f.escapeWeb(ctx) {
		return
	}
	// un mazazo en camino: el escudo de cara a él, o el agua, o apartarse (defense.go)
	if isPlayer && f.defendMace(ctx, target, shield) {
		return
	}
	// el picado con élitros y maza (expert.go)
	if isPlayer && f.diveStep(ctx, target, st) {
		return
	}
	// la maza: cayendo encima de él, o el salto con la carga de viento (arsenal.go)
	if f.smashStep(ctx, target, st) {
		return
	}
	// tácticas contra un jugador (tactics.go): tirarle de donde se subió,
	// quitarle el agua o la lava que puso, encerrarle si se queda quieto
	if isPlayer {
		f.trackStill(target)
		// (el cubo sale el tick siguiente: con la espada en la mano no recoge
		// nada; y el bloque del constructor, igual: con la espada no se pone)
		if f.handsBusy.Load() || f.d.Builder != nil && f.d.Builder.Busy() && f.plan.Load().mode == acting {
			f.setPlan(plan{mode: acting, target: target.id})
			return
		}
		if f.cover(target, shield) || f.counterMace(ctx, target) || f.crystalStep(ctx, target, st) || f.anchorStep(ctx, target, st) ||
			f.arsenal(ctx, target, st) || f.rodStep(ctx, target) || f.pearlChase(ctx, target, st) || f.riptide(ctx, target) || f.clearFluids(ctx, target) || f.breakIn(ctx, target) || f.knockDown(ctx, target) || f.cage(target) {
			return
		}
	}
	if f.bowFight(ctx, target) { // lejos y sin llegar a tiempo: flechas (bow.go)
		return
	}
	if f.gear(ctx, near) {
		return // se equipó: el arma se recalcula el tick siguiente
	}
	f.fight(ctx, target, w, slot, shield)
}

// foes son los atacantes que siguen cerca, del más cercano al más lejano.
func (f *Fighter) foes() []foe {
	p := f.d.Player.Position()
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []foe
	for id, a := range f.aggressors {
		e, ok := f.d.Entities.Get(id)
		if !ok {
			f.logf("%s ya no está (muerto o fuera de vista)", a.name)
			delete(f.aggressors, id)
			continue
		}
		if dead(e) { // un jugador muerto sigue ahí hasta que reaparece (en los logs: 3 min "peleando" con él)
			f.logf("%s está muerto", a.name)
			delete(f.aggressors, id)
			continue
		}
		d := math.Sqrt(e.DistanceSqr(p.X, p.Y, p.Z))
		// (uno de la zona sigue siéndolo mientras esté dentro, aunque esté lejos:
		// salir de ella lo decide scanZone)
		limit := float64(chaseMax)
		if id == f.pearlOwner && f.steps-f.pearlStep < 200 {
			limit *= 2 // se fue con una perla (y el bot va detrás): no es alejarse
		}
		if !a.fromZone && (d > limit || (time.Since(a.lastHit) > forgetIdle && d > 16)) {
			f.logf("%s se alejó: lo dejo", a.name)
			delete(f.aggressors, id)
			continue
		}
		out = append(out, foe{aggressor: *a, e: e, dist: d})
	}
	for i := 1; i < len(out); i++ { // pocos elementos: inserción
		for j := i; j > 0 && out[j].dist < out[j-1].dist; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// health es la vida (más absorción) que el servidor muestra del jugador; 20 si no la manda.
func health(e entities.Entity) float64 {
	hp := 20.0
	if v, ok := e.Data[entitydata.LivingEntityHealth]; ok {
		if x, ok := v.Value.(*pk.Float); ok && *x > 0 && *x <= 1024 {
			hp = float64(*x)
		}
	}
	if v, ok := e.Data[entitydata.PlayerPlayerAbsorption]; ok {
		if x, ok := v.Value.(*pk.Float); ok && *x > 0 {
			hp += float64(*x)
		}
	}
	return hp
}

// dead: el servidor dice que tiene 0 de vida (un jugador muerto, en la
// pantalla de muerte, o un mob cayendo muerto).
func dead(e entities.Entity) bool {
	v, ok := e.Data[entitydata.LivingEntityHealth]
	if !ok {
		return false
	}
	x, ok := v.Value.(*pk.Float)
	return ok && *x <= 0
}

// blocking dice si la entidad está levantando un escudo.
func blocking(e entities.Entity) bool {
	v, ok := e.Data[entitydata.LivingEntityLivingEntityFlags]
	if !ok {
		return false
	}
	b, ok := v.Value.(*pk.Byte)
	if !ok || *b&1 == 0 {
		return false
	}
	hand := 0 // 0 mano derecha, 1 izquierda
	if *b&2 != 0 {
		hand = 1
	}
	return itemName(int32(e.Equipment[hand].Item)) == "minecraft:shield" && e.Equipment[hand].Count > 0
}

// pickTarget elige a quién atacar: el que tarda menos en morir contando lo que
// cuesta llegar hasta él. El objetivo actual tiene ventaja para no cambiar a cada rato.
func (f *Fighter) pickTarget(foes []foe, current int32) (*foe, weapon, int, float64) {
	var (
		best      *foe
		bestW     weapon
		bestSlot  int
		bestTTK   float64
		bestScore = math.Inf(1)
	)
	for i := range foes {
		fo := &foes[i]
		armor, tough := armorOf(fo.e.Equipment)
		slot, w, ttk := f.bestWeapon(health(fo.e), armor, tough, false, kind(fo.e))
		if blocking(fo.e) { // con escudo arriba: un hacha se lo desactiva
			if s, aw, t := f.bestWeapon(health(fo.e), armor, tough, true, kind(fo.e)); !math.IsInf(t, 1) {
				slot, w, ttk = s, aw, t
			}
		}
		score := ttk + math.Max(0, fo.dist-reach)/sprintBPS
		if fo.id == current {
			score -= 0.75
		}
		if score < bestScore {
			best, bestW, bestSlot, bestTTK, bestScore = fo, w, slot, ttk, score
		}
	}
	return best, bestW, bestSlot, bestTTK
}

// gear revisa en plena pelea si lleva algo mejor (armadura que recogió, una
// pieza rota, un tótem o escudo) y se lo pone, como una persona: no con un
// enemigo encima, que abrir el inventario es recibir golpes. Dice si se equipó.
func (f *Fighter) gear(ctx context.Context, near float64) bool {
	if f.gearCheck > 0 {
		f.gearCheck--
		return false
	}
	f.gearCheck = 10 // cada medio segundo
	fp := f.fingerprint()
	if f.geared && fp == f.gearPrint {
		return false // el inventario no cambió
	}
	ups := f.armorUpgrades()
	if len(ups) == 0 {
		f.geared, f.gearPrint = true, fp
		return false
	}
	if near < 6 {
		return false // alguien cerca: ponerse armadura son clicks y ticks sin pelear; luego
	}
	if f.geared {
		f.logf("tengo equipo mejor en el inventario: me lo pongo")
	}
	f.equipArmor(ctx)
	f.geared, f.gearPrint = true, f.fingerprint()
	return true
}

func (f *Fighter) fight(ctx context.Context, t *foe, w weapon, slot int, shield bool) {
	cur := *f.plan.Load()
	if cur.target != t.id {
		f.logf("objetivo: %s (vida ~%.0f, a %.1f bloques)", t.name, health(t.e), t.dist)
	}
	slot, w = f.preferSpear(t, slot, w)
	lunge := f.lungeOn() || f.wantLunge(t)
	if lunge {
		slot, _ = f.spearSlot()
		inv, _ := f.inventory()
		w = weaponOf(inv[slot])
	}
	inv, held := f.inventory()
	// subiendo una torre o cruzando un puente hacia él, la mano es del
	// constructor: con el arma en la mano el bloque no se pone nunca
	if slot != slotHotbar+held && !f.d.Walker.Building() {
		if before, now := nameOf(inv[slotHotbar+held]), nameOf(inv[slot]); before != now && now != "" {
			f.logf("arma: %s (mejor contra %s)", strings.TrimPrefix(now, "minecraft:"), t.name)
		}
		if err := f.hold(ctx, slot); err != nil {
			f.logf("arma: %v", err)
		}
	}
	// cuerpo a cuerpo solo si se llega en línea recta (si está en un saliente,
	// detrás de un muro o al otro lado de un hueco, lo lleva el pathfinding);
	// con margen para no alternar en el borde
	p := f.d.Player.Position()
	reach := 5.0
	if cur.closeIn && cur.target == t.id {
		reach = 6.5
	}
	// (con margen de altura: en PvP se salta y los golpes lanzan por el aire)
	// (atrapado en una telaraña o en la lava: su celda no es camino, pero se le pega igual)
	trapped := (f.webbed(t.e) || f.burningIn(t.e)) && boxDistance(p, t.e) <= w.hitReach()+1.5
	closeIn := t.dist <= reach && math.Abs(t.e.Y-p.Y) < 2.2 && (f.d.Walker.Direct(p, t.e.X, t.e.Z) || f.inCage(t) && boxDistance(p, t.e) <= 3.4 || trapped)
	// un creeper: el cuerpo a cuerpo lleva el baile (entrar con la mecha apagada,
	// huir con ella encendida); el walker solo lo acercaría a la explosión
	if t.e.Type == "minecraft:creeper" && t.dist < 12 {
		closeIn = true
	}
	// sin acercarse ni un bloque en 8 s (detrás de un muro, en un hoyo, en
	// una torre): no se llega. Se deja, en vez de dar vueltas en el sitio
	// (en los logs, 2 minutos así, con las órdenes esperando a la pelea)
	// (avanzar es acercarse, o tenerlo a tiro en línea recta: pegado a un muro
	// con él al otro lado no se avanza)
	// (también: venir de retirarse o comer, que no es no llegar)
	// (en la jaula del bot: sin línea recta hasta su celda, pero a tiro)
	near := t.dist <= 4.5 && f.d.Walker.Direct(p, t.e.X, t.e.Z) || f.inCage(t) && boxDistance(p, t.e) <= reach || trapped // a tiro, aunque salte (un jugador en PvP); o al alcance del golpe (encerrado: sin línea recta hasta su celda)
	// subiendo una torre hacia él (se subió a unos bloques): poner bloques
	// también es avanzar, aunque la distancia apenas cambie
	climbing := f.following == t.id && f.d.Walker.Climbing() && t.e.Y > p.Y+0.5
	if f.chaseID != t.id || t.dist < f.chaseBest-1 || near || climbing || f.steps-f.fightStep > 1 {
		f.chaseID, f.chaseBest, f.chaseAt = t.id, t.dist, f.steps
	} else if t.dist < 6 && f.steps-f.chaseAt > 30 && f.breakThrough(ctx, t) {
		return // (un muro entre los dos, él al otro lado: se rompe; defense.go)
	} else if f.steps-f.chaseAt > 160 {
		f.logf("no llego a %s (a %.1f bloques, 8 s sin acercarme): lo dejo", t.name, t.dist)
		f.giveUp(t.id)
		return
	}
	// un tirador, con escudo y en línea recta: el acercamiento también es
	// nuestro (mirándolo, para que el escudo lo tenga delante cuando tensa)
	if rangedMobs[kind(t.e)] && shield && t.dist < 20 && math.Abs(t.e.Y-p.Y) < 2 && f.d.Walker.Direct(p, t.e.X, t.e.Z) {
		closeIn = true
	}
	// uno que pega de cerca, con escudo: no se corre a por él con el
	// pathfinding (sin escudo arriba y sin el golpe cargado, que es comerse el
	// primero); se le espera de cara con el escudo (thinkFight)
	if !rangedMobs[kind(t.e)] && shield && t.dist < 9 && math.Abs(t.e.Y-p.Y) < 2 && f.d.Walker.Direct(p, t.e.X, t.e.Z) {
		closeIn = true
	}
	// la torre, hasta arriba: pegar desde media torre (a 1 bloque por debajo,
	// en una columna de 1×1) es caerse con el primer golpe y empezar otra
	if climbing && t.e.Type != "minecraft:creeper" {
		closeIn = false
	}
	f.fightStep = f.steps
	// lanzó una perla: a donde cae, no a donde está (ni cuerpo a cuerpo: ya se va)
	pearl := f.chasePearl(t.id, t.name)
	if pearl {
		closeIn = false
		f.pearlOwner, f.pearlStep = t.id, f.steps
	}
	if lunge && nameOf(func() screen.Slot { inv, h := f.inventory(); return inv[slotHotbar+h] }()) == nameOf(inv[slot]) {
		if !f.lungeOn() {
			f.logf("embestida con la lanza contra %s (a %.1f)", t.name, t.dist)
			f.ars.lungeNext, f.ars.lungeAt = f.steps+lungeEvery, f.steps
		}
		closeIn = true
	} else {
		lunge = false
	}
	// un cristal cerca que le quitaría bastante: el escudo de cara a él (expert.go)
	guard, gx, gy, gz := false, 0.0, 0.0, 0.0
	if c, ok := f.crystalThreat(t, f.d.Player.Status()); ok && shield {
		guard, gx, gy, gz = true, c.X, c.Y+0.5, c.Z
		closeIn = true
	}
	f.setPlan(plan{
		guard: guard, gx: gx, gy: gy, gz: gz,
		mode: fighting, target: t.id, closeIn: closeIn, weapon: w, shield: shield, stance: f.stanceNow, holdFire: f.tac.approach && f.tac.id == t.id, lunge: lunge,
		finish:      t.e.Type == "minecraft:player" && f.finishable(t, w),
		noCrit:      blocking(t.e) && isAxe(func() string { inv, h := f.inventory(); return nameOf(inv[slotHotbar+h]) }()),
		hisCooldown: f.hisCooldown(t),
	})
	if pearl {
		return
	}
	id := t.id
	if !closeIn && f.following != id {
		f.d.Walker.SetSprint(true)
		near := 2.5
		if w.stab { // (con la lanza, a su distancia: no dentro de su alcance)
			near = 3.8
		}
		f.d.Walker.Follow(func() (float64, float64, float64, bool) {
			e, ok := f.d.Entities.Get(id)
			return e.X, e.Y, e.Z, ok
		}, near)
		f.following = id
	} else if closeIn && f.following != 0 {
		f.d.Walker.Stop()
		f.following = 0
	}
}

// trade: si el intercambio de golpes con t lo gana el bot (le hacen falta
// tantos golpes como a él, o menos: el bot pega primero si tiene el golpe
// listo). Dice el porqué, para el log.
func (f *Fighter) trade(t *foe, st basic.Status) (bool, string) {
	if t.e.Type == "minecraft:player" { // con su equipo entero: filo, protección, escudo, tótem (matchup.go)
		m := f.assess(t.e, st)
		return m.ratio >= 1, fmt.Sprintf("le hacen falta %d golpes (%.1f s) y a mí me matan en %d (%.1f s)", m.myHits, m.myTime, m.hisHits, m.hisTime)
	}
	armor, tough := armorOf(t.e.Equipment)
	_, w, _ := f.bestWeapon(health(t.e), armor, tough, false, kind(t.e))
	mine := afterArmor(w.damage, armor, tough)
	myArmor, myTough := f.myArmor()
	theirs := afterArmor(theirHit(t.e), myArmor, myTough)
	if mine <= 0 || theirs <= 0 {
		return mine > 0, "sin datos"
	}
	me := math.Ceil(health(t.e) / mine)                               // golpes que le hacen falta al bot
	them := math.Ceil((float64(st.Health) + absorption(st)) / theirs) // y a él
	return me <= them, fmt.Sprintf("le hacen falta %.0f golpes y a mí me matan en %.0f", me, them)
}

// absorption: los corazones amarillos (manzana dorada, tótem), según el efecto.
func absorption(st basic.Status) float64 {
	if e, ok := st.Effects["minecraft:absorption"]; ok {
		return 4 * float64(e.Amplifier+1)
	}
	return 0
}

// myArmor es la armadura que lleva puesta el bot (armadura y dureza).
func (f *Fighter) myArmor() (armor, toughness float64) {
	inv, _ := f.inventory()
	for i := slotArmorHead; i <= slotArmorHead+3; i++ {
		a, t := armorPiece(inv[i])
		armor += a
		toughness += t
	}
	return
}

// theirHit es lo que pega e de un golpe: el arma que lleva si es un jugador,
// lo de su especie si es un mob (dificultad normal).
func theirHit(e entities.Entity) float64 {
	if e.Type == "minecraft:player" {
		if it, ok := e.Equipment[0]; ok && it.Count > 0 {
			if w := weaponOf(screen.Slot(it)); w.damage > 1 {
				return w.damage
			}
		}
		return 1
	}
	if d, ok := mobHit[kind(e)]; ok {
		return d
	}
	return 4
}

// mobHit: lo que pega cada mob de un golpe (dificultad normal).
var mobHit = map[string]float64{"zombie": 3, "husk": 3, "drowned": 3, "zombie_villager": 3, "spider": 2, "cave_spider": 2,
	"skeleton": 3, "stray": 3, "bogged": 3, "vindicator": 13, "piglin_brute": 13, "ravager": 12, "witch": 6, "blaze": 6,
	"wither_skeleton": 8, "hoglin": 6, "zoglin": 6, "enderman": 7, "slime": 4, "magma_cube": 6, "phantom": 2, "silverfish": 1}

// giveUp deja de pelear con id y no le busca pelea en un minuto (si le pega, sí).
func (f *Fighter) giveUp(id int32) {
	f.mu.Lock()
	delete(f.aggressors, id)
	if f.ignored == nil {
		f.ignored = map[int32]uint64{}
	}
	f.ignored[id] = f.steps + 1200
	f.mu.Unlock()
	f.chaseID = 0
	f.stopMoving()
	f.setPlan(plan{})
}

// retreat huye de (cx, cz) y come cuando hay distancia.
// retreat huye de (cx, cz) con el pathfinding (saltos, escalones, sin lava ni
// caídas) y come cuando hay distancia.
func (f *Fighter) retreat(ctx context.Context, st basic.Status, foods []food, near, cx, cz float64) {
	if near >= 6 {
		if fd, ok := pickFood(foods, 20-st.Food, false); ok && st.Food < 20 {
			f.eat(ctx, fd, cx, cz)
			return
		}
	}
	// un sitio nuevo al que huir: al empezar, al llegar o fallar, y cada 2 s (los enemigos se mueven)
	if f.following != fleeing || f.d.Walker.Status() != "walking" || time.Since(f.fleeAt) > 2*time.Second {
		if goal, ok := f.d.Walker.FleeGoal(f.d.Player.Position(), cx, cz); ok {
			f.d.Walker.SetSprint(true)
			f.d.Walker.Goto(goal)
			f.following, f.fleeAt = fleeing, time.Now()
			f.noWayOut = 0
		} else {
			f.noWayOut = f.steps // (sin sitio al que huir: la torre, si hay bloques)
		}
	}
	f.setPlan(plan{mode: retreating, fleeX: cx, fleeZ: cz})
}

// standFirm: con poca vida contra un jugador y sin búnker posible: no se
// huye, se sigue peleando (lo dice en el log cada 5 s).
func (f *Fighter) standFirm(t *foe, st basic.Status) {
	f.standUntil = time.Now().Add(8 * time.Second)
	if time.Since(f.lastStandLog) > 5*time.Second && t != nil {
		f.logf("vida %.1f contra %s y sin búnker: no huyo, peleo", st.Health, t.name)
		f.lastStandLog = time.Now()
	}
}

// fleeing es el valor de following mientras el walker huye (Goto, no Follow).
const fleeing int32 = -1

// eat come fd quieto (comer y moverse a la vez delata a un bot) y vuelve al plan anterior.
func (f *Fighter) eat(ctx context.Context, fd food, cx, cz float64) {
	prev := *f.plan.Load()
	f.stopMoving()
	f.setPlan(plan{mode: eating, fleeX: cx, fleeZ: cz})
	if err := f.d.Ctl.WaitTicks(ctx, 1); err != nil { // que Think baje el escudo
		return
	}
	inv, _ := f.inventory()
	name := nameOf(inv[fd.slot])
	if err := f.hold(ctx, fd.slot); err != nil {
		f.logf("comer: %v", err)
		f.setPlan(prev)
		return
	}
	// en la mano cuando el servidor lo confirma (con lag, unos ticks): comer
	// antes es "lo que tengo en la mano no se come"
	inHand := ""
	for i := 0; i < 10; i++ {
		if err := f.d.Ctl.WaitTicks(ctx, 1); err != nil {
			f.setPlan(prev)
			return
		}
		inv, h := f.inventory()
		if inHand = nameOf(inv[slotHotbar+h]); inHand == name {
			break
		}
	}
	if inHand != name {
		f.logf("comer %s: no me llega a la mano (tengo %s); luego", name, inHand)
		f.setPlan(prev)
		return
	}
	done := make(chan act.Use, 1)
	f.d.Hands.Eat(func(u act.Use) { done <- u })
	select {
	case u := <-done:
		if u.Err != nil {
			f.logf("comer %s: %v", name, u.Err)
		} else {
			f.logf("comí %s (vida %.1f)", name, f.d.Player.Status().Health)
		}
	case <-time.After(5 * time.Second):
	case <-ctx.Done():
	}
	if prev.mode == idle {
		f.setPlan(plan{})
	} else {
		f.setPlan(plan{mode: retreating, fleeX: cx, fleeZ: cz})
	}
}

// upkeep: fuera de combate come si tiene hambre, como cualquier jugador.
func (f *Fighter) upkeep(ctx context.Context, st basic.Status) {
	if !f.EatWhenIdle || f.lastUpkeep != 0 && f.steps-f.lastUpkeep < 100 || f.d.Hands.Busy() { // cada 5 s
		return
	}
	f.lastUpkeep = f.steps
	if st.Food > 16 && !(st.Food < 20 && st.Health < 16) {
		return
	}
	// con poca vida se come lo que sea (sin hambre 18 no se cura), aunque sobre
	if fd, ok := pickFood(f.foods(), 20-st.Food, false); ok && (fd.nutrition <= 20-st.Food+2 || st.Health < 12) {
		p := f.d.Player.Position()
		f.eat(ctx, fd, p.X, p.Z)
	}
}

// safeFrom es a qué distancia se está a salvo de estos enemigos: 16 de los
// que pegan de cerca, 28 de los que disparan (un esqueleto, un stray).
func safeFrom(foes []foe) float64 {
	for _, fo := range foes {
		if rangedMobs[kind(fo.e)] {
			return 28
		}
	}
	return 16
}

func (f *Fighter) stopMoving() {
	if f.following != 0 {
		f.d.Walker.Stop()
		f.following = 0
	}
	f.d.Walker.SetSprint(false)
}

// hisCooldown: los ticks entre dos golpes de t con el arma más fuerte que se
// le ha visto (matchup.go); 0 si no es un jugador o no se sabe.
func (f *Fighter) hisCooldown(t *foe) float64 {
	if t.e.Type != "minecraft:player" || f.matchID != t.id || f.match.them.speed <= 0 {
		return 0
	}
	return 20 / f.match.them.speed
}
