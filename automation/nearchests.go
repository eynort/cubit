package automation

// Guardar en los cofres de alrededor: con el inventario lleno, el bot va
// andando (con el pathfinding) al cofre más cercano y deja ahí lo que no le
// hace falta. Si el servidor no le deja (un cofre protegido por un plugin:
// no se abre, avisa con "no tienes permiso", o no deja meter nada), lo dice
// por el chat y prueba el siguiente.

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/protocol/types"
)

const (
	nearChestR      = 32               // hasta dónde se buscan cofres
	nearChestDenied = 30 * time.Minute // un cofre sin permiso no se vuelve a probar en este rato
	nearChestFull   = 5 * time.Minute  // ni uno lleno
)

// chestDeniedMsg reconoce los avisos de un cofre protegido (LWC, Bolt,
// GriefPrevention, Lands, Towny…).
var chestDeniedMsg = regexp.MustCompile(`(?i)(no tienes permiso|sin permiso|no puedes (abrir|usar|acceder)|est[aá] (bloquead|protegid)|protegid[oa] por|don't have permission|do not have permission|no permission|cannot (open|access|use)|can't (open|access|use)|is locked|locked by|this (chest|container) is protected|hey! sorry)`)

// chestSkip: los cofres que no se usan un rato (sin permiso, o llenos) y si ya se dijo.
type chestSkip struct {
	until time.Time
	told  bool
}

var nearChests = struct {
	sync.Mutex
	skip map[world.BlockPos]*chestSkip
}{skip: map[world.BlockPos]*chestSkip{}}

// NoteServerMessage: un mensaje del sistema; si es de "sin permiso" se
// apunta la hora (el cofre que se estaba abriendo era protegido).
func (r *Runner) NoteServerMessage(text string) {
	if chestDeniedMsg.MatchString(text) {
		r.denied.Store(time.Now().UnixNano())
	}
}

// deniedSince: llegó un aviso de "sin permiso" después de t.
func (j *Job) deniedSince(t time.Time) bool { return j.r.denied.Load() >= t.UnixNano() }

// say escribe en el chat (si el bot tiene con qué).
func (j *Job) say(text string) {
	j.Logf("chat: %s", text)
	if j.r.d.Say != nil {
		j.r.d.Say(text)
	}
}

// chestsNear son los cofres cargados a menos de r bloques, del más cercano al
// más lejano (de uno doble, una mitad), sin los de zonas protegidas.
func (j *Job) chestsNear(r int) []world.BlockPos {
	f := j.Feet()
	var out []world.BlockPos
	for dx := -r; dx <= r; dx++ {
		for dz := -r; dz <= r; dz++ {
			if dx*dx+dz*dz > r*r {
				continue
			}
			for dy := -8; dy <= 8; dy++ {
				q := world.BlockPos{X: f.X + dx, Y: f.Y + dy, Z: f.Z + dz}
				st, ok := j.r.d.World.BlockAt(q)
				if !ok || !isChest(blockName(st)) || j.r.Store.IsProtected(q) {
					continue
				}
				dup := false
				for _, o := range out {
					if o.Y == q.Y && abs(o.X-q.X)+abs(o.Z-q.Z) == 1 && blockName(st) != "barrel" &&
						!strings.Contains(world.StateString(st), "type=single") {
						dup = true
					}
				}
				if !dup {
					out = append(out, q)
				}
			}
		}
	}
	d := func(p world.BlockPos) float64 {
		return math.Sqrt(float64((p.X-f.X)*(p.X-f.X) + (p.Y-f.Y)*(p.Y-f.Y) + (p.Z-f.Z)*(p.Z-f.Z)))
	}
	sort.Slice(out, func(a, b int) bool { return d(out[a]) < d(out[b]) })
	return out
}

func skipChest(p world.BlockPos) bool {
	nearChests.Lock()
	defer nearChests.Unlock()
	s, ok := nearChests.skip[p]
	return ok && time.Now().Before(s.until)
}

// markChest no usa el cofre p en un rato; dice si es la primera vez (para avisar una sola vez).
func markChest(p world.BlockPos, d time.Duration) (first bool) {
	nearChests.Lock()
	defer nearChests.Unlock()
	s, ok := nearChests.skip[p]
	if !ok {
		s = &chestSkip{}
		nearChests.skip[p] = s
	}
	s.until = time.Now().Add(d)
	first = !s.told
	s.told = true
	return first
}

