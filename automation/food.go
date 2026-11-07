package automation

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/entities"
)

// Comida: en difícil el hambre mata (con la comida a 0 se pierde vida hasta
// morir), así que ninguna tarea sigue con hambre y sin nada que comer: para,
// caza (vacas, cerdos, ovejas que no sean del corral, pollos, conejos),
// cocina la carne si puede (si no, se come cruda; el pollo crudo no) o hace
// pan con el trigo, y come.

// edible son todas las comidas de vanilla, con lo que alimentan (la
// nutrición del componente food). Las malas (badFood) están para
// reconocerlas, pero no se cogen ni se cuentan como comida. Un objeto que no
// esté aquí y tenga el componente food (de un plugin, de una versión nueva)
// también vale: foodValue.
var edible = map[string]int{
	"cooked_beef": 8, "cooked_porkchop": 8, "cooked_mutton": 6, "cooked_chicken": 6, "cooked_rabbit": 5,
	"cooked_cod": 5, "cooked_salmon": 6, "bread": 5, "baked_potato": 5, "apple": 4, "carrot": 3,
	"golden_carrot": 6, "golden_apple": 4, "enchanted_golden_apple": 4, "pumpkin_pie": 8,
	"mushroom_stew": 6, "rabbit_stew": 10, "beetroot_soup": 6, "suspicious_stew": 6, "beetroot": 1,
	"potato": 1, "melon_slice": 2, "sweet_berries": 2, "glow_berries": 2, "cookie": 2, "dried_kelp": 1,
	"honey_bottle": 6, "beef": 3, "porkchop": 3, "mutton": 2, "rabbit": 3, "cod": 2, "salmon": 2,
	"tropical_fish": 1,
	// malas: hambre, veneno, náusea o teletransporte
	"chicken": 2, "rotten_flesh": 4, "spider_eye": 2, "poisonous_potato": 2, "pufferfish": 1, "chorus_fruit": 4,
}

// badFood: comida que mejor no (pollo crudo y carne podrida dan hambre; ojo
// de araña, patata venenosa y pez globo envenenan; la fruta de chorus
// teletransporta, en plena tarea o pelea).
var badFood = map[string]bool{
	"chicken": true, "rotten_flesh": true, "spider_eye": true, "poisonous_potato": true, "pufferfish": true, "chorus_fruit": true,
}

// reserveFood: las manzanas doradas son para la pelea (absorción y
// regeneración): se cogen y cuentan, pero para el hambre solo si no hay otra.
var reserveFood = map[string]bool{"golden_apple": true, "enchanted_golden_apple": true}

// goodFood: comida que vale para comer (en la lista y no mala).
func goodFood(it string) (int, bool) {
	n, ok := edible[it]
	return n, ok && !badFood[it]
}

// rawToCooked: lo que se cocina en el horno.
var rawToCooked = map[string]string{
	"beef": "cooked_beef", "porkchop": "cooked_porkchop", "mutton": "cooked_mutton",
	"chicken": "cooked_chicken", "rabbit": "cooked_rabbit", "potato": "baked_potato",
}

// prey son los animales que se cazan para comer.
var prey = []string{"minecraft:cow", "minecraft:pig", "minecraft:sheep", "minecraft:chicken", "minecraft:rabbit", "minecraft:mooshroom"}

const (
	hungryAt  = 8 // con la comida aquí o menos, a comer (o a buscar comida)
	foodStock = 6 // lo que se junta al cazar: comida para un rato
)

// FoodItems es cuánta comida lleva (piezas que se pueden comer).
func (j *Job) FoodItems() int {
	n := 0
	for it, c := range j.Items() {
		if _, ok := goodFood(it); ok {
			n += c
		}
	}
	return n
}

// food es la comida que lleva que más alimenta ("" ninguna).
func (j *Job) food() string {
	best, bestN := "", 0
	for it := range j.Items() {
		if n, ok := goodFood(it); ok && !reserveFood[it] && n > bestN {
			best, bestN = it, n
		}
	}
	if best == "" { // (solo manzanas doradas: antes eso que morir de hambre)
		for it := range j.Items() {
			if reserveFood[it] {
				return it
			}
		}
	}
	return best
}

