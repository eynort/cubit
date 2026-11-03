// Package pathing es el movimiento del bot: búsqueda A* con saltos de
// huecos, diagonales, caídas y escaleras (graph.go), y un ejecutor que
// camina el camino como un jugador: recorta esquinas, salta en el momento
// justo, no retrocede al recalcular y persigue en línea recta cuando puede.
package pathing

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/path"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// Walker camina caminos: pon su Think como Think del controlador (o llámalo
// desde el tuyo) y usa Goto o Follow.
type Walker struct {
	emptyPaths int // búsquedas seguidas que no dieron ni un paso (goto)
	// siguiendo en línea recta: desde dónde y cuándo (si no avanza, algo lo
	// frena que la línea no ve), y hasta qué tick no se va en línea recta
	directFrom     [2]float64
	directAt       int
	noDirectBefore int
	anchor         [3]float64 // dónde estaba (x, z, y) al empezar a contar el atasco
	anchorAt       int        // en qué tick
	g              *graph
	// MaxNodes limita una búsqueda (por defecto 60000).
	MaxNodes int
	sprint   atomic.Bool
	building atomic.Int64 // cuándo pidió un bloque por última vez para una torre o un puente (UnixNano)
	noSprint atomic.Bool  // prohibido correr (manda sobre SetSprint)
	noBuild  atomic.Bool  // sin poner bloques en los caminos (SetBuild)
	noBridge atomic.Bool  // sin puentes, pero con torres (SetBridges)
	noHurt   atomic.Bool  // sin caídas que hagan daño (SetSafeFalls)

	mu       sync.Mutex
	job      job
	path     []Node
	i        int
	done     bool // el camino llega a la meta (no es parcial)
	timedOut bool // el camino es parcial porque la búsqueda se quedó sin tiempo
	status   string
	release  bool

	gen        int
	cancel     context.CancelFunc
	searching  bool
	searchGoal world.BlockPos
	res        *searched
	lastPlan   int
	retryAt    int
	ticks      int

	best    float64
	since   int
	replans int
	strafe  int
	hover   bool
	last    basic.Position
	lastI   int // nodo del tick anterior: al cambiar, el atasco se mide de nuevo

	// correcciones del servidor (Rejected): pausa y celdas que evitar
	pauseUntil int
	rejects    map[world.BlockPos]int                    // veces que el servidor rechazó ir a esa celda
	avoid      map[world.BlockPos]int                    // celda → tick hasta el que se evita
	passable   atomic.Pointer[func(world.BlockPos) bool] // celdas que se pueden romper para pasar (SetPassable)
	climbUntil int                                       // hasta este tick subir/saltar huecos cuesta mucho más

	boost     float64 // impulso extra de salto (efecto de salto: 0.1 por nivel)
	health    float32 // la vida (cuánta caída se puede permitir)
	wasAir    bool    // el tick anterior estaba en el aire (saltando)
	jumpFails int     // saltos hacia el nodo jumpNode que acabaron abajo
	// techos sobre la torre (capBlocked): el que se está rompiendo, desde
	// cuándo, y cuántos se rompieron en cada columna en este viaje
	capCell  world.BlockPos
	capAt    int
	capTime  int
	caps     map[[2]int]int
	jumpNode int

	lastBuildPlan [2]int  // torre y puente del último camino (para no repetir el aviso)
	builder       Builder // pone bloques (puentes, torres); nil: no construye
	buildAt       int     // tick en que se pidió el último bloque
	buildNode     int     // el nodo (índice) para el que se pidió

	mem   *Memory          // rutas que salieron bien y trabas (memory.go)
	trail []world.BlockPos // las celdas que va pisando este trabajo (goto)
}

// Builder pone bloques para el walker (puentes y torres): con el bloque de
// construir en la mano, contra la cara de otro bloque.
type Builder interface {
	// Blocks son los bloques de construir que lleva (0: no se construye).
	Blocks() int
	// Place empieza a poner un bloque en pos contra against; false si no
	// pudo empezar (p.ej. el bloque aún no está en la mano: se reintenta).
	Place(pos, against world.BlockPos) bool
	// Busy: está poniendo uno.
	Busy() bool
}

// Breaker es un Builder que además rompe lo blando que tapa el paso (hojas).
type Breaker interface {
	// Break empieza a romper el bloque de pos; false si no pudo empezar.
	Break(pos world.BlockPos) bool
}

// CapBreaker es un Breaker que sabe lo que tarda en romper un bloque con lo
// mejor que lleva, y lo rompe con eso (un techo sobre la torre).
type CapBreaker interface {
	BreakTicks(st block.StateID) (ticks, hotbar int)
	BreakBest(pos world.BlockPos, st block.StateID) bool
}

// SetBuilder le da al walker con qué construir (nil: no construye).
func (w *Walker) SetBuilder(b Builder) {
	w.mu.Lock()
	w.builder = b
	w.mu.Unlock()
}

type job struct {
	active bool
	goal   world.BlockPos
	near   float64
	follow func() (x, y, z float64, ok bool)
}

type searched struct {
	gen  int
	goal world.BlockPos
	res  result
	err  error
}

// New crea un walker sobre el mundo w.
func New(w physics.Blocks, _ *basic.Player, tags physics.Tags) *Walker {
	wk := &Walker{g: newGraph(w, tags), status: "stopped", mem: NewMemory()}
	wk.sprint.Store(true)
	return wk
}

// SetBuild permite o prohíbe poner bloques en los caminos (torres, puentes):
// la pelea lo prohíbe persiguiendo a un jugador (un puente al borde, con sus
// empujones, es caerse). Por defecto, permitido.
func (w *Walker) SetBuild(b bool) { w.noBuild.Store(!b) }

// SetSafeFalls: true, los caminos no tienen caídas que hagan daño (una tarea
// que sube y baja todo el rato, como construir, las va sumando).
func (w *Walker) SetSafeFalls(b bool) { w.noHurt.Store(b) }

// SetPassable: las celdas para las que p dice true se pueden romper para
// pasar, como las hojas (los andamios de una construcción: sin eso el bot se
// encierra entre sus torres y las paredes). nil: solo lo blando.
func (w *Walker) SetPassable(p func(world.BlockPos) bool) {
	if p == nil {
		w.passable.Store(nil)
		return
	}
	w.passable.Store(&p)
}

func (w *Walker) passableCell(c world.BlockPos) bool {
	p := w.passable.Load()
	return p != nil && (*p)(c)
}

// SetBridges permite o prohíbe solo los puentes (las torres siguen, si
// SetBuild las deja): peleando con un jugador que se sube a unos bloques, se
// sube detrás con una torre (en el sitio, sin borde del que caerse), pero no
// se tienden puentes junto a él. Por defecto, permitidos.
func (w *Walker) SetBridges(b bool) { w.noBridge.Store(!b) }

// SetMemory cambia la memoria de caminos (compartirla, o una cargada de un
// archivo); Memory la devuelve.
func (w *Walker) SetMemory(m *Memory) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if m == nil {
		m = NewMemory()
	}
	w.mem = m
}

func (w *Walker) Memory() *Memory {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.mem
}

// avoidCell: esa celda no se pisa en un rato (600 ticks) y se apunta como traba.
func (w *Walker) avoidCell(c world.BlockPos) {
	if w.avoid == nil {
		w.avoid = map[world.BlockPos]int{}
	}
	w.avoid[c] = w.ticks + 600
	w.mem.snag(c)
}

// SetSprint elige si corre (por defecto sí).
func (w *Walker) SetSprint(b bool) { w.sprint.Store(b) }

// AllowSprint permite o prohíbe correr del todo (orden "sprint on|off"):
// manda sobre SetSprint y sobre el combate.
func (w *Walker) AllowSprint(b bool) { w.noSprint.Store(!b) }

