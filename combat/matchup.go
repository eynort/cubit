package combat

// Antes de pelear con un jugador, el bot mira su equipo (lo que lleva puesto
// y en las manos se ve: arma, armadura, escudo, y los encantamientos, que van
// con los objetos) y el suyo, y calcula el intercambio de golpes:
//
//   - Lo que entra de un golpe de cada uno: el arma (y su filo: 0.5 por nivel
//     y 0.5), contra la armadura del otro (puntos y dureza) y su protección
//     (4 % por nivel de cada pieza, hasta el 80 %). Aspecto ígneo: algo más.
//   - Cuántos golpes y cuánto tiempo hace falta para matar a cada uno: su
//     vida (y la absorción), más lo que el bot tiene para curarse (tótem,
//     manzanas, pociones de curación); el ritmo de su arma. Con escudo, y si el
//     otro no lleva hacha, se le come menos.
//
// La relación (lo que tarda él en matar al bot entre lo que tarda el bot en
// matarle) da la postura:
//
//   - press (gana claro): a por él, sin esperar tapado; no se retira.
//   - even: lo de siempre (thinkFight).
//   - cautious (pierde): el escudo arriba siempre que no sea su ventana de
//     castigo, fuera de su alcance mientras recarga, el arco antes, retirarse
//     antes, y para curarse, encerrarse con bloques (bunker.go).
//   - avoid (no se le gana: un full diamante con filo V contra uno de hierro):
//     no se le busca (en la zona se le deja); si ataca, perla para irse,
//     flechas de lejos o encerrarse a curarse y que se canse. Si no hay nada de
//     eso, se pelea con cautela (huir corriendo de un jugador no sirve).

import (
	"fmt"
	"math"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26/level/component"
)

type stance int

const (
	stanceEven stance = iota
	stancePress
	stanceCautious
	stanceAvoid
)

func (s stance) String() string {
	return [...]string{"igualado", "le gano", "me supera", "no le gano"}[s]
}

// loadout es lo que lleva un luchador, en lo que cuenta para el intercambio.
type loadout struct {
	weapon  string  // el arma ("" el puño)
	hit     float64 // un golpe cargado (con el filo)
	speed   float64 // golpes por segundo
	fire    int     // aspecto ígneo
	armor   float64 // puntos de armadura
	tough   float64 // dureza
	prot    float64 // niveles de protección (en todas las piezas)
	pieces  []string
	shield  bool
	axe     bool
	hp      float64 // vida y absorción
	reserve float64 // lo que puede curarse en la pelea (solo se sabe del bot)
}

// matchup es la cuenta de una pelea contra un jugador.
type matchup struct {
	me, them        loadout
	mine, theirs    float64 // lo que entra de un golpe: del bot a él, de él al bot
	myHits, hisHits int     // golpes que hacen falta: del bot para matarle, de él para matar al bot
	myTime, hisTime float64 // segundos
	ratio           float64 // hisTime / myTime: más de 1, gana el bot
	stance          stance
}

func (m matchup) String() string {
	return fmt.Sprintf("%s (%s, %.1f por golpe) contra mí (%s, %.1f por golpe): le hacen falta %d golpes (%.1f s) y a mí me matan en %d (%.1f s): %s",
		m.them.describe(), shortName(m.them.weapon), m.theirs, m.me.describe(), m.mine, m.myHits, m.myTime, m.hisHits, m.hisTime, m.stance)
}

func shortName(item string) string {
	if item == "" {
		return "puño"
	}
	return strings.TrimPrefix(item, "minecraft:")
}

// describe: "full diamante prot 4" o lo que lleve.
func (l loadout) describe() string {
	if len(l.pieces) == 0 {
		return "sin armadura"
	}
	mat := map[string]int{}
	for _, p := range l.pieces {
		m, _, _ := strings.Cut(strings.TrimPrefix(p, "minecraft:"), "_")
		if es, ok := materials[m]; ok {
			m = es
		}
		mat[m]++
	}
	var out []string
	for m, n := range mat {
		if n == 4 {
			out = append(out, "full "+m)
		} else {
			out = append(out, fmt.Sprintf("%d de %s", n, m))
		}
	}
	s := strings.Join(out, "+")
	if l.prot > 0 {
		s += fmt.Sprintf(" protección %.0f", l.prot/float64(len(l.pieces)))
	}
	return s
}

var materials = map[string]string{"leather": "cuero", "chainmail": "malla", "iron": "hierro", "golden": "oro",
	"diamond": "diamante", "netherite": "netherita", "turtle": "tortuga"}

// enchant es el nivel del encantamiento en el objeto (0: no lo tiene).
func (f *Fighter) enchant(s screen.Slot, name string) int {
	if s.Count <= 0 {
		return 0
	}
	c := act.Component[*component.Enchantments](s)
	if c == nil {
		return 0
	}
	for _, e := range c.Enchantments {
		if f.enchantName(int32(e.Key)) == "minecraft:"+name {
			return int(e.Val)
		}
	}
	return 0
}

