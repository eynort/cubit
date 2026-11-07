package automation

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/act"
)

// Oro para el Nether: con una pieza de armadura de oro puesta los piglins no
// atacan (PiglinAi.isWearingSafeArmor). Las botas son la más barata (4 lingotes).

// goldLevel: el oro del mundo normal abunda hacia la altura -16.
const goldLevel = -16

func goldOre(n string) bool { return n == "gold_ore" || n == "deepslate_gold_ore" }

// armorSlot es el slot del menú del inventario de cada pieza (5 cabeza … 8 pies).
var armorSlot = map[string]int{"helmet": 5, "chestplate": 6, "leggings": 7, "boots": 8}

// WearingGold dice si lleva puesta alguna pieza de oro.
func (j *Job) WearingGold() bool {
	s := j.r.d.Screens
	s.Lock()
	defer s.Unlock()
	for i := 5; i <= 8; i++ {
		if sl := s.Inventory.Slots[i]; sl.Count > 0 && strings.HasPrefix(itemName(int32(sl.Item)), "minecraft:golden_") {
			return true
		}
	}
	return false
}

// ensureGoldIngots consigue n lingotes de oro: mina el oro que falte (con pico
// de hierro, a la altura del oro) y lo funde.
func (j *Job) ensureGoldIngots(n int) error {
	have := func() int { return j.Count("gold_ingot") + j.Count("raw_gold") }
	if have() < n {
		if j.inNether() {
			return errors.New("me falta oro y estoy en el Nether (allí picar oro enfada a los piglins)")
		}
		if err := j.EnsureIronPickaxe(); err != nil {
			return err
		}
		j.Logf("objetivo: %d de oro (a la altura %d)", n-have(), goldLevel)
		err := j.MineAt("oro", goldOre, have, n, goldLevel, 30)
		if serr := j.Surface(); serr != nil {
			j.Logf("%v", serr)
		}
		if err != nil {
			return fmt.Errorf("oro: %w", err)
		}
	}
	if need := n - j.Count("gold_ingot"); need > 0 {
		j.Status("fundiendo oro")
		if err := j.Smelt("raw_gold", min(need, j.Count("raw_gold"))); err != nil {
			return fmt.Errorf("fundir oro: %w", err)
		}
	}
	if j.Count("gold_ingot") < n {
		return fmt.Errorf("solo tengo %d lingotes de oro de %d", j.Count("gold_ingot"), n)
	}
	return nil
}

// EnsureGoldArmor se pone una pieza de oro (la que lleve; si no, hace unas
// botas): los piglins la respetan.
func (j *Job) EnsureGoldArmor() error {
	if j.WearingGold() {
		return nil
	}
	piece := ""
	for _, p := range []string{"golden_boots", "golden_helmet", "golden_leggings", "golden_chestplate"} {
		if j.Count(p) > 0 {
			piece = p
			break
		}
	}
	if piece == "" {
		j.Logf("objetivo: botas de oro (para que los piglins no me ataquen)")
		if err := j.ensureGoldIngots(4); err != nil {
			return err
		}
		g := "gold_ingot"
		if err := j.CraftAtTable([]string{g, "", g, g, "", g}, 3, 1, nil); err != nil {
			return fmt.Errorf("botas de oro: %w", err)
		}
		if j.Count("golden_boots") == 0 {
			return errors.New("botas de oro: la mesa no las dio")
		}
		piece = "golden_boots"
	}
	return j.wear(piece)
}

// wear se pone la pieza de armadura item como un jugador: en la mano y clic
// derecho al aire (se cambia por la que llevara puesta: Equippable.swapWithEquipmentSlot).
func (j *Job) wear(item string) error {
	if armorSlot[item[strings.LastIndex(item, "_")+1:]] == 0 {
		return fmt.Errorf("%s no es armadura", item)
	}
	if err := j.Hold(item); err != nil {
		return err
	}
	j.r.d.Walker.Stop()
	p := j.Pos()
	yaw := float64(p.Yaw) * math.Pi / 180
	done := make(chan act.Use, 1)
	// hacia delante y arriba (al cielo): sin bloque bajo el cursor que se quede el clic
	j.r.d.Hands.UseItemToward(p.X-math.Sin(yaw)*2, p.Y+5, p.Z+math.Cos(yaw)*2, func(u act.Use) { done <- u })
	select {
	case u := <-done:
		if u.Err != nil {
			return u.Err
		}
	case <-time.After(5 * time.Second):
		return errors.New("ponerse la armadura: no contesta")
	case <-j.ctx.Done():
		return j.ctx.Err()
	}
	if err := j.Wait(4); err != nil {
		return err
	}
	if strings.HasPrefix(item, "golden_") && !j.WearingGold() {
		return fmt.Errorf("no consigo ponerme %s", item)
	}
	j.Logf("me pongo %s", item)
	return nil
}