// SprintAllowed dice si se permite correr.
func (w *Walker) SprintAllowed() bool { return !w.noSprint.Load() }

// Goto empieza a caminar hasta goal.
func (w *Walker) Goto(goal world.BlockPos) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.job.active && w.job.follow == nil && w.job.goal == goal {
		return
	}
	w.reset()
	near := 0.0
	if _, ok := w.g.floor(goal); !ok {
		near = 1.5 // una meta que no es suelo: basta con llegar al lado
	}
	w.job = job{active: true, goal: goal, near: near}
	w.status = "walking"
}

// Follow se mantiene a near bloques de lo que diga target hasta Stop.
func (w *Walker) Follow(target func() (x, y, z float64, ok bool), near float64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.reset()
	w.job = job{active: true, follow: target, near: near}
	w.status = "following"
}

// PillarUp sube n bloques en el sitio (pos: donde está el jugador) (una torre: saltar y poner un bloque
// debajo, como un camino de nodos Pillar). Para que un mob que pega de cerca
// no llegue (un zombi alcanza 1,4 bloques: a 2 de altura, nada). false sin
// bloques bastantes.
func (w *Walker) PillarUp(pos basic.Position, n int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.builder == nil || w.builder.Blocks() < n || n <= 0 {
		return false
	}
	f := world.BlockPos{X: int(math.Floor(pos.X)), Y: int(math.Floor(pos.Y + 1e-6)), Z: int(math.Floor(pos.Z))}
	w.reset()
	w.last = pos
	p := []Node{{Pos: f, Floor: float64(f.Y), Kind: path.Walk}}
	for i := 1; i <= n; i++ {
		p = append(p, Node{Pos: world.BlockPos{X: f.X, Y: f.Y + i, Z: f.Z}, Floor: float64(f.Y + i), Kind: Pillar})
	}
	w.job = job{active: true, goal: p[n].Pos}
	w.path, w.done, w.i = p, true, 1
	w.status = "walking"
	return true
}

// Stop deja de caminar y suelta las teclas.
func (w *Walker) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.reset()
	w.status = "stopped"
}

// Status es "walking", "following", "arrived", "stopped" o "failed: …".
func (w *Walker) Status() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status
}

func (w *Walker) reset() {
	w.gen++
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	w.job = job{}
	w.path, w.i, w.done = nil, 0, false
	w.searching, w.res, w.retryAt = false, nil, 0
	w.replans, w.since, w.best, w.hover, w.strafe = 0, 0, math.MaxFloat64, false, 0
	w.emptyPaths, w.anchorAt = 0, w.ticks
	w.buildNode, w.jumpNode, w.jumpFails = -1, -1, 0
	w.trail = nil
	w.caps, w.capAt = nil, 0
}

func (w *Walker) end(s *control.State, status string) {
	if status == "arrived" && w.job.active && w.job.follow == nil && len(w.trail) > 1 {
		w.mem.remember(lastLeg(w.trail))
	}
	w.reset()
	w.status = status
	s.Input = control.Input{}
}

// Think es un tick de caminar.
// floatUp: quieto con la cabeza bajo el agua se hunde y se ahoga; nada hacia arriba.
// Los ojos, a 1.6 de los pies; planeando con élitros (que en el agua siguen
// abiertos: solo los cierra tocar suelo), a 0.4. (En los logs: flotando
// quieto con el agua a la altura de los ojos de planear, el agua a 1.6 no
// estaba: "respiraba"; se ahogó.)
func (w *Walker) floatUp(s *control.State, bp *basic.Player) bool {
	p := bp.Position()
	eye := 1.6
	if s.FallFlying {
		eye = 0.4
	}
	if w.g.water(world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y + eye)), Z: int(math.Floor(p.Z))}) {
		s.Input.Jump = true
		return true
	}
	return false
}

func (w *Walker) Think(s *control.State, bp *basic.Player) {
	w.mu.Lock()
	defer w.mu.Unlock()
	defer func() {
		if w.noSprint.Load() {
			s.Input.Sprint = false
		}
	}()
	if !w.job.active {
		if w.release {
			s.Input = control.Input{}
			w.release = false
		}
		if w.floatUp(s, bp) {
			w.release = true
		}
		return
	}
	w.release = true
	w.ticks++
	pos := bp.Position()
	w.boost = 0
	st := bp.Status()
	w.health = st.Health
	if e, ok := st.Effects["minecraft:jump_boost"]; ok {
		w.boost = 0.1 * float64(e.Amplifier+1) // un beacon de salto: se sube más alto
	}
	if w.ticks < w.pauseUntil { // el servidor acaba de corregir: quieto un momento
		s.Input = control.Input{}
		w.last = pos
		return
	}
	speed := math.Hypot(pos.X-w.last.X, pos.Z-w.last.Z)
	if speed > 1 {
		speed = 0 // un teletransporte
	}
	w.last = pos

	if r := w.res; r != nil {
		w.res = nil
		if w.take(s, r, pos) {
			return
		}
	}

	if f := w.job.follow; f != nil {
		tx, ty, tz, ok := f()
		if !ok {
			s.Input = control.Input{}
			w.floatUp(s, bp)
			return
		}
		hd, vd := math.Hypot(tx-pos.X, tz-pos.Z), ty-pos.Y
		// margen: se para a near y no vuelve a andar hasta near+0.8 (sin tirones)
		// solo se para de pie y a la altura del objetivo: parar en pleno salto
		// hacia su escalón es caerse al de abajo
		if w.hover {
			w.hover = hd <= w.job.near+0.8 && math.Abs(vd) < 1
		} else {
			w.hover = hd <= w.job.near && math.Abs(vd) < 0.6 && s.OnGround
		}
		if w.hover {
			w.path = nil
			s.Input = control.Input{}
			w.turn(bp, yawTo(pos, tx, tz), 10, 30)
			return
		}
		// cerca y en línea recta: directo, sin camino
		if hd <= 7 && math.Abs(vd) <= 1.2 && !w.inWater(pos) && w.lineWalkable(pos, tx, tz, true) && w.directMoving(pos) {
			w.path = nil
			w.steerDirect(s, bp, pos, tx, tz, ty, speed)
			return
		}
		goal := w.cell(tx, ty, tz)
		moved := goal != w.job.goal
		// solo con los pies en el suelo: en pleno salto la búsqueda saldría de la celda de abajo.
		// Mientras llega el camino nuevo se sigue andando el viejo.
		// una búsqueda en curso no se cancela porque el objetivo se mueva: una
		// lejana tarda segundos (en el móvil más), y cancelarla cada medio
		// segundo era no terminar ninguna: el bot quieto, "trabado", mientras
		// el que sigue andaba. Su camino lleva a donde estaba, y desde ahí se
		// busca otra vez. Solo si se fue lejos de esa meta y ya lleva 5 s.
		far := func() bool {
			dx, dy, dz := goal.X-w.searchGoal.X, goal.Y-w.searchGoal.Y, goal.Z-w.searchGoal.Z
			return dx*dx+dy*dy+dz*dz > 16*16
		}
		again := (w.path == nil && !w.searching) || (moved && (!w.searching || w.ticks-w.lastPlan >= 100 && far()))
		if again && w.grounded(s, pos) && w.ticks >= w.retryAt && w.ticks-w.lastPlan >= 4 {
			w.job.goal = goal
			w.plan(s, pos)
		}
	} else if w.path == nil && !w.searching && w.grounded(s, pos) {
		if w.trail == nil {
			c, _ := w.g.start(pos.X, pos.Y, pos.Z)
			w.trail = append(w.trail, c.Pos)
		}
		w.plan(s, pos)
	}
	if r := w.res; r != nil && s.Stepped { // juego por pasos: la búsqueda ya terminó
		w.res = nil
		if w.take(s, r, pos) {
			return
		}
	}
	if w.path == nil {
		if w.job.follow != nil { // mientras busca, hacia el objetivo si se puede
			tx, ty, tz, _ := w.job.follow()
			if !w.inWater(pos) && w.lineWalkable(pos, tx, tz, true) && w.directMoving(pos) {
				w.steerDirect(s, bp, pos, tx, tz, ty, speed)
				return
			}
		}
		fp := world.BlockPos{X: int(math.Floor(pos.X)), Y: int(math.Floor(pos.Y)), Z: int(math.Floor(pos.Z))}
		if w.g.water(fp) || w.g.water(world.BlockPos{X: fp.X, Y: fp.Y + 1, Z: fp.Z}) {
			s.Input = control.Input{Jump: true} // en el agua sin camino: a flote mientras se busca
			return
		}
		if !w.grounded(s, pos) {
			s.Input.Jump = false // en el aire: se mantienen las teclas (soltarlas es caer corto)
			return
		}
		s.Input = control.Input{}
		return
	}
	w.walk(s, bp, pos, speed)
}

