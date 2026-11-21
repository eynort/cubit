package combat

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/entities"
)

// hostileMobs son los mobs que atacan sin que se les provoque: se les pega
// antes de que lleguen (cerca del bot, o del que protege).
var hostileMobs = map[string]bool{
	"zombie": true, "husk": true, "drowned": true, "zombie_villager": true, "skeleton": true, "stray": true,
	"bogged": true, "spider": true, "cave_spider": true, "creeper": true, "witch": true, "slime": true,
	"magma_cube": true, "phantom": true, "pillager": true, "vindicator": true, "evoker": true, "ravager": true,
	"vex": true, "blaze": true, "wither_skeleton": true, "piglin_brute": true, "hoglin": true, "zoglin": true,
	"silverfish": true, "endermite": true, "breeze": true, "creaking": true, "parched": true,
}

// notFightable son entidades que no son seres a los que pegar (objetos,
// flechas, barcos…): nunca son enemigos aunque el juego las nombre como causa.
var notFightable = []string{
	"item", "experience_orb", "arrow", "trident", "boat", "raft", "minecart", "falling_block", "tnt",
	"painting", "item_frame", "armor_stand", "fireball", "wither_skull", "snowball", "egg", "ender_pearl",
	"potion", "eye_of_ender", "firework_rocket", "llama_spit", "shulker_bullet", "area_effect_cloud",
	"lightning_bolt", "marker", "interaction", "_display", "fishing_bobber", "evoker_fangs", "leash_knot",
	"end_crystal", "wind_charge", "ominous_item_spawner",
}

// rangedMobs disparan desde lejos: alejarse 16 bloques no basta.
var rangedMobs = map[string]bool{"skeleton": true, "stray": true, "bogged": true, "pillager": true, "blaze": true,
	"ghast": true, "witch": true, "parched": true}

// avoidMobs no se pelean nunca: se huye de ellos.
var avoidMobs = map[string]bool{"warden": true}

func kind(e entities.Entity) string { return strings.TrimPrefix(e.Type, "minecraft:") }

// fightable: e es un ser al que se puede pegar (un jugador o un mob).
func fightable(e entities.Entity) bool {
	k := kind(e)
	for _, n := range notFightable {
		if strings.Contains(k, n) {
			return false
		}
	}
	return true
}

// label es cómo se nombra a un enemigo en el log: su nombre si es un
// jugador, si no su tipo ("zombie#123").
func (f *Fighter) label(e entities.Entity) string {
	if e.Type == "minecraft:player" {
		return f.nameOf(e.UUID, e.ID)
	}
	return fmt.Sprintf("%s#%d", kind(e), e.ID)
}

// engage hace de e un enemigo (o, si es de los que se evitan, alguien de quien huir).
// why es lo que se dice en el log la primera vez ("me atacó"). hit: le pegó al bot.
func (f *Fighter) engage(e entities.Entity, why string, hit bool) {
	if !fightable(e) || f.isGuarded(e) || e.ID == int32(f.d.Player.Login.PlayerID) || dead(e) {
		return
	}
	if e.Type == "minecraft:player" && f.d.Ally != nil && f.d.Ally(e.UUID) {
		return // otro bot del panel: un golpe suyo es sin querer (un barrido), no pelea
	}
	// un mob que pega a quien va sin arma (a puñetazos no se mata a nada, y en
	// difícil dos zombis matan en segundos): se huye, corriendo se les deja atrás
	if hit && f.guard.Load() == nil && e.Type != "minecraft:player" && !avoidMobs[kind(e)] && !f.armed() {
		f.avoid(e, why+" y voy sin arma: huyo")
		return
	}
	name := f.label(e)
	f.mu.Lock()
	if until, ok := f.ignored[e.ID]; ok && !hit && f.steps < until {
		f.mu.Unlock()
		return // no se llega a él: hasta que pegue, no se le busca
	}
	delete(f.ignored, e.ID)
	a, known := f.aggressors[e.ID]
	if !known {
		a = &aggressor{id: e.ID, name: name, avoid: avoidMobs[kind(e)]}
		f.aggressors[e.ID] = a
	}
	a.lastHit, a.fromZone = time.Now(), false
	if hit {
		a.hits++
	}
	f.mu.Unlock()
	if !known {
		if a.avoid {
			f.logf("%s %s: huyo de él", name, why)
		} else {
			f.logf("%s %s", name, why)
		}
	}
}

