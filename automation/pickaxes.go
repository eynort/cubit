package automation

import (
	"errors"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26/level/component"
	"github.com/mj41/go-mc26/protocol/types"
)

// Los picos que deja el operador en el cofre de minar (/mine chest): el bot los usa
// sin romperlos nunca. Uno con poca durabilidad deja de usarse para minar
// (se devuelve al cofre, para repararlo); al volver a casa, como el camino es
// casi todo aire, se apura un poco más.

const (
	// pickReserve: con menos usos que estos el pico ya no mina (se guarda).
	pickReserve = 40
	// pickHomeward: de vuelta a casa se usa hasta aquí (salir del túnel, algún bloque suelto).
	pickHomeward = 4
)

// ErrNoPicks: no queda ningún pico con durabilidad de sobra.
var ErrNoPicks = errors.New("no me quedan picos sin gastar")

// pickLeft son los usos que le quedan a un pico (-1: no es pico de diamante o
// netherita; muchísimos si es irrompible).
func pickLeft(s types.ItemStack) int { return pickLeftAs(s, goodPick) }

// pickLeftAs es pickLeft con otra idea de qué pico vale (ok).
func pickLeftAs(s types.ItemStack, ok func(string) bool) int {
	if s.Count <= 0 || !ok(strings.TrimPrefix(itemName(int32(s.Item)), "minecraft:")) {
		return -1
	}
	if act.Component[*component.Unbreakable](s) != nil {
		return 1 << 30
	}
	maxd := act.Component[*component.MaxDamage](s)
	if maxd == nil {
		return 1 << 30
	}
	dmg := 0
	if d := act.Component[*component.Damage](s); d != nil {
		dmg = int(d.Value)
	}
	return int(maxd.Value) - dmg
}

// goodPick: un pico que saca escombros ancestrales.
func goodPick(name string) bool { return name == "diamond_pickaxe" || name == "netherite_pickaxe" }

// isPick: un pico de los que vale para la tarea (pickOK; si no, goodPick).
func (j *Job) isPick(name string) bool {
	if j.pickOK != nil {
		return j.pickOK(name)
	}
	return goodPick(name)
}

// pickLeft son los usos que le quedan a s si es un pico de los de la tarea (-1 si no).
func (j *Job) pickLeft(s types.ItemStack) int { return pickLeftAs(s, j.isPick) }

// bestPickSlot es el slot del menú del pico a usar con más de floor usos: el
// de netherita antes que el de diamante y, del mismo, el más gastado (se
// acaba uno antes de empezar el siguiente). -1: ninguno.
func (j *Job) bestPickSlot(floor int) int {
	s := j.r.d.Screens
	s.Lock()
	defer s.Unlock()
	best, bestNether, bestLeft := -1, false, 0
	for i := invStart; i <= invEnd; i++ {
		sl := s.Inventory.Slots[i]
		left := j.pickLeft(sl)
		if left <= floor {
			continue
		}
		nether := itemName(int32(sl.Item)) == "minecraft:netherite_pickaxe"
		if best < 0 || nether && !bestNether || nether == bestNether && left < bestLeft {
			best, bestNether, bestLeft = i, nether, left
		}
	}
	return best
}

// UsablePicks cuenta los picos que aún minan (más de pickReserve usos).
func (j *Job) UsablePicks() int {
	s := j.r.d.Screens
	s.Lock()
	defer s.Unlock()
	n := 0
	for i := invStart; i <= invEnd; i++ {
		if j.pickLeft(s.Inventory.Slots[i]) > pickReserve {
			n++
		}
	}
	return n
}

// pickUses suma los usos (por encima de la reserva) de los picos que lleva.
func (j *Job) pickUses() int {
	s := j.r.d.Screens
	s.Lock()
	defer s.Unlock()
	n := 0
	for i := invStart; i <= invEnd; i++ {
		if l := j.pickLeft(s.Inventory.Slots[i]); l > pickReserve {
			n += min(l-pickReserve, 1<<20)
		}
	}
	return n
}

// holdSlot se pone en la mano lo del slot i del menú del inventario.
func (j *Job) holdSlot(i int) error {
	j.CloseScreen()
	if i < hotbar { // del inventario a la barra, al último hueco (el 8)
		if err := j.r.d.Screens.Click(0, i, 8, types.ContainerInputSwap); err != nil {
			return err
		}
		if err := j.Wait(3); err != nil {
			return err
		}
		i = hotbar + 8
	}
	s := j.r.d.Screens
	s.Lock()
	held := s.HeldSlot
	s.Unlock()
	if held != i-hotbar {
		if err := s.SelectHotbar(i - hotbar); err != nil {
			return err
		}
		return j.Wait(1)
	}
	return nil
}

// chestFoods: lo que se coge del cofre para comer, de lo que más alimenta a lo que menos.
var chestFoods = []string{"golden_carrot", "cooked_beef", "cooked_porkchop", "cooked_mutton", "baked_potato",
	"bread", "cooked_chicken", "cooked_salmon", "cooked_cod", "golden_apple"}

var bedColors = []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
	"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"}