// directMoving: ir en línea recta hacia lo que se sigue sigue valiendo. Si en
// 3 s no se ha movido medio bloque, algo lo frena que la línea no ve (en los
// logs, 2 minutos pulsando W contra lo mismo): 10 s sin línea recta, solo el
// camino (y si no hay camino, quieto, que es lo honrado).
func (w *Walker) directMoving(pos basic.Position) bool {
	if w.ticks < w.noDirectBefore {
		return false
	}
	if w.ticks-w.directAt > 80 { // (no venía de ir recto: se empieza a contar)
		w.directFrom, w.directAt = [2]float64{pos.X, pos.Z}, w.ticks
	}
	if math.Hypot(pos.X-w.directFrom[0], pos.Z-w.directFrom[1]) > 0.5 {
		w.directFrom, w.directAt = [2]float64{pos.X, pos.Z}, w.ticks
		return true
	}
	if w.ticks-w.directAt > 60 {
		w.noDirectBefore = w.ticks + 200
		w.directAt = 0
		log.Printf("pathing: en línea recta no avanzo (algo me frena): 10 s solo por camino")
		return false
	}
	return true
}

// inWater: el cuerpo está en el agua (los pies, o flotando justo encima). En
// línea recta no se va desde ahí: no hay suelo del que saltar a una orilla, y
// la línea no lo ve (en los logs, siguiendo a alguien a 3 bloques, minutos
// contra una orilla alta, tirando el camino que sí salía por otro lado).
func (w *Walker) inWater(pos basic.Position) bool {
	p := world.BlockPos{X: int(math.Floor(pos.X)), Y: int(math.Floor(pos.Y + 1e-6)), Z: int(math.Floor(pos.Z))}
	return w.g.water(p) || w.g.water(world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z}) && pos.Y-float64(p.Y) < 0.5
}

// grounded: de pie (o en agua / escalera, donde no hay suelo que pisar).
func (w *Walker) grounded(s *control.State, pos basic.Position) bool {
	if s.OnGround {
		return true
	}
	p := world.BlockPos{X: int(math.Floor(pos.X)), Y: int(math.Floor(pos.Y)), Z: int(math.Floor(pos.Z))}
	// flotando en la superficie los pies quedan sobre el agua: también vale
	return w.g.water(p) || w.g.water(world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z}) || w.g.climbable(p)
}

func (w *Walker) cell(x, y, z float64) world.BlockPos {
	if n, ok := w.g.start(x, y, z); ok {
		return n.Pos
	}
	return world.BlockPos{X: int(math.Floor(x)), Y: int(math.Floor(y + 1e-6)), Z: int(math.Floor(z))}
}

// searchBudget es lo más que dura una búsqueda en segundo plano: después se
// anda hacia lo más cercano que encontró y se busca otra vez desde allí. En
// los logs, siguiendo a alguien lejos, más de un minuto quieto esperando una.
const searchBudget = 2 * time.Second

