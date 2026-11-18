package automation

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

// Storage es el almacén: un sitio (la base) con cofres en fila, cada uno de
// un solo tipo de objeto (ordenados). Se guarda en un archivo para que
// sobreviva a reinicios del bot.
type Storage struct {
	mu     sync.Mutex
	denied []denial // los "no" de ahora (NoteDenied), sin guardar
	file   string
	Base   *world.BlockPos `json:"base"`
	Chests []Chest         `json:"chests"`
	// Portals: el portal que usó en cada dimensión ("minecraft:the_nether")
	Portals map[string]world.BlockPos `json:"portals,omitempty"`
	// la granja: el corral de ovejas y las celdas de tierra del trigo
	Pen   *Pen             `json:"pen,omitempty"`
	Field []world.BlockPos `json:"field,omitempty"`
	// Protected: sitios donde el servidor no deja romper ni poner (el spawn,
	// terrenos reclamados): ni se tocan ni se buscan árboles cerca
	Protected []world.BlockPos `json:"protected,omitempty"`
	// MineChests: el cofre de /mine chest de cada dimensión
	// ("minecraft:overworld", "minecraft:the_nether"): de ahí coge picos,
	// comida y armadura, y ahí deja lo minado
	MineChests map[string]world.BlockPos `json:"mine_chests,omitempty"`
	// PickChest y PickChestDim: el cofre único de antes (se pasa a MineChests al cargar)
	PickChest    *world.BlockPos `json:"pick_chest,omitempty"`
	PickChestDim string          `json:"pick_chest_dim,omitempty"`
	// FoodChests: los cofres de los que puede coger comida (/foodchest;
	// foodchests.go)
	FoodChests []world.BlockPos `json:"food_chests,omitempty"`
}

// SetMineChest apunta (o con nil olvida) el cofre de minar de la dimensión dim.
func (s *Storage) SetMineChest(dim string, p *world.BlockPos) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p == nil {
		delete(s.MineChests, dim)
	} else {
		if s.MineChests == nil {
			s.MineChests = map[string]world.BlockPos{}
		}
		s.MineChests[dim] = *p
	}
	s.save()
}

// MineChest es el cofre de minar de la dimensión dim.
func (s *Storage) MineChest(dim string) (world.BlockPos, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.MineChests[dim]
	return p, ok
}

// MineChestList son los cofres de minar, por dimensión.
func (s *Storage) MineChestList() map[string]world.BlockPos {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]world.BlockPos, len(s.MineChests))
	for k, v := range s.MineChests {
		out[k] = v
	}
	return out
}

// isMineChest: en p hay un cofre de minar (no se rompe).
func (s *Storage) isMineChest(p world.BlockPos) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.MineChests {
		if c == p {
			return true
		}
	}
	return false
}

// protectRadius: lo que se evita alrededor de un sitio donde el servidor dijo que no.
const protectRadius = 16

// NoteProtected apunta que en p el servidor no deja construir ni romper.
func (s *Storage) NoteProtected(p world.BlockPos) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, q := range s.Protected {
		if abs(q.X-p.X) <= protectRadius/2 && abs(q.Z-p.Z) <= protectRadius/2 {
			return false // ya estaba (casi) apuntado
		}
	}
	s.Protected = append(s.Protected, p)
	s.save()
	return true
}

// denial es un "no tienes permiso" (o un bloque que el servidor devolvió):
// dónde y cuándo. Solo en memoria (las tareas que no se fían de las zonas
// guardadas, como /excavate, miran estas: precisas y de ahora).
type denial struct {
	p world.BlockPos
	t time.Time
}

// NoteDenied apunta que en p el servidor acaba de decir que no.
func (s *Storage) NoteDenied(p world.BlockPos) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.denied) > 512 {
		s.denied = s.denied[256:]
	}
	s.denied = append(s.denied, denial{p, time.Now()})
}

// DeniedNear: el servidor dijo que no a menos de r bloques (en x y z) de p desde since.
func (s *Storage) DeniedNear(p world.BlockPos, r int, since time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.denied {
		if !d.t.Before(since) && abs(d.p.X-p.X) <= r && abs(d.p.Z-p.Z) <= r {
			return true
		}
	}
	return false
}

// IsProtectedSince es IsProtected solo con las zonas apuntadas desde la
// from-ésima (las de antes no cuentan) y a r bloques de cada una.
func (s *Storage) IsProtectedSince(p world.BlockPos, from, r int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := max(from, 0); i < len(s.Protected); i++ {
		q := s.Protected[i]
		if abs(q.X-p.X) <= r && abs(q.Z-p.Z) <= r {
			return true
		}
	}
	return false
}