// StoreNearby deja en los cofres de alrededor lo que store dice que se
// guarda (por slot: el nombre y el montón), del cofre más cercano al más
// lejano, hasta que no quede nada o no haya más cofres. Dice cuántos objetos
// guardó y los cofres que no le dejaron (sin permiso): quien llama lo dice
// por el chat, una sola vez (storageStuck), no un mensaje por cofre.
func (j *Job) StoreNearby(store func(name string, s types.ItemStack) bool) (int, []world.BlockPos) {
	var denied []world.BlockPos
	total := 0
	left := func() int {
		n := 0
		s := j.r.d.Screens
		s.Lock()
		defer s.Unlock()
		for i := invStart; i <= invEnd; i++ {
			sl := s.Inventory.Slots[i]
			if sl.Count > 0 && store(strings.TrimPrefix(itemName(int32(sl.Item)), "minecraft:"), sl) {
				n++
			}
		}
		return n
	}
	if left() == 0 {
		return 0, nil
	}
	chests := j.chestsNear(nearChestR)
	tried := 0
	for _, c := range chests {
		if left() == 0 || j.Fatal() != nil {
			break
		}
		if skipChest(c) {
			continue
		}
		tried++
		j.Status("inventario lleno: al cofre de %v", c)
		if err := j.GoNear(c); err != nil {
			if j.Fatal() != nil {
				break
			}
			j.Logf("cofre de %v: no llego (%v)", c, err)
			markChest(c, nearChestFull) // (otro rato se vuelve a probar)
			continue
		}
		n, refused, err := j.storeIn(c, store)
		total += n
		switch {
		case refused:
			markChest(c, nearChestDenied)
			j.Logf("cofre de %v: no tengo permiso", c)
			denied = append(denied, c)
		case err != nil:
			j.Logf("cofre de %v: %v", c, err)
			markChest(c, nearChestFull)
		case left() > 0:
			markChest(c, nearChestFull) // lleno
		}
	}
	if total > 0 {
		j.Logf("guardé %d objetos en los cofres de alrededor", total)
	} else if tried == 0 && len(chests) == 0 {
		j.Logf("no hay cofres a menos de %d bloques", nearChestR)
	}
	return total, denied
}

// storeIn abre el cofre c y mete lo que store dice. denied: el servidor no
// deja (no se abre y avisa, o se abre pero no deja meter nada con huecos libres).
func (j *Job) storeIn(c world.BlockPos, store func(string, types.ItemStack) bool) (n int, denied bool, err error) {
	start := time.Now()
	if err := j.Open(c); err != nil {
		if j.Fatal() != nil {
			return 0, false, j.Fatal()
		}
		_ = j.Wait(10) // el aviso del plugin puede llegar un poco después
		if j.deniedSince(start) || strings.Contains(err.Error(), "no se abrió") {
			return 0, true, nil
		}
		return 0, false, err
	}
	defer j.CloseScreen()
	m, ok := j.r.d.Screens.Open()
	if !ok {
		return 0, false, fmt.Errorf("no se abrió")
	}
	free := 0
	for i := 0; i < m.PlayerStart(); i++ {
		if m.Slots[i].Count <= 0 {
			free++
		}
	}
	pstart := m.PlayerStart()
	moved := 0
	for i := invStart; i <= invEnd; i++ {
		s := j.r.d.Screens
		s.Lock()
		sl := s.Inventory.Slots[i]
		s.Unlock()
		if sl.Count <= 0 || !store(strings.TrimPrefix(itemName(int32(sl.Item)), "minecraft:"), sl) {
			continue
		}
		if err := j.r.d.Screens.Click(m.ID, pstart+i-invStart, 0, types.ContainerInputQuickMove); err != nil {
			return moved, false, err
		}
		if err := j.Wait(2); err != nil {
			return moved, false, err
		}
		s.Lock()
		after := s.Inventory.Slots[i]
		s.Unlock()
		if after.Count < sl.Count || after.Item != sl.Item {
			moved += int(sl.Count) - int(max(after.Count, 0))
		}
	}
	if moved == 0 && free > 0 && j.deniedSince(start) {
		return 0, true, nil
	}
	if moved == 0 && free > 0 {
		// con huecos y sin meter nada: el servidor lo devuelve todo (protegido sin avisar)
		return 0, true, nil
	}
	if moved > 0 {
		j.Logf("cofre de %v: dejo %d objetos", c, moved)
	}
	return moved, false, nil
}

// storeWait: lo que se espera una respuesta a "no puedo guardar" antes de seguir sin guardar.
var storeWait = time.Minute

// NoteChat: un jugador escribió (en el chat o por privado): cuenta como
// respuesta a una pregunta del bot (storageStuck).
func (r *Runner) NoteChat() { r.chats.Add(1) }