// plan pide una búsqueda desde pos a la meta del trabajo: en otra goroutine
// (en un juego por pasos, aquí mismo).
func (w *Walker) plan(s *control.State, pos basic.Position) {
	if w.cancel != nil {
		w.cancel()
	}
	start, _ := w.g.start(pos.X, pos.Y, pos.Z)
	goal, gen := w.job.goal, w.gen
	near := w.job.near
	if w.job.follow != nil {
		near = 1.5
	}
	max := w.MaxNodes
	if max == 0 {
		max = 60000
	}
	g := newGraph(w.g.w, w.g.kit.Tags) // cada búsqueda con su memoria
	g.build = w.builder != nil && w.builder.Blocks() > 0 && !w.noBuild.Load()
	g.noBridge = w.noBridge.Load()
	if _, ok := w.builder.(Breaker); ok && w.builder != nil {
		g.breakSoft = true
		if p := w.passable.Load(); p != nil {
			g.passable = *p
		}
	}
	if cb, ok := w.builder.(CapBreaker); ok && g.build {
		g.digTicks = func(st block.StateID) int { t, _ := cb.BreakTicks(st); return t }
	}
	g.jump = jumpHeight(w.boost)
	g.fallBudget = math.Max(0, float64(w.health)-8) // bajar dañándose, sin quedar con menos de 8 de vida
	if w.noHurt.Load() {
		g.fallBudget = 0 // (construyendo: se sube y se baja mil veces; cada caída suma)
	}
	g.health = float64(w.health)
	if w.health <= 0 {
		g.health = 20
	}
	if w.health < 6 {
		g.maxFall = 2 // con muy poca vida, ni una caída de 3 (un mal aterrizaje la alarga)
	}
	if w.ticks < w.climbUntil {
		g.climbPenalty = 15 // el servidor rechaza las subidas: rodear por llano si se puede
	}
	for c, until := range w.avoid {
		if w.ticks < until {
			if g.avoid == nil {
				g.avoid = map[world.BlockPos]bool{}
			}
			g.avoid[c] = true
		} else {
			delete(w.avoid, c)
		}
	}
	// la meta pedida no se evita: quien la pide la acaba de abrir (un túnel
	// cavado hasta ella); evitada, no había camino posible
	delete(g.avoid, goal)
	g.penalty = w.mem.penalties()
	mem := w.mem
	search := func(ctx context.Context) (result, error) {
		if res, ok := mem.reuse(g, start, goal, near); ok {
			log.Printf("pathing: camino conocido hacia %v (%d pasos, sin buscar)", goal, len(res.path))
			return res, nil
		}
		mem.Searched.Add(1)
		return g.find(ctx, start, goal, near, max)
	}
	w.lastPlan = w.ticks
	if s.Stepped {
		res, err := search(context.Background())
		w.res = &searched{gen: gen, goal: goal, res: res, err: err}
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel, w.searching, w.searchGoal = cancel, true, goal
	g.budget = searchBudget
	go func() {
		t0 := time.Now()
		res, err := search(ctx)
		if d := time.Since(t0); d > searchBudget/2 && ctx.Err() == nil {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			log.Printf("pathing: búsqueda lenta: %.1f s, %d nodos (sin tiempo=%v), memoria %d MB, %d recolecciones",
				d.Seconds(), res.searched, res.timedOut, m.HeapAlloc>>20, m.NumGC)
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		defer cancel()
		if ctx.Err() != nil || gen != w.gen {
			return
		}
		w.searching, w.cancel = false, nil
		w.res = &searched{gen: gen, goal: goal, res: res, err: err}
	}()
}

// take aplica el resultado de una búsqueda; dice si el tick terminó.
func (w *Walker) take(s *control.State, r *searched, pos basic.Position) bool {
	if r.gen != w.gen {
		return false
	}
	if r.err != nil {
		if errors.Is(r.err, context.Canceled) {
			return false
		}
		if w.job.follow != nil {
			w.path, w.retryAt = nil, w.ticks+20 // inalcanzable por ahora: otra vez en un segundo
			return false
		}
		w.end(s, "failed: "+r.err.Error())
		return true
	}
	if len(r.res.path) == 0 {
		if w.job.follow == nil && r.res.complete {
			w.end(s, "arrived")
			return true
		}
		// un camino vacío y sin llegar: no hay forma de acercarse más; tres
		// seguidas, se dice (antes se buscaba otra vez sin fin, quieto)
		if w.job.follow == nil && !r.res.timedOut {
			if w.emptyPaths++; w.emptyPaths >= 3 {
				w.end(s, "failed: no me puedo acercar más")
				return true
			}
		}
		w.path = nil
		return false
	}
	w.emptyPaths = 0
	w.path, w.done, w.timedOut = r.res.path, r.res.complete, r.res.timedOut
	// avisar cuando el camino construye (una vez por camino distinto)
	pillars, bridges := 0, 0
	for _, n := range w.path {
		switch n.Kind {
		case Pillar:
			pillars++
		case Bridge:
			bridges++
		}
	}
	if k := [2]int{pillars, bridges}; k != w.lastBuildPlan {
		w.lastBuildPlan = k
		if pillars+bridges > 0 {
			log.Printf("pathing: el camino construye: %d de torre, %d de puente", pillars, bridges)
		}
	}
	w.i = splice(w.path, pos)
	w.best, w.since, w.lastI = math.MaxFloat64, 0, w.i
	w.buildNode, w.jumpNode, w.jumpFails = -1, -1, 0
	return false
}

// walked apunta en el rastro los nodos del camino que se acaban de pasar (de from a w.i).
func (w *Walker) walked(from int) {
	if w.job.follow != nil {
		return
	}
	for k := from; k < w.i && k < len(w.path); k++ {
		c := w.path[k].Pos
		if n := len(w.trail); n > 0 && w.trail[n-1] == c {
			continue
		}
		w.trail = append(w.trail, c)
	}
	if len(w.trail) > 2*memMaxRoute {
		w.trail = loopFree(w.trail)
		if len(w.trail) > memMaxRoute {
			w.trail = append([]world.BlockPos(nil), w.trail[len(w.trail)-memMaxRoute:]...)
		}
	}
}

// lastLeg es el último tramo seguido del rastro (un empujón, un recálculo
// desde lejos dejan huecos: lo de antes del hueco no sirve).
func lastLeg(t []world.BlockPos) []world.BlockPos {
	i := len(t) - 1
	for ; i > 0; i-- {
		a, b := t[i-1], t[i]
		dx, dz, dy := abs(a.X-b.X), abs(a.Z-b.Z), a.Y-b.Y
		if dx > 4 || dz > 4 || dy < -2 || dy > 24 {
			break
		}
	}
	return t[i:]
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// splice es por dónde seguir un camino nuevo: el jugador se movió mientras se
// buscaba (incluso subió un escalón). Se empieza en el nodo más cercano a
// donde está, contando la altura, y nunca se vuelve a uno que ya pasó.
func splice(p []Node, pos basic.Position) int {
	best, bestD := 0, math.MaxFloat64
	for j := 0; j < len(p) && j < 8; j++ {
		c := center(p[j])
		d := dist2(pos.X, pos.Z, c[0], c[1]) + 2*math.Abs(pos.Y-p[j].Floor)
		if d < bestD {
			best, bestD = j, d
		}
	}
	if best+1 < len(p) {
		a, b := center(p[best]), center(p[best+1])
		if (reached(pos, p[best]) || dist2(pos.X, pos.Z, b[0], b[1]) < dist2(a[0], a[1], b[0], b[1])-0.05) &&
			math.Abs(pos.Y-p[best+1].Floor) < 1.3 {
			best++
		}
	}
	return best
}

// offPath dice si el jugador se salió del tramo que va del nodo anterior al
// siguiente (un salto de hueco tiene su nodo lejos a propósito).
func (w *Walker) offPath(pos basic.Position) bool {
	n := w.path[w.i]
	b := center(n)
	a := [2]float64{pos.X, pos.Z}
	lo, hi := n.Floor, n.Floor
	if w.i > 0 {
		p := w.path[w.i-1]
		a = center(p)
		lo, hi = math.Min(lo, p.Floor), math.Max(hi, p.Floor)
	}
	if pos.Y < lo-1.1 || pos.Y > hi+1.1 {
		return true
	}
	// distancia del jugador al segmento a-b
	dx, dz := b[0]-a[0], b[1]-a[1]
	t := 0.0
	if l := dx*dx + dz*dz; l > 0 {
		t = math.Max(0, math.Min(1, ((pos.X-a[0])*dx+(pos.Z-a[1])*dz)/l))
	}
	return dist2(pos.X, pos.Z, a[0]+t*dx, a[1]+t*dz) > 1.8
}

func center(n Node) [2]float64 { return [2]float64{float64(n.Pos.X) + 0.5, float64(n.Pos.Z) + 0.5} }

func dist2(ax, az, bx, bz float64) float64 { return math.Hypot(ax-bx, az-bz) }

// reached dice si el jugador ya está en el nodo n: dentro de su celda (no
// hace falta pisar el centro: caer en el borde de un bloque es llegar) y a su altura.
func reached(pos basic.Position, n Node) bool {
	c := center(n)
	in := math.Abs(pos.X-c[0]) < 0.5 && math.Abs(pos.Z-c[1]) < 0.5
	if !in && dist2(pos.X, pos.Z, c[0], c[1]) >= 0.45 {
		return false
	}
	dy := pos.Y - n.Floor
	switch n.Kind {
	case path.Climb, path.Swim:
		return dy > -0.2 && dy < 0.9
	case path.Ascend, path.GapJump:
		return math.Abs(dy) < 0.3
	case Pillar: // encima del bloque puesto, no en lo alto del salto
		return math.Abs(dy) < 0.02
	case path.Descend:
		return dy < 0.6
	}
	return math.Abs(dy) < 0.6
}

func (w *Walker) walk(s *control.State, bp *basic.Player, pos basic.Position, speed float64) {
	// avanzar: nodos alcanzados, y los que ya se pasaron de largo andando
	from := w.i
	for j := w.i; j < len(w.path) && j < w.i+4; j++ {
		if w.path[j].Kind == Pillar && !s.OnGround {
			continue // de pie sobre el bloque, no pasando por esa altura en pleno salto
		}
		if reached(pos, w.path[j]) {
			w.i = j + 1
		}
	}
	for w.i+1 < len(w.path) {
		n, m := w.path[w.i], w.path[w.i+1]
		if n.Kind != path.Walk || (m.Kind != path.Walk && m.Kind != path.Descend) || math.Abs(pos.Y-n.Floor) > 0.6 {
			break
		}
		a, b := center(n), center(m)
		if dist2(pos.X, pos.Z, b[0], b[1]) >= dist2(a[0], a[1], b[0], b[1])-0.05 {
			break
		}
		w.i++
	}
	w.walked(from)
	if w.i >= len(w.path) {
		w.path = nil
		if w.job.follow != nil {
			if w.grounded(s, pos) {
				s.Input = control.Input{}
			}
			return
		}
		if w.done {
			w.end(s, "arrived")
			return
		}
		if w.timedOut {
			return // parcial por falta de tiempo: desde aquí se busca el resto
		}
		if w.replans++; w.replans > 6 { // parcial: más cerca, pero no se llega
			w.end(s, fmt.Sprintf("failed: no llego a %v", w.job.goal))
		}
		return
	}
	n := w.path[w.i]
	// fuera del camino (empujado, o subió donde no tocaba): recalcular desde aquí, no volver
	if w.grounded(s, pos) && !w.searching && w.offPath(pos) {
		{
			w.path = nil
			w.plan(s, pos)
			if r := w.res; r != nil && s.Stepped {
				w.res = nil
				w.take(s, r, pos)
			}
			if w.path == nil {
				return
			}
			n = w.path[w.i]
		}
	}
	if w.i != w.lastI {
		w.lastI, w.best, w.since = w.i, math.MaxFloat64, 0
	}
	// 10 s sin moverse ni bloque y medio siguiendo un camino (en el suelo,
	// nadando arriba y abajo contra una orilla que no se puede subir…): el
	// paso difícil de delante (la primera subida, si no el siguiente) no se
	// puede; evitarlo y otro camino
	if math.Hypot(pos.X-w.anchor[0], pos.Z-w.anchor[1]) > 1.5 || math.Abs(pos.Y-w.anchor[2]) > 2.5 {
		w.anchor, w.anchorAt = [3]float64{pos.X, pos.Z, pos.Y}, w.ticks
	} else if w.ticks-w.anchorAt > 200 {
		bad := n
		for k := w.i; k < len(w.path) && k < w.i+4; k++ {
			if float64(w.path[k].Pos.Y) > pos.Y+0.5 {
				bad = w.path[k]
				break
			}
		}
		if w.avoid == nil {
			w.avoid = map[world.BlockPos]int{}
		}
		w.avoidCell(bad.Pos)
		log.Printf("pathing: 10 s sin avanzar hacia %v (%s): lo evito y busco otro camino", bad.Pos, kindName(bad.Kind))
		w.path, w.since = nil, 0
		w.anchorAt = w.ticks
		s.Input = control.Input{Jump: true}
		return
	}

	// atasco: en el suelo y sin acercarse al nodo (en el aire no cuenta: un salto no es atascarse)
	c := center(n)
	prog := dist2(pos.X, pos.Z, c[0], c[1])
	fp := world.BlockPos{X: int(math.Floor(pos.X)), Y: int(math.Floor(pos.Y + 1e-6)), Z: int(math.Floor(pos.Z))}
	swimming := !s.OnGround && (w.g.water(fp) || w.g.water(world.BlockPos{X: fp.X, Y: fp.Y + 1, Z: fp.Z}))
	if prog < w.best-0.05 {
		w.best, w.since = prog, 0
	} else if s.OnGround || swimming { // (en el agua también: ahí se ahoga uno sin salir)
		w.since++
	}
	if w.since > 50 {
		if swimming { // esa salida del agua no se puede (algo encima, más alta): otra
			if w.avoid == nil {
				w.avoid = map[world.BlockPos]int{}
			}
			w.avoidCell(n.Pos)
			log.Printf("pathing: no salgo del agua por %v: lo evito y busco otra salida", n.Pos)
		}
		w.path, w.since = nil, 0
		if w.replans++; w.replans > 6 && w.job.follow == nil {
			w.end(s, fmt.Sprintf("failed: atascado antes de %v", n.Pos))
		}
		s.Input = control.Input{Jump: true}
		return
	}

	// un salto hacia una subida (o un hueco) que acaba abajo otra vez: dos
	// así y ese paso no se puede (más alto de lo que parece, algo encima):
	// evitarlo y buscar otro camino (una torre, un puente, un rodeo)
	if (n.Kind == path.Ascend || n.Kind == path.GapJump) && s.OnGround && w.wasAir && pos.Y < n.Floor-0.5 {
		if w.jumpNode != w.i {
			w.jumpNode, w.jumpFails = w.i, 0
		}
		if w.jumpFails++; w.jumpFails >= 2 {
			if w.avoid == nil {
				w.avoid = map[world.BlockPos]int{}
			}
			w.avoidCell(n.Pos)
			w.jumpFails, w.path = 0, nil
			log.Printf("pathing: no llego saltando a %v (%s): lo evito y busco otro camino", n.Pos, kindName(n.Kind))
			s.Input = control.Input{}
			w.wasAir = false
			return
		}
	}
	w.wasAir = !s.OnGround

	if n.Kind == Bridge || n.Kind == Pillar {
		if w.build(s, bp, pos, n) {
			return
		}
	}
	if (n.Kind == Break || n.Kind == BreakUp) && w.breakThrough(s, n) {
		return
	}

	ax, az := w.aim(pos)
	diff := 0.0
	if dist2(pos.X, pos.Z, ax, az) > 0.25 { // muy cerca el ángulo es ruido: no girar
		diff = w.turn(bp, yawTo(pos, ax, az), 55, 8)
	}
	in := control.Input{Forward: diff < 80, Sprint: w.sprint.Load() && diff < 35}
	// bajando: andando, no corriendo; corriendo se salta el escalón de en medio
	// y dos bajadas seguidas se vuelven una caída que duele
	if n.Kind == path.Descend || w.i+1 < len(w.path) && w.path[w.i+1].Kind == path.Descend {
		in.Sprint = false
	}
	hd := dist2(pos.X, pos.Z, c[0], c[1])

	switch n.Kind {
	case path.Ascend:
		// saltar a tiempo: la parte delantera del cuerpo cerca del escalón, o tocándolo
		front := hd - 0.5 - halfWidth
		if s.OnGround && pos.Y < n.Floor-maxStep+1e-6 && (s.HorizontalCollision || front < 0.35+2*speed) {
			in.Jump = true
		}
		// saliendo del agua a la orilla: el salto pulsado (nadar hacia arriba)
		// hasta tener los pies a la altura del escalón; sin correr (corriendo en
		// el agua se nada hundido)
		feet := world.BlockPos{X: int(math.Floor(pos.X)), Y: int(math.Floor(pos.Y + 1e-6)), Z: int(math.Floor(pos.Z))}
		if !s.OnGround && (w.g.water(feet) || w.g.water(world.BlockPos{X: feet.X, Y: feet.Y - 1, Z: feet.Z})) && pos.Y < n.Floor+0.1 {
			in.Jump, in.Sprint = true, false
		}
	case path.GapJump:
		in.Forward, in.Sprint = true, true
		var from [2]float64
		floor0 := pos.Y
		if w.i > 0 {
			from, floor0 = center(w.path[w.i-1]), w.path[w.i-1].Floor
		} else {
			from = [2]float64{math.Floor(pos.X) + 0.5, math.Floor(pos.Z) + 0.5}
		}
		dx, dz := c[0]-from[0], c[1]-from[1]
		l := math.Hypot(dx, dz)
		proj := ((pos.X-from[0])*dx + (pos.Z-from[1])*dz) / l
		edge := 0.5
		if dx != 0 && dz != 0 {
			edge = 0.7
		}
		// al borde (o a punto de salirse el próximo tick), saltar
		if s.OnGround && math.Abs(pos.Y-floor0) < 0.3 && proj+speed >= edge-0.15 {
			in.Jump = true
		}
	case path.Climb:
		in.Forward = true
		in.Jump = pos.Y < n.Floor-0.05
	case path.Swim:
		in.Jump = pos.Y < n.Floor+0.3
	}
	if w.g.water(world.BlockPos{X: int(math.Floor(pos.X)), Y: int(math.Floor(pos.Y)), Z: int(math.Floor(pos.Z))}) {
		in.Sprint = false
		if w.diving(n, pos) {
			// bucear: a la altura del nodo (agachado se baja, saltando se sube).
			// Primero bajar, luego avanzar: agachado el cuerpo mide 1.5 y el
			// cliente se cuela bajo un muro donde el servidor (que lo mide de
			// pie) no lo deja: lo devuelve atrás cada tick
			in.Jump = pos.Y < n.Floor-0.05
			if pos.Y > n.Floor+0.1 {
				in.Shift = true
				if w.headBlocked(n, pos) {
					in.Forward = false
				}
			}
		} else {
			in.Jump = true // cabeza fuera, salir por el borde
		}
	}
	if s.HorizontalCollision && s.OnGround && in.Forward && n.Floor > pos.Y+0.5 {
		in.Jump = true // chocando con el escalón al que va
	}
	// atascado un rato: saltar y moverse de lado como haría una persona
	if w.since > 15 && s.OnGround {
		in.Jump = true
	}
	if w.since > 25 {
		if w.strafe == 0 {
			w.strafe = []int{-1, 1}[rand.IntN(2)]
		}
		in.Left, in.Right = w.strafe < 0, w.strafe > 0
	} else {
		w.strafe = 0
	}
	s.Input = in
}

// diving: el camino va por debajo del agua en el nodo n (bajar a él, o
// pasar con la cabeza dentro: bajo un muro, una barca): no se sube a flote.
func (w *Walker) diving(n Node, pos basic.Position) bool {
	if !w.g.water(n.Pos) {
		return false
	}
	return n.Floor < pos.Y-0.5 || w.g.water(world.BlockPos{X: n.Pos.X, Y: n.Pos.Y + 1, Z: n.Pos.Z}) && n.Floor < pos.Y+0.5
}

// headBlocked: a la altura de ahora el cuerpo no cabe en la celda de n (un
// muro encima): hay que bajar antes de entrar.
func (w *Walker) headBlocked(n Node, pos basic.Position) bool {
	return !w.g.clear(n.Pos.X, n.Pos.Z, pos.Y, pos.Y+height)
}

// aim es el punto hacia el que mirar: el nodo siguiente, o uno más adelante
// si se puede ir en línea recta (recorta las esquinas de la cuadrícula).
func (w *Walker) aim(pos basic.Position) (float64, float64) {
	k := w.i
	// en una escalera de mano: mirar a la salida de arriba (contra la pared se sube)
	if w.path[k].Kind == path.Climb {
		for k+1 < len(w.path) && w.path[k].Kind == path.Climb {
			k++
		}
		c := center(w.path[k])
		return c[0], c[1]
	}
	if w.path[k].Kind == path.Walk {
		for k+1 < len(w.path) && k-w.i < 5 {
			m := w.path[k+1]
			if m.Kind != path.Walk || math.Abs(m.Floor-w.path[k].Floor) > 0.01 {
				break
			}
			c := center(m)
			if !w.lineWalkable(pos, c[0], c[1], false) {
				break
			}
			k++
		}
	}
	c := center(w.path[k])
	return c[0], c[1]
}

// steerDirect va directo a (tx, tz): saltando escalones que encuentre.
func (w *Walker) steerDirect(s *control.State, bp *basic.Player, pos basic.Position, tx, tz, ty, speed float64) {
	diff := w.turn(bp, yawTo(pos, tx, tz), 55, 8)
	in := control.Input{Forward: diff < 80, Sprint: w.sprint.Load() && diff < 35}
	// escalón delante: el suelo un poco más adelante está más alto
	ahead := 0.6 + 2*speed
	yaw := float64(bp.Position().Yaw) * math.Pi / 180
	ax, az := pos.X-math.Sin(yaw)*ahead, pos.Z+math.Cos(yaw)*ahead
	up := world.BlockPos{X: int(math.Floor(ax)), Y: int(math.Floor(pos.Y+1e-6)) + 1, Z: int(math.Floor(az))}
	if f, ok := w.g.floor(up); ok && f-pos.Y > maxStep && f-pos.Y <= maxJump && s.OnGround {
		in.Jump = true
	}
	if s.HorizontalCollision && s.OnGround {
		in.Jump = true
	}
	if w.g.water(world.BlockPos{X: int(math.Floor(pos.X)), Y: int(math.Floor(pos.Y)), Z: int(math.Floor(pos.Z))}) {
		in.Jump, in.Sprint = true, false
	}
	_ = ty
	s.Input = in
}

// lineWalkable dice si se puede andar en línea recta de pos a (tx, tz): suelo
// bajo todo el cuerpo, sin peligros. Con steps vale subir/bajar un bloque por
// el camino (para ir directo saltando); sin steps, solo llano.
func (w *Walker) lineWalkable(pos basic.Position, tx, tz float64, steps bool) bool {
	dx, dz := tx-pos.X, tz-pos.Z
	l := math.Hypot(dx, dz)
	if l < 1e-6 {
		return true
	}
	ux, uz := dx/l, dz/l
	px, pz := -uz*halfWidth, ux*halfWidth // a los lados del cuerpo
	y := pos.Y
	swim := w.inWater(pos)
	for t := 0.3; t < l+0.3; t += 0.3 {
		for _, side := range []float64{-1, 0, 1} {
			x, z := pos.X+ux*t+px*side, pos.Z+uz*t+pz*side
			c := world.BlockPos{X: int(math.Floor(x)), Y: int(math.Floor(y + 1e-6)), Z: int(math.Floor(z))}
			f, ok := w.g.floor(c)
			if !ok && !steps {
				return false
			}
			if !ok {
				c.Y++ // un escalón
				if f, ok = w.g.floor(c); !ok {
					c.Y -= 2 // un bajón
					if f, ok = w.g.floor(c); !ok {
						return false
					}
				}
			}
			lim := maxStep
			if steps && !swim {
				lim = maxJump // (nadando no hay salto: solo lo que se sube andando)
			}
			if f-y > lim || y-f > 1.1 || w.g.deadly(world.BlockPos{X: c.X, Y: c.Y - 1, Z: c.Z}) {
				return false
			}
			if side == 0 {
				y = math.Max(y-1.1, math.Min(f, y+maxJump)) // seguir la altura del suelo por la línea
				swim = w.g.swimming(c)
			}
		}
	}
	return true
}

func yawTo(pos basic.Position, x, z float64) float32 {
	return float32(-math.Atan2(x-pos.X, z-pos.Z) * 180 / math.Pi)
}

// turn gira hacia yaw a lo sumo maxYaw grados por tick y lleva el pitch a
// pitch; devuelve cuánto falta por girar.
func (w *Walker) turn(bp *basic.Player, yaw, maxYaw, pitch float32) float64 {
	var left float32
	bp.UpdatePosition(func(p basic.Position) basic.Position {
		d := wrap(yaw - p.Yaw)
		step := max(-maxYaw, min(maxYaw, d))
		p.Yaw += step
		p.Pitch += max(-10, min(10, pitch-p.Pitch))
		left = wrap(yaw - p.Yaw)
		return p
	})
	return math.Abs(float64(left))
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

// Debug describe el camino en curso (para depurar).
func (w *Walker) Debug() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := fmt.Sprintf("i=%d/%d", w.i, len(w.path))
	for k := w.i; k < len(w.path) && k < w.i+3; k++ {
		n := w.path[k]
		s += fmt.Sprintf(" [%d %d %d f=%.2f %s]", n.Pos.X, n.Pos.Y, n.Pos.Z, n.Floor, kindName(n.Kind))
	}
	return s
}

// ---- Ayudas para quien maneja las teclas él mismo (el combate) ----

// Direct dice si se puede ir en línea recta de pos a (x, z), saltando escalones.
func (w *Walker) Direct(pos basic.Position, x, z float64) bool {
	return w.lineWalkable(pos, x, z, true)
}

// Safe dice si moverse desde pos en la dirección (dx, dz) (normalizada) es
// seguro hasta unos 2 bloques por delante del cuerpo (un golpe empuja casi
// uno): suelo a menos de un bloque de caída, sin lava, fuego ni cactus, y
// nada más alto que un salto.
func (w *Walker) Safe(pos basic.Position, dx, dz float64) bool {
	for _, d := range []float64{0.5 + halfWidth, 1.1 + halfWidth, 1.7 + halfWidth} {
		c := world.BlockPos{X: int(math.Floor(pos.X + dx*d)), Y: int(math.Floor(pos.Y + 1e-6)), Z: int(math.Floor(pos.Z + dz*d))}
		ok := false
		for _, dy := range []int{0, 1, -1} {
			p := world.BlockPos{X: c.X, Y: c.Y + dy, Z: c.Z}
			f, found := w.g.floor(p)
			if !found {
				continue
			}
			below := world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z}
			ok = f-pos.Y <= maxJump && pos.Y-f <= 1.1 && !w.g.deadly(p) && !w.g.deadly(below)
			break
		}
		if !ok {
			return false
		}
	}
	return true
}

// NeedJump dice si moviéndose en la dirección (dx, dz) a speed bloques por
// tick hay que saltar ya para subir el escalón que viene.
func (w *Walker) NeedJump(pos basic.Position, dx, dz, speed float64) bool {
	ahead := 0.6 + 2*speed
	up := world.BlockPos{X: int(math.Floor(pos.X + dx*ahead)), Y: int(math.Floor(pos.Y+1e-6)) + 1, Z: int(math.Floor(pos.Z + dz*ahead))}
	f, ok := w.g.floor(up)
	return ok && f-pos.Y > maxStep && f-pos.Y <= maxJump
}

// FleeGoal busca un sitio donde estar a 8-16 bloques de pos, en dirección
// contraria a (fromX, fromZ) (o lo más parecido), para ir con Goto.
func (w *Walker) FleeGoal(pos basic.Position, fromX, fromZ float64) (world.BlockPos, bool) {
	away := math.Atan2(pos.X-fromX, pos.Z-fromZ)
	for _, d := range []float64{16, 12, 8} {
		for _, off := range []float64{0, 0.5, -0.5, 1, -1, 1.5, -1.5} {
			x := int(math.Floor(pos.X + math.Sin(away+off)*d))
			z := int(math.Floor(pos.Z + math.Cos(away+off)*d))
			for _, dy := range []int{0, 1, -1, 2, -2, 3, -3, 4, -4} {
				p := world.BlockPos{X: x, Y: int(math.Floor(pos.Y+1e-6)) + dy, Z: z}
				if _, ok := w.g.floor(p); ok && !w.g.deadly(world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z}) {
					return p, true
				}
			}
		}
	}
	return world.BlockPos{}, false
}

// Rejected avisa al walker de que el servidor corrigió su posición (rechazó el
// movimiento): se para medio segundo y recalcula desde donde lo dejó el
// servidor; si el mismo paso se rechaza dos veces, evita esa celda 30 s y
// busca otro camino. Reintentar sin parar lo que el servidor rechaza no lo
// hace ningún jugador (y para un anticheat cada intento cuenta). Devuelve qué
// hizo, para el log ("" si no estaba caminando).
func (w *Walker) Rejected() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.job.active {
		return ""
	}
	w.pauseUntil = w.ticks + 10
	msg := "me paro un momento y recalculo"
	if w.path != nil && w.i < len(w.path) {
		n := w.path[w.i]
		if w.rejects == nil {
			w.rejects, w.avoid = map[world.BlockPos]int{}, map[world.BlockPos]int{}
		}
		if w.rejects[n.Pos]++; w.rejects[n.Pos] >= 2 {
			// la celda y sus vecinas: si no deja subir aquí, al lado tampoco
			for dx := -1; dx <= 1; dx++ {
				for dz := -1; dz <= 1; dz++ {
					w.avoidCell(world.BlockPos{X: n.Pos.X + dx, Y: n.Pos.Y, Z: n.Pos.Z + dz})
				}
			}
			delete(w.rejects, n.Pos)
			if n.Kind == path.Ascend || n.Kind == path.GapJump {
				w.climbUntil = w.ticks + 600
			}
			msg = fmt.Sprintf("el servidor no me deja pasar a %v (%s): lo evito 30 s y busco otro camino", n.Pos, kindName(n.Kind))
		}
	}
	w.path = nil
	return msg
}

