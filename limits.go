package main

// Límites: lo que el bot no hace nunca, mande quien mande y haga la tarea que
// haga (se comprueba en las manos, act.Hands.Guard, por donde pasa todo):
//
//   - never_break: bloques que no rompe (cofres, camas, carteles, puertas…;
//     "*" vale como comodín: "*_bed"). Los que puso él sí.
//   - area: solo pica y pone bloques dentro de x1 z1 x2 z2.
//   - no_pvp: no ataca a jugadores (ni defendiéndose).
//
// Los reflejos que le salvan la vida (lava, caída, fuego) pueden saltárselos.
// Van en bot.json ("limits"); --area, --pvp y --never-break los cambian.

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/level/block"
)

type limits struct {
	NeverBreak []string `json:"never_break"`    // nil: defaultNeverBreak
	Area       *[4]int  `json:"area,omitempty"` // x1 z1 x2 z2
	NoPvP      bool     `json:"no_pvp,omitempty"`
}

var defaultNeverBreak = []string{
	"chest", "trapped_chest", "barrel", "ender_chest", "*shulker_box", "*_bed", "*sign", "*_door", "*_trapdoor",
	"furnace", "blast_furnace", "smoker", "hopper", "dropper", "dispenser", "beacon", "lectern", "enchanting_table",
	"*anvil", "brewing_stand", "respawn_anchor", "crafter", "decorated_pot", "chiseled_bookshelf", "item_frame", "jukebox",
}

var (
	areaFlag       = flag.String("area", "", `zona donde puede picar y poner bloques: "x1 z1 x2 z2" ("-" = sin límite; se guarda en bot.json)`)
	pvpFlag        = flag.String("pvp", "", "on: puede atacar a jugadores (si se lo mandas o le atacan); off: nunca (se guarda)")
	neverBreakFlag = flag.String("never-break", "", `bloques que no rompe nunca, separados por comas (comodín *: "*_bed"); "default" = la lista de siempre (se guarda)`)
)

func applyLimitFlags() {
	if a := strings.TrimSpace(*areaFlag); a != "" {
		v, err := parseArea(a)
		if err != nil {
			log.Fatalf("--area: %v", err)
		}
		cfg.Limits.Area = v
	}
	switch strings.ToLower(*pvpFlag) {
	case "on", "true", "si", "sí", "yes":
		cfg.Limits.NoPvP = false
	case "off", "false", "no":
		cfg.Limits.NoPvP = true
	}
	if nb := strings.TrimSpace(*neverBreakFlag); nb != "" {
		cfg.Limits.NeverBreak = parseNeverBreak(nb)
	}
}

// parseArea: "x1 z1 x2 z2" (o con comas); "" o "-" = sin límite (nil).
func parseArea(a string) (*[4]int, error) {
	if a = strings.TrimSpace(a); a == "" || a == "-" || strings.EqualFold(a, "off") {
		return nil, nil
	}
	f := strings.Fields(strings.ReplaceAll(a, ",", " "))
	if len(f) != 4 {
		return nil, fmt.Errorf(`"x1 z1 x2 z2" (cuatro números), no %q`, a)
	}
	var v [4]int
	for i := range 4 {
		n, err := strconv.Atoi(f[i])
		if err != nil {
			return nil, fmt.Errorf(`"x1 z1 x2 z2" (cuatro números), no %q`, a)
		}
		v[i] = n
	}
	return &v, nil
}

// parseNeverBreak: la lista separada por comas; "default" = nil (la de
// siempre); vacía = ninguno.
func parseNeverBreak(nb string) []string {
	if strings.TrimSpace(nb) == "default" {
		return nil
	}
	out := []string{}
	for _, x := range strings.Split(nb, ",") {
		if x = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(x)), "minecraft:"); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// ErrLimit: lo que el bot no hace por los límites.
var ErrLimit = errors.New("límite")

// placedByMe: dónde puso bloques (esos sí los puede romper).
var placedByMe = struct {
	sync.Mutex
	m map[world.BlockPos]bool
}{m: map[world.BlockPos]bool{}}

func (l limits) neverBreak() []string {
	if l.NeverBreak == nil {
		return defaultNeverBreak
	}
	return l.NeverBreak
}

