package pathing

// Memoria de caminos: lo que el bot aprendió andando.
//
//   - Rutas que salieron bien (llegó): las celdas que pisó de verdad, sin los
//     rodeos (si volvió a pisar una celda, lo de en medio fue una vuelta: un
//     atasco, un salto fallido) . Para ir otra vez por ahí (o al revés, o a un
//     sitio de en medio) no se busca: se comprueba cada paso con el mundo de
//     ahora (los mismos movimientos que la búsqueda) y se anda. Si algo cambió
//     (un bloque roto, una puerta cerrada), búsqueda normal.
//   - Trabas: las celdas donde se atascó (10 s sin avanzar, saltos que no
//     llegan, salidas del agua imposibles, bloques que no se pusieron). La
//     búsqueda las evita unos segundos (avoid) y, después, durante una hora,
//     le cuestan más: no se vuelve a meter en la misma si hay otra forma.

import (
	"encoding/json"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mj41/go-mc26-kit/bot/world"
)

const (
	memRoutes    = 64        // rutas que se recuerdan (las menos usadas se olvidan)
	memMinRoute  = 6         // más cortas no merecen la pena (buscar es instantáneo)
	memMaxRoute  = 2000      // celdas por ruta
	snagTTL      = time.Hour // lo que dura el recuerdo de una traba
	snagCost     = 6         // coste de más por cada vez que se atascó ahí
	snagMaxCount = 4
	// una ruta conocida vale si no da mucha más vuelta que la línea recta
	reuseSlack = 1.6
)

// Memory guarda rutas y trabas; la puede compartir más de un walker y
// guardarse en un archivo (Save/Load).
type Memory struct {
	mu     sync.Mutex
	routes []memRoute
	snags  map[world.BlockPos]snag
	// para los tests y /path info
	Reused   atomic.Int64 // caminos sacados de la memoria
	Searched atomic.Int64 // búsquedas hechas
}

type memRoute struct {
	Cells []world.BlockPos `json:"cells"`
	Used  time.Time        `json:"used"`
}

type snag struct {
	N  int       `json:"n"`
	At time.Time `json:"at"`
}

// NewMemory crea una memoria vacía.
func NewMemory() *Memory { return &Memory{snags: map[world.BlockPos]snag{}} }

// Routes es cuántas rutas recuerda; Snags, cuántas trabas.
func (m *Memory) Routes() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.routes)
}

func (m *Memory) Snags() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.snags)
}

// Clear olvida todo (otro mundo, otra dimensión).
func (m *Memory) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routes, m.snags = nil, map[world.BlockPos]snag{}
}

// remember guarda una ruta recorrida entera (de la salida a la meta).
func (m *Memory) remember(cells []world.BlockPos) {
	cells = loopFree(cells)
	if len(cells) < memMinRoute {
		return
	}
	if len(cells) > memMaxRoute {
		cells = cells[len(cells)-memMaxRoute:]
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// una que ya está dentro de esta (el mismo camino, más corto) sobra
	out := m.routes[:0]
	for _, r := range m.routes {
		if !contains(cells, r.Cells) {
			out = append(out, r)
		}
	}
	m.routes = append(out, memRoute{Cells: cells, Used: time.Now()})
	if len(m.routes) > memRoutes {
		old := 0
		for i, r := range m.routes {
			if r.Used.Before(m.routes[old].Used) {
				old = i
			}
		}
		m.routes = append(m.routes[:old], m.routes[old+1:]...)
	}
}

// contains: todas las celdas de b están en a, seguidas y en el mismo orden (o al revés).
func contains(a, b []world.BlockPos) bool {
	if len(b) > len(a) || len(b) == 0 {
		return false
	}
	for i := range a {
		if a[i] != b[0] && a[i] != b[len(b)-1] {
			continue
		}
		fwd, back := a[i] == b[0], a[i] == b[len(b)-1]
		for k := 1; k < len(b) && (fwd || back); k++ {
			if i+k >= len(a) {
				return false
			}
			fwd = fwd && a[i+k] == b[k]
			back = back && a[i+k] == b[len(b)-1-k]
		}
		if fwd || back {
			return true
		}
	}
	return false
}

// loopFree quita las vueltas: si una celda vuelve a salir, lo de en medio sobra.
func loopFree(cells []world.BlockPos) []world.BlockPos {
	out := make([]world.BlockPos, 0, len(cells))
	at := map[world.BlockPos]int{}
	for _, c := range cells {
		if i, ok := at[c]; ok {
			for _, d := range out[i+1:] {
				delete(at, d)
			}
			out = out[:i+1]
			continue
		}
		at[c] = len(out)
		out = append(out, c)
	}
	return out
}

// snag apunta una traba en c.
func (m *Memory) snag(c world.BlockPos) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.snags[c]
	if time.Since(s.At) > snagTTL {
		s.N = 0
	}
	if s.N < snagMaxCount {
		s.N++
	}
	s.At = time.Now()
	m.snags[c] = s
}