// Ground es dónde pisar en la columna (x, z): el sitio más cercano en altura
// a nearY (hasta 24 arriba o abajo) donde cabe un jugador.
func (w *Walker) Ground(x, z, nearY int) (world.BlockPos, bool) {
	for d := 0; d <= 24; d++ {
		for _, dy := range []int{d, -d} {
			p := world.BlockPos{X: x, Y: nearY + dy, Z: z}
			if _, ok := w.g.floor(p); ok && !w.g.deadly(world.BlockPos{X: x, Y: p.Y - 1, Z: z}) {
				return p, true
			}
			if d == 0 {
				break
			}
		}
	}
	return world.BlockPos{}, false
}

// build da los pasos de un puente o una torre hacia n; devuelve true si se
// encargó del tick (false: el bloque ya está puesto, se anda como siempre).
func (w *Walker) build(s *control.State, bp *basic.Player, pos basic.Position, n Node) bool {
	var from Node
	if w.i > 0 {
		from = w.path[w.i-1]
	} else {
		st, _ := w.g.start(pos.X, pos.Y, pos.Z)
		from = st
	}
	var target, against world.BlockPos
	if n.Kind == Bridge {
		target = world.BlockPos{X: n.Pos.X, Y: from.Pos.Y - 1, Z: n.Pos.Z}        // bajo la celda de delante
		against = world.BlockPos{X: from.Pos.X, Y: from.Pos.Y - 1, Z: from.Pos.Z} // el bloque que se pisa
	} else {
		target = from.Pos                                                         // donde están los pies
		against = world.BlockPos{X: from.Pos.X, Y: from.Pos.Y - 1, Z: from.Pos.Z} // el de debajo
	}
	if st, ok := w.g.state(target); ok && !block.Replaceable(st) {
		if n.Kind == Pillar && pos.Y < n.Floor-0.3 {
			s.Input = control.Input{} // puesto: caer encima
			return true
		}
		return false // ya está: andar (o subir) como siempre
	}
	if w.builder == nil || w.builder.Blocks() == 0 {
		w.path = nil // sin bloques: otro camino
		return true
	}
	w.building.Store(time.Now().UnixNano()) // la mano es del constructor (Building)
	busy := w.builder.Busy()
	if !busy && w.buildNode == w.i && w.ticks-w.buildAt > 60 {
		// no se pudo (el servidor no lo puso): evitar ese paso y buscar otro
		if w.avoid == nil {
			w.avoid = map[world.BlockPos]int{}
		}
		w.avoidCell(n.Pos)
		w.path, w.buildNode = nil, -1
		return true
	}
	c := center(from)
	switch n.Kind {
	case Bridge:
		// agachado hacia el borde (agachado no se cae), luego quieto y poner
		dx, dz := float64(n.Pos.X-from.Pos.X), float64(n.Pos.Z-from.Pos.Z)
		proj := (pos.X-c[0])*dx + (pos.Z-c[1])*dz
		in := control.Input{Shift: true}
		if busy {
			s.Input = in // sin girar mientras pone el bloque
			return true
		}
		w.turn(bp, yawTo(pos, c[0]+dx, c[1]+dz), 30, 60) // hacia el hueco, mirando abajo
		if proj < 0.2 {
			in.Forward = true
		} else if w.buildNode != w.i || w.ticks-w.buildAt > 20 {
			if w.builder.Place(target, against) {
				w.buildAt, w.buildNode = w.ticks, w.i
			}
		}
		s.Input = in
	case Pillar:
		// al centro del bloque, saltar, y con los pies ya por encima poner el bloque debajo
		if busy {
			// quieto; agachado si la mano lo pidió (pinchando un bloque con uso
			// propio, un horno o un cofre: sin agacharse se abriría en vez de
			// poner el bloque encima). Las manos van antes que el walker en el
			// tick: lo que ellas pusieron sigue aquí
			s.Input = control.Input{Shift: s.Input.Shift}
			return true
		}
		if dist2(pos.X, pos.Z, c[0], c[1]) > 0.2 && s.OnGround {
			w.turn(bp, yawTo(pos, c[0], c[1]), 30, 80)
			s.Input = control.Input{Forward: true, Shift: true}
			return true
		}
		if s.OnGround && w.capBlocked(s, from, n) {
			return true
		}
		w.turn(bp, bp.Position().Yaw, 0, 90) // mirar abajo
		in := control.Input{Jump: s.OnGround}
		if !s.OnGround && pos.Y >= from.Floor+1.05 && (w.buildNode != w.i || w.ticks-w.buildAt > 20) {
			if w.builder.Place(target, against) {
				w.buildAt, w.buildNode = w.ticks, w.i
			}
		}
		s.Input = in
	}
	return true
}