// forbidden: name (sin "minecraft:") está en la lista.
func (l limits) forbidden(name string) bool {
	for _, p := range l.neverBreak() {
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}

func (l limits) inArea(p world.BlockPos) bool {
	a := l.Area
	if a == nil {
		return true
	}
	return p.X >= min(a[0], a[2]) && p.X <= max(a[0], a[2]) && p.Z >= min(a[1], a[3]) && p.Z <= max(a[1], a[3])
}

func currentLimits() limits {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	return cfg.Limits
}

// guard es act.Hands.Guard.
func (b *Bot) guard(op string, p world.BlockPos, entity int32) error {
	if b.reflexActive() {
		return nil // (salvarse la vida va antes)
	}
	l := currentLimits()
	switch op {
	case "place":
		if !l.inArea(p) {
			return b.refuse(fmt.Errorf("%w: %v está fuera de la zona permitida", ErrLimit, p))
		}
		placedByMe.Lock()
		if len(placedByMe.m) > 8192 {
			clear(placedByMe.m)
		}
		placedByMe.m[p] = true
		placedByMe.Unlock()
	case "dig":
		if !l.inArea(p) {
			return b.refuse(fmt.Errorf("%w: %v está fuera de la zona permitida", ErrLimit, p))
		}
		s, ok := b.World.BlockAt(p)
		if !ok || int(s) >= len(block.StateList) {
			return nil
		}
		name := strings.TrimPrefix(block.StateList[s].ID(), "minecraft:")
		if l.forbidden(name) {
			placedByMe.Lock()
			mine := placedByMe.m[p]
			placedByMe.Unlock()
			if !mine {
				return b.refuse(fmt.Errorf("%w: no rompo %s (%v)", ErrLimit, name, p))
			}
		}
	case "attack":
		if !l.NoPvP || b.Entities == nil {
			return nil
		}
		if e, ok := b.Entities.Get(entity); ok && e.Type == "minecraft:player" {
			return b.refuse(fmt.Errorf("%w: no ataco a jugadores (no_pvp)", ErrLimit))
		}
	}
	return nil
}

func (b *Bot) reflexActive() bool {
	return b.Lava != nil && b.Lava.Escaping() || b.Fire != nil && b.Fire.Active() || b.Fall != nil && b.Fall.Active()
}

var lastRefusal struct {
	sync.Mutex
	msg string
	at  time.Time
}

// refuse lo apunta en el log (lo mismo, como mucho una vez por minuto).
func (b *Bot) refuse(err error) error {
	lastRefusal.Lock()
	defer lastRefusal.Unlock()
	if err.Error() != lastRefusal.msg || time.Since(lastRefusal.at) > time.Minute {
		lastRefusal.msg, lastRefusal.at = err.Error(), time.Now()
		log.Print(err)
	}
	return err
}

// describeLimits: los límites en una línea (al arrancar y en /limits).
func describeLimits() string {
	l := currentLimits()
	var parts []string
	if l.Area != nil {
		parts = append(parts, fmt.Sprintf("zona %d %d → %d %d", l.Area[0], l.Area[1], l.Area[2], l.Area[3]))
	}
	if l.NoPvP {
		parts = append(parts, "sin PvP")
	}
	nb := l.neverBreak()
	if len(nb) > 0 {
		parts = append(parts, fmt.Sprintf("no rompe %d tipos de bloque (%s…)", len(nb), strings.Join(nb[:min(4, len(nb))], ", ")))
	}
	if len(parts) == 0 {
		return "sin límites"
	}
	return strings.Join(parts, "; ")
}

// cmdLimits: /limits [area x1 z1 x2 z2|area off|pvp on|off|deny <b>|allow <b>|reset].
func cmdLimits(_ *Bot, args []string) error {
	if len(args) > 0 {
		cfgMu.Lock()
		l := &cfg.Limits
		var err error
		switch strings.ToLower(args[0]) {
		case "area":
			if len(args) == 2 && strings.EqualFold(args[1], "off") {
				l.Area = nil
			} else if len(args) == 5 {
				var v [4]int
				for i := range 4 {
					if v[i], err = strconv.Atoi(args[i+1]); err != nil {
						break
					}
				}
				if err == nil {
					l.Area = &v
				}
			} else {
				err = errors.New("/limits area <x1> <z1> <x2> <z2> | off")
			}
		case "pvp":
			l.NoPvP = len(args) > 1 && strings.EqualFold(args[1], "off")
		case "deny", "allow":
			if len(args) < 2 {
				err = errors.New("/limits " + args[0] + " <bloque>")
				break
			}
			name := strings.TrimPrefix(strings.ToLower(args[1]), "minecraft:")
			list := append([]string(nil), l.neverBreak()...)
			out := list[:0]
			for _, x := range list {
				if x != name {
					out = append(out, x)
				}
			}
			if strings.EqualFold(args[0], "deny") {
				out = append(out, name)
			}
			l.NeverBreak = append([]string{}, out...)
		case "reset":
			*l = limits{}
		default:
			err = errors.New("/limits [area|pvp|deny|allow|reset]")
		}
		if err == nil {
			err = saveConfigLocked()
		}
		cfgMu.Unlock()
		if err != nil {
			return err
		}
	}
	log.Printf("límites: %s", describeLimits())
	if nb := currentLimits().neverBreak(); len(nb) > 0 {
		log.Printf("  no rompe: %s (los que puso él, sí)", strings.Join(nb, ", "))
	}
	return nil
}
