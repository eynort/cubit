package automation

// Utilidades pequeñas que usan varias tareas (excavar, cavar, recoger).

import (
	"fmt"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/world"
)

const (
	overworld = "minecraft:overworld"
	theNether = "minecraft:the_nether"
)

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Dimension es la dimensión donde está el bot.
func (j *Job) Dimension() string { return string(j.r.d.Player.Spawn.Dimension) }

func (j *Job) inNether() bool { return j.Dimension() == theNether }

// dimName es el nombre de una dimensión para el log.
func dimName(dim string) string {
	switch dim {
	case theNether:
		return "el Nether"
	case overworld:
		return "el mundo normal"
	}
	return dim
}

// dimNameEN es dimName en inglés.
func dimNameEN(dim string) string {
	switch dim {
	case theNether:
		return "the Nether"
	case overworld:
		return "the overworld"
	}
	return dim
}

// standAt deja los pies en p (andando, o cavando hasta allí).
func (j *Job) standAt(p world.BlockPos) error {
	if j.Feet() == p {
		return nil
	}
	if err := j.GoTo(p); err == nil && j.Feet() == p {
		return nil
	}
	if err := j.TunnelTo(p); err != nil {
		return err
	}
	for _, c := range []world.BlockPos{{X: p.X, Y: p.Y + 1, Z: p.Z}, p} {
		if err := j.clearCell(c); err != nil {
			return err
		}
	}
	if f := j.Feet(); f != p && (abs(f.X-p.X)+abs(f.Z-p.Z) > 1 || f.Y != p.Y) {
		if err := j.TunnelTo(p); err != nil { // (no al lado: una vuelta más de túnel)
			return err
		}
	}
	if len(j.trail) == 0 || j.trail[len(j.trail)-1] != j.Feet() {
		j.trail = append(j.trail, j.Feet())
	}
	return j.GoTo(p)
}

// naturalLeaves: hojas que crecieron solas (las que pone un jugador son
// "persistent=true"); lo que cae encima se tira rompiéndolas.
func (j *Job) naturalLeaves(p world.BlockPos) bool {
	st, ok := j.r.d.World.BlockAt(p)
	if !ok {
		return false
	}
	s := world.StateString(st)
	if strings.Contains(s, "_leaves") {
		return strings.Contains(s, "persistent=false")
	}
	return strings.Contains(s, "wart_block") || strings.Contains(s, "shroomlight")
}

// edible son las comidas de vanilla con lo que alimentan; las malas (badFood)
// están para reconocerlas, no para comerlas.
var edible = map[string]int{
	"cooked_beef": 8, "cooked_porkchop": 8, "cooked_mutton": 6, "cooked_chicken": 6, "cooked_rabbit": 5,
	"cooked_cod": 5, "cooked_salmon": 6, "bread": 5, "baked_potato": 5, "apple": 4, "carrot": 3,
	"golden_carrot": 6, "golden_apple": 4, "enchanted_golden_apple": 4, "pumpkin_pie": 8,
	"mushroom_stew": 6, "rabbit_stew": 10, "beetroot_soup": 6, "suspicious_stew": 6, "beetroot": 1,
	"potato": 1, "melon_slice": 2, "sweet_berries": 2, "glow_berries": 2, "cookie": 2, "dried_kelp": 1,
	"honey_bottle": 6, "beef": 3, "porkchop": 3, "mutton": 2, "rabbit": 3, "cod": 2, "salmon": 2,
	"tropical_fish": 1,
	"chicken":       2, "rotten_flesh": 4, "spider_eye": 2, "poisonous_potato": 2, "pufferfish": 1, "chorus_fruit": 4,
}

var badFood = map[string]bool{
	"chicken": true, "rotten_flesh": true, "spider_eye": true, "poisonous_potato": true, "pufferfish": true, "chorus_fruit": true,
}

// goodFood: it es comida que se come (y cuánto alimenta).
func goodFood(it string) (int, bool) {
	n, ok := edible[it]
	return n, ok && !badFood[it]
}

// oreDrops: lo que sueltan los minerales al picarlos (se recoge siempre).
var oreDrops = map[string]bool{
	"coal": true, "raw_iron": true, "raw_copper": true, "raw_gold": true, "diamond": true, "emerald": true,
	"lapis_lazuli": true, "redstone": true, "quartz": true, "gold_nugget": true, "ancient_debris": true, "amethyst_shard": true,
}

// pickWait: lo que espera a que le tiren un pico cuando no le queda ninguno.
var pickWait = time.Minute

// say lo dice por el chat del juego (y en el log).
func (j *Job) say(text string) {
	j.Logf("chat: %s", text)
	if j.r.d.Say != nil {
		j.r.d.Say(text)
	}
}

// sayf lo dice en el chat en el idioma del bot. Los mensajes suenan a un
// colega que juega contigo, no a una máquina: en minúsculas, cortos y sin
// prefijos ni corchetes.
func (j *Job) sayf(en, es string, args ...any) {
	t := en
	if j.r.d.Lang != nil && j.r.d.Lang() == "es" {
		t = es
	}
	j.say(fmt.Sprintf(t, args...))
}

// NoteChat: llegó un mensaje de un jugador.
func (r *Runner) NoteChat() { r.chats.Add(1) }

// PickUp es la tarea de recoger lo que haya tirado a menos de r (lo que se
// cayó al morir).
func PickUp(r float64) func(j *Job) error {
	return func(j *Job) error {
		j.StatusT("picking up items nearby", "recogiendo lo que hay tirado")
		before := len(j.Items())
		if err := j.Collect(r); err != nil {
			return err
		}
		j.Logf("recogido: tenía %d tipos de objetos, ahora %d", before, len(j.Items()))
		return nil
	}
}