// Building: está poniendo bloques de una torre o un puente (hace menos de 1 s
// que pidió uno). Mientras, la mano es del constructor: quien cambie el objeto
// de la mano (el arma de la pelea) espera, o el bloque nunca llega a ponerse.
// Climbing: el camino de ahora aún tiene torre por delante (sube poniendo
// bloques). La pelea no lo corta para pegar desde media torre: subido a una
// columna de 1×1, un bloque por debajo de él, el primer golpe lo tira.
func (w *Walker) Climbing() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for k := w.i; k < len(w.path); k++ {
		if w.path[k].Kind == Pillar {
			return true
		}
	}
	return false
}

func (w *Walker) Building() bool {
	return time.Since(time.Unix(0, w.building.Load())) < time.Second
}

// BuildBlocks son los bloques de construir que ve el constructor (0 sin constructor).
func (w *Walker) BuildBlocks() int {
	w.mu.Lock()
	b := w.builder
	w.mu.Unlock()
	if b == nil {
		return 0
	}
	return b.Blocks()
}

// fallsAbove: justo encima de c hay un bloque que cae (romper c lo suelta).
func (w *Walker) fallsAbove(c world.BlockPos) bool {
	st, ok := w.g.state(world.BlockPos{X: c.X, Y: c.Y + 1, Z: c.Z})
	return ok && Falling(world.StateString(st))
}