// storeSig resume dónde se puede guardar (los cofres de minar y la base):
// si cambia, vale la pena volver a intentarlo.
func (j *Job) storeSig() string {
	st := j.r.Store
	base := "sin base"
	if b := st.Base; b != nil {
		base = fmt.Sprint(*b)
	}
	cs := st.MineChestList()
	keys := make([]string, 0, len(cs))
	for d, c := range cs {
		keys = append(keys, fmt.Sprintf("%s=%v", d, c))
	}
	sort.Strings(keys)
	return base + " " + strings.Join(keys, ",")
}

// storageStuck: no hay dónde guardar (why). La primera vez en la tarea lo
// dice por el chat (un solo mensaje) y espera storeWait una respuesta (que
// alguien escriba, o que se marque un cofre): con ella, retry (volver a
// intentar). Sin ella (o ya preguntado), sigue sin guardar: noStore (ni se
// recoge lo del suelo) hasta que cambie dónde se puede guardar.
func (j *Job) storageStuck(why string, denied []world.BlockPos) (retry bool) {
	if j.storageAsked {
		j.giveUpStoring()
		return false
	}
	j.storageAsked = true
	where := ""
	if len(denied) > 0 {
		var cs []string
		for i, c := range denied {
			if i == 2 {
				cs = append(cs, fmt.Sprintf("y %d más", len(denied)-2))
				break
			}
			cs = append(cs, fmt.Sprintf("%d %d %d", c.X, c.Y, c.Z))
		}
		where = " (sin permiso en los cofres de " + strings.Join(cs, ", ") + ")"
	}
	secs := int(storeWait / time.Second)
	if r := []rune(why); len(r) > 90 { // (el chat admite 256: el motivo, corto)
		why = string(r[:89]) + "…"
	}
	j.say(fmt.Sprintf("No puedo guardar: %s%s. ¿Me das acceso o me marcas un cofre (/mine chest)? Si no me contestas en %d s, sigo sin recoger.", why, where, secs))
	asked, sig := j.r.chats.Load(), j.storeSig()
	for waited := 0; waited < secs; waited++ {
		j.Status("esperando respuesta para guardar (%d s de %d)", waited, secs)
		if err := j.Rest(20); err != nil {
			return false
		}
		if j.r.chats.Load() > asked || j.storeSig() != sig {
			j.Logf("me contestaron: vuelvo a intentar guardar")
			return true
		}
	}
	j.giveUpStoring()
	return false
}

// giveUpStoring: a seguir sin guardar ni recoger (hasta que cambie dónde guardar).
func (j *Job) giveUpStoring() {
	if !j.noStore {
		j.Problem("nadie contestó: sigo sin guardar ni recoger lo del suelo (si me marcas un cofre con /mine chest o /base here, vuelvo a guardar)")
	}
	j.noStore, j.noStoreSig = true, j.storeSig()
}

// storingAgain: sin guardar (noStore), si ya hay otro sitio donde guardar,
// se vuelve a intentar. Dice si se sigue sin guardar.
func (j *Job) storingOff() bool {
	if j.noStore && j.storeSig() != j.noStoreSig {
		j.Logf("hay otro sitio donde guardar: vuelvo a guardar y a recoger")
		j.noStore, j.storageAsked = false, false
	}
	return j.noStore
}

// storeFar: un cofre (o la base del almacén) más lejos que esto, en
// horizontal, no se usa: ir y volver es medio mundo (el bot se teletransporta
// con /tpa a otra zona y el cofre se queda donde estaba: a 9000 bloques).
const storeFar = 128

// tooFar: p está a más de storeFar bloques en horizontal del bot. La primera
// vez en la tarea lo dice (what: "el cofre de minar", "la base del almacén").
func (j *Job) tooFar(p world.BlockPos, what string) bool {
	f := j.Feet()
	d := math.Hypot(float64(p.X-f.X), float64(p.Z-f.Z))
	if d <= storeFar {
		return false
	}
	if j.farTold == nil {
		j.farTold = map[string]bool{}
	}
	if !j.farTold[what] {
		j.farTold[what] = true
		j.Logf("%s está a %.0f bloques (%v): no voy hasta allí; marca uno cerca con /mine chest (o /base here) si quieres que guarde aquí", what, d, p)
	}
	return true
}

// nearMineChest es el cofre de minar de la dimensión dim si está cerca (tooFar no).
func (j *Job) nearMineChest(dim string) (world.BlockPos, bool) {
	if j.localChest != nil && j.localChestDim == dim && isChest(j.Block(*j.localChest)) {
		return *j.localChest, true // (el que se hizo en esta tarea)
	}
	c, ok := j.r.Store.MineChest(dim)
	if !ok || j.Dimension() == dim && j.tooFar(c, "el cofre de minar") {
		return world.BlockPos{}, false
	}
	return c, true
}