// ProtectedList son los sitios apuntados como protegidos (cada uno tapa protectRadius alrededor).
func (s *Storage) ProtectedList() []world.BlockPos {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]world.BlockPos(nil), s.Protected...)
}

// ClearProtected olvida las zonas protegidas: todas (near nil) o las que
// tapan near. Devuelve cuántas olvidó.
func (s *Storage) ClearProtected(near *world.BlockPos) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	var keep []world.BlockPos
	for _, q := range s.Protected {
		if near != nil && (abs(q.X-near.X) > protectRadius || abs(q.Z-near.Z) > protectRadius) {
			keep = append(keep, q)
		}
	}
	n := len(s.Protected) - len(keep)
	s.Protected = keep
	s.save()
	return n
}

// ProtectRadius: lo que tapa cada sitio apuntado como protegido (en x y en z).
const ProtectRadius = protectRadius

// IsProtected: p está en una zona donde el servidor no deja tocar nada.
func (s *Storage) IsProtected(p world.BlockPos) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, q := range s.Protected {
		if abs(q.X-p.X) <= protectRadius && abs(q.Z-p.Z) <= protectRadius {
			return true
		}
	}
	return false
}

func (s *Storage) portal(dim string) (world.BlockPos, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.Portals[dim]
	return p, ok
}

func (s *Storage) setPortal(dim string, p world.BlockPos) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Portals == nil {
		s.Portals = map[string]world.BlockPos{}
	}
	s.Portals[dim] = p
	s.save()
}

// Chest es un cofre del almacén.
type Chest struct {
	Pos  world.BlockPos  `json:"pos"`
	Pos2 *world.BlockPos `json:"pos2,omitempty"` // la otra mitad, si es doble
	Item string          `json:"item"`           // lo que guarda: un objeto ("oak_log") o una categoría ("madera")
	Full bool            `json:"full"`
}

func loadStorage(file string) *Storage {
	s := &Storage{file: file}
	if file == "" {
		return s
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return s
	}
	if err := json.Unmarshal(b, s); err != nil {
		log.Printf("almacén: %s no se pudo leer: %v", file, err)
	}
	if s.PickChest != nil { // el cofre único de antes: el de su dimensión
		dim := s.PickChestDim
		if dim == "" {
			dim = "minecraft:overworld"
		}
		if _, ok := s.MineChests[dim]; !ok {
			if s.MineChests == nil {
				s.MineChests = map[string]world.BlockPos{}
			}
			s.MineChests[dim] = *s.PickChest
		}
		s.PickChest, s.PickChestDim = nil, ""
		s.save()
	}
	return s
}

func (s *Storage) save() {
	if s.file == "" {
		return
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	if err := os.WriteFile(s.file, b, 0o644); err != nil {
		log.Printf("almacén: %v", err)
	}
}

// SetBase pone la base del almacén en p (los cofres van en fila desde ahí).
func (s *Storage) SetBase(p world.BlockPos) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Base = &p
	s.save()
}

// chestCount es cuántos cofres tiene el almacén.
func (s *Storage) chestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.Chests)
}

// Describe resume el almacén.
func (s *Storage) Describe() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Base == nil {
		return "sin base (usa /base aquí)"
	}
	count := map[string]int{}
	for _, c := range s.Chests {
		count[c.Item]++
	}
	return fmt.Sprintf("base en %v, %d cofres %v", *s.Base, len(s.Chests), count)
}

// chestFor es un cofre de item que no esté lleno (ok false: hay que poner otro).
func (s *Storage) chestFor(item string) (Chest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.Chests {
		if (c.Item == item || categoryOf(c.Item) == item) && !c.Full {
			return c, true
		}
	}
	return Chest{}, false
}

func (s *Storage) mark(pos world.BlockPos, full bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Chests {
		if s.Chests[i].Pos == pos {
			s.Chests[i].Full = full
		}
	}
	s.save()
}

func (s *Storage) drop(pos world.BlockPos) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Chests {
		if s.Chests[i].Pos == pos {
			s.Chests = append(s.Chests[:i], s.Chests[i+1:]...)
			break
		}
	}
	s.save()
}

func (s *Storage) add(c Chest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Chests = append(s.Chests, c)
	s.save()
}

// taken: hay ya un cofre del almacén en p.
func (s *Storage) taken(p world.BlockPos) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.Chests {
		if c.Pos == p || c.Pos2 != nil && *c.Pos2 == p {
			return true
		}
	}
	return false
}

// categoryOf es en qué cofre va un objeto: toda la madera junta, lo demás cada cosa en el suyo.
func categoryOf(item string) string {
	if isLog(item) {
		return "madera"
	}
	return item
}