// breakThrough rompe lo que tapa la celda n (los pies y la cabeza) antes de
// entrar; dice si el tick se fue en eso. Si no lo consigue en 5 s, evita la
// celda y busca otro camino.
func (w *Walker) breakThrough(s *control.State, n Node) bool {
	br, ok := w.builder.(Breaker)
	if !ok {
		w.path = nil
		return true
	}
	cells := []world.BlockPos{{X: n.Pos.X, Y: n.Pos.Y + 1, Z: n.Pos.Z}, n.Pos} // la cabeza primero
	if n.Kind == BreakUp && w.i > 0 {                                          // y encima de la cabeza de donde salta
		f := w.path[w.i-1].Pos
		cells = append([]world.BlockPos{{X: f.X, Y: f.Y + 2, Z: f.Z}}, cells...)
	}
	var todo *world.BlockPos
	for _, c := range cells {
		if st, ok := w.g.state(c); ok && len(block.CollisionShape(st)) > 0 && (Soft(world.StateString(st)) || w.passableCell(c)) {
			todo = &c
			break
		}
	}
	if todo != nil && w.fallsAbove(*todo) {
		// encima hay grava (o arena): al romper esto caería sobre el bot
		w.avoidCell(n.Pos)
		w.path, w.buildNode = nil, -1
		log.Printf("pathing: encima de %v hay algo que cae (grava): por ahí no", *todo)
		s.Input = control.Input{}
		return true
	}
	if todo == nil {
		// abierto: desde aquí es andar (o subir) como siempre
		if n.Kind == BreakUp {
			w.path[w.i].Kind = path.Ascend
		} else {
			w.path[w.i].Kind = path.Walk
		}
		return false
	}
	s.Input = control.Input{}
	if w.buildNode != w.i {
		w.buildNode, w.buildAt = w.i, w.ticks
	}
	if w.ticks-w.buildAt > 100 {
		if w.avoid == nil {
			w.avoid = map[world.BlockPos]int{}
		}
		w.avoidCell(n.Pos)
		w.path, w.buildNode = nil, -1
		log.Printf("pathing: no consigo romper %v: busco otro camino", *todo)
		return true
	}
	if !w.builder.Busy() {
		if cb, ok := w.builder.(CapBreaker); ok && w.passableCell(*todo) {
			if st, ok := w.g.state(*todo); ok {
				cb.BreakBest(*todo, st) // un andamio: con la herramienta buena
			}
		} else {
			br.Break(*todo)
		}
	}
	return true
}