// Eat come algo de lo que lleva (lo que más alimenta).
func (j *Job) Eat() error {
	it := j.food()
	if it == "" {
		return errors.New("no llevo comida")
	}
	if err := j.Hold(it); err != nil {
		return err
	}
	j.r.d.Walker.Stop()
	before := j.r.d.Player.Status().Food
	done := make(chan act.Use, 1)
	j.r.d.Hands.Eat(func(u act.Use) { done <- u })
	select {
	case u := <-done:
		if u.Err != nil {
			return u.Err
		}
	case <-time.After(10 * time.Second):
		return errors.New("comer: no termina")
	case <-j.ctx.Done():
		return j.ctx.Err()
	}
	_ = j.r.d.Ctl.WaitTicks(j.ctx, 4)
	j.Logf("como %s (comida %d → %d)", it, before, j.r.d.Player.Status().Food)
	return nil
}

// hungerCheck: con hambre y sin comida, a por comida; con comida, a comer.
// (feeding: ya se está buscando comida: Wait no vuelve a empezar.)
func (j *Job) hungerCheck() error {
	if j.feeding {
		return nil
	}
	st := j.r.d.Player.Status()
	if st.Food > hungryAt || st.Health <= 0 {
		return nil
	}
	j.feeding = true
	defer func() { j.feeding = false }()
	if j.FoodItems() == 0 && time.Now().After(j.foodRetry) {
		j.Logf("tengo hambre (comida %d) y nada que comer: voy a por comida", st.Food)
		if err := j.EnsureFood(foodStock); err != nil {
			j.foodRetry = time.Now().Add(time.Minute) // no a cada paso: dentro de un minuto, otra vez
			if j.ctx.Err() != nil {
				return j.ctx.Err()
			}
			if j.FoodItems() == 0 && st.Food <= 2 {
				return fmt.Errorf("me muero de hambre y no encuentro comida: %w", err)
			}
			j.Logf("comida: %v", err)
		}
	}
	// comer: lo hace el módulo de pelea fuera de combate (si está); si en unos
	// segundos no ha comido, come la tarea
	for t := 0; t < 6 && j.r.d.HasFood != nil && j.r.d.HasFood() && j.r.d.Player.Status().Food <= hungryAt; t++ {
		if err := j.r.d.Ctl.WaitTicks(j.ctx, 20); err != nil {
			return err
		}
	}
	for k := 0; k < 4 && j.r.d.Player.Status().Food <= hungryAt+6 && j.FoodItems() > 0; k++ {
		if err := j.Eat(); err != nil {
			if j.ctx.Err() != nil {
				return j.ctx.Err()
			}
			j.Logf("comer: %v", err)
			break
		}
	}
	return nil
}

// EnsureFood junta comida hasta llevar n piezas: pan del trigo que lleve,
// cazando animales de alrededor (y explorando un poco si no ve ninguno), y
// cocinando la carne cruda si se puede.
func (j *Job) EnsureFood(n int) error {
	// pan: 3 de trigo cada uno
	if w := j.Count("wheat"); w >= 3 && j.FoodItems() < n {
		if err := j.CraftAtTable([]string{"wheat", "wheat", "wheat"}, 3, min(w/3, n), nil); err != nil {
			j.Logf("pan: %v", err)
		}
	}
	// los cofres de comida (los marcados, los del almacén, los de la base)
	// antes que cazar: es comida hecha, y está en casa
	if j.FoodItems() < n {
		got, err := j.FoodFromChests(max(n, foodChestWant))
		if err != nil {
			return err
		}
		if got > 0 || j.FoodItems() >= n {
			return nil // (lo que había en casa: no se sale a cazar por el resto)
		}
	}
	if j.Underground() { // los animales están arriba
		if err := j.ToSky(); err != nil {
			if j.Fatal() != nil {
				return j.Fatal()
			}
			j.Logf("%v", err)
		}
	}
	misses := 0
	for tries := 0; j.FoodItems()+j.rawMeat() < n && tries < 20 && misses < 4; tries++ {
		if err := j.r.d.Ctl.WaitTicks(j.ctx, 1); err != nil {
			return err
		}
		j.Status("cazando para comer (%d de %d)", j.FoodItems()+j.rawMeat(), n)
		animal, ok := j.nearestPrey(48)
		if !ok && j.FoodItems()+j.rawMeat() > 0 {
			break // algo ya tengo: no se va a explorar por más
		}
		if !ok {
			misses++
			j.Logf("no veo animales: exploro")
			if err := j.explore(j.Feet(), misses+tries); err != nil {
				j.Logf("explorar: %v", err)
			}
			continue
		}
		if err := j.Hunt(animal.ID); err != nil {
			if j.ctx.Err() != nil {
				return j.ctx.Err()
			}
			j.Logf("cazar %s: %v", animal.Type, err)
			continue
		}
		_ = j.Collect(6)
	}
	j.cookMeat()
	if j.FoodItems() == 0 {
		return errors.New("no consigo comida (ni animales ni trigo cerca)")
	}
	return nil
}