// Deposit guarda en el almacén todo el item que lleva: en el cofre de su
// categoría, y si está lleno (o no hay), en uno doble nuevo que hace y pone.
func (j *Job) Deposit(item string) error {
	if j.depositMarked(item) {
		return nil // (todo en el cofre marcado con /chest)
	}
	st := j.r.Store
	if st.Base == nil {
		f := j.Feet()
		st.SetBase(f)
		j.Logf("almacén: base aquí, en %v", f)
	}
	cat := categoryOf(item)
	for tries := 0; j.Count(item) > 0 && tries < 6; tries++ {
		c, ok := st.chestFor(cat)
		if !ok {
			var err error
			if c, err = j.newChest(cat); err != nil {
				return fmt.Errorf("almacén: %w", err)
			}
		}
		if j.Block(c.Pos) != "chest" { // ya no está (roto)
			st.drop(c.Pos)
			continue
		}
		j.Status("guardando %s en el cofre de %v", item, c.Pos)
		if err := j.Open(c.Pos); err != nil {
			return fmt.Errorf("almacén: abrir %v: %w", c.Pos, err)
		}
		n, err := j.r.ui.Move(j.ctx, full(item), true)
		j.CloseScreen()
		if err != nil {
			return fmt.Errorf("almacén: %w", err)
		}
		j.Logf("almacén: %d %s al cofre de %v", n, item, c.Pos)
		if j.Count(item) > 0 { // no cupo todo: lleno
			st.mark(c.Pos, true)
		}
	}
	return nil
}

// depositMarked: el cofre marcado (/chest; el de minar de esta dimensión),
// si está cerca, va primero. Dice si ya no queda nada de item encima.
func (j *Job) depositMarked(item string) bool {
	c, ok := j.nearMineChest(j.Dimension())
	if !ok || !isChest(j.Block(c)) {
		return false
	}
	j.Status("guardando %s en el cofre de %v", item, c)
	if err := j.Open(c); err != nil {
		j.Logf("cofre marcado %v: %v", c, err)
		return false
	}
	n, err := j.r.ui.Move(j.ctx, full(item), true)
	j.CloseScreen()
	if err == nil && n > 0 {
		j.Logf("cofre marcado: %d %s a %v", n, item, c)
	}
	if j.Count(item) > 0 {
		j.Logf("el cofre marcado de %v está lleno: lo que no cabe, al almacén", c)
		return false
	}
	return true
}

// craftChests hace cofres hasta llevar n (8 tablones iguales cada uno).
func (j *Job) craftChests(n int) error {
	if j.Count("chest") >= n {
		return nil
	}
	t, err := j.tablePos() // la mesa primero: gasta tablones
	if err != nil {
		return fmt.Errorf("cofre: %w", err)
	}
	for tries := 0; j.Count("chest") < n && tries < n+2; tries++ {
		p, err := j.EnsureSamePlanks(8)
		if err != nil {
			return fmt.Errorf("cofre: %w", err)
		}
		if err := j.craftOnTable(t, []string{p, p, p, p, "", p, p, p, p}, 3, 1); err != nil {
			return fmt.Errorf("cofre: %w", err)
		}
	}
	if j.Count("chest") < n {
		return fmt.Errorf("cofre: la mesa no los dio")
	}
	return nil
}

// chestSlot es el sitio k de la fila del almacén: cofres dobles a lo largo
// de x (dos cofres y un hueco), desde base.X, a 2 bloques de la base en z;
// cada 8 pares, otra fila 3 más allá. La altura es la del suelo de ahí.
func (j *Job) chestSlot(base world.BlockPos, k int) (a, b world.BlockPos, ok bool) {
	x, z := base.X+3*(k%8), base.Z+2+3*(k/8)
	g, ok1 := j.r.d.Walker.Ground(x, z, base.Y)
	g2, ok2 := j.r.d.Walker.Ground(x+1, z, base.Y)
	if !ok1 || !ok2 || g.Y != g2.Y {
		return a, b, false
	}
	return g, g2, true
}

// newChest hace un cofre doble y lo pone en el siguiente sitio libre de la
// fila del almacén.
func (j *Job) newChest(cat string) (Chest, error) {
	st := j.r.Store
	if err := j.craftChests(2); err != nil {
		return Chest{}, err
	}
	base := *st.Base
	for k := 0; k < 32; k++ {
		a, b, ok := j.chestSlot(base, k)
		if !ok || st.taken(a) || st.taken(b) || j.Block(a) == "chest" || j.Block(b) == "chest" {
			continue
		}
		if err := j.placeDoubleChest(a, b); err != nil {
			j.Logf("cofre en %v: %v", a, err)
			continue
		}
		c := Chest{Pos: a, Pos2: &b, Item: cat}
		st.add(c)
		j.Logf("almacén: cofre doble nuevo para %s en %v", cat, a)
		return c, nil
	}
	return Chest{}, fmt.Errorf("no encuentro sitio para otro cofre junto a la base %v", base)
}