// penalties es lo que cuesta de más cada celda con trabas (para una búsqueda).
func (m *Memory) penalties() map[world.BlockPos]float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.snags) == 0 {
		return nil
	}
	out := make(map[world.BlockPos]float64, len(m.snags))
	for c, s := range m.snags {
		if time.Since(s.At) > snagTTL {
			delete(m.snags, c)
			continue
		}
		out[c] = snagCost * float64(s.N)
	}
	return out
}

// reuse busca en la memoria un camino de start a goal y lo comprueba con el
// mundo de ahora (g: los movimientos de la búsqueda). ok false: a buscar.
func (m *Memory) reuse(g *graph, start Node, goal world.BlockPos, near float64) (result, bool) {
	m.mu.Lock()
	routes := make([][]world.BlockPos, len(m.routes))
	for i, r := range m.routes {
		routes[i] = r.Cells
	}
	m.mu.Unlock()
	// la meta: la propia celda, o a near (como en find)
	at := func(p world.BlockPos) bool {
		if p == goal {
			return true
		}
		dx, dy, dz := float64(p.X-goal.X), float64(p.Y-goal.Y), float64(p.Z-goal.Z)
		return near > 0 && dx*dx+dz*dz <= near*near && math.Abs(dy) <= 1
	}
	straight := math.Sqrt(float64((start.Pos.X-goal.X)*(start.Pos.X-goal.X) + (start.Pos.Z-goal.Z)*(start.Pos.Z-goal.Z) + (start.Pos.Y-goal.Y)*(start.Pos.Y-goal.Y)))
	var best []world.BlockPos
	bestRoute := -1
	for ri, r := range routes {
		for _, cells := range [2][]world.BlockPos{r, reversed(r)} {
			i, j := -1, -1
			for k, c := range cells {
				if c == start.Pos {
					i = k
					break
				}
				if i < 0 && adjacent(c, start.Pos) {
					i = k
				}
			}
			if i >= 0 {
				for k := i + 1; k < len(cells); k++ {
					if at(cells[k]) {
						j = k
						break
					}
				}
			}
			if i < 0 || j < 0 {
				continue
			}
			seg := cells[i : j+1]
			if seg[0] != start.Pos {
				seg = append([]world.BlockPos{start.Pos}, seg...)
			}
			if len(seg) < memMinRoute || float64(len(seg)) > reuseSlack*straight+8 {
				continue
			}
			if best == nil || len(seg) < len(best) {
				best, bestRoute = seg, ri
			}
		}
	}
	if best == nil {
		return result{}, false
	}
	nodes, ok := g.follow(start, best[1:])
	if !ok {
		return result{}, false
	}
	m.mu.Lock()
	if bestRoute < len(m.routes) {
		m.routes[bestRoute].Used = time.Now()
	}
	m.mu.Unlock()
	m.Reused.Add(1)
	return result{path: nodes, complete: true}, true
}

func reversed(c []world.BlockPos) []world.BlockPos {
	out := make([]world.BlockPos, len(c))
	for i, p := range c {
		out[len(c)-1-i] = p
	}
	return out
}

// adjacent: a un paso (las 8 de alrededor, un bloque arriba o abajo).
func adjacent(a, b world.BlockPos) bool {
	dx, dy, dz := a.X-b.X, a.Y-b.Y, a.Z-b.Z
	return a != b && dx >= -1 && dx <= 1 && dz >= -1 && dz <= 1 && dy >= -1 && dy <= 1
}

// follow comprueba que de start se pueda ir celda a celda por cells con los
// movimientos de ahora (los de la búsqueda: suelo, saltos, puentes si hay
// bloques…) y devuelve los nodos.
func (g *graph) follow(start Node, cells []world.BlockPos) ([]Node, bool) {
	cur := start
	out := make([]Node, 0, len(cells))
	for _, c := range cells {
		if g.avoid[c] || g.penalty[c] > 0 || g.deadly(c) { // (una traba después: mejor buscar)
			return nil, false
		}
		found := false
		for _, e := range g.neighbours(cur) {
			if e.to.Pos == c {
				cur, found = e.to, true
				break
			}
		}
		if !found {
			return nil, false
		}
		out = append(out, cur)
	}
	return out, true
}

// memFile es lo que se guarda en el archivo.
type memFile struct {
	Routes []memRoute  `json:"routes"`
	Snags  []savedSnag `json:"snags"`
}

type savedSnag struct {
	Pos world.BlockPos `json:"pos"`
	snag
}

// Save guarda la memoria en path (JSON).
func (m *Memory) Save(path string) error {
	m.mu.Lock()
	f := memFile{Routes: append([]memRoute(nil), m.routes...)}
	for c, s := range m.snags {
		if time.Since(s.At) <= snagTTL {
			f.Snags = append(f.Snags, savedSnag{c, s})
		}
	}
	m.mu.Unlock()
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load lee lo guardado con Save (sin archivo, nada que cargar).
func (m *Memory) Load(path string) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var f memFile
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.routes = f.Routes
	if len(m.routes) > memRoutes {
		m.routes = m.routes[len(m.routes)-memRoutes:]
	}
	m.snags = map[world.BlockPos]snag{}
	for _, s := range f.Snags {
		if time.Since(s.At) <= snagTTL {
			m.snags[s.Pos] = s.snag
		}
	}
	return nil
}