// enchantName: el registro de encantamientos lo manda el servidor (su orden
// es el id); sin él, el de vanilla.
func (f *Fighter) enchantName(id int32) string {
	if f.d.Client != nil {
		if k, ok := f.d.Client.Registries.Enchantment.KeyOf(id); ok {
			return k
		}
	}
	if id >= 0 && int(id) < len(vanillaEnchantments) {
		return vanillaEnchantments[id]
	}
	return ""
}

var vanillaEnchantments = []string{"minecraft:aqua_affinity", "minecraft:bane_of_arthropods", "minecraft:binding_curse",
	"minecraft:blast_protection", "minecraft:breach", "minecraft:channeling", "minecraft:density", "minecraft:depth_strider",
	"minecraft:efficiency", "minecraft:feather_falling", "minecraft:fire_aspect", "minecraft:fire_protection", "minecraft:flame",
	"minecraft:fortune", "minecraft:frost_walker", "minecraft:impaling", "minecraft:infinity", "minecraft:knockback",
	"minecraft:looting", "minecraft:loyalty", "minecraft:luck_of_the_sea", "minecraft:lunge", "minecraft:lure", "minecraft:mending",
	"minecraft:multishot", "minecraft:piercing", "minecraft:power", "minecraft:projectile_protection", "minecraft:protection",
	"minecraft:punch", "minecraft:quick_charge", "minecraft:respiration", "minecraft:riptide", "minecraft:sharpness",
	"minecraft:silk_touch", "minecraft:smite", "minecraft:soul_speed", "minecraft:sweeping_edge", "minecraft:swift_sneak",
	"minecraft:thorns", "minecraft:unbreaking", "minecraft:vanishing_curse", "minecraft:wind_burst"}

// sharpBonus es lo que suma el filo a un golpe (Java: 0.5 por nivel y 0.5).
func sharpBonus(level int) float64 {
	if level <= 0 {
		return 0
	}
	return 0.5*float64(level) + 0.5
}

// armed: lo que da un objeto como arma (con su filo).
func (f *Fighter) armedWith(s screen.Slot) (hit, speed float64, fire int) {
	w := weaponOf(s)
	return w.damage + sharpBonus(f.enchant(s, "sharpness")), w.speed, f.enchant(s, "fire_aspect")
}

// wear suma una pieza de armadura.
func (f *Fighter) wear(l *loadout, s screen.Slot) {
	if s.Count <= 0 {
		return
	}
	a, t := armorPiece(s)
	if a <= 0 {
		return
	}
	l.armor += a
	l.tough += t
	l.prot += float64(f.enchant(s, "protection"))
	l.pieces = append(l.pieces, nameOf(s))
}

// armsSeen: el arma más fuerte que se le ha visto en la mano a un jugador, y
// cuándo. Cambiar de objeto es instantáneo: uno que saca el puño, un cubo o
// bloques un momento sigue teniendo la espada (en los logs: con el puño a la
// vista la cuenta pasaba a "le gano", el bot entraba, y él volvía a la
// espada de diamante).
type armsSeen struct {
	weapon string
	hit    float64
	speed  float64
	fire   int
	at     uint64 // el paso
}

// armsMemory: los pasos que se recuerda el arma (un minuto).
const armsMemory = 1200

// theirLoadout es lo que se le ve a un jugador (con el arma más fuerte que
// se le ha visto hace poco, aunque ahora lleve otra cosa en la mano).
func (f *Fighter) theirLoadout(e entities.Entity) loadout {
	l := loadout{hp: health(e), speed: 4, hit: 1}
	if it, ok := e.Equipment[0]; ok && it.Count > 0 {
		s := screen.Slot(it)
		l.weapon = nameOf(s)
		l.hit, l.speed, l.fire = f.armedWith(s)
		l.axe = isAxe(l.weapon)
	}
	if f.arms == nil {
		f.arms = map[int32]armsSeen{}
	}
	cur := l.hit * math.Min(l.speed, 1.6) // (lo que pesa un arma: golpe y ritmo, el puño no cuenta como rápido)
	if l.weapon == "" || l.speed <= 0 {
		cur = 0
	}
	if a, ok := f.arms[e.ID]; ok && f.steps-a.at < armsMemory && a.hit*math.Min(a.speed, 1.6) > cur {
		l.weapon, l.hit, l.speed, l.fire = a.weapon, a.hit, a.speed, a.fire
		l.axe = isAxe(l.weapon)
	} else if cur > 0 && (armedName(l.weapon) || l.fire > 0) {
		f.arms[e.ID] = armsSeen{weapon: l.weapon, hit: l.hit, speed: l.speed, fire: l.fire, at: f.steps}
	}
	if it, ok := e.Equipment[1]; ok && itemName(int32(it.Item)) == "minecraft:shield" && it.Count > 0 {
		l.shield = true
	}
	for slot := 2; slot <= 5; slot++ {
		if it, ok := e.Equipment[slot]; ok {
			f.wear(&l, screen.Slot(it))
		}
	}
	return l
}