// scanThreats: los mobs hostiles que están cerca del bot (5 bloques) o del
// que protege (6) se atacan antes de que peguen; de un warden (24) se huye.
func (f *Fighter) scanThreats() {
	p := f.d.Player.Position()
	weak := f.d.Player.Status().Health < 10 // con poca vida: solo se pelea con quien ataque
	armed := f.armed()
	shield := f.hasItem(itemShield)
	for _, e := range f.d.Entities.Nearby(p.X, p.Y, p.Z, 24) {
		k := kind(e)
		d := math.Sqrt(e.DistanceSqr(p.X, p.Y, p.Z))
		switch {
		case avoidMobs[k]:
			f.engage(e, "está cerca", false)
		case k == "creeper" && d <= 8 && (!armed || weak && !shield):
			// a puñetazos un creeper explota antes de morir, y con poca vida y
			// sin escudo no se arriesga: se huye
			f.avoid(e, "se acerca y no puedo con él: huyo")
		case rangedMobs[k] && d <= 16 && armed && aiming(e, p):
			// un tirador que tensa el arco: a por él ya (no al primer flechazo)
			f.engage(e, "me apunta: voy a por él", false)
		case k == "creeper" && d <= 8:
			// un creeper se vigila siempre (aunque vaya con poca vida): sin
			// verlo venir explota encima
			f.engage(e, "se acerca: me defiendo", false)
		case hostileMobs[k] && d <= 5 && !weak && armed:
			f.engage(e, "se acerca: me defiendo", false)
		}
	}
	g := f.guard.Load()
	if g == nil {
		return
	}
	boss, ok := f.d.Entities.ByUUID(g.id)
	if !ok {
		return
	}
	for _, e := range f.d.Entities.Nearby(boss.X, boss.Y, boss.Z, 6) {
		if hostileMobs[kind(e)] {
			f.engage(e, "se acerca a "+g.name+": lo defiendo", false)
		}
	}
}

// hasItem: lleva el objeto (en cualquier sitio del inventario o en la mano izquierda).
func (f *Fighter) hasItem(name string) bool {
	inv, _ := f.inventory()
	for i := slotMainStart; i <= slotOffhand; i++ {
		if nameOf(inv[i]) == name {
			return true
		}
	}
	return false
}

// armed: lleva algo con lo que pelear de verdad (una espada, un hacha; no el
// puño ni una herramienta cualquiera). Sin arma no se buscan peleas con mobs:
// solo se defiende de quien le pegue.
func (f *Fighter) armed() bool {
	inv, _ := f.inventory()
	for i := slotMainStart; i <= slotHotbar+8; i++ {
		if weaponOf(inv[i]).damage >= 4 {
			return true
		}
	}
	return false
}

// avoid hace de e alguien de quien huir (no se le pega).
func (f *Fighter) avoid(e entities.Entity, why string) {
	if !fightable(e) {
		return
	}
	name := f.label(e)
	f.mu.Lock()
	a, known := f.aggressors[e.ID]
	if !known {
		a = &aggressor{id: e.ID, name: name}
		f.aggressors[e.ID] = a
	}
	wasAvoid := a.avoid
	a.avoid, a.lastHit, a.fromZone = true, time.Now(), false
	f.mu.Unlock()
	if !wasAvoid {
		f.logf("%s %s", a.name, why)
	}
}