// capBlocked: subiendo en torre, con un bloque justo encima de la cabeza (un
// techo: el de arriba lo pone para que no suba; con él el salto no llega a
// dejar sitio para el bloque de debajo, y la torre se queda saltando en el
// sitio). Si se rompe enseguida con lo que lleva (MaxCapTicks), se rompe y se
// sigue; si no, o si en esta columna ya le taparon 3 veces (el de arriba
// tiene bloques de sobra: cada uno que pone, el bot lo rompe y vuelve a
// tapar), se sube por otra columna. True: el tick se fue en eso.
func (w *Walker) capBlocked(s *control.State, from, n Node) bool {
	c := world.BlockPos{X: from.Pos.X, Y: from.Pos.Y + 2, Z: from.Pos.Z}
	st, ok := w.g.state(c)
	if !ok || len(block.CollisionShape(st)) == 0 {
		if w.capAt != 0 && c == w.capCell {
			w.capAt = 0 // roto: a subir
		}
		return false
	}
	col := [2]int{c.X, c.Z}
	ticks, cb := -1, CapBreaker(nil)
	if b, ok := w.builder.(CapBreaker); ok {
		cb = b
		ticks, _ = b.BreakTicks(st)
	}
	if nb, ok := w.builder.(interface{ CanBreak(world.BlockPos) bool }); ok && !nb.CanBreak(c) {
		ticks = -1 // lo construido (una construcción en marcha): por otra columna
	}
	name := strings.TrimPrefix(world.StateString(st), "minecraft:")
	if i := strings.IndexByte(name, '['); i >= 0 {
		name = name[:i]
	}
	falls := w.fallsAbove(c) // romperlo suelta grava sobre la cabeza
	if w.capAt == 0 || w.capCell != c {
		w.capCell, w.capAt, w.capTime = c, w.ticks, ticks
		if w.caps == nil {
			w.caps = map[[2]int]int{}
		}
		w.caps[col]++
		switch {
		case falls:
			log.Printf("pathing: encima del techo de %s en %v hay algo que cae (grava): subo por otro lado", name, c)
		case cb == nil || ticks < 0 || ticks > MaxCapTicks:
			log.Printf("pathing: un techo de %s encima de la torre en %v (no lo rompo rápido): subo por otro lado", name, c)
		case w.caps[col] > 3:
			log.Printf("pathing: otro techo de %s en %v (van %d en esta columna): subo por otro lado", name, c, w.caps[col])
		default:
			log.Printf("pathing: un techo de %s encima de la torre en %v: lo rompo (%.1f s)", name, c, float64(ticks)/20)
		}
	}
	if falls || cb == nil || ticks < 0 || ticks > MaxCapTicks || w.caps[col] > 3 || w.ticks-w.capAt > 3*w.capTime+40 {
		// por otra columna: esta celda de la torre, evitada un rato
		w.avoidCell(n.Pos)
		w.path, w.buildNode, w.capAt = nil, -1, 0
		s.Input = control.Input{}
		return true
	}
	w.building.Store(time.Now().UnixNano()) // la mano es del constructor (la pelea no la toca)
	s.Input = control.Input{}
	if !w.builder.Busy() {
		cb.BreakBest(c, st)
	}
	return true
}