// myLoadout es lo del bot: la mejor arma que lleve contra esa armadura, lo puesto y lo que tiene para curarse.
func (f *Fighter) myLoadout(st basic.Status, vs loadout) loadout {
	inv, _ := f.inventory()
	l := loadout{hp: float64(st.Health) + absorption(st), speed: 4, hit: 1}
	best := -1.0
	for i := slotMainStart; i <= slotOffhand; i++ {
		hit, speed, fire := f.armedWith(inv[i])
		if speed <= 0 || hit <= 1 {
			continue
		}
		dps := afterArmor(hit, vs.armor, vs.tough) * speed
		if dps > best {
			best = dps
			l.weapon, l.hit, l.speed, l.fire = nameOf(inv[i]), hit, speed, fire
		}
	}
	l.axe = isAxe(l.weapon)
	for i := slotArmorHead; i <= slotArmorHead+3; i++ {
		f.wear(&l, inv[i])
	}
	for i := slotMainStart; i <= slotOffhand; i++ {
		switch nameOf(inv[i]) {
		case "minecraft:shield":
			l.shield = true
		case "minecraft:totem_of_undying":
			l.reserve += 10 // (1 de vida, absorción II, regeneración II)
		case "minecraft:golden_apple":
			l.reserve += 6 * float64(min(inv[i].Count, 4)) // (4 de absorción y unos 4 de regeneración; comerlas cuesta tiempo)
		case "minecraft:enchanted_golden_apple":
			l.reserve += 16 * float64(min(inv[i].Count, 2))
		}
	}
	for _, p := range f.potions() {
		if p.effect() == "healing" {
			if strings.HasPrefix(p.kind, "strong_") {
				l.reserve += 8
			} else {
				l.reserve += 4
			}
		}
	}
	return l
}

// blow es lo que entra de un golpe cargado de a en d.
func blow(a, d loadout) float64 {
	dmg := afterArmor(a.hit, d.armor, d.tough) * (1 - math.Min(20, d.prot)/25)
	return dmg + 0.5*float64(a.fire) // (el fuego: unos puntos más mientras dura, contados por golpe)
}

// assess cuenta la pelea contra e.
func (f *Fighter) assess(e entities.Entity, st basic.Status) matchup {
	var m matchup
	m.them = f.theirLoadout(e)
	m.me = f.myLoadout(st, m.them)
	m.mine, m.theirs = blow(m.me, m.them), blow(m.them, m.me)
	// el escudo: contra quien no lleva hacha se come una parte de sus golpes
	// (el ritmo tapar-castigar: medido, unos 4 de cada 10)
	mine, theirs := m.mine, m.theirs
	if m.them.shield && !m.me.axe {
		mine *= 0.6
	}
	if m.me.shield && !m.them.axe {
		theirs *= 0.6
	}
	myHP := m.me.hp + 0.7*m.me.reserve // (curarse en plena pelea no sale entero)
	m.myHits = int(math.Ceil(m.them.hp / math.Max(mine, 0.1)))
	m.hisHits = int(math.Ceil(myHP / math.Max(theirs, 0.1)))
	m.myTime = float64(m.myHits-1) / m.me.speed
	m.hisTime = float64(m.hisHits-1) / m.them.speed
	m.ratio = (m.hisTime + 0.5) / (m.myTime + 0.5)
	switch {
	case m.ratio >= 1.5:
		m.stance = stancePress
	case m.ratio >= 0.8:
		m.stance = stanceEven
	case m.ratio >= 0.4:
		m.stance = stanceCautious
	default:
		m.stance = stanceAvoid
	}
	return m
}

// matchupFor: la cuenta contra t (se rehace cada segundo, con la vida de
// ahora); se dice en el log al empezar con él o al cambiar la postura.
func (f *Fighter) matchupFor(t *foe, st basic.Status) matchup {
	if t.id == f.matchID && f.steps-f.matchAt < 20 {
		return f.match
	}
	m := f.assess(t.e, st)
	if t.id != f.matchID || m.stance != f.match.stance {
		f.logf("contra %s: %s", t.name, m)
	}
	f.match, f.matchID, f.matchAt = m, t.id, f.steps
	return m
}

// armedName: un arma de verdad (no una herramienta que se lleva para otra cosa).
func armedName(n string) bool {
	return strings.HasSuffix(n, "_sword") || isAxe(n) || n == "minecraft:trident" || n == "minecraft:mace" || strings.HasSuffix(n, "_spear")
}