// rawMeat es la carne cruda que lleva (aún no cuenta como comida si es pollo).
func (j *Job) rawMeat() int {
	n := 0
	for raw := range rawToCooked {
		n += j.Count(raw)
	}
	return n
}

// cookMeat cocina la carne cruda (horno: el de cerca o uno nuevo, si hay
// adoquines); lo que no se pueda cocinar se queda crudo (el pollo crudo no se come).
func (j *Job) cookMeat() {
	for raw := range rawToCooked {
		c := j.Count(raw)
		if c == 0 {
			continue
		}
		j.Status("cocinando %s", raw)
		if err := j.Smelt(raw, c); err != nil {
			j.Logf("cocinar %s: %v (me la como cruda si se puede)", raw, err)
		}
	}
}

// nearestPrey es el animal más cercano que se puede cazar (no las ovejas del corral).
func (j *Job) nearestPrey(r float64) (entities.Entity, bool) {
	p := j.Pos()
	var cs []entities.Entity
	for _, e := range j.r.d.Entities.Nearby(p.X, p.Y, p.Z, r) {
		ok := false
		for _, k := range prey {
			ok = ok || e.Type == k
		}
		if !ok {
			continue
		}
		if pen := j.r.Store.Pen; pen != nil && pen.inside(e.X, e.Z) {
			continue // las del corral son para lana
		}
		if e.Type == "minecraft:sheep" && sheepOf(e).baby {
			continue
		}
		cs = append(cs, e)
	}
	if len(cs) == 0 {
		return entities.Entity{}, false
	}
	sort.Slice(cs, func(a, b int) bool {
		return math.Hypot(cs[a].X-p.X, cs[a].Z-p.Z) < math.Hypot(cs[b].X-p.X, cs[b].Z-p.Z)
	})
	return cs[0], true
}

// Hunt persigue al animal id y le pega (con la espada o el hacha) hasta matarlo.
func (j *Job) Hunt(id int32) error {
	for _, w := range []string{j.best("sword"), j.best("axe")} {
		if w != "" {
			_ = j.Hold(w)
			break
		}
	}
	where := func() (entities.Entity, bool) { return j.r.d.Entities.Get(id) }
	for hits := 0; hits < 30; hits++ {
		e, ok := where()
		if !ok {
			return nil // muerto (o se fue)
		}
		p := j.Pos()
		if math.Sqrt(e.DistanceSqr(p.X, p.Y+1.62, p.Z)) > 2.8 {
			if err := j.goNearEntity(id, 2); err != nil {
				return err
			}
		}
		done := make(chan act.Attack, 1)
		j.r.d.Hands.Attack(id, where, func(a act.Attack) { done <- a })
		select {
		case a := <-done:
			if a.Err != nil && a.Err.Error() != "act: the entity is gone" {
				continue // fuera de alcance: acercarse otra vez
			}
		case <-time.After(5 * time.Second):
		case <-j.ctx.Done():
			return j.ctx.Err()
		}
		if err := j.r.d.Ctl.WaitTicks(j.ctx, 2); err != nil {
			return err
		}
	}
	if _, ok := where(); ok {
		return errors.New("no consigo matarlo")
	}
	return nil
}

// FindFood es la tarea de buscar comida (la lanza el bot solo si se queda
// sin comida y con hambre sin nada que hacer).
func FindFood() func(j *Job) error {
	return func(j *Job) error {
		j.feeding = true
		err := j.EnsureFood(foodStock)
		j.feeding = false
		for k := 0; k < 4 && j.r.d.Player.Status().Food < 18 && j.FoodItems() > 0; k++ {
			if e := j.Eat(); e != nil {
				break
			}
		}
		return err
	}
}

// PickUp es la tarea de recoger lo que haya tirado a menos de r (lo que se
// cayó al morir).
func PickUp(r float64) func(j *Job) error {
	return func(j *Job) error {
		j.Status("recogiendo lo que hay tirado")
		before := len(j.Items())
		if err := j.Collect(r); err != nil {
			return err
		}
		j.Logf("recogido: tenía %d tipos de objetos, ahora %d", before, len(j.Items()))
		return nil
	}
}