// beWary: un jugador armado (espada, hacha, tridente) viene corriendo hacia
// el bot y mirándolo, a menos de 8 bloques: en guardia (arma en mano, escudo
// listo, mirándolo). No se ataca a nadie por venir (puede ser un amigo de
// paso): el primero que pega es él, pero su golpe da en el escudo. Dice si
// está en guardia.
func (f *Fighter) beWary(ctx context.Context, st basic.Status) bool {
	cur := *f.plan.Load()
	p := f.d.Player.Position()
	var threat *entities.Entity
	near := map[int32]bool{}
	defer func() { // (se olvida a los que ya no están cerca)
		for id := range f.waryDist {
			if !near[id] {
				delete(f.waryDist, id)
			}
		}
	}()
	for _, e := range f.d.Entities.Nearby(p.X, p.Y, p.Z, 16) {
		near[e.ID] = true
		if d := math.Hypot(e.X-p.X, e.Z-p.Z); d > 8 {
			// más lejos: solo para ver venir a uno al que no se le gana (encerrarse
			// a tiempo: la pared de su lado lleva un segundo)
			if f.farThreat(e, d, st) {
				return true
			}
			continue
		}
		if e.Type != "minecraft:player" || e.ID == int32(f.d.Player.Login.PlayerID) || f.isGuarded(e) || dead(e) ||
			f.d.Friend != nil && f.d.Friend(e.UUID) || !armedEntity(e) || !facing(e, p) || f.steps < f.waryIgnore[e.ID] {
			continue
		}
		// viene: más cerca que hace un momento (o ya encima)
		d := math.Hypot(e.X-p.X, e.Z-p.Z)
		last, seen := f.waryDist[e.ID]
		f.waryDist[e.ID] = d
		if d < 3.5 || seen && d < last-0.05 || cur.mode == wary && cur.target == e.ID && d < 6 {
			e := e
			threat = &e
			break
		}
	}
	if threat == nil {
		if cur.mode == wary {
			f.setPlan(plan{})
		}
		return false
	}
	if cur.mode != wary || cur.target != threat.ID {
		f.logf("%s viene armado hacia mí: en guardia", f.label(*threat))
		f.waryFrom = f.steps
	}
	// 10 s en guardia y no ataca: no viene a por el bot (está por aquí, de
	// paso). Se le deja de vigilar un minuto (si pega, es pelea igual)
	if f.steps-f.waryFrom > 200 {
		if f.waryIgnore == nil {
			f.waryIgnore = map[int32]uint64{}
		}
		f.waryIgnore[threat.ID] = f.steps + 1200
		f.logf("%s lleva 10 s cerca sin atacar: dejo la guardia", f.label(*threat))
		f.setPlan(plan{})
		return false
	}
	// uno al que no se le gana (su equipo contra el del bot): mejor encerrarse
	// antes de que llegue que esperarle con el escudo
	t := &foe{aggressor: aggressor{id: threat.ID, name: f.label(*threat)}, e: *threat, dist: math.Hypot(threat.X-p.X, threat.Z-p.Z)}
	if why, ok := f.shelterFrom(t, st); ok {
		f.startBunker(t, why)
		return true
	}
	inv, _ := f.inventory()
	if slot, ok := bestArm(inv, -1); ok && !f.d.Walker.Building() {
		_ = f.hold(ctx, slot)
	}
	f.manageOffhand(ctx, []foe{{e: *threat, dist: math.Hypot(threat.X-p.X, threat.Z-p.Z)}}, st.Health, 9)
	inv, _ = f.inventory()
	f.stopMoving()
	f.setPlan(plan{mode: wary, target: threat.ID, shield: nameOf(inv[slotOffhand]) == itemShield})
	return true
}

// armedEntity: lleva un arma en la mano (espada, hacha, tridente, maza).
func armedEntity(e entities.Entity) bool {
	it, ok := e.Equipment[0]
	if !ok || it.Count <= 0 {
		return false
	}
	n := itemName(int32(it.Item))
	return strings.HasSuffix(n, "_sword") || isAxe(n) || n == "minecraft:trident" || n == "minecraft:mace"
}

// farThreat: uno armado que viene de lejos (8 a 16 bloques) y al que no se le
// gana: a encerrarse ya, que pegado no da tiempo. Dice si empezó el búnker.
func (f *Fighter) farThreat(e entities.Entity, d float64, st basic.Status) bool {
	p := f.d.Player.Position()
	if e.Type != "minecraft:player" || e.ID == int32(f.d.Player.Login.PlayerID) || f.isGuarded(e) || dead(e) ||
		f.d.Friend != nil && f.d.Friend(e.UUID) || !armedEntity(e) || !facing(e, p) {
		return false
	}
	last, seen := f.waryDist[e.ID]
	f.waryDist[e.ID] = d
	if !seen || d >= last-0.05 {
		return false
	}
	t := &foe{aggressor: aggressor{id: e.ID, name: f.label(e)}, e: e, dist: d}
	why, ok := f.shelterFrom(t, st)
	if ok {
		f.startBunker(t, why)
	}
	return ok
}

// shelterFrom: encerrarse antes de que llegue t: no se le gana, o con la
// vida de ahora le supera (con menos de 14: curarse primero; pegado ya no
// da tiempo).
func (f *Fighter) shelterFrom(t *foe, st basic.Status) (string, bool) {
	m := f.matchupFor(t, st)
	switch {
	case !f.wantBunker(t):
		return "", false
	case m.stance == stanceAvoid:
		return "viene uno al que no le gano", true
	case m.stance == stanceCautious && st.Health < 14:
		return fmt.Sprintf("viene y con vida %.1f me supera: me curo antes", st.Health), true
	}
	return "", false
}