// placeDoubleChest pone dos cofres en a y b (vecinos en x) que se juntan en
// uno grande: los dos mirando igual (puestos desde el mismo lado, a dos
// bloques) y contra el suelo, no contra el otro cofre (eso sería con
// mayúsculas, que no los junta).
func (j *Job) placeDoubleChest(a, b world.BlockPos) error {
	for _, p := range []world.BlockPos{a, b} {
		if !j.placeable(p) {
			return fmt.Errorf("%v no está libre", p)
		}
	}
	var stood bool
	for _, dz := range []int{-2, 2} {
		stand := world.BlockPos{X: a.X, Y: a.Y, Z: a.Z + dz}
		if g, ok := j.r.d.Walker.Ground(stand.X, stand.Z, a.Y); ok && abs(g.Y-a.Y) <= 1 {
			if err := j.GoTo(g); err == nil && j.Reaches(a) && j.Reaches(b) {
				stood = true
				break
			}
		}
	}
	if !stood {
		return fmt.Errorf("no encuentro desde dónde ponerlos")
	}
	for _, p := range []world.BlockPos{a, b} {
		if j.Block(p) == "chest" {
			continue
		}
		if err := j.PlaceOn("chest", p, world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z}); err != nil {
			return err
		}
	}
	if st, ok := j.r.d.World.BlockAt(a); ok && strings.Contains(world.StateString(st), "type=single") {
		j.Logf("los cofres de %v no se juntaron (quedan dos sencillos)", a)
	}
	return nil
}

// placeable: en p se puede poner un bloque (libre, suelo firme debajo), sin
// mirar si llega la mano.
func (j *Job) placeable(p world.BlockPos) bool {
	if j.inBody(p) {
		return false
	}
	st, ok := j.r.d.World.BlockAt(p)
	if !ok || !block.Replaceable(st) {
		return false
	}
	below, ok := j.r.d.World.BlockAt(world.BlockPos{X: p.X, Y: p.Y - 1, Z: p.Z})
	return ok && len(block.CollisionShape(below)) > 0 && !block.Replaceable(below)
}

// SetupBase deja lista la zona del almacén: una mesa de trabajo y un cofre
// doble para la madera, junto a la base (que pone donde está si no hay).
func (j *Job) SetupBase() error {
	st := j.r.Store
	if st.Base == nil {
		f := j.Feet()
		st.SetBase(f)
		j.Logf("almacén: base aquí, en %v", f)
	}
	base := *st.Base
	j.Status("preparando la base en %v", base)
	if err := j.GoNear(base); err != nil {
		return fmt.Errorf("base: %w", err)
	}
	// la mesa: al principio de la fila de cofres (o lo más cerca que se pueda)
	if _, ok := j.nearTo(base, 4, func(n string) bool { return n == "crafting_table" }); !ok {
		if j.Count("crafting_table") == 0 {
			p, err := j.EnsureSamePlanks(4)
			if err != nil {
				return fmt.Errorf("mesa de la base: %w", err)
			}
			j.CloseScreen()
			if err := j.craftHere([]string{p, p, p, p}, 2, 1); err != nil {
				return fmt.Errorf("mesa de la base: %w", err)
			}
		}
		spot, ok := world.BlockPos{}, false
		if g, ok1 := j.r.d.Walker.Ground(base.X-2, base.Z+2, base.Y); ok1 && j.placeable(g) {
			spot, ok = g, true
			if err := j.GoNear(g); err != nil {
				ok = false
			}
		}
		placed := false
		if ok {
			placed = j.Place("crafting_table", spot) == nil
		}
		if !placed {
			var err error
			if spot, err = j.placeNear("crafting_table"); err != nil {
				return fmt.Errorf("mesa de la base: %w", err)
			}
		}
		j.Logf("base: mesa de trabajo en %v", spot)
	}
	// el cofre grande de la madera
	if _, ok := st.chestFor("madera"); !ok {
		if _, err := j.newChest("madera"); err != nil {
			return err
		}
	}
	j.Logf("base lista en %v: mesa y cofre doble", base)
	return nil
}

// nearTo busca un bloque que cumpla ok a menos de r (en horizontal, ±3 en
// altura) de la posición c.
func (j *Job) nearTo(c world.BlockPos, r int, ok func(string) bool) (world.BlockPos, bool) {
	for dy := -3; dy <= 3; dy++ {
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				p := world.BlockPos{X: c.X + dx, Y: c.Y + dy, Z: c.Z + dz}
				if ok(j.Block(p)) {
					return p, true
				}
			}
		}
	}
	return world.BlockPos{}, false
}
